package tools

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOwnerClient struct {
	owners             map[int]inventree.Owner
	ownerSearchResults []inventree.Owner
	searchErr          error
	lastOwnerQuery     inventree.OwnerQuery

	parts           map[int]inventree.PartDetail
	purchaseOrders  map[int]inventree.PurchaseOrderDetail
	stockItems      map[int]inventree.StockItemDetail
	stockLocations  map[int]inventree.StockLocation
	updateErr       error
	updateCalls     int
	lastPatch       inventree.PatchFields
	afterUpdate     func()
	suppressPersist bool
}

func newFakeOwnerClient() *fakeOwnerClient {
	return &fakeOwnerClient{
		owners: map[int]inventree.Owner{}, parts: map[int]inventree.PartDetail{},
		purchaseOrders: map[int]inventree.PurchaseOrderDetail{}, stockItems: map[int]inventree.StockItemDetail{},
		stockLocations: map[int]inventree.StockLocation{},
	}
}

func ownerDeps(fake *fakeOwnerClient) Dependencies {
	return Dependencies{
		ClientFromContext: func(context.Context) (any, error) { return fake, nil },
		ownerPlanStore:    newOwnerPlanStore(time.Now, randomStockPlanToken),
	}
}

func (f *fakeOwnerClient) GetOwner(_ context.Context, id int) (inventree.Owner, error) {
	value, ok := f.owners[id]
	if !ok {
		return inventree.Owner{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeOwnerClient) SearchOwnersPage(_ context.Context, query inventree.OwnerQuery) (inventree.Page[inventree.Owner], error) {
	f.lastOwnerQuery = query
	if f.searchErr != nil {
		return inventree.Page[inventree.Owner]{}, f.searchErr
	}
	return inventree.Page[inventree.Owner]{Count: len(f.ownerSearchResults), Results: f.ownerSearchResults}, nil
}

func (f *fakeOwnerClient) GetPartDetail(_ context.Context, id int) (inventree.PartDetail, error) {
	value, ok := f.parts[id]
	if !ok {
		return inventree.PartDetail{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeOwnerClient) UpdatePart(_ context.Context, id int, fields inventree.PatchFields) (inventree.Part, error) {
	f.updateCalls++
	f.lastPatch = fields
	if !f.suppressPersist {
		record := f.parts[id]
		record.Responsible = patchedOwnerValue(fields["responsible"])
		f.parts[id] = record
	}
	if f.afterUpdate != nil {
		f.afterUpdate()
	}
	return inventree.Part{PK: id}, f.updateErr
}

func (f *fakeOwnerClient) GetPurchaseOrderDetail(_ context.Context, id int) (inventree.PurchaseOrderDetail, error) {
	value, ok := f.purchaseOrders[id]
	if !ok {
		return inventree.PurchaseOrderDetail{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeOwnerClient) UpdatePurchaseOrderDetail(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderDetail, error) {
	f.updateCalls++
	f.lastPatch = fields
	if !f.suppressPersist {
		record := f.purchaseOrders[id]
		record.Responsible = patchedOwnerValue(fields["responsible"])
		f.purchaseOrders[id] = record
	}
	if f.afterUpdate != nil {
		f.afterUpdate()
	}
	return f.purchaseOrders[id], f.updateErr
}

func (f *fakeOwnerClient) GetStockItemDetail(_ context.Context, id int) (inventree.StockItemDetail, error) {
	value, ok := f.stockItems[id]
	if !ok {
		return inventree.StockItemDetail{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeOwnerClient) UpdateStockItem(_ context.Context, id int, fields inventree.PatchFields) (inventree.StockItem, error) {
	f.updateCalls++
	f.lastPatch = fields
	if !f.suppressPersist {
		record := f.stockItems[id]
		record.Owner = patchedOwnerValue(fields["owner"])
		f.stockItems[id] = record
	}
	if f.afterUpdate != nil {
		f.afterUpdate()
	}
	return inventree.StockItem{PK: id}, f.updateErr
}

func (f *fakeOwnerClient) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
	value, ok := f.stockLocations[id]
	if !ok {
		return inventree.StockLocation{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeOwnerClient) UpdateStockLocation(_ context.Context, id int, fields inventree.PatchFields) (inventree.StockLocation, error) {
	f.updateCalls++
	f.lastPatch = fields
	if !f.suppressPersist {
		record := f.stockLocations[id]
		record.Owner = patchedOwnerValue(fields["owner"])
		f.stockLocations[id] = record
	}
	if f.afterUpdate != nil {
		f.afterUpdate()
	}
	return f.stockLocations[id], f.updateErr
}

func patchedOwnerValue(value inventree.PatchValue) *int {
	raw := value.Value()
	if raw == nil {
		return nil
	}
	id := raw.(int)
	return &id
}

func TestSearchOwnersRequiresQueryOrSupportedObjectType(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()

	_, _, err := searchOwners(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchOwnersInput{})
	require.Error(t, err)

	_, out, err := searchOwners(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchOwnersInput{ObjectType: OwnerObjectPart})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestSearchOwnersFiltersByTypeAndProjectsPrivacySafeFields(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()
	fake.ownerSearchResults = []inventree.Owner{
		{PK: 1, OwnerID: 5, OwnerModel: "user", Name: "jdoe", Label: "User: jdoe"},
		{PK: 2, OwnerID: 6, OwnerModel: "group", Name: "engineering", Label: "Group: engineering"},
	}

	_, out, err := searchOwners(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchOwnersInput{Query: "j"})
	require.NoError(t, err)
	require.Equal(t, StatusOK, out.Status)
	require.Len(t, out.Results, 2)

	_, filtered, err := searchOwners(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchOwnersInput{Query: "j", Type: "user"})
	require.NoError(t, err)
	require.Equal(t, StatusOK, filtered.Status)
	require.Len(t, filtered.Results, 1)
	assert.Equal(t, 1, filtered.Results[0].PK)
	assert.Equal(t, "user", filtered.Results[0].Type)
	assert.Equal(t, "jdoe", filtered.Results[0].Name)
}

func TestSearchOwnersPassesIsActiveFilterToClient(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()

	_, _, err := searchOwners(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchOwnersInput{Query: "jdoe", IsActive: dvgoutils.Ptr(true)})
	require.NoError(t, err)
	require.NotNil(t, fake.lastOwnerQuery.IsActive)
	assert.True(t, *fake.lastOwnerQuery.IsActive)

	_, _, err = searchOwners(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchOwnersInput{Query: "jdoe", IsActive: dvgoutils.Ptr(false)})
	require.NoError(t, err)
	require.NotNil(t, fake.lastOwnerQuery.IsActive)
	assert.False(t, *fake.lastOwnerQuery.IsActive)
}

func TestGetOwnerReturnsNotFoundForMissingRecord(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()

	_, out, err := getOwner(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 99})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)

	fake.owners[7] = inventree.Owner{PK: 7, OwnerModel: "user", Name: "jdoe", Label: "User: jdoe"}
	_, found, err := getOwner(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 7})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, found.Status)
	assert.Equal(t, 7, found.Record.PK)
}

func TestAssignOwnerRejectsInvalidObjectTypeAndID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()

	_, out, err := assignOwner(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: "sales_order", ObjectID: 1})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)

	_, out, err = assignOwner(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectPart, ObjectID: 0})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestAssignOwnerReturnsNotFoundForMissingObject(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()

	_, out, err := assignOwner(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockItem, ObjectID: 40})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestAssignOwnerRejectsNoopClearAndNoopAssign(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()
	fake.stockLocations[10] = inventree.StockLocation{PK: 10, Name: "Bin"}

	_, out, err := assignOwner(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)

	fake.stockLocations[10] = inventree.StockLocation{PK: 10, Name: "Bin", Owner: dvgoutils.Ptr(5)}
	_, out, err = assignOwner(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, OwnerID: dvgoutils.Ptr(5)})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestAssignOwnerRejectsUnresolvableOwnerID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()
	fake.stockLocations[10] = inventree.StockLocation{PK: 10, Name: "Bin"}

	_, out, err := assignOwner(ownerDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, OwnerID: dvgoutils.Ptr(99)})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
	assert.Zero(t, fake.updateCalls)
}

// TestAssignOwnerAcrossObjectMatrix exercises preview-then-confirm assignment
// and clearing across every F-S48 object type, proving the correct
// underlying PATCH field name (responsible vs owner) and cross-object
// consistency of the guarded plan-token flow.
func TestAssignOwnerAcrossObjectMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		objectType string
		field      string
		seed       func(*fakeOwnerClient)
		current    func(*fakeOwnerClient) *int
	}{
		{
			objectType: OwnerObjectPart, field: "responsible",
			seed:    func(f *fakeOwnerClient) { f.parts[1] = inventree.PartDetail{PK: 1, Name: "Widget"} },
			current: func(f *fakeOwnerClient) *int { return f.parts[1].Responsible },
		},
		{
			objectType: OwnerObjectPurchaseOrder, field: "responsible",
			seed: func(f *fakeOwnerClient) {
				f.purchaseOrders[2] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 2, Reference: "PO-1"}}
			},
			current: func(f *fakeOwnerClient) *int { return f.purchaseOrders[2].Responsible },
		},
		{
			objectType: OwnerObjectStockItem, field: "owner",
			seed:    func(f *fakeOwnerClient) { f.stockItems[3] = inventree.StockItemDetail{PK: 3} },
			current: func(f *fakeOwnerClient) *int { return f.stockItems[3].Owner },
		},
		{
			objectType: OwnerObjectStockLocation, field: "owner",
			seed:    func(f *fakeOwnerClient) { f.stockLocations[4] = inventree.StockLocation{PK: 4, Name: "Bin"} },
			current: func(f *fakeOwnerClient) *int { return f.stockLocations[4].Owner },
		},
	}
	objectID := map[string]int{OwnerObjectPart: 1, OwnerObjectPurchaseOrder: 2, OwnerObjectStockItem: 3, OwnerObjectStockLocation: 4}

	for _, tc := range cases {
		t.Run(tc.objectType, func(t *testing.T) {
			t.Parallel()
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeOwnerClient()
			tc.seed(fake)
			fake.owners[9] = inventree.Owner{PK: 9, OwnerModel: "user", Name: "jdoe", Label: "User: jdoe"}
			deps := ownerDeps(fake)
			id := objectID[tc.objectType]

			_, preview, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: tc.objectType, ObjectID: id, OwnerID: dvgoutils.Ptr(9)})
			require.NoError(t, err)
			require.Equal(t, StatusClarificationRequired, preview.Status)
			require.NotEmpty(t, preview.PlanHash)
			require.NotNil(t, preview.Owner)
			assert.Equal(t, 9, preview.Owner.PK)
			assert.Zero(t, fake.updateCalls)

			_, confirmed, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: tc.objectType, ObjectID: id, OwnerID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
			require.NoError(t, err)
			require.Equal(t, StatusOK, confirmed.Status)
			assert.True(t, confirmed.Verified)
			require.NotNil(t, confirmed.OwnerID)
			assert.Equal(t, 9, *confirmed.OwnerID)
			assert.Equal(t, 1, fake.updateCalls)
			require.Contains(t, fake.lastPatch, tc.field)
			require.NotNil(t, tc.current(fake))
			assert.Equal(t, 9, *tc.current(fake))

			// Clear it back out through the same guarded flow.
			_, clearPreview, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: tc.objectType, ObjectID: id})
			require.NoError(t, err)
			require.Equal(t, StatusClarificationRequired, clearPreview.Status)
			require.Nil(t, clearPreview.Owner)
			_, cleared, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: tc.objectType, ObjectID: id, Confirm: true, PlanHash: clearPreview.PlanHash})
			require.NoError(t, err)
			require.Equal(t, StatusOK, cleared.Status)
			assert.Nil(t, cleared.OwnerID)
			assert.Nil(t, tc.current(fake))
		})
	}
}

