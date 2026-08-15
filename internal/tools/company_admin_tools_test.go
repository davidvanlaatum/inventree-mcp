package tools

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCompanyAdminIncludesApprovedFieldsAndRedactsWebsite(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	website := "https://example.test/company?token=secret#fragment"
	notes := "operator-only note"
	fake := &fakeCompanyAdmin{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", Currency: "AUD", IsSupplier: true, IsCustomer: true}, Website: website, Notes: &notes}}

	_, out, err := getCompanyAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 30})
	require.NoError(t, err)
	require.NotNil(t, out.Record)
	assert.Equal(t, website, out.Record.Website)
	assert.Equal(t, notes, *out.Record.Notes)
	assert.True(t, out.Record.IsCustomer)
}

func TestCompanyAdminExactSourcingReadsRedactLinks(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	supplierLink := "https://user:pass@example.test/supplier?token=secret#fragment"
	manufacturerLink := "https://example.test/manufacturer?secret=yes"
	shortNote, supplierNotes, manufacturerNotes := "approved short note", "supplier **Markdown**", "manufacturer **Markdown**"
	mpn := "MPN-1"
	availabilityUpdated, updated := "2026-08-15T00:00:00Z", "2026-08-15T00:01:00Z"
	inStock, onOrder := 4.5, 2.0
	fake.supplier.Link, fake.supplier.Note, fake.supplier.Notes = &supplierLink, &shortNote, &supplierNotes
	fake.supplier.MPN = &mpn
	fake.supplier.Available, fake.supplier.AvailabilityUpdated = 0, &availabilityUpdated
	fake.supplier.InStock, fake.supplier.OnOrder, fake.supplier.Updated = &inStock, &onOrder, &updated
	fake.manufacturer.Link, fake.manufacturer.Notes = &manufacturerLink, &manufacturerNotes

	_, supplier, err := getSupplierPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 40})
	r.NoError(err)
	r.NotNil(supplier.Record)
	a.Empty(supplier.Record.Link)
	a.Equal(shortNote, *supplier.Record.Note)
	a.Equal(supplierNotes, *supplier.Record.Notes)
	a.Equal(mpn, *supplier.Record.MPN)
	a.Zero(supplier.Record.Available)
	a.Equal(availabilityUpdated, *supplier.Record.AvailabilityUpdated)
	a.Equal(inStock, *supplier.Record.InStock)
	a.Equal(onOrder, *supplier.Record.OnOrder)
	a.Equal(updated, *supplier.Record.Updated)

	_, manufacturer, err := getManufacturerPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 50})
	r.NoError(err)
	r.NotNil(manufacturer.Record)
	a.Equal(manufacturerLink, manufacturer.Record.Link)
	a.Equal(manufacturerNotes, *manufacturer.Record.Notes)
}

