package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	StatusNoMatch                = "no_match"
	StatusMatchedUnsupportedType = "matched_unsupported_type"
	StatusBarcodeConflict        = "conflict"

	// barcodeDuplicateConflictMessage is the exact FieldErrors["error"]
	// message InvenTree returns when a barcode is already linked to a
	// different object, confirmed live against the pinned instance. It is
	// deliberately matched by exact substring, not merely "FieldErrors[error]
	// is present", because a different rejection (an unsupported object
	// field, e.g. sending "company") also populates FieldErrors["error"]
	// with a different message ("Missing data: provide one of [...]").
	barcodeDuplicateConflictMessage = "Barcode matches existing item"

	barcodeScanHistoryUpstreamPageSize = 100
	// barcodeScanHistoryWalkByteBudget and barcodeScanHistoryWalkTimeBudget
	// are new-to-this-repo bounds: no existing tool in this codebase walks
	// multiple upstream pages applying client-side filtering, so there is no
	// established precedent to mirror. The byte budget is approximated by
	// summing the character length of each scanned entry's Data/Endpoint/
	// Timestamp fields (a simple proxy for response size, not an exact wire
	// count). Both bounds are conservative: hitting either stops the walk
	// and reports HasMore true rather than risk an unbounded scan.
	barcodeScanHistoryWalkByteBudget = 2 * 1024 * 1024
	barcodeScanHistoryWalkTimeBudget = 10 * time.Second
)

// defaultScanHistoryMaxPageDepth mirrors defaultBulkMaxItems/
// defaultBulkConcurrency in bulk_progress.go: real deployments always set
// Dependencies.ScanHistoryMaxPageDepth from validated Config (whose
// Validate() hard-rejects <= 0), so this fallback only matters for
// Dependencies literals built directly -- chiefly tests -- that predate the
// field.
const defaultScanHistoryMaxPageDepth = 50

// effectiveScanHistoryMaxPageDepth returns deps.ScanHistoryMaxPageDepth, or
// defaultScanHistoryMaxPageDepth when it has not been explicitly configured.
func effectiveScanHistoryMaxPageDepth(deps Dependencies) int {
	if deps.ScanHistoryMaxPageDepth > 0 {
		return deps.ScanHistoryMaxPageDepth
	}
	return defaultScanHistoryMaxPageDepth
}

