package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveCompanyCustomerRoleRejectsNonPositiveAndNonCustomer(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: false}}}

	_, out, err := removeCompanyCustomerRole(companyRoleDeps(fake))(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 0})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)

	_, out, err = removeCompanyCustomerRole(companyRoleDeps(fake))(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestRemoveCompanyCustomerRoleDryRunThenConfirmSucceedsWithoutDependencies(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}}
	deps := companyRoleDeps(fake)

	_, preview, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.NoError(t, err)
	require.Equal(t, StatusClarificationRequired, preview.Status)
	require.NotEmpty(t, preview.PlanHash)
	assert.Zero(t, fake.updateCalls)

	fake.afterUpdate = func() { fake.company.IsCustomer = false }
	_, out, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status)
	require.NotNil(t, out.Record)
	assert.False(t, out.Record.IsCustomer)
	assert.True(t, out.Verified)
	assert.Equal(t, 1, fake.updateCalls)
}

func TestRemoveCompanyCustomerRoleRejectsMissingOrStaleToken(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}}
	deps := companyRoleDeps(fake)

	_, out, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30, Confirm: true, PlanHash: "bogus"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCalls)

	// A plan issued for a different company must not authorize this one.
	_, other, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.NoError(t, err)
	fake.company.PK = 31
	_, out, err = removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 31, Confirm: true, PlanHash: other.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCalls)
}

func TestRemoveCompanyCustomerRoleFailsClosedOnStockDependency(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}, stockCount: 2}
	deps := companyRoleDeps(fake)

	_, out, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Equal(t, 2, out.DependencyStockItems)
	assert.Empty(t, out.PlanHash, "no plan token is issued while a dependency remains")
}

func TestRemoveCompanyCustomerRoleFailsClosedOnSalesOrderDependency(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}, salesOrderCount: 1}
	deps := companyRoleDeps(fake)

	_, out, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Equal(t, 1, out.DependencySalesOrders)
	assert.Empty(t, out.PlanHash)
}

func TestRemoveCompanyCustomerRoleBlockedAttemptLeavesTokenReusableOnceDependencyClears(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}}
	deps := companyRoleDeps(fake)

	_, preview, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.NoError(t, err)
	require.NotEmpty(t, preview.PlanHash)

	// A dependency appears after the preview but before confirmation: the
	// unconditional audit blocks before the token is ever consumed, so the
	// token is not spent by this attempt.
	fake.stockCount = 1
	_, out, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Equal(t, 1, out.DependencyStockItems)
	assert.Zero(t, fake.updateCalls)

	// Once the dependency clears, the same plan_hash still authorizes
	// execution: the token binds company identity and role, not a
	// dependency snapshot.
	fake.stockCount = 0
	fake.afterUpdate = func() { fake.company.IsCustomer = false }
	_, out, err = removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status)
	assert.Equal(t, 1, fake.updateCalls)

	// Retrying now correctly reports the role is already gone rather than
	// attempting a second mutation with the already-spent token.
	_, out, err = removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
	assert.Equal(t, 1, fake.updateCalls, "no second mutation must occur")
}

func TestCompanyRolePlanStoreTokenIsSingleUse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newCompanyRolePlanStore(time.Now, randomStockPlanToken)
	plan := CompanyRolePlan{Action: "remove_customer_role", CompanyID: 30, Name: "Acme", Role: "customer"}

	token, err := store.issue(ctx, plan)
	require.NoError(t, err)
	assert.True(t, store.consume(ctx, token, plan), "the freshly issued token must authorize the exact plan")
	assert.False(t, store.consume(ctx, token, plan), "a consumed token must not authorize a second execution")
}

func TestCompanyRolePlanStoreRejectsMismatchedPlanOrExpiredToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now()
	store := newCompanyRolePlanStore(func() time.Time { return now }, randomStockPlanToken)
	plan := CompanyRolePlan{Action: "remove_customer_role", CompanyID: 30, Name: "Acme", Role: "customer"}
	other := plan
	other.CompanyID = 31

	token, err := store.issue(ctx, plan)
	require.NoError(t, err)
	assert.False(t, store.consume(ctx, token, other), "a token must not authorize a different company's plan")

	staleToken, err := store.issue(ctx, plan)
	require.NoError(t, err)
	now = now.Add(companyRolePlanLifetime + time.Minute)
	assert.False(t, store.consume(ctx, staleToken, plan), "an expired token must not authorize execution")
}