func TestSupplierPartExactViewPreservesNullableAndDistinctNoteFieldsOnWire(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	shortNote := ""
	unsafeLink := "https://user:password@example.test/private?token=secret"
	view := supplierPartView(inventree.SupplierPartDetail{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1", Link: &unsafeLink, Note: &shortNote})

	encoded, err := json.Marshal(view)
	r.NoError(err)
	var wire map[string]any
	r.NoError(json.Unmarshal(encoded, &wire))
	a.Contains(wire, "manufacturer_part_id")
	a.Nil(wire["manufacturer_part_id"])
	a.Contains(wire, "packaging")
	a.Nil(wire["packaging"])
	a.Contains(wire, "note")
	a.Equal("", wire["note"])
	a.Contains(wire, "notes")
	a.Nil(wire["notes"])
	a.Contains(wire, "available")
	a.Equal(float64(0), wire["available"])
	a.NotContains(wire, "link", "unsafe external links remain omitted rather than serialized")
}

func TestCompanyAdminSourcingSearchesAreBoundedAndSorted(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	fake.supplierPage = inventree.SupplierPartPage{Count: 2, Results: []inventree.SupplierPartDetail{{PK: 42}, {PK: 41}}}
	fake.manufacturerPage = inventree.ManufacturerPartPage{Count: 2, Results: []inventree.ManufacturerPartDetail{{PK: 52}, {PK: 51}}}

	_, suppliers, err := searchSupplierPartsAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, SupplierPartSearchInput{Limit: 2})
	require.NoError(t, err)
	require.Len(t, suppliers.Results, 2)
	assert.Equal(t, 41, suppliers.Results[0].ID)
	_, manufacturers, err := searchManufacturerPartsAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, ManufacturerPartSearchInput{Limit: 2})
	require.NoError(t, err)
	require.Len(t, manufacturers.Results, 2)
	assert.Equal(t, 51, manufacturers.Results[0].ID)
	require.Len(t, fake.supplierQueries, 1)
	assert.Equal(t, inventree.SupplierPartQuery{Ordering: "SKU", Limit: companyAdminScanLimit}, fake.supplierQueries[0])
	require.Len(t, fake.manufacturerQueries, 1)
	assert.Equal(t, inventree.ManufacturerPartQuery{Ordering: "MPN", Limit: companyAdminScanLimit}, fake.manufacturerQueries[0])

	_, _, err = searchSupplierPartsAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, SupplierPartSearchInput{Limit: 101})
	assert.Error(t, err)
	_, _, err = searchManufacturerPartsAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, ManufacturerPartSearchInput{Offset: -1})
	assert.Error(t, err)
	_, _, err = searchSupplierPartsAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, SupplierPartSearchInput{Offset: int(^uint(0) >> 1)})
	assert.Error(t, err)
}

func TestCompanyAdminSourcingSearchFailsClosedAboveBound(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	fake.supplierPage.HasMore = true

	_, _, err := searchSupplierPartsAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, SupplierPartSearchInput{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1000-record safety bound")
}

func TestUpdateCompanyRoleRemovalRequiresConfirmationAndNoLinks(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeCompanyAdmin{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", Currency: "AUD", IsSupplier: true}}}
	remove := false

	_, out, err := updateCompanyAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: 30, IsSupplier: &remove})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCompanyCalls)

	fake.supplierPage = inventree.SupplierPartPage{Results: []inventree.SupplierPartDetail{{PK: 40, Supplier: 30}}}
	_, out, err = updateCompanyAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: 30, IsSupplier: &remove, Confirm: true})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCompanyCalls)
}

func TestUpdateCompanyConfirmedRoleRemovalWithoutLinks(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeCompanyAdmin{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", Currency: "AUD", IsSupplier: true}}}
	fake.afterCompanyUpdate = func(inventree.PatchFields) { fake.company.IsSupplier = false }
	remove := false

	_, out, err := updateCompanyAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: 30, IsSupplier: &remove, Confirm: true})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status)
	require.NotNil(t, out.Record)
	assert.False(t, out.Record.IsSupplier)
}

func TestUpdateCompanyDetectsPostWriteRoleDependencyRace(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeCompanyAdmin{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", Currency: "AUD", IsSupplier: true}}}
	pageCall := 0
	fake.supplierPageFn = func(inventree.SupplierPartQuery) inventree.SupplierPartPage {
		pageCall++
		if pageCall == 1 {
			return inventree.SupplierPartPage{}
		}
		return inventree.SupplierPartPage{Results: []inventree.SupplierPartDetail{{PK: 41, Part: 10, Supplier: 30, SKU: "RACE"}}}
	}
	fake.afterCompanyUpdate = func(inventree.PatchFields) { fake.company.IsSupplier = false }
	remove := false

	_, out, err := updateCompanyAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: 30, IsSupplier: &remove, Confirm: true})
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
	require.NotNil(t, out.Current)
	assert.False(t, out.Current.IsSupplier)
}

func TestUpdateCompanyNotesOmitSensitiveRecovery(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	notes := "secret note"
	fake := &fakeCompanyAdmin{company: inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", Currency: "AUD", IsSupplier: true}, Notes: &notes}}
	updatedNotes := "replacement note"

	fake.afterCompanyUpdate = func(inventree.PatchFields) { fake.company.Notes = &updatedNotes }
	_, out, err := updateCompanyAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: 30, Notes: &updatedNotes})
	require.NoError(t, err)
	require.NotNil(t, out.Record)
	require.NotNil(t, out.Record.Notes)
	assert.Equal(t, updatedNotes, *out.Record.Notes)
	require.NotNil(t, out.Before)
	assert.Equal(t, CompanyRecoveryView{ID: 30, Name: "Acme", Currency: "AUD", IsSupplier: true}, *out.Before)
}