// BarcodeStateClient is the narrow client surface needed to read one
// object's current has_barcode state, shared by assign_barcode's and
// unassign_barcode's read-after-write verification and unassign_barcode's
// preflight check.
type BarcodeStateClient interface {
	GetPartDetail(context.Context, int) (inventree.PartDetail, error)
	GetStockItemDetail(context.Context, int) (inventree.StockItemDetail, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
	GetPurchaseOrderDetail(context.Context, int) (inventree.PurchaseOrderDetail, error)
}

// barcodeObjectDescriptor centralizes everything the barcode tools need to
// know about one object type: InvenTree's bare-lowercase spelling (used
// identically by generate's "model" field and as the JSON key for link/
// unlink), and how to read its current has_barcode state.
type barcodeObjectDescriptor struct {
	bareModel  string
	hasBarcode func(ctx context.Context, client BarcodeStateClient, objectID int) (bool, error)
}

// barcodeObjectDescriptors reuses the exact object_type key strings
// (OwnerObjectPart etc., from owner_tools.go) as the barcode tools'
// object_type input vocabulary, for consistency across tool families.
var barcodeObjectDescriptors = map[string]barcodeObjectDescriptor{
	OwnerObjectPart: {
		bareModel: "part",
		hasBarcode: func(ctx context.Context, client BarcodeStateClient, objectID int) (bool, error) {
			record, err := client.GetPartDetail(ctx, objectID)
			if err != nil {
				return false, err
			}
			if record.PK != objectID {
				return false, fmt.Errorf("part %d returned a mismatched identity", objectID)
			}
			return record.HasBarcode, nil
		},
	},
	OwnerObjectStockItem: {
		bareModel: "stockitem",
		hasBarcode: func(ctx context.Context, client BarcodeStateClient, objectID int) (bool, error) {
			record, err := client.GetStockItemDetail(ctx, objectID)
			if err != nil {
				return false, err
			}
			if record.PK != objectID {
				return false, fmt.Errorf("stock item %d returned a mismatched identity", objectID)
			}
			return record.HasBarcode, nil
		},
	},
	OwnerObjectStockLocation: {
		bareModel: "stocklocation",
		hasBarcode: func(ctx context.Context, client BarcodeStateClient, objectID int) (bool, error) {
			record, err := client.GetStockLocation(ctx, objectID)
			if err != nil {
				return false, err
			}
			if record.PK != objectID {
				return false, fmt.Errorf("stock location %d returned a mismatched identity", objectID)
			}
			return record.HasBarcode, nil
		},
	},
	OwnerObjectPurchaseOrder: {
		bareModel: "purchaseorder",
		hasBarcode: func(ctx context.Context, client BarcodeStateClient, objectID int) (bool, error) {
			record, err := client.GetPurchaseOrderDetail(ctx, objectID)
			if err != nil {
				return false, err
			}
			if record.PK != objectID {
				return false, fmt.Errorf("purchase order %d returned a mismatched identity", objectID)
			}
			return record.HasBarcode, nil
		},
	},
}

// barcodeObjectTypeByBareModel is barcodeObjectDescriptors' bareModel
// mapping reversed, used by resolve_barcode to translate a matched
// response's bare-lowercase InvenTree object-type key back to this server's
// canonical object_type vocabulary.
var barcodeObjectTypeByBareModel = map[string]string{
	"part":          OwnerObjectPart,
	"stockitem":     OwnerObjectStockItem,
	"stocklocation": OwnerObjectStockLocation,
	"purchaseorder": OwnerObjectPurchaseOrder,
}

const barcodeObjectTypeHint = "object_type must be one of part, stock_item, stock_location, or purchase_order"

// --- generate_barcode ---

type GenerateBarcodeClient interface {
	GenerateBarcode(context.Context, string, int) (string, error)
}

type GenerateBarcodeInput struct {
	ObjectType string `json:"object_type" jsonschema:"Object type to generate barcode text for: part, stock_item, stock_location, or purchase_order."`
	ObjectID   int    `json:"object_id" jsonschema:"Stable primary key of the object."`
}

type GenerateBarcodeOutput struct {
	Status      string             `json:"status"`
	BarcodeText string             `json:"barcode_text,omitempty"`
	Validation  *ValidationFailure `json:"validation,omitempty"`
}

func generateBarcode(deps Dependencies) mcp.ToolHandlerFor[GenerateBarcodeInput, GenerateBarcodeOutput] {
	return LookupHandler[GenerateBarcodeClient, GenerateBarcodeInput, GenerateBarcodeOutput](deps, GenerateBarcodeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client GenerateBarcodeClient, input GenerateBarcodeInput) (*mcp.CallToolResult, GenerateBarcodeOutput, error) {
			descriptor, ok := barcodeObjectDescriptors[input.ObjectType]
			if !ok {
				return generateBarcodeValidation(barcodeObjectTypeHint)
			}
			if input.ObjectID <= 0 {
				return generateBarcodeValidation("object_id must be a positive stable primary key")
			}
			barcodeText, err := client.GenerateBarcode(ctx, descriptor.bareModel, input.ObjectID)
			if err != nil {
				if validation, ok := safeValidationFailure(err); ok {
					return TextResult(StatusValidationFailed), GenerateBarcodeOutput{Status: StatusValidationFailed, Validation: validation}, nil
				}
				return nil, GenerateBarcodeOutput{}, err
			}
			return TextResult(StatusOK), GenerateBarcodeOutput{Status: StatusOK, BarcodeText: barcodeText}, nil
		})
}

func generateBarcodeValidation(message string) (*mcp.CallToolResult, GenerateBarcodeOutput, error) {
	return TextResult(StatusValidationFailed), GenerateBarcodeOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "object", Messages: []string{message}}}}}, nil
}

// --- resolve_barcode ---

type ResolveBarcodeClient interface {
	ResolveBarcode(context.Context, string) (inventree.BarcodeMatch, bool, error)
}

type ResolveBarcodeInput struct {
	BarcodeText string `json:"barcode_text" jsonschema:"Raw scanned or typed barcode text to resolve."`
}

// ResolveBarcodeOutput never carries the raw barcode text/hash or any nested
// object record -- the caller already has the barcode text it searched for.
// Status is a closed vocabulary: StatusOK (matched, in-scope object type),
// StatusNoMatch (no InvenTree object links this barcode), or
// StatusMatchedUnsupportedType (the barcode matched an InvenTree object of a
// type this server has no tool support for, e.g. a Build).
type ResolveBarcodeOutput struct {
	Status     string             `json:"status"`
	Matched    bool               `json:"matched"`
	ObjectType string             `json:"object_type,omitempty"`
	ObjectID   int                `json:"object_id,omitempty"`
	Validation *ValidationFailure `json:"validation,omitempty"`
}

