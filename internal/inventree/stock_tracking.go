package inventree

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	stockTrackingDeltasMaxDepth = 4
	stockTrackingDeltasMaxKeys  = 64
	stockTrackingDeltasMaxBytes = 8192
)

// stockTrackingDeltasDroppedKeys are non-"_detail"-suffixed keys InvenTree
// still uses to embed a full nested record inside a StockItemTracking
// `deltas` payload. Pinned 2026-08-18 spike evidence: a purchase-order
// receipt event's `purchaseorder_detail.created_by` exposes the receiving
// user's email and username; see docs/api-schema.md's "Verified Stock
// Tracking And Stocktake Endpoints" section for the full write-up.
var stockTrackingDeltasDroppedKeys = map[string]bool{
	"created_by": true,
}

// stockTrackingDeltasIdentityFields are keys whose presence inside a nested
// object marks it as an embedded user/account record, independent of the
// enclosing key's own name. The key-name denylist above only catches the
// two conventions InvenTree is confirmed to use today (a "_detail" suffix,
// or the literal "created_by"); this content-based check is a second,
// defense-in-depth layer against a future/undiscovered event type embedding
// a similarly-shaped nested record under some other key name.
var stockTrackingDeltasIdentityFields = map[string]bool{
	"email":    true,
	"username": true,
}

// StockTracking is the client projection of one StockItemTracking audit
// event, backing both the list and exact-detail endpoints (InvenTree uses
// the same serializer for both). Deltas is always the redacted, bounded
// representation produced by SanitizeStockTrackingDeltas; nested item, part,
// and user detail stay separate stable-ID lookups because item_detail and
// user_detail are never requested.
type StockTracking struct {
	PK           int            `json:"pk"`
	Item         *int           `json:"item"`
	Part         *int           `json:"part"`
	Date         string         `json:"date"`
	Deltas       map[string]any `json:"deltas"`
	Label        string         `json:"label"`
	Notes        string         `json:"notes"`
	TrackingType int            `json:"tracking_type"`
	User         *int           `json:"user"`
}

func (s *StockTracking) UnmarshalJSON(data []byte) error {
	type alias StockTracking
	wire := struct {
		Deltas json.RawMessage `json:"deltas"`
		*alias
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	sanitized, err := SanitizeStockTrackingDeltas(wire.Deltas)
	if err != nil {
		return err
	}
	s.Deltas = sanitized
	return nil
}

// SanitizeStockTrackingDeltas decodes a raw StockItemTracking `deltas`
// payload, strips nested `*_detail`/`created_by` full-record keys InvenTree
// embeds unconditionally for some event types, and enforces bounded
// depth/key-count/byte-size limits on what remains. It returns an error
// rather than silently truncating when the payload is not a JSON object, or
// the redacted result still exceeds a bound, or contains an unsupported JSON
// value type. See docs/api-schema.md's "Verified Stock Tracking And
// Stocktake Endpoints" section for the pinned spike evidence behind this
// design.
func SanitizeStockTrackingDeltas(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("stock tracking deltas is not a JSON object: %w", err)
	}
	redacted := redactStockTrackingDeltas(decoded)
	keyCount := 0
	if err := boundStockTrackingDeltas(redacted, 1, &keyCount); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return nil, fmt.Errorf("encode redacted stock tracking deltas: %w", err)
	}
	if len(encoded) > stockTrackingDeltasMaxBytes {
		return nil, fmt.Errorf("stock tracking deltas exceeds the %d-byte safety bound after redaction", stockTrackingDeltasMaxBytes)
	}
	return redacted, nil
}

func redactStockTrackingDeltas(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, v := range value {
		if strings.HasSuffix(key, "_detail") || stockTrackingDeltasDroppedKeys[key] {
			continue
		}
		if nested, ok := v.(map[string]any); ok && looksLikeIdentityRecord(nested) {
			continue
		}
		result[key] = redactStockTrackingDeltasValue(v)
	}
	return result
}

