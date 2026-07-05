package tools

import (
	"context"
	"log/slog"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const HealthVersionToolName = "health_version"

type HealthVersionOutput struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

func Register(server *mcp.Server, deps Dependencies) {
	registerHealthVersion(server, deps)
	registerLookupTools(server, deps)
}

func registerHealthVersion(server *mcp.Server, _ Dependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        HealthVersionToolName,
		Title:       "Health and version",
		Description: "Returns server health and build version metadata.",
		Annotations: ToolAnnotations(ReadOnlyAnnotations),
	}, healthVersion)
}

func healthVersion(ctx context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, HealthVersionOutput, error) {
	logger := logging.FromContext(ctx).With(slog.String("tool", HealthVersionToolName))
	ctx = logging.WithLogger(ctx, logger)
	logger.DebugContext(ctx, "tool called")

	out := HealthVersionOutput{
		Status:  "ok",
		Version: buildinfo.Version,
		Commit:  buildinfo.Commit,
		Date:    buildinfo.Date,
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
	}, out, nil
}
