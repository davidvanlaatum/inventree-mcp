package inventree

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeAuditDeltasStripsNestedDetailAndCreatedBy(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	raw := json.RawMessage(`{
		"location": 2,
		"location_detail": {"pk": 2, "name": "Second Location"},
		"purchaseorder": 1,
		"purchaseorder_detail": {
			"pk": 1,
			"created_by": {"pk": 2, "username": "someone", "email": "someone@example.test"}
		},
		"quantity": 20
	}`)

	sanitized, err := SanitizeAuditDeltas(raw)
	r.NoError(err)
	a.Equal(map[string]any{"location": 2.0, "purchaseorder": 1.0, "quantity": 20.0}, sanitized)
}

func TestSanitizeAuditDeltasStripsUnnamedNestedIdentityRecords(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	// A future/undiscovered event type could embed a nested user record
	// under a key that is neither "_detail"-suffixed nor "created_by". The
	// content-based check must still catch this by the presence of an
	// identity-shaped field (email/username) inside the nested object.
	raw := json.RawMessage(`{
		"assigned_to": {"pk": 3, "username": "reviewer", "email": "reviewer@example.test"},
		"quantity": 5
	}`)

	sanitized, err := SanitizeAuditDeltas(raw)
	r.NoError(err)
	a.Equal(map[string]any{"quantity": 5.0}, sanitized)
}

func TestSanitizeAuditDeltasKeepsNestedObjectsWithoutIdentityFields(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	raw := json.RawMessage(`{"batch_info": {"batch": "B-1", "count": 3}}`)

	sanitized, err := SanitizeAuditDeltas(raw)
	r.NoError(err)
	a.Equal(map[string]any{"batch_info": map[string]any{"batch": "B-1", "count": 3.0}}, sanitized)
}

func TestSanitizeAuditDeltasStripsNestedCreatedByAtAnyDepth(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	raw := json.RawMessage(`{"outer": {"created_by": {"email": "leak@example.test"}, "kept": 1}}`)

	sanitized, err := SanitizeAuditDeltas(raw)
	r.NoError(err)
	a.Equal(map[string]any{"outer": map[string]any{"kept": 1.0}}, sanitized)
}

func TestSanitizeAuditDeltasHandlesFlatScalarShapes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want map[string]any
	}{
		{"created", `{"quantity": 10, "status": 10}`, map[string]any{"quantity": 10.0, "status": 10.0}},
		{"added", `{"added": 2, "quantity": 12}`, map[string]any{"added": 2.0, "quantity": 12.0}},
		{"removed", `{"removed": 1, "quantity": 11}`, map[string]any{"removed": 1.0, "quantity": 11.0}},
		{"counted", `{"quantity": 20}`, map[string]any{"quantity": 20.0}},
		{"status_change", `{"old_status": 10, "old_status_logical": 10, "status": 50, "status_logical": 50}`, map[string]any{"old_status": 10.0, "old_status_logical": 10.0, "status": 50.0, "status_logical": 50.0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sanitized, err := SanitizeAuditDeltas(json.RawMessage(tc.raw))
			require.NoError(t, err)
			assert.Equal(t, tc.want, sanitized)
		})
	}
}

func TestSanitizeAuditDeltasEmptyForNilAndNull(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	sanitized, err := SanitizeAuditDeltas(nil)
	a.NoError(err)
	a.Equal(map[string]any{}, sanitized)

	sanitized, err = SanitizeAuditDeltas(json.RawMessage(`null`))
	a.NoError(err)
	a.Equal(map[string]any{}, sanitized)
}

func TestSanitizeAuditDeltasRejectsNonObjectPayload(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	for _, raw := range []string{`[1,2,3]`, `"unexpected"`, `42`} {
		_, err := SanitizeAuditDeltas(json.RawMessage(raw))
		a.Error(err, raw)
	}
}

func TestSanitizeAuditDeltasFailsClosedOnExcessiveKeyCount(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	var builder strings.Builder
	builder.WriteByte('{')
	for i := range stockAuditDeltasMaxKeys + 1 {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(`"k`)
		builder.WriteString(strconv.Itoa(i))
		builder.WriteString(`":1`)
	}
	builder.WriteByte('}')

	_, err := SanitizeAuditDeltas(json.RawMessage(builder.String()))
	a.Error(err)
	a.Contains(err.Error(), "key safety bound")
}

