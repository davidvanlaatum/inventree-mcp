package tools

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupToolAuthorizationsUseReadOnlyScope(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	for _, name := range lookupToolNames {
		auth, ok := ToolAuthorizations[name]
		r.True(ok, "missing authorization for %s", name)
		a.Equal("read_only", auth.MutationClass)
		a.Equal([]string{ScopeInventreeRead}, auth.Scopes)
		a.Equal(ReadOnlyAnnotations, auth.Annotations)
	}
}

func TestSearchPartsReturnsClarificationForAmbiguousResults(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts: []inventree.Part{
			{PK: 10, Name: "10k resistor"},
			{PK: 11, Name: "10k resistor precision"},
		},
	}
	handler := searchParts(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "10k"})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusClarificationRequired, result.Content[0].(*mcp.TextContent).Text)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("part_id", output.Clarification.Retry)
	a.Equal("part", output.Clarification.Field)
	a.Equal("10k", output.Clarification.RetryValues["search"])
	r.Len(output.Clarification.Candidates, 2)
	a.Equal("10", output.Clarification.Candidates[0].ID)
	a.Equal("10k resistor", output.Clarification.Candidates[0].Label)
	a.Equal("/api/part/10/", output.Clarification.Candidates[0].URL)
	a.Equal(inventree.SearchQuery{Search: "10k", Limit: 20}, fake.lastSearchPartsQuery)
}

func TestGetPartReturnsStructuredNotFoundForMissingRecord(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		getPartErr: &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound},
	}
	handler := getPart(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, IDInput{ID: 404})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusNotFound, result.Content[0].(*mcp.TextContent).Text)
	a.Equal(StatusNotFound, output.Status)
}

func TestSearchCompaniesReturnsNotFoundForNoResults(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}
	handler := searchCompanies(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "missing"})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusNotFound, result.Content[0].(*mcp.TextContent).Text)
	a.Equal(StatusNotFound, output.Status)
	a.Empty(output.Results)
}

func TestAttachmentMetadataToolsGateScopeAndRedactURLs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fileURL := "/media/file.pdf?signature=secret#fragment"
	thumbURL := "https://inventory.example.test/media/thumb.png?signature=secret"
	linkURL := "https://user:pass@example.test/datasheet.pdf?token=secret#fragment"
	fake := &fakeMilestoneLookupClient{
		attachments: []inventree.Attachment{{
			PK:         90,
			ModelType:  "part",
			ModelID:    10,
			Attachment: &fileURL,
			Thumbnail:  &thumbURL,
			Link:       &linkURL,
			Filename:   "datasheet.pdf",
		}},
		attachment: inventree.Attachment{
			PK:         90,
			ModelType:  "part",
			ModelID:    10,
			Attachment: &fileURL,
			Thumbnail:  &thumbURL,
			Link:       &linkURL,
			Filename:   "datasheet.pdf",
		},
	}

	listHandler := listAttachments(depsForFake(fake))
	result, listOutput, err := listHandler(ctx, &mcp.CallToolRequest{}, ObjectLookupInput{ModelType: "part", ModelID: 10})
	r.NoError(err)
	r.NotNil(result)
	r.Len(listOutput.Results, 1)
	a.Equal("/media/file.pdf", listOutput.Results[0].AttachmentURL)
	a.Equal("https://inventory.example.test/media/thumb.png", listOutput.Results[0].ThumbnailURL)
	a.Equal("https://example.test/datasheet.pdf", listOutput.Results[0].LinkURL)

	getHandler := getAttachmentMetadata(depsForFake(fake))
	_, recordOutput, err := getHandler(ctx, &mcp.CallToolRequest{}, IDInput{ID: 90})
	r.NoError(err)
	a.Equal("/media/file.pdf", recordOutput.Record.AttachmentURL)
	a.Equal("https://example.test/datasheet.pdf", recordOutput.Record.LinkURL)

	_, _, err = listHandler(ctx, &mcp.CallToolRequest{}, ObjectLookupInput{ModelType: "salesorder", ModelID: 10})
	r.ErrorContains(err, `model type "salesorder" is out of scope`)

	fake.attachment.ModelType = "salesorder"
	_, _, err = getHandler(ctx, &mcp.CallToolRequest{}, IDInput{ID: 91})
	r.ErrorContains(err, `model type "salesorder" is out of scope`)
}

