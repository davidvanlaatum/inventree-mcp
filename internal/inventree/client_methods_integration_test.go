//go:build !no_integration_tests

package inventree_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientMethodsAgainstInvenTree(t *testing.T) {
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	if testenv.SkipDocker(os.Getenv) || testing.Short() {
		t.Skipf("Docker-backed InvenTree integration test excluded by %s or -short", testenv.EnvSkipDocker)
	}
	t.Parallel()

	opts := testenv.DefaultTestOptions(t)
	opts.StartWorker = true
	t.Logf("starting client method integration stack with image %s, expected version %s, expected API %s", opts.Image, opts.ExpectedVersion, opts.ExpectedAPIVersion)
	shared, err := testenv.StartSharedInvenTree(ctx, opts)
	r.NoError(err)
	r.NotNil(shared)
	t.Cleanup(testenv.CleanupForTest(t, func() error {
		return shared.Close(context.WithoutCancel(ctx))
	}))

	t.Run("testing_and_requirements_discovery", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)

		getJSON := func(path string, query url.Values, out *map[string]any) {
			req, err := fixture.client.NewRequest(ctx, http.MethodGet, path, query, nil)
			r.NoError(err)
			r.NoError(fixture.client.DoJSON(req, out))
		}

		var templates map[string]any
		getJSON("/api/part/test-template/", url.Values{"limit": {"100"}}, &templates)
		filterReq, err := fixture.client.NewRequest(ctx, http.MethodGet, "/api/part/test-template/", url.Values{"limit": {"100"}, "part": {strconv.Itoa(part.ID)}}, nil)
		r.NoError(err)
		filterErr := fixture.client.DoJSON(filterReq, &map[string]any{})
		var filterAPIError *inventree.APIError
		r.ErrorAs(filterErr, &filterAPIError)
		if filterAPIError != nil {
			r.Equal(http.StatusBadRequest, filterAPIError.StatusCode, "the documented part filter is rejected on the pinned API")
		}
		assertPaginated := func(page map[string]any, label string) {
			results, ok := page["results"].([]any)
			r.True(ok, "%s results should be an array", label)
			count, ok := page["count"].(float64)
			r.True(ok, "%s count should be numeric", label)
			if ok {
				r.GreaterOrEqual(count, float64(len(results)), "%s count should include the current page", label)
				r.LessOrEqual(len(results), 100, "%s must respect the requested page size", label)
			}
		}
		assertPaginated(templates, "part test templates")

		var results map[string]any
		getJSON("/api/stock/test/", url.Values{"limit": {"100"}}, &results)
		assertPaginated(results, "stock test results")

		var requirements map[string]any
		getJSON("/api/part/"+strconv.Itoa(part.ID)+"/requirements/", nil, &requirements)
		for _, key := range []string{
			"total_stock", "unallocated_stock", "can_build", "ordering", "building",
			"scheduled_to_build", "required_for_build_orders", "allocated_to_build_orders",
			"required_for_sales_orders", "allocated_to_sales_orders",
		} {
			r.Contains(requirements, key, "requirements response missing %q", key)
		}

		var barcodeHistory map[string]any
		getJSON("/api/barcode/history/", url.Values{"limit": {"100"}}, &barcodeHistory)
		assertPaginated(barcodeHistory, "barcode history")
		storageSetting, err := fixture.client.GetGlobalSetting(ctx, "BARCODE_STORE_RESULTS")
		r.NoError(err)
		r.Equal("BARCODE_STORE_RESULTS", storageSetting.Key)
		r.Equal("false", strings.ToLower(storageSetting.Value))
		barcodeResults, ok := barcodeHistory["results"].([]any)
		r.True(ok)
		barcodeCount, ok := barcodeHistory["count"].(float64)
		r.True(ok)
		if ok {
			r.Zero(barcodeCount, "default-disabled barcode storage should expose no history")
		}
		r.Empty(barcodeResults, "default-disabled barcode storage should return an empty history page")
		t.Logf("schema-backed read-only discovery: templates=%v results=%v requirements=%v", templates["count"], results["count"], requirements)
	})

	t.Run("current_user_and_connector_token", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		user, err := fixture.client.GetCurrentUser(ctx)
		r.NoError(err)
		r.NotZero(user.PK)
		r.Equal(fixture.account.Username, user.Username)

		tokenName, err := fixture.run.Name("connector-token")
		r.NoError(err)
		token, err := fixture.client.CreateCurrentUserToken(ctx, tokenName)
		r.NoError(err)
		r.NotEmpty(token.Token)
		r.Equal(tokenName, token.Name)
		dedicatedClient, err := inventree.NewClient(inventree.Config{
			BaseURL: shared.Environment().BaseURL,
			Credential: inventree.Credential{
				Scheme: inventree.AuthSchemeToken,
				Token:  token.Token,
			},
		})
		r.NoError(err)
		dedicatedUser, err := dedicatedClient.GetCurrentUser(ctx)
		r.NoError(err)
		r.Equal(user.PK, dedicatedUser.PK)

		secondTokenName, err := fixture.run.Name("connector-token-second")
		r.NoError(err)
		secondToken, err := fixture.client.CreateCurrentUserToken(ctx, secondTokenName)
		r.NoError(err)
		r.NotEmpty(secondToken.Token)
		r.NotEqual(token.Token, secondToken.Token)
		secondDedicatedClient, err := inventree.NewClient(inventree.Config{
			BaseURL: shared.Environment().BaseURL,
			Credential: inventree.Credential{
				Scheme: inventree.AuthSchemeToken,
				Token:  secondToken.Token,
			},
		})
		r.NoError(err)
		secondDedicatedUser, err := secondDedicatedClient.GetCurrentUser(ctx)
		r.NoError(err)
		r.Equal(user.PK, secondDedicatedUser.PK)

		firstStillUsable, err := dedicatedClient.GetCurrentUser(ctx)
		r.NoError(err)
		r.Equal(user.PK, firstStillUsable.PK)
		suppliedStillUsable, err := fixture.client.GetCurrentUser(ctx)
		r.NoError(err)
		r.Equal(user.PK, suppliedStillUsable.PK)
	})

	t.Run("part_category", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		part := fixture.ensure(t, testenv.FixturePart)

		parts, err := fixture.client.SearchParts(ctx, inventree.SearchQuery{Search: part.Name})
		r.NoError(err)
		r.NotEmpty(parts)
		r.Equal(part.ID, parts[0].PK)
		partPage, err := fixture.client.SearchPartsPage(ctx, inventree.PartQuery{CategoryID: category.ID, Limit: 100})
		r.NoError(err)
		r.NotEmpty(partPage.Results)
		r.Equal(part.ID, partPage.Results[0].PK)
		gotPart, err := fixture.client.GetPart(ctx, part.ID)
		r.NoError(err)
		r.Equal(part.Name, gotPart.Name)
		gotPartDetail, err := fixture.client.GetPartDetail(ctx, part.ID)
		r.NoError(err)
		r.Equal(part.ID, gotPartDetail.PK)
		r.Equal(part.Name, gotPartDetail.Name)

		categories, err := fixture.client.SearchPartCategories(ctx, inventree.SearchQuery{Search: category.Name})
		r.NoError(err)
		r.NotEmpty(categories)
		r.Equal(category.ID, categories[0].PK)
		gotCategory, err := fixture.client.GetPartCategory(ctx, category.ID)
		r.NoError(err)
		r.Equal(category.Name, gotCategory.Name)

		rootName, err := fixture.run.Name("client-category-root")
		r.NoError(err)
		root, err := fixture.client.CreatePartCategory(ctx, inventree.CategoryCreate{Name: rootName, Description: dvgoutils.Ptr("client category root"), Structural: dvgoutils.Ptr(false)})
		r.NoError(err)
		r.NotZero(root.PK)
		childName, err := fixture.run.Name("client-category-child")
		r.NoError(err)
		child, err := fixture.client.CreatePartCategory(ctx, inventree.CategoryCreate{Name: childName, Parent: &root.PK, DefaultKeywords: dvgoutils.Ptr("client-keyword")})
		r.NoError(err)
		r.Equal(root.PK, *child.Parent)
		childPage, err := fixture.client.SearchPartCategoriesPage(ctx, inventree.CategoryQuery{Parent: &root.PK, Limit: 100})
		r.NoError(err)
		r.NotEmpty(childPage.Results)
		updatedCategory, err := fixture.client.UpdatePartCategory(ctx, child.PK, inventree.PatchFields{"description": inventree.Set("updated client category"), "default_keywords": inventree.Set("")})
		r.NoError(err)
		r.Equal("updated client category", updatedCategory.Description)
		readbackCategory, err := fixture.client.GetPartCategory(ctx, child.PK)
		r.NoError(err)
		r.Equal("", *readbackCategory.DefaultKeywords)

		detailName, err := fixture.run.Name("client-part-detail")
		r.NoError(err)
		link := "https://example.test/parts/detail?source=integration#notes"
		createdDetail, err := fixture.client.CreatePart(ctx, inventree.PartCreate{
			Name: detailName, Category: &category.ID, Consumable: dvgoutils.Ptr(true), DefaultExpiry: dvgoutils.Ptr(30),
			IsTemplate: dvgoutils.Ptr(false), Keywords: dvgoutils.Ptr("client detail"), Link: &link,
			Locked: dvgoutils.Ptr(false), MinimumStock: dvgoutils.Ptr(2.5), MaximumStock: dvgoutils.Ptr(5.5),
			Revision: dvgoutils.Ptr("A"), Salable: dvgoutils.Ptr(true), Testable: dvgoutils.Ptr(true), Notes: dvgoutils.Ptr("integration markdown"),
		})
		r.NoError(err)
		detail, err := fixture.client.GetPartDetail(ctx, createdDetail.PK)
		r.NoError(err)
		r.True(detail.Consumable)
		r.Equal(30, detail.DefaultExpiry)
		r.Equal("client detail", *detail.Keywords)
		r.Equal(link, *detail.Link)
		r.Equal(2.5, detail.MinimumStock)
		r.Equal(5.5, detail.MaximumStock)
		r.Equal("A", *detail.Revision)
		r.Equal("integration markdown", *detail.Notes)
		r.NotNil(detail.CreationUser)

		_, err = fixture.client.UpdatePart(ctx, createdDetail.PK, inventree.PatchFields{
			"keywords": inventree.Null(), "link": inventree.Null(), "revision": inventree.Null(), "notes": inventree.Null(),
			"default_expiry": inventree.Set(0), "minimum_stock": inventree.Set(3.0), "maximum_stock": inventree.Set(0.0),
			"consumable": inventree.Set(false), "salable": inventree.Set(false), "testable": inventree.Set(false),
		})
		r.NoError(err)
		detail, err = fixture.client.GetPartDetail(ctx, createdDetail.PK)
		r.NoError(err)
		r.Nil(detail.Keywords)
		r.Nil(detail.Link)
		r.Nil(detail.Revision)
		r.Nil(detail.Notes)
		r.Zero(detail.DefaultExpiry)
		r.Equal(3.0, detail.MinimumStock)
		r.Zero(detail.MaximumStock)
		r.False(detail.Consumable)
		r.False(detail.Salable)
		r.False(detail.Testable)
	})

	t.Run("company_supplier", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		manufacturer := fixture.ensure(t, testenv.FixtureManufacturer)
		part := fixture.ensure(t, testenv.FixturePart)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)

		companies, err := fixture.client.SearchCompanies(ctx, inventree.SearchQuery{Search: supplier.Name})
		r.NoError(err)
		r.NotEmpty(companies)
		r.Equal(supplier.ID, companies[0].PK)

		suppliers, err := fixture.client.SearchSuppliers(ctx, inventree.SearchQuery{Search: supplier.Name})
		r.NoError(err)
		r.NotEmpty(suppliers)
		r.Equal(supplier.ID, suppliers[0].PK)
		r.True(suppliers[0].IsSupplier)
		gotSupplier, err := fixture.client.GetCompany(ctx, supplier.ID)
		r.NoError(err)
		r.Equal(supplier.ID, gotSupplier.PK)
		r.True(gotSupplier.IsSupplier)

		manufacturers, err := fixture.client.SearchManufacturers(ctx, inventree.SearchQuery{Search: manufacturer.Name})
		r.NoError(err)
		r.NotEmpty(manufacturers)
		r.Equal(manufacturer.ID, manufacturers[0].PK)
		r.True(manufacturers[0].IsManufacturer)

		supplierParts, err := fixture.client.SearchSupplierParts(ctx, inventree.SupplierPartQuery{SKU: supplierPart.Name})
		r.NoError(err)
		r.NotEmpty(supplierParts)
		r.Equal(supplierPart.ID, supplierParts[0].PK)
		r.Equal(part.ID, supplierParts[0].Part)
		r.Equal(supplier.ID, supplierParts[0].Supplier)

		gotSupplierPart, err := fixture.client.GetSupplierPart(ctx, supplierPart.ID)
		r.NoError(err)
		r.Equal(supplierPart.ID, gotSupplierPart.PK)
		r.Equal(part.ID, gotSupplierPart.Part)
		r.Equal(supplier.ID, gotSupplierPart.Supplier)
		r.Equal(supplierPart.Name, gotSupplierPart.SKU)
	})

	t.Run("writes", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		location := fixture.ensure(t, testenv.FixtureLocation)

		partName, err := fixture.run.Name("part")
		r.NoError(err)
		part, err := fixture.client.CreatePart(ctx, inventree.PartCreate{
			Name:            partName,
			Description:     "created through client integration test",
			Category:        dvgoutils.Ptr(category.ID),
			DefaultLocation: dvgoutils.Ptr(location.ID),
			Active:          dvgoutils.Ptr(true),
			Assembly:        dvgoutils.Ptr(false),
			Purchaseable:    dvgoutils.Ptr(true),
			Component:       dvgoutils.Ptr(true),
			Trackable:       dvgoutils.Ptr(false),
			Virtual:         dvgoutils.Ptr(false),
		})
		r.NoError(err)
		r.NotZero(part.PK)
		r.Equal(partName, part.Name)
		r.Equal(category.ID, *part.Category)

		updated, err := fixture.client.UpdatePart(ctx, part.PK, inventree.PatchFields{
			"description": inventree.Set("updated through client integration test"),
			"active":      inventree.Set(false),
		})
		r.NoError(err)
		r.Equal(part.PK, updated.PK)
		r.False(updated.Active)
		r.Equal("updated through client integration test", updated.Description)

		supplierName, err := fixture.run.Name("supplier")
		r.NoError(err)
		supplier, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{
			Name:       supplierName,
			Currency:   "USD",
			IsSupplier: true,
		})
		r.NoError(err)
		r.NotZero(supplier.PK)
		r.True(supplier.IsSupplier)
		companyPage, err := fixture.client.SearchCompaniesPage(ctx, inventree.SearchQuery{Search: supplierName, Limit: 20})
		r.NoError(err)
		r.NotEmpty(companyPage.Results)
		companyDetail, err := fixture.client.GetCompanyDetail(ctx, supplier.PK)
		r.NoError(err)
		r.Equal(supplier.PK, companyDetail.PK)
		updatedCompany, err := fixture.client.UpdateCompany(ctx, supplier.PK, inventree.PatchFields{"description": inventree.Set("updated supplier")})
		r.NoError(err)
		r.Equal("updated supplier", updatedCompany.Description)

		manufacturerName, err := fixture.run.Name("mfg")
		r.NoError(err)
		manufacturer, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{
			Name:           manufacturerName,
			Currency:       "USD",
			IsManufacturer: true,
		})
		r.NoError(err)
		r.NotZero(manufacturer.PK)
		r.True(manufacturer.IsManufacturer)

		sku, err := fixture.run.Name("sku")
		r.NoError(err)
		supplierNotes := "supplier **Markdown** notes"
		available := 12.5
		supplierPart, err := fixture.client.CreateSupplierPart(ctx, inventree.SupplierPartCreate{
			Part:      part.PK,
			Supplier:  supplier.PK,
			SKU:       sku,
			Active:    dvgoutils.Ptr(false),
			Notes:     &supplierNotes,
			Available: &available,
		})
		r.NoError(err)
		r.NotZero(supplierPart.PK)
		r.Equal(part.PK, supplierPart.Part)
		r.Equal(supplier.PK, supplierPart.Supplier)
		r.False(supplierPart.Active)
		supplierPartPage, err := fixture.client.SearchSupplierPartsPage(ctx, inventree.SupplierPartQuery{Supplier: supplier.PK, SKU: sku, Limit: 20})
		r.NoError(err)
		r.NotEmpty(supplierPartPage.Results)
		supplierPartDetail, err := fixture.client.GetSupplierPartDetail(ctx, supplierPart.PK)
		r.NoError(err)
		r.Equal(supplierPart.PK, supplierPartDetail.PK)
		r.Equal(supplierNotes, *supplierPartDetail.Notes)
		r.Equal(available, supplierPartDetail.Available)
		r.NotNil(supplierPartDetail.AvailabilityUpdated)
		r.NotNil(supplierPartDetail.Updated)
		var supplierRawPlain map[string]any
		plainSupplierReq, err := fixture.client.NewRequest(ctx, "GET", fmt.Sprintf("/api/company/part/%d/", supplierPart.PK), nil, nil)
		r.NoError(err)
		r.NoError(fixture.client.DoJSON(plainSupplierReq, &supplierRawPlain))
		r.NotContains(supplierRawPlain, "tags", "plain GET /api/company/part/{id}/ is expected to omit the tags field")
		var supplierRaw map[string]any
		req, err := fixture.client.NewRequest(ctx, "GET", fmt.Sprintf("/api/company/part/%d/", supplierPart.PK), url.Values{"tags": []string{"true"}}, nil)
		r.NoError(err)
		r.NoError(fixture.client.DoJSON(req, &supplierRaw))
		for field, class := range inventree.SupplierPartFieldInventory {
			if class == inventree.SourcingPartFieldExposed {
				r.Contains(supplierRaw, field, "pinned supplier response field %s", field)
			}
		}
		for field := range supplierRaw {
			r.Contains(inventree.SupplierPartFieldInventory, field, "unclassified supplier response field %s", field)
		}
		updatedSupplierPart, err := fixture.client.UpdateSupplierPart(ctx, supplierPart.PK, inventree.PatchFields{"description": inventree.Set("updated supplier part"), "primary": inventree.Set(false)})
		r.NoError(err)
		r.NotNil(updatedSupplierPart.Description)
		r.Equal("updated supplier part", *updatedSupplierPart.Description)

		mpn, err := fixture.run.Name("mpn")
		r.NoError(err)
		manufacturerNotes := "manufacturer **Markdown** notes"
		manufacturerPart, err := fixture.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{
			Part:         part.PK,
			Manufacturer: manufacturer.PK,
			MPN:          dvgoutils.Ptr(mpn),
			Notes:        &manufacturerNotes,
		})
		r.NoError(err)
		r.NotZero(manufacturerPart.PK)
		r.Equal(part.PK, manufacturerPart.Part)
		r.Equal(manufacturer.PK, manufacturerPart.Manufacturer)
		r.Equal(mpn, manufacturerPart.MPN)
		manufacturerPartPage, err := fixture.client.SearchManufacturerPartsPage(ctx, inventree.ManufacturerPartQuery{Manufacturer: manufacturer.PK, MPN: mpn, Limit: 20})
		r.NoError(err)
		r.NotEmpty(manufacturerPartPage.Results)
		manufacturerPartDetail, err := fixture.client.GetManufacturerPartDetail(ctx, manufacturerPart.PK)
		r.NoError(err)
		r.Equal(manufacturerPart.PK, manufacturerPartDetail.PK)
		r.Equal(manufacturerNotes, *manufacturerPartDetail.Notes)
		var manufacturerRawPlain map[string]any
		plainManufacturerReq, err := fixture.client.NewRequest(ctx, "GET", fmt.Sprintf("/api/company/part/manufacturer/%d/", manufacturerPart.PK), nil, nil)
		r.NoError(err)
		r.NoError(fixture.client.DoJSON(plainManufacturerReq, &manufacturerRawPlain))
		r.NotContains(manufacturerRawPlain, "tags", "plain GET /api/company/part/manufacturer/{id}/ is expected to omit the tags field")
		var manufacturerRaw map[string]any
		req, err = fixture.client.NewRequest(ctx, "GET", fmt.Sprintf("/api/company/part/manufacturer/%d/", manufacturerPart.PK), url.Values{"tags": []string{"true"}}, nil)
		r.NoError(err)
		r.NoError(fixture.client.DoJSON(req, &manufacturerRaw))
		for field, class := range inventree.ManufacturerPartFieldInventory {
			if class == inventree.SourcingPartFieldExposed {
				r.Contains(manufacturerRaw, field, "pinned manufacturer response field %s", field)
			}
		}
		for field := range manufacturerRaw {
			r.Contains(inventree.ManufacturerPartFieldInventory, field, "unclassified manufacturer response field %s", field)
		}
		updatedManufacturerPart, err := fixture.client.UpdateManufacturerPart(ctx, manufacturerPart.PK, inventree.PatchFields{"description": inventree.Set("updated manufacturer part")})
		r.NoError(err)
		r.NotNil(updatedManufacturerPart.Description)
		r.Equal("updated manufacturer part", *updatedManufacturerPart.Description)

		patchedSupplierPart, err := fixture.client.UpdateSupplierPart(ctx, supplierPart.PK, inventree.PatchFields{"manufacturer_part": inventree.Set(manufacturerPart.PK), "available": inventree.Set(0.0), "notes": inventree.Null()})
		r.NoError(err)
		r.NotNil(patchedSupplierPart.MPN, "PATCH .../company/part/%d/ response should reflect MPN %q from newly linked manufacturer_part %d", supplierPart.PK, mpn, manufacturerPart.PK)
		r.Equal(mpn, *patchedSupplierPart.MPN)
		supplierPartDetail, err = fixture.client.GetSupplierPartDetail(ctx, supplierPart.PK)
		r.NoError(err)
		r.NotNil(supplierPartDetail.MPN, "GET .../company/part/%d/ should reflect MPN %q from manufacturer_part %d set moments earlier by PATCH", supplierPart.PK, mpn, manufacturerPart.PK)
		r.Equal(mpn, *supplierPartDetail.MPN)
		r.Zero(supplierPartDetail.Available)
		r.Nil(supplierPartDetail.Notes)
		_, err = fixture.client.UpdateManufacturerPart(ctx, manufacturerPart.PK, inventree.PatchFields{"notes": inventree.Null()})
		r.NoError(err)
		manufacturerPartDetail, err = fixture.client.GetManufacturerPartDetail(ctx, manufacturerPart.PK)
		r.NoError(err)
		r.Nil(manufacturerPartDetail.Notes)

		manufacturerParts, err := fixture.client.SearchManufacturerParts(ctx, inventree.ManufacturerPartQuery{
			Part:         part.PK,
			Manufacturer: manufacturer.PK,
			MPN:          mpn,
		})
		r.NoError(err)
		r.NotEmpty(manufacturerParts)
		r.Equal(manufacturerPart.PK, manufacturerParts[0].PK)
	})

	t.Run("company_detail_and_role_completeness", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		location := fixture.ensure(t, testenv.FixtureLocation)
		part := fixture.ensure(t, testenv.FixturePart)

		customerName, err := fixture.run.Name("customer")
		r.NoError(err)
		created, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{Name: customerName, Currency: "USD"})
		r.NoError(err)
		r.NotZero(created.PK)
		customer, err := fixture.client.UpdateCompany(ctx, created.PK, inventree.PatchFields{"is_customer": inventree.Set(true)})
		r.NoError(err)
		r.True(customer.IsCustomer)

		phone, contact, taxID, email, link := "555-0100", "Jane Doe", "ABN123", "ap@example.test", "https://example.test/info"
		updated, err := fixture.client.UpdateCompany(ctx, customer.PK, inventree.PatchFields{
			"phone": inventree.Set(phone), "contact": inventree.Set(contact), "tax_id": inventree.Set(taxID),
			"email": inventree.Set(email), "link": inventree.Set(link),
		})
		r.NoError(err)
		a.Equal(phone, updated.Phone)
		a.Equal(contact, updated.Contact)
		a.Equal(taxID, updated.TaxID)
		r.NotNil(updated.Email)
		a.Equal(email, *updated.Email)
		a.Equal(link, updated.Link)

		detail, err := fixture.client.GetCompanyDetail(ctx, customer.PK)
		r.NoError(err)
		a.Equal(phone, detail.Phone)
		a.Equal(contact, detail.Contact)
		a.Equal(taxID, detail.TaxID)
		r.NotNil(detail.Email)
		a.Equal(email, *detail.Email)

		// clear_email and empty-string clears for the plain (non-nullable) fields.
		_, err = fixture.client.UpdateCompany(ctx, customer.PK, inventree.PatchFields{
			"phone": inventree.Set(""), "contact": inventree.Set(""), "tax_id": inventree.Set(""), "email": inventree.Null(),
		})
		r.NoError(err)
		cleared, err := fixture.client.GetCompanyDetail(ctx, customer.PK)
		r.NoError(err)
		a.Empty(cleared.Phone)
		a.Empty(cleared.Contact)
		a.Empty(cleared.TaxID)
		a.Nil(cleared.Email)

		var companyRaw map[string]any
		req, err := fixture.client.NewRequest(ctx, "GET", fmt.Sprintf("/api/company/%d/", customer.PK), nil, nil)
		r.NoError(err)
		r.NoError(fixture.client.DoJSON(req, &companyRaw))
		for field, class := range inventree.CompanyFieldInventory {
			// tags is Exposed but, per F-S56/F-S91, only appears when the
			// request carries the ?tags=true query flag GetCompanyDetail
			// always sends; a plain GET (this probe) omits it like the
			// still-deferred/separate-lookup fields below.
			if class == inventree.CompanyFieldExposed && field != "tags" {
				r.Contains(companyRaw, field, "pinned company response field %s", field)
			}
		}
		for field := range companyRaw {
			r.Contains(inventree.CompanyFieldInventory, field, "unclassified company response field %s", field)
		}
		_, hasParameters := companyRaw["parameters"]
		_, hasTags := companyRaw["tags"]
		_, hasPrimaryAddress := companyRaw["primary_address"]
		a.False(hasParameters, "deferred parameters field must not appear in the live raw response the classification was checked against")
		a.False(hasTags, "tags must not appear in a plain GET without the ?tags=true query flag")
		a.False(hasPrimaryAddress, "separate-lookup primary_address field must not appear in the live raw response the classification was checked against")

		var companyRawWithTags map[string]any
		reqWithTags, err := fixture.client.NewRequest(ctx, "GET", fmt.Sprintf("/api/company/%d/", customer.PK), url.Values{"tags": []string{"true"}}, nil)
		r.NoError(err)
		r.NoError(fixture.client.DoJSON(reqWithTags, &companyRawWithTags))
		a.Contains(companyRawWithTags, "tags", "pinned company response field tags with the ?tags=true query flag")

		// Customer-role dependency audit: SearchStockItemsPage and
		// SearchSalesOrdersPage are new bounded existence-count client
		// methods introduced solely to gate remove_company_customer_role.
		noStock, err := fixture.client.SearchStockItemsPage(ctx, inventree.StockItemQuery{Customer: customer.PK, Limit: 1})
		r.NoError(err)
		a.Zero(noStock.Count)
		noSalesOrders, err := fixture.client.SearchSalesOrdersPage(ctx, inventree.SalesOrderQuery{Customer: customer.PK, Limit: 1})
		r.NoError(err)
		a.Zero(noSalesOrders.Count)

		stockItem, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 1})
		r.NoError(err)
		r.NotZero(stockItem.PK)
		_, err = fixture.client.UpdateStockItem(ctx, stockItem.PK, inventree.PatchFields{"customer": inventree.Set(customer.PK)})
		r.NoError(err)
		withStock, err := fixture.client.SearchStockItemsPage(ctx, inventree.StockItemQuery{Customer: customer.PK, Limit: 1})
		r.NoError(err)
		a.Equal(1, withStock.Count)
		_, err = fixture.client.UpdateStockItem(ctx, stockItem.PK, inventree.PatchFields{"customer": inventree.Null()})
		r.NoError(err)
		afterClear, err := fixture.client.SearchStockItemsPage(ctx, inventree.StockItemQuery{Customer: customer.PK, Limit: 1})
		r.NoError(err)
		a.Zero(afterClear.Count)

		var soRaw json.RawMessage
		r.NoError(fixture.client.Post(ctx, "/api/order/so/", map[string]any{"customer": customer.PK}, &soRaw))
		var salesOrder inventree.SalesOrderSummary
		r.NoError(json.Unmarshal(soRaw, &salesOrder))
		r.NotZero(salesOrder.PK)
		withSalesOrder, err := fixture.client.SearchSalesOrdersPage(ctx, inventree.SalesOrderQuery{Customer: customer.PK, Limit: 1})
		r.NoError(err)
		a.Equal(1, withSalesOrder.Count)
	})

	t.Run("helpers", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)

		name, err := fixture.run.Name("company")
		r.NoError(err)
		var created inventree.Company
		r.NoError(fixture.client.Post(ctx, "/api/company/", map[string]any{
			"name":            name,
			"currency":        "USD",
			"is_supplier":     true,
			"is_manufacturer": false,
			"is_customer":     false,
		}, &created))
		r.NotZero(created.PK)
		r.Equal(name, created.Name)
		r.True(created.IsSupplier)

		var updated inventree.Company
		r.NoError(fixture.client.Patch(ctx, "/api/company/"+strconv.Itoa(created.PK)+"/", inventree.PatchFields{
			"description": inventree.Set("patched through low-level helper"),
		}, &updated))
		r.Equal(created.PK, updated.PK)
		r.Equal("patched through low-level helper", updated.Description)
	})

	t.Run("stock", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		location := fixture.ensure(t, testenv.FixtureLocation)
		part := fixture.ensure(t, testenv.FixturePart)
		stockItem, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 7})
		r.NoError(err)
		r.NotZero(stockItem.PK)
		r.Equal(part.ID, stockItem.Part)
		r.NotNil(stockItem.Location)
		r.Equal(location.ID, *stockItem.Location)
		r.Equal(float64(7), stockItem.Quantity)

		locations, err := fixture.client.SearchStockLocations(ctx, inventree.SearchQuery{Search: location.Name})
		r.NoError(err)
		r.NotEmpty(locations)
		r.Equal(location.ID, locations[0].PK)
		gotLocation, err := fixture.client.GetStockLocation(ctx, location.ID)
		r.NoError(err)
		r.Equal(location.Name, gotLocation.Name)

		stockItems, err := fixture.client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: part.ID, LocationID: location.ID})
		r.NoError(err)
		r.NotEmpty(stockItems)
		r.Equal(stockItem.PK, stockItems[0].PK)
		r.Equal(part.ID, stockItems[0].Part)
		r.NotNil(stockItems[0].Location)
		r.Equal(location.ID, *stockItems[0].Location)
		r.Equal(float64(7), stockItems[0].Quantity)
	})

	t.Run("stock_location_and_metadata_administration", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		parent := fixture.ensure(t, testenv.FixtureLocation)
		owner := fixture.ensure(t, testenv.FixtureSupplier)
		part := fixture.ensure(t, testenv.FixturePart)
		typeName, err := fixture.run.Name("location-type")
		r.NoError(err)
		var locationType inventree.StockLocationType
		r.NoError(fixture.client.Post(ctx, "/api/stock/location-type/", map[string]any{"name": typeName, "description": "F-S21 integration location type", "icon": ""}, &locationType))
		r.NotZero(locationType.PK)

		name, err := fixture.run.Name("child-location")
		r.NoError(err)
		created, err := fixture.client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: name, Description: dvgoutils.Ptr("created by F-S21 integration"), Parent: &parent.ID, Owner: &owner.ID, Structural: dvgoutils.Ptr(false), External: dvgoutils.Ptr(false), LocationType: &locationType.PK})
		r.NoError(err)
		r.NotZero(created.PK)
		r.Equal(name, created.Name)
		r.Equal(parent.ID, *created.Parent)
		r.Equal(owner.ID, *created.Owner)
		r.Equal(locationType.PK, *created.LocationType)

		page, err := fixture.client.SearchStockLocationsPage(ctx, inventree.StockLocationQuery{Parent: &parent.ID, PathDetail: dvgoutils.Ptr(true), Limit: 100})
		r.NoError(err)
		r.Contains(stockLocationIDs(page.Results), created.PK)
		types, err := fixture.client.SearchStockLocationTypes(ctx, inventree.SearchQuery{Search: typeName, Limit: 100})
		r.NoError(err)
		r.Contains(stockLocationTypeIDs(types), locationType.PK)
		gotType, err := fixture.client.GetStockLocationType(ctx, locationType.PK)
		r.NoError(err)
		r.Equal(typeName, gotType.Name)

		updated, err := fixture.client.UpdateStockLocation(ctx, created.PK, inventree.PatchFields{"description": inventree.Set("updated by F-S21 integration"), "owner": inventree.Null(), "location_type": inventree.Null(), "external": inventree.Set(true)})
		r.NoError(err)
		r.Equal("updated by F-S21 integration", updated.Description)
		r.Nil(updated.Owner)
		r.Nil(updated.LocationType)
		r.True(updated.External)

		stock, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: created.PK, Quantity: 2})
		r.NoError(err)
		stock, err = fixture.client.UpdateStockItem(ctx, stock.PK, inventree.PatchFields{"batch": inventree.Set("F-S21-BATCH"), "expiry_date": inventree.Set("2027-01-02"), "packaging": inventree.Set("tray"), "notes": inventree.Set("F-S21 metadata"), "link": inventree.Set("https://example.test/stock")})
		r.NoError(err)
		r.Equal("F-S21-BATCH", *stock.Batch)
		r.Equal("2027-01-02", *stock.ExpiryDate)
		r.Equal("tray", *stock.Packaging)
		r.Equal("F-S21 metadata", *stock.Notes)
		r.Equal("https://example.test/stock", stock.Link)
	})

	t.Run("stock_location_effective_icon", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		parent := fixture.ensure(t, testenv.FixtureLocation)

		typeName, err := fixture.run.Name("icon-location-type")
		r.NoError(err)
		locationType, err := fixture.client.CreateStockLocationType(ctx, inventree.StockLocationTypeCreate{Name: typeName, Icon: "ti:box:outline"})
		r.NoError(err)
		r.NotZero(locationType.PK)

		name, err := fixture.run.Name("icon-location")
		r.NoError(err)
		created, err := fixture.client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: name, Parent: &parent.ID, LocationType: &locationType.PK})
		r.NoError(err)

		// F-S67: a location with no custom_icon falls back to its
		// location type's icon as the InvenTree-computed effective icon.
		withType, err := fixture.client.GetStockLocation(ctx, created.PK)
		r.NoError(err)
		a.Nil(withType.CustomIcon)
		a.Equal("ti:box:outline", withType.Icon)

		customIcon := "ti:archive:filled"
		_, err = fixture.client.UpdateStockLocation(ctx, created.PK, inventree.PatchFields{"custom_icon": inventree.Set(customIcon)})
		r.NoError(err)

		// A configured custom_icon takes precedence over the location
		// type's icon in the effective icon InvenTree reports.
		withCustomIcon, err := fixture.client.GetStockLocation(ctx, created.PK)
		r.NoError(err)
		r.NotNil(withCustomIcon.CustomIcon)
		a.Equal(customIcon, *withCustomIcon.CustomIcon)
		a.Equal(customIcon, withCustomIcon.Icon)
	})

	t.Run("stock_location_type_administration", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		parent := fixture.ensure(t, testenv.FixtureLocation)

		name, err := fixture.run.Name("admin-location-type")
		r.NoError(err)
		created, err := fixture.client.CreateStockLocationType(ctx, inventree.StockLocationTypeCreate{Name: name, Description: "F-S67 integration location type", Icon: "ti:box:outline"})
		r.NoError(err)
		r.NotZero(created.PK)
		r.Equal(name, created.Name)
		r.Equal("F-S67 integration location type", created.Description)
		r.Equal("ti:box:outline", created.Icon)

		page, err := fixture.client.SearchStockLocationTypesPage(ctx, inventree.SearchQuery{Limit: 100})
		r.NoError(err)
		r.Contains(stockLocationTypeIDs(page.Results), created.PK)

		updated, err := fixture.client.UpdateStockLocationType(ctx, created.PK, inventree.PatchFields{"description": inventree.Set("updated by F-S67 integration"), "icon": inventree.Set("")})
		r.NoError(err)
		r.Equal("updated by F-S67 integration", updated.Description)
		r.Empty(updated.Icon)

		locationName, err := fixture.run.Name("admin-typed-location")
		r.NoError(err)
		typedLocation, err := fixture.client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: locationName, Parent: &parent.ID, LocationType: &created.PK})
		r.NoError(err)
		r.Equal(created.PK, *typedLocation.LocationType)

		// Live discovery (F-S67): InvenTree 1.5.0 safely SET_NULLs
		// location_type on every referencing location when the type is
		// deleted rather than refusing the delete or cascading further;
		// this is why delete_stock_location_type reports referencing
		// locations for operator review instead of blocking on them.
		r.NoError(fixture.client.DeleteStockLocationType(ctx, created.PK))
		_, err = fixture.client.GetStockLocationType(ctx, created.PK)
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		a.Equal(inventree.ErrorKindNotFound, apiErr.Kind)

		afterDelete, err := fixture.client.GetStockLocation(ctx, typedLocation.PK)
		r.NoError(err)
		a.Nil(afterDelete.LocationType)
	})

	t.Run("stock_location_delete_client_methods", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		name, err := fixture.run.Name("delete-location")
		r.NoError(err)
		created, err := fixture.client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: name})
		r.NoError(err)
		builds, err := fixture.client.SearchBuildsPage(ctx, inventree.BuildQuery{Limit: 100})
		r.NoError(err)
		r.LessOrEqual(len(builds.Results), builds.Count)
		transfers, err := fixture.client.SearchTransferOrdersPage(ctx, inventree.TransferOrderQuery{Limit: 100})
		r.NoError(err)
		r.LessOrEqual(len(transfers.Results), transfers.Count)
		orders, err := fixture.client.SearchPurchaseOrdersPage(ctx, inventree.PurchaseOrderQuery{Limit: 100})
		r.NoError(err)
		r.LessOrEqual(len(orders.Results), orders.Count)
		lines, err := fixture.client.SearchPurchaseOrderLinesPage(ctx, inventree.PurchaseOrderLineQuery{Limit: 100})
		r.NoError(err)
		r.LessOrEqual(len(lines.Results), lines.Count)
		r.NoError(fixture.client.DeleteStockLocation(ctx, created.PK))
		_, err = fixture.client.GetStockLocation(ctx, created.PK)
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		r.Equal(inventree.ErrorKindNotFound, apiErr.Kind)
	})

	t.Run("part_category_delete", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		parent := fixture.ensure(t, testenv.FixtureCategory)

		newTestCategory := func(suffix string) inventree.Category {
			name, nameErr := fixture.run.Name(suffix)
			r.NoError(nameErr)
			parentID := parent.ID
			created, createErr := fixture.client.CreatePartCategory(ctx, inventree.CategoryCreate{Name: name, Parent: &parentID})
			r.NoError(createErr)
			r.NotZero(created.PK)
			return created
		}

		// Each subsection below isolates exactly one reference surface on its
		// own leaf category, proves the corresponding bounded scan finds it,
		// then calls the raw, unguarded DeletePartCategory directly to pin
		// InvenTree 1.5.0's real behavior. delete_part_category's own guard
		// never calls DELETE while any surface is non-empty, so none of this
		// permissive behavior is ever reachable through the guarded tool; it
		// is exactly why the guard exists rather than relying on upstream.
		//
		// Live discovery: unlike the schema's undocumented zero-parameter
		// DELETE, pinned InvenTree 1.5.0 requires an explicit JSON body with
		// boolean delete_parts and delete_child_categories -- omitting either
		// is rejected with 400 "This field is required.". DeletePartCategory
		// always sends both false. Even so, InvenTree does not protect a
		// referenced category: a direct part or direct child category is
		// silently reparented one level up, to the deleted category's own
		// parent, rather than refused, destroyed, or orphaned to null; a
		// category-parameter-template link and a generic part.partcategory
		// parameter value are both cascade-deleted along with the category
		// instead. None of these four outcomes is a InvenTree-side refusal,
		// so delete_part_category's own preflight is the only thing standing
		// between an operator and an unreviewed reparent or permanent data
		// loss.

		t.Log("empty leaf category")
		emptyCategory := newTestCategory("category-delete-empty")
		r.NoError(fixture.client.DeletePartCategory(ctx, emptyCategory.PK))
		_, err := fixture.client.GetPartCategory(ctx, emptyCategory.PK)
		var notFoundErr *inventree.APIError
		r.ErrorAs(err, &notFoundErr)
		r.Equal(inventree.ErrorKindNotFound, notFoundErr.Kind)

		t.Log("direct_parts")
		partsCategory := newTestCategory("category-delete-parts")
		partName, err := fixture.run.Name("category-delete-part")
		r.NoError(err)
		part, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: partName, Category: &partsCategory.PK})
		r.NoError(err)
		r.NotZero(part.PK)
		cascade := false
		partsPage, err := fixture.client.SearchPartsPage(ctx, inventree.PartQuery{CategoryID: partsCategory.PK, Cascade: &cascade})
		r.NoError(err)
		r.Len(partsPage.Results, 1)
		r.NoError(fixture.client.DeletePartCategory(ctx, partsCategory.PK), "InvenTree 1.5.0 does not refuse deleting a category with a direct part")
		afterPart, err := fixture.client.GetPart(ctx, part.PK)
		r.NoError(err, "InvenTree 1.5.0 does not destroy a direct part when its category is deleted")
		r.NotNil(afterPart.Category)
		r.Equal(parent.ID, *afterPart.Category, "InvenTree 1.5.0 reparents an orphaned part to the deleted category's own parent")

		t.Log("direct_child_categories")
		parentCategory := newTestCategory("category-delete-parent")
		childCategory := newTestCategory("category-delete-child")
		_, err = fixture.client.UpdatePartCategory(ctx, childCategory.PK, inventree.PatchFields{"parent": inventree.Set(parentCategory.PK)})
		r.NoError(err)
		childrenPage, err := fixture.client.SearchPartCategoriesPage(ctx, inventree.CategoryQuery{Parent: &parentCategory.PK})
		r.NoError(err)
		r.Len(childrenPage.Results, 1)
		r.NoError(fixture.client.DeletePartCategory(ctx, parentCategory.PK), "InvenTree 1.5.0 does not refuse deleting a category with a direct child category")
		afterChild, err := fixture.client.GetPartCategory(ctx, childCategory.PK)
		r.NoError(err, "InvenTree 1.5.0 does not destroy a direct child category when its parent is deleted")
		r.NotNil(afterChild.Parent)
		r.Equal(parent.ID, *afterChild.Parent, "InvenTree 1.5.0 reparents an orphaned child category to the deleted category's own parent")

		t.Log("category_parameter_template_links")
		templateLinkCategory := newTestCategory("category-delete-template-link")
		template := createParameterTemplate(t, fixture.client, fixture.run, "category-delete-template", "", "")
		link := createCategoryParameterTemplate(t, fixture.client, templateLinkCategory.PK, template.PK)
		r.NotZero(link.PK)
		fetchParent := false
		linksPage, err := fixture.client.SearchCategoryParameterTemplatesPage(ctx, inventree.CategoryParameterTemplateQuery{CategoryID: templateLinkCategory.PK, FetchParent: &fetchParent})
		r.NoError(err)
		r.Len(linksPage.Results, 1)
		r.NoError(fixture.client.DeletePartCategory(ctx, templateLinkCategory.PK), "InvenTree 1.5.0 does not refuse deleting a category with a category-parameter-template link")
		_, err = fixture.client.GetCategoryParameterTemplate(ctx, link.PK)
		r.ErrorAs(err, &notFoundErr, "InvenTree 1.5.0 cascade-deletes the category-parameter-template link along with its category")
		r.Equal(inventree.ErrorKindNotFound, notFoundErr.Kind)

		t.Log("generic_parameter_values")
		parameterValueCategory := newTestCategory("category-delete-parameter-value")
		genericTemplate := createParameterTemplate(t, fixture.client, fixture.run, "category-delete-generic-template", "", "")
		parameterValue, err := fixture.client.CreatePartParameter(ctx, inventree.ParameterCreate{Template: genericTemplate.PK, ModelType: "part.partcategory", ModelID: parameterValueCategory.PK, Data: "1"})
		r.NoError(err)
		r.NotZero(parameterValue.PK)
		valuesPage, err := fixture.client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: "part.partcategory", ModelID: parameterValueCategory.PK})
		r.NoError(err)
		r.Len(valuesPage.Results, 1)
		r.NoError(fixture.client.DeletePartCategory(ctx, parameterValueCategory.PK), "InvenTree 1.5.0 does not refuse deleting a category with a generic parameter value")
		_, err = fixture.client.GetPartParameter(ctx, parameterValue.PK)
		r.ErrorAs(err, &notFoundErr, "InvenTree 1.5.0 cascade-deletes the generic parameter value along with its category")
		r.Equal(inventree.ErrorKindNotFound, notFoundErr.Kind)
	})

	t.Run("stock_item_detail", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		root := fixture.ensure(t, testenv.FixtureLocation)
		part := fixture.ensure(t, testenv.FixturePart)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)

		childName, err := fixture.run.Name("stock-detail-child")
		r.NoError(err)
		child, err := fixture.client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: childName, Parent: &root.ID})
		r.NoError(err)
		r.NotZero(child.PK)

		created, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: child.PK, Quantity: 3})
		r.NoError(err)
		r.NotZero(created.PK)

		bare, err := fixture.client.GetStockItemDetail(ctx, created.PK)
		r.NoError(err)
		r.Equal(created.PK, bare.PK)
		a.Nil(bare.SKU)
		a.Nil(bare.MPN)
		a.Nil(bare.SalesOrder)
		a.Nil(bare.SalesOrderReference)
		r.Len(bare.LocationPath, 2)
		a.Equal(root.Name, bare.LocationPath[0].Name)
		a.Equal(childName, bare.LocationPath[len(bare.LocationPath)-1].Name)

		_, err = fixture.client.UpdateStockItem(ctx, created.PK, inventree.PatchFields{"supplier_part": inventree.Set(supplierPart.ID), "expiry_date": inventree.Set("2020-01-01")})
		r.NoError(err)

		detail, err := fixture.client.GetStockItemDetail(ctx, created.PK)
		r.NoError(err)
		r.NotNil(detail.SKU)
		a.Equal(supplierPart.Name, *detail.SKU)
		a.Nil(detail.MPN)
		r.NotNil(detail.Expired)
		a.True(*detail.Expired)

		// Live discovery: omitting `location` on create does not leave the
		// item locationless — pinned InvenTree 1.5.0 falls back to the
		// part's default_location. This still exercises a real
		// non-omitted, single-segment location_path.
		var raw json.RawMessage
		r.NoError(fixture.client.Post(ctx, "/api/stock/", map[string]any{"part": part.ID, "quantity": 1}, &raw))
		var defaulted inventree.StockItem
		if err := json.Unmarshal(raw, &defaulted); err != nil {
			var batch []inventree.StockItem
			r.NoError(json.Unmarshal(raw, &batch))
			r.NotEmpty(batch)
			defaulted = batch[0]
		}
		r.NotZero(defaulted.PK)
		withDefaultLocation, err := fixture.client.GetStockItemDetail(ctx, defaulted.PK)
		r.NoError(err)
		r.NotNil(withDefaultLocation.Location)
		r.Len(withDefaultLocation.LocationPath, 1)
		a.Equal(root.Name, withDefaultLocation.LocationPath[0].Name)

		// A stock item with location explicitly cleared is the genuine
		// null-location, empty-location_path state.
		_, err = fixture.client.UpdateStockItem(ctx, defaulted.PK, inventree.PatchFields{"location": inventree.Null()})
		r.NoError(err)
		noLocation, err := fixture.client.GetStockItemDetail(ctx, defaulted.PK)
		r.NoError(err)
		a.Nil(noLocation.Location)
		a.Empty(noLocation.LocationPath)
	})

	t.Run("parameter", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		part := fixture.ensure(t, testenv.FixturePart)
		template := createParameterTemplate(t, fixture.client, fixture.run, "Resistance", "ohm", "10k,22k")
		categoryTemplate := createCategoryParameterTemplate(t, fixture.client, category.ID, template.PK)
		parameter, err := fixture.client.CreatePartParameter(ctx, inventree.NewPartParameter(part.ID, template.PK, "10k"))
		r.NoError(err)
		r.NotZero(parameter.PK)
		r.Equal("part.part", parameter.ModelType)
		r.Equal(part.ID, parameter.ModelID)
		r.Equal(template.PK, parameter.Template)
		r.Equal("10k", parameter.Data)

		updated, err := fixture.client.UpdatePartParameter(ctx, parameter.PK, inventree.PatchFields{"data": inventree.Set("22k")})
		r.NoError(err)
		r.Equal(parameter.PK, updated.PK)
		r.Equal("22k", updated.Data)

		parameters, err := fixture.client.SearchPartParameters(ctx, inventree.PartParameterQuery{PartID: part.ID})
		r.NoError(err)
		r.NotEmpty(parameters)
		r.Equal(parameter.PK, parameters[0].PK)
		r.Equal("part.part", parameters[0].ModelType)
		r.Equal(part.ID, parameters[0].ModelID)
		r.Equal("22k", parameters[0].Data)
		parameterPage, err := fixture.client.SearchPartParametersPage(ctx, inventree.PartParameterQuery{PartID: part.ID, TemplateID: template.PK, Search: "22k", Limit: 1})
		r.NoError(err)
		r.NotEmpty(parameterPage.Results)
		r.Equal(parameter.PK, parameterPage.Results[0].PK)

		gotParameter, err := fixture.client.GetPartParameter(ctx, parameter.PK)
		r.NoError(err)
		r.Equal(parameter.PK, gotParameter.PK)
		r.Equal(part.ID, gotParameter.ModelID)

		templates, err := fixture.client.SearchParameterTemplates(ctx, inventree.SearchQuery{Search: template.Name})
		r.NoError(err)
		r.NotEmpty(templates)
		r.Equal(template.PK, templates[0].PK)
		r.Equal("10k,22k", templates[0].Choices)
		templatePage, err := fixture.client.SearchParameterTemplatesPage(ctx, inventree.SearchQuery{Search: template.Name, Limit: 100})
		r.NoError(err)
		r.NotEmpty(templatePage.Results)
		r.Equal(template.PK, templatePage.Results[0].PK)

		gotTemplate, err := fixture.client.GetParameterTemplate(ctx, template.PK)
		r.NoError(err)
		r.Equal(template.PK, gotTemplate.PK)
		r.True(gotTemplate.Enabled)

		categoryTemplatePage, err := fixture.client.SearchCategoryParameterTemplatesPage(ctx, inventree.CategoryParameterTemplateQuery{CategoryID: category.ID, FetchParent: dvgoutils.Ptr(false), Limit: 100})
		r.NoError(err)
		r.NotEmpty(categoryTemplatePage.Results)
		r.Equal(categoryTemplate.PK, categoryTemplatePage.Results[0].PK)
		r.Equal(category.ID, categoryTemplatePage.Results[0].Category)
		r.Equal(template.PK, categoryTemplatePage.Results[0].Template)

		r.NoError(fixture.client.DeletePartParameter(ctx, parameter.PK))
		_, err = fixture.client.GetPartParameter(ctx, parameter.PK)
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		r.Equal(inventree.ErrorKindNotFound, apiErr.Kind)
	})

	t.Run("parameter_template_administration", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		name, err := fixture.run.Name("template-admin")
		r.NoError(err)
		created, err := fixture.client.CreateParameterTemplate(ctx, inventree.ParameterTemplateCreate{
			Name: name, Units: "ohm", Description: "integration template", ModelType: "part.part", Checkbox: false, Choices: "10k,22k", Enabled: true,
		})
		r.NoError(err)
		r.NotZero(created.PK)
		updated, err := fixture.client.UpdateParameterTemplate(ctx, created.PK, inventree.PatchFields{"description": inventree.Set("updated integration template"), "choices": inventree.Set("")})
		r.NoError(err)
		r.Equal("updated integration template", updated.Description)
		r.Empty(updated.Choices)
		page, err := fixture.client.SearchTemplateParametersPage(ctx, inventree.TemplateParameterQuery{TemplateID: created.PK, Limit: 100})
		r.NoError(err)
		r.Empty(page.Results)
		r.NoError(fixture.client.DeleteParameterTemplate(ctx, created.PK))
		_, err = fixture.client.GetParameterTemplate(ctx, created.PK)
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		r.Equal(inventree.ErrorKindNotFound, apiErr.Kind)
	})

	t.Run("category_parameter_default_administration", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		template := createParameterTemplate(t, fixture.client, fixture.run, "category-default-admin", "", "")
		created, err := fixture.client.CreateCategoryParameterTemplate(ctx, inventree.CategoryParameterTemplateCreate{Category: category.ID, Template: template.PK, DefaultValue: "initial"})
		r.NoError(err)
		r.NotZero(created.PK)
		r.Equal("initial", created.DefaultValue)

		got, err := fixture.client.GetCategoryParameterTemplate(ctx, created.PK)
		r.NoError(err)
		r.Equal(created.PK, got.PK)
		page, err := fixture.client.SearchCategoryParameterTemplatesPage(ctx, inventree.CategoryParameterTemplateQuery{CategoryID: category.ID, FetchParent: dvgoutils.Ptr(false), Limit: 100})
		r.NoError(err)
		r.Contains(page.Results, got)

		updated, err := fixture.client.UpdateCategoryParameterTemplate(ctx, created.PK, inventree.PatchFields{"default_value": inventree.Set("")})
		r.NoError(err)
		r.Empty(updated.DefaultValue)
		r.NoError(fixture.client.DeleteCategoryParameterTemplate(ctx, created.PK))
		_, err = fixture.client.GetCategoryParameterTemplate(ctx, created.PK)
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		r.Equal(inventree.ErrorKindNotFound, apiErr.Kind)
	})

	t.Run("object_parameter", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		location := fixture.ensure(t, testenv.FixtureLocation)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		template := createParameterTemplate(t, fixture.client, fixture.run, "object-parameter", "", "")

		locationParameter, err := fixture.client.CreatePartParameter(ctx, inventree.ParameterCreate{Template: template.PK, ModelType: "stock.stocklocation", ModelID: location.ID, Data: "shelf-a"})
		r.NoError(err)
		r.Equal("stock.stocklocation", locationParameter.ModelType)
		r.Equal(location.ID, locationParameter.ModelID)

		companyParameter, err := fixture.client.CreatePartParameter(ctx, inventree.ParameterCreate{Template: template.PK, ModelType: "company.company", ModelID: supplier.ID, Data: "vendor-code"})
		r.NoError(err)
		r.Equal("company.company", companyParameter.ModelType)
		r.Equal(supplier.ID, companyParameter.ModelID)

		locationPage, err := fixture.client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: "stock.stocklocation", ModelID: location.ID, TemplateID: template.PK, Limit: 10})
		r.NoError(err)
		r.Len(locationPage.Results, 1)
		r.Equal(locationParameter.PK, locationPage.Results[0].PK)
		r.Equal("shelf-a", locationPage.Results[0].Data)

		companyPage, err := fixture.client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: "company.company", ModelID: supplier.ID, TemplateID: template.PK, Limit: 10})
		r.NoError(err)
		r.Len(companyPage.Results, 1)
		r.Equal(companyParameter.PK, companyPage.Results[0].PK)

		// A model_type-scoped query for one object type never returns rows belonging to another.
		scopedPage, err := fixture.client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: "stock.stocklocation", TemplateID: template.PK, Limit: 100})
		r.NoError(err)
		for _, row := range scopedPage.Results {
			r.Equal("stock.stocklocation", row.ModelType)
		}

		r.NoError(fixture.client.DeletePartParameter(ctx, locationParameter.PK))
		_, err = fixture.client.GetPartParameter(ctx, locationParameter.PK)
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		r.Equal(inventree.ErrorKindNotFound, apiErr.Kind)
		r.NoError(fixture.client.DeletePartParameter(ctx, companyParameter.PK))
	})

	t.Run("parameter_template_uniqueness", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		name, err := fixture.run.Name("uniqueness-admin")
		r.NoError(err)
		created, err := fixture.client.CreateParameterTemplate(ctx, inventree.ParameterTemplateCreate{
			Name: name, Units: "", Description: "uniqueness integration template", ModelType: "", Checkbox: false, Choices: "", Enabled: true, Unique: dvgoutils.Ptr(inventree.ParameterUniquenessModelType),
		})
		r.NoError(err)
		r.NotZero(created.PK)
		r.Equal(inventree.ParameterUniquenessModelType, created.Unique)

		updated, err := fixture.client.UpdateParameterTemplate(ctx, created.PK, inventree.PatchFields{"unique": inventree.Set(int(inventree.ParameterUniquenessGlobal))})
		r.NoError(err)
		r.Equal(inventree.ParameterUniquenessGlobal, updated.Unique)

		got, err := fixture.client.GetParameterTemplate(ctx, created.PK)
		r.NoError(err)
		r.Equal(inventree.ParameterUniquenessGlobal, got.Unique)

		r.NoError(fixture.client.DeleteParameterTemplate(ctx, created.PK))
	})

	t.Run("cross_object_tag_workflow_discovery", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)

		part := fixture.ensure(t, testenv.FixturePart)
		location := fixture.ensure(t, testenv.FixtureLocation)
		company := fixture.ensure(t, testenv.FixtureSupplier)

		tagOne, err := fixture.run.Name("fs56-tag-one")
		r.NoError(err)
		tagTwo, err := fixture.run.Name("fs56-tag-two")
		r.NoError(err)

		patchTags := func(path string, id int, tags []string) map[string]any {
			t.Helper()
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodPatch, fmt.Sprintf(path, id), nil, map[string]any{"tags": tags})
			r.NoError(reqErr)
			var out map[string]any
			r.NoError(fixture.client.DoJSON(req, &out))
			return out
		}
		searchTags := func(query url.Values) []map[string]any {
			t.Helper()
			if query == nil {
				query = url.Values{}
			}
			query.Set("limit", "100")
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodGet, "/api/tag/", query, nil)
			r.NoError(reqErr)
			var out struct {
				Results []map[string]any `json:"results"`
			}
			r.NoError(fixture.client.DoJSON(req, &out))
			return out.Results
		}

		// Assign two tags to a part through the ordinary part PATCH endpoint
		// (no dedicated tags-write scope beyond the object's own write
		// permission is declared in the pinned schema) and confirm exact
		// read-back matches.
		partAfterTag := patchTags("/api/part/%d/", part.ID, []string{tagOne, tagTwo})
		a.ElementsMatch([]any{tagOne, tagTwo}, partAfterTag["tags"])
		// A plain detail GET omits the "tags" field entirely; only the
		// undocumented "?tags=true" query flag includes it. get_part must add
		// that flag to expose tags at all.
		getReq, err := fixture.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/part/%d/", part.ID), nil, nil)
		r.NoError(err)
		var partGet map[string]any
		r.NoError(fixture.client.DoJSON(getReq, &partGet))
		_, tagsPresentWithoutFlag := partGet["tags"]
		a.False(tagsPresentWithoutFlag, "plain GET /api/part/{id}/ is expected to omit the tags field")
		getWithTagsReq, err := fixture.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/part/%d/", part.ID), url.Values{"tags": []string{"true"}}, nil)
		r.NoError(err)
		var partGetWithTags map[string]any
		r.NoError(fixture.client.DoJSON(getWithTagsReq, &partGetWithTags))
		a.ElementsMatch([]any{tagOne, tagTwo}, partGetWithTags["tags"], "GET /api/part/{id}/?tags=true must include the assigned tags")

		// Assign the SAME tag name to two other object types (stock location,
		// company) to characterize whether InvenTree tags are a single shared
		// global taxonomy or per-model-type duplicates.
		patchTags("/api/stock/location/%d/", location.ID, []string{tagOne})
		patchTags("/api/company/%d/", company.ID, []string{tagOne})

		// Confirm the "omitted unless ?tags=true" quirk observed on Part is
		// (or is not) shared by the other two tagged object types, resolving
		// F-S44's deferred company tags read-back gap.
		companyGetReq, err := fixture.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/company/%d/", company.ID), nil, nil)
		r.NoError(err)
		var companyGet map[string]any
		r.NoError(fixture.client.DoJSON(companyGetReq, &companyGet))
		_, companyTagsPresent := companyGet["tags"]
		a.False(companyTagsPresent, "plain GET /api/company/{id}/ is expected to omit the tags field")
		companyGetWithTagsReq, err := fixture.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/company/%d/", company.ID), url.Values{"tags": []string{"true"}}, nil)
		r.NoError(err)
		var companyGetWithTags map[string]any
		r.NoError(fixture.client.DoJSON(companyGetWithTagsReq, &companyGetWithTags))
		a.ElementsMatch([]any{tagOne}, companyGetWithTags["tags"], "GET /api/company/{id}/?tags=true must include the assigned tags")

		locationGetReq, err := fixture.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/stock/location/%d/", location.ID), nil, nil)
		r.NoError(err)
		var locationGet map[string]any
		r.NoError(fixture.client.DoJSON(locationGetReq, &locationGet))
		_, locationTagsPresent := locationGet["tags"]
		a.False(locationTagsPresent, "plain GET /api/stock/location/{id}/ is expected to omit the tags field")
		locationGetWithTagsReq, err := fixture.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/stock/location/%d/", location.ID), url.Values{"tags": []string{"true"}}, nil)
		r.NoError(err)
		var locationGetWithTags map[string]any
		r.NoError(fixture.client.DoJSON(locationGetWithTagsReq, &locationGetWithTags))
		a.ElementsMatch([]any{tagOne}, locationGetWithTags["tags"], "GET /api/stock/location/{id}/?tags=true must include the assigned tags")

		unscoped := searchTags(url.Values{"search": []string{tagOne}})
		t.Logf("unscoped /api/tag/?search=%s rows: %v", tagOne, unscoped)
		r.Len(unscoped, 1, "expected exactly one shared Tag row for the same tag name across object types")
		tagOnePK := unscoped[0]["pk"]
		tagOneSlug, _ := unscoped[0]["slug"].(string)
		r.NotEmpty(tagOneSlug)

		partScoped := searchTags(url.Values{"model_type": []string{"part.part"}, "search": []string{tagOne}})
		locationScoped := searchTags(url.Values{"model_type": []string{"stock.stocklocation"}, "search": []string{tagOne}})
		companyScoped := searchTags(url.Values{"model_type": []string{"company.company"}, "search": []string{tagOne}})
		t.Logf("model_type-scoped rows: part=%v location=%v company=%v", partScoped, locationScoped, companyScoped)
		for _, scoped := range [][]map[string]any{partScoped, locationScoped, companyScoped} {
			r.Len(scoped, 1)
			a.Equal(tagOnePK, scoped[0]["pk"], "model_type-scoped /api/tag/ search must resolve to the same shared Tag row")
		}

		// Case-variant re-assignment: does InvenTree normalize/dedupe tag
		// names case-insensitively, or treat differently-cased strings as
		// distinct tags? Run-scoped names are always upper-cased by
		// testenv.Run.Name, so lower-case tagOne to get a genuine variant.
		lowerTagOne := strings.ToLower(tagOne)
		afterLowerPatch := patchTags("/api/part/%d/", part.ID, []string{lowerTagOne, tagTwo})
		t.Logf("part tags after re-assigning lower-cased variant %q of %q: %v", lowerTagOne, tagOne, afterLowerPatch["tags"])
		a.Contains(afterLowerPatch["tags"], tagOne, "re-assigning a case-variant of an existing tag name must resolve to the original tag's stored casing")
		a.NotContains(afterLowerPatch["tags"], lowerTagOne, "a case-variant must not create a sibling tag distinct from the existing one")
		afterLower := searchTags(url.Values{"search": []string{tagOne}})
		r.Len(afterLower, 1, "a case-insensitive re-assignment must not create a second Tag row")
		a.Equal(tagOnePK, afterLower[0]["pk"])

		// Server-side tags__name filter on the part list endpoint works for
		// tag-based selection.
		byName := searchPartsRaw(t, ctx, fixture.client, url.Values{"tags__name": []string{tagOne}})
		t.Logf("/api/part/?tags__name=%s matched IDs: %v", tagOne, rawPartIDs(byName))
		a.Contains(rawPartIDs(byName), float64(part.ID))

		// The undocumented boolean "tags" list filter on /api/part/ is NOT a
		// has-tags presence filter: both true and false exclude every part,
		// including our known-tagged part, while an unfiltered list finds it.
		// Pinned so an MCP presence design does not rely on this filter.
		byPresenceTrue := searchPartsRaw(t, ctx, fixture.client, url.Values{"tags": []string{"true"}})
		byPresenceFalse := searchPartsRaw(t, ctx, fixture.client, url.Values{"tags": []string{"false"}})
		byPresenceNone := searchPartsRaw(t, ctx, fixture.client, nil)
		t.Logf("/api/part/?tags=true: %v, ?tags=false: %v, unfiltered: %v", rawPartIDs(byPresenceTrue), rawPartIDs(byPresenceFalse), rawPartIDs(byPresenceNone))
		a.Empty(byPresenceTrue, "pinned InvenTree 1.5.2 characterization: the tags=true list filter unexpectedly excludes every part")
		a.Empty(byPresenceFalse, "pinned InvenTree 1.5.2 characterization: the tags=false list filter unexpectedly excludes every part")
		a.Contains(rawPartIDs(byPresenceNone), float64(part.ID))

		// Orphan behavior: drop every reference to tagOne and see whether the
		// shared Tag row survives with zero references or is cleaned up.
		patchTags("/api/part/%d/", part.ID, []string{tagTwo})
		patchTags("/api/stock/location/%d/", location.ID, []string{})
		patchTags("/api/company/%d/", company.ID, []string{})
		orphanReq, err := fixture.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/tag/%v/", tagOnePK), nil, nil)
		r.NoError(err)
		var orphanOut map[string]any
		r.NoError(fixture.client.DoJSON(orphanReq, &orphanOut),
			"pinned characterization: InvenTree does not auto-delete a Tag row when its last object reference is removed")
		a.Equal(tagOnePK, orphanOut["pk"])

		// Direct /api/tag/ mutation is declared staff-only (`a:staff`) in the
		// pinned schema, unlike ordinary object PATCH above. Confirm live
		// with a non-staff account that create/delete are rejected even
		// though the same account's own object writes above were not
		// specially gated.
		nonStaffClient := newNonStaffClient(t, ctx, fixture, "fs56-nonstaff", "F-S56-live-characterization-password")

		// Recreate a live Tag row (via ordinary admin object PATCH) for the
		// non-staff direct-CRUD characterization.
		tagThree, err := fixture.run.Name("fs56-tag-three")
		r.NoError(err)
		patchTags("/api/part/%d/", part.ID, []string{tagTwo, tagThree})
		tagThreeRows := searchTags(url.Values{"search": []string{tagThree}})
		r.Len(tagThreeRows, 1)
		tagThreePK := tagThreeRows[0]["pk"]

		nonStaffCreateReq, err := nonStaffClient.NewRequest(ctx, http.MethodPost, "/api/tag/", nil, map[string]any{"name": tagThree + "-direct"})
		r.NoError(err)
		nonStaffCreateErr := nonStaffClient.DoJSON(nonStaffCreateReq, nil)
		var createAPIErr *inventree.APIError
		r.ErrorAs(nonStaffCreateErr, &createAPIErr)
		a.Equal(http.StatusForbidden, createAPIErr.StatusCode)

		nonStaffDeleteReq, err := nonStaffClient.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/tag/%v/", tagThreePK), nil, nil)
		r.NoError(err)
		nonStaffDeleteErr := nonStaffClient.DoJSON(nonStaffDeleteReq, nil)
		var deleteAPIErr *inventree.APIError
		r.ErrorAs(nonStaffDeleteErr, &deleteAPIErr)
		a.Equal(http.StatusForbidden, deleteAPIErr.StatusCode)
		t.Logf("non-staff direct /api/tag/ create and delete both rejected with HTTP 403, unlike ordinary object PATCH tag assignment")
	})

	t.Run("pricing_and_price_break_workflow_discovery", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)

		part := fixture.ensure(t, testenv.FixturePart)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)

		getJSON := func(path string, query url.Values) map[string]any {
			t.Helper()
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodGet, path, query, nil)
			r.NoError(reqErr)
			var out map[string]any
			r.NoError(fixture.client.DoJSON(req, &out))
			return out
		}
		postJSON := func(path string, body map[string]any) (map[string]any, error) {
			t.Helper()
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodPost, path, nil, body)
			r.NoError(reqErr)
			var out map[string]any
			doErr := fixture.client.DoJSON(req, &out)
			return out, doErr
		}
		patchJSON := func(path string, body map[string]any) map[string]any {
			t.Helper()
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodPatch, path, nil, body)
			r.NoError(reqErr)
			var out map[string]any
			r.NoError(fixture.client.DoJSON(req, &out))
			return out
		}
		listJSON := func(path string, query url.Values) []map[string]any {
			t.Helper()
			if query == nil {
				query = url.Values{}
			}
			query.Set("limit", "100")
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodGet, path, query, nil)
			r.NoError(reqErr)
			var out struct {
				Results []map[string]any `json:"results"`
			}
			r.NoError(fixture.client.DoJSON(req, &out))
			return out.Results
		}
		deleteRow := func(path string) error {
			t.Helper()
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodDelete, path, nil, nil)
			r.NoError(reqErr)
			return fixture.client.DoJSON(req, nil)
		}
		// decimal tolerates both encodings observed live for "format: decimal"
		// fields across these endpoints: a JSON string on some rows/endpoints
		// and a bare JSON number on others (matching this package's existing
		// DecimalString.UnmarshalJSON, which was written to accept either).
		decimal := func(v any) float64 {
			t.Helper()
			switch value := v.(type) {
			case string:
				f, parseErr := strconv.ParseFloat(value, 64)
				r.NoError(parseErr)
				return f
			case float64:
				return value
			default:
				r.Fail("unexpected decimal field encoding", "got %T (%v)", v, v)
				return 0
			}
		}
		describeErr := func(err error) string {
			var apiErr *inventree.APIError
			if errors.As(err, &apiErr) {
				return fmt.Sprintf("status=%d detail=%q field_errors=%v", apiErr.StatusCode, apiErr.Detail, apiErr.FieldErrors)
			}
			return err.Error()
		}
		// requireRejected asserts err is a genuine inventree.APIError with the
		// expected HTTP status, rather than merely non-nil, so a transport
		// failure, decode error, or unexpected 5xx cannot be silently
		// mislabeled as the business-rule rejection being characterized.
		requireRejected := func(err error, wantStatus int) {
			t.Helper()
			var apiErr *inventree.APIError
			r.ErrorAs(err, &apiErr, "expected an inventree.APIError, got: %v", err)
			a.Equal(wantStatus, apiErr.StatusCode)
		}
		// pollUntilSettled bounded-polls part pricing until scheduled_for_update
		// is false, mirroring F-S60's terminal-state poll pattern.
		pollUntilSettled := func(timeout time.Duration) (map[string]any, bool) {
			t.Helper()
			deadline := time.Now().Add(timeout)
			var latest map[string]any
			for time.Now().Before(deadline) {
				latest = getJSON(fmt.Sprintf("/api/part/%d/pricing/", part.ID), nil)
				if scheduled, ok := latest["scheduled_for_update"].(bool); ok && !scheduled {
					return latest, true
				}
				time.Sleep(500 * time.Millisecond)
			}
			return latest, false
		}

		// 1. Baseline computed pricing for a part with no price/BOM/purchase
		// data at all: every computed min/max field is null. Creating the
		// SupplierPart fixture above independently schedules a pricing
		// recalculation, so scheduled_for_update is not asserted here.
		baseline := getJSON(fmt.Sprintf("/api/part/%d/pricing/", part.ID), nil)
		t.Logf("baseline part pricing (scheduled_for_update=%v): %v", baseline["scheduled_for_update"], baseline)
		a.Nil(baseline["overall_min"])
		a.Nil(baseline["overall_max"])
		a.Nil(baseline["supplier_price_min"])
		a.Nil(baseline["internal_cost_min"])
		a.Nil(baseline["sale_price_min"])

		// 2. PartInternalPriceBreak: ordinary create/list/filter/delete, plus
		// live characterization of duplicate-quantity and non-positive-price
		// acceptance (the schema's decimal pattern syntactically allows a
		// leading "-", but does not say whether InvenTree's serializer
		// enforces a business-level minimum).
		internalOne, err := postJSON("/api/part/internal-price/", map[string]any{
			"part": part.ID, "quantity": 1, "price": "10.00", "price_currency": "USD",
		})
		r.NoError(err)
		t.Logf("created internal price break: %v", internalOne)
		internalTwo, err := postJSON("/api/part/internal-price/", map[string]any{
			"part": part.ID, "quantity": 5, "price": "8.00", "price_currency": "USD",
		})
		r.NoError(err)
		internalTwoPK := internalTwo["pk"]

		_, dupErr := postJSON("/api/part/internal-price/", map[string]any{
			"part": part.ID, "quantity": 1, "price": "11.00", "price_currency": "USD",
		})
		requireRejected(dupErr, http.StatusBadRequest)
		t.Logf("pinned characterization: a second PartInternalPriceBreak at the same (part, quantity) is REJECTED: %s", describeErr(dupErr))

		zeroPriceOut, zeroPriceErr := postJSON("/api/part/internal-price/", map[string]any{
			"part": part.ID, "quantity": 100, "price": "0.00", "price_currency": "USD",
		})
		r.NoError(zeroPriceErr, "a zero-price PartInternalPriceBreak is expected to be allowed")
		t.Logf("pinned characterization: a zero-price PartInternalPriceBreak is ALLOWED: %v", zeroPriceOut)

		_, negativePriceErr := postJSON("/api/part/internal-price/", map[string]any{
			"part": part.ID, "quantity": 200, "price": "-1.00", "price_currency": "USD",
		})
		requireRejected(negativePriceErr, http.StatusBadRequest)
		t.Logf("pinned characterization: a negative-price PartInternalPriceBreak is REJECTED: %s", describeErr(negativePriceErr))

		internalRows := listJSON("/api/part/internal-price/", url.Values{"part": []string{strconv.Itoa(part.ID)}, "ordering": []string{"quantity"}})
		t.Logf("internal price rows for part %d ordered by quantity: %v", part.ID, internalRows)
		r.GreaterOrEqual(len(internalRows), 2)
		a.InDelta(1, internalRows[0]["quantity"], 0.0001)
		a.InDelta(5, internalRows[1]["quantity"], 0.0001)

		r.NoError(deleteRow(fmt.Sprintf("/api/part/internal-price/%v/", internalTwoPK)))
		afterDeleteReq, err := fixture.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/part/internal-price/%v/", internalTwoPK), nil, nil)
		r.NoError(err)
		var afterDeleteOut map[string]any
		afterDeleteErr := fixture.client.DoJSON(afterDeleteReq, &afterDeleteOut)
		t.Logf("deleted internal price row read-back (expect not-found error): %v", afterDeleteErr)
		var afterDeleteAPIErr *inventree.APIError
		r.ErrorAs(afterDeleteErr, &afterDeleteAPIErr)
		a.Equal(http.StatusNotFound, afterDeleteAPIErr.StatusCode)

		// 3. PartSalePriceBreak: first confirm the fixture's non-salable part
		// is fully rejected -- not merely a business-rule 400, but the FK's
		// own validator queryset already excludes it ("Invalid pk ... object
		// does not exist" on create, "not one of the available choices" on
		// the list filter) -- then flip the part salable and retry to
		// exercise the ordinary CRUD/list shape.
		_, saleCreateOnNonSalableErr := postJSON("/api/part/sale-price/", map[string]any{
			"part": part.ID, "quantity": 1, "price": "20.00", "price_currency": "USD",
		})
		r.Error(saleCreateOnNonSalableErr, "creating a PartSalePriceBreak against a non-salable part is expected to be rejected")
		t.Logf("pinned characterization: creating a PartSalePriceBreak on a non-salable part is REJECTED: %s", describeErr(saleCreateOnNonSalableErr))

		saleListReq, err := fixture.client.NewRequest(ctx, http.MethodGet, "/api/part/sale-price/", url.Values{"part": []string{strconv.Itoa(part.ID)}, "limit": []string{"100"}}, nil)
		r.NoError(err)
		saleListOnNonSalableErr := fixture.client.DoJSON(saleListReq, nil)
		r.Error(saleListOnNonSalableErr, "the sale-price list part filter is expected to reject a non-salable part id the same way")
		t.Logf("pinned characterization: GET /api/part/sale-price/?part=%d is REJECTED: %s", part.ID, describeErr(saleListOnNonSalableErr))

		_, err = fixture.client.UpdatePart(ctx, part.ID, inventree.PatchFields{"salable": inventree.Set(true)})
		r.NoError(err)
		saleOne, err := postJSON("/api/part/sale-price/", map[string]any{
			"part": part.ID, "quantity": 1, "price": "20.00", "price_currency": "USD",
		})
		r.NoError(err, "creating a PartSalePriceBreak against a salable part is expected to succeed")
		t.Logf("created sale price break on a now-salable part: %v", saleOne)
		saleRowsReq, err := fixture.client.NewRequest(ctx, http.MethodGet, "/api/part/sale-price/", url.Values{"part": []string{strconv.Itoa(part.ID)}, "limit": []string{"100"}}, nil)
		r.NoError(err)
		var saleRowsOut struct {
			Results []map[string]any `json:"results"`
		}
		r.NoError(fixture.client.DoJSON(saleRowsReq, &saleRowsOut))
		t.Logf("sale price rows for part %d after flipping salable=true: %v", part.ID, saleRowsOut.Results)
		r.Len(saleRowsOut.Results, 1)

		// 4. SupplierPriceBreak: created against the SupplierPart id (the
		// schema names this field "part" but its FK target is SupplierPart,
		// not Part -- confirm live) and characterize the read-only supplier
		// field plus part_detail/supplier_detail expansion.
		supplierBreak, err := postJSON("/api/company/price-break/", map[string]any{
			"part": supplierPart.ID, "quantity": 1, "price": "9.50", "price_currency": "USD",
		})
		r.NoError(err)
		t.Logf("created supplier price break: %v", supplierBreak)
		supplierBreakPK := supplierBreak["pk"]
		a.InDelta(9.50, decimal(supplierBreak["price"]), 0.0001)

		expandedSupplierBreak := getJSON(fmt.Sprintf("/api/company/price-break/%v/", supplierBreakPK), url.Values{"part_detail": []string{"true"}, "supplier_detail": []string{"true"}})
		t.Logf("expanded supplier price break: %v", expandedSupplierBreak)
		expandedPartDetail, expandedPartDetailOK := expandedSupplierBreak["part_detail"].(map[string]any)
		r.True(expandedPartDetailOK, "part_detail expansion is expected to describe the linked SupplierPart, not the base Part")
		a.Equal(supplierPart.Name, expandedPartDetail["SKU"], "part_detail's SKU is expected to match the SupplierPart fixture, proving this expansion is the SupplierPart, not the base Part")
		a.NotNil(expandedSupplierBreak["supplier_detail"])

		_, supplierDupErr := postJSON("/api/company/price-break/", map[string]any{
			"part": supplierPart.ID, "quantity": 1, "price": "9.75", "price_currency": "USD",
		})
		requireRejected(supplierDupErr, http.StatusBadRequest)
		t.Logf("pinned characterization: a second SupplierPriceBreak at the same (supplier_part, quantity) is REJECTED: %s", describeErr(supplierDupErr))

		// 5. Part pricing overrides and explicit recalculation. overall_min /
		// overall_max are documented read-only computed fields; override_min
		// / override_max / override_min_currency / override_max_currency are
		// the only writable fields besides the write-only "update" trigger.
		overridden := patchJSON(fmt.Sprintf("/api/part/%d/pricing/", part.ID), map[string]any{
			"override_min": "3.00", "override_min_currency": "USD",
			"override_max": "300.00", "override_max_currency": "USD",
		})
		t.Logf("part pricing immediately after setting overrides (before an explicit update trigger): %v", overridden)
		a.InDelta(3.00, decimal(overridden["override_min"]), 0.0001)
		a.InDelta(300.00, decimal(overridden["override_max"]), 0.0001)

		triggered := patchJSON(fmt.Sprintf("/api/part/%d/pricing/", part.ID), map[string]any{"update": true})
		t.Logf("part pricing immediately after PATCH update:true: %v", triggered)

		final, reachedTerminal := pollUntilSettled(30 * time.Second)
		t.Logf("final part pricing after bounded 30s poll for scheduled_for_update=false (reached=%t): %v", reachedTerminal, final)
		a.True(reachedTerminal, "pinned InvenTree 1.5.x/API 530 characterization: background pricing recalculation is expected to complete within 30s against the shared worker")
		// internal_cost_min/max unexpectedly stayed null above despite live
		// PartInternalPriceBreak rows: dump every global setting whose key
		// mentions pricing/internal to find the gating cause. Capture
		// PART_INTERNAL_PRICE's original value so it can be restored below --
		// this is an instance-wide (not run-scoped) setting shared with any
		// other subtest in this file that reuses the same shared instance.
		settingsRows := listJSON("/api/settings/global/", nil)
		originalPartInternalPrice := "False"
		for _, row := range settingsRows {
			key, _ := row["key"].(string)
			if key == "PART_INTERNAL_PRICE" {
				if value, ok := row["value"].(string); ok {
					originalPartInternalPrice = value
				}
			}
			if strings.Contains(key, "PRIC") || strings.Contains(key, "INTERNAL") {
				t.Logf("global setting %s = %v", key, row["value"])
			}
		}
		t.Cleanup(func() {
			cleanupCtx := context.WithoutCancel(ctx)
			var restored map[string]any
			r.NoError(fixture.client.Patch(cleanupCtx, "/api/settings/global/PART_INTERNAL_PRICE/", inventree.PatchFields{"value": inventree.Set(originalPartInternalPrice)}, &restored))
		})
		if reachedTerminal {
			a.NotNil(final["overall_min"])
			a.NotNil(final["overall_max"])
			a.Nil(final["internal_cost_min"], "pinned characterization: PartInternalPriceBreak rows do not feed overall pricing while the PART_INTERNAL_PRICE global setting is disabled (the default)")
			a.NotNil(final["supplier_price_min"], "supplier_price_min is expected to reflect the SupplierPriceBreak row created above")
			a.NotEmpty(final["currency"], "computed pricing is expected to be normalized into a single instance currency")
		}

		// 6. Currency-conversion drift: add a second internal price break in a
		// different currency, enable the PART_INTERNAL_PRICE global setting
		// that gates internal price data (found disabled by default above),
		// and re-trigger recalculation to see whether mixed-currency
		// price-break inputs convert cleanly into the single computed
		// currency or surface a conversion error/omission.
		_, err = postJSON("/api/part/internal-price/", map[string]any{
			"part": part.ID, "quantity": 10, "price": "6.00", "price_currency": "EUR",
		})
		r.NoError(err)
		patchJSON("/api/settings/global/PART_INTERNAL_PRICE/", map[string]any{"value": "True"})
		patchJSON(fmt.Sprintf("/api/part/%d/pricing/", part.ID), map[string]any{"update": true})
		afterMixedCurrency, mixedReachedTerminal := pollUntilSettled(30 * time.Second)
		t.Logf("part pricing after enabling PART_INTERNAL_PRICE, adding a EUR internal price break, and re-triggering update (reached=%t): %v", mixedReachedTerminal, afterMixedCurrency)
		if mixedReachedTerminal {
			t.Logf("internal_cost_min=%v internal_cost_max=%v currency=%v (remaining rows for part %d: USD 10.00@qty1, USD 0.00@qty100, EUR 6.00@qty10)", afterMixedCurrency["internal_cost_min"], afterMixedCurrency["internal_cost_max"], afterMixedCurrency["currency"], part.ID)
			a.NotNil(afterMixedCurrency["internal_cost_min"], "internal_cost_min is expected to populate once PART_INTERNAL_PRICE is enabled")
		}

		// Clean up the supplier price break row via its delete endpoint as a
		// stable-recovery-identity check (mirrors the internal-price delete
		// above for a second endpoint family).
		r.NoError(deleteRow(fmt.Sprintf("/api/company/price-break/%v/", supplierBreakPK)))
		afterSupplierDeleteReq, err := fixture.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/company/price-break/%v/", supplierBreakPK), nil, nil)
		r.NoError(err)
		var afterSupplierDeleteOut map[string]any
		afterSupplierDeleteErr := fixture.client.DoJSON(afterSupplierDeleteReq, &afterSupplierDeleteOut)
		t.Logf("deleted supplier price break read-back (expect not-found error): %v", afterSupplierDeleteErr)
		var afterSupplierDeleteAPIErr *inventree.APIError
		r.ErrorAs(afterSupplierDeleteErr, &afterSupplierDeleteAPIErr)
		a.Equal(http.StatusNotFound, afterSupplierDeleteAPIErr.StatusCode)
	})

	t.Run("pricing_and_price_break_client_methods", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)

		part := fixture.ensure(t, testenv.FixturePart)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)

		// PartInternalPriceBreak: create, search page, get, update, delete.
		internalCreated, err := fixture.client.CreatePartInternalPriceBreak(ctx, inventree.PartInternalPriceBreakCreate{
			Part: part.ID, Quantity: 1, Price: "10.00", PriceCurrency: "USD",
		})
		r.NoError(err)
		r.Positive(internalCreated.PK)

		internalPage, err := fixture.client.SearchPartInternalPriceBreaksPage(ctx, inventree.PartInternalPriceBreakQuery{Part: part.ID, Limit: 100})
		r.NoError(err)
		r.Len(internalPage.Results, 1)
		a.Equal(internalCreated.PK, internalPage.Results[0].PK)

		internalGot, err := fixture.client.GetPartInternalPriceBreak(ctx, internalCreated.PK)
		r.NoError(err)
		a.Equal(internalCreated.PK, internalGot.PK)

		internalUpdated, err := fixture.client.UpdatePartInternalPriceBreak(ctx, internalCreated.PK, inventree.PatchFields{"price": inventree.Set("11.00")})
		r.NoError(err)
		requireDecimalEqual(t, "11.00", internalUpdated.Price)

		r.NoError(fixture.client.DeletePartInternalPriceBreak(ctx, internalCreated.PK))
		_, err = fixture.client.GetPartInternalPriceBreak(ctx, internalCreated.PK)
		var internalDeletedErr *inventree.APIError
		r.ErrorAs(err, &internalDeletedErr, "expected not-found after DeletePartInternalPriceBreak, got %v", err)
		a.Equal(inventree.ErrorKindNotFound, internalDeletedErr.Kind)

		// PartSalePriceBreak: the part must be salable first (mirrors the
		// discovery subtest's live-confirmed requirement).
		_, err = fixture.client.UpdatePart(ctx, part.ID, inventree.PatchFields{"salable": inventree.Set(true)})
		r.NoError(err)
		saleCreated, err := fixture.client.CreatePartSalePriceBreak(ctx, inventree.PartSalePriceBreakCreate{
			Part: part.ID, Quantity: 1, Price: "20.00", PriceCurrency: "USD",
		})
		r.NoError(err)
		r.Positive(saleCreated.PK)

		salePage, err := fixture.client.SearchPartSalePriceBreaksPage(ctx, inventree.PartSalePriceBreakQuery{Part: part.ID, Limit: 100})
		r.NoError(err)
		r.Len(salePage.Results, 1)
		a.Equal(saleCreated.PK, salePage.Results[0].PK)

		saleGot, err := fixture.client.GetPartSalePriceBreak(ctx, saleCreated.PK)
		r.NoError(err)
		a.Equal(saleCreated.PK, saleGot.PK)

		saleUpdated, err := fixture.client.UpdatePartSalePriceBreak(ctx, saleCreated.PK, inventree.PatchFields{"price": inventree.Set("21.00")})
		r.NoError(err)
		requireDecimalEqual(t, "21.00", saleUpdated.Price)

		r.NoError(fixture.client.DeletePartSalePriceBreak(ctx, saleCreated.PK))
		_, err = fixture.client.GetPartSalePriceBreak(ctx, saleCreated.PK)
		var saleDeletedErr *inventree.APIError
		r.ErrorAs(err, &saleDeletedErr, "expected not-found after DeletePartSalePriceBreak, got %v", err)
		a.Equal(inventree.ErrorKindNotFound, saleDeletedErr.Kind)

		// SupplierPriceBreak: created against the SupplierPart id.
		supplierCreated, err := fixture.client.CreateSupplierPriceBreak(ctx, inventree.SupplierPriceBreakCreate{
			SupplierPart: supplierPart.ID, Quantity: 1, Price: "9.50", PriceCurrency: "USD",
		})
		r.NoError(err)
		r.Positive(supplierCreated.PK)

		supplierPage, err := fixture.client.SearchSupplierPriceBreaksPage(ctx, inventree.SupplierPriceBreakQuery{SupplierPart: supplierPart.ID, Limit: 100})
		r.NoError(err)
		r.Len(supplierPage.Results, 1)
		a.Equal(supplierCreated.PK, supplierPage.Results[0].PK)

		supplierGot, err := fixture.client.GetSupplierPriceBreak(ctx, supplierCreated.PK)
		r.NoError(err)
		a.Equal(supplierCreated.PK, supplierGot.PK)

		supplierUpdated, err := fixture.client.UpdateSupplierPriceBreak(ctx, supplierCreated.PK, inventree.PatchFields{"price": inventree.Set("9.75")})
		r.NoError(err)
		requireDecimalEqual(t, "9.75", supplierUpdated.Price)

		r.NoError(fixture.client.DeleteSupplierPriceBreak(ctx, supplierCreated.PK))
		_, err = fixture.client.GetSupplierPriceBreak(ctx, supplierCreated.PK)
		var supplierDeletedErr *inventree.APIError
		r.ErrorAs(err, &supplierDeletedErr, "expected not-found after DeleteSupplierPriceBreak, got %v", err)
		a.Equal(inventree.ErrorKindNotFound, supplierDeletedErr.Kind)

		// PartPricing: GetPartPricing, UpdatePartPricing (override fields
		// only), RefreshPartPricing (the write-only update trigger).
		baseline, err := fixture.client.GetPartPricing(ctx, part.ID)
		r.NoError(err)
		a.Equal("USD", baseline.Currency)

		overridden, err := fixture.client.UpdatePartPricing(ctx, part.ID, inventree.PatchFields{
			"override_min": inventree.Set("3.00"), "override_min_currency": inventree.Set("USD"),
		})
		r.NoError(err)
		r.NotNil(overridden.OverrideMin)
		requireDecimalEqual(t, "3.00", *overridden.OverrideMin)

		refreshed, err := fixture.client.RefreshPartPricing(ctx, part.ID)
		r.NoError(err)
		t.Logf("part pricing immediately after RefreshPartPricing (scheduled_for_update=%t): overall_min=%v overall_max=%v", refreshed.ScheduledForUpdate, refreshed.OverallMin, refreshed.OverallMax)
	})

	t.Run("barcode_workflow_discovery", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)

		part := fixture.ensure(t, testenv.FixturePart)
		location := fixture.ensure(t, testenv.FixtureLocation)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)

		stockItem, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 5})
		r.NoError(err)
		stockItemTwo, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 5})
		r.NoError(err)
		order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
		r.NoError(err)

		rawGet := func(path string) map[string]any {
			t.Helper()
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodGet, path, nil, nil)
			r.NoError(reqErr)
			var out map[string]any
			r.NoError(fixture.client.DoJSON(req, &out))
			return out
		}
		barcodeScan := func(barcode string) (map[string]any, error) {
			t.Helper()
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodPost, "/api/barcode/", nil, map[string]any{"barcode": barcode})
			r.NoError(reqErr)
			var out map[string]any
			return out, fixture.client.DoJSON(req, &out)
		}
		barcodeGenerate := func(model string, pk int) (map[string]any, error) {
			t.Helper()
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodPost, "/api/barcode/generate/", nil, map[string]any{"model": model, "pk": pk})
			r.NoError(reqErr)
			var out map[string]any
			return out, fixture.client.DoJSON(req, &out)
		}
		barcodeLink := func(fields map[string]any) (map[string]any, error) {
			t.Helper()
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodPost, "/api/barcode/link/", nil, fields)
			r.NoError(reqErr)
			var out map[string]any
			return out, fixture.client.DoJSON(req, &out)
		}
		barcodeUnlink := func(fields map[string]any) (map[string]any, error) {
			t.Helper()
			req, reqErr := fixture.client.NewRequest(ctx, http.MethodPost, "/api/barcode/unlink/", nil, fields)
			r.NoError(reqErr)
			var out map[string]any
			return out, fixture.client.DoJSON(req, &out)
		}
		barcodeHistory := func(query url.Values, asClient *inventree.Client) ([]map[string]any, error) {
			t.Helper()
			if query == nil {
				query = url.Values{}
			}
			query.Set("limit", "100")
			req, reqErr := asClient.NewRequest(ctx, http.MethodGet, "/api/barcode/history/", query, nil)
			r.NoError(reqErr)
			var out struct {
				Results []map[string]any `json:"results"`
			}
			err := asClient.DoJSON(req, &out)
			return out.Results, err
		}

		// Confirm barcode support and the active generation plugin on the
		// pinned instance before relying on generate/scan behavior below.
		enableSetting, err := fixture.client.GetGlobalSetting(ctx, "BARCODE_ENABLE")
		r.NoError(err)
		pluginSetting, err := fixture.client.GetGlobalSetting(ctx, "BARCODE_GENERATION_PLUGIN")
		r.NoError(err)
		t.Logf("BARCODE_ENABLE=%q BARCODE_GENERATION_PLUGIN=%q", enableSetting.Value, pluginSetting.Value)
		a.Equal("true", enableSetting.Value, "pinned characterization: barcode support is enabled by default")

		// Unlike tags, barcode_hash is present on a plain GET with no
		// special query flag, and starts as an empty string rather than
		// null or an omitted key.
		partGet := rawGet(fmt.Sprintf("/api/part/%d/", part.ID))
		a.Contains(partGet, "barcode_hash", "unlike tags, barcode_hash is expected on a plain GET with no query flag")
		a.Equal("", partGet["barcode_hash"], "an unassigned part starts with an empty barcode_hash")
		locationGet := rawGet(fmt.Sprintf("/api/stock/location/%d/", location.ID))
		a.Equal("", locationGet["barcode_hash"])
		stockItemGet := rawGet(fmt.Sprintf("/api/stock/%d/", stockItem.PK))
		a.Equal("", stockItemGet["barcode_hash"])
		companyGet := rawGet(fmt.Sprintf("/api/company/%d/", supplier.ID))
		a.NotContains(companyGet, "barcode_hash", "the pinned schema declares no barcode_hash property on Company; it cannot carry a barcode")

		// Generate a barcode for the stock item using the configured
		// barcode-generation plugin. The pinned schema's BarcodeGenerate
		// serializer does not document the expected "model" value format, so
		// probe several candidate spellings and assert on the specific
		// pinned result: the dotted app-label form used by tags'
		// model_type ("stock.stockitem") is rejected, while the bare
		// lowercase model name ("stockitem") is accepted.
		const dottedModelCandidate = "stock.stockitem"
		const bareModelCandidate = "stockitem"
		var generatedBarcode, acceptedModel string
		for _, model := range []string{dottedModelCandidate, bareModelCandidate, "StockItem", "stock.StockItem", "stock_stockitem"} {
			generated, genErr := barcodeGenerate(model, stockItem.PK)
			if genErr != nil {
				var genAPIErr *inventree.APIError
				if errors.As(genErr, &genAPIErr) {
					t.Logf("barcode generate with model=%q rejected: status=%d detail=%q fields=%v", model, genAPIErr.StatusCode, genAPIErr.Detail, genAPIErr.FieldErrors)
					if model == dottedModelCandidate {
						a.Equal(http.StatusBadRequest, genAPIErr.StatusCode, "pinned characterization: the dotted app-label model form is rejected, not merely a different success shape")
					}
				} else {
					t.Logf("barcode generate with model=%q failed: %v", model, genErr)
				}
				continue
			}
			generatedBarcode, _ = generated["barcode"].(string)
			acceptedModel = model
			t.Logf("barcode generate with model=%q succeeded: %v", model, generated)
			break
		}
		a.Equal(bareModelCandidate, acceptedModel, "pinned characterization: only the bare lowercase model name is accepted by /api/barcode/generate/ for a stock item")
		if generatedBarcode == "" {
			t.Logf("no candidate \"model\" value was accepted by /api/barcode/generate/ for a stock item; falling back to a locally-constructed barcode string for the remaining scan/link/unlink checks")
			generatedBarcode, err = fixture.run.Name("fs55-fallback-barcode")
			r.NoError(err)
		} else {
			afterGenerate := rawGet(fmt.Sprintf("/api/stock/%d/", stockItem.PK))
			a.Equal("", afterGenerate["barcode_hash"], "generation is expected to return text only, without assigning/linking it")
		}

		// Generic scan resolution: a matched barcode and an unmatched one.
		scanResult, scanErr := barcodeScan(generatedBarcode)
		r.NoError(scanErr, "scanning a value the generation plugin just produced is expected to resolve successfully")
		t.Logf("generic scan resolution for generated barcode: result=%v", scanResult)
		a.NotNil(scanResult["success"], "a matched scan is expected to report a success message")
		scannedStockItem, _ := scanResult["stockitem"].(map[string]any)
		r.NotNil(scannedStockItem, "a matched scan for a stock-item barcode is expected to include a \"stockitem\" match object")
		a.Equal(float64(stockItem.PK), scannedStockItem["pk"], "the matched scan must resolve to the stock item the barcode was generated for")

		unmatchedBarcode, err := fixture.run.Name("fs55-unmatched-barcode")
		r.NoError(err)
		_, unmatchedErr := barcodeScan(unmatchedBarcode)
		r.Error(unmatchedErr, "scanning a barcode with no matching object is expected to be rejected rather than returning an empty 200 match")
		var unmatchedAPIErr *inventree.APIError
		r.ErrorAs(unmatchedErr, &unmatchedAPIErr)
		a.Equal(http.StatusBadRequest, unmatchedAPIErr.StatusCode, "pinned characterization: an unmatched scan is HTTP 400, not 200-with-no-match or 404")
		t.Logf("generic scan resolution for an unmatched barcode: status=%d detail=%q fields=%v", unmatchedAPIErr.StatusCode, unmatchedAPIErr.Detail, unmatchedAPIErr.FieldErrors)

		orderGet := rawGet(fmt.Sprintf("/api/order/po/%d/", order.PK))
		a.Equal("", orderGet["barcode_hash"], "an unassigned purchase order also starts with an empty barcode_hash")

		// Explicitly link a custom barcode string to a second stock item.
		customBarcode, err := fixture.run.Name("fs55-custom-barcode")
		r.NoError(err)
		linkResult, linkErr := barcodeLink(map[string]any{"barcode": customBarcode, "stockitem": stockItemTwo.PK})
		r.NoError(linkErr)
		t.Logf("link result: %v", linkResult)
		afterLink := rawGet(fmt.Sprintf("/api/stock/%d/", stockItemTwo.PK))
		linkedHash, _ := afterLink["barcode_hash"].(string)
		a.NotEmpty(linkedHash, "linking a barcode must populate barcode_hash")
		a.NotEqual(customBarcode, linkedHash, "barcode_hash must be a derived hash, not the raw assigned barcode text")

		// Resolve the just-linked custom barcode through the same generic
		// scan endpoint used above for the plugin-generated value, since a
		// real resolve_barcode tool must primarily serve caller-assigned
		// (e.g. printed/scanned) barcodes, not only the plugin's own native
		// generated format.
		linkedScanResult, linkedScanErr := barcodeScan(customBarcode)
		r.NoError(linkedScanErr, "scanning a value that was just explicitly linked is expected to resolve successfully")
		linkedScannedStockItem, _ := linkedScanResult["stockitem"].(map[string]any)
		r.NotNil(linkedScannedStockItem)
		a.Equal(float64(stockItemTwo.PK), linkedScannedStockItem["pk"], "scanning an explicitly-linked barcode must resolve to the object it was linked to")

		// Duplicate assignment: the same raw barcode text must not be
		// assignable to a second, different object while still linked.
		_, dupErr := barcodeLink(map[string]any{"barcode": customBarcode, "purchaseorder": order.PK})
		r.Error(dupErr, "assigning an already-linked barcode to a different object is expected to be rejected")
		var dupAPIErr *inventree.APIError
		r.ErrorAs(dupErr, &dupAPIErr)
		a.Equal(http.StatusBadRequest, dupAPIErr.StatusCode)
		t.Logf("duplicate-assignment rejection: status=%d detail=%q fields=%v", dupAPIErr.StatusCode, dupAPIErr.Detail, dupAPIErr.FieldErrors)
		dupFieldErrors := fmt.Sprintf("%v", dupAPIErr.FieldErrors)
		a.Contains(dupFieldErrors, "part_detail", "pinned characterization: the duplicate-assignment rejection embeds the existing owner's complete record (including nested part_detail), not just its identity")
		a.Contains(dupFieldErrors, strconv.Itoa(stockItemTwo.PK), "the embedded owner record is expected to identify the actual conflicting stock item")

		// Unsupported-object field: the pinned BarcodeAssign schema has no
		// "company" property, so linking against one must be rejected
		// rather than silently ignored.
		unsupportedBarcode, err := fixture.run.Name("fs55-unsupported-object")
		r.NoError(err)
		_, unsupportedErr := barcodeLink(map[string]any{"barcode": unsupportedBarcode, "company": supplier.ID})
		r.Error(unsupportedErr, "linking a barcode against an unsupported object field is expected to be rejected")
		var unsupportedAPIErr *inventree.APIError
		r.ErrorAs(unsupportedErr, &unsupportedAPIErr)
		a.Equal(http.StatusBadRequest, unsupportedAPIErr.StatusCode)
		t.Logf("unsupported-object rejection: status=%d detail=%q fields=%v", unsupportedAPIErr.StatusCode, unsupportedAPIErr.Detail, unsupportedAPIErr.FieldErrors)

		// Unlink clears barcode_hash back to empty.
		unlinkResult, unlinkErr := barcodeUnlink(map[string]any{"stockitem": stockItemTwo.PK})
		r.NoError(unlinkErr)
		t.Logf("unlink result: %v", unlinkResult)
		afterUnlink := rawGet(fmt.Sprintf("/api/stock/%d/", stockItemTwo.PK))
		a.Equal("", afterUnlink["barcode_hash"], "unlinking must clear barcode_hash back to empty")

		// A released barcode text becomes assignable to a different object
		// (and a different object type) once unlinked.
		relinkResult, relinkErr := barcodeLink(map[string]any{"barcode": customBarcode, "purchaseorder": order.PK})
		r.NoError(relinkErr, "a released barcode text is expected to be assignable to a different object type")
		t.Logf("relink of released barcode to purchase order result: %v", relinkResult)

		// Scan history retention and content.
		history, histErr := barcodeHistory(nil, fixture.client)
		r.NoError(histErr)
		t.Logf("barcode scan history rows recorded so far: %d", len(history))
		for _, row := range history {
			t.Logf("history row: endpoint=%v result=%v data=%v", row["endpoint"], row["result"], row["data"])
		}
		// staffOnlyBarcode records the raw text of a scan performed only by
		// the staff/admin fixture client, so the non-staff read-scoping
		// check below can confirm whether a non-staff account's own history
		// read includes rows it did not itself generate.
		var staffOnlyBarcode string
		if len(history) == 0 {
			// Pinned characterization candidate: history stayed empty despite
			// the generate/scan/link/unlink calls above. Probe whether a
			// BARCODE_STORE_RESULTS-shaped setting gates recording.
			storeResultsReq, err := fixture.client.NewRequest(ctx, http.MethodGet, "/api/settings/global/BARCODE_STORE_RESULTS/", nil, nil)
			r.NoError(err)
			var storeResultsOut map[string]any
			storeResultsErr := fixture.client.DoJSON(storeResultsReq, &storeResultsOut)
			if storeResultsErr != nil {
				var settingAPIErr *inventree.APIError
				if errors.As(storeResultsErr, &settingAPIErr) {
					t.Logf("BARCODE_STORE_RESULTS setting lookup: status=%d detail=%q (no such setting on this pinned instance)", settingAPIErr.StatusCode, settingAPIErr.Detail)
				}
			} else {
				t.Logf("BARCODE_STORE_RESULTS=%v; scan history may require this setting enabled to record rows", storeResultsOut["value"])
				if storeResultsOut["value"] != "true" && storeResultsOut["value"] != true {
					enableStoreReq, err := fixture.client.NewRequest(ctx, http.MethodPatch, "/api/settings/global/BARCODE_STORE_RESULTS/", nil, map[string]any{"value": "true"})
					r.NoError(err)
					r.NoError(fixture.client.DoJSON(enableStoreReq, nil))
					t.Cleanup(func() {
						cleanupCtx := context.WithoutCancel(ctx)
						disableReq, err := fixture.client.NewRequest(cleanupCtx, http.MethodPatch, "/api/settings/global/BARCODE_STORE_RESULTS/", nil, map[string]any{"value": "false"})
						if err == nil {
							_ = fixture.client.DoJSON(disableReq, nil)
						}
					})

					replayBarcode, replayErr := fixture.run.Name("fs55-store-results-scan")
					r.NoError(replayErr)
					staffOnlyBarcode = replayBarcode
					replayResult, replayScanErr := barcodeScan(replayBarcode)
					t.Logf("scan after enabling BARCODE_STORE_RESULTS: result=%v err=%v", replayResult, replayScanErr)

					history, histErr = barcodeHistory(nil, fixture.client)
					r.NoError(histErr)
					t.Logf("barcode scan history rows after enabling BARCODE_STORE_RESULTS: %d", len(history))
					for _, row := range history {
						t.Logf("history row: endpoint=%v result=%v data=%v", row["endpoint"], row["result"], row["data"])
					}
					a.NotEmpty(history, "enabling BARCODE_STORE_RESULTS is expected to make the generic scan endpoint record history rows")
				}
			}
		}

		// Non-staff read/delete authorization boundary, matching the
		// staff-only (a:staff) scope declared for barcode_history_destroy
		// and barcode_history_bulk_destroy in the pinned schema.
		nonStaffClient := newNonStaffClient(t, ctx, fixture, "fs55-nonstaff", "F-S55-live-characterization-password")
		nonStaffScanBarcode, err := fixture.run.Name("fs55-nonstaff-scan")
		r.NoError(err)
		nonStaffScanReq, err := nonStaffClient.NewRequest(ctx, http.MethodPost, "/api/barcode/", nil, map[string]any{"barcode": nonStaffScanBarcode})
		r.NoError(err)
		var nonStaffScanOut map[string]any
		nonStaffScanErr := nonStaffClient.DoJSON(nonStaffScanReq, &nonStaffScanOut)
		t.Logf("non-staff generic scan result=%v err=%v", nonStaffScanOut, nonStaffScanErr)

		nonStaffHistory, nonStaffHistErr := barcodeHistory(nil, nonStaffClient)
		r.NoError(nonStaffHistErr, "pinned characterization: /api/barcode/history/ read access (g:read) is not staff-gated")
		// Re-read as staff at the same point in time (rather than reusing the
		// earlier "history" snapshot, which predates the non-staff scan just
		// above) so the comparison below reflects current state, not a stale
		// count from before the non-staff account made its own scan.
		freshStaffHistory, freshErr := barcodeHistory(nil, fixture.client)
		r.NoError(freshErr)
		t.Logf("non-staff /api/barcode/history/ read: %d rows visible (staff sees %d)", len(nonStaffHistory), len(freshStaffHistory))
		if staffOnlyBarcode == "" {
			t.Logf("BARCODE_STORE_RESULTS was already enabled before this subtest ran (shared instance), so no staff-only row exists to test read-scoping against; skipping the row-level scoping assertion")
		} else {
			nonStaffSeesStaffRow := false
			for _, row := range nonStaffHistory {
				if data, _ := row["data"].(string); data == staffOnlyBarcode {
					nonStaffSeesStaffRow = true
					break
				}
			}
			staffSeesStaffRow := false
			for _, row := range freshStaffHistory {
				if data, _ := row["data"].(string); data == staffOnlyBarcode {
					staffSeesStaffRow = true
					break
				}
			}
			r.True(staffSeesStaffRow, "sanity check: the staff account itself must still see its own recorded scan")
			// Pinned characterization: /api/barcode/history/ reads are
			// scoped per requesting user (a non-staff account sees only
			// scans it personally performed), not a shared global log
			// visible to every authenticated user.
			a.False(nonStaffSeesStaffRow, "pinned characterization: a non-staff account's own history read does not include a scan performed by a different (staff) account")
		}
		history = freshStaffHistory

		if len(history) > 0 {
			firstID, ok := history[0]["pk"].(float64)
			r.True(ok)
			nonStaffDeleteReq, err := nonStaffClient.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/barcode/history/%d/", int(firstID)), nil, nil)
			r.NoError(err)
			nonStaffDeleteErr := nonStaffClient.DoJSON(nonStaffDeleteReq, nil)
			r.Error(nonStaffDeleteErr, "deleting scan history is declared staff-only (a:staff) in the pinned schema")
			var deleteAPIErr *inventree.APIError
			r.ErrorAs(nonStaffDeleteErr, &deleteAPIErr)
			a.Equal(http.StatusForbidden, deleteAPIErr.StatusCode)

			staffDeleteReq, err := fixture.client.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/barcode/history/%d/", int(firstID)), nil, nil)
			r.NoError(err)
			r.NoError(fixture.client.DoJSON(staffDeleteReq, nil), "a staff account is expected to be able to delete a scan history row")
		}
	})

	// barcode_presence_resolution_and_assignment exercises F-S99's actual
	// production code paths (Client.GenerateBarcode/ResolveBarcode/
	// LinkBarcode/UnlinkBarcode/SearchBarcodeScanHistoryPage and the
	// has_barcode-deriving Get*Detail methods) against the same pinned
	// InvenTree instance barcode_workflow_discovery (F-S55) already
	// characterized at the raw-request level. It re-asserts the shapes this
	// story's redaction/mapping logic depends on (the duplicate-conflict and
	// no-match rejection shapes, the invalid-user-id 400) so a future
	// upstream drift is caught here, not only inferred from F-S55's history.
	t.Run("barcode_presence_resolution_and_assignment", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)

		part := fixture.ensure(t, testenv.FixturePart)
		location := fixture.ensure(t, testenv.FixtureLocation)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)

		stockItem, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 5})
		r.NoError(err)
		stockItemTwo, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 5})
		r.NoError(err)
		order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
		r.NoError(err)

		// has_barcode starts false for all four in-scope types, read through
		// the actual production Get*Detail methods this story modified.
		partDetail, err := fixture.client.GetPartDetail(ctx, part.ID)
		r.NoError(err)
		a.False(partDetail.HasBarcode)
		locationDetail, err := fixture.client.GetStockLocation(ctx, location.ID)
		r.NoError(err)
		a.False(locationDetail.HasBarcode)
		stockItemDetail, err := fixture.client.GetStockItemDetail(ctx, stockItem.PK)
		r.NoError(err)
		a.False(stockItemDetail.HasBarcode)
		orderDetail, err := fixture.client.GetPurchaseOrderDetail(ctx, order.PK)
		r.NoError(err)
		a.False(orderDetail.HasBarcode)

		// Company carries no barcode_hash property at all (confirmed live by
		// F-S55); CompanyDetail must therefore still carry no has_barcode
		// field either.
		companyDetail, err := fixture.client.GetCompanyDetail(ctx, supplier.ID)
		r.NoError(err)
		encodedCompany, err := json.Marshal(companyDetail)
		r.NoError(err)
		a.NotContains(string(encodedCompany), "has_barcode", "Company must never gain a has_barcode field; it has no upstream barcode_hash to derive one from")

		// Generate is text-only and never assigns/links it (barcode_hash
		// stays empty). Pinned characterization (F-S99, correcting the
		// original plan's assumption): InvenTree's default InvenTreeBarcode
		// plugin returns a native format ("INV-SI<pk>") that ALREADY
		// resolves back to the same object through the generic resolve
		// endpoint without ever being linked -- resolution here does not go
		// through barcode_hash at all, it decodes the native format
		// directly. Explicitly LinkBarcode-ing that exact generated text
		// back to the very object it was generated for is consequently
		// rejected with the same "Barcode matches existing item" shape used
		// for a genuine cross-object duplicate, because the link endpoint's
		// own preflight treats any object the text already resolves to as
		// already claimed. assign_barcode's real purpose is linking a
		// different (externally sourced or printed) barcode value to an
		// object, not re-linking a native-plugin-generated value to itself.
		generatedText, err := fixture.client.GenerateBarcode(ctx, "stockitem", stockItem.PK)
		r.NoError(err)
		r.NotEmpty(generatedText)
		afterGenerate, err := fixture.client.GetStockItemDetail(ctx, stockItem.PK)
		r.NoError(err)
		a.False(afterGenerate.HasBarcode, "generation must not itself assign/link the generated text")

		match, matched, err := fixture.client.ResolveBarcode(ctx, generatedText)
		r.NoError(err)
		r.True(matched, "InvenTree's default plugin's native generated format is expected to resolve immediately, without any explicit link")
		a.Equal("stockitem", match.ObjectType)
		a.Equal(stockItem.PK, match.ObjectID)
		a.NotEmpty(match.WebURL)

		selfLinkErr := fixture.client.LinkBarcode(ctx, generatedText, "stockitem", stockItem.PK)
		r.Error(selfLinkErr, "linking the native plugin's own generated text back to the object it already resolves to is expected to be rejected as an existing match")
		var selfLinkAPIErr *inventree.APIError
		r.ErrorAs(selfLinkErr, &selfLinkAPIErr)
		a.Equal(http.StatusBadRequest, selfLinkAPIErr.StatusCode)
		a.Equal([]string{"Barcode matches existing item"}, selfLinkAPIErr.FieldErrors["error"])
		afterSelfLink, err := fixture.client.GetStockItemDetail(ctx, stockItem.PK)
		r.NoError(err)
		a.False(afterSelfLink.HasBarcode, "the rejected self-link attempt must not have populated barcode_hash")

		// No-match resolve's real shape: ResolveBarcode must decode this into
		// (zero value, false, nil), not propagate an error.
		unmatchedBarcode, err := fixture.run.Name("fs99-unmatched")
		r.NoError(err)
		noMatch, matched, err := fixture.client.ResolveBarcode(ctx, unmatchedBarcode)
		r.NoError(err, "a genuine no-match must decode into (false, nil), not an error")
		a.False(matched)
		a.Empty(noMatch.ObjectType)

		// Explicit link to a second stock item, then the duplicate-conflict
		// rejection's real shape when a different object tries to claim the
		// same barcode text.
		customBarcode, err := fixture.run.Name("fs99-custom-barcode")
		r.NoError(err)
		r.NoError(fixture.client.LinkBarcode(ctx, customBarcode, "stockitem", stockItemTwo.PK))

		dupErr := fixture.client.LinkBarcode(ctx, customBarcode, "purchaseorder", order.PK)
		r.Error(dupErr, "assigning an already-linked barcode to a different object must be rejected")
		var dupAPIErr *inventree.APIError
		r.ErrorAs(dupErr, &dupAPIErr)
		a.Equal(http.StatusBadRequest, dupAPIErr.StatusCode)
		a.Equal([]string{"Barcode matches existing item"}, dupAPIErr.FieldErrors["error"])
		r.Len(dupAPIErr.FieldErrors["stockitem"], 1)
		// Pinned characterization: the embedded conflict object is a
		// JSON-encoded string (not a nested object), and DRF's nested-
		// serializer error path stringifies every field within it --
		// including "pk" (e.g. "pk":"2" rather than "pk":2) -- contrary to
		// this story's original numeric-pk assumption. redactBarcodeConflict
		// (internal/tools/barcode_tools.go) accepts both forms via
		// flexibleConflictInt; this asserts the string form directly against
		// the raw upstream shape so a future drift back to a numeric pk is
		// also caught.
		var embedded struct {
			PK     string `json:"pk"`
			WebURL string `json:"web_url"`
		}
		r.NoError(json.Unmarshal([]byte(dupAPIErr.FieldErrors["stockitem"][0]), &embedded), "the embedded conflict object is a JSON-encoded string, not a nested object")
		embeddedPK, err := strconv.Atoi(embedded.PK)
		r.NoError(err)
		a.Equal(stockItemTwo.PK, embeddedPK)
		a.NotEmpty(embedded.WebURL)

		// Unlink frees the value for a different object AND a different
		// object type.
		r.NoError(fixture.client.UnlinkBarcode(ctx, "stockitem", stockItemTwo.PK))
		afterUnlink, err := fixture.client.GetStockItemDetail(ctx, stockItemTwo.PK)
		r.NoError(err)
		a.False(afterUnlink.HasBarcode)

		r.NoError(fixture.client.LinkBarcode(ctx, customBarcode, "purchaseorder", order.PK))
		afterRelink, err := fixture.client.GetPurchaseOrderDetail(ctx, order.PK)
		r.NoError(err)
		a.True(afterRelink.HasBarcode)

		// Scan history: ensure BARCODE_STORE_RESULTS is enabled so the scans
		// below are actually recorded, restoring its original value after.
		storeResults, err := fixture.client.GetGlobalSetting(ctx, "BARCODE_STORE_RESULTS")
		r.NoError(err)
		if storeResults.Value != "true" {
			r.NoError(fixture.client.Patch(ctx, "/api/settings/global/BARCODE_STORE_RESULTS/", inventree.PatchFields{"value": inventree.Set("true")}, nil))
			originalValue := storeResults.Value
			t.Cleanup(func() {
				cleanupCtx := context.WithoutCancel(ctx)
				_ = fixture.client.Patch(cleanupCtx, "/api/settings/global/BARCODE_STORE_RESULTS/", inventree.PatchFields{"value": inventree.Set(originalValue)}, nil)
			})
		}

		searchToken, err := fixture.run.Name("fs99-history-search-token")
		r.NoError(err)
		_, _, err = fixture.client.ResolveBarcode(ctx, searchToken)
		r.NoError(err, "a no-match scan is still expected to be recorded, not fail the call")
		_, matchedAgain, err := fixture.client.ResolveBarcode(ctx, generatedText)
		r.NoError(err)
		r.True(matchedAgain)

		// search filter narrows to the exact recorded scan.
		bySearch, err := fixture.client.SearchBarcodeScanHistoryPage(ctx, inventree.BarcodeScanHistoryQuery{Search: searchToken, Limit: 50})
		r.NoError(err)
		foundSearchToken := false
		for _, row := range bySearch.Results {
			if row.Data == searchToken {
				foundSearchToken = true
				a.False(row.Result, "the unmatched scan must be recorded with result:false")
			}
		}
		a.True(foundSearchToken, "search:%q must find the just-recorded scan row", searchToken)
		require.NotEmpty(t, bySearch.Results)
		// internal/tools' bounded scan-history walk client-side-filters
		// endpoint/from/to (they are not real upstream query params) by
		// parsing each row's own Timestamp field through a small set of
		// candidate layouts (parseBarcodeScanHistoryTimestamp in
		// internal/tools/barcode_tools.go), since a prior version of that
		// parser assumed RFC3339-only and silently excluded every real row
		// (fixed in this same story). This canary reconfirms live, against
		// this exact instance, that a freshly recorded row's real Timestamp
		// still matches one of that parser's layouts, so a future InvenTree
		// format change is caught here rather than only by an idealized
		// fixture in a unit test.
		timestampParsed := false
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
			if _, parseErr := time.Parse(layout, bySearch.Results[0].Timestamp); parseErr == nil {
				timestampParsed = true
				break
			}
		}
		a.True(timestampParsed, "scan-history row Timestamp %q must match one of internal/tools' parseBarcodeScanHistoryTimestamp layouts, or search_barcode_scan_history's from/to filter will silently exclude every real row again", bySearch.Results[0].Timestamp)

		// result filter narrows to successful scans only.
		resultFilter := true
		successOnly, err := fixture.client.SearchBarcodeScanHistoryPage(ctx, inventree.BarcodeScanHistoryQuery{Result: &resultFilter, Limit: 50})
		r.NoError(err)
		for _, row := range successOnly.Results {
			a.True(row.Result, "result:true must narrow to successful scans only")
		}

		// user filter narrows to this fixture's own account's scans.
		currentUser, err := fixture.client.GetCurrentUser(ctx)
		r.NoError(err)
		byUser, err := fixture.client.SearchBarcodeScanHistoryPage(ctx, inventree.BarcodeScanHistoryQuery{UserID: &currentUser.PK, Limit: 50})
		r.NoError(err)
		r.NotEmpty(byUser.Results, "user:<this account> must find at least the scans this subtest just performed")
		for _, row := range byUser.Results {
			if row.UserID != nil {
				a.Equal(currentUser.PK, *row.UserID)
			}
		}

		// An unknown user id 400s upstream (live-confirmed).
		bogusUserID := currentUser.PK + 987654321
		_, err = fixture.client.SearchBarcodeScanHistoryPage(ctx, inventree.BarcodeScanHistoryQuery{UserID: &bogusUserID, Limit: 20})
		r.Error(err, "an unknown user id must be rejected, not silently return zero rows")
		var userAPIErr *inventree.APIError
		r.ErrorAs(err, &userAPIErr)
		a.Equal(http.StatusBadRequest, userAPIErr.StatusCode)

		// Non-staff read scoping: a non-staff account's own history read must
		// not include this subtest's own staff-performed scan. F-S55 already
		// established the underlying API behavior at the raw-request level;
		// this repeats a lighter version through the typed client method for
		// F-S99's own coverage.
		nonStaffClient := newNonStaffClient(t, ctx, fixture, "fs99-nonstaff", "F-S99-live-characterization-password")
		nonStaffHistory, err := nonStaffClient.SearchBarcodeScanHistoryPage(ctx, inventree.BarcodeScanHistoryQuery{Limit: 50})
		r.NoError(err, "pinned characterization: /api/barcode/history/ read access is not staff-gated")
		for _, row := range nonStaffHistory.Results {
			a.NotEqual(searchToken, row.Data, "a non-staff account's own history read must not include this subtest's staff-performed scan")
		}

		// Infosec residual-risk check (F-S99 review): does the user_id
		// filter's FK-existence validation run independent of g:read row
		// scoping, letting a non-staff caller use the "no such user id"
		// 400-vs-success distinction as an oracle to enumerate valid user
		// PKs it can never actually see the scans of? Query as non-staff
		// using the STAFF fixture account's own real, valid, different PK:
		// if this errored the way the truly-bogus PK above does, the filter
		// would be scoping-aware; if it succeeds (regardless of how many
		// rows come back, since those remain separately scoped), the FK
		// check is scope-independent, confirming the enumeration-oracle risk
		// this story's Decisions/Residual-risk record must document.
		_, err = nonStaffClient.SearchBarcodeScanHistoryPage(ctx, inventree.BarcodeScanHistoryQuery{UserID: &currentUser.PK, Limit: 20})
		a.NoError(err, "pinned characterization: user_id FK validation accepts any real user id regardless of the caller's own visibility into that user's scans -- a non-staff caller can use the invalid-vs-valid-id 400 distinction to enumerate real user PKs on the instance, a documented residual risk, not a bug to fix here")
	})

	t.Run("cross_object_tag_search_and_assignment", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)

		part := fixture.ensure(t, testenv.FixturePart)
		company := fixture.ensure(t, testenv.FixtureSupplier)
		location := fixture.ensure(t, testenv.FixtureLocation)
		// FixturePurchaseOrder's reference ("PO-<supplierID>") is not
		// run-prefixed like every other fixture kind (see the
		// global_search subtest's comment on this same exception), so it
		// must bypass fixture.ensure's ownership check.
		order, err := fixture.shared.EnsureFixture(ctx, fixture.account, fixture.run, testenv.FixturePurchaseOrder)
		r.NoError(err)

		sharedTag, err := fixture.run.Name("fs91-shared-tag")
		r.NoError(err)
		partOnlyTag, err := fixture.run.Name("fs91-part-only-tag")
		r.NoError(err)

		// A plain GET (no ?tags=true flag) must omit tags for PurchaseOrder
		// too, matching the already-pinned Part/Company/StockLocation
		// behavior: GetPurchaseOrderDetail's own ?tags=true flag is the only
		// reason its exact reads above and below ever see tags at all.
		var orderRawPlain map[string]any
		plainOrderReq, err := fixture.client.NewRequest(ctx, "GET", fmt.Sprintf("/api/order/po/%d/", order.ID), nil, nil)
		r.NoError(err)
		r.NoError(fixture.client.DoJSON(plainOrderReq, &orderRawPlain))
		r.NotContains(orderRawPlain, "tags", "plain GET /api/order/po/{id}/ is expected to omit the tags field")

		// Assign through the typed UpdatePart/UpdateCompany/UpdateStockLocation
		// client methods (PatchFields{"tags": Set(...)}) rather than raw
		// requests, proving the F-S91 production code path end to end.
		_, err = fixture.client.UpdatePart(ctx, part.ID, inventree.PatchFields{"tags": inventree.Set([]string{sharedTag, partOnlyTag})})
		r.NoError(err)
		_, err = fixture.client.UpdateCompany(ctx, company.ID, inventree.PatchFields{"tags": inventree.Set([]string{sharedTag})})
		r.NoError(err)
		_, err = fixture.client.UpdateStockLocation(ctx, location.ID, inventree.PatchFields{"tags": inventree.Set([]string{sharedTag})})
		r.NoError(err)
		// UpdatePurchaseOrderDetail is the one Update*Detail method that
		// requests ?tags=true on its own PATCH request rather than relying on
		// a later GetPurchaseOrderDetail read-back (update_purchase_order
		// returns this PATCH response directly); assert its response carries
		// tags immediately, not just on a follow-up GET.
		orderAfterPatch, err := fixture.client.UpdatePurchaseOrderDetail(ctx, order.ID, inventree.PatchFields{"tags": inventree.Set([]string{sharedTag})})
		r.NoError(err)
		a.Equal([]string{sharedTag}, orderAfterPatch.Tags, "UpdatePurchaseOrderDetail's own PATCH response must already carry tags")

		// Exact read-back through the ?tags=true-requesting Get*Detail methods
		// must expose the assigned tags.
		partDetail, err := fixture.client.GetPartDetail(ctx, part.ID)
		r.NoError(err)
		a.ElementsMatch([]string{sharedTag, partOnlyTag}, partDetail.Tags)
		companyDetail, err := fixture.client.GetCompanyDetail(ctx, company.ID)
		r.NoError(err)
		a.Equal([]string{sharedTag}, companyDetail.Tags)
		locationDetail, err := fixture.client.GetStockLocation(ctx, location.ID)
		r.NoError(err)
		a.Equal([]string{sharedTag}, locationDetail.Tags)
		orderDetail, err := fixture.client.GetPurchaseOrderDetail(ctx, order.ID)
		r.NoError(err)
		a.Equal([]string{sharedTag}, orderDetail.Tags)

		// search_tags backing method: unscoped search for the shared tag name
		// must resolve to exactly one row (InvenTree's tag taxonomy is
		// global, not per-model-type), matching F-S56's pinned finding.
		unscoped, err := fixture.client.SearchTagsPage(ctx, inventree.TagQuery{Search: sharedTag, Limit: 20})
		r.NoError(err)
		r.Len(unscoped.Results, 1, "expected exactly one shared Tag row across part/company/location/purchase-order")
		sharedTagPK := unscoped.Results[0].PK
		r.NotEmpty(unscoped.Results[0].Slug)

		// model_type-scoped search_tags must resolve to the very same Tag row
		// for every object type it was assigned to.
		for _, modelType := range []string{"part.part", "company.company", "stock.stocklocation", "order.purchaseorder"} {
			scoped, err := fixture.client.SearchTagsPage(ctx, inventree.TagQuery{ModelType: modelType, Search: sharedTag, Limit: 20})
			r.NoError(err)
			r.Len(scoped.Results, 1, "model_type %s", modelType)
			a.Equal(sharedTagPK, scoped.Results[0].PK, "model_type %s must resolve to the shared Tag row", modelType)
		}

		// partOnlyTag was never assigned to company/location/order, so a
		// company-scoped search for it must find nothing even though an
		// unscoped search for it still finds the part's own row.
		partOnlyUnscoped, err := fixture.client.SearchTagsPage(ctx, inventree.TagQuery{Search: partOnlyTag, Limit: 20})
		r.NoError(err)
		r.Len(partOnlyUnscoped.Results, 1)
		partOnlyCompanyScoped, err := fixture.client.SearchTagsPage(ctx, inventree.TagQuery{ModelType: "company.company", Search: partOnlyTag, Limit: 20})
		r.NoError(err)
		r.Empty(partOnlyCompanyScoped.Results, "a tag never assigned to any company must not appear in a company-scoped search")

		// Whole-array replace with explicit [] clears every tag; PATCH
		// response and read-back must both reflect the empty array, not
		// merely omit the field.
		// UpdatePart returns the concise Part projection, which has no tags
		// field to assert on directly; only the exact-read GetPartDetail
		// below proves the clear took effect.
		_, err = fixture.client.UpdatePart(ctx, part.ID, inventree.PatchFields{"tags": inventree.Set([]string{})})
		r.NoError(err)
		clearedPartDetail, err := fixture.client.GetPartDetail(ctx, part.ID)
		r.NoError(err)
		a.Empty(clearedPartDetail.Tags, "explicit tags:[] must clear every tag on read-back")
	})

	t.Run("attachment", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		comment := "Run-scoped integration fixture attachment"
		linkAttachment, err := fixture.client.CreateLinkAttachment(ctx, inventree.AttachmentCreate{
			ModelType: "part",
			ModelID:   part.ID,
			Link:      "https://example.test/datasheet.pdf",
			Comment:   &comment,
		})
		r.NoError(err)
		r.NotZero(linkAttachment.PK)
		fileAttachment, err := fixture.client.UploadAttachment(ctx, inventree.AttachmentCreate{
			ModelType:   "part",
			ModelID:     part.ID,
			Filename:    "datasheet.txt",
			ContentType: "text/plain",
			Content:     []byte("datasheet bytes"),
			Comment:     &comment,
		})
		r.NoError(err)
		r.NotZero(fileAttachment.PK)

		attachments, err := fixture.client.ListAttachments(ctx, inventree.AttachmentQuery{ModelType: "part", ModelID: part.ID})
		r.NoError(err)
		r.NotEmpty(attachments)
		r.Contains(attachmentIDs(attachments), linkAttachment.PK)
		r.Contains(attachmentIDs(attachments), fileAttachment.PK)
		gotAttachment, err := fixture.client.GetAttachmentMetadata(ctx, linkAttachment.PK)
		r.NoError(err)
		r.Equal("part", gotAttachment.ModelType)
		r.Equal(part.ID, gotAttachment.ModelID)
		_, err = fixture.client.DownloadAttachment(ctx, linkAttachment.PK, inventree.AttachmentContentOriginal, 1024)
		r.Error(err)
		r.Contains(err.Error(), "no file attachment URL")

		download, err := fixture.client.DownloadAttachment(ctx, fileAttachment.PK, inventree.AttachmentContentOriginal, 1024)
		r.NoError(err)
		r.Equal("datasheet bytes", string(download.Content))
		r.Equal(fileAttachment.PK, download.Attachment.PK)
		r.NotContains(download.SourceURL, "?")

		updated, err := fixture.client.UpdateAttachmentMetadata(ctx, linkAttachment.PK, inventree.PatchFields{
			"comment": inventree.Set("updated through client integration test"),
		})
		r.NoError(err)
		r.Equal(linkAttachment.PK, updated.PK)
		r.Equal("updated through client integration test", updated.Comment)

		deleteAttachment, err := fixture.client.CreateLinkAttachment(ctx, inventree.AttachmentCreate{
			ModelType: "part",
			ModelID:   part.ID,
			Link:      "https://example.test/delete-me.pdf",
		})
		r.NoError(err)
		r.NotZero(deleteAttachment.PK)
		r.NoError(fixture.client.DeleteAttachment(ctx, deleteAttachment.PK))
		_, err = fixture.client.GetAttachmentMetadata(ctx, deleteAttachment.PK)
		r.Error(err)
	})

	t.Run("image", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		setPartImage(t, shared.Environment().BaseURL, fixture.account.Token, part.ID, "part-image.png", tinyPNG())

		download, err := fixture.client.DownloadPartImage(ctx, part.ID, inventree.AttachmentContentOriginal, 1024)
		r.NoError(err)
		r.Equal(tinyPNG(), download.Content)
		r.Equal(part.ID, download.Part.PK)
		r.Contains(download.ContentType, "image/png")
		r.NotContains(download.SourceURL, "?")

		replacementBytes := alternateTinyPNG()
		attachment, err := fixture.client.UploadAttachment(ctx, inventree.AttachmentCreate{
			ModelType:   "part",
			ModelID:     part.ID,
			Filename:    "replacement.png",
			ContentType: "image/png",
			Content:     replacementBytes,
		})
		r.NoError(err)
		r.NotNil(attachment.Attachment)
		r.NotEmpty(*attachment.Attachment)
		downloadedAttachment, err := fixture.client.DownloadAttachment(ctx, attachment.PK, inventree.AttachmentContentOriginal, 1024)
		r.NoError(err)
		r.Equal(replacementBytes, downloadedAttachment.Content)

		thumb, err := fixture.client.SetPartPrimaryImage(ctx, part.ID, inventree.PartPrimaryImageCreate{
			Filename:    attachment.Filename,
			ContentType: downloadedAttachment.ContentType,
			Content:     downloadedAttachment.Content,
		})
		r.NoError(err)
		r.NotNil(thumb.Image)

		replacement, err := fixture.client.DownloadPartImage(ctx, part.ID, inventree.AttachmentContentOriginal, 1024)
		r.NoError(err)
		r.Equal(replacementBytes, replacement.Content)
	})

	t.Run("company_image", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		company := fixture.ensure(t, testenv.FixtureSupplier)
		before, err := fixture.client.GetCompanyDetail(ctx, company.ID)
		r.NoError(err)
		r.Nil(before.Image)

		initialBytes := tinyPNG()
		updated, err := fixture.client.SetCompanyPrimaryImage(ctx, company.ID, inventree.CompanyPrimaryImageCreate{
			Filename: "company-initial.png", ContentType: "image/png", Content: initialBytes,
		})
		r.NoError(err)
		r.Equal(company.ID, updated.PK)
		r.NotNil(updated.Image)
		initial, err := fixture.client.DownloadCompanyImage(ctx, company.ID, 1024)
		r.NoError(err)
		r.Equal(initialBytes, initial.Content)
		r.Equal(company.ID, initial.Company.PK)
		r.Contains(initial.ContentType, "image/png")
		r.NotContains(initial.SourceURL, "?")

		replacementBytes := alternateTinyPNG()
		replaced, err := fixture.client.SetCompanyPrimaryImage(ctx, company.ID, inventree.CompanyPrimaryImageCreate{
			Filename: "company-replacement.png", ContentType: "image/png", Content: replacementBytes,
		})
		r.NoError(err)
		r.Equal(company.ID, replaced.PK)
		replacement, err := fixture.client.DownloadCompanyImage(ctx, company.ID, 1024)
		r.NoError(err)
		r.Equal(replacementBytes, replacement.Content)
		r.NotEqual(initial.SourceURL, replacement.SourceURL, "InvenTree retains the prior file and selects a collision-safe replacement name")
		r.Equal(http.StatusNotFound, authenticatedMediaStatus(t, ctx, initial.SourceURL, fixture.account.Token), "InvenTree removes the prior media file after replacement")

		cleared, err := fixture.client.ClearCompanyPrimaryImage(ctx, company.ID)
		r.NoError(err)
		r.Equal(company.ID, cleared.PK)
		r.Nil(cleared.Image)
		readback, err := fixture.client.GetCompanyDetail(ctx, company.ID)
		r.NoError(err)
		r.Nil(readback.Image)
		_, err = fixture.client.DownloadCompanyImage(ctx, company.ID, 1024)
		r.ErrorIs(err, inventree.ErrCompanyImageMissing)

		r.Equal(http.StatusNotFound, authenticatedMediaStatus(t, ctx, replacement.SourceURL, fixture.account.Token), "InvenTree removes the current media file when the image association is cleared")
	})

	t.Run("po", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		destination := fixture.ensure(t, testenv.FixtureLocation)
		orderWithoutSupplierReference, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
		r.NoError(err)
		r.NotZero(orderWithoutSupplierReference.PK)
		r.NotEmpty(orderWithoutSupplierReference.Reference)
		r.Empty(orderWithoutSupplierReference.SupplierReference)
		supplierReference, err := fixture.run.Name("po")
		r.NoError(err)
		order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, SupplierReference: &supplierReference, Description: dvgoutils.Ptr("client-method integration order"), OrderCurrency: dvgoutils.Ptr("AUD")})
		r.NoError(err)
		r.NotZero(order.PK)
		r.NotEmpty(order.Reference)
		r.Equal(supplierReference, order.SupplierReference)
		lineReference, err := fixture.run.Name("po-line")
		r.NoError(err)
		line, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &lineReference, Quantity: 3, PurchasePrice: dvgoutils.Ptr("1.25"), PurchasePriceCurrency: dvgoutils.Ptr("AUD"), Destination: &destination.ID})
		r.NoError(err)
		r.NotZero(line.PK)
		r.NotNil(line.Destination)
		r.Equal(destination.ID, *line.Destination)

		orders, err := fixture.client.SearchPurchaseOrders(ctx, inventree.PurchaseOrderQuery{Supplier: supplier.ID})
		r.NoError(err)
		r.NotEmpty(orders)
		r.Contains(purchaseOrderIDs(orders), order.PK)

		gotOrder, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.Equal(order.PK, gotOrder.PK)
		r.Equal(order.Reference, gotOrder.Reference)
		r.Equal(supplier.ID, gotOrder.Supplier)

		gotOrderDetail, err := fixture.client.GetPurchaseOrderDetail(ctx, order.PK)
		r.NoError(err)
		r.Equal(order.PK, gotOrderDetail.PK)
		r.Equal(order.Reference, gotOrderDetail.Reference)
		r.NotZero(gotOrderDetail.CreatedBy.PK)
		r.NotEmpty(gotOrderDetail.SupplierName)
		r.NotNil(gotOrderDetail.LineItems)
		r.NotNil(gotOrderDetail.CompletedLines)
		r.NotNil(gotOrderDetail.StatusText)
		r.Empty(gotOrderDetail.Link)
		r.Nil(gotOrderDetail.CompleteDate, "an unissued order must preserve a null complete_date rather than a zero value")

		lines, err := fixture.client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{Order: order.PK})
		r.NoError(err)
		r.NotEmpty(lines)
		r.Contains(purchaseOrderLineIDs(lines), line.PK)
		gotLine, err := fixture.client.GetPurchaseOrderLine(ctx, line.PK)
		r.NoError(err)
		r.Equal(line.PK, gotLine.PK)
		r.Equal(supplierPart.ID, gotLine.Part)
		r.NotNil(gotLine.Destination)
		r.Equal(destination.ID, *gotLine.Destination)

		gotLineDetail, err := fixture.client.GetPurchaseOrderLineDetail(ctx, line.PK)
		r.NoError(err)
		r.Equal(line.PK, gotLineDetail.PK)
		r.NotNil(gotLineDetail.SKU)
		r.Equal(supplierPart.Name, *gotLineDetail.SKU, "sku must reflect the supplier-part fixture's SKU")
		r.NotNil(gotLineDetail.InternalPart)
		internalPart, err := fixture.client.GetPartDetail(ctx, *gotLineDetail.InternalPart)
		r.NoError(err)
		r.Equal(internalPart.Name, gotLineDetail.InternalPartName)
		r.NotNil(gotLineDetail.TotalPrice, "a priced, quantified line must expose InvenTree's computed total")
		r.Nil(gotLineDetail.BuildOrder)
		orderBeforeExtra, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.NotNil(orderBeforeExtra.TotalPrice)
		unpricedExtraReference, err := fixture.run.Name("po-extra-line-unpriced")
		r.NoError(err)
		unpricedExtra, err := fixture.client.CreatePurchaseOrderExtraLine(ctx, inventree.PurchaseOrderExtraLineCreate{Order: order.PK, Reference: unpricedExtraReference, Quantity: 1})
		r.NoError(err)
		r.Nil(unpricedExtra.Price)
		r.Nil(unpricedExtra.TotalPrice, "an extra line with no price must preserve a null total_price rather than zero")
		gotUnpricedExtra, err := fixture.client.GetPurchaseOrderExtraLine(ctx, unpricedExtra.PK)
		r.NoError(err)
		r.Nil(gotUnpricedExtra.TotalPrice, "a read of an unpriced extra line must preserve a null total_price rather than zero")
		r.NoError(fixture.client.DeletePurchaseOrderExtraLine(ctx, unpricedExtra.PK))

		extraReference, err := fixture.run.Name("po-extra-line")
		r.NoError(err)
		extra, err := fixture.client.CreatePurchaseOrderExtraLine(ctx, inventree.PurchaseOrderExtraLineCreate{Order: order.PK, Reference: extraReference, Description: dvgoutils.Ptr("informational supplier line"), Quantity: 1, Price: dvgoutils.Ptr("0"), PriceCurrency: dvgoutils.Ptr("AUD")})
		r.NoError(err)
		r.NotZero(extra.PK)
		r.NotNil(extra.Price)
		requireDecimalEqual(t, "0", *extra.Price)
		extraPage, err := fixture.client.SearchPurchaseOrderExtraLinesPage(ctx, inventree.PurchaseOrderExtraLineQuery{Order: order.PK, Search: extraReference, Limit: 100})
		r.NoError(err)
		r.Contains(purchaseOrderExtraLineIDs(extraPage.Results), extra.PK)
		gotExtra, err := fixture.client.GetPurchaseOrderExtraLine(ctx, extra.PK)
		r.NoError(err)
		r.Equal(order.PK, gotExtra.Order)
		r.Equal(extraReference, gotExtra.Reference)
		r.Zero(gotExtra.Discount, "extra-line discount defaults to zero when never set")
		orderAfterZeroExtra, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.NotNil(orderAfterZeroExtra.TotalPrice)
		r.Equal(*orderBeforeExtra.TotalPrice, *orderAfterZeroExtra.TotalPrice, "zero-priced extra line must preserve InvenTree's exact total representation")
		updatedExtra, err := fixture.client.UpdatePurchaseOrderExtraLine(ctx, extra.PK, inventree.PatchFields{"quantity": inventree.Set(2.0), "price": inventree.Set("-0.5"), "price_currency": inventree.Set("AUD")})
		r.NoError(err)
		r.Equal(2.0, updatedExtra.Quantity)
		r.NotNil(updatedExtra.Price)
		requireDecimalEqual(t, "-0.5", *updatedExtra.Price)
		r.NotNil(updatedExtra.TotalPrice, "a priced, quantified extra line must expose InvenTree's computed total")
		orderAfterDiscount, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.NotNil(orderAfterDiscount.TotalPrice)
		beforeTotal, ok := new(big.Rat).SetString(string(*orderBeforeExtra.TotalPrice))
		r.True(ok)
		afterDiscountTotal, ok := new(big.Rat).SetString(string(*orderAfterDiscount.TotalPrice))
		r.True(ok)
		r.Zero(new(big.Rat).Sub(afterDiscountTotal, beforeTotal).Cmp(big.NewRat(-1, 1)), "quantity 2 at unit price -0.5 must reduce the exact order total by 1")
		extraWithDiscount, err := fixture.client.UpdatePurchaseOrderExtraLine(ctx, extra.PK, inventree.PatchFields{"discount": inventree.Set(5.0)})
		r.NoError(err)
		r.Equal(5.0, extraWithDiscount.Discount, "extra-line discount must exact read-back the value just set")
		r.NoError(fixture.client.DeletePurchaseOrderExtraLine(ctx, extra.PK))
		_, err = fixture.client.GetPurchaseOrderExtraLine(ctx, extra.PK)
		r.Error(err)
		orderAfterExtraDelete, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.NotNil(orderAfterExtraDelete.TotalPrice)
		r.Equal(*orderBeforeExtra.TotalPrice, *orderAfterExtraDelete.TotalPrice)
		updatedOrder, err := fixture.client.UpdatePurchaseOrder(ctx, order.PK, inventree.PatchFields{"description": inventree.Set("updated client-method integration order")})
		r.NoError(err)
		r.Equal("updated client-method integration order", updatedOrder.Description)

		orderLink := "https://example.com/po/" + supplierReference
		updatedOrderDetail, err := fixture.client.UpdatePurchaseOrderDetail(ctx, order.PK, inventree.PatchFields{"notes": inventree.Set("updated via client-method integration"), "link": inventree.Set(orderLink)})
		r.NoError(err)
		r.NotNil(updatedOrderDetail.Notes)
		r.Equal("updated via client-method integration", *updatedOrderDetail.Notes)
		r.Equal(orderLink, updatedOrderDetail.Link)
		clearedOrderDetail, err := fixture.client.UpdatePurchaseOrderDetail(ctx, order.PK, inventree.PatchFields{"notes": inventree.Null()})
		r.NoError(err)
		r.Nil(clearedOrderDetail.Notes, "an explicit null PATCH must clear notes rather than storing an empty string")

		withDatesAndDestination, err := fixture.client.UpdatePurchaseOrderDetail(ctx, order.PK, inventree.PatchFields{"creation_date": inventree.Set("2026-01-01"), "start_date": inventree.Set("2026-01-02"), "target_date": inventree.Set("2026-01-03"), "destination": inventree.Set(destination.ID)})
		r.NoError(err)
		r.NotNil(withDatesAndDestination.CreationDate)
		r.NotNil(withDatesAndDestination.StartDate)
		r.NotNil(withDatesAndDestination.TargetDate)
		r.NotNil(withDatesAndDestination.Destination)
		clearedDatesAndDestination, err := fixture.client.UpdatePurchaseOrderDetail(ctx, order.PK, inventree.PatchFields{"start_date": inventree.Null(), "target_date": inventree.Null(), "destination": inventree.Null()})
		r.NoError(err)
		r.Nil(clearedDatesAndDestination.StartDate, "an explicit null PATCH must clear start_date rather than storing a zero value")
		r.Nil(clearedDatesAndDestination.TargetDate, "an explicit null PATCH must clear target_date rather than storing a zero value")
		r.Nil(clearedDatesAndDestination.Destination, "an explicit null PATCH must clear destination rather than storing a zero value")
		// Pinned InvenTree 1.5.0 behavior: unlike start_date/target_date/destination
		// above, an explicit null PATCH to creation_date does not clear it -
		// InvenTree resets it to the current date instead. update_purchase_order
		// therefore never offers a clear_creation_date flag (see
		// UpdatePurchaseOrderInput.CreationDate); this pins that upstream quirk at
		// the client layer so a future InvenTree release changing it is caught.
		creationDateResetToToday, err := fixture.client.UpdatePurchaseOrderDetail(ctx, order.PK, inventree.PatchFields{"creation_date": inventree.Null()})
		r.NoError(err)
		r.NotNil(creationDateResetToToday.CreationDate, "pinned InvenTree 1.5.0 resets creation_date to today on null rather than clearing it")

		updatedLine, err := fixture.client.UpdatePurchaseOrderLine(ctx, line.PK, inventree.PatchFields{"order": inventree.Set(order.PK), "part": inventree.Set(supplierPart.ID), "quantity": inventree.Set(4.0), "link": inventree.Set("https://example.com/line/" + lineReference), "discount": inventree.Set(10.0)})
		r.NoError(err)
		r.Equal("https://example.com/line/"+lineReference, updatedLine.Link)
		r.Equal(10.0, updatedLine.Discount)
		r.Equal(4.0, updatedLine.Quantity)
		// InvenTree 1.5.0's DELETE /api/order/po-line/{id}/ does not itself
		// restrict deletion by order status or by received quantity; the
		// tools package layers its own zero-received/no-linked-stock guard
		// on top of this permissive upstream behavior. These client-method
		// assertions pin the exact upstream contract the tool relies on.
		pendingDeleteReference, err := fixture.run.Name("po-line-delete-pending")
		r.NoError(err)
		lineDeletedWhilePending, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &pendingDeleteReference, Quantity: 1})
		r.NoError(err)
		r.NoError(fixture.client.DeletePurchaseOrderLine(ctx, lineDeletedWhilePending.PK))
		_, err = fixture.client.GetPurchaseOrderLine(ctx, lineDeletedWhilePending.PK)
		r.Error(err, "InvenTree must delete an unreceived line on a pending order")

		r.NoError(fixture.client.IssuePurchaseOrder(ctx, order.PK))
		issuedOrder, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.Equal(inventree.PurchaseOrderStatusPlaced, issuedOrder.Status)

		placedDeleteReference, err := fixture.run.Name("po-line-delete-placed")
		r.NoError(err)
		lineDeletedWhilePlaced, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &placedDeleteReference, Quantity: 1})
		r.NoError(err, "InvenTree must allow adding a line to a PLACED order")
		r.NoError(fixture.client.DeletePurchaseOrderLine(ctx, lineDeletedWhilePlaced.PK), "InvenTree must delete an unreceived line on a PLACED order")

		received, err := fixture.client.ReceivePurchaseOrder(ctx, order.PK, inventree.PurchaseOrderReceive{Items: []inventree.PurchaseOrderReceiveItem{{LineItem: line.PK, Location: &destination.ID, Quantity: "1.5", BatchCode: dvgoutils.Ptr("client-method-receipt")}}})
		r.NoError(err)
		r.NotEmpty(received)
		r.Equal(1.5, received[0].Quantity)
		r.NotNil(received[0].Location)
		r.Equal(destination.ID, *received[0].Location)
		lineAfterReceipt, err := fixture.client.GetPurchaseOrderLine(ctx, line.PK)
		r.NoError(err)
		r.Equal(1.5, lineAfterReceipt.Received)

		stockBeforeDelete, err := fixture.client.SearchStockItems(ctx, inventree.StockItemQuery{PurchaseOrderID: order.PK})
		r.NoError(err)
		r.Len(stockBeforeDelete, 1)
		receivedStockItemPK := stockBeforeDelete[0].PK

		r.NoError(fixture.client.DeletePurchaseOrderLine(ctx, line.PK), "InvenTree does not itself refuse to delete a line with received quantity")
		_, err = fixture.client.GetPurchaseOrderLine(ctx, line.PK)
		r.Error(err)

		stockAfterDelete, err := fixture.client.SearchStockItems(ctx, inventree.StockItemQuery{PurchaseOrderID: order.PK})
		r.NoError(err)
		r.Len(stockAfterDelete, 1, "the previously received stock item must survive, now orphaned from its deleted line")
		a := assert.New(t)
		a.Equal(receivedStockItemPK, stockAfterDelete[0].PK)
		a.Equal(1.5, stockAfterDelete[0].Quantity, "InvenTree does not adjust surviving stock quantity when its originating line is deleted")

		// Pin the explicit completion endpoint with upstream auto-completion
		// disabled, including the non-overridable accept_incomplete:false body.
		var originalSetting struct {
			Value *bool `json:"value"`
		}
		settingRequest, err := fixture.client.NewRequest(ctx, http.MethodGet, "/api/settings/global/PURCHASEORDER_AUTO_COMPLETE/", nil, nil)
		r.NoError(err)
		r.NoError(fixture.client.DoJSON(settingRequest, &originalSetting))
		r.NotNil(originalSetting.Value)
		var setting map[string]any
		r.NoError(fixture.client.Patch(ctx, "/api/settings/global/PURCHASEORDER_AUTO_COMPLETE/", inventree.PatchFields{"value": inventree.Set("False")}, &setting))
		t.Cleanup(func() {
			cleanupCtx := context.WithoutCancel(ctx)
			var restored map[string]any
			r.NoError(fixture.client.Patch(cleanupCtx, "/api/settings/global/PURCHASEORDER_AUTO_COMPLETE/", inventree.PatchFields{"value": inventree.Set(strconv.FormatBool(*originalSetting.Value))}, &restored))
			var restoredSetting struct {
				Value *bool `json:"value"`
			}
			restoredRequest, requestErr := fixture.client.NewRequest(cleanupCtx, http.MethodGet, "/api/settings/global/PURCHASEORDER_AUTO_COMPLETE/", nil, nil)
			r.NoError(requestErr)
			r.NoError(fixture.client.DoJSON(restoredRequest, &restoredSetting))
			r.Equal(originalSetting.Value, restoredSetting.Value)
		})
		completionOrder, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
		r.NoError(err)
		completionReference, err := fixture.run.Name("po-explicit-complete")
		r.NoError(err)
		completionLine, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: completionOrder.PK, SupplierPart: supplierPart.ID, Reference: &completionReference, Quantity: 1, Destination: &destination.ID})
		r.NoError(err)
		r.NoError(fixture.client.IssuePurchaseOrder(ctx, completionOrder.PK))
		_, err = fixture.client.ReceivePurchaseOrder(ctx, completionOrder.PK, inventree.PurchaseOrderReceive{Items: []inventree.PurchaseOrderReceiveItem{{LineItem: completionLine.PK, Location: &destination.ID, Quantity: "1"}}})
		r.NoError(err)
		fullyReceived, err := fixture.client.GetPurchaseOrder(ctx, completionOrder.PK)
		r.NoError(err)
		r.Equal(inventree.PurchaseOrderStatusPlaced, fullyReceived.Status)
		r.NoError(fixture.client.CompletePurchaseOrder(ctx, completionOrder.PK))
		explicitlyCompleted, err := fixture.client.GetPurchaseOrder(ctx, completionOrder.PK)
		r.NoError(err)
		r.Equal(inventree.PurchaseOrderStatusComplete, explicitlyCompleted.Status)

		// F-S62: pin the exact hold/issue(resume)/cancel status codes and
		// InvenTree 1.5.1/API 530's permissive transition behavior at the
		// client layer. InvenTree validates almost none of these
		// transitions itself: hold/issue succeed unconditionally from
		// PENDING or PLACED, are silent no-ops on a CANCELLED order despite
		// returning success, and cancel is refused only from COMPLETE (even
		// a partially received PLACED order can be cancelled, leaving the
		// received stock orphaned but still order-linked). The tools
		// package layers its own source-state and received-quantity guards
		// on top of this permissive upstream contract; see
		// purchase_order_lifecycle_tools.go.
		lifecycleOrder, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
		r.NoError(err)
		r.Equal(inventree.PurchaseOrderStatusPending, lifecycleOrder.Status)
		r.NoError(fixture.client.HoldPurchaseOrder(ctx, lifecycleOrder.PK))
		heldFromPending, err := fixture.client.GetPurchaseOrder(ctx, lifecycleOrder.PK)
		r.NoError(err)
		r.Equal(inventree.PurchaseOrderStatusOnHold, heldFromPending.Status, "hold succeeds unconditionally from PENDING")
		r.NoError(fixture.client.IssuePurchaseOrder(ctx, lifecycleOrder.PK))
		resumedToPlaced, err := fixture.client.GetPurchaseOrder(ctx, lifecycleOrder.PK)
		r.NoError(err)
		r.Equal(inventree.PurchaseOrderStatusPlaced, resumedToPlaced.Status, "resume reuses the issue endpoint and always lands on PLACED, even for an order held directly from PENDING")

		r.NoError(fixture.client.CancelPurchaseOrder(ctx, lifecycleOrder.PK))
		cancelledOrder, err := fixture.client.GetPurchaseOrder(ctx, lifecycleOrder.PK)
		r.NoError(err)
		r.Equal(inventree.PurchaseOrderStatusCancelled, cancelledOrder.Status)

		r.NoError(fixture.client.IssuePurchaseOrder(ctx, lifecycleOrder.PK), "InvenTree accepts issue on a CANCELLED order without an error")
		r.NoError(fixture.client.HoldPurchaseOrder(ctx, lifecycleOrder.PK), "InvenTree accepts hold on a CANCELLED order without an error")
		stillCancelled, err := fixture.client.GetPurchaseOrder(ctx, lifecycleOrder.PK)
		r.NoError(err)
		r.Equal(inventree.PurchaseOrderStatusCancelled, stillCancelled.Status, "issue/hold on a CANCELLED order are silent no-ops despite returning success")

		cancelAfterCompleteErr := fixture.client.CancelPurchaseOrder(ctx, completionOrder.PK)
		r.Error(cancelAfterCompleteErr, "InvenTree refuses to cancel a COMPLETE order")
		var cancelAPIErr *inventree.APIError
		r.ErrorAs(cancelAfterCompleteErr, &cancelAPIErr)
		r.Equal(http.StatusBadRequest, cancelAPIErr.StatusCode)
	})

	t.Run("part_relation_crud", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		part1Name, err := fixture.run.Name("relation-part-1")
		r.NoError(err)
		part2Name, err := fixture.run.Name("relation-part-2")
		r.NoError(err)
		part1, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: part1Name, Category: dvgoutils.Ptr(category.ID), Active: dvgoutils.Ptr(true)})
		r.NoError(err)
		part2, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: part2Name, Category: dvgoutils.Ptr(category.ID), Active: dvgoutils.Ptr(true)})
		r.NoError(err)

		relation, err := fixture.client.CreatePartRelation(ctx, inventree.PartRelationCreate{Part1: part1.PK, Part2: part2.PK, Note: "created"})
		r.NoError(err)
		r.NotZero(relation.PK)
		a.Equal(part1.PK, relation.Part1)
		a.Equal(part2.PK, relation.Part2)

		got, err := fixture.client.GetPartRelation(ctx, relation.PK)
		r.NoError(err)
		a.Equal(relation, got)
		for _, partID := range []int{part1.PK, part2.PK} {
			page, pageErr := fixture.client.SearchPartRelationsPage(ctx, inventree.PartRelationQuery{Part: partID, Limit: 10})
			r.NoError(pageErr)
			a.Contains(partRelationIDs(page.Results), relation.PK, "the generic part filter must match either endpoint")
		}
		forward, err := fixture.client.SearchPartRelationsPage(ctx, inventree.PartRelationQuery{Part1: part1.PK, Part2: part2.PK, Limit: 10})
		r.NoError(err)
		a.Contains(partRelationIDs(forward.Results), relation.PK)
		reverseFilter, err := fixture.client.SearchPartRelationsPage(ctx, inventree.PartRelationQuery{Part1: part2.PK, Part2: part1.PK, Limit: 10})
		r.NoError(err)
		a.NotContains(partRelationIDs(reverseFilter.Results), relation.PK, "endpoint-specific filters preserve stored direction")

		_, err = fixture.client.CreatePartRelation(ctx, inventree.PartRelationCreate{Part1: part2.PK, Part2: part1.PK, Note: "reversed duplicate"})
		r.Error(err, "InvenTree 1.5.0 must reject the same undirected pair in reversed order")
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		a.Equal(http.StatusBadRequest, apiErr.StatusCode)

		updated, err := fixture.client.UpdatePartRelation(ctx, relation.PK, inventree.PatchFields{"note": inventree.Set("updated")})
		r.NoError(err)
		a.Equal("updated", updated.Note)
		a.Equal(part1.PK, updated.Part1)
		a.Equal(part2.PK, updated.Part2)
		r.NoError(fixture.client.DeletePartRelation(ctx, relation.PK))
		_, err = fixture.client.GetPartRelation(ctx, relation.PK)
		r.Error(err)

		part1, err = fixture.client.UpdatePart(ctx, part1.PK, inventree.PatchFields{"active": inventree.Set(false)})
		r.NoError(err)
		a.False(part1.Active)
		r.NoError(fixture.client.DeletePart(ctx, part1.PK), "removing the relation must unblock an otherwise-unused inactive part")
	})

	t.Run("part_delete", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		location := fixture.ensure(t, testenv.FixtureLocation)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		manufacturer := fixture.ensure(t, testenv.FixtureManufacturer)

		newTestPart := func(suffix string, opts inventree.PartCreate) inventree.Part {
			name, nameErr := fixture.run.Name(suffix)
			r.NoError(nameErr)
			opts.Name = name
			opts.Category = &category.ID
			part, createErr := fixture.client.CreatePart(ctx, opts)
			r.NoError(createErr)
			r.NotZero(part.PK)
			return part
		}

		// Shared support parts referenced by more than one isolated
		// blocking-category case below, so each case only needs to create
		// the one relationship it is actually pinning.
		assemblySupport := newTestPart("part-delete-assembly-support", inventree.PartCreate{Assembly: dvgoutils.Ptr(true)})
		componentSupport := newTestPart("part-delete-component-support", inventree.PartCreate{Component: dvgoutils.Ptr(true)})

		customerName, err := fixture.run.Name("part-delete-customer")
		r.NoError(err)
		var customer inventree.Company
		r.NoError(fixture.client.Post(ctx, "/api/company/", map[string]any{"name": customerName, "is_customer": true}, &customer))
		r.NotZero(customer.PK)
		var salesOrder struct {
			PK        int    `json:"pk"`
			Reference string `json:"reference"`
		}
		r.NoError(fixture.client.Post(ctx, "/api/order/so/", map[string]any{"customer": customer.PK}, &salesOrder))
		r.NotZero(salesOrder.PK)

		purchaseOrder, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
		r.NoError(err)
		r.NotZero(purchaseOrder.PK)

		// Each subsection below isolates exactly one blocking category on
		// its own part, proves the Search query finds it, then calls the
		// raw, unguarded DeletePart directly to pin whether InvenTree 1.5.0
		// itself rejects that single category alone. This is the granular
		// per-condition pinning style the sibling "po" subtest above uses,
		// rather than one part carrying every category simultaneously,
		// which can only prove upstream rejects *something* without
		// isolating which reference actually caused it.

		t.Log("stock")
		stockPart := newTestPart("part-delete-stock", inventree.PartCreate{Purchaseable: dvgoutils.Ptr(true)})
		stockItem, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: stockPart.PK, Location: location.ID, Quantity: 5})
		r.NoError(err)
		r.NotZero(stockItem.PK)
		stockItems, err := fixture.client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: stockPart.PK})
		r.NoError(err)
		r.Contains(stockItemIDs(stockItems), stockItem.PK)
		deactivatePart(t, ctx, fixture.client, stockPart.PK)
		err = fixture.client.DeletePart(ctx, stockPart.PK)
		// InvenTree 1.5.0 does not merely orphan a referencing stock item: it
		// destroys it along with the part. This is exactly the silent-loss
		// risk delete_part's own stock guard exists to prevent.
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part that still has a stock item")
		_, stockErr := fixture.client.GetStockItem(ctx, stockItem.PK)
		r.Error(stockErr, "InvenTree 1.5.0 destroys the stock item along with its part rather than orphaning it")

		t.Log("bom_as_assembly")
		bomAssemblyPart := newTestPart("part-delete-bom-assembly", inventree.PartCreate{Assembly: dvgoutils.Ptr(true)})
		var ownBOMItem inventree.BomItem
		r.NoError(fixture.client.Post(ctx, "/api/bom/", map[string]any{"part": bomAssemblyPart.PK, "sub_part": componentSupport.PK, "quantity": 1}, &ownBOMItem))
		r.NotZero(ownBOMItem.PK)
		ownBOM, err := fixture.client.SearchBomItems(ctx, inventree.BomItemQuery{Part: bomAssemblyPart.PK})
		r.NoError(err)
		r.Contains(bomItemIDs(ownBOM), ownBOMItem.PK)
		deactivatePart(t, ctx, fixture.client, bomAssemblyPart.PK)
		err = fixture.client.DeletePart(ctx, bomAssemblyPart.PK)
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part that has its own BOM")

		t.Log("bom_as_component")
		bomComponentPart := newTestPart("part-delete-bom-component", inventree.PartCreate{Component: dvgoutils.Ptr(true)})
		var usesBOMItem inventree.BomItem
		r.NoError(fixture.client.Post(ctx, "/api/bom/", map[string]any{"part": assemblySupport.PK, "sub_part": bomComponentPart.PK, "quantity": 1}, &usesBOMItem))
		r.NotZero(usesBOMItem.PK)
		usesBOM, err := fixture.client.SearchBomItems(ctx, inventree.BomItemQuery{Uses: bomComponentPart.PK})
		r.NoError(err)
		r.Contains(bomItemIDs(usesBOM), usesBOMItem.PK)
		// This is the one category InvenTree 1.5.0 genuinely protects at the
		// database level, independent of the active-state rule above:
		// deleting a part while it is still used as a component elsewhere
		// is rejected outright.
		deactivatePart(t, ctx, fixture.client, bomComponentPart.PK)
		err = fixture.client.DeletePart(ctx, bomComponentPart.PK)
		r.Error(err, "InvenTree 1.5.0 must reject deleting a part used as a component in another part's BOM")
		var componentAPIErr *inventree.APIError
		r.True(errors.As(err, &componentAPIErr))
		r.Equal([]string{"Cannot delete this part as it is used in an assembly"}, componentAPIErr.FieldErrors["non_field_errors"])

		t.Log("build")
		buildPart := newTestPart("part-delete-build", inventree.PartCreate{Assembly: dvgoutils.Ptr(true)})
		var build inventree.Build
		r.NoError(fixture.client.Post(ctx, "/api/build/", map[string]any{"part": buildPart.PK, "quantity": 1}, &build))
		r.NotZero(build.PK)
		builds, err := fixture.client.SearchBuilds(ctx, inventree.BuildQuery{Part: buildPart.PK})
		r.NoError(err)
		r.Contains(buildIDs(builds), build.PK)
		deactivatePart(t, ctx, fixture.client, buildPart.PK)
		err = fixture.client.DeletePart(ctx, buildPart.PK)
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part that is the top-level part of a build")

		t.Log("purchase_order_line")
		poPart := newTestPart("part-delete-po-line", inventree.PartCreate{Purchaseable: dvgoutils.Ptr(true)})
		poName, err := fixture.run.Name("part-delete-po-line")
		r.NoError(err)
		poSupplierPart, err := fixture.client.CreateSupplierPart(ctx, inventree.SupplierPartCreate{Part: poPart.PK, Supplier: supplier.ID, SKU: poName + "-sku"})
		r.NoError(err)
		r.NotZero(poSupplierPart.PK)
		poLine, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: purchaseOrder.PK, SupplierPart: poSupplierPart.PK, Quantity: 1})
		r.NoError(err)
		r.NotZero(poLine.PK)
		// The line-level query filters by supplier-part PK, not the base
		// InvenTree Part PK (PurchaseOrderLineItem.Part is a supplier-part
		// PK on the wire); delete_part instead uses the dedicated
		// base_part filter to query directly by the base Part PK.
		poLines, err := fixture.client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{BasePart: poPart.PK})
		r.NoError(err)
		r.Contains(purchaseOrderLineIDs(poLines), poLine.PK)
		deactivatePart(t, ctx, fixture.client, poPart.PK)
		err = fixture.client.DeletePart(ctx, poPart.PK)
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part referenced by a purchase-order line")
		_, lineErr := fixture.client.GetPurchaseOrderLine(ctx, poLine.PK)
		r.NoError(lineErr, "unlike stock, InvenTree 1.5.0 leaves the purchase-order line behind, orphaned, rather than destroying it")

		t.Log("sales_order_line")
		soPart := newTestPart("part-delete-so-line", inventree.PartCreate{Purchaseable: dvgoutils.Ptr(true)})
		soPart, err = fixture.client.UpdatePart(ctx, soPart.PK, inventree.PatchFields{"salable": inventree.Set(true)})
		r.NoError(err)
		r.True(soPart.Salable)
		var salesOrderLine inventree.SalesOrderLineItem
		r.NoError(fixture.client.Post(ctx, "/api/order/so-line/", map[string]any{"order": salesOrder.PK, "part": soPart.PK, "quantity": 1}, &salesOrderLine))
		r.NotZero(salesOrderLine.PK)
		salesLines, err := fixture.client.SearchSalesOrderLines(ctx, inventree.SalesOrderLineQuery{Part: soPart.PK})
		r.NoError(err)
		r.Contains(salesOrderLineIDs(salesLines), salesOrderLine.PK)
		deactivatePart(t, ctx, fixture.client, soPart.PK)
		err = fixture.client.DeletePart(ctx, soPart.PK)
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part referenced by a sales-order line")

		t.Log("variant")
		templatePart := newTestPart("part-delete-variant-template", inventree.PartCreate{})
		templatePart, err = fixture.client.UpdatePart(ctx, templatePart.PK, inventree.PatchFields{"is_template": inventree.Set(true)})
		r.NoError(err)
		variantOf := templatePart.PK
		variantPart := newTestPart("part-delete-variant-child", inventree.PartCreate{})
		variantPart, err = fixture.client.UpdatePart(ctx, variantPart.PK, inventree.PatchFields{"variant_of": inventree.Set(variantOf)})
		r.NoError(err)
		r.NotNil(variantPart.VariantOf)
		r.Equal(variantOf, *variantPart.VariantOf)
		variants, err := fixture.client.SearchPartsByQuery(ctx, inventree.PartQuery{VariantOf: templatePart.PK})
		r.NoError(err)
		r.Contains(partIDs(variants), variantPart.PK)
		deactivatePart(t, ctx, fixture.client, templatePart.PK)
		err = fixture.client.DeletePart(ctx, templatePart.PK)
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part template that has variants")

		// The issue that requested delete_part suggested treating supplier
		// parts, manufacturer parts, parameters, attachments, and
		// related-part links as informational-only, non-blocking context
		// that InvenTree cascades away on its own. Pinned directly here:
		// InvenTree 1.5.0 does no such thing -- but not in the direction
		// that suggestion implied. DELETE /api/part/{id}/ silently permits
		// deleting a part while any of these five references still exists,
		// once the part is inactive; there is no upstream protection to
		// rely on for any of them, so delete_part's own guard treats every
		// one of them as blocking. Each category is isolated on its own
		// part below rather than assumed, per this repo's pin-don't-assume
		// philosophy.
		t.Log("supplier_part_only")
		infoSupplierOnlyPart := newTestPart("part-delete-info-supplier", inventree.PartCreate{Purchaseable: dvgoutils.Ptr(true)})
		infoSupplierOnlyName, err := fixture.run.Name("part-delete-info-supplier")
		r.NoError(err)
		infoSupplierPart, err := fixture.client.CreateSupplierPart(ctx, inventree.SupplierPartCreate{Part: infoSupplierOnlyPart.PK, Supplier: supplier.ID, SKU: infoSupplierOnlyName + "-sku"})
		r.NoError(err)
		r.NotZero(infoSupplierPart.PK)
		supplierPartsFound, err := fixture.client.SearchSupplierParts(ctx, inventree.SupplierPartQuery{Part: infoSupplierOnlyPart.PK})
		r.NoError(err)
		r.Len(supplierPartsFound, 1)
		deactivatePart(t, ctx, fixture.client, infoSupplierOnlyPart.PK)
		err = fixture.client.DeletePart(ctx, infoSupplierOnlyPart.PK)
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part with only a supplier-part link")

		t.Log("manufacturer_part_only")
		infoManufacturerOnlyPart := newTestPart("part-delete-info-manufacturer", inventree.PartCreate{})
		infoManufacturerOnlyName, err := fixture.run.Name("part-delete-info-manufacturer")
		r.NoError(err)
		infoManufacturerPart, err := fixture.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{Part: infoManufacturerOnlyPart.PK, Manufacturer: manufacturer.ID, MPN: dvgoutils.Ptr(infoManufacturerOnlyName + "-mpn")})
		r.NoError(err)
		r.NotZero(infoManufacturerPart.PK)
		manufacturerPartsFound, err := fixture.client.SearchManufacturerParts(ctx, inventree.ManufacturerPartQuery{Part: infoManufacturerOnlyPart.PK})
		r.NoError(err)
		r.Len(manufacturerPartsFound, 1)
		deactivatePart(t, ctx, fixture.client, infoManufacturerOnlyPart.PK)
		err = fixture.client.DeletePart(ctx, infoManufacturerOnlyPart.PK)
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part with only a manufacturer-part link")

		t.Log("related_part_only")
		infoRelatedOnlyPart := newTestPart("part-delete-info-related", inventree.PartCreate{})
		var relation inventree.PartRelation
		// The tested part is deliberately placed as part_2 (rather than
		// part_1) to pin that InvenTree's generic ?part= filter matches
		// either side of the relation, not just the first.
		r.NoError(fixture.client.Post(ctx, "/api/part/related/", map[string]any{"part_1": componentSupport.PK, "part_2": infoRelatedOnlyPart.PK}, &relation))
		r.NotZero(relation.PK)
		relationsFound, err := fixture.client.SearchPartRelations(ctx, inventree.PartRelationQuery{Part: infoRelatedOnlyPart.PK})
		r.NoError(err)
		r.Contains(partRelationIDs(relationsFound), relation.PK, "the ?part= filter must match a relation where the part is part_2, not only part_1")
		deactivatePart(t, ctx, fixture.client, infoRelatedOnlyPart.PK)
		err = fixture.client.DeletePart(ctx, infoRelatedOnlyPart.PK)
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part with only a related-part link")

		t.Log("parameter_only")
		infoParamPart := newTestPart("part-delete-info-parameter", inventree.PartCreate{})
		template := createParameterTemplate(t, fixture.client, fixture.run, "part-delete-info-parameter-template", "", "")
		parameter, err := fixture.client.CreatePartParameter(ctx, inventree.NewPartParameter(infoParamPart.PK, template.PK, "1"))
		r.NoError(err)
		r.NotZero(parameter.PK)
		parametersFound, err := fixture.client.SearchPartParameters(ctx, inventree.PartParameterQuery{PartID: infoParamPart.PK})
		r.NoError(err)
		r.Len(parametersFound, 1)
		deactivatePart(t, ctx, fixture.client, infoParamPart.PK)
		err = fixture.client.DeletePart(ctx, infoParamPart.PK)
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part with only a parameter row")

		t.Log("attachment_only")
		infoAttachmentPart := newTestPart("part-delete-info-attachment", inventree.PartCreate{})
		attachmentComment := "part_delete integration fixture attachment"
		attachment, err := fixture.client.CreateLinkAttachment(ctx, inventree.AttachmentCreate{
			ModelType: "part",
			ModelID:   infoAttachmentPart.PK,
			Link:      "https://example.test/part-delete-datasheet.pdf",
			Comment:   &attachmentComment,
		})
		r.NoError(err)
		r.NotZero(attachment.PK)
		attachmentsFound, err := fixture.client.ListAttachments(ctx, inventree.AttachmentQuery{ModelType: "part", ModelID: infoAttachmentPart.PK})
		r.NoError(err)
		r.Contains(attachmentIDs(attachmentsFound), attachment.PK)
		deactivatePart(t, ctx, fixture.client, infoAttachmentPart.PK)
		err = fixture.client.DeletePart(ctx, infoAttachmentPart.PK)
		r.NoError(err, "InvenTree 1.5.0 permits deleting an inactive part with only an attachment")

		// InvenTree 1.5.0 refuses to delete any *active* part regardless of
		// references, independent of every category above -- confirmed by
		// inspecting the field error on a part with zero other references.
		t.Log("clean_active_part_is_rejected")
		activeCleanPart := newTestPart("part-delete-active-clean", inventree.PartCreate{})
		err = fixture.client.DeletePart(ctx, activeCleanPart.PK)
		r.Error(err, "InvenTree 1.5.0 must reject deleting any active part, regardless of other references")
		var apiErr *inventree.APIError
		r.True(errors.As(err, &apiErr))
		r.Equal([]string{"Cannot delete this part as it is still active"}, apiErr.FieldErrors["non_field_errors"])

		t.Log("genuinely_unreferenced_inactive_part_succeeds")
		cleanPart := newTestPart("part-delete-clean", inventree.PartCreate{})
		deactivatePart(t, ctx, fixture.client, cleanPart.PK)
		err = fixture.client.DeletePart(ctx, cleanPart.PK)
		r.NoError(err, "InvenTree 1.5.0 must allow deleting an inactive part with no other references")
		_, err = fixture.client.GetPart(ctx, cleanPart.PK)
		r.Error(err, "the deleted part must no longer be readable")
	})

	t.Run("stock_adjustments", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		location := fixture.ensure(t, testenv.FixtureLocation)
		batch, err := fixture.run.Name("stock-adjustment")
		r.NoError(err)
		stock, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 5, Batch: &batch})
		r.NoError(err)

		r.NoError(fixture.client.AddStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: stock.PK, Quantity: "2"}}, Notes: "integration add"}))
		stock, err = fixture.client.GetStockItem(ctx, stock.PK)
		r.NoError(err)
		r.Equal(7.0, stock.Quantity)

		r.NoError(fixture.client.RemoveStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: stock.PK, Quantity: "1"}}, Notes: "integration remove"}))
		stock, err = fixture.client.GetStockItem(ctx, stock.PK)
		r.NoError(err)
		r.Equal(6.0, stock.Quantity)

		r.NoError(fixture.client.CountStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: stock.PK, Quantity: "4"}}, Notes: "integration count"}))
		stock, err = fixture.client.GetStockItem(ctx, stock.PK)
		r.NoError(err)
		r.Equal(4.0, stock.Quantity)
		r.NotNil(stock.Location)
		r.Equal(location.ID, *stock.Location)
		r.NotNil(stock.Batch)
		r.Equal(batch, *stock.Batch)

		destinationName, err := fixture.run.Name("stock-transfer-destination")
		r.NoError(err)
		external := true
		transferDestination, err := fixture.client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: destinationName, Parent: &location.ID, External: &external})
		r.NoError(err)
		r.True(transferDestination.External)
		r.NoError(fixture.client.TransferStock(ctx, inventree.StockTransfer{Items: []inventree.StockAdjustmentItem{{PK: stock.PK, Quantity: "4"}}, Notes: "integration full transfer", Location: transferDestination.PK}))
		transferred, err := fixture.client.GetStockItem(ctx, stock.PK)
		r.NoError(err)
		r.Equal(stock.PK, transferred.PK)
		r.Equal(4.0, transferred.Quantity)
		r.NotNil(transferred.Location)
		r.Equal(transferDestination.PK, *transferred.Location)
		r.Equal(stock.Part, transferred.Part)
		r.Equal(stock.Status, transferred.Status)
		r.Equal(stock.Batch, transferred.Batch)
		r.Equal(stock.Packaging, transferred.Packaging)
		r.Equal(stock.SupplierPart, transferred.SupplierPart)
		r.Equal(stock.PurchaseOrder, transferred.PurchaseOrder)
		r.Equal(stock.PurchasePrice, transferred.PurchasePrice)
		r.Equal(stock.PurchasePriceCurrency, transferred.PurchasePriceCurrency)

		var tracking struct {
			Results []struct {
				PK           int     `json:"pk"`
				Item         *int    `json:"item"`
				Notes        *string `json:"notes"`
				TrackingType int     `json:"tracking_type"`
			} `json:"results"`
		}
		req, err := fixture.client.NewRequest(ctx, http.MethodGet, "/api/stock/track/", url.Values{"item": []string{strconv.Itoa(stock.PK)}, "limit": []string{"100"}, "ordering": []string{"-date"}}, nil)
		r.NoError(err)
		r.NoError(fixture.client.DoJSON(req, &tracking))
		r.NotEmpty(tracking.Results)
		foundTransfer := false
		for _, entry := range tracking.Results {
			if entry.Item != nil && *entry.Item == stock.PK && entry.Notes != nil && *entry.Notes == "integration full transfer" {
				foundTransfer = true
				r.NotZero(entry.PK)
				r.NotZero(entry.TrackingType)
			}
		}
		r.True(foundTransfer, "native full transfer should record an exact-item tracking entry with the audit reason")

		t.Run("partial_transfer_split_characterization", func(t *testing.T) {
			r := require.New(t)
			partialBatch, err := fixture.run.Name("stock-partial-transfer")
			r.NoError(err)
			supplier := fixture.ensure(t, testenv.FixtureSupplier)
			supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
			partialReference, err := fixture.run.Name("stock-partial-transfer-po")
			r.NoError(err)
			partialOrder, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, SupplierReference: &partialReference, OrderCurrency: dvgoutils.Ptr("AUD")})
			r.NoError(err)
			partialLine, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: partialOrder.PK, SupplierPart: supplierPart.ID, Quantity: 6, PurchasePrice: dvgoutils.Ptr("2.50"), PurchasePriceCurrency: dvgoutils.Ptr("AUD"), Destination: &location.ID})
			r.NoError(err)
			r.NoError(fixture.client.IssuePurchaseOrder(ctx, partialOrder.PK))
			received, err := fixture.client.ReceivePurchaseOrder(ctx, partialOrder.PK, inventree.PurchaseOrderReceive{Items: []inventree.PurchaseOrderReceiveItem{{LineItem: partialLine.PK, Location: &location.ID, Quantity: "6", BatchCode: &partialBatch}}})
			r.NoError(err)
			r.Len(received, 1)
			partial, err := fixture.client.GetStockItem(ctx, received[0].PK)
			r.NoError(err)
			r.NoError(fixture.client.TransferStock(ctx, inventree.StockTransfer{Items: []inventree.StockAdjustmentItem{{PK: partial.PK, Quantity: "2"}}, Notes: "integration partial transfer", Location: transferDestination.PK}))

			items, err := fixture.client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: part.ID, Limit: 100})
			r.NoError(err)
			var source *inventree.StockItem
			var destinations []*inventree.StockItem
			for i := range items {
				item := &items[i]
				if item.Batch == nil || *item.Batch != partialBatch {
					continue
				}
				if item.PK == partial.PK {
					source = item
				} else if item.Location != nil && *item.Location == transferDestination.PK && item.Quantity == 2 {
					destinations = append(destinations, item)
				}
			}
			r.NotNil(source, "partial transfer must preserve or expose the source identity")
			r.Len(destinations, 1, "partial transfer must expose exactly one distinct destination identity")
			destination := destinations[0]
			r.Equal(4.0, source.Quantity)
			r.Equal(2.0, destination.Quantity)
			r.NotNil(destination.Location)
			r.Equal(transferDestination.PK, *destination.Location)
			r.Equal(partial.Part, destination.Part)
			r.Equal(partial.Status, destination.Status)
			r.Equal(partial.Packaging, destination.Packaging)
			r.Equal(partial.Batch, destination.Batch)
			r.Equal(partial.SupplierPart, destination.SupplierPart)
			r.Equal(partial.PurchaseOrder, destination.PurchaseOrder)
			r.Equal(partial.PurchasePrice, destination.PurchasePrice)
			r.Equal(partial.PurchasePriceCurrency, destination.PurchasePriceCurrency)

			var tracking struct {
				Results []struct {
					PK           int     `json:"pk"`
					Item         *int    `json:"item"`
					Notes        *string `json:"notes"`
					TrackingType int     `json:"tracking_type"`
				} `json:"results"`
			}
			req, err := fixture.client.NewRequest(ctx, http.MethodGet, "/api/stock/track/", url.Values{"part": []string{strconv.Itoa(part.ID)}, "limit": []string{"100"}, "ordering": []string{"-date"}}, nil)
			r.NoError(err)
			r.NoError(fixture.client.DoJSON(req, &tracking))
			foundPartial := false
			for _, entry := range tracking.Results {
				if entry.Notes != nil && *entry.Notes == "integration partial transfer" {
					foundPartial = true
					r.NotZero(entry.PK)
					r.NotZero(entry.TrackingType)
					r.NotNil(entry.Item)
				}
			}
			r.True(foundPartial, "native partial transfer must record an audit event")
		})

		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		supplierReference, err := fixture.run.Name("stock-transfer-po")
		r.NoError(err)
		order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, SupplierReference: &supplierReference, OrderCurrency: dvgoutils.Ptr("AUD")})
		r.NoError(err)
		lineReference, err := fixture.run.Name("stock-transfer-line")
		r.NoError(err)
		line, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &lineReference, Quantity: 2, PurchasePrice: dvgoutils.Ptr("1.25"), PurchasePriceCurrency: dvgoutils.Ptr("AUD"), Destination: &location.ID})
		r.NoError(err)
		r.NoError(fixture.client.IssuePurchaseOrder(ctx, order.PK))
		received, err := fixture.client.ReceivePurchaseOrder(ctx, order.PK, inventree.PurchaseOrderReceive{Items: []inventree.PurchaseOrderReceiveItem{{LineItem: line.PK, Location: &location.ID, Quantity: "2", BatchCode: dvgoutils.Ptr("transfer-provenance")}}})
		r.NoError(err)
		r.Len(received, 1)
		provenanceBefore, err := fixture.client.GetStockItem(ctx, received[0].PK)
		r.NoError(err)
		r.NotNil(provenanceBefore.SupplierPart)
		r.NotNil(provenanceBefore.PurchaseOrder)
		r.NotNil(provenanceBefore.PurchasePrice)
		r.Equal("AUD", provenanceBefore.PurchasePriceCurrency)
		r.NoError(fixture.client.TransferStock(ctx, inventree.StockTransfer{Items: []inventree.StockAdjustmentItem{{PK: provenanceBefore.PK, Quantity: "2"}}, Notes: "integration provenance transfer", Location: transferDestination.PK}))
		provenanceAfter, err := fixture.client.GetStockItem(ctx, provenanceBefore.PK)
		r.NoError(err)
		r.Equal(provenanceBefore.PK, provenanceAfter.PK)
		r.Equal(provenanceBefore.SupplierPart, provenanceAfter.SupplierPart)
		r.Equal(provenanceBefore.PurchaseOrder, provenanceAfter.PurchaseOrder)
		r.Equal(provenanceBefore.PurchasePrice, provenanceAfter.PurchasePrice)
		r.Equal(provenanceBefore.PurchasePriceCurrency, provenanceAfter.PurchasePriceCurrency)
		r.Equal(provenanceBefore.Batch, provenanceAfter.Batch)

		r.NoError(fixture.client.ChangeStockStatus(ctx, inventree.StockStatusChange{Items: []int{stock.PK}, Status: 55, Note: "integration damaged status"}))
		stock, err = fixture.client.GetStockItem(ctx, stock.PK)
		r.NoError(err)
		r.Equal(55, stock.Status)

		deleteOnDeplete := true
		depleted, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 2, DeleteOnDeplete: &deleteOnDeplete})
		r.NoError(err)
		r.True(depleted.DeleteOnDeplete)
		r.NoError(fixture.client.RemoveStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: depleted.PK, Quantity: "2"}}, Notes: "integration complete depletion"}))
		_, err = fixture.client.GetStockItem(ctx, depleted.PK)
		r.Error(err)
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		r.Equal(inventree.ErrorKindNotFound, apiErr.Kind)
	})

	t.Run("stock_delete_on_deplete_policy", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		location := fixture.ensure(t, testenv.FixtureLocation)

		positive, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 3, DeleteOnDeplete: dvgoutils.Ptr(false)})
		r.NoError(err)
		a.False(positive.DeleteOnDeplete)
		enabled, err := fixture.client.UpdateStockItem(ctx, positive.PK, inventree.PatchFields{"delete_on_deplete": inventree.Set(true)})
		r.NoError(err)
		a.True(enabled.DeleteOnDeplete)
		a.Equal(3.0, enabled.Quantity, "enabling the policy alone must not change quantity")
		reread, err := fixture.client.GetStockItem(ctx, positive.PK)
		r.NoError(err)
		a.True(reread.DeleteOnDeplete)

		disabled, err := fixture.client.UpdateStockItem(ctx, positive.PK, inventree.PatchFields{"delete_on_deplete": inventree.Set(false)})
		r.NoError(err)
		a.False(disabled.DeleteOnDeplete)
		r.NoError(fixture.client.RemoveStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: positive.PK, Quantity: "3"}}, Notes: "integration depletion with policy disabled"}))
		survived, err := fixture.client.GetStockItem(ctx, positive.PK)
		r.NoError(err, "the item must still exist after full depletion because delete_on_deplete was disabled")
		a.Equal(0.0, survived.Quantity)
		a.False(survived.DeleteOnDeplete)

		zero, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 1, DeleteOnDeplete: dvgoutils.Ptr(false)})
		r.NoError(err)
		r.NoError(fixture.client.RemoveStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: zero.PK, Quantity: "1"}}, Notes: "integration deplete before enabling policy"}))
		zero, err = fixture.client.GetStockItem(ctx, zero.PK)
		r.NoError(err)
		r.Equal(0.0, zero.Quantity)
		enabledAtZero, err := fixture.client.UpdateStockItem(ctx, zero.PK, inventree.PatchFields{"delete_on_deplete": inventree.Set(true)})
		r.NoError(err, "enabling the policy on an already-zero-quantity item must not itself delete the record")
		a.True(enabledAtZero.DeleteOnDeplete)
		a.Equal(0.0, enabledAtZero.Quantity)
		stillPresent, err := fixture.client.GetStockItem(ctx, zero.PK)
		r.NoError(err)
		a.True(stillPresent.DeleteOnDeplete)

		r.NoError(fixture.client.AddStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: zero.PK, Quantity: "2"}}, Notes: "integration restock before interacting with depletion"}))
		r.NoError(fixture.client.RemoveStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: zero.PK, Quantity: "2"}}, Notes: "integration depletion after policy toggled at zero quantity"}))
		_, err = fixture.client.GetStockItem(ctx, zero.PK)
		r.Error(err, "the policy set through this tool must still trigger native deletion once quantity later reaches zero again")
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		r.Equal(inventree.ErrorKindNotFound, apiErr.Kind)
	})

	t.Run("stock_serial_management", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		location := fixture.ensure(t, testenv.FixtureLocation)

		trackableName, err := fixture.run.Name("serial-trackable-part")
		r.NoError(err)
		trackablePart, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: trackableName, Category: dvgoutils.Ptr(category.ID), Active: dvgoutils.Ptr(true), Trackable: dvgoutils.Ptr(true)})
		r.NoError(err)
		a.True(trackablePart.Trackable)

		untrackableName, err := fixture.run.Name("serial-untrackable-part")
		r.NoError(err)
		untrackablePart, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: untrackableName, Category: dvgoutils.Ptr(category.ID), Active: dvgoutils.Ptr(true), Trackable: dvgoutils.Ptr(false)})
		r.NoError(err)

		// Pinned InvenTree 1.5.1 does not reject /api/part/{id}/serial-numbers/
		// for a non-trackable part; it returns an ordinary 200 with no latest
		// serial. The tool-layer trackability preflight exists to give a
		// clearer, more actionable client-side signal than this silent
		// no-latest-serial response.
		untrackableSerials, err := fixture.client.GetPartSerialNumbers(ctx, untrackablePart.PK)
		r.NoError(err)
		a.Nil(untrackableSerials.Latest)

		unserialized, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: trackablePart.PK, Location: location.ID, Quantity: 1})
		r.NoError(err)
		r.Nil(unserialized.Serial)

		noneYet, err := fixture.client.GetPartSerialNumbers(ctx, trackablePart.PK)
		r.NoError(err)
		a.Nil(noneYet.Latest)

		assigned, err := fixture.client.UpdateStockItem(ctx, unserialized.PK, inventree.PatchFields{"serial": inventree.Set("101")})
		r.NoError(err)
		r.NotNil(assigned.Serial)
		a.Equal("101", *assigned.Serial)

		withLatest, err := fixture.client.GetPartSerialNumbers(ctx, trackablePart.PK)
		r.NoError(err)
		r.NotNil(withLatest.Latest)
		a.Equal("101", *withLatest.Latest)
		a.NotEmpty(withLatest.Next)

		found, err := fixture.client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: trackablePart.PK, Serial: "101"})
		r.NoError(err)
		r.Len(found, 1)
		a.Equal(unserialized.PK, found[0].PK)

		serializedOnly, err := fixture.client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: trackablePart.PK, Serialized: dvgoutils.Ptr(true)})
		r.NoError(err)
		r.Len(serializedOnly, 1)
		unserializedOnly, err := fixture.client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: trackablePart.PK, Serialized: dvgoutils.Ptr(false)})
		r.NoError(err)
		r.Empty(unserializedOnly)

		replaced, err := fixture.client.UpdateStockItem(ctx, unserialized.PK, inventree.PatchFields{"serial": inventree.Set("150")})
		r.NoError(err)
		r.NotNil(replaced.Serial)
		a.Equal("150", *replaced.Serial)

		// Pinned InvenTree 1.5.1 rejects a same-part duplicate serial at
		// write time, proving stockSerialCollision's best-effort client-side
		// preflight has real server-side backing.
		duplicateAttempt, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: trackablePart.PK, Location: location.ID, Quantity: 1})
		r.NoError(err)
		_, err = fixture.client.UpdateStockItem(ctx, duplicateAttempt.PK, inventree.PatchFields{"serial": inventree.Set("150")})
		r.Error(err, "InvenTree must reject assigning an already-used serial to a second stock item of the same part")
		var duplicateErr *inventree.APIError
		r.ErrorAs(err, &duplicateErr)
		a.Equal(inventree.ErrorKindValidation, duplicateErr.Kind)
		afterFailedDuplicate, err := fixture.client.GetStockItem(ctx, duplicateAttempt.PK)
		r.NoError(err)
		a.Nil(afterFailedDuplicate.Serial, "the rejected PATCH must not have partially applied")

		// serial_gte/serial_lte bound the same numeric-comparable range
		// InvenTree itself uses for serial ordering.
		low, err := fixture.client.UpdateStockItem(ctx, duplicateAttempt.PK, inventree.PatchFields{"serial": inventree.Set("10")})
		r.NoError(err)
		r.NotNil(low.Serial)
		highItemBase, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: trackablePart.PK, Location: location.ID, Quantity: 1})
		r.NoError(err)
		highItem, err := fixture.client.UpdateStockItem(ctx, highItemBase.PK, inventree.PatchFields{"serial": inventree.Set("500")})
		r.NoError(err)
		r.NotNil(highItem.Serial)

		gte, lte := 100, 300
		inRange, err := fixture.client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: trackablePart.PK, SerialGTE: &gte, SerialLTE: &lte})
		r.NoError(err)
		r.Len(inRange, 1, "only the %q serial falls within [100,300]", "150")
		a.Equal(unserialized.PK, inRange[0].PK)

		cleared, err := fixture.client.UpdateStockItem(ctx, unserialized.PK, inventree.PatchFields{"serial": inventree.Null()})
		r.NoError(err)
		a.Nil(cleared.Serial)
		reread, err := fixture.client.GetStockItem(ctx, unserialized.PK)
		r.NoError(err)
		a.Nil(reread.Serial)
	})

	t.Run("stock_install_uninstall", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		location := fixture.ensure(t, testenv.FixtureLocation)
		otherLocationName, err := fixture.run.Name("install-other-location")
		r.NoError(err)
		otherLocation, err := fixture.client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: otherLocationName})
		r.NoError(err)

		assemblyName, err := fixture.run.Name("install-assembly")
		r.NoError(err)
		assemblyPart, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: assemblyName, Category: dvgoutils.Ptr(category.ID), Active: dvgoutils.Ptr(true), Assembly: dvgoutils.Ptr(true), Trackable: dvgoutils.Ptr(true)})
		r.NoError(err)

		componentName, err := fixture.run.Name("install-component")
		r.NoError(err)
		componentPart, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: componentName, Category: dvgoutils.Ptr(category.ID), Active: dvgoutils.Ptr(true), Component: dvgoutils.Ptr(true), Trackable: dvgoutils.Ptr(true)})
		r.NoError(err)

		outOfBOMName, err := fixture.run.Name("install-out-of-bom")
		r.NoError(err)
		outOfBOMPart, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: outOfBOMName, Category: dvgoutils.Ptr(category.ID), Active: dvgoutils.Ptr(true), Component: dvgoutils.Ptr(true), Trackable: dvgoutils.Ptr(true)})
		r.NoError(err)

		var bomItem inventree.BomItem
		r.NoError(fixture.client.Post(ctx, "/api/bom/", map[string]any{"part": assemblyPart.PK, "sub_part": componentPart.PK, "quantity": 1}, &bomItem))
		r.NotZero(bomItem.PK)

		newSerializedStock := func(partID int, serial string) inventree.StockItem {
			item, createErr := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: partID, Location: location.ID, Quantity: 1})
			r.NoError(createErr)
			item, updateErr := fixture.client.UpdateStockItem(ctx, item.PK, inventree.PatchFields{"serial": inventree.Set(serial)})
			r.NoError(updateErr)
			r.NotNil(item.Serial)
			return item
		}

		parentStock := newSerializedStock(assemblyPart.PK, "1")
		outOfBOMStock := newSerializedStock(outOfBOMPart.PK, "1")

		// InvenTree 1.5.1 enforces BOM membership itself: a child part not on
		// the parent's BOM is rejected before any relationship is written.
		childStock := newSerializedStock(componentPart.PK, "1")
		outOfBOMErr := fixture.client.InstallStockItem(ctx, parentStock.PK, inventree.InstallStockItem{StockItem: outOfBOMStock.PK, Quantity: 1})
		r.Error(outOfBOMErr, "InvenTree must reject installing a child whose part is not in the parent's BOM")
		var bomAPIErr *inventree.APIError
		r.ErrorAs(outOfBOMErr, &bomAPIErr)
		a.Equal([]string{"Selected part is not in the Bill of Materials"}, bomAPIErr.FieldErrors["stock_item"])

		// The 201 response only echoes the request (InstallStockItem), not
		// resulting stock-item state, so the applied effect must be observed
		// via a follow-up GetStockItem on both sides.
		r.NoError(fixture.client.InstallStockItem(ctx, parentStock.PK, inventree.InstallStockItem{StockItem: childStock.PK, Quantity: 1}))
		afterInstallParent, err := fixture.client.GetStockItem(ctx, parentStock.PK)
		r.NoError(err)
		r.NotNil(afterInstallParent.InstalledItems)
		a.Equal(1, *afterInstallParent.InstalledItems, "install increments the parent's installed_items count")
		r.NotNil(afterInstallParent.ChildItems)
		a.Zero(*afterInstallParent.ChildItems, "install does not touch the unrelated split-lineage child_items count")
		a.InDelta(1.0, afterInstallParent.Quantity, 1e-9, "install does not change the parent's own quantity")

		afterInstallChild, err := fixture.client.GetStockItem(ctx, childStock.PK)
		r.NoError(err)
		r.NotNil(afterInstallChild.BelongsTo)
		a.Equal(parentStock.PK, *afterInstallChild.BelongsTo)
		a.Nil(afterInstallChild.Location, "an installed child loses its own independent location")
		a.False(afterInstallChild.InStock, "an installed child is no longer independently in stock")

		// Pin install's stock-tracking history on both sides of the
		// relationship: InvenTree 1.5.1 records a distinct native tracking
		// event type for each side (30 "Installed into assembly" on the
		// child, 35 "Installed component item" on the parent) rather than
		// leaving either side's install unaudited. F-S54's acceptance
		// criteria call out "tracking events"/"history behavior" explicitly.
		childTrackingAfterInstall, err := fixture.client.SearchStockTrackingPage(ctx, inventree.StockTrackingQuery{ItemID: childStock.PK, Limit: 100})
		r.NoError(err)
		a.True(hasStockTrackingType(childTrackingAfterInstall.Results, 30), "install must record a native tracking event on the child stock item")
		parentTrackingAfterInstall, err := fixture.client.SearchStockTrackingPage(ctx, inventree.StockTrackingQuery{ItemID: parentStock.PK, Limit: 100})
		r.NoError(err)
		a.True(hasStockTrackingType(parentTrackingAfterInstall.Results, 35), "install must record a native tracking event on the parent stock item")

		// A now-unavailable (installed) child cannot be installed again;
		// InvenTree reports this as an availability error, not a
		// belongs_to-specific one, which is why the MCP-side guard checks
		// belongs_to directly for a clearer clarification.
		reinstallErr := fixture.client.InstallStockItem(ctx, parentStock.PK, inventree.InstallStockItem{StockItem: childStock.PK, Quantity: 1})
		r.Error(reinstallErr, "InvenTree must reject re-installing an already-installed child")
		var unavailableAPIErr *inventree.APIError
		r.ErrorAs(reinstallErr, &unavailableAPIErr)
		a.Equal([]string{"Stock item is unavailable"}, unavailableAPIErr.FieldErrors["stock_item"])

		// quantity is validated against the CHILD's own available quantity,
		// not the BOM's required quantity: a serialized (quantity-1) child
		// cannot satisfy a request for more than 1.
		quantityChild := newSerializedStock(componentPart.PK, "2")
		quantityErr := fixture.client.InstallStockItem(ctx, parentStock.PK, inventree.InstallStockItem{StockItem: quantityChild.PK, Quantity: 5})
		r.Error(quantityErr, "InvenTree must reject an install quantity exceeding the child's own available quantity")
		var quantityAPIErr *inventree.APIError
		r.ErrorAs(quantityErr, &quantityAPIErr)
		a.Equal([]string{"Quantity to install must not exceed available quantity"}, quantityAPIErr.FieldErrors["quantity"])

		// Despite the endpoint's docstring describing the child as required
		// to be serialized, InvenTree 1.5.1 does not actually enforce that:
		// an ordinary unserialized quantity-1 stock item installs
		// successfully. This tool intentionally does not require a serial
		// either; it only requires quantity exactly 1, to avoid the
		// unrelated partial-quantity/split-identity workflow this story
		// does not cover.
		unserializedChild, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: componentPart.PK, Location: location.ID, Quantity: 1})
		r.NoError(err)
		r.Nil(unserializedChild.Serial)
		r.NoError(fixture.client.InstallStockItem(ctx, parentStock.PK, inventree.InstallStockItem{StockItem: unserializedChild.PK, Quantity: 1}))
		afterUnserializedChild, err := fixture.client.GetStockItem(ctx, unserializedChild.PK)
		r.NoError(err)
		r.NotNil(afterUnserializedChild.BelongsTo)
		a.Equal(parentStock.PK, *afterUnserializedChild.BelongsTo)

		// Uninstall targets the currently-installed CHILD item's own id
		// (there is no separate child-id body field to disambiguate), and
		// clears belongs_to while setting the item's location to the
		// supplied destination.
		r.NoError(fixture.client.UninstallStockItem(ctx, childStock.PK, inventree.UninstallStockItem{Location: otherLocation.PK}))
		afterUninstallChild, err := fixture.client.GetStockItem(ctx, childStock.PK)
		r.NoError(err)
		a.Nil(afterUninstallChild.BelongsTo)
		r.NotNil(afterUninstallChild.Location)
		a.Equal(otherLocation.PK, *afterUninstallChild.Location)

		// Uninstalling releases the parent's installed_items count back down.
		afterUninstallParent, err := fixture.client.GetStockItem(ctx, parentStock.PK)
		r.NoError(err)
		r.NotNil(afterUninstallParent.InstalledItems)
		a.Equal(1, *afterUninstallParent.InstalledItems, "uninstalling one of two remaining installed children (childStock, unserializedChild) leaves one behind")

		// Pin uninstall's own tracking event (31 "Removed from assembly"),
		// alongside the install event (30) still present in the same
		// item's history.
		childTrackingAfterUninstall, err := fixture.client.SearchStockTrackingPage(ctx, inventree.StockTrackingQuery{ItemID: childStock.PK, Limit: 100})
		r.NoError(err)
		a.True(hasStockTrackingType(childTrackingAfterUninstall.Results, 31), "uninstall must record a native tracking event on the child stock item")
		a.True(hasStockTrackingType(childTrackingAfterUninstall.Results, 30), "the earlier install tracking event must remain in history")

		// InvenTree does not itself reject uninstalling an item that was
		// never installed anywhere (belongs_to already nil) -- it silently
		// sets the location anyway. This is a real safety gap the MCP-side
		// guard must close by requiring belongs_to != nil before allowing
		// uninstall, rather than relying on upstream rejection.
		neverInstalledErr := fixture.client.UninstallStockItem(ctx, quantityChild.PK, inventree.UninstallStockItem{Location: otherLocation.PK})
		a.NoError(neverInstalledErr, "pinning: InvenTree accepts uninstalling a never-installed item rather than rejecting it")

		badLocationErr := fixture.client.UninstallStockItem(ctx, unserializedChild.PK, inventree.UninstallStockItem{Location: 9999999})
		r.Error(badLocationErr, "InvenTree must reject uninstalling to a nonexistent location")
		var locationAPIErr *inventree.APIError
		r.ErrorAs(badLocationErr, &locationAPIErr)
		a.Contains(locationAPIErr.FieldErrors["location"][0], "does not exist")
	})

	t.Run("instance_info", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)

		server, err := fixture.client.GetServerInfo(ctx)
		r.NoError(err)
		a.NotEmpty(server.Server)
		a.Equal(shared.Environment().Version, server.Version)
		a.Equal(shared.Environment().APIVersion, strconv.Itoa(server.APIVersion))
		a.NotEmpty(server.Instance)

		version, err := fixture.client.GetVersionInfo(ctx)
		r.NoError(err, "the shared fixture's admin account must have InvenTree staff access for /api/version/'s declared a:staff OAuth scope")
		a.NotEmpty(version.CommitHash)
		a.NotEmpty(version.CommitDate)

		for _, key := range instanceInfoAllowlistedGlobalSettingsForTest {
			setting, err := fixture.client.GetGlobalSetting(ctx, key)
			r.NoErrorf(err, "allowlisted global setting %s must be present and readable by a staff credential on the pinned InvenTree baseline", key)
			a.Equal(key, setting.Key)
		}

		for _, key := range instanceInfoAllowlistedUserSettingsForTest {
			setting, err := fixture.client.GetUserSetting(ctx, key)
			r.NoErrorf(err, "allowlisted user setting %s must be present and readable", key)
			a.Equal(key, setting.Key)
		}

		_, err = fixture.client.GetGlobalSetting(ctx, "F_S71_NONEXISTENT_SETTING_KEY")
		r.Error(err, "a removed/renamed allowlisted key must be reported as an omittable error, not silently succeed")
		a.True(inventree.IsOmittableFetchError(err), "a missing global setting key must classify as omittable so the instance-info tool omits it instead of failing the call")
	})

	t.Run("stock_tracking_and_stocktake_history", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)

		// fixture.ensure is idempotent per (run, kind), so this is the same
		// underlying part used throughout the rest of the subtest. The
		// empty-history assertion must run first, before any stock item
		// exists for it.
		part := fixture.ensure(t, testenv.FixturePart)
		emptyPage, err := fixture.client.SearchStockTrackingPage(ctx, inventree.StockTrackingQuery{PartID: part.ID, Limit: 100})
		r.NoError(err)
		a.Zero(emptyPage.Count, "a part with no stock items must report empty tracking history rather than erroring")
		a.Empty(emptyPage.Results)

		location := fixture.ensure(t, testenv.FixtureLocation)
		secondLocationName, err := fixture.run.Name("track-second-location")
		r.NoError(err)
		secondLocation, err := fixture.client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: secondLocationName})
		r.NoError(err)

		stockItem, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 10})
		r.NoError(err)

		r.NoError(fixture.client.AddStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: stockItem.PK, Quantity: "2"}}, Notes: "track-add"}))
		r.NoError(fixture.client.TransferStock(ctx, inventree.StockTransfer{Items: []inventree.StockAdjustmentItem{{PK: stockItem.PK, Quantity: "12"}}, Notes: "track-transfer", Location: secondLocation.PK}))

		byItem, err := fixture.client.SearchStockTrackingPage(ctx, inventree.StockTrackingQuery{ItemID: stockItem.PK, Limit: 100})
		r.NoError(err)
		r.GreaterOrEqual(byItem.Count, 3, "creation plus the two triggered adjustments must all be recorded")
		byPart, err := fixture.client.SearchStockTrackingPage(ctx, inventree.StockTrackingQuery{PartID: part.ID, Limit: 100})
		r.NoError(err)
		a.Equal(byItem.Count, byPart.Count, "the item and part filters must agree for a part with exactly one stock item")

		var created, added, transferred inventree.StockTracking
		for _, entry := range byItem.Results {
			r.NotNil(entry.Item)
			a.Equal(stockItem.PK, *entry.Item)
			switch entry.Notes {
			case "track-add":
				added = entry
			case "track-transfer":
				transferred = entry
			case "":
				created = entry
			}
			// Audit-note safety: the free-text notes field must never leak into the
			// separately-bounded deltas payload, and vice versa.
			_, notesKeyInDeltas := entry.Deltas["notes"]
			a.False(notesKeyInDeltas, "deltas must never carry a notes key; notes is a sibling field")
		}
		r.NotZero(created.PK, "the automatic creation event must be present")
		r.NotZero(added.PK, "the manual add event must be present")
		r.NotZero(transferred.PK, "the transfer event must be present")

		exactAdded, err := fixture.client.GetStockTrackingEntry(ctx, added.PK)
		r.NoError(err)
		a.Equal("track-add", exactAdded.Notes)
		a.Equal(added.PK, exactAdded.PK)
		a.EqualValues(2, exactAdded.Deltas["added"])

		// Pinned 2026-08-18 spike finding: a "Location changed" event
		// unconditionally embeds a full location_detail record. Confirm the
		// client-layer sanitizer strips it while preserving the stable
		// location ID sibling key.
		a.Contains(transferred.Deltas, "location")
		a.NotContains(transferred.Deltas, "location_detail", "nested location detail must be redacted from deltas")
		a.EqualValues(secondLocation.PK, transferred.Deltas["location"])

		_, err = fixture.client.GetStockTrackingEntry(ctx, 0)
		r.Error(err, "a non-existent tracking-event ID must fail rather than silently succeed")

		// Pinned 2026-08-18 spike finding: a purchase-order receipt event
		// unconditionally embeds a full purchaseorder_detail record,
		// including a created_by sub-object with the receiving user's email
		// and username. Confirm both are redacted.
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		poReference, err := fixture.run.Name("track-po")
		r.NoError(err)
		order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, SupplierReference: &poReference})
		r.NoError(err)
		line, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Quantity: 3})
		r.NoError(err)
		r.NoError(fixture.client.IssuePurchaseOrder(ctx, order.PK))
		received, err := fixture.client.ReceivePurchaseOrder(ctx, order.PK, inventree.PurchaseOrderReceive{Items: []inventree.PurchaseOrderReceiveItem{{LineItem: line.PK, Location: &location.ID, Quantity: "3"}}})
		r.NoError(err)
		r.NotEmpty(received)

		receivedHistory, err := fixture.client.SearchStockTrackingPage(ctx, inventree.StockTrackingQuery{ItemID: received[0].PK, Limit: 100})
		r.NoError(err)
		r.NotEmpty(receivedHistory.Results)
		receiptEntry := receivedHistory.Results[0]
		a.Contains(receiptEntry.Deltas, "purchaseorder")
		a.NotContains(receiptEntry.Deltas, "purchaseorder_detail", "nested purchase-order detail must be redacted from deltas")
		encoded, err := json.Marshal(receiptEntry.Deltas)
		r.NoError(err)
		a.NotContains(string(encoded), "@example", "a redacted deltas payload must never contain the receiving user's email")
		a.NotContains(string(encoded), "created_by", "created_by must never survive redaction")

		// Historical PartStocktake snapshots have no pinned-InvenTree-1.5.0
		// path this Testcontainers suite can populate synchronously:
		// POST /api/part/stocktake/ (the raw create endpoint) crashes with
		// a 500 on every request regardless of payload -- InvenTree's view
		// unconditionally passes an unsupported `user` keyword to the
		// PartStocktake model constructor, which pinned InvenTree 1.5.0's
		// PartStocktake schema does not declare a field for -- and
		// POST /api/part/stocktake/generate/ only offloads generation to a
		// background worker process. This suite starts that worker with the
		// same signing key as the web process, but this characterization still
		// treats generation as enqueue-only and does not claim a completed
		// snapshot or report artifact.
		// The MCP tool surface never calls either write endpoint, so this
		// is pinned as a documented upstream limitation rather than routed
		// around: prove the empty-history and not-found read paths live,
		// and prove create is unusable so a future InvenTree fix (which
		// would need a corresponding follow-up story to add write support)
		// is caught by this assertion changing.
		emptyStocktakePage, err := fixture.client.SearchPartStocktakesPage(ctx, inventree.PartStocktakeQuery{PartID: part.ID, Limit: 100})
		r.NoError(err)
		a.Zero(emptyStocktakePage.Count, "a part with no generated stocktake snapshots must report an empty history rather than erroring")
		a.Empty(emptyStocktakePage.Results)

		_, err = fixture.client.GetPartStocktake(ctx, 0)
		r.Error(err, "a non-existent stocktake ID must fail rather than silently succeed")

		rawCreateReq, err := fixture.client.NewRequest(ctx, http.MethodPost, "/api/part/stocktake/", nil, map[string]any{"part": part.ID, "quantity": 1})
		r.NoError(err)
		var rawCreateOut map[string]any
		rawCreateErr := fixture.client.DoJSON(rawCreateReq, &rawCreateOut)
		r.Error(rawCreateErr, "pinned InvenTree 1.5.0's raw PartStocktake create endpoint is expected to fail; a future InvenTree release fixing this needs a follow-up story adding stocktake generation/write support")
		var rawCreateAPIErr *inventree.APIError
		r.ErrorAs(rawCreateErr, &rawCreateAPIErr)
		a.Equal(http.StatusInternalServerError, rawCreateAPIErr.StatusCode, "pin the specific 500 failure mode rather than accepting any error, so an unrelated 400/403 regression is also caught")

		// The generation endpoint is a separate asynchronous surface from raw
		// PartStocktake creation. Pin the selector and output contract against
		// the released API. This client-method stack starts the pinned worker,
		// while the default testenv stack remains web-only for unrelated tests.
		category := fixture.ensure(t, testenv.FixtureCategory)
		for _, tc := range []struct {
			name    string
			payload map[string]any
		}{
			{name: "part", payload: map[string]any{"part": part.ID}},
			{name: "category", payload: map[string]any{"category": category.ID}},
			{name: "location", payload: map[string]any{"location": secondLocation.PK}},
			{name: "composed_selectors", payload: map[string]any{"part": part.ID, "category": category.ID, "location": secondLocation.PK}},
			{name: "generate_entries", payload: map[string]any{"part": part.ID, "generate_entry": true}},
			{name: "generate_report", payload: map[string]any{"part": part.ID, "generate_report": true}},
			{name: "generate_entries_and_report", payload: map[string]any{"part": part.ID, "generate_entry": true, "generate_report": true}},
		} {
			t.Run("generation_"+tc.name, func(t *testing.T) {
				r := require.New(t)
				a := assert.New(t)
				generationCtx, _, _ := testhandler.SetupTestHandler(t)
				req, err := fixture.client.NewRequest(generationCtx, http.MethodPost, "/api/part/stocktake/generate/", nil, tc.payload)
				r.NoError(err)
				var out map[string]any
				r.NoError(fixture.client.DoJSON(req, &out))
				a.Contains(out, "output", "generation response must expose the asynchronous DataOutput contract")
			})
		}

		// Exercise the exported typed client methods on the successful enqueue
		// path and exact DataOutput read-back. Completion remains an explicit
		// tool-layer concern because F-S60 observed report jobs that did not
		// reach a terminal state within the bounded probe.
		generation, err := fixture.client.GeneratePartStocktake(ctx, inventree.PartStocktakeGenerate{
			Part:           &part.ID,
			GenerateEntry:  true,
			GenerateReport: true,
		})
		r.NoError(err)
		r.NotNil(generation.Output)
		r.Positive(generation.Output.PK)
		output, err := fixture.client.GetDataOutput(ctx, generation.Output.PK)
		r.NoError(err)
		a.Equal(generation.Output.PK, output.PK)
		terminal, reachedTerminal := pollDataOutputForStocktakeCharacterization(ctx, fixture.client, generation.Output.PK, 30*time.Second)
		t.Logf("combined entry/report terminal characterization: task_id=%d complete=%t progress=%d total=%d output_available=%t reached_terminal=%t", terminal.PK, terminal.Complete, terminal.Progress, terminal.Total, terminal.Output != nil && *terminal.Output != "", reachedTerminal)
		a.False(reachedTerminal, "pinned InvenTree 1.5.1/API 530 report generation is expected to remain non-terminal within the bounded characterization")
		a.False(terminal.Complete)
		a.Nil(terminal.Output)
		if reachedTerminal && terminal.Output != nil && *terminal.Output != "" {
			report, err := fixture.client.DownloadDataOutput(ctx, *terminal.Output, 10<<20)
			r.NoError(err)
			r.NotEmpty(report.Content)
			t.Logf("combined entry/report artifact characterization: content_type=%s bytes=%d", report.ContentType, len(report.Content))
		}

		// F-S74 characterization: the pinned endpoint does not advertise a
		// staff-only security scope. Two identical same-day requests must not be
		// mistaken for an idempotent retry: record whether InvenTree allocates
		// distinct DataOutput tasks, and leave completion/report behavior to the
		// task-handle poll surface.
		duplicateIDs := make([]int, 0, 2)
		for i := 0; i < 2; i++ {
			duplicate, err := fixture.client.GeneratePartStocktake(ctx, inventree.PartStocktakeGenerate{
				Part:           &part.ID,
				GenerateEntry:  true,
				GenerateReport: true,
			})
			r.NoError(err)
			r.NotNil(duplicate.Output)
			duplicateIDs = append(duplicateIDs, duplicate.Output.PK)
		}
		a.NotEqual(duplicateIDs[0], duplicateIDs[1], "identical same-day requests must remain visibly distinct task submissions when InvenTree does not deduplicate them")
		t.Logf("same-day duplicate characterization: task IDs %v were both accepted as distinct DataOutput submissions", duplicateIDs)

		// Characterize the permission boundary with a run-scoped non-staff
		// account created through the staff fixture client. The result is
		// intentionally logged rather than assumed from the schema: this is a
		// live product fact that must remain visible if the upstream permission
		// policy changes.
		nonStaffClient := newNonStaffClient(t, ctx, fixture, "stocktake-nonstaff", "F-S74-live-characterization-password")
		nonStaffGeneration, err := nonStaffClient.GeneratePartStocktake(ctx, inventree.PartStocktakeGenerate{Part: &part.ID, GenerateEntry: true, GenerateReport: true})
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		a.Equal(http.StatusForbidden, apiErr.StatusCode)
		r.Nil(nonStaffGeneration.Output)
		t.Logf("non-staff stocktake generation characterization: rejected with HTTP %d", apiErr.StatusCode)
	})

	t.Run("global_search", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)

		category := fixture.ensure(t, testenv.FixtureCategory)
		location := fixture.ensure(t, testenv.FixtureLocation)
		part := fixture.ensure(t, testenv.FixturePart)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		manufacturer := fixture.ensure(t, testenv.FixtureManufacturer)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)

		mpn, err := fixture.run.Name("mpn")
		r.NoError(err)
		manufacturerPart, err := fixture.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{
			Part: part.ID, Manufacturer: manufacturer.ID, MPN: &mpn,
		})
		r.NoError(err)
		r.NotZero(manufacturerPart.PK)

		stockItem, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 5})
		r.NoError(err)
		r.NotZero(stockItem.PK)

		// FixturePurchaseOrder's reference ("PO-<supplierID>") must satisfy
		// InvenTree's configured purchase-order reference pattern, so it
		// cannot carry the same run-prefix marker every other fixture here
		// does (fixture.ensure's ownership check would reject it, matching
		// the same exception internal/tools's milestone fixture makes for
		// this one fixture kind); it is verified separately below by its
		// own reference text.
		purchaseOrder, err := fixture.shared.EnsureFixture(ctx, fixture.account, fixture.run, testenv.FixturePurchaseOrder)
		r.NoError(err)

		result, err := fixture.client.GlobalSearch(ctx, inventree.GlobalSearchQuery{
			Search:      fixture.run.Prefix,
			ObjectTypes: inventree.SupportedGlobalSearchObjectTypes,
			Limit:       25,
		})
		r.NoError(err)

		r.NotNil(result.Parts)
		a.True(globalSearchContainsPK(result.Parts.Results, part.ID, func(v inventree.Part) int { return v.PK }), "parts bucket must include the run-scoped fixture part")
		r.NotNil(result.PartCategories)
		a.True(globalSearchContainsPK(result.PartCategories.Results, category.ID, func(v inventree.Category) int { return v.PK }), "part_categories bucket must include the run-scoped fixture category")
		r.NotNil(result.StockLocations)
		a.True(globalSearchContainsPK(result.StockLocations.Results, location.ID, func(v inventree.StockLocation) int { return v.PK }), "stock_locations bucket must include the run-scoped fixture location")
		r.NotNil(result.StockItems)
		a.True(globalSearchContainsPK(result.StockItems.Results, stockItem.PK, func(v inventree.StockItem) int { return v.PK }), "stock_items bucket must include the stock item on the run-scoped fixture part")
		r.NotNil(result.Companies)
		a.True(globalSearchContainsPK(result.Companies.Results, supplier.ID, func(v inventree.Company) int { return v.PK }), "companies bucket must include the run-scoped fixture supplier")
		r.NotNil(result.SupplierParts)
		a.True(globalSearchContainsPK(result.SupplierParts.Results, supplierPart.ID, func(v inventree.SupplierPart) int { return v.PK }), "supplier_parts bucket must include the run-scoped fixture supplier part")
		r.NotNil(result.ManufacturerParts)
		a.True(globalSearchContainsPK(result.ManufacturerParts.Results, manufacturerPart.PK, func(v inventree.ManufacturerPart) int { return v.PK }), "manufacturer_parts bucket must include the run-scoped fixture manufacturer part")
		// result.PurchaseOrders is intentionally not asserted here: it is
		// requested (so its bucket must still be present), but the fixture
		// purchase order's reference cannot carry the shared run-prefix
		// marker, so it is verified separately below by its own reference.
		r.NotNil(result.PurchaseOrders)

		poResult, err := fixture.client.GlobalSearch(ctx, inventree.GlobalSearchQuery{
			Search:      purchaseOrder.Name,
			ObjectTypes: []inventree.GlobalSearchObjectType{inventree.GlobalSearchPurchaseOrder},
			Limit:       25,
		})
		r.NoError(err)
		r.NotNil(poResult.PurchaseOrders)
		a.True(globalSearchContainsPK(poResult.PurchaseOrders.Results, purchaseOrder.ID, func(v inventree.PurchaseOrder) int { return v.PK }), "purchase_orders bucket must include the fixture purchase order when searched by its own reference")

		t.Run("scopes_to_requested_object_types_only", func(t *testing.T) {
			r := require.New(t)
			narrow, err := fixture.client.GlobalSearch(ctx, inventree.GlobalSearchQuery{
				Search:      fixture.run.Prefix,
				ObjectTypes: []inventree.GlobalSearchObjectType{inventree.GlobalSearchCompany},
				Limit:       25,
			})
			r.NoError(err)
			r.NotNil(narrow.Companies)
			r.Nil(narrow.Parts)
			r.Nil(narrow.PartCategories)
			r.Nil(narrow.StockItems)
			r.Nil(narrow.StockLocations)
			r.Nil(narrow.SupplierParts)
			r.Nil(narrow.ManufacturerParts)
			r.Nil(narrow.PurchaseOrders)
		})

		t.Run("limit_applies_independently_per_bucket", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)

			// The run-scoped fixtures created above give "company" two real
			// matches (supplier, manufacturer) and "part" exactly one. limit:1
			// with both types requested together proves limit truncates each
			// bucket on its own -- company drops from 2 real matches to 1
			// returned result, while part's single match is not starved to 0
			// by company's truncation (which a shared/split global cap would
			// otherwise risk, since only one top-level "limit" field exists
			// on the InvenTree request).
			bounded, err := fixture.client.GlobalSearch(ctx, inventree.GlobalSearchQuery{
				Search:      fixture.run.Prefix,
				ObjectTypes: []inventree.GlobalSearchObjectType{inventree.GlobalSearchCompany, inventree.GlobalSearchPart},
				Limit:       1,
			})
			r.NoError(err)
			r.NotNil(bounded.Companies)
			a.Equal(2, bounded.Companies.Count, "two run-scoped companies (supplier, manufacturer) must both be counted as matches")
			a.Len(bounded.Companies.Results, 1, "company results must be truncated to limit:1 despite two real matches")
			r.NotNil(bounded.Parts)
			a.Equal(1, bounded.Parts.Count)
			a.Len(bounded.Parts.Results, 1, "part's own single match must not be starved by the company bucket's truncation")
		})

		t.Run("search_notes_gates_notes_only_matches", func(t *testing.T) {
			r := require.New(t)
			marker, err := fixture.run.Name("notesmarker")
			r.NoError(err)
			plainName, err := fixture.run.Name("plainnotescompany")
			r.NoError(err)
			notesCompany, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{Name: plainName, Currency: "USD", IsSupplier: true})
			r.NoError(err)
			r.NotZero(notesCompany.PK)
			_, err = fixture.client.UpdateCompany(ctx, notesCompany.PK, inventree.PatchFields{
				"notes": inventree.Set("contains " + marker + " marker"),
			})
			r.NoError(err)

			withoutNotes, err := fixture.client.GlobalSearch(ctx, inventree.GlobalSearchQuery{
				Search:      marker,
				ObjectTypes: []inventree.GlobalSearchObjectType{inventree.GlobalSearchCompany},
				Limit:       25,
			})
			r.NoError(err)
			r.NotNil(withoutNotes.Companies)
			r.False(globalSearchContainsPK(withoutNotes.Companies.Results, notesCompany.PK, func(v inventree.Company) int { return v.PK }), "a notes-only match must not surface without search_notes")

			withNotes, err := fixture.client.GlobalSearch(ctx, inventree.GlobalSearchQuery{
				Search:      marker,
				SearchNotes: true,
				ObjectTypes: []inventree.GlobalSearchObjectType{inventree.GlobalSearchCompany},
				Limit:       25,
			})
			r.NoError(err)
			r.NotNil(withNotes.Companies)
			r.True(globalSearchContainsPK(withNotes.Companies.Results, notesCompany.PK, func(v inventree.Company) int { return v.PK }), "search_notes:true must surface a notes-only match")
		})
	})
}

