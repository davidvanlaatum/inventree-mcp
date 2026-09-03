package tools

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBarcodeClient hand-rolls every method the barcode tool family's narrow
// client interfaces need, mirroring tag_tools_test.go's fakeTagSearchClient
// shape rather than pulling in a mocking library. The Get*Sequence fields let
// a test return different has_barcode states across successive calls (e.g.
// preflight versus post-mutation verification) when a single fixed field
// would not distinguish them.
type fakeBarcodeClient struct {
	generateText        string
	generateErr         error
	generateCalledModel string
	generateCalledPK    int

	resolveMatch   inventree.BarcodeMatch
	resolveMatched bool
	resolveErr     error

	linkErr           error
	linkCalledBarcode string
	linkCalledKey     string
	linkCalledID      int

	unlinkErr       error
	unlinkCalledKey string
	unlinkCalledID  int

	partDetail              inventree.PartDetail
	partErr                 error
	stockItemDetail         inventree.StockItemDetail
	stockItemDetailSequence []inventree.StockItemDetail
	stockItemErr            error
	stockLocation           inventree.StockLocation
	stockLocationErr        error
	purchaseOrderDetail     inventree.PurchaseOrderDetail
	purchaseOrderErr        error

	historyPage    inventree.BarcodeScanHistoryPage
	historyErr     error
	historyQueries []inventree.BarcodeScanHistoryQuery
}

func (f *fakeBarcodeClient) GenerateBarcode(_ context.Context, model string, pk int) (string, error) {
	f.generateCalledModel = model
	f.generateCalledPK = pk
	return f.generateText, f.generateErr
}

func (f *fakeBarcodeClient) ResolveBarcode(_ context.Context, _ string) (inventree.BarcodeMatch, bool, error) {
	return f.resolveMatch, f.resolveMatched, f.resolveErr
}

func (f *fakeBarcodeClient) LinkBarcode(_ context.Context, barcodeText, objectTypeKey string, objectID int) error {
	f.linkCalledBarcode = barcodeText
	f.linkCalledKey = objectTypeKey
	f.linkCalledID = objectID
	return f.linkErr
}

func (f *fakeBarcodeClient) UnlinkBarcode(_ context.Context, objectTypeKey string, objectID int) error {
	f.unlinkCalledKey = objectTypeKey
	f.unlinkCalledID = objectID
	return f.unlinkErr
}

func (f *fakeBarcodeClient) GetPartDetail(_ context.Context, _ int) (inventree.PartDetail, error) {
	return f.partDetail, f.partErr
}

func (f *fakeBarcodeClient) GetStockItemDetail(_ context.Context, _ int) (inventree.StockItemDetail, error) {
	if len(f.stockItemDetailSequence) > 0 {
		next := f.stockItemDetailSequence[0]
		f.stockItemDetailSequence = f.stockItemDetailSequence[1:]
		return next, f.stockItemErr
	}
	return f.stockItemDetail, f.stockItemErr
}

func (f *fakeBarcodeClient) GetStockLocation(_ context.Context, _ int) (inventree.StockLocation, error) {
	return f.stockLocation, f.stockLocationErr
}

func (f *fakeBarcodeClient) GetPurchaseOrderDetail(_ context.Context, _ int) (inventree.PurchaseOrderDetail, error) {
	return f.purchaseOrderDetail, f.purchaseOrderErr
}

func (f *fakeBarcodeClient) SearchBarcodeScanHistoryPage(_ context.Context, query inventree.BarcodeScanHistoryQuery) (inventree.BarcodeScanHistoryPage, error) {
	f.historyQueries = append(f.historyQueries, query)
	return f.historyPage, f.historyErr
}

func depsForFakeBarcode(fake *fakeBarcodeClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func TestGenerateBarcodeHappyPathAndInvalidObjectType(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeBarcodeClient{generateText: "abc123"}
	_, out, err := generateBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, GenerateBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 12})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal("abc123", out.BarcodeText)
	a.Equal("stockitem", fake.generateCalledModel)
	a.Equal(12, fake.generateCalledPK)

	fake = &fakeBarcodeClient{}
	_, out, err = generateBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, GenerateBarcodeInput{ObjectType: "bogus", ObjectID: 12})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
	require.NotNil(t, out.Validation)
}

