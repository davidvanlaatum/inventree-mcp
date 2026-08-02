package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/oauth"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
	"github.com/davidvanlaatum/inventree-mcp/internal/systemdnotify"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const (
	defaultHTTPReadHeaderTimeout = 10 * time.Second
	defaultHTTPReadTimeout       = 30 * time.Second
	defaultHTTPShutdownTimeout   = 10 * time.Second
)

func New(deps tools.Dependencies) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "inventree-mcp",
		Title:   "InvenTree MCP",
		Version: buildinfo.Version,
	}, nil)
	tools.Register(srv, deps)
	return srv
}

func Run(ctx context.Context, cfg config.Config, deps tools.Dependencies, notifier systemdnotify.Notifier) error {
	if cfg.Transport == config.TransportHTTP && deps.EnableWriteTools && deps.AuthorizationMode != tools.AuthorizationModeOAuth {
		return errors.New("HTTP transport cannot register write tools without per-tool OAuth scope enforcement")
	}
	var traffic *trafficLog
	var closer io.Closer
	if cfg.DebugTrafficLog != "" {
		var err error
		traffic, closer, err = openTrafficLog(cfg.DebugTrafficLog)
		if err != nil {
			return fmt.Errorf("open debug traffic log: %w", err)
		}
		defer func() {
			_ = closer.Close()
		}()
	}
	srv := New(deps)
	switch cfg.Transport {
	case config.TransportStdio:
		return RunStdio(ctx, srv, traffic)
	case config.TransportHTTP:
		return RunHTTP(ctx, cfg, srv, traffic, notifier)
	default:
		return cfg.Validate()
	}
}

func RunStdio(ctx context.Context, srv *mcp.Server, traffic *trafficLog) error {
	ctx = WithTransportLogger(ctx, string(config.TransportStdio))
	transport := mcp.Transport(&mcp.StdioTransport{})
	if traffic != nil {
		transport = loggingTransport{transport: transport, log: traffic, name: string(config.TransportStdio)}
	}
	return srv.Run(ctx, transport)
}

func RunHTTP(ctx context.Context, cfg config.Config, srv *mcp.Server, traffic *trafficLog, notifier systemdnotify.Notifier) error {
	handler, err := httpMux(ctx, cfg, srv, traffic)
	if err != nil {
		return err
	}
	httpServer := newHTTPServer(ctx, cfg, handler)
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
	}()
	return serveHTTP(ctx, httpServer, listener, notifier)
}

func newHTTPServer(ctx context.Context, cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: defaultHTTPReadHeaderTimeout,
		ReadTimeout:       defaultHTTPReadTimeout,
		BaseContext: func(net.Listener) context.Context {
			return context.WithoutCancel(ctx)
		},
	}
}

func serveHTTP(ctx context.Context, httpServer *http.Server, listener net.Listener, notifier systemdnotify.Notifier) error {
	if err := notifier.Ready(); err != nil {
		return fmt.Errorf("notify systemd that HTTP service is ready: %w", err)
	}

	watchdogCtx, stopWatchdog := context.WithCancel(context.WithoutCancel(ctx))
	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		notifier.RunWatchdog(watchdogCtx, func(err error) {
			logger := logging.FromContext(ctx)
			logger.Error("systemd watchdog notification failed; continuing until systemd terminates the service", logging.Err(err))
			if notifyErr := notifier.Degraded(); notifyErr != nil {
				logger.Error("failed to publish degraded systemd status", logging.Err(notifyErr))
			}
		})
	}()
	defer func() {
		stopWatchdog()
		<-watchdogDone
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.Serve(listener)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		if err := notifier.Stopping(); err != nil {
			logging.FromContext(ctx).Error("failed to notify systemd that service is stopping", logging.Err(err))
		}
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultHTTPShutdownTimeout)
		defer cancel()
		shutdownErr := httpServer.Shutdown(shutdownCtx)
		var closeErr error
		if shutdownErr != nil {
			closeErr = httpServer.Close()
		}
		serveErr := <-errCh
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		if shutdownErr == nil && closeErr == nil && serveErr == nil {
			return ctx.Err()
		}
		return errors.Join(shutdownErr, closeErr, serveErr)
	}
}

func HTTPMux(ctx context.Context, cfg config.Config, srv *mcp.Server) (http.Handler, error) {
	return httpMux(ctx, cfg, srv, nil)
}

func httpMux(ctx context.Context, cfg config.Config, srv *mcp.Server, traffic *trafficLog) (http.Handler, error) {
	return httpMuxWithMetadataClient(ctx, cfg, srv, traffic, nil)
}

func httpMuxWithMetadataClient(ctx context.Context, cfg config.Config, srv *mcp.Server, traffic *trafficLog, metadataClient *http.Client) (http.Handler, error) {
	return httpMuxWithOptions(ctx, cfg, srv, traffic, httpMuxOptions{metadataClient: metadataClient})
}

type httpMuxOptions struct {
	metadataClient         *http.Client
	now                    func() time.Time
	authorizationRateLimit int
}

