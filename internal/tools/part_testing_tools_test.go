package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePartTestTemplateClient struct {
	parts         map[int]inventree.PartDetail
	templates     map[int]inventree.PartTestTemplate
	searchResults []inventree.PartTestTemplate
	searchNext    *string
	searchErr     error
	lastQuery     inventree.PartTestTemplateQuery
}

func newFakePartTestTemplateClient() *fakePartTestTemplateClient {
	return &fakePartTestTemplateClient{parts: map[int]inventree.PartDetail{}, templates: map[int]inventree.PartTestTemplate{}}
}

func (f *fakePartTestTemplateClient) SearchPartTestTemplatesPage(_ context.Context, query inventree.PartTestTemplateQuery) (inventree.Page[inventree.PartTestTemplate], error) {
	f.lastQuery = query
	if f.searchErr != nil {
		return inventree.Page[inventree.PartTestTemplate]{}, f.searchErr
	}
	return inventree.Page[inventree.PartTestTemplate]{Count: len(f.searchResults), Results: f.searchResults, Next: f.searchNext}, nil
}

func (f *fakePartTestTemplateClient) GetPartTestTemplate(_ context.Context, id int) (inventree.PartTestTemplate, error) {
	value, ok := f.templates[id]
	if !ok {
		return inventree.PartTestTemplate{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakePartTestTemplateClient) GetPartDetail(_ context.Context, id int) (inventree.PartDetail, error) {
	value, ok := f.parts[id]
	if !ok {
		return inventree.PartDetail{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func partTestTemplateDeps(fake *fakePartTestTemplateClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func TestSearchPartTestTemplatesRequiresPositivePartID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartTestTemplateClient()

	_, out, err := searchPartTestTemplates(partTestTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchPartTestTemplatesInput{})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
	require.NotNil(t, out.Validation)
}

func TestSearchPartTestTemplatesRejectsNegativeOffset(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartTestTemplateClient()
	fake.parts[1] = inventree.PartDetail{PK: 1, Testable: true}

	_, out, err := searchPartTestTemplates(partTestTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchPartTestTemplatesInput{PartID: 1, Offset: -1})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestSearchPartTestTemplatesReturnsNotFoundForMissingPart(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartTestTemplateClient()

	_, out, err := searchPartTestTemplates(partTestTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchPartTestTemplatesInput{PartID: 99})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestSearchPartTestTemplatesRejectsNonTestablePartWithActionableMessage(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartTestTemplateClient()
	fake.parts[1] = inventree.PartDetail{PK: 1, Testable: false}

	_, out, err := searchPartTestTemplates(partTestTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchPartTestTemplatesInput{PartID: 1})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
	require.NotNil(t, out.Validation)
	require.Len(t, out.Validation.Fields, 1)
	assert.Contains(t, out.Validation.Fields[0].Messages[0], "testable:true")
}

func TestSearchPartTestTemplatesReturnsResultsAndForwardsFilters(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartTestTemplateClient()
	fake.parts[1] = inventree.PartDetail{PK: 1, Testable: true}
	next := "http://example.test/next"
	fake.searchResults = []inventree.PartTestTemplate{{PK: 5, Part: 1, TestName: "Voltage check", Enabled: true}}
	fake.searchNext = &next
	enabled := true
	required := true
	requiresValue := true
	requiresAttachment := true
	hasResults := false

	_, out, err := searchPartTestTemplates(partTestTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchPartTestTemplatesInput{
		PartID: 1, Enabled: &enabled, Required: &required, RequiresValue: &requiresValue,
		RequiresAttachment: &requiresAttachment, HasResults: &hasResults, Search: "voltage",
	})
	require.NoError(t, err)
	require.Equal(t, StatusOK, out.Status)
	require.True(t, out.HasMore)
	require.Len(t, out.Results, 1)
	assert.Equal(t, 5, out.Results[0].PK)
	assert.Equal(t, 1, fake.lastQuery.Part)
	require.NotNil(t, fake.lastQuery.Enabled)
	assert.True(t, *fake.lastQuery.Enabled)
	require.NotNil(t, fake.lastQuery.Required)
	assert.True(t, *fake.lastQuery.Required)
	require.NotNil(t, fake.lastQuery.RequiresValue)
	assert.True(t, *fake.lastQuery.RequiresValue)
	require.NotNil(t, fake.lastQuery.RequiresAttachment)
	assert.True(t, *fake.lastQuery.RequiresAttachment)
	require.NotNil(t, fake.lastQuery.HasResults)
	assert.False(t, *fake.lastQuery.HasResults)
	assert.Equal(t, "voltage", fake.lastQuery.Search)
}

func TestGetPartTestTemplateReturnsNotFoundForMissingRecord(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartTestTemplateClient()

	_, out, err := getPartTestTemplate(partTestTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 99})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)

	fake.templates[5] = inventree.PartTestTemplate{PK: 5, Part: 1, TestName: "Voltage check"}
	_, found, err := getPartTestTemplate(partTestTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 5})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, found.Status)
	assert.Equal(t, 5, found.Record.PK)
}

type fakeStockItemTestResultClient struct {
	results         map[int]inventree.StockItemTestResult
	searchResults   []inventree.StockItemTestResult
	searchNext      *string
	downloads       map[int]inventree.DownloadedStockItemTestResultAttachment
	downloadErr     map[int]error
	lastQuery       inventree.StockItemTestResultQuery
	lastDownloadID  int
	lastDownloadMax int64
}

func newFakeStockItemTestResultClient() *fakeStockItemTestResultClient {
	return &fakeStockItemTestResultClient{
		results:     map[int]inventree.StockItemTestResult{},
		downloads:   map[int]inventree.DownloadedStockItemTestResultAttachment{},
		downloadErr: map[int]error{},
	}
}

func (f *fakeStockItemTestResultClient) SearchStockItemTestResultsPage(_ context.Context, query inventree.StockItemTestResultQuery) (inventree.Page[inventree.StockItemTestResult], error) {
	f.lastQuery = query
	return inventree.Page[inventree.StockItemTestResult]{Count: len(f.searchResults), Results: f.searchResults, Next: f.searchNext}, nil
}

func (f *fakeStockItemTestResultClient) GetStockItemTestResult(_ context.Context, id int) (inventree.StockItemTestResult, error) {
	value, ok := f.results[id]
	if !ok {
		return inventree.StockItemTestResult{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *fakeStockItemTestResultClient) DownloadStockItemTestResultAttachment(_ context.Context, id int, maxBytes int64) (inventree.DownloadedStockItemTestResultAttachment, error) {
	f.lastDownloadID = id
	f.lastDownloadMax = maxBytes
	if err, ok := f.downloadErr[id]; ok {
		return inventree.DownloadedStockItemTestResultAttachment{}, err
	}
	value, ok := f.downloads[id]
	if !ok {
		return inventree.DownloadedStockItemTestResultAttachment{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func stockItemTestResultDeps(fake *fakeStockItemTestResultClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func TestSearchStockItemTestResultsRequiresPositiveStockItemID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockItemTestResultClient()

	_, out, err := searchStockItemTestResults(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchStockItemTestResultsInput{})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestSearchStockItemTestResultsRejectsNegativeOffset(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockItemTestResultClient()

	_, out, err := searchStockItemTestResults(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchStockItemTestResultsInput{StockItemID: 1, Offset: -1})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestSearchStockItemTestResultsRejectsNegativeTemplateID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockItemTestResultClient()

	_, out, err := searchStockItemTestResults(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchStockItemTestResultsInput{StockItemID: 1, TemplateID: -1})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestSearchStockItemTestResultsReturnsNotFoundForEmptyResults(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockItemTestResultClient()

	_, out, err := searchStockItemTestResults(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchStockItemTestResultsInput{StockItemID: 1})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestSearchStockItemTestResultsForwardsFiltersAndProjectsSafeFields(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockItemTestResultClient()
	next := "http://example.test/next"
	template := 7
	user := 3
	attachment := "/media/stock_files/1/probe.txt?token=secret"
	fake.searchResults = []inventree.StockItemTestResult{{
		PK: 1, StockItem: 1, Template: &template, Result: true, Value: "12.3", Notes: "n",
		TestStation: "bench-1", User: &user, Date: "2026-01-01", Attachment: &attachment,
	}}
	fake.searchNext = &next
	passing := true

	_, out, err := searchStockItemTestResults(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchStockItemTestResultsInput{StockItemID: 1, TemplateID: 7, Result: &passing, IncludeInstalled: true})
	require.NoError(t, err)
	require.Equal(t, StatusOK, out.Status)
	require.True(t, out.HasMore)
	require.Len(t, out.Results, 1)
	view := out.Results[0]
	assert.Equal(t, 1, view.PK)
	assert.True(t, view.HasAttachment)
	assert.NotContains(t, view.AttachmentURL, "token=secret")
	assert.Equal(t, &user, view.User)
	assert.Equal(t, &template, view.Template)

	assert.Equal(t, 1, fake.lastQuery.StockItem)
	require.NotNil(t, fake.lastQuery.Template)
	assert.Equal(t, 7, *fake.lastQuery.Template)
	require.NotNil(t, fake.lastQuery.Result)
	assert.True(t, *fake.lastQuery.Result)
	assert.True(t, fake.lastQuery.IncludeInstalled)

	wire, marshalErr := json.Marshal(out)
	require.NoError(t, marshalErr)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(wire, &decoded))
	results, ok := decoded["results"].([]any)
	require.True(t, ok)
	require.Len(t, results, 1)
	record, ok := results[0].(map[string]any)
	require.True(t, ok)
	for _, forbidden := range []string{"user_detail", "template_detail", "email", "attachment"} {
		_, present := record[forbidden]
		assert.False(t, present, "wire output must never include %q", forbidden)
	}
}

func TestGetStockItemTestResultReturnsNotFoundForMissingRecord(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockItemTestResultClient()

	_, out, err := getStockItemTestResult(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 99})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)

	fake.results[5] = inventree.StockItemTestResult{PK: 5, StockItem: 1, Result: true, Date: "2026-01-01"}
	_, found, err := getStockItemTestResult(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 5})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, found.Status)
	assert.Equal(t, 5, found.Record.PK)
}

func TestDownloadStockItemTestResultAttachmentReturnsNoAttachmentStatus(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockItemTestResultClient()
	fake.downloadErr[5] = inventree.ErrStockItemTestResultAttachmentMissing

	_, out, err := downloadStockItemTestResultAttachment(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, StockItemTestResultAttachmentDownloadInput{ID: 5})
	require.NoError(t, err)
	assert.Equal(t, StatusNoAttachment, out.Status)
	assert.Equal(t, 5, out.ID)
}

func TestDownloadStockItemTestResultAttachmentReturnsNotFound(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockItemTestResultClient()

	_, out, err := downloadStockItemTestResultAttachment(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, StockItemTestResultAttachmentDownloadInput{ID: 99})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestDownloadStockItemTestResultAttachmentReturnsContentAndAppliesBounds(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockItemTestResultClient()
	fake.downloads[5] = inventree.DownloadedStockItemTestResultAttachment{
		Result:      inventree.StockItemTestResult{PK: 5},
		Content:     []byte("hello"),
		Filename:    "probe.txt",
		ContentType: "text/plain",
		SourceURL:   "https://inventree.example/media/stock_files/1/probe.txt",
	}

	_, out, err := downloadStockItemTestResultAttachment(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, StockItemTestResultAttachmentDownloadInput{ID: 5})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status)
	assert.Equal(t, "hello", out.Text)
	assert.Equal(t, "probe.txt", out.Filename)
	assert.Equal(t, defaultDownloadMaxBytes, fake.lastDownloadMax)

	_, _, err = downloadStockItemTestResultAttachment(stockItemTestResultDeps(fake))(ctx, &mcp.CallToolRequest{}, StockItemTestResultAttachmentDownloadInput{ID: 5, MaxBytes: maxDownloadMaxBytes + 1})
	require.NoError(t, err)
	assert.Equal(t, maxDownloadMaxBytes, fake.lastDownloadMax)
}