func TestAssignOwnerRejectsStaleReusedAndCrossPrincipalPlan(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()
	fake.stockLocations[10] = inventree.StockLocation{PK: 10, Name: "Bin"}
	fake.owners[9] = inventree.Owner{PK: 9, OwnerModel: "user", Name: "jdoe"}
	deps := ownerDeps(fake)

	_, out, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, OwnerID: dvgoutils.Ptr(9), Confirm: true, PlanHash: "bogus"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCalls)

	_, preview, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, OwnerID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	// The location's owner changes before confirmation, so the plan is stale.
	fake.stockLocations[10] = inventree.StockLocation{PK: 10, Name: "Bin", Owner: dvgoutils.Ptr(1)}
	_, staleOut, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, OwnerID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, staleOut.Status)
	assert.Zero(t, fake.updateCalls)

	// Revert the drift and prove the same token still confirms once state matches again.
	fake.stockLocations[10] = inventree.StockLocation{PK: 10, Name: "Bin"}
	_, confirmed, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, OwnerID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, confirmed.Status)
	assert.Equal(t, 1, fake.updateCalls)

	// The single-use token cannot be reused, even for a different (non-no-op) action.
	_, reused, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, reused.Status)
	assert.Equal(t, 1, fake.updateCalls)
}

func TestAssignOwnerVerifiesReadBackAndReportsPartialFailure(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()
	fake.stockLocations[10] = inventree.StockLocation{PK: 10, Name: "Bin"}
	fake.owners[9] = inventree.Owner{PK: 9, OwnerModel: "user", Name: "jdoe"}
	fake.suppressPersist = true
	deps := ownerDeps(fake)

	_, preview, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, OwnerID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	_, out, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, OwnerID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
	assert.NotEmpty(t, out.RecoveryPlan)
}

func TestAssignOwnerReturnsErrorOnDefiniteMutationRejection(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeOwnerClient()
	fake.stockLocations[10] = inventree.StockLocation{PK: 10, Name: "Bin"}
	fake.owners[9] = inventree.Owner{PK: 9, OwnerModel: "user", Name: "jdoe"}
	fake.updateErr = &inventree.APIError{StatusCode: http.StatusBadRequest}
	deps := ownerDeps(fake)

	_, preview, err := assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, OwnerID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	_, _, err = assignOwner(deps)(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: 10, OwnerID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.Error(t, err)
}
