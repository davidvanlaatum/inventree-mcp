package tools

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test fakes
// ---------------------------------------------------------------------------

type bulkFakeCompanyAdmin struct {
	mu                         sync.Mutex
	parts                      map[int]inventree.Part
	companies                  map[int]inventree.CompanyDetail
	suppliers                  map[int]inventree.SupplierPartDetail
	manufacturers              map[int]inventree.ManufacturerPartDetail
	companyUpdateErr           map[int]error
	supplierUpdateErr          map[int]error
	manufacturerUpdateErr      map[int]error
	companyApplyBeforeErr      map[int]bool
	supplierApplyBeforeErr     map[int]bool
	manufacturerApplyBeforeErr map[int]bool
	companyUpdateCalls         int
	supplierUpdateCalls        int
	manufacturerUpdateCalls    int
}

func newBulkFakeCompanyAdmin() *bulkFakeCompanyAdmin {
	return &bulkFakeCompanyAdmin{
		parts:                      map[int]inventree.Part{},
		companies:                  map[int]inventree.CompanyDetail{},
		suppliers:                  map[int]inventree.SupplierPartDetail{},
		manufacturers:              map[int]inventree.ManufacturerPartDetail{},
		companyUpdateErr:           map[int]error{},
		supplierUpdateErr:          map[int]error{},
		manufacturerUpdateErr:      map[int]error{},
		companyApplyBeforeErr:      map[int]bool{},
		supplierApplyBeforeErr:     map[int]bool{},
		manufacturerApplyBeforeErr: map[int]bool{},
	}
}