func resolveBarcode(deps Dependencies) mcp.ToolHandlerFor[ResolveBarcodeInput, ResolveBarcodeOutput] {
	return LookupHandler[ResolveBarcodeClient, ResolveBarcodeInput, ResolveBarcodeOutput](deps, ResolveBarcodeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ResolveBarcodeClient, input ResolveBarcodeInput) (*mcp.CallToolResult, ResolveBarcodeOutput, error) {
			if strings.TrimSpace(input.BarcodeText) == "" {
				return resolveBarcodeValidation("barcode_text is required")
			}
			match, matched, err := client.ResolveBarcode(ctx, input.BarcodeText)
			if err != nil {
				return nil, ResolveBarcodeOutput{}, err
			}
			if !matched {
				return TextResult(StatusNoMatch), ResolveBarcodeOutput{Status: StatusNoMatch}, nil
			}
			if match.ObjectType == "" {
				return TextResult(StatusMatchedUnsupportedType), ResolveBarcodeOutput{Status: StatusMatchedUnsupportedType, Matched: true}, nil
			}
			objectType, ok := barcodeObjectTypeByBareModel[match.ObjectType]
			if !ok {
				return nil, ResolveBarcodeOutput{}, fmt.Errorf("resolve_barcode matched unrecognized object type %q", match.ObjectType)
			}
			return TextResult(StatusOK), ResolveBarcodeOutput{Status: StatusOK, Matched: true, ObjectType: objectType, ObjectID: match.ObjectID}, nil
		})
}

func resolveBarcodeValidation(message string) (*mcp.CallToolResult, ResolveBarcodeOutput, error) {
	return TextResult(StatusValidationFailed), ResolveBarcodeOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "barcode_text", Messages: []string{message}}}}}, nil
}

// --- assign_barcode ---

type AssignBarcodeClient interface {
	BarcodeStateClient
	LinkBarcode(context.Context, string, string, int) error
}

type AssignBarcodeInput struct {
	ObjectType  string `json:"object_type" jsonschema:"Object type to assign the barcode to: part, stock_item, stock_location, or purchase_order."`
	ObjectID    int    `json:"object_id" jsonschema:"Stable primary key of the object."`
	BarcodeText string `json:"barcode_text" jsonschema:"Raw barcode text to link, e.g. from generate_barcode or a physical scan."`
}

// BarcodeConflict reports a duplicate-assignment rejection's conflicting
// object: the same ObjectType the caller sent (already known, since it is
// the request's own key) plus the conflicting object's PK/WebURL, redacted
// from InvenTree's response by redactBarcodeConflict. Never carries the
// upstream response's embedded "instance" record.
type BarcodeConflict struct {
	ObjectType string `json:"object_type"`
	ObjectID   int    `json:"object_id,omitempty"`
	WebURL     string `json:"web_url,omitempty"`
}

type AssignBarcodeOutput struct {
	Status       string             `json:"status"`
	ObjectType   string             `json:"object_type,omitempty"`
	ObjectID     int                `json:"object_id,omitempty"`
	Conflict     *BarcodeConflict   `json:"conflict,omitempty"`
	Validation   *ValidationFailure `json:"validation,omitempty"`
	RecoveryPlan string             `json:"recovery_plan,omitempty"`
}

func assignBarcode(deps Dependencies) mcp.ToolHandlerFor[AssignBarcodeInput, AssignBarcodeOutput] {
	return LookupHandler[AssignBarcodeClient, AssignBarcodeInput, AssignBarcodeOutput](deps, AssignBarcodeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AssignBarcodeClient, input AssignBarcodeInput) (*mcp.CallToolResult, AssignBarcodeOutput, error) {
			descriptor, ok := barcodeObjectDescriptors[input.ObjectType]
			if !ok {
				return assignBarcodeValidation(barcodeObjectTypeHint)
			}
			if input.ObjectID <= 0 {
				return assignBarcodeValidation("object_id must be a positive stable primary key")
			}
			if strings.TrimSpace(input.BarcodeText) == "" {
				return assignBarcodeValidation("barcode_text is required")
			}
			mutationErr := client.LinkBarcode(ctx, input.BarcodeText, descriptor.bareModel, input.ObjectID)
			if mutationErr != nil {
				if apiErr, ok := barcodeDuplicateConflictError(mutationErr); ok {
					conflict := redactBarcodeConflict(apiErr, input.ObjectType, descriptor.bareModel)
					return TextResult(StatusBarcodeConflict), AssignBarcodeOutput{Status: StatusBarcodeConflict, Conflict: conflict}, nil
				}
				if validation, ok := safeValidationFailure(mutationErr); ok {
					return TextResult(StatusValidationFailed), AssignBarcodeOutput{Status: StatusValidationFailed, Validation: validation}, nil
				}
				// definiteMutationRejection matches verifyOwnerAssignment's
				// pattern (owner_tools.go): a definite 4xx rejection (never
				// applied) fails fast, but an ambiguous error (5xx, timeout,
				// network) falls through to the read-after-write verification
				// below instead of trusting an error that might not reflect
				// what actually happened upstream.
				var apiErr *inventree.APIError
				if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
					return nil, AssignBarcodeOutput{}, errors.New("barcode assignment was rejected")
				}
			}
			hasBarcode, verifyErr := descriptor.hasBarcode(ctx, client, input.ObjectID)
			if verifyErr != nil || !hasBarcode {
				return TextResult(StatusPartialFailure), AssignBarcodeOutput{
					Status: StatusPartialFailure, ObjectType: input.ObjectType, ObjectID: input.ObjectID,
					RecoveryPlan: fmt.Sprintf("Read the %s again before retrying; the barcode assignment could not be verified.", input.ObjectType),
				}, nil
			}
			return TextResult(StatusOK), AssignBarcodeOutput{Status: StatusOK, ObjectType: input.ObjectType, ObjectID: input.ObjectID}, nil
		})
}