func TestSearchStockItemsUsesStableFilters(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		stockItems: []inventree.StockItem{{PK: 50, Part: 10, Quantity: 2}},
	}
	handler := searchStockItems(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, StockItemsInput{PartID: 10, LocationID: 40, Limit: 250})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	a.Equal(1, output.Count)
	a.Equal(inventree.StockItemQuery{PartID: 10, LocationID: 40, Limit: 100}, fake.lastSearchStockItemsQuery)
}

func TestLookupHandlersPassStructuredQueriesToClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(context.Context, *require.Assertions, *fakeMilestoneLookupClient)
	}{
		{
			name: "categories",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.categories = []inventree.Category{{PK: 20, Name: "passives"}}
				_, _, err := searchPartCategories(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "pass", Limit: 101, Offset: 2})
				r.NoError(err)
				r.Equal(inventree.SearchQuery{Search: "pass", Limit: 100, Offset: 2}, fake.lastSearchPartCategoriesQuery)
			},
		},
		{
			name: "parameter templates",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.parameterTemplates = []inventree.ParameterTemplate{{PK: 70, Name: "Resistance"}}
				_, _, err := searchParameterTemplates(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "resistance", Limit: 5, Offset: 1})
				r.NoError(err)
				r.Equal(inventree.SearchQuery{Search: "resistance", Limit: 5, Offset: 1}, fake.lastSearchParameterTemplatesQuery)
			},
		},
		{
			name: "part parameters",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.parameters = []inventree.Parameter{{PK: 60, ModelID: 10}}
				_, _, err := getPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, PartParametersInput{PartID: 10, Limit: 0, Offset: 3})
				r.NoError(err)
				r.Equal(inventree.PartParameterQuery{PartID: 10, Limit: 20, Offset: 3}, fake.lastSearchPartParametersQuery)
			},
		},
		{
			name: "suppliers",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.suppliers = []inventree.Company{{PK: 30, Name: "supplier", IsSupplier: true}}
				_, _, err := searchSuppliers(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "sup", Limit: 6})
				r.NoError(err)
				r.Equal(inventree.SearchQuery{Search: "sup", Limit: 6}, fake.lastSearchSuppliersQuery)
			},
		},
		{
			name: "manufacturers",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.manufacturers = []inventree.Company{{PK: 31, Name: "maker", IsManufacturer: true}}
				_, _, err := searchManufacturers(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "make", Offset: 4})
				r.NoError(err)
				r.Equal(inventree.SearchQuery{Search: "make", Limit: 20, Offset: 4}, fake.lastSearchManufacturersQuery)
			},
		},
		{
			name: "stock locations",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.stockLocations = []inventree.StockLocation{{PK: 40, Name: "bin"}}
				_, _, err := searchStockLocations(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "bin", Limit: 12})
				r.NoError(err)
				r.Equal(inventree.SearchQuery{Search: "bin", Limit: 12}, fake.lastSearchStockLocationsQuery)
			},
		},
		{
			name: "attachments",
			run: func(ctx context.Context, r *require.Assertions, fake *fakeMilestoneLookupClient) {
				fake.attachments = []inventree.Attachment{{PK: 90, ModelType: "part", ModelID: 10, Filename: "datasheet.pdf"}}
				_, _, err := listAttachments(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, ObjectLookupInput{ModelType: "part", ModelID: 10, Search: "data", Limit: 3, Offset: 2})
				r.NoError(err)
				r.Equal(inventree.AttachmentQuery{ModelType: "part", ModelID: 10, Search: "data", Limit: 3, Offset: 2}, fake.lastListAttachmentsQuery)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			tt.run(ctx, r, &fakeMilestoneLookupClient{})
		})
	}
}