func (f *bulkFakeCompanyAdmin) GetPart(_ context.Context, id int) (inventree.Part, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	part, ok := f.parts[id]
	if !ok {
		return inventree.Part{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return part, nil
}
func (f *bulkFakeCompanyAdmin) GetCompanyDetail(_ context.Context, id int) (inventree.CompanyDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	company, ok := f.companies[id]
	if !ok {
		return inventree.CompanyDetail{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return company, nil
}
func (f *bulkFakeCompanyAdmin) SearchCompaniesPage(context.Context, inventree.SearchQuery) (inventree.CompanyPage, error) {
	return inventree.CompanyPage{}, nil
}
func (f *bulkFakeCompanyAdmin) GetSupplierPartDetail(_ context.Context, id int) (inventree.SupplierPartDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	supplier, ok := f.suppliers[id]
	if !ok {
		return inventree.SupplierPartDetail{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return supplier, nil
}
func (f *bulkFakeCompanyAdmin) SearchSupplierPartsPage(_ context.Context, query inventree.SupplierPartQuery) (inventree.SupplierPartPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var results []inventree.SupplierPartDetail
	for _, supplier := range f.suppliers {
		if query.Company != 0 && supplier.Supplier != query.Company {
			continue
		}
		results = append(results, supplier)
	}
	return inventree.SupplierPartPage{Results: results, Count: len(results)}, nil
}
func (f *bulkFakeCompanyAdmin) GetManufacturerPartDetail(_ context.Context, id int) (inventree.ManufacturerPartDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	manufacturer, ok := f.manufacturers[id]
	if !ok {
		return inventree.ManufacturerPartDetail{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return manufacturer, nil
}
func (f *bulkFakeCompanyAdmin) SearchManufacturerPartsPage(_ context.Context, query inventree.ManufacturerPartQuery) (inventree.ManufacturerPartPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var results []inventree.ManufacturerPartDetail
	for _, manufacturer := range f.manufacturers {
		if query.Manufacturer != 0 && manufacturer.Manufacturer != query.Manufacturer {
			continue
		}
		results = append(results, manufacturer)
	}
	return inventree.ManufacturerPartPage{Results: results, Count: len(results)}, nil
}
func (f *bulkFakeCompanyAdmin) UpdateCompany(_ context.Context, id int, fields inventree.PatchFields) (inventree.CompanyDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.companyUpdateCalls++
	company := f.companies[id]
	applyPatchToCompany(&company, fields)
	if err := f.companyUpdateErr[id]; err != nil {
		if f.companyApplyBeforeErr[id] {
			f.companies[id] = company
		}
		return inventree.CompanyDetail{}, err
	}
	f.companies[id] = company
	return company, nil
}
func (f *bulkFakeCompanyAdmin) UpdateSupplierPart(_ context.Context, id int, fields inventree.PatchFields) (inventree.SupplierPartDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.supplierUpdateCalls++
	supplier := f.suppliers[id]
	applyPatchToSupplierPart(&supplier, fields)
	if err := f.supplierUpdateErr[id]; err != nil {
		if f.supplierApplyBeforeErr[id] {
			f.suppliers[id] = supplier
		}
		return inventree.SupplierPartDetail{}, err
	}
	f.suppliers[id] = supplier
	return supplier, nil
}
func (f *bulkFakeCompanyAdmin) UpdateManufacturerPart(_ context.Context, id int, fields inventree.PatchFields) (inventree.ManufacturerPartDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.manufacturerUpdateCalls++
	manufacturer := f.manufacturers[id]
	applyPatchToManufacturerPart(&manufacturer, fields)
	if err := f.manufacturerUpdateErr[id]; err != nil {
		if f.manufacturerApplyBeforeErr[id] {
			f.manufacturers[id] = manufacturer
		}
		return inventree.ManufacturerPartDetail{}, err
	}
	f.manufacturers[id] = manufacturer
	return manufacturer, nil
}

func applyPatchToCompany(company *inventree.CompanyDetail, fields inventree.PatchFields) {
	if field, ok := fields["name"]; ok {
		company.Name = field.Value().(string)
	}
	if field, ok := fields["active"]; ok {
		company.Active = field.Value().(bool)
	}
	if field, ok := fields["is_supplier"]; ok {
		company.IsSupplier = field.Value().(bool)
	}
	if field, ok := fields["is_manufacturer"]; ok {
		company.IsManufacturer = field.Value().(bool)
	}
}

func applyPatchToSupplierPart(supplier *inventree.SupplierPartDetail, fields inventree.PatchFields) {
	if field, ok := fields["SKU"]; ok {
		supplier.SKU = field.Value().(string)
	}
	if field, ok := fields["active"]; ok {
		supplier.Active = field.Value().(bool)
	}
}

func applyPatchToManufacturerPart(manufacturer *inventree.ManufacturerPartDetail, fields inventree.PatchFields) {
	if field, ok := fields["MPN"]; ok {
		mpn := field.Value().(string)
		manufacturer.MPN = &mpn
	}
}

type bulkFakeCategoryAdmin struct {
	mu             sync.Mutex
	categories     map[int]inventree.Category
	locations      map[int]inventree.StockLocation
	updateErr      map[int]error
	applyBeforeErr map[int]bool
	updateCalls    int
}

func newBulkFakeCategoryAdmin() *bulkFakeCategoryAdmin {
	return &bulkFakeCategoryAdmin{categories: map[int]inventree.Category{}, locations: map[int]inventree.StockLocation{}, updateErr: map[int]error{}, applyBeforeErr: map[int]bool{}}
}

func (f *bulkFakeCategoryAdmin) GetPartCategory(_ context.Context, id int) (inventree.Category, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	category, ok := f.categories[id]
	if !ok {
		return inventree.Category{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return category, nil
}
func (f *bulkFakeCategoryAdmin) SearchPartCategoriesPage(_ context.Context, query inventree.CategoryQuery) (inventree.CategoryPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var results []inventree.Category
	for _, category := range f.categories {
		if query.Parent != nil && (category.Parent == nil || *category.Parent != *query.Parent) {
			continue
		}
		results = append(results, category)
	}
	return inventree.CategoryPage{Results: results, Count: len(results)}, nil
}
func (f *bulkFakeCategoryAdmin) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	location, ok := f.locations[id]
	if !ok {
		return inventree.StockLocation{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return location, nil
}
func (f *bulkFakeCategoryAdmin) SearchPartsPage(context.Context, inventree.PartQuery) (inventree.PartPage, error) {
	return inventree.PartPage{}, nil
}
func (f *bulkFakeCategoryAdmin) CreatePartCategory(context.Context, inventree.CategoryCreate) (inventree.Category, error) {
	return inventree.Category{}, errors.New("not implemented")
}
func (f *bulkFakeCategoryAdmin) UpdatePartCategory(_ context.Context, id int, fields inventree.PatchFields) (inventree.Category, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalls++
	category := f.categories[id]
	if field, ok := fields["name"]; ok {
		category.Name = field.Value().(string)
	}
	if field, ok := fields["description"]; ok {
		category.Description = field.Value().(string)
	}
	if field, ok := fields["default_location"]; ok {
		category.DefaultLocation = nullableIntPatchValue(field)
	}
	if field, ok := fields["default_keywords"]; ok {
		category.DefaultKeywords = nullableStringPatchValue(field)
	}
	if field, ok := fields["icon"]; ok {
		category.Icon = nullableStringPatchValue(field)
	}
	if err := f.updateErr[id]; err != nil {
		if f.applyBeforeErr[id] {
			f.categories[id] = category
		}
		return inventree.Category{}, err
	}
	f.categories[id] = category
	return category, nil
}

func nullableIntPatchValue(field inventree.PatchValue) *int {
	value, ok := field.Value().(int)
	if !ok {
		return nil
	}
	return &value
}

func nullableStringPatchValue(field inventree.PatchValue) *string {
	value, ok := field.Value().(string)
	if !ok {
		return nil
	}
	return &value
}

type bulkFakePartWrite struct {
	mu             sync.Mutex
	parts          map[int]inventree.PartDetail
	updateErr      map[int]error
	applyBeforeErr map[int]bool
	updateCalls    int

	// updateDelay and the in-flight counters below let a test observe the
	// actual concurrency batch.Execute grants UpdatePart, independent of
	// this fake's own mu (which only guards the parts map, not concurrency).
	updateDelay        time.Duration
	inFlightUpdates    atomic.Int64
	maxInFlightUpdates atomic.Int64
}

func newBulkFakePartWrite() *bulkFakePartWrite {
	return &bulkFakePartWrite{parts: map[int]inventree.PartDetail{}, updateErr: map[int]error{}, applyBeforeErr: map[int]bool{}}
}

func (f *bulkFakePartWrite) SearchParts(context.Context, inventree.SearchQuery) ([]inventree.Part, error) {
	return nil, nil
}
func (f *bulkFakePartWrite) GetPartDetail(_ context.Context, id int) (inventree.PartDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	part, ok := f.parts[id]
	if !ok {
		return inventree.PartDetail{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return part, nil
}
func (f *bulkFakePartWrite) CreatePart(context.Context, inventree.PartCreate) (inventree.Part, error) {
	return inventree.Part{}, errors.New("not implemented")
}
func (f *bulkFakePartWrite) UpdatePart(_ context.Context, id int, fields inventree.PatchFields) (inventree.Part, error) {
	current := f.inFlightUpdates.Add(1)
	defer f.inFlightUpdates.Add(-1)
	for {
		previous := f.maxInFlightUpdates.Load()
		if current <= previous || f.maxInFlightUpdates.CompareAndSwap(previous, current) {
			break
		}
	}
	if f.updateDelay > 0 {
		time.Sleep(f.updateDelay)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalls++
	part := f.parts[id]
	if field, ok := fields["name"]; ok {
		part.Name = field.Value().(string)
	}
	if field, ok := fields["active"]; ok {
		part.Active = field.Value().(bool)
	}
	if err := f.updateErr[id]; err != nil {
		if f.applyBeforeErr[id] {
			// Simulate a write that landed upstream before the response was lost.
			f.parts[id] = part
		}
		return inventree.Part{}, err
	}
	f.parts[id] = part
	return inventree.Part{PK: part.PK, Name: part.Name}, nil
}

// bulkResultsByID indexes a bulk tool's item results by ID for assertions;
// shared by this file's unit tests and milestone_integration_test.go's
// "bulk_catalog_and_company_updates" live subtests.
func bulkResultsByID[View any](items []BulkUpdateItemResult[View]) map[int]BulkUpdateItemResult[View] {
	byID := make(map[int]BulkUpdateItemResult[View], len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	return byID
}

// ---------------------------------------------------------------------------
// Dependency helpers
// ---------------------------------------------------------------------------

func testCatalogBulkDeps(client any) Dependencies {
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }, BulkMaxItems: 25, BulkConcurrency: 4}
	deps.partBulkPlanStore = mustBulkStore(batch.Options[partBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p partBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	deps.companyBulkPlanStore = mustBulkStore(batch.Options[companyBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p companyBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	deps.categoryBulkPlanStore = mustBulkStore(batch.Options[categoryBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p categoryBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	deps.supplierPartBulkPlanStore = mustBulkStore(batch.Options[supplierPartBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p supplierPartBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	deps.manufacturerPartBulkPlanStore = mustBulkStore(batch.Options[manufacturerPartBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p manufacturerPartBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	return deps
}

// ---------------------------------------------------------------------------
// Plan-build exclusion tests
// ---------------------------------------------------------------------------

func TestBuildCompanyBulkPlanRejectsExplicitRoleRemoval(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.companies[1] = inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "Acme", IsSupplier: true}}

	plan := buildCompanyBulkPlan(ctx, client, []BulkUpdateCompanyItem{{ID: 1, IsSupplier: dvgoutils.Ptr(false)}})
	require.Len(t, plan.Items, 1)
	assert.NotEmpty(t, plan.Items[0].FailReason)
	assert.Contains(t, plan.Items[0].FailReason, "cannot remove")
}

func TestBuildCompanyBulkPlanRejectsUnknownID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()

	plan := buildCompanyBulkPlan(ctx, client, []BulkUpdateCompanyItem{{ID: 99, Name: dvgoutils.Ptr("New Name")}})
	require.Len(t, plan.Items, 1)
	assert.NotEmpty(t, plan.Items[0].FailReason)
}

func TestBuildCompanyBulkPlanRejectsDuplicateIDWithinBatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.companies[1] = inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "Old"}}

	plan := buildCompanyBulkPlan(ctx, client, []BulkUpdateCompanyItem{
		{ID: 1, Description: dvgoutils.Ptr("first")},
		{ID: 1, Description: dvgoutils.Ptr("second")},
	})
	r.Len(plan.Items, 2)
	a.NotEmpty(plan.Items[0].FailReason)
	a.NotEmpty(plan.Items[1].FailReason)
	a.Contains(plan.Items[0].FailReason, "once")
	a.Contains(plan.Items[1].FailReason, "once")
}

func TestBuildCategoryBulkPlanRejectsSameParentDuplicateName(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCategoryAdmin()
	client.categories[1] = inventree.Category{PK: 1, Name: "Resistors"}
	client.categories[2] = inventree.Category{PK: 2, Name: "Capacitors"}

	plan := buildCategoryBulkPlan(ctx, client, []BulkUpdatePartCategoryItem{{ID: 2, Name: dvgoutils.Ptr("Resistors")}})
	require.Len(t, plan.Items, 1)
	assert.NotEmpty(t, plan.Items[0].FailReason)
	assert.Contains(t, plan.Items[0].FailReason, "collides")
}

func TestBuildSupplierPartBulkPlanRejectsDuplicateSKU(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.parts[10] = inventree.Part{PK: 10}
	client.companies[20] = inventree.CompanyDetail{Company: inventree.Company{PK: 20, IsSupplier: true}}
	client.suppliers[1] = inventree.SupplierPartDetail{PK: 1, Part: 10, Supplier: 20, SKU: "SKU-A"}
	client.suppliers[2] = inventree.SupplierPartDetail{PK: 2, Part: 10, Supplier: 20, SKU: "SKU-B"}

	plan := buildSupplierPartBulkPlan(ctx, client, []BulkUpdateSupplierPartItem{{ID: 2, SKU: dvgoutils.Ptr("SKU-A")}})
	require.Len(t, plan.Items, 1)
	assert.NotEmpty(t, plan.Items[0].FailReason)
	assert.Contains(t, plan.Items[0].FailReason, "collides")
}

func TestBuildManufacturerPartBulkPlanRejectsDuplicateMPN(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.parts[10] = inventree.Part{PK: 10}
	client.companies[30] = inventree.CompanyDetail{Company: inventree.Company{PK: 30, IsManufacturer: true}}
	mpnA, mpnB := "MPN-A", "MPN-B"
	client.manufacturers[1] = inventree.ManufacturerPartDetail{PK: 1, Part: 10, Manufacturer: 30, MPN: &mpnA}
	client.manufacturers[2] = inventree.ManufacturerPartDetail{PK: 2, Part: 10, Manufacturer: 30, MPN: &mpnB}

	plan := buildManufacturerPartBulkPlan(ctx, client, []BulkUpdateManufacturerPartItem{{ID: 2, MPN: dvgoutils.Ptr("MPN-A")}})
	require.Len(t, plan.Items, 1)
	assert.NotEmpty(t, plan.Items[0].FailReason)
	assert.Contains(t, plan.Items[0].FailReason, "collides")
}

func TestBuildPartBulkPlanRejectsInvalidCategoryID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePartWrite()
	client.parts[1] = inventree.PartDetail{PK: 1, Name: "Widget"}

	plan := buildPartBulkPlan(ctx, client, []BulkUpdatePartItem{{ID: 1, CategoryID: dvgoutils.Ptr(-1)}})
	require.Len(t, plan.Items, 1)
	assert.NotEmpty(t, plan.Items[0].FailReason)
}

// ---------------------------------------------------------------------------
// Full handler flow: dry_run -> confirm, mixed outcomes, digest staleness
// ---------------------------------------------------------------------------

func TestBulkUpdateCompaniesDryRunThenConfirmApplies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.companies[1] = inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "Old Name", Currency: "AUD"}}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateCompanies(deps)

	items := []BulkUpdateCompanyItem{{ID: 1, Name: dvgoutils.Ptr("New Name")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: items, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, dryOut.Status)
	r.Len(dryOut.Items, 1)
	a.Equal(bulkOutcomePlanned, dryOut.Items[0].Outcome)
	a.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, confirmOut.Status)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("New Name", confirmOut.Items[0].Record.Name)
	a.Equal(1, client.companyUpdateCalls)
}

func TestBulkUpdateCompaniesRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.companies[1] = inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "Old Name"}}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateCompanies(deps)

	items := []BulkUpdateCompanyItem{{ID: 1, Name: dvgoutils.Ptr("New Name")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: items, DryRun: true})
	r.NoError(err)

	// State drifts after the dry run but before confirm: the digest embedded
	// in the freshly rebuilt plan at confirm time no longer matches, so the
	// stored token must be rejected rather than silently applied.
	client.companies[1] = inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "Someone Else Changed It"}}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.companyUpdateCalls)
}

func TestBulkUpdatePartsRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePartWrite()
	client.parts[1] = inventree.PartDetail{PK: 1, Name: "Old Name"}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateParts(deps)

	items := []BulkUpdatePartItem{{ID: 1, Name: dvgoutils.Ptr("New Name")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, DryRun: true})
	r.NoError(err)

	client.parts[1] = inventree.PartDetail{PK: 1, Name: "Someone Else Changed It"}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.updateCalls)
}

func TestBulkUpdatePartCategoriesRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCategoryAdmin()
	client.categories[1] = inventree.Category{PK: 1, Name: "Old Name"}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdatePartCategories(deps)

	items := []BulkUpdatePartCategoryItem{{ID: 1, Name: dvgoutils.Ptr("New Name")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartCategoriesInput{Items: items, DryRun: true})
	r.NoError(err)

	client.categories[1] = inventree.Category{PK: 1, Name: "Someone Else Changed It"}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartCategoriesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.updateCalls)
}

func TestBulkUpdateSupplierPartsRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.parts[10] = inventree.Part{PK: 10}
	client.companies[20] = inventree.CompanyDetail{Company: inventree.Company{PK: 20, IsSupplier: true}}
	client.suppliers[1] = inventree.SupplierPartDetail{PK: 1, Part: 10, Supplier: 20, SKU: "SKU-OLD"}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateSupplierParts(deps)

	items := []BulkUpdateSupplierPartItem{{ID: 1, SKU: dvgoutils.Ptr("SKU-NEW")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateSupplierPartsInput{Items: items, DryRun: true})
	r.NoError(err)

	client.suppliers[1] = inventree.SupplierPartDetail{PK: 1, Part: 10, Supplier: 20, SKU: "SKU-CHANGED"}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateSupplierPartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.supplierUpdateCalls)
}

func TestBulkUpdateManufacturerPartsRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.parts[10] = inventree.Part{PK: 10}
	client.companies[30] = inventree.CompanyDetail{Company: inventree.Company{PK: 30, IsManufacturer: true}}
	oldMPN := "MPN-OLD"
	client.manufacturers[1] = inventree.ManufacturerPartDetail{PK: 1, Part: 10, Manufacturer: 30, MPN: &oldMPN}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateManufacturerParts(deps)

	items := []BulkUpdateManufacturerPartItem{{ID: 1, MPN: dvgoutils.Ptr("MPN-NEW")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateManufacturerPartsInput{Items: items, DryRun: true})
	r.NoError(err)

	changedMPN := "MPN-CHANGED"
	client.manufacturers[1] = inventree.ManufacturerPartDetail{PK: 1, Part: 10, Manufacturer: 30, MPN: &changedMPN}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateManufacturerPartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.manufacturerUpdateCalls)
}

func TestBulkUpdateCompaniesMixedOutcomesAreIndependent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.companies[1] = inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "Applies"}}
	client.companies[2] = inventree.CompanyDetail{Company: inventree.Company{PK: 2, Name: "AlreadyThere"}}
	client.companies[3] = inventree.CompanyDetail{Company: inventree.Company{PK: 3, Name: "Ambiguous"}}
	client.companyUpdateErr[3] = &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}
	// no client.companies[4]: this item fails plan-build entirely (unknown ID).
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateCompanies(deps)

	items := []BulkUpdateCompanyItem{
		{ID: 1, Name: dvgoutils.Ptr("Applied Name")},
		{ID: 2, Name: dvgoutils.Ptr("AlreadyThere")}, // no-op: already at target state
		{ID: 3, Name: dvgoutils.Ptr("Would Not Apply Cleanly")},
		{ID: 4, Name: dvgoutils.Ptr("Unknown Company")},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, confirmOut.Status)
	r.Len(confirmOut.Items, 4)
	byID := bulkResultsByID(confirmOut.Items)
	a.Equal(string(batch.OutcomeApplied), byID[1].Outcome)
	a.Equal(string(batch.OutcomeSkipped), byID[2].Outcome)
	// Any error returned from Mutate is reported as ambiguous, never failed:
	// the write was attempted, so whether it partially applied is unknown.
	a.Equal(string(batch.OutcomeAmbiguous), byID[3].Outcome)
	// A plan-build failure (item 4, unknown ID) is caught by Preflight before
	// any write is attempted, so it is reported as failed, not ambiguous.
	a.Equal(string(batch.OutcomeFailed), byID[4].Outcome)
	// Every outcome is independent: item 2's skip, item 3's ambiguous
	// mutation, and item 4's plan-build failure must not affect item 1's
	// success or each other. UpdateCompany is called exactly for items 1
	// and 3 -- never for the skipped no-op or the never-planned unknown ID.
	a.Equal(2, client.companyUpdateCalls)
}