func httpMuxWithOptions(ctx context.Context, cfg config.Config, srv *mcp.Server, traffic *trafficLog, options httpMuxOptions) (http.Handler, error) {
	sourceResolver, err := newSourceIPResolver(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	handler := HTTPHandler(ctx, srv, cfg.MCPMaxRequestBodyBytes)
	mux := http.NewServeMux()
	if cfg.Transport == config.TransportHTTP && cfg.Environment == config.EnvironmentProduction {
		if err := cfg.ValidateProductionRoutePaths(); err != nil {
			return nil, err
		}
		keyring, err := cfg.OAuthKeyring.Keyring()
		if err != nil {
			return nil, err
		}
		verifier := oauth.AccessTokenVerifier(oauth.EnvelopeCodec{Keyring: keyring}, cfg.OAuthIssuerURL, cfg.OAuthResourceURL, cfg.OAuthClientIDs, nil)
		if traffic != nil {
			handler = traffic.middleware(string(config.TransportHTTP), cfg.MCPMaxRequestBodyBytes, handler)
		}
		handler = auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
			ResourceMetadataURL: cfg.OAuthProtectedResourceMetadataURL(),
		})(handler)
		metadataURL, err := url.Parse(cfg.OAuthProtectedResourceMetadataURL())
		if err != nil || metadataURL.Path == "" {
			return nil, errors.New("invalid OAuth protected-resource metadata URL")
		}
		mux.Handle(metadataURL.Path, auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
			Resource:                      cfg.OAuthResourceURL,
			AuthorizationServers:          []string{cfg.OAuthIssuerURL},
			ScopesSupported:               supportedOAuthScopes(),
			BearerMethodsSupported:        []string{"header"},
			ResourceName:                  "InvenTree MCP",
			ResourceDocumentation:         "https://github.com/davidvanlaatum/inventree-mcp",
			ResourcePolicyURI:             "",
			ResourceTOSURI:                "",
			DPOPSigningAlgValuesSupported: nil,
		}))
		metadataClient := options.metadataClient
		if metadataClient == nil {
			metadataClient = &http.Client{Timeout: oauth.DefaultClientMetadataTimeout}
		}
		now := options.now
		if now == nil {
			now = time.Now
		}
		metadataFetcher := oauth.ClientMetadataFetcher{
			HTTPClient:       metadataClient,
			AllowedOrigins:   append([]string(nil), cfg.OAuthClientIDs...),
			AllowedClientIDs: append([]string(nil), cfg.OAuthClientIDs...),
		}
		broker := oauth.InvenTreeCredentialBroker{
			BaseURL:    cfg.InvenTreeURL,
			HTTPClient: &http.Client{Timeout: cfg.InvenTreeTimeout},
		}
		oauthService := oauth.Service{
			Codec:           oauth.EnvelopeCodec{Keyring: keyring},
			MetadataFetcher: metadataFetcher,
			CodeStore:       oauth.NewCodeStore(1024, now),
			CredentialValidator: oauth.CredentialValidatorFunc(func(ctx context.Context, credential oauth.Credential) error {
				_, err := broker.ValidateCredential(ctx, credential)
				return err
			}),
			AccessLifetime:  cfg.OAuthAccessLifetime,
			RefreshLifetime: cfg.OAuthRefreshLifetime,
			SessionLifetime: cfg.OAuthSessionLifetime,
		}
		authorizationServer := &oauth.AuthorizationServer{
			Issuer:           cfg.OAuthIssuerURL,
			Resource:         cfg.OAuthResourceURL,
			Scopes:           supportedOAuthScopes(),
			Service:          oauthService,
			MetadataFetcher:  metadataFetcher,
			CredentialBroker: broker,
			AssertionVerifier: oauth.PrivateKeyJWTVerifier{
				HTTPClient:  metadataClient,
				ReplayStore: oauth.NewAssertionReplayStore(4096, now),
			},
			RateLimiter: oauth.NewRequestRateLimiter(options.authorizationRateLimit, time.Minute, now),
		}
		if err := authorizationServer.Register(mux); err != nil {
			return nil, err
		}
	} else if traffic != nil {
		handler = traffic.middleware(string(config.TransportHTTP), cfg.MCPMaxRequestBodyBytes, handler)
	}
	mux.Handle(cfg.Path, handler)
	return sourceIPMiddleware(logging.FromContext(ctx), sourceResolver, mux), nil
}

func HTTPHandler(ctx context.Context, srv *mcp.Server, maxRequestBodyBytes int64) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		Logger:                       logging.FromContext(ctx),
		MaxRequestBodyBytes:          maxRequestBodyBytes,
		PropagateRequestCancellation: true,
	})

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		logger := logging.FromContext(ctx)
		if _, ok := requestctx.SourceIP(req.Context()); ok {
			logger = logging.FromContext(req.Context())
		}
		requestCtx := logging.WithLogger(req.Context(), logger)
		requestCtx = WithTransportLogger(requestCtx, string(config.TransportHTTP))
		requestCtx = logging.WithLogger(requestCtx, logging.FromContext(requestCtx).With(
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
		))
		handler.ServeHTTP(w, req.WithContext(requestCtx))
	})
}

func WithTransportLogger(ctx context.Context, transport string) context.Context {
	return logging.WithLogger(ctx, logging.FromContext(ctx).With(slog.String("transport", transport)))
}

func supportedOAuthScopes() []string {
	return []string{
		tools.ScopeInventreeRead,
		tools.ScopeInventreeWrite,
		tools.ScopeInventreeUpload,
		tools.ScopeInventreeOperational,
		tools.ScopeInventreeDestructive,
	}
}