func TestCompanyAndSupplierUpdatesRecoverPostPersistResponseLoss(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	complete := "https://supplier.test/item?account=42&view=full#pricing"
	fake.companyUpdateErr = errors.New("connection reset")
	fake.afterCompanyUpdate = func(inventree.PatchFields) { fake.company.Website = complete }

	_, company, err := updateCompanyAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: 30, Website: &complete})
	require.NoError(t, err)
	assert.True(t, company.Recovered)
	require.NotNil(t, company.Record)
	assert.Equal(t, complete, company.Record.Website)
	require.NotNil(t, company.Current)

	fake.companyUpdateErr = nil
	fake.supplierUpdateErr = errors.New("connection reset")
	fake.afterSupplierUpdate = func(inventree.PatchFields) { fake.supplier.Link = &complete }
	_, supplier, err := updateSupplierPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: 40, Link: &complete})
	require.NoError(t, err)
	assert.True(t, supplier.Recovered)
	require.NotNil(t, supplier.Record)
	assert.Equal(t, complete, supplier.Record.Link)
	require.NotNil(t, supplier.Current)
	encoded, err := json.Marshal(supplier.Current)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "description")
	assert.NotContains(t, string(encoded), "note")
	assert.NotContains(t, string(encoded), "link")
}

func TestUpdateSupplierAndManufacturerPartSuccess(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	packaging := "tray"
	fake.afterSupplierUpdate = func(inventree.PatchFields) { fake.supplier.Packaging = &packaging }
	_, supplier, err := updateSupplierPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: 40, Packaging: &packaging})
	require.NoError(t, err)
	require.NotNil(t, supplier.Record)
	assert.Equal(t, packaging, *supplier.Record.Packaging)

	description := "new description"
	fake.afterManufacturerUpdate = func(inventree.PatchFields) { fake.manufacturer.Description = &description }
	_, manufacturer, err := updateManufacturerPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: 50, Description: &description})
	require.NoError(t, err)
	require.NotNil(t, manufacturer.Record)
	require.NotNil(t, manufacturer.Record.Description)
	assert.Equal(t, description, *manufacturer.Record.Description)
}

func TestCompanyAndSourcingUpdatesPreserveCompleteLinksAndRejectCredentials(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	complete := "https://supplier.test/item?account=42&view=full#pricing"
	fake := baseSourcingFake()

	fake.afterCompanyUpdate = func(fields inventree.PatchFields) {
		a.Equal(inventree.Set(complete), fields["website"])
		fake.company.Website = complete
	}
	_, company, err := updateCompanyAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: 30, Website: &complete})
	r.NoError(err)
	r.NotNil(company.Record)
	a.Equal(complete, company.Record.Website)

	fake.afterSupplierUpdate = func(fields inventree.PatchFields) {
		a.Equal(inventree.Set(complete), fields["link"])
		fake.supplier.Link = &complete
	}
	_, supplier, err := updateSupplierPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: 40, Link: &complete})
	r.NoError(err)
	r.NotNil(supplier.Record)
	a.Equal(complete, supplier.Record.Link)

	fake.afterManufacturerUpdate = func(fields inventree.PatchFields) {
		a.Equal(inventree.Set(complete), fields["link"])
		fake.manufacturer.Link = &complete
	}
	_, manufacturer, err := updateManufacturerPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: 50, Link: &complete})
	r.NoError(err)
	r.NotNil(manufacturer.Record)
	a.Equal(complete, manufacturer.Record.Link)

	unsafe := "https://user:password@supplier.test/private?token=secret#fragment"
	companyCalls, supplierCalls, manufacturerCalls := fake.updateCompanyCalls, fake.updateSupplierCalls, fake.updateManufacturerCalls
	_, _, err = updateCompanyAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: 30, Website: &unsafe})
	r.ErrorContains(err, "must not include userinfo or credentials")
	_, _, err = updateSupplierPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: 40, Link: &unsafe})
	r.ErrorContains(err, "must not include userinfo or credentials")
	_, _, err = updateManufacturerPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: 50, Link: &unsafe})
	r.ErrorContains(err, "must not include userinfo or credentials")
	a.Equal(companyCalls, fake.updateCompanyCalls)
	a.Equal(supplierCalls, fake.updateSupplierCalls)
	a.Equal(manufacturerCalls, fake.updateManufacturerCalls)
}