func TestDownloadAttachmentReturnsTextOrBase64WithDigest(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		downloadedAttachment: inventree.DownloadedAttachment{
			Attachment:  inventree.Attachment{PK: 90, Filename: "datasheet.txt"},
			Content:     []byte("hello"),
			ContentType: "text/plain; charset=utf-8",
			SourceURL:   "https://inventory.example.test/media/datasheet.txt",
		},
	}
	handler := downloadAttachment(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, DownloadInput{ID: 90, MaxBytes: 100})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	a.Equal("hello", output.Text)
	a.Empty(output.Base64)
	a.Equal("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", output.SHA256)
	a.Equal(int64(100), fake.lastAttachmentMaxBytes)
}

func TestDownloadPartImageReturnsBase64ForBinaryContent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		downloadedPartImage: inventree.DownloadedPartImage{
			Part:        inventree.Part{PK: 10, Name: "resistor"},
			Content:     []byte{0x89, 0x50, 0x4e, 0x47},
			ContentType: "image/png",
			SourceURL:   "https://inventory.example.test/media/resistor.png",
		},
	}
	handler := downloadPartImage(depsForFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, DownloadInput{ID: 10})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	a.Empty(output.Text)
	a.Equal("iVBORw==", output.Base64)
	a.Equal(defaultDownloadMaxBytes, fake.lastPartImageMaxBytes)
}

func depsForFake(fake *fakeMilestoneLookupClient) Dependencies {
	return Dependencies{
		ClientFromContext: func(context.Context) (any, error) {
			return fake, nil
		},
	}
}

type fakeMilestoneLookupClient struct {
	parts                      []inventree.Part
	categories                 []inventree.Category
	companies                  []inventree.Company
	suppliers                  []inventree.Company
	manufacturers              []inventree.Company
	stockLocations             []inventree.StockLocation
	stockItems                 []inventree.StockItem
	parameters                 []inventree.Parameter
	parameterTemplates         []inventree.ParameterTemplate
	categoryParameterTemplates []inventree.CategoryParameterTemplate
	attachments                []inventree.Attachment
	supplierParts              []inventree.SupplierPart
	manufacturerParts          []inventree.ManufacturerPart
	attachment                 inventree.Attachment
	downloadedAttachment       inventree.DownloadedAttachment
	downloadedPartImage        inventree.DownloadedPartImage
	getPartErr                 error
	part                       inventree.Part
	createdPart                bool
	createdPartParameter       bool
	createPartParameterCount   int
	createdCompany             bool
	createdSupplierPart        bool
	createdManufacturerPart    bool

	lastSearchPartsQuery                      inventree.SearchQuery
	lastSearchPartCategoriesQuery             inventree.SearchQuery
	lastSearchPartParametersQuery             inventree.PartParameterQuery
	lastSearchParameterTemplatesQuery         inventree.SearchQuery
	lastGetParameterTemplateID                int
	lastSearchCategoryParameterTemplatesQuery inventree.CategoryParameterTemplateQuery
	lastSearchCompaniesQuery                  inventree.SearchQuery
	lastSearchSuppliersQuery                  inventree.SearchQuery
	lastSearchManufacturersQuery              inventree.SearchQuery
	lastSearchStockLocationsQuery             inventree.SearchQuery
	lastSearchStockItemsQuery                 inventree.StockItemQuery
	lastListAttachmentsQuery                  inventree.AttachmentQuery
	lastSearchSupplierPartsQuery              inventree.SupplierPartQuery
	lastSearchManufacturerPartsQuery          inventree.ManufacturerPartQuery
	lastCreatePart                            inventree.PartCreate
	lastCreatePartParameter                   inventree.ParameterCreate
	lastCreateCompany                         inventree.CompanyCreate
	lastCreateSupplierPart                    inventree.SupplierPartCreate
	lastCreateManufacturerPart                inventree.ManufacturerPartCreate
	lastUpdatePartFields                      inventree.PatchFields
	lastUpdatePartParameterFields             inventree.PatchFields
	updatePartParameterCount                  int
	lastAttachmentMaxBytes                    int64
	lastPartImageMaxBytes                     int64
}

