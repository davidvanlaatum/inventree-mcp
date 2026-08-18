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

type fakeProjectCodeClient struct {
	projectCodes             map[int]inventree.ProjectCode
	projectCodeSearchResults []inventree.ProjectCode
	searchErr                error
	lastProjectCodeQuery     inventree.ProjectCodeQuery

	purchaseOrders  map[int]inventree.PurchaseOrderDetail
	lines           map[int]inventree.PurchaseOrderLineItem
	extraLines      map[int]inventree.PurchaseOrderExtraLine
	updateErr       error
	updateCalls     int
	lastPatch       inventree.PatchFields
	afterUpdate     func()
	suppressPersist bool
}

func newFakeProjectCodeClient() *fakeProjectCodeClient {
	return &fakeProjectCodeClient{
		projectCodes:   map[int]inventree.ProjectCode{},
		purchaseOrders: map[int]inventree.PurchaseOrderDetail{},
		lines:          map[int]inventree.PurchaseOrderLineItem{},
		extraLines:     map[int]inventree.PurchaseOrderExtraLine{},
	}
}

func projectCodeDeps(fake *fakeProjectCodeClient) Dependencies {
	return Dependencies{
		ClientFromContext:    func(context.Context) (any, error) { return fake, nil },
		projectCodePlanStore: newProjectCodePlanStore(time.Now, randomStockPlanToken),
	}
}

func (f *fakeProjectCodeClient) GetProjectCode(_ context.Context, id int) (inventree.ProjectCode, error) {
	value, ok := f.projectCodes[id]
	if !ok {
		return inventree.ProjectCode{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeProjectCodeClient) SearchProjectCodesPage(_ context.Context, query inventree.ProjectCodeQuery) (inventree.Page[inventree.ProjectCode], error) {
	f.lastProjectCodeQuery = query
	if f.searchErr != nil {
		return inventree.Page[inventree.ProjectCode]{}, f.searchErr
	}
	return inventree.Page[inventree.ProjectCode]{Count: len(f.projectCodeSearchResults), Results: f.projectCodeSearchResults}, nil
}

func (f *fakeProjectCodeClient) GetPurchaseOrderDetail(_ context.Context, id int) (inventree.PurchaseOrderDetail, error) {
	value, ok := f.purchaseOrders[id]
	if !ok {
		return inventree.PurchaseOrderDetail{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeProjectCodeClient) UpdatePurchaseOrderDetail(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderDetail, error) {
	f.updateCalls++
	f.lastPatch = fields
	if !f.suppressPersist {
		record := f.purchaseOrders[id]
		record.ProjectCode = patchedProjectCodeValue(fields["project_code"])
		f.purchaseOrders[id] = record
	}
	if f.afterUpdate != nil {
		f.afterUpdate()
	}
	return f.purchaseOrders[id], f.updateErr
}

func (f *fakeProjectCodeClient) GetPurchaseOrderLine(_ context.Context, id int) (inventree.PurchaseOrderLineItem, error) {
	value, ok := f.lines[id]
	if !ok {
		return inventree.PurchaseOrderLineItem{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeProjectCodeClient) UpdatePurchaseOrderLine(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderLineItem, error) {
	f.updateCalls++
	f.lastPatch = fields
	if !f.suppressPersist {
		record := f.lines[id]
		record.ProjectCode = patchedProjectCodeValue(fields["project_code"])
		f.lines[id] = record
	}
	if f.afterUpdate != nil {
		f.afterUpdate()
	}
	return f.lines[id], f.updateErr
}

func (f *fakeProjectCodeClient) GetPurchaseOrderExtraLine(_ context.Context, id int) (inventree.PurchaseOrderExtraLine, error) {
	value, ok := f.extraLines[id]
	if !ok {
		return inventree.PurchaseOrderExtraLine{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeProjectCodeClient) UpdatePurchaseOrderExtraLine(_ context.Context, id int, fields inventree.PatchFields) (inventree.PurchaseOrderExtraLine, error) {
	f.updateCalls++
	f.lastPatch = fields
	if !f.suppressPersist {
		record := f.extraLines[id]
		record.ProjectCode = patchedProjectCodeValue(fields["project_code"])
		f.extraLines[id] = record
	}
	if f.afterUpdate != nil {
		f.afterUpdate()
	}
	return f.extraLines[id], f.updateErr
}

func patchedProjectCodeValue(value inventree.PatchValue) *int {
	raw := value.Value()
	if raw == nil {
		return nil
	}
	id := raw.(int)
	return &id
}

func TestSearchProjectCodesRequiresQuery(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()

	_, _, err := searchProjectCodes(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchProjectCodesInput{})
	require.Error(t, err)
}

func TestSearchProjectCodesPassesIsActiveFilterToClient(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()
	fake.projectCodeSearchResults = []inventree.ProjectCode{{PK: 1, Code: "PRJ-001", Description: "Widget rollout", Active: true}}

	_, out, err := searchProjectCodes(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchProjectCodesInput{Query: "PRJ", IsActive: dvgoutils.Ptr(true)})
	require.NoError(t, err)
	require.Equal(t, StatusOK, out.Status)
	require.Len(t, out.Results, 1)
	require.NotNil(t, fake.lastProjectCodeQuery.IsActive)
	assert.True(t, *fake.lastProjectCodeQuery.IsActive)
}

func TestGetProjectCodeReturnsNotFoundForMissingRecord(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()

	_, out, err := getProjectCode(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 99})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)

	fake.projectCodes[7] = inventree.ProjectCode{PK: 7, Code: "PRJ-007", Active: true}
	_, found, err := getProjectCode(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 7})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, found.Status)
	assert.Equal(t, 7, found.Record.PK)
}

func TestGetProjectCodeRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()
	fake.projectCodes[10] = inventree.ProjectCode{PK: 11, Code: "WRONG"}

	_, _, err := getProjectCode(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 10})
	require.ErrorContains(t, err, "mismatched project-code identity")
}

func TestAssignProjectCodeRejectsInvalidObjectTypeAndID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()

	_, out, err := assignProjectCode(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: "sales_order", ObjectID: 1})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)

	_, out, err = assignProjectCode(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrder, ObjectID: 0})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestAssignProjectCodeReturnsNotFoundForMissingObject(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()

	_, out, err := assignProjectCode(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderLine, ObjectID: 40})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestAssignProjectCodeRejectsNoopClearAndNoopAssign(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()
	fake.extraLines[10] = inventree.PurchaseOrderExtraLine{PK: 10, Order: 1, Reference: "INV-1"}

	_, out, err := assignProjectCode(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)

	fake.extraLines[10] = inventree.PurchaseOrderExtraLine{PK: 10, Order: 1, Reference: "INV-1", ProjectCode: dvgoutils.Ptr(5)}
	_, out, err = assignProjectCode(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, ProjectCodeID: dvgoutils.Ptr(5)})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestAssignProjectCodeRejectsUnresolvableProjectCodeID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()
	fake.extraLines[10] = inventree.PurchaseOrderExtraLine{PK: 10, Order: 1, Reference: "INV-1"}

	_, out, err := assignProjectCode(projectCodeDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, ProjectCodeID: dvgoutils.Ptr(99)})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
	assert.Zero(t, fake.updateCalls)
}