func TestUpdateCompanyManufacturerRoleRemovalChecksLinks(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	fake.manufacturerPage = inventree.ManufacturerPartPage{Results: []inventree.ManufacturerPartDetail{fake.manufacturer}}
	remove := false
	_, out, err := updateCompanyAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: 30, IsManufacturer: &remove, Confirm: true})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestUpdateSupplierPartRefusesNormalizedDuplicate(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	fake.supplierPage = inventree.SupplierPartPage{Results: []inventree.SupplierPartDetail{{PK: 41, Part: 10, Supplier: 30, SKU: " sku-1 "}}}
	sku := "SKU-1"

	_, out, err := updateSupplierPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: 40, SKU: &sku})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	require.Len(t, out.Candidates, 1)
	assert.Equal(t, 41, out.Candidates[0].ID)
	assert.Zero(t, fake.updateSupplierCalls)
}

func TestUpdateManufacturerPartRecoversPostPersistResponseLoss(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	fake.manufacturerUpdateErr = errors.New("connection reset after write")
	complete := "https://supplier.test/item?account=42&view=full#pricing"
	fake.afterManufacturerUpdate = func(fields inventree.PatchFields) { fake.manufacturer.Link = &complete }

	_, out, err := updateManufacturerPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: 50, Link: &complete})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status)
	assert.True(t, out.Recovered)
	require.NotNil(t, out.Record)
	assert.Equal(t, complete, out.Record.Link)
	require.NotNil(t, out.Current)
}

func TestCompanyAdminRecoveryKeepsProtectedFieldsOutOfCurrent(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	secret := "do not disclose"
	fake.manufacturer.Description = &secret
	fake.manufacturer.Link = dvgoutils.Ptr("https://example.test/?token=secret")
	fake.manufacturerUpdateErr = errors.New("connection reset after write")
	fake.afterManufacturerUpdate = func(inventree.PatchFields) { fake.manufacturer.MPN = dvgoutils.Ptr("RECOVERED") }
	mpn := "RECOVERED"

	_, out, err := updateManufacturerPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: 50, MPN: &mpn})
	require.NoError(t, err)
	require.NotNil(t, out.Record)
	assert.Equal(t, "https://example.test/?token=secret", out.Record.Link)
	encoded, err := json.Marshal(out.Current)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), secret)
	assert.NotContains(t, string(encoded), "token=secret")
	assert.NotContains(t, string(encoded), "description")
	assert.NotContains(t, string(encoded), "link")
}

func TestCompanyAdminSafeErrorAllowListsFields(t *testing.T) {
	t.Parallel()
	err := safeCompanyAdminError("company update", &inventree.APIError{StatusCode: 400, FieldErrors: map[string][]string{"name": {"required"}, "tax_id": {"secret-tax"}, "notes": {"too long"}}})
	assert.Contains(t, err.Error(), "name: rejected by InvenTree")
	assert.Contains(t, err.Error(), "notes: rejected by InvenTree")
	assert.NotContains(t, err.Error(), "secret-tax")
	assert.NotContains(t, err.Error(), "required")
}

func TestUpdateSupplierPartDoesNotConfuseNullWithEmpty(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	empty := ""

	_, out, err := updateSupplierPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: 40, Description: &empty})
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
}

func TestUpdateManufacturerPartRefusesBasePartChangeWithReverseLink(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	fake.manufacturer.Part = 11
	fake.supplierPage = inventree.SupplierPartPage{Results: []inventree.SupplierPartDetail{{PK: 41, Part: 11, ManufacturerPart: dvgoutils.Ptr(50)}}}
	partID := 10

	_, out, err := updateManufacturerPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: 50, PartID: &partID})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateManufacturerCalls)
}