func globalSearchContainsPK[T any](records []T, id int, pk func(T) int) bool {
	for _, record := range records {
		if pk(record) == id {
			return true
		}
	}
	return false
}

func pollDataOutputForStocktakeCharacterization(ctx context.Context, client *inventree.Client, id int, timeout time.Duration) (inventree.DataOutput, bool) {
	deadline := time.Now().Add(timeout)
	latest := inventree.DataOutput{PK: id}
	for time.Now().Before(deadline) {
		current, err := client.GetDataOutput(ctx, id)
		if err != nil || current.PK != id {
			return latest, false
		}
		latest = current
		if current.Complete {
			return current, true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return latest, false
}

// instanceInfoAllowlistedGlobalSettingsForTest and instanceInfoAllowlistedUserSettingsForTest
// mirror internal/tools's F-S71 operator-approved allowlist (allowlistedGlobalSettings and
// allowlistedUserSettings in internal/tools/instance_info_tools.go -- keep this list in sync
// with that one by hand on any future allowlist change). They are duplicated here rather than
// imported to keep this package's Testcontainers suite free of a dependency on internal/tools,
// and to keep this test asserting the client contract the tool package consumes rather than
// reaching into that package's implementation.
var instanceInfoAllowlistedGlobalSettingsForTest = []string{
	"INVENTREE_INSTANCE",
	"INVENTREE_INSTANCE_ID",
	"INVENTREE_COMPANY_NAME",
	"INVENTREE_DEFAULT_CURRENCY",
	"CURRENCY_CODES",
	"BUILDORDER_REFERENCE_PATTERN",
	"SALESORDER_REFERENCE_PATTERN",
	"PURCHASEORDER_REFERENCE_PATTERN",
	"RETURNORDER_REFERENCE_PATTERN",
	"TRANSFERORDER_REFERENCE_PATTERN",
	"RETURNORDER_ENABLED",
	"TRANSFERORDER_ENABLED",
	"STOCKTAKE_ENABLE",
	"PROJECT_CODES_ENABLED",
	"PART_ENABLE_REVISION",
	"PART_ENABLE_LOCKING",
	"SERIAL_NUMBER_GLOBALLY_UNIQUE",
	"PARAMETER_ENFORCE_UNITS",
	"BARCODE_ENABLE",
	"LABEL_ENABLE",
	"REPORT_ENABLE",
	"CURRENCY_UPDATE_PLUGIN",
	"BARCODE_GENERATION_PLUGIN",
}

var instanceInfoAllowlistedUserSettingsForTest = []string{
	"DATE_DISPLAY_FORMAT",
}

// deactivatePart controls for InvenTree 1.5.0's independent "Cannot delete
// this part as it is still active" rule, pinned by the part_delete
// subtest's "clean_active_part_is_rejected" case, so every other blocking
// category in that subtest is exercised against an otherwise-deletable
// (inactive) part rather than being confounded by the separate active-state
// rejection.
func deactivatePart(t *testing.T, ctx context.Context, client *inventree.Client, id int) {
	t.Helper()
	r := require.New(t)
	_, err := client.UpdatePart(ctx, id, inventree.PatchFields{"active": inventree.Set(false)})
	r.NoError(err)
}

func requireDecimalEqual(t *testing.T, expected string, actual inventree.DecimalString) {
	t.Helper()
	r := require.New(t)
	expectedValue, ok := new(big.Rat).SetString(expected)
	r.True(ok)
	actualValue, ok := new(big.Rat).SetString(string(actual))
	r.True(ok)
	r.Zero(expectedValue.Cmp(actualValue))
}

func searchPartsRaw(t *testing.T, ctx context.Context, client *inventree.Client, query url.Values) []map[string]any {
	t.Helper()
	r := require.New(t)
	if query == nil {
		query = url.Values{}
	}
	query.Set("limit", "100")
	req, err := client.NewRequest(ctx, http.MethodGet, "/api/part/", query, nil)
	r.NoError(err)
	var out struct {
		Results []map[string]any `json:"results"`
	}
	r.NoError(client.DoJSON(req, &out))
	return out.Results
}

func rawPartIDs(rows []map[string]any) []any {
	ids := make([]any, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row["pk"])
	}
	return ids
}

func authenticatedMediaStatus(t *testing.T, ctx context.Context, rawURL string, token string) int {
	t.Helper()
	r := require.New(t)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	r.NoError(err)
	req.Header.Set("Authorization", "Token "+token)
	resp, err := http.DefaultClient.Do(req)
	r.NoError(err)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

type clientMethodFixture struct {
	shared  *testenv.SharedInvenTree
	run     *testenv.Run
	account *testenv.Account
	client  *inventree.Client
}

func newClientMethodFixture(t *testing.T, shared *testenv.SharedInvenTree) clientMethodFixture {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	run, err := shared.NewRun(t)
	r.NoError(err)
	account, err := shared.Account(ctx, run, testenv.AccountAdmin)
	r.NoError(err)
	client, err := shared.Client(account)
	r.NoError(err)

	return clientMethodFixture{
		shared:  shared,
		run:     run,
		account: account,
		client:  client,
	}
}

func (f clientMethodFixture) ensure(t *testing.T, kind testenv.FixtureKind) testenv.FixtureRecord {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	record, err := f.shared.EnsureFixture(ctx, f.account, f.run, kind)
	r.NoError(err)
	r.NoError(f.run.RequireOwnedName(record.Name))
	return record
}

// newNonStaffClient creates a run-scoped non-staff InvenTree account through
// the fixture's staff client and returns an authenticated client for it, so
// live tests can characterize permission boundaries that differ between
// staff and ordinary accounts. Extracted per the F-S56 review note in
// docs/TASKS.md, which flagged this block duplicated between the
// cross-object-tag and stocktake-generation subtests; F-S91 is its third
// caller.
func newNonStaffClient(t *testing.T, ctx context.Context, fixture clientMethodFixture, usernamePrefix, password string) *inventree.Client {
	t.Helper()
	r := require.New(t)
	nonStaffUsername, err := fixture.run.Name(usernamePrefix)
	r.NoError(err)
	createUserReq, err := fixture.client.NewRequest(ctx, http.MethodPost, "/api/user/", nil, map[string]any{
		"username":     nonStaffUsername,
		"first_name":   "Integration",
		"last_name":    "Nonstaff",
		"email":        strings.ToLower(nonStaffUsername) + "@example.test",
		"is_staff":     false,
		"is_superuser": false,
		"is_active":    true,
	})
	r.NoError(err)
	var nonStaffUser map[string]any
	r.NoError(fixture.client.DoJSON(createUserReq, &nonStaffUser))
	nonStaffID, ok := nonStaffUser["pk"].(float64)
	r.True(ok)
	setPasswordReq, err := fixture.client.NewRequest(ctx, http.MethodPut, fmt.Sprintf("/api/user/%d/set-password/", int(nonStaffID)), nil, map[string]any{"password": password, "override_warning": true})
	r.NoError(err)
	r.NoError(fixture.client.DoJSON(setPasswordReq, nil))
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.shared.Environment().BaseURL+"/api/user/me/token/?name="+url.QueryEscape(nonStaffUsername), nil)
	r.NoError(err)
	tokenReq.SetBasicAuth(nonStaffUsername, password)
	tokenResponse, err := http.DefaultClient.Do(tokenReq)
	r.NoError(err)
	t.Cleanup(func() { _ = tokenResponse.Body.Close() })
	r.Equal(http.StatusOK, tokenResponse.StatusCode)
	var nonStaffToken struct {
		Token string `json:"token"`
	}
	r.NoError(json.NewDecoder(tokenResponse.Body).Decode(&nonStaffToken))
	r.NotEmpty(nonStaffToken.Token)
	nonStaffClient, err := inventree.NewClient(inventree.Config{BaseURL: fixture.shared.Environment().BaseURL, Credential: inventree.Credential{Scheme: inventree.AuthSchemeToken, Token: nonStaffToken.Token}})
	r.NoError(err)
	return nonStaffClient
}

func createParameterTemplate(t *testing.T, client *inventree.Client, run *testenv.Run, suffix string, units string, choices string) inventree.ParameterTemplate {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	name, err := run.Name(suffix)
	r.NoError(err)
	req, err := client.NewRequest(ctx, http.MethodPost, "/api/parameter/template/", nil, map[string]any{
		"name":     name,
		"units":    units,
		"choices":  choices,
		"checkbox": false,
		"enabled":  true,
	})
	r.NoError(err)
	var created inventree.ParameterTemplate
	r.NoError(client.DoJSON(req, &created))
	r.NotZero(created.PK)
	r.Equal(name, created.Name)
	return created
}

func createCategoryParameterTemplate(t *testing.T, client *inventree.Client, categoryID int, templateID int) inventree.CategoryParameterTemplate {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	req, err := client.NewRequest(ctx, http.MethodPost, "/api/part/category/parameters/", nil, map[string]any{
		"category": categoryID,
		"template": templateID,
	})
	r.NoError(err)
	var created inventree.CategoryParameterTemplate
	r.NoError(client.DoJSON(req, &created))
	r.NotZero(created.PK)
	r.Equal(categoryID, created.Category)
	r.Equal(templateID, created.Template)
	return created
}

func setPartImage(t *testing.T, baseURL string, token string, partID int, filename string, content []byte) {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("image", filename)
	r.NoError(err)
	_, err = fileWriter.Write(content)
	r.NoError(err)
	r.NoError(writer.Close())

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, baseURL+"/api/part/"+strconv.Itoa(partID)+"/", &body)
	r.NoError(err)
	req.Header.Set("Authorization", "Token "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	r.NoError(err)
	defer func() {
		r.NoError(resp.Body.Close())
	}()
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		r.NoError(err)
		r.Failf("part image upload failed", "status %d body %s", resp.StatusCode, string(body))
	}
}