// TestAssignProjectCodeAcrossObjectMatrix exercises preview-then-confirm
// assignment and clearing across every F-S50 object type, proving the
// project_code PATCH field name and cross-object consistency of the guarded
// plan-token flow.
func TestAssignProjectCodeAcrossObjectMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		objectType string
		seed       func(*fakeProjectCodeClient)
		current    func(*fakeProjectCodeClient) *int
	}{
		{
			objectType: ProjectCodeObjectPurchaseOrder,
			seed: func(f *fakeProjectCodeClient) {
				f.purchaseOrders[1] = inventree.PurchaseOrderDetail{PurchaseOrder: inventree.PurchaseOrder{PK: 1, Reference: "PO-1"}}
			},
			current: func(f *fakeProjectCodeClient) *int { return f.purchaseOrders[1].ProjectCode },
		},
		{
			objectType: ProjectCodeObjectPurchaseOrderLine,
			seed:       func(f *fakeProjectCodeClient) { f.lines[2] = inventree.PurchaseOrderLineItem{PK: 2, Order: 1, Part: 5} },
			current:    func(f *fakeProjectCodeClient) *int { return f.lines[2].ProjectCode },
		},
		{
			objectType: ProjectCodeObjectPurchaseOrderExtraLine,
			seed: func(f *fakeProjectCodeClient) {
				f.extraLines[3] = inventree.PurchaseOrderExtraLine{PK: 3, Order: 1, Reference: "INV-1"}
			},
			current: func(f *fakeProjectCodeClient) *int { return f.extraLines[3].ProjectCode },
		},
	}
	objectID := map[string]int{ProjectCodeObjectPurchaseOrder: 1, ProjectCodeObjectPurchaseOrderLine: 2, ProjectCodeObjectPurchaseOrderExtraLine: 3}

	for _, tc := range cases {
		t.Run(tc.objectType, func(t *testing.T) {
			t.Parallel()
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeProjectCodeClient()
			tc.seed(fake)
			fake.projectCodes[9] = inventree.ProjectCode{PK: 9, Code: "PRJ-009", Active: true}
			deps := projectCodeDeps(fake)
			id := objectID[tc.objectType]

			_, preview, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: tc.objectType, ObjectID: id, ProjectCodeID: dvgoutils.Ptr(9)})
			require.NoError(t, err)
			require.Equal(t, StatusClarificationRequired, preview.Status)
			require.NotEmpty(t, preview.PlanHash)
			require.NotNil(t, preview.ProjectCode)
			assert.Equal(t, 9, preview.ProjectCode.PK)
			assert.Zero(t, fake.updateCalls)

			_, confirmed, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: tc.objectType, ObjectID: id, ProjectCodeID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
			require.NoError(t, err)
			require.Equal(t, StatusOK, confirmed.Status)
			assert.True(t, confirmed.Verified)
			require.NotNil(t, confirmed.ProjectCodeID)
			assert.Equal(t, 9, *confirmed.ProjectCodeID)
			assert.Equal(t, 1, fake.updateCalls)
			require.Contains(t, fake.lastPatch, "project_code")
			if tc.objectType == ProjectCodeObjectPurchaseOrderLine {
				// Pinned InvenTree 1.5.0/API 530 rejects a project_code-only
				// PATCH on a line; part/order must be re-supplied. quantity is
				// deliberately NOT resupplied (confirmed unnecessary live) to
				// avoid clobbering a concurrent quantity edit.
				assert.Contains(t, fake.lastPatch, "part")
				assert.Contains(t, fake.lastPatch, "order")
				assert.NotContains(t, fake.lastPatch, "quantity")
			}
			require.NotNil(t, tc.current(fake))
			assert.Equal(t, 9, *tc.current(fake))

			// Clear it back out through the same guarded flow.
			_, clearPreview, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: tc.objectType, ObjectID: id})
			require.NoError(t, err)
			require.Equal(t, StatusClarificationRequired, clearPreview.Status)
			require.Nil(t, clearPreview.ProjectCode)
			_, cleared, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: tc.objectType, ObjectID: id, Confirm: true, PlanHash: clearPreview.PlanHash})
			require.NoError(t, err)
			require.Equal(t, StatusOK, cleared.Status)
			assert.Nil(t, cleared.ProjectCodeID)
			assert.Nil(t, tc.current(fake))
		})
	}
}