func TestResolveBarcodeStatusVocabulary(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeBarcodeClient{resolveMatched: false}
	_, out, err := resolveBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, ResolveBarcodeInput{BarcodeText: "abc123"})
	r.NoError(err)
	a.Equal(StatusNoMatch, out.Status)
	a.False(out.Matched)

	fake = &fakeBarcodeClient{resolveMatched: true, resolveMatch: inventree.BarcodeMatch{ObjectType: "stockitem", ObjectID: 7, WebURL: "https://inventory.example.test/stock/item/7/"}}
	_, out, err = resolveBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, ResolveBarcodeInput{BarcodeText: "abc123"})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(out.Matched)
	a.Equal(OwnerObjectStockItem, out.ObjectType)
	a.Equal(7, out.ObjectID)

	fake = &fakeBarcodeClient{resolveMatched: true, resolveMatch: inventree.BarcodeMatch{}}
	_, out, err = resolveBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, ResolveBarcodeInput{BarcodeText: "abc123"})
	r.NoError(err)
	a.Equal(StatusMatchedUnsupportedType, out.Status)
	a.True(out.Matched)
	a.Empty(out.ObjectType)

	fake = &fakeBarcodeClient{}
	_, out, err = resolveBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, ResolveBarcodeInput{BarcodeText: "   "})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
}

func TestAssignBarcodeVerifiesAfterLinkAndReportsPartialFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeBarcodeClient{stockItemDetail: inventree.StockItemDetail{PK: 12, HasBarcode: true}}
	_, out, err := assignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, AssignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 12, BarcodeText: "abc123"})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal("abc123", fake.linkCalledBarcode)
	a.Equal("stockitem", fake.linkCalledKey)
	a.Equal(12, fake.linkCalledID)

	unverified := &fakeBarcodeClient{stockItemDetail: inventree.StockItemDetail{PK: 12, HasBarcode: false}}
	_, out, err = assignBarcode(depsForFakeBarcode(unverified))(ctx, &mcp.CallToolRequest{}, AssignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 12, BarcodeText: "abc123"})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	a.NotEmpty(out.RecoveryPlan)
}

func TestAssignBarcodeInvalidInputsRejectedBeforeClientCall(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeBarcodeClient{}
	_, out, err := assignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, AssignBarcodeInput{ObjectType: "bogus", ObjectID: 12, BarcodeText: "abc123"})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
	a.Empty(fake.linkCalledKey, "an invalid object_type must be rejected before the client is called")

	_, out, err = assignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, AssignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 12, BarcodeText: "  "})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
	a.Empty(fake.linkCalledKey, "blank barcode_text must be rejected before the client is called")
}

// TestAssignBarcodeRedactsDuplicateConflict feeds the fake client a synthetic
// *inventree.APIError shaped exactly like the live-confirmed duplicate-
// assignment rejection: FieldErrors["error"] = ["Barcode matches existing
// item"], and FieldErrors[<the objectTypeKey sent>][0] a JSON-encoded string
// (not a nested object) that decodes to {api_url, instance, pk, web_url}.
func TestAssignBarcodeRedactsDuplicateConflict(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	embedded := `{"api_url":"/api/stock/12/","instance":{"pk":12,"part_detail":{"pk":5}},"pk":12,"web_url":"https://inventory.example.test/stock/item/12/"}`
	apiErr := &inventree.APIError{
		StatusCode: http.StatusBadRequest,
		Kind:       inventree.ErrorKindValidation,
		FieldErrors: map[string][]string{
			"error":     {"Barcode matches existing item"},
			"stockitem": {embedded},
		},
	}
	fake := &fakeBarcodeClient{linkErr: apiErr}
	_, out, err := assignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, AssignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 99, BarcodeText: "abc123"})
	r.NoError(err)
	a.Equal(StatusBarcodeConflict, out.Status)
	require.NotNil(t, out.Conflict)
	a.Equal(OwnerObjectStockItem, out.Conflict.ObjectType)
	a.Equal(12, out.Conflict.ObjectID)
	a.Equal("https://inventory.example.test/stock/item/12/", out.Conflict.WebURL)
}

