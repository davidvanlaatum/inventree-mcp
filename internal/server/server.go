package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	handler := HTTPHandler(ctx, srv)
	if traffic != nil {
		handler = traffic.middleware(string(config.TransportHTTP), handler)
	}
	mux := http.NewServeMux()
	mux.Handle(cfg.Path, handler)
	httpServer := &http.Server{
		Addr:    cfg.Listen,
		Handler: mux,
	}
	return httpServer.ListenAndServe()
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