func TestAssignProjectCodeRejectsStaleReusedAndCrossPrincipalPlan(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()
	fake.extraLines[10] = inventree.PurchaseOrderExtraLine{PK: 10, Order: 1, Reference: "INV-1"}
	fake.projectCodes[9] = inventree.ProjectCode{PK: 9, Code: "PRJ-009"}
	deps := projectCodeDeps(fake)

	_, out, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, ProjectCodeID: dvgoutils.Ptr(9), Confirm: true, PlanHash: "bogus"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCalls)

	_, preview, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, ProjectCodeID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	// The extra line's project code changes before confirmation, so the plan is stale.
	fake.extraLines[10] = inventree.PurchaseOrderExtraLine{PK: 10, Order: 1, Reference: "INV-1", ProjectCode: dvgoutils.Ptr(1)}
	_, staleOut, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, ProjectCodeID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, staleOut.Status)
	assert.Zero(t, fake.updateCalls)

	// Revert the drift and prove the same token still confirms once state matches again.
	fake.extraLines[10] = inventree.PurchaseOrderExtraLine{PK: 10, Order: 1, Reference: "INV-1"}
	_, confirmed, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, ProjectCodeID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, confirmed.Status)
	assert.Equal(t, 1, fake.updateCalls)

	// The single-use token cannot be reused, even for a different (non-no-op) action.
	_, reused, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, reused.Status)
	assert.Equal(t, 1, fake.updateCalls)
}

func TestAssignProjectCodeVerifiesReadBackAndReportsPartialFailure(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()
	fake.extraLines[10] = inventree.PurchaseOrderExtraLine{PK: 10, Order: 1, Reference: "INV-1"}
	fake.projectCodes[9] = inventree.ProjectCode{PK: 9, Code: "PRJ-009"}
	fake.suppressPersist = true
	deps := projectCodeDeps(fake)

	_, preview, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, ProjectCodeID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	_, out, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, ProjectCodeID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
	assert.NotEmpty(t, out.RecoveryPlan)
}

func TestAssignProjectCodeReturnsErrorOnDefiniteMutationRejection(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeProjectCodeClient()
	fake.extraLines[10] = inventree.PurchaseOrderExtraLine{PK: 10, Order: 1, Reference: "INV-1"}
	fake.projectCodes[9] = inventree.ProjectCode{PK: 9, Code: "PRJ-009"}
	fake.updateErr = &inventree.APIError{StatusCode: http.StatusBadRequest}
	deps := projectCodeDeps(fake)

	_, preview, err := assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, ProjectCodeID: dvgoutils.Ptr(9)})
	require.NoError(t, err)

	_, _, err = assignProjectCode(deps)(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderExtraLine, ObjectID: 10, ProjectCodeID: dvgoutils.Ptr(9), Confirm: true, PlanHash: preview.PlanHash})
	require.Error(t, err)
}