func TestBulkUpdatePartsRejectsEmptyAndOversizedBatches(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePartWrite()
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateParts(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)

	tooMany := make([]BulkUpdatePartItem, bulkUpdateMaxItems+1)
	for i := range tooMany {
		tooMany[i] = BulkUpdatePartItem{ID: i + 1}
	}
	_, out, err = handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: tooMany, DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestBulkUpdatePartsAcceptsExactlyMaxItems(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePartWrite()
	maxItems := make([]BulkUpdatePartItem, bulkUpdateMaxItems)
	for i := range maxItems {
		id := i + 1
		client.parts[id] = inventree.PartDetail{PK: id, Name: "Widget"}
		maxItems[i] = BulkUpdatePartItem{ID: id, Description: dvgoutils.Ptr("bulk description")}
	}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateParts(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: maxItems, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	r.Len(out.Items, bulkUpdateMaxItems)
	for _, item := range out.Items {
		a.Equal(bulkOutcomePlanned, item.Outcome)
	}
	a.NotEmpty(out.PlanHash)
}

// TestBulkUpdatePartsHonorsConfiguredMaxItems proves the item-count limit is
// genuinely read from Dependencies.BulkMaxItems (F-S81's operator-configurable
// setting), not the fixed 25 every other test in this file exercises via
// testCatalogBulkDeps' default.
func TestBulkUpdatePartsHonorsConfiguredMaxItems(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePartWrite()
	deps := testCatalogBulkDeps(client)
	deps.BulkMaxItems = 3
	handler := bulkUpdateParts(deps)

	withinLimit := []BulkUpdatePartItem{{ID: 1}, {ID: 2}, {ID: 3}}
	for _, item := range withinLimit {
		client.parts[item.ID] = inventree.PartDetail{PK: item.ID, Name: "Widget"}
	}
	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{
		Items: []BulkUpdatePartItem{
			{ID: 1, Description: dvgoutils.Ptr("d")},
			{ID: 2, Description: dvgoutils.Ptr("d")},
			{ID: 3, Description: dvgoutils.Ptr("d")},
		},
		DryRun: true,
	})
	r.NoError(err)
	a.Equal(StatusOK, out.Status, "a 3-item batch must be accepted when BulkMaxItems is configured to 3")

	_, out, err = handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{
		Items: []BulkUpdatePartItem{
			{ID: 1, Description: dvgoutils.Ptr("d")},
			{ID: 2, Description: dvgoutils.Ptr("d")},
			{ID: 3, Description: dvgoutils.Ptr("d")},
			{ID: 4, Description: dvgoutils.Ptr("d")},
		},
		DryRun: true,
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status, "a 4-item batch must be rejected when BulkMaxItems is configured to 3, well below the 25 default")
}

// TestBulkUpdatePartsHonorsConfiguredConcurrency proves worker concurrency is
// genuinely read from Dependencies.BulkConcurrency, by configuring it to 1
// and asserting the fake client never sees more than one in-flight mutation
// at a time even though the batch has multiple items.
func TestBulkUpdatePartsHonorsConfiguredConcurrency(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePartWrite()
	for _, id := range []int{1, 2, 3} {
		client.parts[id] = inventree.PartDetail{PK: id, Name: "Widget", Active: true}
	}
	client.updateDelay = 20 * time.Millisecond
	deps := testCatalogBulkDeps(client)
	deps.BulkConcurrency = 1
	handler := bulkUpdateParts(deps)

	items := []BulkUpdatePartItem{
		{ID: 1, Active: dvgoutils.Ptr(false)},
		{ID: 2, Active: dvgoutils.Ptr(false)},
		{ID: 3, Active: dvgoutils.Ptr(false)},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 3)
	r.LessOrEqual(client.maxInFlightUpdates.Load(), int64(1), "BulkConcurrency:1 must bound the fake client to one in-flight mutation at a time")
}

func TestBulkUpdatePartsAppliesAndSkipsIndependently(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePartWrite()
	client.parts[1] = inventree.PartDetail{PK: 1, Name: "Widget A", Active: true}
	client.parts[2] = inventree.PartDetail{PK: 2, Name: "Widget B", Active: true}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateParts(deps)

	items := []BulkUpdatePartItem{
		{ID: 1, Active: dvgoutils.Ptr(false)},
		{ID: 2, Active: dvgoutils.Ptr(true)}, // no-op
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 2)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	byID := bulkResultsByID(confirmOut.Items)
	a.Equal(string(batch.OutcomeApplied), byID[1].Outcome)
	a.Equal(string(batch.OutcomeSkipped), byID[2].Outcome)
	a.Equal(1, client.updateCalls)
}

func TestBulkUpdatePartsRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePartWrite()
	client.parts[1] = inventree.PartDetail{PK: 1, Name: "Widget", Active: true}
	// A 5xx response is ambiguous: the fake still applies the field change
	// before returning the error, simulating a write that landed upstream
	// before the response was lost. Mutate must recover by reading back
	// current state rather than reporting a bare ambiguous failure.
	client.updateErr[1] = &inventree.APIError{StatusCode: http.StatusInternalServerError}
	client.applyBeforeErr[1] = true
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateParts(deps)

	items := []BulkUpdatePartItem{{ID: 1, Active: dvgoutils.Ptr(false)}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.False(confirmOut.Items[0].Record.Active)
}

func TestBulkUpdateCompaniesRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.companies[1] = inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "Old"}}
	client.companyUpdateErr[1] = &inventree.APIError{StatusCode: http.StatusInternalServerError}
	client.companyApplyBeforeErr[1] = true
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateCompanies(deps)

	items := []BulkUpdateCompanyItem{{ID: 1, Name: dvgoutils.Ptr("New")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("New", confirmOut.Items[0].Record.Name)
}

func TestBulkUpdatePartCategoriesRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCategoryAdmin()
	client.categories[1] = inventree.Category{PK: 1, Name: "Old"}
	client.updateErr[1] = &inventree.APIError{StatusCode: http.StatusInternalServerError}
	client.applyBeforeErr[1] = true
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdatePartCategories(deps)

	items := []BulkUpdatePartCategoryItem{{ID: 1, Name: dvgoutils.Ptr("New")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartCategoriesInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartCategoriesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("New", confirmOut.Items[0].Record.Name)
}

func TestBulkUpdateSupplierPartsRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.parts[10] = inventree.Part{PK: 10}
	client.companies[20] = inventree.CompanyDetail{Company: inventree.Company{PK: 20, IsSupplier: true}}
	client.suppliers[1] = inventree.SupplierPartDetail{PK: 1, Part: 10, Supplier: 20, SKU: "SKU-OLD"}
	client.supplierUpdateErr[1] = &inventree.APIError{StatusCode: http.StatusInternalServerError}
	client.supplierApplyBeforeErr[1] = true
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateSupplierParts(deps)

	items := []BulkUpdateSupplierPartItem{{ID: 1, SKU: dvgoutils.Ptr("SKU-NEW")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateSupplierPartsInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateSupplierPartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("SKU-NEW", confirmOut.Items[0].Record.SKU)
}

func TestBulkUpdateManufacturerPartsRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.parts[10] = inventree.Part{PK: 10}
	client.companies[30] = inventree.CompanyDetail{Company: inventree.Company{PK: 30, IsManufacturer: true}}
	oldMPN := "MPN-OLD"
	client.manufacturers[1] = inventree.ManufacturerPartDetail{PK: 1, Part: 10, Manufacturer: 30, MPN: &oldMPN}
	client.manufacturerUpdateErr[1] = &inventree.APIError{StatusCode: http.StatusInternalServerError}
	client.manufacturerApplyBeforeErr[1] = true
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateManufacturerParts(deps)

	items := []BulkUpdateManufacturerPartItem{{ID: 1, MPN: dvgoutils.Ptr("MPN-NEW")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateManufacturerPartsInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateManufacturerPartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	require.NotNil(t, confirmOut.Items[0].Record.MPN)
	a.Equal("MPN-NEW", *confirmOut.Items[0].Record.MPN)
}

func TestBulkUpdateCompaniesRejectsWhenOutstandingPlanCapacityIsExceeded(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.companies[1] = inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "A"}}
	client.companies[2] = inventree.CompanyDetail{Company: inventree.Company{PK: 2, Name: "B"}}
	deps := testCatalogBulkDeps(client)
	deps.companyBulkPlanStore = mustBulkStore(batch.Options[companyBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey:           func(p companyBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		MaxEntriesPerPrincipal: 1,
	})
	handler := bulkUpdateCompanies(deps)

	_, first, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: []BulkUpdateCompanyItem{{ID: 1, Name: dvgoutils.Ptr("A2")}}, DryRun: true})
	r.NoError(err)
	a.NotEmpty(first.PlanHash)

	_, second, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: []BulkUpdateCompanyItem{{ID: 2, Name: dvgoutils.Ptr("B2")}}, DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, second.Status)
	a.Empty(second.PlanHash)
}