func assignBarcodeValidation(message string) (*mcp.CallToolResult, AssignBarcodeOutput, error) {
	return TextResult(StatusValidationFailed), AssignBarcodeOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "object", Messages: []string{message}}}}}, nil
}

// barcodeDuplicateConflictError reports whether err is specifically the
// duplicate-assignment rejection shape (HTTP 400, FieldErrors["error"]
// containing exactly barcodeDuplicateConflictMessage), returning the
// underlying *inventree.APIError for redactBarcodeConflict to parse further.
func barcodeDuplicateConflictError(err error) (*inventree.APIError, bool) {
	var apiErr *inventree.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return nil, false
	}
	for _, message := range apiErr.FieldErrors["error"] {
		if message == barcodeDuplicateConflictMessage {
			return apiErr, true
		}
	}
	return nil, false
}

// redactBarcodeConflict extracts only the conflicting object's PK and
// web_url from InvenTree's duplicate-assignment rejection. This is a
// purpose-built redaction, not internal/tools/validation_errors.go's
// existing allowlist-based safeValidationFailure, because the shape differs
// fundamentally: safeValidationFailure redacts a flat list of upstream
// message strings per field, but here FieldErrors[objectTypeKey][0] is
// itself a JSON-encoded string that must be unmarshaled a second time to
// reach the embedded object -- a shape safeValidationFailure's allowlist
// mechanism has no way to parse. If that second unmarshal fails for any
// reason, this falls back to a bare conflict carrying only the already-known
// ObjectType, never forwarding the raw upstream string.
func redactBarcodeConflict(apiErr *inventree.APIError, objectType, objectTypeKey string) *BarcodeConflict {
	raw := apiErr.FieldErrors[objectTypeKey]
	if len(raw) == 0 {
		return &BarcodeConflict{ObjectType: objectType}
	}
	var embedded struct {
		PK     flexibleConflictInt `json:"pk"`
		WebURL string              `json:"web_url"`
	}
	if err := json.Unmarshal([]byte(raw[0]), &embedded); err != nil {
		return &BarcodeConflict{ObjectType: objectType}
	}
	return &BarcodeConflict{ObjectType: objectType, ObjectID: int(embedded.PK), WebURL: embedded.WebURL}
}

// flexibleConflictInt decodes the embedded conflict object's "pk" field,
// which live evidence confirmed InvenTree encodes inconsistently: the plain
// duplicate-assignment rejection tested during design encodes it as a JSON
// number, but a live Testcontainers run against the pinned instance (F-S99)
// found DRF's nested-serializer error path instead stringifies every field
// in the embedded object, including "pk" (e.g. "pk":"12" rather than
// "pk":12). Both are accepted so redactBarcodeConflict does not spuriously
// fall back to the generic "no further detail" message on the string form.
type flexibleConflictInt int

func (v *flexibleConflictInt) UnmarshalJSON(data []byte) error {
	var asNumber int
	if err := json.Unmarshal(data, &asNumber); err == nil {
		*v = flexibleConflictInt(asNumber)
		return nil
	}
	var asString string
	if err := json.Unmarshal(data, &asString); err != nil {
		return err
	}
	parsed, err := strconv.Atoi(asString)
	if err != nil {
		return err
	}
	*v = flexibleConflictInt(parsed)
	return nil
}

// --- unassign_barcode ---

type UnassignBarcodeClient interface {
	BarcodeStateClient
	UnlinkBarcode(context.Context, string, int) error
}

type UnassignBarcodeInput struct {
	ObjectType string `json:"object_type" jsonschema:"Object type to unassign the barcode from: part, stock_item, stock_location, or purchase_order."`
	ObjectID   int    `json:"object_id" jsonschema:"Stable primary key of the object."`
	Confirm    bool   `json:"confirm,omitempty" jsonschema:"Required true before unassigning the barcode currently linked to this object."`
}

