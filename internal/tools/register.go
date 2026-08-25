package tools

import (
	"context"
	"log/slog"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const HealthVersionToolName = "health_version"

const GetLocalUploadPolicyToolName = "get_local_upload_policy"

type HealthVersionOutput struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

func Register(server *mcp.Server, deps Dependencies) {
	if deps.stocktakePlanStore == nil {
		deps.stocktakePlanStore = newStocktakePlanStore(time.Now, randomStockPlanToken)
	}
	if deps.stocktakeTaskStore == nil {
		deps.stocktakeTaskStore = newStocktakeTaskStore(time.Now)
	}
	registerHealthVersion(server, deps)
	registerLocalUploadPolicyTool(server, deps)
	registerPrompts(server)
	registerLookupTools(server, deps)
	if deps.EnableWriteTools {
		registerWriteTools(server, deps)
	}
}

// RegisteredToolNames returns the tool names registered by Register for the
// supplied write-tool configuration. It is shared with bounded observability
// labels so caller-provided unknown tool names cannot create metric series.
func RegisteredToolNames(enableWriteTools bool) []string {
	names := make([]string, 0, 2+len(lookupToolNames)+len(writeToolNames))
	names = append(names, HealthVersionToolName, GetLocalUploadPolicyToolName)
	names = append(names, lookupToolNames...)
	if enableWriteTools {
		names = append(names, writeToolNames...)
	}
	return names
}

func registerHealthVersion(server *mcp.Server, _ Dependencies) {
	mcp.AddTool(server, ToolDescriptor(HealthVersionToolName, "Health and version", "Returns server health and build version metadata."), healthVersion)
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
