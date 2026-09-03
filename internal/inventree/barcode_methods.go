package inventree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
)

// barcodeInScopeObjectKeys maps the bare-lowercase InvenTree object-type
// spelling (used identically by /api/barcode/generate/'s "model" field and
// as the JSON key for /api/barcode/link/ and /api/barcode/unlink/, and as
// the top-level match-object key in a /api/barcode/ resolve response) to
// itself, for the four object types this server has tool support for.
var barcodeInScopeObjectKeys = map[string]bool{
	"part":          true,
	"stockitem":     true,
	"stocklocation": true,
	"purchaseorder": true,
}

// barcodeOutOfScopeObjectKeys are the other object-type keys InvenTree's
// generic /api/barcode/ resolve response can carry (per live evidence),
// recognized so ResolveBarcode can report "matched but unsupported object
// type" instead of misreporting a genuine match as "no match".
var barcodeOutOfScopeObjectKeys = map[string]bool{
	"build":              true,
	"manufacturerpart":   true,
	"supplierpart":       true,
	"returnorder":        true,
	"salesorder":         true,
	"salesordershipment": true,
	"transferorder":      true,
}

// barcodeNoMatchMessage is the exact FieldErrors["error"] message InvenTree
// returns for a generic scan with no matching object, confirmed live
// against the pinned instance (HTTP 400).
const barcodeNoMatchMessage = "No match found for barcode data"

// GenerateBarcode requests barcode text for an existing object from the
// configured barcode-generation plugin. Model must be one of the bare
// lowercase spellings in barcodeInScopeObjectKeys. Generation is text-only:
// it never populates the object's barcode_hash, so a separate LinkBarcode
// call is required to assign the generated text.
func (c *Client) GenerateBarcode(ctx context.Context, model string, pk int) (string, error) {
	var out struct {
		Barcode string `json:"barcode"`
	}
	err := c.Post(ctx, "/api/barcode/generate/", map[string]any{"model": model, "pk": pk}, &out)
	return out.Barcode, err
}

type barcodeMatchObject struct {
	PK     int    `json:"pk"`
	WebURL string `json:"web_url"`
}

// ResolveBarcode scans/resolves raw barcode text against InvenTree's generic
// /api/barcode/ endpoint. It returns (BarcodeMatch{}, false, nil) for the
// specific "no match" 400 shape (barcodeNoMatchMessage); any other error
// propagates unchanged. A match on an in-scope object type returns
// (match, true, nil) with ObjectType set to the bare-lowercase InvenTree
// spelling. A match on a recognized out-of-scope object type (e.g. "build")
// returns (BarcodeMatch{}, true, nil) -- Matched true but ObjectType empty
// signals "matched, but this server has no tool support for that object
// type" to the tools layer, distinct from a genuine no-match. Live evidence
// only ever showed one recognized object-type key per response, but the
// response keys are collected and sorted before deciding rather than picking
// the first hit from a map range (whose iteration order is randomized): if a
// future InvenTree response ever carried more than one recognized key at
// once, silently picking one at random would return a different matched
// object across identical calls. That condition is instead reported as an
// explicit error.
func (c *Client) ResolveBarcode(ctx context.Context, barcodeText string) (BarcodeMatch, bool, error) {
	var out map[string]json.RawMessage
	err := c.Post(ctx, "/api/barcode/", map[string]any{"barcode": barcodeText}, &out)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && fieldErrorContains(apiErr.FieldErrors["error"], barcodeNoMatchMessage) {
			return BarcodeMatch{}, false, nil
		}
		return BarcodeMatch{}, false, err
	}

	var inScopeKeys, outOfScopeKeys []string
	for key := range out {
		switch {
		case barcodeInScopeObjectKeys[key]:
			inScopeKeys = append(inScopeKeys, key)
		case barcodeOutOfScopeObjectKeys[key]:
			outOfScopeKeys = append(outOfScopeKeys, key)
		}
	}
	sort.Strings(inScopeKeys)
	sort.Strings(outOfScopeKeys)

	if len(inScopeKeys)+len(outOfScopeKeys) > 1 {
		return BarcodeMatch{}, false, fmt.Errorf("ambiguous barcode match: response carried multiple recognized object-type keys %v", append(inScopeKeys, outOfScopeKeys...))
	}
	if len(inScopeKeys) == 1 {
		key := inScopeKeys[0]
		var object barcodeMatchObject
		if unmarshalErr := json.Unmarshal(out[key], &object); unmarshalErr != nil {
			return BarcodeMatch{}, false, unmarshalErr
		}
		return BarcodeMatch{ObjectType: key, ObjectID: object.PK, WebURL: object.WebURL}, true, nil
	}
	if len(outOfScopeKeys) == 1 {
		return BarcodeMatch{}, true, nil
	}
	return BarcodeMatch{}, false, nil
}

// LinkBarcode assigns raw barcode text to an existing object through
// /api/barcode/link/. objectTypeKey is both the bare-lowercase InvenTree
// object-type spelling and the JSON body key InvenTree expects. On
// rejection (duplicate assignment, or an unsupported objectTypeKey), the raw
// *APIError propagates unchanged -- this client method stays a thin
// transport; redacting the duplicate-conflict shape's embedded object record
// is the tools layer's responsibility (see redactBarcodeConflict in
// internal/tools/barcode_tools.go).
func (c *Client) LinkBarcode(ctx context.Context, barcodeText, objectTypeKey string, objectID int) error {
	return c.Post(ctx, "/api/barcode/link/", map[string]any{"barcode": barcodeText, objectTypeKey: objectID}, nil)
}

// UnlinkBarcode removes any barcode linked to an existing object through
// /api/barcode/unlink/.
func (c *Client) UnlinkBarcode(ctx context.Context, objectTypeKey string, objectID int) error {
	return c.Post(ctx, "/api/barcode/unlink/", map[string]any{objectTypeKey: objectID}, nil)
}

// SearchBarcodeScanHistoryPage fetches a single bounded page over
// /api/barcode/history/, shaped like SearchTagsPage: HasMore is computed
// from the upstream Next cursor rather than surfacing raw Next/Previous
// URLs.
func (c *Client) SearchBarcodeScanHistoryPage(ctx context.Context, query BarcodeScanHistoryQuery) (BarcodeScanHistoryPage, error) {
	page, err := listPage[BarcodeScanHistoryEntry](ctx, c, "/api/barcode/history/", query.values())
	return BarcodeScanHistoryPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil && *page.Next != ""}, err
}

func fieldErrorContains(messages []string, target string) bool {
	for _, message := range messages {
		if message == target {
			return true
		}
	}
	return false
}