func (f *fakeMilestoneLookupClient) SearchParts(_ context.Context, query inventree.SearchQuery) ([]inventree.Part, error) {
	f.lastSearchPartsQuery = query
	return f.parts, nil
}

func (f *fakeMilestoneLookupClient) GetPart(_ context.Context, id int) (inventree.Part, error) {
	if f.getPartErr != nil {
		return inventree.Part{}, f.getPartErr
	}
	if f.part.PK != 0 {
		return f.part, nil
	}
	return inventree.Part{PK: id, Name: "part"}, nil
}

func (f *fakeMilestoneLookupClient) SearchPartCategories(_ context.Context, query inventree.SearchQuery) ([]inventree.Category, error) {
	f.lastSearchPartCategoriesQuery = query
	return f.categories, nil
}

func (f *fakeMilestoneLookupClient) SearchPartParameters(_ context.Context, query inventree.PartParameterQuery) ([]inventree.Parameter, error) {
	f.lastSearchPartParametersQuery = query
	return f.parameters, nil
}

func (f *fakeMilestoneLookupClient) SearchParameterTemplates(_ context.Context, query inventree.SearchQuery) ([]inventree.ParameterTemplate, error) {
	f.lastSearchParameterTemplatesQuery = query
	return f.parameterTemplates, nil
}

func (f *fakeMilestoneLookupClient) GetParameterTemplate(_ context.Context, id int) (inventree.ParameterTemplate, error) {
	f.lastGetParameterTemplateID = id
	for _, template := range f.parameterTemplates {
		if template.PK == id {
			return template, nil
		}
	}
	return inventree.ParameterTemplate{PK: id, Name: "template", Enabled: true}, nil
}

func (f *fakeMilestoneLookupClient) SearchCategoryParameterTemplates(_ context.Context, query inventree.CategoryParameterTemplateQuery) ([]inventree.CategoryParameterTemplate, error) {
	f.lastSearchCategoryParameterTemplatesQuery = query
	return f.categoryParameterTemplates, nil
}

func (f *fakeMilestoneLookupClient) SearchCompanies(_ context.Context, query inventree.SearchQuery) ([]inventree.Company, error) {
	f.lastSearchCompaniesQuery = query
	return f.companies, nil
}

func (f *fakeMilestoneLookupClient) CreateCompany(_ context.Context, input inventree.CompanyCreate) (inventree.Company, error) {
	f.createdCompany = true
	f.lastCreateCompany = input
	return inventree.Company{PK: 30, Name: input.Name, Currency: input.Currency, IsSupplier: input.IsSupplier, IsManufacturer: input.IsManufacturer}, nil
}

func (f *fakeMilestoneLookupClient) SearchSuppliers(_ context.Context, query inventree.SearchQuery) ([]inventree.Company, error) {
	f.lastSearchSuppliersQuery = query
	return f.suppliers, nil
}

func (f *fakeMilestoneLookupClient) SearchManufacturers(_ context.Context, query inventree.SearchQuery) ([]inventree.Company, error) {
	f.lastSearchManufacturersQuery = query
	return f.manufacturers, nil
}

func (f *fakeMilestoneLookupClient) SearchStockLocations(_ context.Context, query inventree.SearchQuery) ([]inventree.StockLocation, error) {
	f.lastSearchStockLocationsQuery = query
	return f.stockLocations, nil
}

func (f *fakeMilestoneLookupClient) SearchStockItems(_ context.Context, query inventree.StockItemQuery) ([]inventree.StockItem, error) {
	f.lastSearchStockItemsQuery = query
	return f.stockItems, nil
}