func TestRemoveCompanyCustomerRoleConfirmAuditRunsBeforeTokenConsumption(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}}
	deps := companyRoleDeps(fake)

	_, preview, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.NoError(t, err)
	require.NotEmpty(t, preview.PlanHash)

	// A dependency created after the preview but discovered by the
	// confirm call's own fresh audit must block before the token is ever
	// consumed: exactly one audit call happens per confirm request, and
	// it precedes consumption, so the token survives a blocked attempt.
	fake.stockCount = 1
	fake.stockCountCalls = 0
	_, out, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Equal(t, 1, out.DependencyStockItems)
	assert.Zero(t, fake.updateCalls, "the dependency must be caught before the mutation is ever attempted")
	assert.Equal(t, 1, fake.stockCountCalls, "the confirm path must audit exactly once per request")

	fake.stockCount = 0
	fake.afterUpdate = func() { fake.company.IsCustomer = false }
	_, out, err = removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status, "the still-unspent token must authorize execution once the dependency clears")
	assert.Equal(t, 1, fake.updateCalls)
}

func TestRemoveCompanyCustomerRoleFailsClosedWhenDependencyAuditErrors(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)

	stockAuditFailure := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}, stockErr: errors.New("stock search unavailable")}
	_, _, err := removeCompanyCustomerRole(companyRoleDeps(stockAuditFailure))(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.Error(t, err)
	assert.Zero(t, stockAuditFailure.updateCalls)

	soAuditFailure := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}, salesOrderErr: errors.New("sales-order search unavailable")}
	_, _, err = removeCompanyCustomerRole(companyRoleDeps(soAuditFailure))(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.Error(t, err)
	assert.Zero(t, soAuditFailure.updateCalls)

	// The confirm path must fail closed on an audit error too, without
	// ever consuming the token or attempting the mutation.
	fake := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}}
	deps := companyRoleDeps(fake)
	_, preview, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.NoError(t, err)
	fake.stockErr = errors.New("stock search unavailable")
	_, _, err = removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30, Confirm: true, PlanHash: preview.PlanHash})
	require.Error(t, err)
	assert.Zero(t, fake.updateCalls)
}

func TestRemoveCompanyCustomerRoleRecoversAmbiguousMutationErrorAndRejectsDefinite(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)

	// An ambiguous (non-APIError) mutation failure whose read-back proves
	// the role was actually removed must be reported as recovered success,
	// mirroring update_company's TestCompanyAndSupplierUpdatesRecoverPostPersistResponseLoss.
	recovered := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}, updateErr: errors.New("connection reset")}
	recovered.afterUpdate = func() { recovered.company.IsCustomer = false }
	deps := companyRoleDeps(recovered)
	_, preview, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.NoError(t, err)
	_, out, err := removeCompanyCustomerRole(deps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30, Confirm: true, PlanHash: preview.PlanHash})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status)
	assert.True(t, out.Recovered)
	require.NotNil(t, out.Record)
	assert.False(t, out.Record.IsCustomer)

	// A definite upstream rejection (a real 4xx) must be returned as an
	// actionable error rather than silently treated as ambiguous.
	rejected := &fakeCompanyRole{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", IsCustomer: true}}, updateErr: &inventree.APIError{StatusCode: 400}}
	rejectedDeps := companyRoleDeps(rejected)
	_, preview, err = removeCompanyCustomerRole(rejectedDeps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30})
	require.NoError(t, err)
	_, _, err = removeCompanyCustomerRole(rejectedDeps)(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: 30, Confirm: true, PlanHash: preview.PlanHash})
	require.Error(t, err)
	assert.True(t, rejected.company.IsCustomer, "a definite rejection must leave the role untouched")
}

type fakeCompanyRole struct {
	company         inventree.CompanyDetail
	stockCount      int
	stockCountCalls int
	stockErr        error
	salesOrderCount int
	salesOrderErr   error
	updateCalls     int
	updateErr       error
	afterUpdate     func()
}

func (f *fakeCompanyRole) GetCompanyDetail(context.Context, int) (inventree.CompanyDetail, error) {
	return f.company, nil
}
func (f *fakeCompanyRole) UpdateCompany(_ context.Context, _ int, _ inventree.PatchFields) (inventree.CompanyDetail, error) {
	f.updateCalls++
	if f.afterUpdate != nil {
		f.afterUpdate()
	}
	return f.company, f.updateErr
}
func (f *fakeCompanyRole) SearchStockItemsPage(context.Context, inventree.StockItemQuery) (inventree.StockItemPage, error) {
	f.stockCountCalls++
	if f.stockErr != nil {
		return inventree.StockItemPage{}, f.stockErr
	}
	return inventree.StockItemPage{Count: f.stockCount}, nil
}
func (f *fakeCompanyRole) SearchSalesOrdersPage(context.Context, inventree.SalesOrderQuery) (inventree.SalesOrderPage, error) {
	if f.salesOrderErr != nil {
		return inventree.SalesOrderPage{}, f.salesOrderErr
	}
	return inventree.SalesOrderPage{Count: f.salesOrderCount}, nil
}

func companyRoleDeps(fake *fakeCompanyRole) Dependencies {
	return Dependencies{
		ClientFromContext:    func(context.Context) (any, error) { return fake, nil },
		companyRolePlanStore: newCompanyRolePlanStore(time.Now, randomStockPlanToken),
	}
}
