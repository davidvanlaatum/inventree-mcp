package platform

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootContextSeedsLogger(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	ctx, err := NewRootContext(ctx, LoggerConfig{Level: "debug"})
	r.NoError(err)
	r.NotNil(logging.FromContext(ctx))
}

func TestNewLoggerRejectsUnknownLevel(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := NewLogger(LoggerConfig{Level: "verbose"})

	r.Error(err)
	r.Contains(err.Error(), "log level must be")
}

func TestScopedLoggerAttributesSurviveContextReattachment(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With(slog.String("transport", "stdio")))

	logging.FromContext(ctx).InfoContext(ctx, "scoped")

	record := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == "scoped"
	})
	r.NotNil(record)
	a.Equal("scoped", record["msg"])
	a.Equal("stdio", record["transport"])
}

func TestNewLoggerRedactsSensitiveAttributes(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var output strings.Builder
	logger, err := NewLogger(LoggerConfig{
		Level:  "info",
		Output: &output,
	})
	r.NoError(err)

	logger.Info("credential check", slog.String("token", "raw-secret"), slog.String("part", "R1"))

	logLine := output.String()
	a.Contains(logLine, `token=[REDACTED]`)
	a.Contains(logLine, `part=R1`)
	a.NotContains(logLine, "raw-secret")
}
