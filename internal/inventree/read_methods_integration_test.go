//go:build !no_integration_tests

package inventree_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestReadOnlyClientReads(t *testing.T) {
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	if testenv.SkipDocker(os.Getenv) || testing.Short() {
		t.Skipf("Docker-backed InvenTree integration test excluded by %s or -short", testenv.EnvSkipDocker)
	}
	t.Parallel()

	opts := testenv.DefaultTestOptions(t)
	t.Logf("starting read-only client integration stack with image %s, expected version %s, expected API %s", opts.Image, opts.ExpectedVersion, opts.ExpectedAPIVersion)
	shared, err := testenv.StartSharedInvenTree(ctx, opts)
	r.NoError(err)
	r.NotNil(shared)
	t.Cleanup(testenv.CleanupForTest(t, func() error {
		return shared.Close(context.WithoutCancel(ctx))
	}))

	t.Run("part_category", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newReadOnlyClientFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		part := fixture.ensure(t, testenv.FixturePart)

		parts, err := fixture.client.SearchParts(ctx, url.Values{"name": []string{part.Name}})
		r.NoError(err)
		r.NotEmpty(parts)
		r.Equal(part.ID, parts[0].PK)
		gotPart, err := fixture.client.GetPart(ctx, part.ID)
		r.NoError(err)
		r.Equal(part.Name, gotPart.Name)

		categories, err := fixture.client.SearchPartCategories(ctx, url.Values{"name": []string{category.Name}})
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
		fixture := newReadOnlyClientFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		manufacturer := fixture.ensure(t, testenv.FixtureManufacturer)
		part := fixture.ensure(t, testenv.FixturePart)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)

		suppliers, err := fixture.client.SearchSuppliers(ctx, url.Values{"search": []string{supplier.Name}})
		r.NoError(err)
		r.NotEmpty(suppliers)
		r.Equal(supplier.ID, suppliers[0].PK)
		r.True(suppliers[0].IsSupplier)

		manufacturers, err := fixture.client.SearchManufacturers(ctx, url.Values{"search": []string{manufacturer.Name}})
		r.NoError(err)
		r.NotEmpty(manufacturers)
		r.Equal(manufacturer.ID, manufacturers[0].PK)
		r.True(manufacturers[0].IsManufacturer)

		supplierParts, err := fixture.client.SearchSupplierParts(ctx, url.Values{"SKU": []string{supplierPart.Name}})
		r.NoError(err)
		r.NotEmpty(supplierParts)
		r.Equal(supplierPart.ID, supplierParts[0].PK)
		r.Equal(part.ID, supplierParts[0].Part)
		r.Equal(supplier.ID, supplierParts[0].Supplier)
	})

	t.Run("stock", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newReadOnlyClientFixture(t, shared)
		location := fixture.ensure(t, testenv.FixtureLocation)
		part := fixture.ensure(t, testenv.FixturePart)

		locations, err := fixture.client.SearchStockLocations(ctx, url.Values{"search": []string{location.Name}})
		r.NoError(err)
		r.NotEmpty(locations)
		r.Equal(location.ID, locations[0].PK)
		gotLocation, err := fixture.client.GetStockLocation(ctx, location.ID)
		r.NoError(err)
		r.Equal(location.Name, gotLocation.Name)

		stockItems, err := fixture.client.SearchStockItems(ctx, url.Values{"part": []string{strconv.Itoa(part.ID)}})
		r.NoError(err)
		r.Empty(stockItems)
	})

	t.Run("parameter", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newReadOnlyClientFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		part := fixture.ensure(t, testenv.FixturePart)
		template := createParameterTemplate(t, fixture.client, fixture.run, "Resistance", "ohm", "10k,22k")
		categoryTemplate := createCategoryParameterTemplate(t, fixture.client, category.ID, template.PK)
		parameter := createPartParameter(t, fixture.client, part.ID, template.PK, "10k")

		parameters, err := fixture.client.SearchPartParameters(ctx, url.Values{"part": []string{strconv.Itoa(part.ID)}})
		r.NoError(err)
		r.NotEmpty(parameters)
		r.Equal(parameter.PK, parameters[0].PK)
		r.Equal("part.part", parameters[0].ModelType)
		r.Equal(part.ID, parameters[0].ModelID)

		templates, err := fixture.client.SearchParameterTemplates(ctx, url.Values{"search": []string{template.Name}})
		r.NoError(err)
		r.NotEmpty(templates)
		r.Equal(template.PK, templates[0].PK)
		r.Equal("10k,22k", templates[0].Choices)

		categoryTemplates, err := fixture.client.SearchCategoryParameterTemplates(ctx, url.Values{"category": []string{strconv.Itoa(category.ID)}})
		r.NoError(err)
		r.NotEmpty(categoryTemplates)
		r.Equal(categoryTemplate.PK, categoryTemplates[0].PK)
		r.Equal(category.ID, categoryTemplates[0].Category)
		r.Equal(template.PK, categoryTemplates[0].Template)
	})

	t.Run("attachment", func(t *testing.T) {
		r := require.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newReadOnlyClientFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		linkAttachment := createLinkAttachment(t, fixture.client, part.ID, "https://example.test/datasheet.pdf")
		fileAttachment := createFileAttachment(t, shared.Environment().BaseURL, fixture.account.Token, part.ID, "datasheet.txt", "datasheet bytes")

		attachments, err := fixture.client.ListAttachments(ctx, url.Values{
			"model_type": []string{"part"},
			"model_id":   []string{strconv.Itoa(part.ID)},
		})
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
	})
}

