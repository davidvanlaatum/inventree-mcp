package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/oauth"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
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
	srv := New(deps)
	switch cfg.Transport {
	case config.TransportStdio:
		return RunStdio(ctx, srv)
	case config.TransportHTTP:
		return RunHTTP(ctx, cfg, srv)
	default:
		return cfg.Validate()
	}
}

func RunStdio(ctx context.Context, srv *mcp.Server) error {
	ctx = WithTransportLogger(ctx, string(config.TransportStdio))
	return srv.Run(ctx, &mcp.StdioTransport{})
}

func RunHTTP(ctx context.Context, cfg config.Config, srv *mcp.Server) error {
	handler, err := HTTPMux(ctx, cfg, srv)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:    cfg.Listen,
		Handler: handler,
	}
	return httpServer.ListenAndServe()
}

func HTTPMux(ctx context.Context, cfg config.Config, srv *mcp.Server) (http.Handler, error) {
	handler := HTTPHandler(ctx, srv)
	mux := http.NewServeMux()
	if cfg.Transport == config.TransportHTTP && cfg.Environment == config.EnvironmentProduction {
		keyring, err := cfg.OAuthKeyring.Keyring()
		if err != nil {
			return nil, err
		}
		verifier := oauth.AccessTokenVerifier(oauth.EnvelopeCodec{Keyring: keyring}, cfg.OAuthIssuerURL, cfg.OAuthResourceURL, cfg.OAuthClientIDs, nil)
		handler = auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
			ResourceMetadataURL: cfg.OAuthProtectedResourceMetadataURL(),
		})(handler)
		mux.Handle("/.well-known/oauth-protected-resource", auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
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
	}
	mux.Handle(cfg.Path, handler)
	return mux, nil
}

func HTTPHandler(ctx context.Context, srv *mcp.Server) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
		Logger:    logging.FromContext(ctx),
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
