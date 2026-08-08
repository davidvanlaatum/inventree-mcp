//go:build !no_integration_tests

package inventree_test

import (
	"bytes"
	"context"
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
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
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
	t.Logf("starting client method integration stack with image %s, expected version %s, expected API %s", opts.Image, opts.ExpectedVersion, opts.ExpectedAPIVersion)
	shared, err := testenv.StartSharedInvenTree(ctx, opts)
	r.NoError(err)
	r.NotNil(shared)
	t.Cleanup(testenv.CleanupForTest(t, func() error {
		return shared.Close(context.WithoutCancel(ctx))
	}))

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
		supplierPart, err := fixture.client.CreateSupplierPart(ctx, inventree.SupplierPartCreate{
			Part:     part.PK,
			Supplier: supplier.PK,
			SKU:      sku,
			Active:   dvgoutils.Ptr(false),
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
		updatedSupplierPart, err := fixture.client.UpdateSupplierPart(ctx, supplierPart.PK, inventree.PatchFields{"description": inventree.Set("updated supplier part"), "primary": inventree.Set(false)})
		r.NoError(err)
		r.NotNil(updatedSupplierPart.Description)
		r.Equal("updated supplier part", *updatedSupplierPart.Description)

		mpn, err := fixture.run.Name("mpn")
		r.NoError(err)
		manufacturerPart, err := fixture.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{
			Part:         part.PK,
			Manufacturer: manufacturer.PK,
			MPN:          dvgoutils.Ptr(mpn),
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
		updatedManufacturerPart, err := fixture.client.UpdateManufacturerPart(ctx, manufacturerPart.PK, inventree.PatchFields{"description": inventree.Set("updated manufacturer part")})
		r.NoError(err)
		r.NotNil(updatedManufacturerPart.Description)
		r.Equal("updated manufacturer part", *updatedManufacturerPart.Description)

		manufacturerParts, err := fixture.client.SearchManufacturerParts(ctx, inventree.ManufacturerPartQuery{
			Part:         part.PK,
			Manufacturer: manufacturer.PK,
			MPN:          mpn,
		})
		r.NoError(err)
		r.NotEmpty(manufacturerParts)
		r.Equal(manufacturerPart.PK, manufacturerParts[0].PK)
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

		categoryTemplates, err := fixture.client.SearchCategoryParameterTemplates(ctx, inventree.CategoryParameterTemplateQuery{CategoryID: category.ID})
		r.NoError(err)
		r.NotEmpty(categoryTemplates)
		r.Equal(categoryTemplate.PK, categoryTemplates[0].PK)
		r.Equal(category.ID, categoryTemplates[0].Category)
		r.Equal(template.PK, categoryTemplates[0].Template)

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
		orderBeforeExtra, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.NotNil(orderBeforeExtra.TotalPrice)
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
		orderAfterZeroExtra, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.NotNil(orderAfterZeroExtra.TotalPrice)
		r.Equal(*orderBeforeExtra.TotalPrice, *orderAfterZeroExtra.TotalPrice, "zero-priced extra line must preserve InvenTree's exact total representation")
		updatedExtra, err := fixture.client.UpdatePurchaseOrderExtraLine(ctx, extra.PK, inventree.PatchFields{"quantity": inventree.Set(2.0), "price": inventree.Set("-0.5"), "price_currency": inventree.Set("AUD")})
		r.NoError(err)
		r.Equal(2.0, updatedExtra.Quantity)
		r.NotNil(updatedExtra.Price)
		requireDecimalEqual(t, "-0.5", *updatedExtra.Price)
		orderAfterDiscount, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.NotNil(orderAfterDiscount.TotalPrice)
		beforeTotal, ok := new(big.Rat).SetString(string(*orderBeforeExtra.TotalPrice))
		r.True(ok)
		afterDiscountTotal, ok := new(big.Rat).SetString(string(*orderAfterDiscount.TotalPrice))
		r.True(ok)
		r.Zero(new(big.Rat).Sub(afterDiscountTotal, beforeTotal).Cmp(big.NewRat(-1, 1)), "quantity 2 at unit price -0.5 must reduce the exact order total by 1")
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
		updatedLine, err := fixture.client.UpdatePurchaseOrderLine(ctx, line.PK, inventree.PatchFields{"order": inventree.Set(order.PK), "part": inventree.Set(supplierPart.ID), "quantity": inventree.Set(4.0)})
		r.NoError(err)
		r.Equal(4.0, updatedLine.Quantity)
		r.NoError(fixture.client.IssuePurchaseOrder(ctx, order.PK))
		issuedOrder, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.Equal(inventree.PurchaseOrderStatusPlaced, issuedOrder.Status)
		received, err := fixture.client.ReceivePurchaseOrder(ctx, order.PK, inventree.PurchaseOrderReceive{Items: []inventree.PurchaseOrderReceiveItem{{LineItem: line.PK, Location: &destination.ID, Quantity: "1.5", BatchCode: dvgoutils.Ptr("client-method-receipt")}}})
		r.NoError(err)
		r.NotEmpty(received)
		r.Equal(1.5, received[0].Quantity)
		r.NotNil(received[0].Location)
		r.Equal(destination.ID, *received[0].Location)
		lineAfterReceipt, err := fixture.client.GetPurchaseOrderLine(ctx, line.PK)
		r.NoError(err)
		r.Equal(1.5, lineAfterReceipt.Received)
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
