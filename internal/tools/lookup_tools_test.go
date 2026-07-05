package tools

import (
	"context"
	"net/http"
	"net/url"
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
	a.Equal(url.Values{"limit": []string{"20"}, "search": []string{"10k"}}, fake.lastSearchPartsQuery)
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
	a.Equal(url.Values{"limit": []string{"100"}, "location": []string{"40"}, "part": []string{"10"}}, fake.lastSearchStockItemsQuery)
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
	parts                []inventree.Part
	companies            []inventree.Company
	stockItems           []inventree.StockItem
	attachments          []inventree.Attachment
	attachment           inventree.Attachment
	downloadedAttachment inventree.DownloadedAttachment
	downloadedPartImage  inventree.DownloadedPartImage
	getPartErr           error

	lastSearchPartsQuery      url.Values
	lastSearchCompaniesQuery  url.Values
	lastSearchStockItemsQuery url.Values
	lastAttachmentMaxBytes    int64
	lastPartImageMaxBytes     int64
}

func (f *fakeMilestoneLookupClient) SearchParts(_ context.Context, query url.Values) ([]inventree.Part, error) {
	f.lastSearchPartsQuery = query
	return f.parts, nil
}

func (f *fakeMilestoneLookupClient) GetPart(_ context.Context, id int) (inventree.Part, error) {
	if f.getPartErr != nil {
		return inventree.Part{}, f.getPartErr
	}
	return inventree.Part{PK: id, Name: "part"}, nil
}

func (f *fakeMilestoneLookupClient) SearchPartCategories(context.Context, url.Values) ([]inventree.Category, error) {
	return nil, nil
}

func (f *fakeMilestoneLookupClient) SearchPartParameters(context.Context, url.Values) ([]inventree.Parameter, error) {
	return nil, nil
}

func (f *fakeMilestoneLookupClient) SearchParameterTemplates(context.Context, url.Values) ([]inventree.ParameterTemplate, error) {
	return nil, nil
}

func (f *fakeMilestoneLookupClient) SearchCompanies(_ context.Context, query url.Values) ([]inventree.Company, error) {
	f.lastSearchCompaniesQuery = query
	return f.companies, nil
}

func (f *fakeMilestoneLookupClient) SearchSuppliers(context.Context, url.Values) ([]inventree.Company, error) {
	return nil, nil
}

func (f *fakeMilestoneLookupClient) SearchManufacturers(context.Context, url.Values) ([]inventree.Company, error) {
	return nil, nil
}

func (f *fakeMilestoneLookupClient) SearchStockLocations(context.Context, url.Values) ([]inventree.StockLocation, error) {
	return nil, nil
}

func (f *fakeMilestoneLookupClient) SearchStockItems(_ context.Context, query url.Values) ([]inventree.StockItem, error) {
	f.lastSearchStockItemsQuery = query
	return f.stockItems, nil
}

func (f *fakeMilestoneLookupClient) ListAttachments(context.Context, url.Values) ([]inventree.Attachment, error) {
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
