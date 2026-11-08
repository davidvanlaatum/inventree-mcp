package inventree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// AuditScope selects one ordinary business object. Child history is opt-in and
// remains restricted to the same six audited model types as direct reads.
type AuditScope struct {
	ModelType       string
	ModelID         int
	IncludeChildren bool
}

type AuditQuery struct {
	AuditScope
	Limit  int
	Offset int
}
type AuditEntryQuery struct {
	AuditScope
	ID int
}

func ValidAuditModelType(model string) bool {
	switch model {
	case "part.part", "company.company", "company.supplierpart", "company.manufacturerpart", "stock.stockitem", "order.purchaseorder":
		return true
	default:
		return false
	}
}

func (s AuditScope) valid() bool { return ValidAuditModelType(s.ModelType) && s.ModelID > 0 }
func (s AuditScope) Contains(entry AuditEntrySummary) bool {
	if entry.ModelType == s.ModelType && entry.ModelID == s.ModelID {
		return true
	}
	return s.IncludeChildren && entry.ParentModelType != nil && entry.ParentModelID != nil && *entry.ParentModelType == s.ModelType && *entry.ParentModelID == s.ModelID
}

func (q AuditQuery) values() url.Values {
	values := url.Values{"model_type": {q.ModelType}, "model_id": {strconv.Itoa(q.ModelID)}, "include_children": {strconv.FormatBool(q.IncludeChildren)}, "actor_detail": {"false"}, "changes_detail": {"false"}}
	setPagination(values, q.Limit, q.Offset)
	return values
}

// AuditEntrySummary is the complete list projection. Raw actor/network details,
// object representations, change display text, and additional_data are absent.
type AuditEntrySummary struct {
	PK              int            `json:"pk"`
	ModelType       string         `json:"model_type"`
	ModelID         int            `json:"model_id"`
	ParentModelType *string        `json:"parent_model_type,omitempty"`
	ParentModelID   *int           `json:"parent_model_id,omitempty"`
	Timestamp       string         `json:"timestamp"`
	Action          int            `json:"action"`
	Actor           *int           `json:"actor,omitempty"`
	EventType       *int           `json:"event_type"`
	EventLabel      string         `json:"event_label"`
	Deltas          map[string]any `json:"deltas"`
	Changes         map[string]any `json:"changes"`
}

type AuditEntry struct {
	AuditEntrySummary
	Notes string `json:"notes"`
}

func (w stockAuditWire) auditEntry() (AuditEntry, error) {
	if !ValidAuditModelType(w.ModelType) {
		return AuditEntry{}, errStockAuditScope
	}
	id, err := strconv.Atoi(w.ObjectPK)
	if err != nil || id <= 0 || w.PK <= 0 || w.Timestamp == "" || w.Action < 0 || w.Action > 3 {
		return AuditEntry{}, errors.New("InvenTree returned a malformed audit entry")
	}
	entry := AuditEntry{AuditEntrySummary: AuditEntrySummary{PK: w.PK, ModelType: w.ModelType, ModelID: id, Timestamp: w.Timestamp, Action: w.Action, Actor: w.Actor, EventType: w.EventType, EventLabel: w.EventLabel}, Notes: w.Notes}
	if w.ParentModelType != nil && ValidAuditModelType(*w.ParentModelType) && w.ParentObjectPK != nil {
		parent, parseErr := strconv.Atoi(*w.ParentObjectPK)
		if parseErr != nil || parent <= 0 {
			return AuditEntry{}, errors.New("InvenTree returned a malformed audit parent")
		}
		entry.ParentModelType = w.ParentModelType
		entry.ParentModelID = &parent
	}
	entry.Deltas, err = SanitizeAuditDeltas(w.Deltas)
	if err != nil {
		return AuditEntry{}, err
	}
	entry.Changes, err = sanitizeAuditChanges(w.ModelType, w.Changes)
	if err != nil {
		return AuditEntry{}, err
	}
	return entry, nil
}