// looksLikeIdentityRecord reports whether obj's own keys include a field
// this codebase treats as identifying a person (an email address or a
// username), which is the shape InvenTree's confirmed nested `created_by`
// leak takes. It only inspects obj's direct keys, not nested objects within
// it, so the recursive redaction walk still reaches deeper structure.
func looksLikeIdentityRecord(obj map[string]any) bool {
	for key := range obj {
		if stockTrackingDeltasIdentityFields[strings.ToLower(key)] {
			return true
		}
	}
	return false
}

func redactStockTrackingDeltasValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return redactStockTrackingDeltas(v)
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = redactStockTrackingDeltasValue(item)
		}
		return result
	default:
		return v
	}
}

func boundStockTrackingDeltas(value any, depth int, keyCount *int) error {
	if depth > stockTrackingDeltasMaxDepth {
		return fmt.Errorf("stock tracking deltas exceeds the %d-level nesting safety bound", stockTrackingDeltasMaxDepth)
	}
	switch v := value.(type) {
	case map[string]any:
		for _, nested := range v {
			*keyCount++
			if *keyCount > stockTrackingDeltasMaxKeys {
				return fmt.Errorf("stock tracking deltas exceeds the %d-key safety bound", stockTrackingDeltasMaxKeys)
			}
			if err := boundStockTrackingDeltas(nested, depth+1, keyCount); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range v {
			if err := boundStockTrackingDeltas(item, depth+1, keyCount); err != nil {
				return err
			}
		}
	case string, float64, bool, nil:
		// scalar; nothing further to bound.
	default:
		return fmt.Errorf("stock tracking deltas contains an unsupported JSON value type %T", v)
	}
	return nil
}

// PartStocktake is the client projection of one historical PartStocktake
// snapshot. Nested part_detail remains a separate get_part lookup.
type PartStocktake struct {
	PK              int            `json:"pk"`
	Part            int            `json:"part"`
	PartName        string         `json:"part_name"`
	PartIPN         *string        `json:"part_ipn"`
	PartDescription *string        `json:"part_description"`
	Date            string         `json:"date"`
	ItemCount       int            `json:"item_count"`
	Quantity        float64        `json:"quantity"`
	CostMin         *DecimalString `json:"cost_min"`
	CostMinCurrency string         `json:"cost_min_currency"`
	CostMax         *DecimalString `json:"cost_max"`
	CostMaxCurrency string         `json:"cost_max_currency"`
}

type StockTrackingQuery struct {
	ItemID int
	PartID int
	Limit  int
	Offset int
}

func (q StockTrackingQuery) values() url.Values {
	values := url.Values{}
	if q.ItemID != 0 {
		values.Set("item", strconv.Itoa(q.ItemID))
	}
	if q.PartID != 0 {
		values.Set("part", strconv.Itoa(q.PartID))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

type PartStocktakeQuery struct {
	PartID int
	Limit  int
	Offset int
}

func (q PartStocktakeQuery) values() url.Values {
	values := url.Values{}
	if q.PartID != 0 {
		values.Set("part", strconv.Itoa(q.PartID))
	}
	setPagination(values, q.Limit, q.Offset)
	return values
}

// DataOutput is the bounded asynchronous task descriptor returned by
// InvenTree's background-worker-backed operations. Output is an upstream URL
// when the task produces a report; callers must apply their own same-instance
// URL and content policy before fetching it.
type DataOutput struct {
	PK           int     `json:"pk"`
	Created      string  `json:"created"`
	User         *int    `json:"user"`
	Total        int     `json:"total"`
	Progress     int     `json:"progress"`
	Complete     bool    `json:"complete"`
	OutputType   *string `json:"output_type"`
	TemplateName *string `json:"template_name"`
	Plugin       *string `json:"plugin"`
	Output       *string `json:"output"`
	Errors       any     `json:"errors"`
}

// PartStocktakeGenerate is the request/response contract for the asynchronous
// stocktake generation endpoint. The selectors and flags are validated by the
// guarded tool; the client preserves the upstream nullable/write-only shape.
type PartStocktakeGenerate struct {
	Part           *int        `json:"part,omitempty"`
	Category       *int        `json:"category,omitempty"`
	Location       *int        `json:"location,omitempty"`
	GenerateEntry  bool        `json:"generate_entry,omitempty"`
	GenerateReport bool        `json:"generate_report,omitempty"`
	Output         *DataOutput `json:"output,omitempty"`
}
