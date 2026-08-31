package tools

import (
	"context"
	"time"

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
	registerComponentRenderTool(server, deps)
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
	names := make([]string, 0, 3+len(lookupToolNames)+len(writeToolNames))
	names = append(names, HealthVersionToolName, GetLocalUploadPolicyToolName, RenderComponentImageToolName)
	names = append(names, lookupToolNames...)
	if enableWriteTools {
		names = append(names, writeToolNames...)
	}
	return names
}

func registerHealthVersion(server *mcp.Server, _ Dependencies) {
	mcp.AddTool(server, ToolDescriptor(HealthVersionToolName, "Health and version", "Returns server health and build version metadata."), healthVersion)
}

func healthVersion(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, HealthVersionOutput, error) {
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
