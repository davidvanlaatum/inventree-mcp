package inventree

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// stockAuditWire deliberately omits actor_detail, actor_username, object_repr,
// changes_detail, additional_data, correlation IDs, and remote addresses/ports.
// Those generic audit fields are outside the stock-history tool contract.
type stockAuditWire struct {
	PK              int             `json:"pk"`
	Timestamp       string          `json:"timestamp"`
	Action          int             `json:"action"`
	Actor           *int            `json:"actor"`
	EventType       *int            `json:"event_type"`
	EventLabel      string          `json:"event_label"`
	Notes           string          `json:"notes"`
	Deltas          json.RawMessage `json:"deltas"`
	Changes         json.RawMessage `json:"changes"`
	ModelType       string          `json:"model_type"`
	ObjectPK        string          `json:"object_pk"`
	ParentModelType *string         `json:"parent_model_type"`
	ParentObjectPK  *string         `json:"parent_object_pk"`
}

var errStockAuditScope = errors.New("audit entry is outside the stock-item history scope")

func (w stockAuditWire) stockEntry() (StockAudit, error) {
	if w.ModelType != "stock.stockitem" {
		return StockAudit{}, errStockAuditScope
	}
	entry, err := w.auditEntry()
	if err != nil {
		return StockAudit{}, err
	}
	return stockAuditProjection(entry), nil
}
func stockAuditProjection(entry AuditEntry) StockAudit {
	item := entry.ModelID
	var part *int
	if entry.ParentModelType != nil && *entry.ParentModelType == "part.part" {
		part = entry.ParentModelID
	}
	return StockAudit{PK: entry.PK, Item: &item, Part: part, Date: entry.Timestamp, User: entry.Actor, EventType: entry.EventType, Action: entry.Action, Label: entry.EventLabel, Notes: entry.Notes, Deltas: entry.Deltas, Changes: entry.Changes}
}

// SearchStockAuditPage reads one server-scoped audit page. Count and Next refer
// to the upstream page, which can include part-owned non-stock audit entries;
// Results contains only stock-item entries. Callers must follow Next (not an
// empty Results slice) and compute their filtered count after the bounded scan.
func (c *Client) SearchStockAuditPage(ctx context.Context, query StockAuditQuery) (Page[StockAudit], error) {
	if query.ItemID < 0 || query.PartID < 0 {
		return Page[StockAudit]{}, errors.New("stock audit IDs must be non-negative")
	}
	scope := AuditScope{ModelType: "stock.stockitem", ModelID: query.ItemID}
	if query.ItemID == 0 {
		scope = AuditScope{ModelType: "part.part", ModelID: query.PartID, IncludeChildren: true}
	}
	page, err := c.SearchAuditEntriesPage(ctx, AuditQuery{AuditScope: scope, Limit: query.Limit, Offset: query.Offset})
	if err != nil {
		return Page[StockAudit]{}, err
	}
	out := Page[StockAudit]{Count: page.Count, Next: page.Next, Previous: page.Previous, Results: []StockAudit{}}
	for _, entry := range page.Results {
		if entry.ModelType != "stock.stockitem" {
			continue
		}
		stock := stockAuditProjection(entry)
		if query.PartID > 0 && (stock.Part == nil || *stock.Part != query.PartID) {
			continue
		}
		out.Results = append(out.Results, stock)
	}
	return out, nil
}

func (c *Client) GetStockAuditEntry(ctx context.Context, id int) (StockAudit, error) {
	if id <= 0 {
		return StockAudit{}, &APIError{StatusCode: http.StatusNotFound, Kind: ErrorKindNotFound}
	}
	var wire stockAuditWire
	if err := c.getAuditWire(ctx, id, &wire); err != nil {
		return StockAudit{}, err
	}
	if wire.PK != id {
		return StockAudit{}, errors.New("InvenTree returned a mismatched stock audit identity")
	}
	entry, err := wire.stockEntry()
	if errors.Is(err, errStockAuditScope) {
		return StockAudit{}, &APIError{StatusCode: http.StatusNotFound, Kind: ErrorKindNotFound}
	}
	return entry, err
}

func sanitizeStockAuditChanges(raw json.RawMessage) (map[string]any, error) {
	return sanitizeAuditChanges("stock.stockitem", raw)
}
