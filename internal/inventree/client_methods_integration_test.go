//go:build !no_integration_tests

package inventree_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
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

		templates, err := fixture.client.SearchParameterTemplates(ctx, inventree.SearchQuery{Search: template.Name})
		r.NoError(err)
		r.NotEmpty(templates)
		r.Equal(template.PK, templates[0].PK)
		r.Equal("10k,22k", templates[0].Choices)

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

	t.Run("po", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newClientMethodFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		order := createPurchaseOrder(t, fixture.client, supplier.ID)
		line := createPurchaseOrderLine(t, fixture.client, order.PK, supplierPart.ID, 3)

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
	})
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

func createPurchaseOrder(t *testing.T, client *inventree.Client, supplierID int) inventree.PurchaseOrder {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	reference := "PO-" + strconv.Itoa(supplierID)
	req, err := client.NewRequest(ctx, http.MethodPost, "/api/order/po/", nil, map[string]any{
		"reference":   reference,
		"supplier":    supplierID,
		"description": "Run-scoped integration fixture purchase order",
	})
	r.NoError(err)
	var created inventree.PurchaseOrder
	r.NoError(client.DoJSON(req, &created))
	r.NotZero(created.PK)
	r.Equal(reference, created.Reference)
	r.Equal(supplierID, created.Supplier)
	return created
}

func createPurchaseOrderLine(t *testing.T, client *inventree.Client, orderID int, supplierPartID int, quantity float64) inventree.PurchaseOrderLineItem {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	req, err := client.NewRequest(ctx, http.MethodPost, "/api/order/po-line/", nil, map[string]any{
		"order":    orderID,
		"part":     supplierPartID,
		"quantity": quantity,
	})
	r.NoError(err)
	var created inventree.PurchaseOrderLineItem
	r.NoError(client.DoJSON(req, &created))
	r.NotZero(created.PK)
	r.Equal(orderID, created.Order)
	r.Equal(quantity, created.Quantity)
	return created
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