func (f *fakeMilestoneLookupClient) ListAttachments(_ context.Context, query inventree.AttachmentQuery) ([]inventree.Attachment, error) {
	f.lastListAttachmentsQuery = query
	return f.attachments, nil
}

func (f *fakeMilestoneLookupClient) GetAttachmentMetadata(_ context.Context, id int) (inventree.Attachment, error) {
	if f.attachment.PK != 0 {
		return f.attachment, nil
	}
	return inventree.Attachment{PK: id, Filename: "attachment"}, nil
}

func (f *fakeMilestoneLookupClient) DownloadAttachment(_ context.Context, _ int, _ inventree.AttachmentContentMode, maxBytes int64) (inventree.DownloadedAttachment, error) {
	f.lastAttachmentMaxBytes = maxBytes
	return f.downloadedAttachment, nil
}

func (f *fakeMilestoneLookupClient) DownloadPartImage(_ context.Context, _ int, _ inventree.AttachmentContentMode, maxBytes int64) (inventree.DownloadedPartImage, error) {
	f.lastPartImageMaxBytes = maxBytes
	return f.downloadedPartImage, nil
}

func (f *fakeMilestoneLookupClient) CreatePart(_ context.Context, input inventree.PartCreate) (inventree.Part, error) {
	f.createdPart = true
	f.lastCreatePart = input
	return inventree.Part{PK: 10, Name: input.Name, Category: input.Category, Purchaseable: input.Purchaseable != nil && *input.Purchaseable}, nil
}

func (f *fakeMilestoneLookupClient) UpdatePart(_ context.Context, id int, fields inventree.PatchFields) (inventree.Part, error) {
	f.lastUpdatePartFields = fields
	return inventree.Part{PK: id}, nil
}

func (f *fakeMilestoneLookupClient) CreatePartParameter(_ context.Context, input inventree.ParameterCreate) (inventree.Parameter, error) {
	f.createdPartParameter = true
	f.createPartParameterCount++
	f.lastCreatePartParameter = input
	return inventree.Parameter{PK: 61, Template: input.Template, ModelType: input.ModelType, ModelID: input.ModelID, Data: input.Data}, nil
}

func (f *fakeMilestoneLookupClient) UpdatePartParameter(_ context.Context, id int, fields inventree.PatchFields) (inventree.Parameter, error) {
	f.updatePartParameterCount++
	f.lastUpdatePartParameterFields = fields
	return inventree.Parameter{PK: id, Data: fmt.Sprint(fields["data"])}, nil
}

func (f *fakeMilestoneLookupClient) SearchSupplierParts(_ context.Context, query inventree.SupplierPartQuery) ([]inventree.SupplierPart, error) {
	f.lastSearchSupplierPartsQuery = query
	return f.supplierParts, nil
}

func (f *fakeMilestoneLookupClient) CreateSupplierPart(_ context.Context, input inventree.SupplierPartCreate) (inventree.SupplierPart, error) {
	f.createdSupplierPart = true
	f.lastCreateSupplierPart = input
	return inventree.SupplierPart{PK: 40, Part: input.Part, Supplier: input.Supplier, SKU: input.SKU}, nil
}

func (f *fakeMilestoneLookupClient) SearchManufacturerParts(_ context.Context, query inventree.ManufacturerPartQuery) ([]inventree.ManufacturerPart, error) {
	f.lastSearchManufacturerPartsQuery = query
	return f.manufacturerParts, nil
}

func (f *fakeMilestoneLookupClient) CreateManufacturerPart(_ context.Context, input inventree.ManufacturerPartCreate) (inventree.ManufacturerPart, error) {
	f.createdManufacturerPart = true
	f.lastCreateManufacturerPart = input
	mpn := ""
	if input.MPN != nil {
		mpn = *input.MPN
	}
	return inventree.ManufacturerPart{PK: 50, Part: input.Part, Manufacturer: input.Manufacturer, MPN: mpn}, nil
}