func TestUpdateSupplierPartDetectsPostWriteDuplicateRace(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	pageCall := 0
	fake.supplierPageFn = func(inventree.SupplierPartQuery) inventree.SupplierPartPage {
		pageCall++
		if pageCall == 1 {
			return inventree.SupplierPartPage{}
		}
		return inventree.SupplierPartPage{Results: []inventree.SupplierPartDetail{{PK: 41, Part: 10, Supplier: 30, SKU: "new"}}}
	}
	fake.afterSupplierUpdate = func(inventree.PatchFields) { fake.supplier.SKU = "NEW" }
	sku := "NEW"

	_, out, err := updateSupplierPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: 40, SKU: &sku})
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
	require.NotNil(t, out.Current)
	assert.Equal(t, "NEW", out.Current.SKU)
	require.Len(t, out.Candidates, 1)
	assert.Equal(t, 41, out.Candidates[0].ID)
}

func TestUpdateManufacturerPartDetectsPostWriteDuplicateRace(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	pageCall := 0
	fake.manufacturerPageFn = func(inventree.ManufacturerPartQuery) inventree.ManufacturerPartPage {
		pageCall++
		if pageCall == 1 {
			return inventree.ManufacturerPartPage{}
		}
		return inventree.ManufacturerPartPage{Results: []inventree.ManufacturerPartDetail{{PK: 51, Part: 10, Manufacturer: 30, MPN: dvgoutils.Ptr("new-mpn")}}}
	}
	fake.afterManufacturerUpdate = func(inventree.PatchFields) { fake.manufacturer.MPN = dvgoutils.Ptr("NEW-MPN") }
	mpn := "NEW-MPN"

	_, out, err := updateManufacturerPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: 50, MPN: &mpn})
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
	require.NotNil(t, out.Current)
	require.Len(t, out.Candidates, 1)
	assert.Equal(t, 51, out.Candidates[0].ID)
}

func TestUpdateManufacturerPartDetectsPostWriteReverseLinkRace(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := baseSourcingFake()
	fake.manufacturer.Part = 11
	pageCall := 0
	fake.supplierPageFn = func(inventree.SupplierPartQuery) inventree.SupplierPartPage {
		pageCall++
		if pageCall == 1 {
			return inventree.SupplierPartPage{}
		}
		return inventree.SupplierPartPage{Results: []inventree.SupplierPartDetail{{PK: 41, Part: 11, ManufacturerPart: dvgoutils.Ptr(50)}}}
	}
	fake.afterManufacturerUpdate = func(inventree.PatchFields) { fake.manufacturer.Part = 10 }
	partID := 10

	_, out, err := updateManufacturerPartAdmin(companyAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: 50, PartID: &partID})
	require.NoError(t, err)
	assert.Equal(t, StatusPartialFailure, out.Status)
	require.Len(t, out.BlockingSupplierParts, 1)
	assert.Equal(t, 41, out.BlockingSupplierParts[0].ID)
}