func TestBulkUpdatePartCategoriesAppliesScalarField(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCategoryAdmin()
	client.categories[1] = inventree.Category{PK: 1, Name: "Old"}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdatePartCategories(deps)

	items := []BulkUpdatePartCategoryItem{{ID: 1, Name: dvgoutils.Ptr("New")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartCategoriesInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartCategoriesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("New", confirmOut.Items[0].Record.Name)
}

func TestBulkUpdatePartCategoriesAppliesLocationKeywordsAndIcon(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCategoryAdmin()
	oldKeywords, oldIcon := "old-keywords", "old-icon"
	client.categories[1] = inventree.Category{PK: 1, Name: "Old", Description: "old desc", DefaultLocation: dvgoutils.Ptr(10), DefaultKeywords: &oldKeywords, Icon: &oldIcon}
	client.locations[20] = inventree.StockLocation{PK: 20}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdatePartCategories(deps)

	newKeywords, newIcon := "new-keywords", "new-icon"
	items := []BulkUpdatePartCategoryItem{{
		ID: 1, Name: dvgoutils.Ptr("New"), Description: dvgoutils.Ptr("new desc"),
		DefaultLocationID: dvgoutils.Ptr(20), DefaultKeywords: &newKeywords, Icon: &newIcon,
	}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartCategoriesInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdatePartCategoriesInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	record := confirmOut.Items[0].Record
	a.Equal("New", record.Name)
	require.NotNil(t, record.DefaultLocation)
	a.Equal(20, *record.DefaultLocation)
	require.NotNil(t, record.DefaultKeywords)
	a.Equal(newKeywords, *record.DefaultKeywords)
	require.NotNil(t, record.Icon)
	a.Equal(newIcon, *record.Icon)
}

// The following TestXBulkAdapterPreflightDetectsDrift tests call each
// Adapter's Preflight directly with a hand-crafted item whose Before no
// longer matches current state. Reaching this branch through the full
// dry_run/confirm handler flow is impractical: confirm rebuilds the plan
// from fresh state immediately before Store.Consume, so any drift wide
// enough to observe from outside the handler already fails at the digest
// check (see TestBulkUpdate*RejectsStalePlanHash) before Preflight ever
// runs. Preflight's own drift check only guards the brief window between
// that rebuild and Execute actually reaching this item, which a direct
// call is the only practical way to exercise per resource.

func TestPartBulkAdapterPreflightDetectsDrift(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakePartWrite()
	client.parts[1] = inventree.PartDetail{PK: 1, Name: "Drifted"}
	adapter := &partBulkAdapter{client: client}
	item := partBulkPlanItem{
		bulkPlanItemBase: bulkPlanItemBase{ID: 1},
		Before:           inventree.PartDetail{PK: 1, Name: "Original"},
		Fields:           inventree.PatchFields{"name": inventree.Set("Target")},
	}

	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Error(err)
	a.Equal(bulkReasonDrifted, reason)
}

func TestCompanyBulkAdapterPreflightDetectsDrift(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.companies[1] = inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "Drifted"}}
	adapter := &companyBulkAdapter{client: client}
	item := companyBulkPlanItem{
		bulkPlanItemBase: bulkPlanItemBase{ID: 1},
		Before:           inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "Original"}},
		Fields:           inventree.PatchFields{"name": inventree.Set("Target")},
	}

	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Error(err)
	a.Equal(bulkReasonDrifted, reason)
}