func attachmentIDs(attachments []inventree.Attachment) []int {
	ids := make([]int, 0, len(attachments))
	for _, attachment := range attachments {
		ids = append(ids, attachment.PK)
	}
	return ids
}

func purchaseOrderIDs(orders []inventree.PurchaseOrder) []int {
	ids := make([]int, 0, len(orders))
	for _, order := range orders {
		ids = append(ids, order.PK)
	}
	return ids
}

func purchaseOrderLineIDs(lines []inventree.PurchaseOrderLineItem) []int {
	ids := make([]int, 0, len(lines))
	for _, line := range lines {
		ids = append(ids, line.PK)
	}
	return ids
}

func purchaseOrderExtraLineIDs(lines []inventree.PurchaseOrderExtraLine) []int {
	ids := make([]int, 0, len(lines))
	for _, line := range lines {
		ids = append(ids, line.PK)
	}
	return ids
}

func stockLocationIDs(locations []inventree.StockLocation) []int {
	ids := make([]int, 0, len(locations))
	for _, location := range locations {
		ids = append(ids, location.PK)
	}
	return ids
}

func stockLocationTypeIDs(types []inventree.StockLocationType) []int {
	ids := make([]int, 0, len(types))
	for _, locationType := range types {
		ids = append(ids, locationType.PK)
	}
	return ids
}

