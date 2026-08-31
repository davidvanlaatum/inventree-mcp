package tools

import (
	"encoding/json"
	"log/slog"
)

// invocationFieldExtractor builds bounded, typed structured-log fields from
// one tool's raw call arguments. Extractors must never surface raw search
// text, presence-only flags, or unbounded data.
type invocationFieldExtractor func(json.RawMessage) []slog.Attr

// idInputToolNames lists every tool whose input is the shared IDInput shape
// (a single positive stable primary key), safe to log verbatim as "id".
var idInputToolNames = []string{
	GetAddressToolName, GetCompanyToolName, GetSupplierPartToolName, GetManufacturerPartToolName,
	GetContactToolName, GetPartToolName, GetPartCategoryToolName, GetStockLocationToolName,
	GetStockLocationTypeToolName, GetStockItemToolName, GetAttachmentMetadataToolName, GetOwnerToolName,
	GetProjectCodeToolName, GetPartRelationToolName, GetPurchaseOrderExtraLineToolName, GetPurchaseOrderToolName,
	GetPurchaseOrderLineToolName, GetStockTrackingEntryToolName, GetPartStocktakeToolName,
}

// searchInputToolNames lists every tool whose input is the shared
// SearchInput shape. Only the bounded limit/offset pagination parameters are
// logged; the raw search text is never included.
var searchInputToolNames = []string{
	SearchPartsToolName, SearchPartCategoriesToolName, SearchParameterTemplatesToolName,
	SearchCompaniesToolName, SearchSuppliersToolName, SearchManufacturersToolName,
	SearchStockLocationsToolName, SearchStockLocationTypesToolName,
}

// objectLookupInputToolNames lists every tool whose input is the shared
// ObjectLookupInput shape (a closed-vocabulary model_type plus a bounded
// model_id and pagination). The model_type value is domain vocabulary
// documented on the tool schema, not free text.
var objectLookupInputToolNames = []string{
	ListAttachmentsToolName,
}

// bulkItemsInputToolNames lists every bulk tool whose input carries an
// "items" batch plus dry_run/confirm flags. Only the item count and the two
// booleans are logged, never the batch contents.
var bulkItemsInputToolNames = []string{
	BulkUpdateAttachmentsToolName, BulkUpdatePartsToolName, BulkUpdateCompaniesToolName,
	BulkUpdatePartCategoriesToolName, BulkUpdateSupplierPartsToolName, BulkUpdateManufacturerPartsToolName,
	BulkUpdateObjectParametersToolName, BulkUpdateStockItemMetadataToolName, BulkSetStockStatusToolName,
	BulkUpdatePurchaseOrdersToolName, BulkUpdatePurchaseOrderLinesToolName, BulkUpdatePurchaseOrderExtraLinesToolName,
}

const maxLoggedShortStringLength = 32

// toolInvocationFieldExtractors is the centralized deny-by-default field map:
// only tools listed here get any argument-derived structured log fields.
// Every other registered tool logs just the base invocation fields (request
// ID, tool name, transport, source IP, trace correlation). This currently
// covers the ID-lookup, search, object-lookup, and items-batch bulk shapes
// that make up most of the tool surface; additional tool-specific extractors
// can be added here without touching InvocationLoggingMiddleware.
var toolInvocationFieldExtractors = buildToolInvocationFieldExtractors()

func buildToolInvocationFieldExtractors() map[string]invocationFieldExtractor {
	extractors := make(map[string]invocationFieldExtractor,
		len(idInputToolNames)+len(searchInputToolNames)+len(objectLookupInputToolNames)+len(bulkItemsInputToolNames)+1)
	for _, name := range idInputToolNames {
		extractors[name] = idInputFields
	}
	for _, name := range searchInputToolNames {
		extractors[name] = searchInputFields
	}
	for _, name := range objectLookupInputToolNames {
		extractors[name] = objectLookupInputFields
	}
	for _, name := range bulkItemsInputToolNames {
		extractors[name] = bulkItemsInputFields
	}
	extractors[BulkPropagatePartParametersToolName] = bulkPropagatePartParametersFields
	extractors[RenderComponentImageToolName] = renderComponentImageFields
	return extractors
}