func TestCategoryBulkAdapterPreflightDetectsDrift(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCategoryAdmin()
	client.categories[1] = inventree.Category{PK: 1, Name: "Drifted"}
	adapter := &categoryBulkAdapter{client: client}
	item := categoryBulkPlanItem{
		bulkPlanItemBase: bulkPlanItemBase{ID: 1},
		Before:           inventree.Category{PK: 1, Name: "Original"},
		Fields:           inventree.PatchFields{"name": inventree.Set("Target")},
	}

	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Error(err)
	a.Equal(bulkReasonDrifted, reason)
}

func TestSupplierPartBulkAdapterPreflightDetectsDrift(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.suppliers[1] = inventree.SupplierPartDetail{PK: 1, Part: 10, Supplier: 20, SKU: "DRIFTED"}
	adapter := &supplierPartBulkAdapter{client: client}
	item := supplierPartBulkPlanItem{
		bulkPlanItemBase: bulkPlanItemBase{ID: 1},
		Before:           inventree.SupplierPartDetail{PK: 1, Part: 10, Supplier: 20, SKU: "ORIGINAL"},
		Fields:           inventree.PatchFields{"SKU": inventree.Set("TARGET")},
	}

	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Error(err)
	a.Equal(bulkReasonDrifted, reason)
}

