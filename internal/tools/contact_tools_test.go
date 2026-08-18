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

type fakeContactClient struct {
	contacts             map[int]inventree.Contact
	contactSearchResults []inventree.Contact
	searchErr            error
	lastContactQuery     inventree.ContactQuery

	orders          map[int]inventree.PurchaseOrderDetail
	updateErr       error
	updateCalls     int
	lastPatch       inventree.PatchFields
	afterUpdate     func()
	suppressPersist bool
}

func newFakeContactClient() *fakeContactClient {
	return &fakeContactClient{
		contacts: map[int]inventree.Contact{}, orders: map[int]inventree.PurchaseOrderDetail{},
	}
}

func contactDeps(fake *fakeContactClient) Dependencies {
	return Dependencies{
		ClientFromContext: func(context.Context) (any, error) { return fake, nil },
		contactPlanStore:  newContactPlanStore(time.Now, randomStockPlanToken),
	}
}

func (f *fakeContactClient) GetContact(_ context.Context, id int) (inventree.Contact, error) {
	value, ok := f.contacts[id]
	if !ok {
		return inventree.Contact{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeContactClient) SearchContactsPage(_ context.Context, query inventree.ContactQuery) (inventree.Page[inventree.Contact], error) {
	f.lastContactQuery = query
	if f.searchErr != nil {
		return inventree.Page[inventree.Contact]{}, f.searchErr
	}
	return inventree.Page[inventree.Contact]{Count: len(f.contactSearchResults), Results: f.contactSearchResults}, nil
}

func (f *fakeContactClient) GetPurchaseOrderDetail(_ context.Context, id int) (inventree.PurchaseOrderDetail, error) {
	value, ok := f.orders[id]
	if !ok {
		return inventree.PurchaseOrderDetail{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeContactClient) UpdatePurchaseOrderDetail(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderDetail, error) {
	f.updateCalls++
	f.lastPatch = fields
	if !f.suppressPersist {
		record := f.orders[id]
		record.Contact = patchedOwnerValue(fields["contact"])
		f.orders[id] = record
	}
	if f.afterUpdate != nil {
		f.afterUpdate()
	}
	return f.orders[id], f.updateErr
}

func TestSearchContactsRequiresCompanyID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()

	_, _, err := searchContacts(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchContactsInput{})
	require.Error(t, err)

	_, out, err := searchContacts(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchContactsInput{CompanyID: 30})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
	assert.Equal(t, 30, fake.lastContactQuery.CompanyID)
}

func TestSearchContactsProjectsPrivacySafeFields(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()
	fake.contactSearchResults = []inventree.Contact{
		{PK: 1, Company: 30, CompanyName: "Acme", Name: "Jane Doe", Role: "Purchasing"},
	}

	_, out, err := searchContacts(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchContactsInput{CompanyID: 30, Search: "jane"})
	require.NoError(t, err)
	require.Equal(t, StatusOK, out.Status)
	require.Len(t, out.Results, 1)
	assert.Equal(t, 1, out.Results[0].PK)
	assert.Equal(t, "Jane Doe", out.Results[0].Name)
	assert.Equal(t, "jane", fake.lastContactQuery.Search)
}

func TestGetContactReturnsNotFoundForMissingRecord(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()

	_, out, err := getContact(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 99})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)

	fake.contacts[7] = inventree.Contact{PK: 7, Company: 30, CompanyName: "Acme", Name: "Jane Doe"}
	_, found, err := getContact(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 7})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, found.Status)
	assert.Equal(t, 7, found.Record.PK)
}

func TestGetContactRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()
	fake.contacts[7] = inventree.Contact{PK: 999, Company: 30, CompanyName: "Acme", Name: "Jane Doe"}

	_, _, err := getContact(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 7})
	require.Error(t, err)
}

func TestAssignContactRejectsInvalidPurchaseOrderID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()

	_, out, err := assignContact(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 0})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestAssignContactReturnsNotFoundForMissingOrder(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()

	_, out, err := assignContact(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 40, ContactID: dvgoutils.Ptr(1)})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestAssignContactRejectsNoopClearAndNoopAssign(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}

	_, out, err := assignContact(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)

	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}, Contact: dvgoutils.Ptr(5)}
	_, out, err = assignContact(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(5)})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestAssignContactRejectsUnresolvableContactID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}

	_, out, err := assignContact(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(99)})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
	assert.Zero(t, fake.updateCalls)
}

func TestAssignContactRejectsCompanyMismatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	fake.contacts[1] = inventree.Contact{PK: 1, Company: 99, CompanyName: "Other", Name: "External Contact"}

	_, out, err := assignContact(contactDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(1)})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
	assert.Zero(t, fake.updateCalls)
}

func TestAssignContactPreviewConfirmAndClear(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	fake.contacts[9] = inventree.Contact{PK: 9, Company: 30, CompanyName: "Acme", Name: "Jane Doe"}
	deps := contactDeps(fake)

	_, preview, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(9)})
	require.NoError(t, err)
	require.Equal(t, StatusClarificationRequired, preview.Status)
	require.NotEmpty(t, preview.PlanHash)
	require.NotNil(t, preview.Contact)
	assert.Equal(t, 9, preview.Contact.PK)
	assert.Zero(t, fake.updateCalls)

	_, confirmed, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	require.Equal(t, StatusOK, confirmed.Status)
	assert.True(t, confirmed.Verified)
	require.NotNil(t, confirmed.ContactID)
	assert.Equal(t, 9, *confirmed.ContactID)
	assert.Equal(t, 1, fake.updateCalls)
	require.Contains(t, fake.lastPatch, "contact")
	require.NotNil(t, fake.orders[10].Contact)
	assert.Equal(t, 9, *fake.orders[10].Contact)

	_, clearPreview, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10})
	require.NoError(t, err)
	require.Equal(t, StatusClarificationRequired, clearPreview.Status)
	require.Nil(t, clearPreview.Contact)
	_, cleared, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, Confirm: true, PlanHash: clearPreview.PlanHash})
	require.NoError(t, err)
	require.Equal(t, StatusOK, cleared.Status)
	assert.Nil(t, cleared.ContactID)
	assert.Nil(t, fake.orders[10].Contact)
}

func TestAssignContactRejectsStaleReusedAndCrossPrincipalPlan(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	fake.contacts[9] = inventree.Contact{PK: 9, Company: 30, CompanyName: "Acme", Name: "Jane Doe"}
	deps := contactDeps(fake)

	_, out, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(9), Confirm: true, PlanHash: "bogus"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCalls)

	_, preview, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	// The order's contact changes before confirmation, so the plan is stale.
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}, Contact: dvgoutils.Ptr(1)}
	_, staleOut, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, staleOut.Status)
	assert.Zero(t, fake.updateCalls)

	// Revert the drift and prove the same token still confirms once state matches again.
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	_, confirmed, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, confirmed.Status)
	assert.Equal(t, 1, fake.updateCalls)

	// The single-use token cannot be reused, even for a different (non-no-op) action.
	_, reused, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, reused.Status)
	assert.Equal(t, 1, fake.updateCalls)
}

// TestAssignContactRejectsPlanStaleAfterSupplierChange isolates the
// CompanyID binding from the CurrentContactID binding: the order's contact
// value is left untouched, only its supplier changes, and the previewed
// plan must still be rejected as stale.
func TestAssignContactRejectsPlanStaleAfterSupplierChange(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}, Contact: dvgoutils.Ptr(9)}
	deps := contactDeps(fake)

	_, preview, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10})
	require.NoError(t, err)
	require.Equal(t, StatusClarificationRequired, preview.Status)

	// The order's supplier changes to a different company; its contact value is untouched.
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 99}, Contact: dvgoutils.Ptr(9)}
	_, staleOut, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, staleOut.Status)
	assert.Zero(t, fake.updateCalls)

	// Revert the supplier and prove the same token confirms once state matches again.
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}, Contact: dvgoutils.Ptr(9)}
	_, confirmed, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, confirmed.Status)
	assert.Equal(t, 1, fake.updateCalls)
}

func TestAssignContactVerifiesReadBackAndReportsPartialFailure(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	fake.contacts[9] = inventree.Contact{PK: 9, Company: 30, CompanyName: "Acme", Name: "Jane Doe"}
	fake.suppressPersist = true
	deps := contactDeps(fake)

	_, preview, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	_, out, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
	assert.NotEmpty(t, out.RecoveryPlan)
}

func TestAssignContactReturnsErrorOnDefiniteMutationRejection(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeContactClient()
	fake.orders[10] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 10, Supplier: 30}}
	fake.contacts[9] = inventree.Contact{PK: 9, Company: 30, CompanyName: "Acme", Name: "Jane Doe"}
	fake.updateErr = &inventree.APIError{StatusCode: http.StatusBadRequest}
	deps := contactDeps(fake)

	_, preview, err := assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	_, _, err = assignContact(deps)(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 10, ContactID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.Error(t, err)
}
