package platform

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/davidvanlaatum/dvgoutils/logging"
)

type LoggerConfig struct {
	Level  string
	Output io.Writer
}

func NewRootContext(ctx context.Context, cfg LoggerConfig) (context.Context, error) {
	logger, err := NewLogger(cfg)
	if err != nil {
		return nil, err
	}
	return logging.WithLogger(ctx, logger), nil
}

func NewLogger(cfg LoggerConfig) (*slog.Logger, error) {
	output := cfg.Output
	if output == nil {
		output = io.Discard
	}
	level, err := parseLogLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	return slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: RedactLogAttr,
	})), nil
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("log level must be debug, info, warn, or error")
	}
}