func TestManufacturerPartBulkAdapterPreflightDetectsDrift(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	driftedMPN, originalMPN := "DRIFTED", "ORIGINAL"
	client.manufacturers[1] = inventree.ManufacturerPartDetail{PK: 1, Part: 10, Manufacturer: 30, MPN: &driftedMPN}
	adapter := &manufacturerPartBulkAdapter{client: client}
	item := manufacturerPartBulkPlanItem{
		bulkPlanItemBase: bulkPlanItemBase{ID: 1},
		Before:           inventree.ManufacturerPartDetail{PK: 1, Part: 10, Manufacturer: 30, MPN: &originalMPN},
		Fields:           inventree.PatchFields{"MPN": inventree.Set("TARGET")},
	}

	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Error(err)
	a.Equal(bulkReasonDrifted, reason)
}

func TestBulkUpdateSupplierPartsAppliesScalarField(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.parts[10] = inventree.Part{PK: 10}
	client.companies[20] = inventree.CompanyDetail{Company: inventree.Company{PK: 20, IsSupplier: true}}
	client.suppliers[1] = inventree.SupplierPartDetail{PK: 1, Part: 10, Supplier: 20, SKU: "SKU-OLD"}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateSupplierParts(deps)

	items := []BulkUpdateSupplierPartItem{{ID: 1, SKU: dvgoutils.Ptr("SKU-NEW")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateSupplierPartsInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateSupplierPartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("SKU-NEW", confirmOut.Items[0].Record.SKU)
}

func TestBulkUpdateManufacturerPartsAppliesScalarField(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.parts[10] = inventree.Part{PK: 10}
	client.companies[30] = inventree.CompanyDetail{Company: inventree.Company{PK: 30, IsManufacturer: true}}
	oldMPN := "MPN-OLD"
	client.manufacturers[1] = inventree.ManufacturerPartDetail{PK: 1, Part: 10, Manufacturer: 30, MPN: &oldMPN}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateManufacturerParts(deps)

	items := []BulkUpdateManufacturerPartItem{{ID: 1, MPN: dvgoutils.Ptr("MPN-NEW")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateManufacturerPartsInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateManufacturerPartsInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	require.NotNil(t, confirmOut.Items[0].Record.MPN)
	a.Equal("MPN-NEW", *confirmOut.Items[0].Record.MPN)
}

func TestBulkUpdateCompaniesConfirmWithoutFlagAsksForConfirmation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeCompanyAdmin()
	client.companies[1] = inventree.CompanyDetail{Company: inventree.Company{PK: 1, Name: "Name"}}
	deps := testCatalogBulkDeps(client)
	handler := bulkUpdateCompanies(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: []BulkUpdateCompanyItem{{ID: 1, Name: dvgoutils.Ptr("Other")}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Equal(0, client.companyUpdateCalls)
}