type readOnlyClientFixture struct {
	shared  *testenv.SharedInvenTree
	run     *testenv.Run
	account *testenv.Account
	client  *inventree.Client
}

func newReadOnlyClientFixture(t *testing.T, shared *testenv.SharedInvenTree) readOnlyClientFixture {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	run, err := shared.NewRun(t)
	r.NoError(err)
	account, err := shared.Account(ctx, run, testenv.AccountAdmin)
	r.NoError(err)
	client, err := shared.Client(account)
	r.NoError(err)

	return readOnlyClientFixture{
		shared:  shared,
		run:     run,
		account: account,
		client:  client,
	}
}

func (f readOnlyClientFixture) ensure(t *testing.T, kind testenv.FixtureKind) testenv.FixtureRecord {
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

func createPartParameter(t *testing.T, client *inventree.Client, partID int, templateID int, data string) inventree.Parameter {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	req, err := client.NewRequest(ctx, http.MethodPost, "/api/parameter/", nil, map[string]any{
		"template":   templateID,
		"model_type": "part.part",
		"model_id":   partID,
		"data":       data,
	})
	r.NoError(err)
	var created inventree.Parameter
	r.NoError(client.DoJSON(req, &created))
	r.NotZero(created.PK)
	r.Equal("part.part", created.ModelType)
	r.Equal(partID, created.ModelID)
	return created
}

func createLinkAttachment(t *testing.T, client *inventree.Client, partID int, link string) inventree.Attachment {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	req, err := client.NewRequest(ctx, http.MethodPost, "/api/attachment/", nil, map[string]any{
		"model_type": "part",
		"model_id":   partID,
		"link":       link,
		"comment":    "Run-scoped integration fixture link attachment",
	})
	r.NoError(err)
	var created inventree.Attachment
	r.NoError(client.DoJSON(req, &created))
	r.NotZero(created.PK)
	r.Equal("part", created.ModelType)
	r.Equal(partID, created.ModelID)
	return created
}

func createFileAttachment(t *testing.T, baseURL string, token string, partID int, filename string, content string) inventree.Attachment {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	r.NoError(writer.WriteField("model_type", "part"))
	r.NoError(writer.WriteField("model_id", strconv.Itoa(partID)))
	r.NoError(writer.WriteField("comment", "Run-scoped integration fixture file attachment"))
	fileWriter, err := writer.CreateFormFile("attachment", filename)
	r.NoError(err)
	_, err = io.WriteString(fileWriter, content)
	r.NoError(err)
	r.NoError(writer.Close())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/attachment/", &body)
	r.NoError(err)
	req.Header.Set("Authorization", "Token "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	r.NoError(err)
	defer func() {
		r.NoError(resp.Body.Close())
	}()
	r.Equal(http.StatusCreated, resp.StatusCode)
	var created inventree.Attachment
	r.NoError(json.NewDecoder(resp.Body).Decode(&created))
	r.NotZero(created.PK)
	r.Equal("part", created.ModelType)
	r.Equal(partID, created.ModelID)
	r.NotNil(created.Attachment)
	return created
}

func attachmentIDs(attachments []inventree.Attachment) []int {
	ids := make([]int, 0, len(attachments))
	for _, attachment := range attachments {
		ids = append(ids, attachment.PK)
	}
	return ids
}