func TestSanitizeAuditDeltasFailsClosedOnExcessiveDepth(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	raw := json.RawMessage(`{"a":{"b":{"c":{"d":{"e":1}}}}}`)
	_, err := SanitizeAuditDeltas(raw)
	a.Error(err)
	a.Contains(err.Error(), "nesting safety bound")
}

func TestSanitizeAuditDeltasFailsClosedOnExcessiveByteSize(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	raw := json.RawMessage(`{"batch": "` + strings.Repeat("x", stockAuditDeltasMaxBytes) + `"}`)
	_, err := SanitizeAuditDeltas(raw)
	a.Error(err)
	a.Contains(err.Error(), "byte safety bound")
}

func TestStockAuditUnmarshalJSONSanitizesDeltas(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	raw := []byte(`{
		"pk": 5,
		"item": 1,
		"part": 1,
		"date": "2026-08-18 01:26",
		"deltas": {"location": 2, "location_detail": {"pk": 2, "name": "Second Location"}, "quantity": 20},
		"event_label": "Location changed",
		"notes": "moved",
		"event_type": 20,
		"user": 2
	}`)

	var entry StockAudit
	r.NoError(json.Unmarshal(raw, &entry))
	a.Equal(5, entry.PK)
	a.Equal("Location changed", entry.Label)
	a.Equal("moved", entry.Notes)
	a.Equal(map[string]any{"location": 2.0, "quantity": 20.0}, entry.Deltas)
}

// TestPartStocktakeUnmarshalJSONDecodesPinnedSchemaShape pins the
// PartStocktake JSON decode contract against the pinned InvenTree 1.5.0
// schema (docs/api-schema.yaml's PartStocktake). A populated record's
// successful read path could not be exercised live in the Testcontainers
// suite because pinned InvenTree 1.5.0 has no working path to create a
// PartStocktake record (see the "stock_audit_and_stocktake_history"
// integration subtest for the pinned upstream-bug write-up), so this unit
// test is the primary coverage for the populated-record decode shape.
func TestPartStocktakeUnmarshalJSONDecodesPinnedSchemaShape(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	raw := []byte(`{
		"pk": 3,
		"part": 7,
		"part_name": "Widget",
		"part_ipn": null,
		"part_description": "A widget",
		"date": "2026-08-18",
		"item_count": 4,
		"quantity": 40.0,
		"cost_min": "10.000000",
		"cost_min_currency": "AUD",
		"cost_max": "12.500000",
		"cost_max_currency": "AUD"
	}`)

	var stocktake PartStocktake
	r.NoError(json.Unmarshal(raw, &stocktake))
	a.Equal(3, stocktake.PK)
	a.Equal(7, stocktake.Part)
	a.Equal("Widget", stocktake.PartName)
	a.Nil(stocktake.PartIPN)
	r.NotNil(stocktake.PartDescription)
	a.Equal("A widget", *stocktake.PartDescription)
	a.Equal(4, stocktake.ItemCount)
	a.InDelta(40.0, stocktake.Quantity, 0)
	r.NotNil(stocktake.CostMin)
	a.Equal(DecimalString("10.000000"), *stocktake.CostMin)
	a.Equal("AUD", stocktake.CostMinCurrency)
	r.NotNil(stocktake.CostMax)
	a.Equal(DecimalString("12.500000"), *stocktake.CostMax)
	a.Equal("AUD", stocktake.CostMaxCurrency)
}

func TestAuditDeltasDropsNestedReferencesAndArrayIdentities(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	raw := json.RawMessage(`{"direct":{"model_type":"company.company","pk":7,"str":"private"},"refs":[{"model_type":"company.company","pk":123,"str":"private"},{"email":"private@example.test","username":"private"}],"deeper":{"ref":{"model_type":"part.part","pk":999,"str":"private"},"rows":[{"email":"private@example.test"}]}}`)
	safe, err := SanitizeAuditDeltas(raw)
	r.NoError(err)
	a.Equal(float64(7), safe["direct"])
	encoded, err := json.Marshal(safe)
	r.NoError(err)
	for _, value := range []string{"123", "999", "private", "email", "username", "model_type"} {
		a.NotContains(string(encoded), value)
	}
}

func TestAuditDeltasDropsMalformedReferenceIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	safe, err := SanitizeAuditDeltas(json.RawMessage(`{"bad_type":{"model_type":3,"pk":123},"bad_pk":{"model_type":"part.part","pk":{"secret":"private"}}}`))
	r.NoError(err)
	a.Equal(map[string]any{"bad_type": nil, "bad_pk": nil}, safe)
}
