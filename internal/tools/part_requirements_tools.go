package tools

import (
	"context"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PartRequirementsLookupClient is the narrow client surface
// get_part_requirements needs.
type PartRequirementsLookupClient interface {
	GetPartRequirements(context.Context, int) (inventree.PartRequirements, error)
}

type GetPartRequirementsInput struct {
	PartID int `json:"part_id" jsonschema:"Stable part primary key."`
}

func registerPartRequirementsLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, GetPartRequirementsToolName, "Get part requirements",
		"Retrieves one part's calculated build/sales-demand snapshot (total_stock, unallocated_stock, can_build, ordering, building, scheduled_to_build, required_for_build_orders, allocated_to_build_orders, required_for_sales_orders, allocated_to_sales_orders). This is a non-atomic, point-in-time calculation, not a transactional planning quantity, and several fields overlap by name with get_part's own aggregate fields; the two are computed independently and may momentarily disagree.",
		getPartRequirements(deps))
}

func getPartRequirements(deps Dependencies) mcp.ToolHandlerFor[GetPartRequirementsInput, RecordOutput[inventree.PartRequirements]] {
	return LookupHandler[PartRequirementsLookupClient, GetPartRequirementsInput, RecordOutput[inventree.PartRequirements]](deps, GetPartRequirementsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartRequirementsLookupClient, input GetPartRequirementsInput) (*mcp.CallToolResult, RecordOutput[inventree.PartRequirements], error) {
			if input.PartID <= 0 {
				return recordOutput(inventree.PartRequirements{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound})
			}
			record, err := client.GetPartRequirements(ctx, input.PartID)
			return recordOutput(record, err)
		})
}