func TestCompanyAdminPatchBuildersPreserveApprovedExplicitValues(t *testing.T) {
	t.Parallel()
	falseValue := false
	trueValue := true
	empty := ""
	name := " Updated "
	currency := " AUD "
	partID, companyID, manufacturerPartID := 11, 31, 51

	companyFields, err := companyPatch(UpdateCompanyInput{ID: 30, Name: &name, Description: &empty, Website: &empty, Currency: &currency, Active: &falseValue, IsSupplier: &trueValue, IsManufacturer: &falseValue, Notes: &empty})
	require.NoError(t, err)
	companyJSON := patchJSON(t, companyFields)
	assert.Equal(t, "Updated", companyJSON["name"])
	assert.Equal(t, "AUD", companyJSON["currency"])
	assert.Equal(t, false, companyJSON["active"])

	available := 0.0
	supplierFields, targetSupplier, err := supplierPartPatch(UpdateSupplierPartInput{ID: 40, PartID: &partID, SupplierID: &companyID, SKU: dvgoutils.Ptr(" SKU "), Description: &empty, Link: &empty, Active: &falseValue, Primary: &falseValue, ManufacturerPartID: &manufacturerPartID, Packaging: &empty, PackQuantity: dvgoutils.Ptr("1"), Note: &empty, Notes: &empty, Available: &available}, inventree.SupplierPartDetail{})
	require.NoError(t, err)
	assert.Equal(t, 11, targetSupplier.Part)
	assert.Equal(t, "SKU", targetSupplier.SKU)
	assert.Equal(t, false, patchJSON(t, supplierFields)["primary"])
	assert.Equal(t, float64(0), patchJSON(t, supplierFields)["available"])
	assert.Equal(t, "", patchJSON(t, supplierFields)["notes"])

	manufacturerFields, targetManufacturer, err := manufacturerPartPatch(UpdateManufacturerPartInput{ID: 50, PartID: &partID, ManufacturerID: &companyID, MPN: dvgoutils.Ptr(" MPN "), Description: &empty, Link: &empty, Notes: &empty}, inventree.ManufacturerPartDetail{})
	require.NoError(t, err)
	require.NotNil(t, targetManufacturer.MPN)
	assert.Equal(t, "MPN", *targetManufacturer.MPN)
	assert.Equal(t, "", patchJSON(t, manufacturerFields)["link"])

	companyFields, err = companyPatch(UpdateCompanyInput{ID: 30, ClearNotes: true})
	require.NoError(t, err)
	assert.Nil(t, patchJSON(t, companyFields)["notes"])
	supplierFields, _, err = supplierPartPatch(UpdateSupplierPartInput{ID: 40, ClearDescription: true, ClearLink: true, ClearManufacturerPart: true, ClearPackaging: true, ClearNote: true, ClearNotes: true}, inventree.SupplierPartDetail{})
	require.NoError(t, err)
	supplierJSON := patchJSON(t, supplierFields)
	assert.Nil(t, supplierJSON["description"])
	assert.Nil(t, supplierJSON["manufacturer_part"])
	assert.Nil(t, supplierJSON["notes"])
	manufacturerFields, _, err = manufacturerPartPatch(UpdateManufacturerPartInput{ID: 50, ClearMPN: true, ClearDescription: true, ClearLink: true, ClearNotes: true}, inventree.ManufacturerPartDetail{})
	require.NoError(t, err)
	assert.Nil(t, patchJSON(t, manufacturerFields)["MPN"])
	assert.Nil(t, patchJSON(t, manufacturerFields)["notes"])

	_, err = companyPatch(UpdateCompanyInput{Notes: &empty, ClearNotes: true})
	assert.Error(t, err)
	_, _, err = supplierPartPatch(UpdateSupplierPartInput{Description: &empty, ClearDescription: true}, inventree.SupplierPartDetail{})
	assert.Error(t, err)
	_, _, err = manufacturerPartPatch(UpdateManufacturerPartInput{MPN: &empty, ClearMPN: true}, inventree.ManufacturerPartDetail{})
	assert.Error(t, err)
	_, _, err = supplierPartPatch(UpdateSupplierPartInput{Notes: &empty, ClearNotes: true}, inventree.SupplierPartDetail{})
	assert.Error(t, err)
	_, _, err = manufacturerPartPatch(UpdateManufacturerPartInput{Notes: &empty, ClearNotes: true}, inventree.ManufacturerPartDetail{})
	assert.Error(t, err)
	notFinite := math.Inf(1)
	_, _, err = supplierPartPatch(UpdateSupplierPartInput{Available: &notFinite}, inventree.SupplierPartDetail{})
	assert.ErrorContains(t, err, "available must be finite")
}