type UnassignBarcodeOutput struct {
	Status        string                 `json:"status"`
	ObjectType    string                 `json:"object_type,omitempty"`
	ObjectID      int                    `json:"object_id,omitempty"`
	Validation    *ValidationFailure     `json:"validation,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
	RecoveryPlan  string                 `json:"recovery_plan,omitempty"`
}

func unassignBarcode(deps Dependencies) mcp.ToolHandlerFor[UnassignBarcodeInput, UnassignBarcodeOutput] {
	return LookupHandler[UnassignBarcodeClient, UnassignBarcodeInput, UnassignBarcodeOutput](deps, UnassignBarcodeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client UnassignBarcodeClient, input UnassignBarcodeInput) (*mcp.CallToolResult, UnassignBarcodeOutput, error) {
			descriptor, ok := barcodeObjectDescriptors[input.ObjectType]
			if !ok {
				return unassignBarcodeValidation(barcodeObjectTypeHint)
			}
			if input.ObjectID <= 0 {
				return unassignBarcodeValidation("object_id must be a positive stable primary key")
			}
			hasBarcode, err := descriptor.hasBarcode(ctx, client, input.ObjectID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), UnassignBarcodeOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, UnassignBarcodeOutput{}, err
			}
			if !hasBarcode {
				return unassignBarcodeValidation("the object has no barcode assigned")
			}
			if !input.Confirm {
				clarification := NewClarification("Unassign the barcode linked to this exact object?", "confirm", "unassign_barcode requires confirm:true after reviewing the object", "confirm", false, nil, map[string]any{"object_type": input.ObjectType, "object_id": input.ObjectID, "confirm": true})
				return TextResult(StatusClarificationRequired), UnassignBarcodeOutput{Status: StatusClarificationRequired, ObjectType: input.ObjectType, ObjectID: input.ObjectID, Clarification: &clarification}, nil
			}
			mutationErr := client.UnlinkBarcode(ctx, descriptor.bareModel, input.ObjectID)
			if mutationErr != nil {
				if validation, ok := safeValidationFailure(mutationErr); ok {
					return TextResult(StatusValidationFailed), UnassignBarcodeOutput{Status: StatusValidationFailed, Validation: validation}, nil
				}
				// See assignBarcode's matching comment: a definite rejection
				// fails fast, an ambiguous error falls through to read-after-
				// write verification instead of trusting an error that might
				// not reflect what actually happened upstream.
				var apiErr *inventree.APIError
				if errors.As(mutationErr, &apiErr) && definiteMutationRejection(apiErr.StatusCode) {
					return nil, UnassignBarcodeOutput{}, errors.New("barcode removal was rejected")
				}
			}
			stillHasBarcode, verifyErr := descriptor.hasBarcode(ctx, client, input.ObjectID)
			if verifyErr != nil || stillHasBarcode {
				return TextResult(StatusPartialFailure), UnassignBarcodeOutput{
					Status: StatusPartialFailure, ObjectType: input.ObjectType, ObjectID: input.ObjectID,
					RecoveryPlan: fmt.Sprintf("Read the %s again before retrying; the barcode removal could not be verified.", input.ObjectType),
				}, nil
			}
			return TextResult(StatusOK), UnassignBarcodeOutput{Status: StatusOK, ObjectType: input.ObjectType, ObjectID: input.ObjectID}, nil
		})
}

func unassignBarcodeValidation(message string) (*mcp.CallToolResult, UnassignBarcodeOutput, error) {
	return TextResult(StatusValidationFailed), UnassignBarcodeOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "object", Messages: []string{message}}}}}, nil
}

// --- search_barcode_scan_history ---

type SearchBarcodeScanHistoryClient interface {
	SearchBarcodeScanHistoryPage(context.Context, inventree.BarcodeScanHistoryQuery) (inventree.BarcodeScanHistoryPage, error)
}

// SearchBarcodeScanHistoryInput's Endpoint/From/To are NOT real upstream
// InvenTree query filters (confirmed live: they are silently ignored
// server-side). Supplying any of them switches the handler from the fast
// single-upstream-page path to a bounded internal multi-page walk that
// applies these as client-side filtering (see walkBarcodeScanHistory).
type SearchBarcodeScanHistoryInput struct {
	Endpoint string `json:"endpoint,omitempty" jsonschema:"Optional client-side filter on the recorded endpoint name. Not a real upstream filter; supplying it triggers a bounded internal multi-page walk."`
	Result   *bool  `json:"result,omitempty" jsonschema:"Optional filter: true for successful scans only, false for failed scans only."`
	UserID   *int   `json:"user_id,omitempty" jsonschema:"Optional filter to scans recorded by one InvenTree user ID. An unknown user ID is rejected as a validation failure."`
	From     string `json:"from,omitempty" jsonschema:"Optional inclusive lower timestamp bound, RFC3339 form. Not a real upstream filter; supplying it (with or without to) triggers a bounded internal multi-page walk."`
	To       string `json:"to,omitempty" jsonschema:"Optional exclusive upper timestamp bound, RFC3339 form; a record timestamped exactly at to is not included. Not a real upstream filter; supplying it (with or without from) triggers a bounded internal multi-page walk."`
	Search   string `json:"search,omitempty" jsonschema:"Optional full-text search over the raw scanned data."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset   int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

// BarcodeScanHistoryRecord exposes Data (the raw scanned text) but excludes
// Context, Response, and UserDetail: an expanded/embedded user object is
// never returned, only the raw nullable UserID.
type BarcodeScanHistoryRecord struct {
	PK        int    `json:"pk"`
	Data      string `json:"data"`
	Timestamp string `json:"timestamp"`
	Endpoint  string `json:"endpoint,omitempty"`
	Result    bool   `json:"result"`
	UserID    *int   `json:"user_id,omitempty"`
}

type SearchBarcodeScanHistoryOutput struct {
	Status     string                     `json:"status"`
	Count      int                        `json:"count,omitempty"`
	HasMore    bool                       `json:"has_more,omitempty"`
	Records    []BarcodeScanHistoryRecord `json:"records,omitempty"`
	Validation *ValidationFailure         `json:"validation,omitempty"`
}

func searchBarcodeScanHistory(deps Dependencies) mcp.ToolHandlerFor[SearchBarcodeScanHistoryInput, SearchBarcodeScanHistoryOutput] {
	return LookupHandler[SearchBarcodeScanHistoryClient, SearchBarcodeScanHistoryInput, SearchBarcodeScanHistoryOutput](deps, SearchBarcodeScanHistoryToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client SearchBarcodeScanHistoryClient, input SearchBarcodeScanHistoryInput) (*mcp.CallToolResult, SearchBarcodeScanHistoryOutput, error) {
			if input.Offset < 0 {
				return barcodeHistoryValidation("offset must not be negative")
			}
			var from, to time.Time
			if input.From != "" {
				parsed, err := time.Parse(time.RFC3339, input.From)
				if err != nil {
					return barcodeHistoryValidation("from must be an RFC3339 timestamp")
				}
				from = parsed
			}
			if input.To != "" {
				parsed, err := time.Parse(time.RFC3339, input.To)
				if err != nil {
					return barcodeHistoryValidation("to must be an RFC3339 timestamp")
				}
				to = parsed
			}

			limit := NormalizeLookupLimit(input.Limit)
			base := inventree.BarcodeScanHistoryQuery{Result: input.Result, UserID: input.UserID, Search: input.Search, Ordering: "-timestamp"}
			needsWalk := input.Endpoint != "" || input.From != "" || input.To != ""

			var records []inventree.BarcodeScanHistoryEntry
			var hasMore bool
			if needsWalk {
				var err error
				records, hasMore, err = walkBarcodeScanHistory(ctx, client, base, input.Endpoint, from, to, limit, input.Offset, effectiveScanHistoryMaxPageDepth(deps))
				if err != nil {
					if validation, ok := barcodeHistoryUserValidation(err, input.UserID != nil); ok {
						return TextResult(StatusValidationFailed), SearchBarcodeScanHistoryOutput{Status: StatusValidationFailed, Validation: validation}, nil
					}
					return nil, SearchBarcodeScanHistoryOutput{}, err
				}
			} else {
				query := base
				query.Limit = limit
				query.Offset = input.Offset
				page, err := client.SearchBarcodeScanHistoryPage(ctx, query)
				if err != nil {
					if validation, ok := barcodeHistoryUserValidation(err, input.UserID != nil); ok {
						return TextResult(StatusValidationFailed), SearchBarcodeScanHistoryOutput{Status: StatusValidationFailed, Validation: validation}, nil
					}
					return nil, SearchBarcodeScanHistoryOutput{}, err
				}
				records, hasMore = page.Results, page.HasMore
			}

			if len(records) == 0 {
				// HasMore is still surfaced here: a bounded walk can be
				// truncated (maxPages/byte/time budget) with zero matches on
				// this page while a matching record may still exist further
				// ahead. Dropping HasMore would let a caller wrongly read
				// "no such records" as exhaustive instead of "this bounded
				// search didn't find one, try a later offset."
				return TextResult(StatusNotFound), SearchBarcodeScanHistoryOutput{Status: StatusNotFound, HasMore: hasMore}, nil
			}
			return TextResult(StatusOK), SearchBarcodeScanHistoryOutput{Status: StatusOK, Count: len(records), HasMore: hasMore, Records: barcodeScanHistoryRecords(records)}, nil
		})
}