// TestAssignBarcodeRedactsDuplicateConflictWithStringifiedPK covers the
// shape a live Testcontainers run against the pinned instance actually
// found (F-S99): DRF's nested-serializer error path stringifies every field
// in the embedded conflict object, including "pk" (e.g. "pk":"12" rather
// than "pk":12), unlike the plain numeric form exercised above.
func TestAssignBarcodeRedactsDuplicateConflictWithStringifiedPK(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	embedded := `{"api_url":"/api/stock/12/","instance":{"pk":"12","quantity":"5.0"},"pk":"12","web_url":"/web/stock/item/12"}`
	apiErr := &inventree.APIError{
		StatusCode: http.StatusBadRequest,
		Kind:       inventree.ErrorKindValidation,
		FieldErrors: map[string][]string{
			"error":     {"Barcode matches existing item"},
			"stockitem": {embedded},
		},
	}
	fake := &fakeBarcodeClient{linkErr: apiErr}
	_, out, err := assignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, AssignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 99, BarcodeText: "abc123"})
	r.NoError(err)
	a.Equal(StatusBarcodeConflict, out.Status)
	require.NotNil(t, out.Conflict)
	a.Equal(12, out.Conflict.ObjectID)
	a.Equal("/web/stock/item/12", out.Conflict.WebURL)
}

// TestAssignBarcodeUnsupportedObjectFieldIsNotTreatedAsConflict confirms the
// duplicate-conflict detection matches on the exact "Barcode matches
// existing item" substring, not merely FieldErrors["error"]'s presence, so
// InvenTree's different unsupported-object-field rejection is not
// misclassified as a conflict.
func TestAssignBarcodeUnsupportedObjectFieldIsNotTreatedAsConflict(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	apiErr := &inventree.APIError{
		StatusCode: http.StatusBadRequest,
		Kind:       inventree.ErrorKindValidation,
		FieldErrors: map[string][]string{
			"error": {"Missing data: provide one of ['part', 'stockitem', 'stocklocation', 'purchaseorder']"},
		},
	}
	fake := &fakeBarcodeClient{linkErr: apiErr}
	_, out, err := assignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, AssignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 99, BarcodeText: "abc123"})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
	a.Nil(out.Conflict)
}

func TestUnassignBarcodeRequiresConfirm(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeBarcodeClient{stockItemDetail: inventree.StockItemDetail{PK: 12, HasBarcode: true}}
	_, out, err := unassignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, UnassignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 12, Confirm: false})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	require.NotNil(t, out.Clarification)
	a.Equal("confirm", out.Clarification.Field)
	a.Empty(fake.unlinkCalledKey, "unassign must not call UnlinkBarcode before confirm:true")
}

func TestUnassignBarcodeConfirmedExecutesAndVerifies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeBarcodeClient{stockItemDetailSequence: []inventree.StockItemDetail{
		{PK: 12, HasBarcode: true},  // preflight read
		{PK: 12, HasBarcode: false}, // post-unlink verification
	}}
	_, out, err := unassignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, UnassignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 12, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal("stockitem", fake.unlinkCalledKey)
	a.Equal(12, fake.unlinkCalledID)
}

func TestUnassignBarcodeRejectsObjectWithNoBarcodeAssigned(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeBarcodeClient{stockItemDetail: inventree.StockItemDetail{PK: 12, HasBarcode: false}}
	_, out, err := unassignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, UnassignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 12, Confirm: true})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
	a.Empty(fake.unlinkCalledKey)
}

func TestSearchBarcodeScanHistoryMapsInvalidUserID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	apiErr := &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation, FieldErrors: map[string][]string{}}
	fake := &fakeBarcodeClient{historyErr: apiErr}
	userID := 999
	_, out, err := searchBarcodeScanHistory(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, SearchBarcodeScanHistoryInput{UserID: &userID})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
	require.NotNil(t, out.Validation)
	require.Len(t, out.Validation.Fields, 1)
	a.Equal("user_id", out.Validation.Fields[0].Field)
}

