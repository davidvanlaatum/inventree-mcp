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
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const (
	defaultHTTPReadHeaderTimeout = 10 * time.Second
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

func Run(ctx context.Context, cfg config.Config, deps tools.Dependencies) error {
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
		return RunHTTP(ctx, cfg, srv, traffic)
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

func RunHTTP(ctx context.Context, cfg config.Config, srv *mcp.Server, traffic *trafficLog) error {
	handler, err := httpMux(ctx, cfg, srv, traffic)
	if err != nil {
		return err
	}
	httpServer := newHTTPServer(ctx, cfg, handler)
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	return serveHTTP(ctx, httpServer, listener)
}

func newHTTPServer(ctx context.Context, cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: defaultHTTPReadHeaderTimeout,
		BaseContext: func(net.Listener) context.Context {
			return context.WithoutCancel(ctx)
		},
	}
}

func serveHTTP(ctx context.Context, httpServer *http.Server, listener net.Listener) error {
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
	handler := HTTPHandler(ctx, srv, cfg.MCPMaxRequestBodyBytes)
	mux := http.NewServeMux()
	if cfg.Transport == config.TransportHTTP && cfg.Environment == config.EnvironmentProduction {
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
	} else if traffic != nil {
		handler = traffic.middleware(string(config.TransportHTTP), cfg.MCPMaxRequestBodyBytes, handler)
	}
	mux.Handle(cfg.Path, handler)
	return mux, nil
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
		requestCtx := logging.WithLogger(req.Context(), logging.FromContext(ctx))
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