// safeInvocationFields returns the bounded structured log fields for one
// tool's raw arguments. Tools without an explicit entry in
// toolInvocationFieldExtractors return no extra fields.
func safeInvocationFields(toolName string, arguments json.RawMessage) []slog.Attr {
	extractor, ok := toolInvocationFieldExtractors[toolName]
	if !ok || len(arguments) == 0 {
		return nil
	}
	return extractor(arguments)
}

func idInputFields(arguments json.RawMessage) []slog.Attr {
	var input struct {
		ID int `json:"id"`
	}
	if json.Unmarshal(arguments, &input) != nil || input.ID <= 0 {
		return nil
	}
	return []slog.Attr{slog.Int("id", input.ID)}
}

func searchInputFields(arguments json.RawMessage) []slog.Attr {
	var input struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	if json.Unmarshal(arguments, &input) != nil {
		return nil
	}
	attrs := make([]slog.Attr, 0, 2)
	if input.Limit > 0 {
		attrs = append(attrs, slog.Int("limit", NormalizeLookupLimit(input.Limit)))
	}
	if input.Offset > 0 {
		attrs = append(attrs, slog.Int("offset", input.Offset))
	}
	return attrs
}

func objectLookupInputFields(arguments json.RawMessage) []slog.Attr {
	var input struct {
		ModelType string `json:"model_type"`
		ModelID   int    `json:"model_id"`
		Limit     int    `json:"limit"`
		Offset    int    `json:"offset"`
	}
	if json.Unmarshal(arguments, &input) != nil {
		return nil
	}
	attrs := make([]slog.Attr, 0, 4)
	if modelType := boundedShortString(input.ModelType); modelType != "" {
		attrs = append(attrs, slog.String("model_type", modelType))
	}
	if input.ModelID > 0 {
		attrs = append(attrs, slog.Int("model_id", input.ModelID))
	}
	if input.Limit > 0 {
		attrs = append(attrs, slog.Int("limit", NormalizeLookupLimit(input.Limit)))
	}
	if input.Offset > 0 {
		attrs = append(attrs, slog.Int("offset", input.Offset))
	}
	return attrs
}

func boundedShortString(modelType string) string {
	if len(modelType) > maxLoggedShortStringLength {
		return modelType[:maxLoggedShortStringLength]
	}
	return modelType
}

func bulkItemsInputFields(arguments json.RawMessage) []slog.Attr {
	var input struct {
		Items   []json.RawMessage `json:"items"`
		DryRun  bool              `json:"dry_run"`
		Confirm bool              `json:"confirm"`
	}
	if json.Unmarshal(arguments, &input) != nil {
		return nil
	}
	return []slog.Attr{
		slog.Int("item_count", len(input.Items)),
		slog.Bool("dry_run", input.DryRun),
		slog.Bool("confirm", input.Confirm),
	}
}

func bulkPropagatePartParametersFields(arguments json.RawMessage) []slog.Attr {
	var input struct {
		PartIDs []int `json:"part_ids"`
		DryRun  bool  `json:"dry_run"`
		Confirm bool  `json:"confirm"`
	}
	if json.Unmarshal(arguments, &input) != nil {
		return nil
	}
	return []slog.Attr{
		slog.Int("item_count", len(input.PartIDs)),
		slog.Bool("dry_run", input.DryRun),
		slog.Bool("confirm", input.Confirm),
	}
}

// renderComponentImageFields logs only the closed-vocabulary component
// family, never the rendering parameter values.
func renderComponentImageFields(arguments json.RawMessage) []slog.Attr {
	var input struct {
		Family string `json:"family"`
	}
	if json.Unmarshal(arguments, &input) != nil || input.Family == "" {
		return nil
	}
	return []slog.Attr{slog.String("family", boundedShortString(input.Family))}
}