// TestSearchBarcodeScanHistoryFastPathVsBoundedWalk confirms the handler
// only takes the bounded internal multi-page walk (upstream page size
// barcodeScanHistoryUpstreamPageSize) when endpoint/from/to filtering is
// requested, and forwards the caller's own limit/offset directly to a single
// upstream page otherwise.
func TestSearchBarcodeScanHistoryFastPathVsBoundedWalk(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	entry := inventree.BarcodeScanHistoryEntry{PK: 1, Data: "abc123", Timestamp: "2026-01-01T00:00:00Z", Endpoint: "stock-detail", Result: true}

	fastPath := &fakeBarcodeClient{historyPage: inventree.BarcodeScanHistoryPage{Count: 1, HasMore: false, Results: []inventree.BarcodeScanHistoryEntry{entry}}}
	_, out, err := searchBarcodeScanHistory(depsForFakeBarcode(fastPath))(ctx, &mcp.CallToolRequest{}, SearchBarcodeScanHistoryInput{Limit: 5, Offset: 3})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	require.Len(t, fastPath.historyQueries, 1, "no endpoint/from/to filter must forward directly to a single upstream page")
	a.Equal(5, fastPath.historyQueries[0].Limit)
	a.Equal(3, fastPath.historyQueries[0].Offset)
	require.Len(t, out.Records, 1)
	a.Equal("abc123", out.Records[0].Data)

	walk := &fakeBarcodeClient{historyPage: inventree.BarcodeScanHistoryPage{Count: 1, HasMore: false, Results: []inventree.BarcodeScanHistoryEntry{entry}}}
	_, out, err = searchBarcodeScanHistory(depsForFakeBarcode(walk))(ctx, &mcp.CallToolRequest{}, SearchBarcodeScanHistoryInput{Endpoint: "stock-detail"})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	require.Len(t, walk.historyQueries, 1)
	a.Equal(barcodeScanHistoryUpstreamPageSize, walk.historyQueries[0].Limit, "endpoint filtering must switch to the bounded internal walk's own upstream page size")

	noMatch := &fakeBarcodeClient{historyPage: inventree.BarcodeScanHistoryPage{Count: 1, HasMore: false, Results: []inventree.BarcodeScanHistoryEntry{entry}}}
	_, out, err = searchBarcodeScanHistory(depsForFakeBarcode(noMatch))(ctx, &mcp.CallToolRequest{}, SearchBarcodeScanHistoryInput{Endpoint: "does-not-exist"})
	r.NoError(err)
	a.Equal(StatusNotFound, out.Status)
	a.Empty(out.Records)
}

func TestSearchBarcodeScanHistoryRejectsNegativeOffsetAndBadTimestamps(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeBarcodeClient{}
	_, out, err := searchBarcodeScanHistory(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, SearchBarcodeScanHistoryInput{Offset: -1})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
	a.Empty(fake.historyQueries)

	_, out, err = searchBarcodeScanHistory(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, SearchBarcodeScanHistoryInput{From: "not-a-timestamp"})
	r.NoError(err)
	a.Equal(StatusValidationFailed, out.Status)
	a.Empty(fake.historyQueries)
}

// TestParseBarcodeScanHistoryTimestampAcceptsInvenTreesRealFormat guards
// against regressing to a single-layout (RFC3339-only) parse. Live evidence
// against the pinned instance (F-S99) found /api/barcode/history/ actually
// renders "timestamp" as "2026-09-03 11:08" -- space-separated, no seconds,
// no timezone -- not RFC3339. Before this fix, an entry in this exact shape
// silently failed to parse, so barcodeScanHistoryEntryMatches excluded every
// row whenever a caller supplied from/to, always returning zero matches.
func TestParseBarcodeScanHistoryTimestampAcceptsInvenTreesRealFormat(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	for _, value := range []string{
		"2026-09-03 11:08",
		"2026-09-03T11:08:00Z",
		"2026-09-03 11:08:00",
	} {
		_, ok := parseBarcodeScanHistoryTimestamp(value)
		a.True(ok, "expected %q to parse", value)
	}

	_, ok := parseBarcodeScanHistoryTimestamp("not-a-timestamp")
	a.False(ok)
}

// TestSearchBarcodeScanHistoryFromToWalkMatchesInvenTreesRealTimestampFormat
// exercises the bounded walk end-to-end with a row shaped exactly like live
// InvenTree evidence, confirming a from/to-filtered query actually returns
// it rather than silently excluding every row via a failed timestamp parse.
func TestSearchBarcodeScanHistoryFromToWalkMatchesInvenTreesRealTimestampFormat(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	entry := inventree.BarcodeScanHistoryEntry{PK: 1, Data: "abc123", Timestamp: "2026-09-03 11:08", Result: true}
	fake := &fakeBarcodeClient{historyPage: inventree.BarcodeScanHistoryPage{Count: 1, HasMore: false, Results: []inventree.BarcodeScanHistoryEntry{entry}}}

	_, out, err := searchBarcodeScanHistory(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, SearchBarcodeScanHistoryInput{
		From: "2026-09-01T00:00:00Z",
		To:   "2026-09-30T00:00:00Z",
	})
	r.NoError(err)
	a.Equal(StatusOK, out.Status, "a row in InvenTree's real timestamp format must be found within a matching from/to range")
	require.Len(t, out.Records, 1)
	a.Equal("abc123", out.Records[0].Data)
}