func patchJSON(t *testing.T, fields inventree.PatchFields) map[string]any {
	t.Helper()
	data, err := json.Marshal(fields)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

func baseSourcingFake() *fakeCompanyAdmin {
	return &fakeCompanyAdmin{
		part:         inventree.Part{PK: 10},
		company:      inventree.CompanyDetail{Company: inventree.Company{PK: 30, Name: "Acme", Currency: "AUD", IsSupplier: true, IsManufacturer: true}},
		supplier:     inventree.SupplierPartDetail{PK: 40, Part: 10, Supplier: 30, SKU: "OLD"},
		manufacturer: inventree.ManufacturerPartDetail{PK: 50, Part: 10, Manufacturer: 30, MPN: dvgoutils.Ptr("OLD-MPN")},
	}
}

type fakeCompanyAdmin struct {
	part                    inventree.Part
	company                 inventree.CompanyDetail
	supplier                inventree.SupplierPartDetail
	manufacturer            inventree.ManufacturerPartDetail
	supplierPage            inventree.SupplierPartPage
	manufacturerPage        inventree.ManufacturerPartPage
	supplierPageFn          func(inventree.SupplierPartQuery) inventree.SupplierPartPage
	manufacturerPageFn      func(inventree.ManufacturerPartQuery) inventree.ManufacturerPartPage
	supplierQueries         []inventree.SupplierPartQuery
	manufacturerQueries     []inventree.ManufacturerPartQuery
	updateCompanyCalls      int
	updateSupplierCalls     int
	updateManufacturerCalls int
	manufacturerUpdateErr   error
	companyUpdateErr        error
	supplierUpdateErr       error
	afterCompanyUpdate      func(inventree.PatchFields)
	afterSupplierUpdate     func(inventree.PatchFields)
	afterManufacturerUpdate func(inventree.PatchFields)
}

func (f *fakeCompanyAdmin) GetPart(context.Context, int) (inventree.Part, error) { return f.part, nil }
func (f *fakeCompanyAdmin) GetCompanyDetail(context.Context, int) (inventree.CompanyDetail, error) {
	return f.company, nil
}
func (f *fakeCompanyAdmin) SearchCompaniesPage(context.Context, inventree.SearchQuery) (inventree.CompanyPage, error) {
	return inventree.CompanyPage{Results: []inventree.CompanyDetail{f.company}}, nil
}
func (f *fakeCompanyAdmin) GetSupplierPartDetail(context.Context, int) (inventree.SupplierPartDetail, error) {
	return f.supplier, nil
}

func (f *fakeCompanyAdmin) SearchSupplierPartsPage(_ context.Context, query inventree.SupplierPartQuery) (inventree.SupplierPartPage, error) {
	f.supplierQueries = append(f.supplierQueries, query)
	if f.supplierPageFn != nil {
		return f.supplierPageFn(query), nil
	}
	return f.supplierPage, nil
}
func (f *fakeCompanyAdmin) GetManufacturerPartDetail(context.Context, int) (inventree.ManufacturerPartDetail, error) {
	return f.manufacturer, nil
}

func (f *fakeCompanyAdmin) SearchManufacturerPartsPage(_ context.Context, query inventree.ManufacturerPartQuery) (inventree.ManufacturerPartPage, error) {
	f.manufacturerQueries = append(f.manufacturerQueries, query)
	if f.manufacturerPageFn != nil {
		return f.manufacturerPageFn(query), nil
	}
	return f.manufacturerPage, nil
}
func (f *fakeCompanyAdmin) UpdateCompany(_ context.Context, _ int, fields inventree.PatchFields) (inventree.CompanyDetail, error) {
	f.updateCompanyCalls++
	if f.afterCompanyUpdate != nil {
		f.afterCompanyUpdate(fields)
	}
	return f.company, f.companyUpdateErr
}
func (f *fakeCompanyAdmin) UpdateSupplierPart(_ context.Context, _ int, fields inventree.PatchFields) (inventree.SupplierPartDetail, error) {
	f.updateSupplierCalls++
	if f.afterSupplierUpdate != nil {
		f.afterSupplierUpdate(fields)
	}
	return f.supplier, f.supplierUpdateErr
}
func (f *fakeCompanyAdmin) UpdateManufacturerPart(_ context.Context, _ int, fields inventree.PatchFields) (inventree.ManufacturerPartDetail, error) {
	f.updateManufacturerCalls++
	if f.afterManufacturerUpdate != nil {
		f.afterManufacturerUpdate(fields)
	}
	return f.manufacturer, f.manufacturerUpdateErr
}

func companyAdminDeps(fake *fakeCompanyAdmin) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}