// SearchAuditEntriesPage returns only allowlisted, scope-matching entries.
// Count and Next describe the raw upstream page, including excluded child
// models. Callers must follow Next even when Results is empty, then compute
// their filtered count after the bounded scan.
func (c *Client) SearchAuditEntriesPage(ctx context.Context, query AuditQuery) (Page[AuditEntry], error) {
	if !query.valid() || query.Offset < 0 || query.Limit < 0 || query.Limit > 1000 {
		return Page[AuditEntry]{}, errors.New("audit reads require a supported model_type, positive model_id and bounded pagination")
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	raw, err := listPage[stockAuditWire](ctx, c, "/api/audit/", query.values())
	if err != nil {
		return Page[AuditEntry]{}, err
	}
	out := Page[AuditEntry]{Count: raw.Count, Next: raw.Next, Previous: raw.Previous, Results: []AuditEntry{}}
	for _, wire := range raw.Results {
		if !ValidAuditModelType(wire.ModelType) {
			continue
		}
		entry, entryErr := wire.auditEntry()
		if entryErr != nil {
			return Page[AuditEntry]{}, entryErr
		}
		if !query.Contains(entry.AuditEntrySummary) {
			return Page[AuditEntry]{}, errors.New("InvenTree returned an out-of-scope audit entry")
		}
		out.Results = append(out.Results, entry)
	}
	return out, nil
}

func (c *Client) GetAuditEntry(ctx context.Context, query AuditEntryQuery) (AuditEntry, error) {
	if !query.valid() {
		return AuditEntry{}, errors.New("audit detail requires a supported model_type and positive model_id")
	}
	if query.ID <= 0 {
		return AuditEntry{}, &APIError{StatusCode: http.StatusNotFound, Kind: ErrorKindNotFound}
	}
	var wire stockAuditWire
	if err := c.getAuditWire(ctx, query.ID, &wire); err != nil {
		return AuditEntry{}, err
	}
	if wire.PK != query.ID {
		return AuditEntry{}, errors.New("InvenTree returned a mismatched audit identity")
	}
	if !ValidAuditModelType(wire.ModelType) {
		return AuditEntry{}, &APIError{StatusCode: http.StatusNotFound, Kind: ErrorKindNotFound}
	}
	entry, err := wire.auditEntry()
	if err != nil {
		return AuditEntry{}, err
	}
	if !query.Contains(entry.AuditEntrySummary) {
		return AuditEntry{}, &APIError{StatusCode: http.StatusNotFound, Kind: ErrorKindNotFound}
	}
	return entry, nil
}

func auditChangeFieldExposed(model, key string) bool {
	switch model {
	case "part.part":
		return PartFieldInventory[key] == PartFieldExposed
	case "company.company":
		return CompanyFieldInventory[key] == CompanyFieldExposed
	case "company.supplierpart":
		return SupplierPartFieldInventory[key] == SourcingPartFieldExposed
	case "company.manufacturerpart":
		return ManufacturerPartFieldInventory[key] == SourcingPartFieldExposed
	case "stock.stockitem":
		return StockItemFieldInventory[key] == StockItemFieldExposed
	case "order.purchaseorder":
		return PurchaseOrderFieldInventory[key] == PurchaseOrderFieldExposed
	default:
		return false
	}
}

func sanitizeAuditChanges(model string, raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("audit changes is not a JSON object")
	}
	allowed := map[string]json.RawMessage{}
	for key, value := range fields {
		if auditChangeFieldExposed(model, key) {
			allowed[key] = value
		}
	}
	encoded, err := json.Marshal(allowed)
	if err != nil {
		return nil, err
	}
	return sanitizeAuditObject(encoded, false)
}

func (c *Client) getAuditWire(ctx context.Context, id int, wire *stockAuditWire) error {
	req, err := c.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/audit/%d/", id), url.Values{"actor_detail": {"false"}, "changes_detail": {"false"}}, nil)
	if err != nil {
		return err
	}
	return c.DoJSON(req, wire)
}