// TestBarcodeScanHistoryToBoundIsExclusive locks in the operator-approved
// contract (docs/TASKS.md F-S99 Decisions: "from is inclusive and to is
// exclusive"). A row timestamped exactly at to must be excluded, not
// included -- the original timestamp.After(to) comparison wrongly kept it.
func TestBarcodeScanHistoryToBoundIsExclusive(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	boundary, err := time.Parse(time.RFC3339, "2026-09-03T11:08:00Z")
	require.NoError(t, err)
	entry := inventree.BarcodeScanHistoryEntry{PK: 1, Data: "abc123", Timestamp: "2026-09-03T11:08:00Z"}

	a.False(barcodeScanHistoryEntryMatches(entry, "", time.Time{}, boundary), "a row timestamped exactly at to must be excluded (to is exclusive)")
	a.True(barcodeScanHistoryEntryMatches(entry, "", time.Time{}, boundary.Add(time.Second)), "a row timestamped just before to must be included")
	a.True(barcodeScanHistoryEntryMatches(entry, "", boundary, time.Time{}), "a row timestamped exactly at from must be included (from is inclusive)")
}

// TestSearchBarcodeScanHistoryWalkReportsHasMoreWhenTruncatedByMaxPageDepth
// covers the case none of walkBarcodeScanHistory's other cutoffs (record
// limit, byte budget, time budget) represent: the loop simply exhausts its
// "for page < maxPages" condition while the last fetched upstream page still
// had HasMore true. Before this fix, that path fell through with hasMore
// left false, wrongly telling the caller pagination was complete when it was
// only bounded by the configured page depth.
func TestSearchBarcodeScanHistoryWalkReportsHasMoreWhenTruncatedByMaxPageDepth(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	nonMatching := inventree.BarcodeScanHistoryEntry{PK: 1, Data: "abc123", Endpoint: "other-endpoint", Timestamp: "2026-09-03 11:08"}
	fake := &fakeBarcodeClient{historyPage: inventree.BarcodeScanHistoryPage{Count: 1, HasMore: true, Results: []inventree.BarcodeScanHistoryEntry{nonMatching}}}
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }, ScanHistoryMaxPageDepth: 2}

	_, out, err := searchBarcodeScanHistory(deps)(ctx, &mcp.CallToolRequest{}, SearchBarcodeScanHistoryInput{Endpoint: "does-not-exist"})
	r.NoError(err)
	a.Equal(StatusNotFound, out.Status)
	a.True(out.HasMore, "the walk was truncated by ScanHistoryMaxPageDepth while the last upstream page still had more; hasMore must reflect that")
	r.Len(fake.historyQueries, 2, "the walk must have stopped exactly at ScanHistoryMaxPageDepth, not looped forever")
}

// TestAssignBarcodeDefiniteRejectionFailsFastWithoutVerifying confirms a
// definite (non-duplicate-conflict, non-validation-mappable) 4xx rejection
// -- e.g. 403 Forbidden -- returns an error immediately, matching
// verifyOwnerAssignment's precedent (owner_tools.go): a definite rejection
// never applied, so there is nothing to verify.
func TestAssignBarcodeDefiniteRejectionFailsFastWithoutVerifying(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	apiErr := &inventree.APIError{StatusCode: http.StatusForbidden, Kind: inventree.ErrorKindPermission}
	fake := &fakeBarcodeClient{linkErr: apiErr, stockItemErr: errors.New("hasBarcode must not be called after a definite rejection")}
	_, _, err := assignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, AssignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 99, BarcodeText: "abc123"})
	r.Error(err)
}