func barcodeHistoryValidation(message string) (*mcp.CallToolResult, SearchBarcodeScanHistoryOutput, error) {
	return TextResult(StatusValidationFailed), SearchBarcodeScanHistoryOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "search_barcode_scan_history", Messages: []string{message}}}}}, nil
}

// barcodeHistoryUserValidation maps an upstream rejection of an unknown
// user_id filter into a clean field-scoped validation message. InvenTree
// returns a generic HTTP 400 (no field-scoped detail) for this case, so this
// is a purpose-built mapping rather than internal/tools/validation_errors.go's
// existing safeValidationFailure allowlist mechanism -- there is no upstream
// field-error content for that allowlist to redact. hasUserFilter narrows
// this mapping to only the calls this server itself made with a caller-
// supplied user_id; every other 400 falls through to safeValidationFailure.
func barcodeHistoryUserValidation(err error, hasUserFilter bool) (*ValidationFailure, bool) {
	if !hasUserFilter {
		return safeValidationFailure(err)
	}
	var apiErr *inventree.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return safeValidationFailure(err)
	}
	return &ValidationFailure{StatusCode: apiErr.StatusCode, Fields: []ValidationFieldError{{Field: "user_id", Messages: []string{"no such user id"}}}}, true
}

// walkBarcodeScanHistory fetches successive barcodeScanHistoryUpstreamPageSize
// upstream pages (ordered newest-first), applying endpointFilter/from/to as
// client-side filtering since none of them are real upstream query
// parameters, until it has assembled limit matching records past the first
// offset matches, the upstream page set is exhausted, or one of the bounded
// walk's own limits (maxPages, a byte budget, a time budget, or ctx
// cancellation) is reached. hasMore is only ever set true when a genuinely
// matching record is known to exist beyond what was returned, except at the
// byte/time budget cutoffs, where it is a conservative "may be more" signal.
func walkBarcodeScanHistory(ctx context.Context, client SearchBarcodeScanHistoryClient, base inventree.BarcodeScanHistoryQuery, endpointFilter string, from, to time.Time, limit, offset, maxPages int) ([]inventree.BarcodeScanHistoryEntry, bool, error) {
	start := time.Now()
	matched := make([]inventree.BarcodeScanHistoryEntry, 0, limit)
	skipped := 0
	approxBytes := 0
	hasMore := false
	pagesFetched := 0
	lastPageHasMore := false

pages:
	for page, upstreamOffset := 0, 0; page < maxPages; page, upstreamOffset = page+1, upstreamOffset+barcodeScanHistoryUpstreamPageSize {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if time.Since(start) > barcodeScanHistoryWalkTimeBudget {
			hasMore = true
			break pages
		}

		query := base
		query.Limit = barcodeScanHistoryUpstreamPageSize
		query.Offset = upstreamOffset
		upstreamPage, err := client.SearchBarcodeScanHistoryPage(ctx, query)
		if err != nil {
			return nil, false, err
		}
		pagesFetched++
		lastPageHasMore = upstreamPage.HasMore

		for _, entry := range upstreamPage.Results {
			approxBytes += len(entry.Data) + len(entry.Endpoint) + len(entry.Timestamp)
			if !barcodeScanHistoryEntryMatches(entry, endpointFilter, from, to) {
				continue
			}
			if skipped < offset {
				skipped++
				continue
			}
			if len(matched) >= limit {
				hasMore = true
				break pages
			}
			matched = append(matched, entry)
		}

		if approxBytes > barcodeScanHistoryWalkByteBudget {
			hasMore = upstreamPage.HasMore
			break pages
		}
		if !upstreamPage.HasMore {
			break pages
		}
	}

	// The loop can also exit by exhausting maxPages via the "for" condition
	// itself, with no explicit break above ever running -- e.g. the last
	// allowed page still had upstreamPage.HasMore true, but no limit/byte/
	// time cutoff fired on it. That is a truncation just like the byte/time
	// budget cutoffs above, and must be reported the same conservative way:
	// hasMore true whenever the last fetched upstream page still had more.
	if pagesFetched == maxPages && lastPageHasMore {
		hasMore = true
	}

	return matched, hasMore, nil
}

