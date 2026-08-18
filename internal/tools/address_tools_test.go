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

type fakeAddressClient struct {
	addresses            map[int]inventree.Address
	addressSearchResults []inventree.Address
	searchErr            error
	lastAddressQuery     inventree.AddressQuery

	orders          map[int]inventree.PurchaseOrderDetail
	updateErr       error
	updateCalls     int
	lastPatch       inventree.PatchFields
	afterUpdate     func()
	suppressPersist bool
}

func newFakeAddressClient() *fakeAddressClient {
	return &fakeAddressClient{
		addresses: map[int]inventree.Address{}, orders: map[int]inventree.PurchaseOrderDetail{},
	}
}

func addressDeps(fake *fakeAddressClient) Dependencies {
	return Dependencies{
		ClientFromContext: func(context.Context) (any, error) { return fake, nil },
		addressPlanStore:  newAddressPlanStore(time.Now, randomStockPlanToken),
	}
}

func (f *fakeAddressClient) GetAddress(_ context.Context, id int) (inventree.Address, error) {
	value, ok := f.addresses[id]
	if !ok {
		return inventree.Address{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeAddressClient) SearchAddressesPage(_ context.Context, query inventree.AddressQuery) (inventree.Page[inventree.Address], error) {
	f.lastAddressQuery = query
	if f.searchErr != nil {
		return inventree.Page[inventree.Address]{}, f.searchErr
	}
	return inventree.Page[inventree.Address]{Count: len(f.addressSearchResults), Results: f.addressSearchResults}, nil
}

func (f *fakeAddressClient) GetPurchaseOrderDetail(_ context.Context, id int) (inventree.PurchaseOrderDetail, error) {
	value, ok := f.orders[id]
	if !ok {
		return inventree.PurchaseOrderDetail{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeAddressClient) UpdatePurchaseOrderDetail(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderDetail, error) {
	f.updateCalls++
	f.lastPatch = fields
	if !f.suppressPersist {
		record := f.orders[id]
		record.Address = patchedOwnerValue(fields["address"])
		f.orders[id] = record
	}
	if f.afterUpdate != nil {
		f.afterUpdate()
	}
	return f.orders[id], f.updateErr
}

func TestSearchAddressesRequiresCompanyID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()

	_, _, err := searchAddresses(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchAddressesInput{})
	require.Error(t, err)

	_, out, err := searchAddresses(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchAddressesInput{CompanyID: 30})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
	assert.Equal(t, 30, fake.lastAddressQuery.CompanyID)
}

func TestSearchAddressesProjectsPrivacySafeFields(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.addressSearchResults = []inventree.Address{
		{PK: 1, Company: 30, Title: "Head Office", Primary: true, PostalCity: "Adelaide", Country: "AU"},
	}

	_, out, err := searchAddresses(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchAddressesInput{CompanyID: 30, Search: "head"})
	require.NoError(t, err)
	require.Equal(t, StatusOK, out.Status)
	require.Len(t, out.Results, 1)
	assert.Equal(t, 1, out.Results[0].PK)
	assert.Equal(t, "Head Office", out.Results[0].Title)
	assert.Equal(t, "Adelaide", out.Results[0].PostalCity)
	assert.Equal(t, "head", fake.lastAddressQuery.Search)
}

func TestSearchAddressesSanitizesInvalidLink(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.addressSearchResults = []inventree.Address{
		{PK: 1, Company: 30, Title: "Head Office", Link: "javascript:alert(1)"},
	}

	_, out, err := searchAddresses(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchAddressesInput{CompanyID: 30})
	require.NoError(t, err)
	require.Equal(t, StatusOK, out.Status)
	require.Len(t, out.Results, 1)
	assert.Empty(t, out.Results[0].Link)
}

func TestGetAddressReturnsNotFoundForMissingRecord(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()

	_, out, err := getAddress(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 99})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)

	fake.addresses[7] = inventree.Address{PK: 7, Company: 30, Title: "Head Office"}
	_, found, err := getAddress(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 7})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, found.Status)
	assert.Equal(t, 7, found.Record.PK)
}

func TestGetAddressRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.addresses[7] = inventree.Address{PK: 999, Company: 30, Title: "Head Office"}

	_, _, err := getAddress(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 7})
	require.Error(t, err)
}

func TestAssignAddressRejectsInvalidPurchaseOrderID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()

	_, out, err := assignAddress(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 0})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestAssignAddressReturnsNotFoundForMissingOrder(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()

	_, out, err := assignAddress(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 40, AddressID: dvgoutils.Ptr(1)})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestAssignAddressRejectsNoopClearAndNoopAssign(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}

	_, out, err := assignAddress(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)

	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}, Address: dvgoutils.Ptr(5)}
	_, out, err = assignAddress(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(5)})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestAssignAddressRejectsUnresolvableAddressID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}

	_, out, err := assignAddress(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(99)})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
	assert.Zero(t, fake.updateCalls)
}

func TestAssignAddressRejectsCompanyMismatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	fake.addresses[1] = inventree.Address{PK: 1, Company: 99, Title: "External Address"}

	_, out, err := assignAddress(addressDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(1)})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
	assert.Zero(t, fake.updateCalls)
}

func TestAssignAddressPreviewConfirmAndClear(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	fake.addresses[9] = inventree.Address{PK: 9, Company: 30, Title: "Head Office"}
	deps := addressDeps(fake)

	_, preview, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(9)})
	require.NoError(t, err)
	require.Equal(t, StatusClarificationRequired, preview.Status)
	require.NotEmpty(t, preview.PlanHash)
	require.NotNil(t, preview.Address)
	assert.Equal(t, 9, preview.Address.PK)
	assert.Zero(t, fake.updateCalls)

	_, confirmed, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	require.Equal(t, StatusOK, confirmed.Status)
	assert.True(t, confirmed.Verified)
	require.NotNil(t, confirmed.AddressID)
	assert.Equal(t, 9, *confirmed.AddressID)
	assert.Equal(t, 1, fake.updateCalls)
	require.Contains(t, fake.lastPatch, "address")
	require.NotNil(t, fake.orders[10].Address)
	assert.Equal(t, 9, *fake.orders[10].Address)

	_, clearPreview, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10})
	require.NoError(t, err)
	require.Equal(t, StatusClarificationRequired, clearPreview.Status)
	require.Nil(t, clearPreview.Address)
	_, cleared, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, Confirm: true, PlanHash: clearPreview.PlanHash})
	require.NoError(t, err)
	require.Equal(t, StatusOK, cleared.Status)
	assert.Nil(t, cleared.AddressID)
	assert.Nil(t, fake.orders[10].Address)
}

func TestAssignAddressRejectsStaleReusedAndCrossPrincipalPlan(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	fake.addresses[9] = inventree.Address{PK: 9, Company: 30, Title: "Head Office"}
	deps := addressDeps(fake)

	_, out, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(9), Confirm: true, PlanHash: "bogus"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCalls)

	_, preview, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	// The order's address changes before confirmation, so the plan is stale.
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}, Address: dvgoutils.Ptr(1)}
	_, staleOut, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, staleOut.Status)
	assert.Zero(t, fake.updateCalls)

	// Revert the drift and prove the same token still confirms once state matches again.
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	_, confirmed, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, confirmed.Status)
	assert.Equal(t, 1, fake.updateCalls)

	// The single-use token cannot be reused, even for a different (non-no-op) action.
	_, reused, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, reused.Status)
	assert.Equal(t, 1, fake.updateCalls)
}

// TestAssignAddressRejectsPlanStaleAfterSupplierChange isolates the
// CompanyID binding from the CurrentAddressID binding: the order's address
// value is left untouched, only its supplier changes, and the previewed
// plan must still be rejected as stale.
func TestAssignAddressRejectsPlanStaleAfterSupplierChange(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}, Address: dvgoutils.Ptr(9)}
	deps := addressDeps(fake)

	_, preview, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10})
	require.NoError(t, err)
	require.Equal(t, StatusClarificationRequired, preview.Status)

	// The order's supplier changes to a different company; its address value is untouched.
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 99}, Address: dvgoutils.Ptr(9)}
	_, staleOut, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, staleOut.Status)
	assert.Zero(t, fake.updateCalls)

	// Revert the supplier and prove the same token confirms once state matches again.
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}, Address: dvgoutils.Ptr(9)}
	_, confirmed, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, confirmed.Status)
	assert.Equal(t, 1, fake.updateCalls)
}

func TestAssignAddressVerifiesReadBackAndReportsPartialFailure(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	fake.addresses[9] = inventree.Address{PK: 9, Company: 30, Title: "Head Office"}
	fake.suppressPersist = true
	deps := addressDeps(fake)

	_, preview, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	_, out, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
	assert.NotEmpty(t, out.RecoveryPlan)
}

func TestAssignAddressReturnsErrorOnDefiniteMutationRejection(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeAddressClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	fake.addresses[9] = inventree.Address{PK: 9, Company: 30, Title: "Head Office"}
	fake.updateErr = &inventree.APIError{StatusCode: http.StatusBadRequest}
	deps := addressDeps(fake)

	_, preview, err := assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	_, _, err = assignAddress(deps)(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: 10, AddressID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.Error(t, err)
}