// TestAssignBarcodeAmbiguousErrorFallsThroughToVerification confirms an
// ambiguous mutation error (a 5xx, not a definite 4xx rejection) does not
// fail fast: it falls through to the same read-after-write verification the
// success path uses, since the mutation may have actually applied upstream
// despite the error.
func TestAssignBarcodeAmbiguousErrorFallsThroughToVerification(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	apiErr := &inventree.APIError{StatusCode: http.StatusBadGateway, Kind: inventree.ErrorKindServer}
	fake := &fakeBarcodeClient{linkErr: apiErr, stockItemDetail: inventree.StockItemDetail{PK: 99, HasBarcode: true}}
	_, out, err := assignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, AssignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 99, BarcodeText: "abc123"})
	r.NoError(err)
	a.Equal(StatusOK, out.Status, "an ambiguous error must not be trusted blindly when the read-back proves the link actually applied")
}

// TestUnassignBarcodeDefiniteRejectionFailsFastWithoutVerifying and
// TestUnassignBarcodeAmbiguousErrorFallsThroughToVerification mirror the
// assign_barcode cases above for unassign_barcode's UnlinkBarcode call.
func TestUnassignBarcodeDefiniteRejectionFailsFastWithoutVerifying(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	apiErr := &inventree.APIError{StatusCode: http.StatusForbidden, Kind: inventree.ErrorKindPermission}
	fake := &fakeBarcodeClient{
		unlinkErr: apiErr,
		// Only one entry: the preflight read. If the fix incorrectly falls
		// through to a post-mutation verification read after a definite
		// rejection, the sequence is exhausted and GetStockItemDetail falls
		// back to the zero-value stockItemDetail (HasBarcode false, no
		// error) -- which reads as a successful unlink, turning this test's
		// expected error into a nil error and StatusOK, catching the bug.
		stockItemDetailSequence: []inventree.StockItemDetail{{PK: 99, HasBarcode: true}},
	}
	_, _, err := unassignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, UnassignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 99, Confirm: true})
	r.Error(err)
}

func TestUnassignBarcodeAmbiguousErrorFallsThroughToVerification(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	apiErr := &inventree.APIError{StatusCode: http.StatusBadGateway, Kind: inventree.ErrorKindServer}
	fake := &fakeBarcodeClient{
		unlinkErr: apiErr,
		// preflight read (has a barcode, so the tool proceeds) then the
		// post-mutation verification read (barcode actually gone).
		stockItemDetailSequence: []inventree.StockItemDetail{{PK: 99, HasBarcode: true}, {PK: 99, HasBarcode: false}},
	}
	_, out, err := unassignBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, UnassignBarcodeInput{ObjectType: OwnerObjectStockItem, ObjectID: 99, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, out.Status, "an ambiguous error must not be trusted blindly when the read-back proves the unlink actually applied")
}

// TestRedactBarcodeConflictFallsBackWhenEmbeddedJSONIsUnparsable confirms the
// fallback path (an embedded conflict string that fails to json.Unmarshal)
// never forwards the raw string -- it degrades to a bare conflict carrying
// only the already-known ObjectType.
func TestRedactBarcodeConflictFallsBackWhenEmbeddedJSONIsUnparsable(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	apiErr := &inventree.APIError{
		StatusCode: http.StatusBadRequest,
		FieldErrors: map[string][]string{
			"error":     {"Barcode matches existing item"},
			"stockitem": {"not valid json"},
		},
	}
	conflict := redactBarcodeConflict(apiErr, OwnerObjectStockItem, "stockitem")
	require.NotNil(t, conflict)
	a.Equal(OwnerObjectStockItem, conflict.ObjectType)
	a.Zero(conflict.ObjectID)
	a.Empty(conflict.WebURL)
}

// TestResolveBarcodeRejectsBareModelNotInObjectTypeVocabulary covers
// resolveBarcode's defensive branch for a non-empty ObjectType the client
// returned that isn't in barcodeObjectTypeByBareModel -- a should-never-
// happen path today (the client only ever returns keys from its own
// barcodeInScopeObjectKeys, the same domain), but worth locking in rather
// than leaving untested.
func TestResolveBarcodeRejectsBareModelNotInObjectTypeVocabulary(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeBarcodeClient{resolveMatched: true, resolveMatch: inventree.BarcodeMatch{ObjectType: "notarealtype", ObjectID: 5}}
	_, _, err := resolveBarcode(depsForFakeBarcode(fake))(ctx, &mcp.CallToolRequest{}, ResolveBarcodeInput{BarcodeText: "abc123"})
	r.Error(err)
}