func hasStockTrackingType(entries []inventree.StockTracking, trackingType int) bool {
	for _, entry := range entries {
		if entry.TrackingType == trackingType {
			return true
		}
	}
	return false
}

func bomItemIDs(items []inventree.BomItem) []int {
	ids := make([]int, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.PK)
	}
	return ids
}

func buildIDs(builds []inventree.Build) []int {
	ids := make([]int, 0, len(builds))
	for _, build := range builds {
		ids = append(ids, build.PK)
	}
	return ids
}

func salesOrderLineIDs(lines []inventree.SalesOrderLineItem) []int {
	ids := make([]int, 0, len(lines))
	for _, line := range lines {
		ids = append(ids, line.PK)
	}
	return ids
}

func partRelationIDs(relations []inventree.PartRelation) []int {
	ids := make([]int, 0, len(relations))
	for _, relation := range relations {
		ids = append(ids, relation.PK)
	}
	return ids
}

func partIDs(parts []inventree.Part) []int {
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		ids = append(ids, part.PK)
	}
	return ids
}

func stockItemIDs(items []inventree.StockItem) []int {
	ids := make([]int, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.PK)
	}
	return ids
}

func tinyPNG() []byte {
	return tinyPNGColor(color.NRGBA{R: 0, G: 0, B: 0, A: 0})
}

func alternateTinyPNG() []byte {
	return tinyPNGColor(color.NRGBA{R: 255, G: 0, B: 0, A: 255})
}

func tinyPNGColor(pixel color.NRGBA) []byte {
	var buf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, pixel)
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