// barcodeScanHistoryTimestampLayouts are tried in order against a scan-
// history row's own Timestamp field. Live evidence against the pinned
// instance (F-S99) found InvenTree renders /api/barcode/history/'s
// "timestamp" as "2026-09-03 11:08" -- a space-separated, seconds-less,
// timezone-less layout, NOT the RFC3339 form this tool requires for the
// caller-supplied from/to input bounds. Both are tried (RFC3339 first, since
// other InvenTree endpoints/instances may render it that way) rather than
// assuming either is the only real-world shape, since this repo has no other
// precedent for parsing an InvenTree-rendered timestamp string client-side.
// The zoneless layouts ("2006-01-02T15:04:05", "2006-01-02 15:04:05",
// "2006-01-02 15:04") are parsed by Go as UTC. This assumes the pinned
// instance's Django TIME_ZONE/USE_TZ configuration renders this field in
// UTC, which the live evidence behind this fix did not specifically
// disprove but also did not directly confirm against a non-UTC-configured
// instance -- from/to filtering could misalign by the server's UTC offset on
// such an instance. Flagged as a residual risk rather than fixed blind,
// since there is no live evidence either way to code against.
var barcodeScanHistoryTimestampLayouts = []string{
	time.RFC3339,
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
}

func parseBarcodeScanHistoryTimestamp(value string) (time.Time, bool) {
	for _, layout := range barcodeScanHistoryTimestampLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func barcodeScanHistoryEntryMatches(entry inventree.BarcodeScanHistoryEntry, endpointFilter string, from, to time.Time) bool {
	if endpointFilter != "" && entry.Endpoint != endpointFilter {
		return false
	}
	if from.IsZero() && to.IsZero() {
		return true
	}
	timestamp, ok := parseBarcodeScanHistoryTimestamp(entry.Timestamp)
	if !ok {
		return false
	}
	// from is inclusive, to is exclusive, per the operator-approved contract
	// (docs/TASKS.md F-S99 Decisions: "from is inclusive and to is
	// exclusive"), also documented on SearchBarcodeScanHistoryInput.To's
	// jsonschema description above. A timestamp exactly equal to to must be
	// excluded, so the comparison is !timestamp.Before(to), not
	// timestamp.After(to) (which would wrongly keep the boundary row).
	if !from.IsZero() && timestamp.Before(from) {
		return false
	}
	if !to.IsZero() && !timestamp.Before(to) {
		return false
	}
	return true
}

func barcodeScanHistoryRecords(entries []inventree.BarcodeScanHistoryEntry) []BarcodeScanHistoryRecord {
	records := make([]BarcodeScanHistoryRecord, len(entries))
	for i, entry := range entries {
		records[i] = BarcodeScanHistoryRecord{PK: entry.PK, Data: entry.Data, Timestamp: entry.Timestamp, Endpoint: entry.Endpoint, Result: entry.Result, UserID: entry.UserID}
	}
	return records
}

// --- registration ---

func registerBarcodeLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchBarcodeScanHistoryToolName, "Search barcode scan history", "Lists /api/barcode/history/ scan rows, bounded and paginated, optionally filtered by result, user, search text, endpoint, or a timestamp range.", searchBarcodeScanHistory(deps))
}

// registerBarcodeWriteTools registers generate_barcode, resolve_barcode,
// assign_barcode, and unassign_barcode with {ScopeInventreeRead,
// ScopeInventreeWrite} (see the scope assignment in lookup_tools.go's init
// for the non-destructive rationale, matching F-S91/F-S98's preflighted-
// PATCH precedent rather than F-S48's destructive plan-token pattern).
func registerBarcodeWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, GenerateBarcodeToolName, "Generate barcode", "Requests barcode text for an existing object from the configured barcode-generation plugin. Generation is text-only and never assigns/links it. With InvenTree's default plugin, the returned text already resolves to the object via resolve_barcode without any link call; calling assign_barcode with that exact text on the same object is expected to be rejected as an existing match. Use assign_barcode to link a different (e.g. externally sourced or printed) barcode value instead.", generateBarcode(deps))
	addWriteTool(server, deps, ResolveBarcodeToolName, "Resolve barcode", "Scans/resolves raw barcode text to the InvenTree object it is linked to, or a clean no-match result.", resolveBarcode(deps))
	addWriteTool(server, deps, AssignBarcodeToolName, "Assign barcode", "Links raw barcode text to a part, stock item, stock location, or purchase order, and verifies the link by reading the object back. Rejected with a conflict if the text already resolves to a different object -- which includes an InvenTree default-plugin-generated value for the very object being assigned, since that text already resolves without a link.", assignBarcode(deps))
	addWriteTool(server, deps, UnassignBarcodeToolName, "Unassign barcode", "Unlinks the barcode currently linked to a part, stock item, stock location, or purchase order after confirm:true, and verifies removal by reading the object back.", unassignBarcode(deps))
}
