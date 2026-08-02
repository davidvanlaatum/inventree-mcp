package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchCategoryParameterDefaultsIsExactByDefaultAndInheritanceIsExplicit(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	deps := categoryDefaultDeps(fake)

	_, exact, err := searchCategoryParameterDefaults(deps)(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{CategoryID: 21})
	r.NoError(err)
	r.Len(exact.Records, 1)
	a.Equal(91, exact.Records[0].LinkID)
	a.False(exact.Records[0].Inherited)
	a.False(*fake.lastQuery.FetchParent)

	_, inherited, err := searchCategoryParameterDefaults(deps)(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{CategoryID: 21, IncludeParentDefaults: true})
	r.NoError(err)
	r.Len(inherited.Records, 2)
	a.True(*fake.lastQuery.FetchParent)
	a.True(inherited.Records[0].Inherited)
	a.False(inherited.Records[1].Inherited)
	a.Equal(21, inherited.Records[0].RequestedCategoryID)
}

func TestSearchCategoryParameterDefaultsValidatesIDsBeforeListing(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	_, output, err := searchCategoryParameterDefaults(categoryDefaultDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{CategoryID: 999})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("category_id", output.Clarification.Retry)
	a.Zero(fake.pageCalls)
}

func TestSearchCategoryParameterDefaultsValidatesFilterShapeAndPaginatesTemplateFilter(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	deps := categoryDefaultDeps(fake)

	_, invalid, err := searchCategoryParameterDefaults(deps)(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{CategoryID: -1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, invalid.Status)
	_, missingCategory, err := searchCategoryParameterDefaults(deps)(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{IncludeParentDefaults: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, missingCategory.Status)

	_, filtered, err := searchCategoryParameterDefaults(deps)(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{TemplateID: 70, Limit: 1})
	r.NoError(err)
	a.Equal(StatusOK, filtered.Status)
	a.Equal(1, filtered.Count)
	r.Len(filtered.Records, 1)
	a.Equal(90, filtered.Records[0].LinkID)
	a.False(filtered.HasMore)
}

func TestCategoryParameterDefaultWritesValidateRequiredIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	deps := categoryDefaultDeps(fake)

	_, createInvalid, err := createCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, CreateCategoryParameterDefaultInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, createInvalid.Status)
	_, createMissing, err := createCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, CreateCategoryParameterDefaultInput{CategoryID: 999, TemplateID: 70})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, createMissing.Status)
	a.Equal("category_id", createMissing.Clarification.Retry)
	_, createMissingTemplate, err := createCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, CreateCategoryParameterDefaultInput{CategoryID: 21, TemplateID: 999})
	r.NoError(err)
	a.Equal("template_id", createMissingTemplate.Clarification.Retry)
	_, updateInvalid, err := updateCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: 91})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, updateInvalid.Status)
	_, deleteInvalid, err := deleteCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, DeleteCategoryParameterDefaultInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, deleteInvalid.Status)
	_, unicodeDefault, err := createCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, CreateCategoryParameterDefaultInput{CategoryID: 21, TemplateID: 70, DefaultValue: strings.Repeat("é", 500)})
	r.NoError(err)
	a.Equal(StatusOK, unicodeDefault.Status)
}

func TestUpdateCategoryParameterDefaultValidatesReplacementIDsAndPatchesBoth(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	fake.categories[22] = inventree.Category{PK: 22, Name: "Other"}
	fake.templates[72] = inventree.ParameterTemplate{PK: 72, Name: "Voltage"}
	deps := categoryDefaultDeps(fake)

	_, missingLink, err := updateCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: 999, DefaultValue: dvgoutils.Ptr("x")})
	r.NoError(err)
	a.Equal("link_id", missingLink.Clarification.Retry)
	_, missingCategory, err := updateCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: 91, CategoryID: dvgoutils.Ptr(999)})
	r.NoError(err)
	a.Equal("category_id", missingCategory.Clarification.Retry)
	_, missingTemplate, err := updateCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: 91, TemplateID: dvgoutils.Ptr(999)})
	r.NoError(err)
	a.Equal("template_id", missingTemplate.Clarification.Retry)

	_, updated, err := updateCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: 91, CategoryID: dvgoutils.Ptr(22), TemplateID: dvgoutils.Ptr(72)})
	r.NoError(err)
	a.Equal(StatusOK, updated.Status)
	a.Equal(22, updated.Record.CategoryID)
	a.Equal(72, updated.Record.TemplateID)
	a.Contains(fake.lastPatch, "category")
	a.Contains(fake.lastPatch, "template")
}

func TestCategoryParameterDefaultWriteDuplicatePreflightIsBounded(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	createFake := categoryDefaultFake()
	createFake.emptyPageWithNext = true
	_, createOutput, err := createCategoryParameterDefault(categoryDefaultDeps(createFake))(ctx, &mcp.CallToolRequest{}, CreateCategoryParameterDefaultInput{CategoryID: 21, TemplateID: 70})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, createOutput.Status)

	updateFake := categoryDefaultFake()
	updateFake.emptyPageWithNext = true
	_, updateOutput, err := updateCategoryParameterDefault(categoryDefaultDeps(updateFake))(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: 91, DefaultValue: dvgoutils.Ptr("changed")})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, updateOutput.Status)
}

func TestCategoryParameterDefaultListUsesEmbeddedDetails(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	link := fake.links[91]
	link.CategoryDetail = dvgoutils.Ptr(fake.categories[21])
	link.TemplateDetail = dvgoutils.Ptr(fake.templates[71])
	fake.links[91] = link
	_, output, err := searchCategoryParameterDefaults(categoryDefaultDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{CategoryID: 21})
	r.NoError(err)
	r.Len(output.Records, 1)
	a.Equal("Child", output.Records[0].CategoryName)
	a.Equal("Tolerance", output.Records[0].TemplateName)
}

func TestCategoryParameterDefaultStableLinkIdentityMismatchesFailClosed(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	link := fake.links[91]
	link.PK = 999
	fake.links[91] = link
	deps := categoryDefaultDeps(fake)

	_, _, err := updateCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: 91, DefaultValue: dvgoutils.Ptr("changed")})
	r.ErrorContains(err, "identity mismatch")
	_, _, err = deleteCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, DeleteCategoryParameterDefaultInput{LinkID: 91, Confirm: true})
	r.ErrorContains(err, "identity mismatch")
	r.False(fake.deleted[91])
}

func TestCategoryParameterDefaultScanAndExactViewFailClosed(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	fake.emptyPageWithNext = true
	_, bounded, err := searchCategoryParameterDefaults(categoryDefaultDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{TemplateID: 70})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, bounded.Status)
	a.Equal("category_id", bounded.Clarification.Retry)
	a.Equal(1, fake.pageCalls)

	fake = categoryDefaultFake()
	fake.unexpectedExactSource = true
	_, _, err = searchCategoryParameterDefaults(categoryDefaultDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{CategoryID: 21})
	r.ErrorContains(err, "exact category-default response")
}

func TestCreateAndUpdateCategoryParameterDefaultGuardDuplicatesAndPreserveExplicitBlank(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	deps := categoryDefaultDeps(fake)

	_, duplicate, err := createCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, CreateCategoryParameterDefaultInput{CategoryID: 21, TemplateID: 71, DefaultValue: "new"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, duplicate.Status)
	a.Equal(91, duplicate.Record.LinkID)
	a.Equal("link_id", duplicate.Clarification.Retry)
	a.Equal(UpdateCategoryParameterDefaultToolName, duplicate.Clarification.RetryTool)
	a.Equal("new", duplicate.Clarification.RetryValues["default_value"])

	_, created, err := createCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, CreateCategoryParameterDefaultInput{CategoryID: 21, TemplateID: 70, DefaultValue: "child"})
	r.NoError(err)
	a.Equal(StatusOK, created.Status)
	a.Equal(21, created.Record.CategoryID)
	a.Equal(70, created.Record.TemplateID)

	_, updated, err := updateCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: created.Record.LinkID, DefaultValue: dvgoutils.Ptr("")})
	r.NoError(err)
	a.Equal(StatusOK, updated.Status)
	a.Empty(updated.Record.DefaultValue)
	a.Contains(fake.lastPatch, "default_value")
	a.NotContains(fake.lastPatch, "category")
	a.NotContains(fake.lastPatch, "template")
}

func TestUpdateCategoryParameterDefaultRejectsCollisionFromStableLink(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	fake.links[92] = inventree.CategoryParameterTemplate{PK: 92, Category: 21, Template: 70, DefaultValue: "child"}
	_, output, err := updateCategoryParameterDefault(categoryDefaultDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: 91, TemplateID: dvgoutils.Ptr(70)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal(92, output.Record.LinkID)
	a.Equal("link_id", output.Clarification.Retry)
	a.Equal(DeleteCategoryParameterDefaultToolName, output.Clarification.RetryTool)
	a.Equal(map[string]any{"link_id": 92, "confirm": false}, output.Clarification.RetryValues)
	a.Empty(fake.lastPatch)
}

func TestDeleteCategoryParameterDefaultRequiresConfirmationAndVerifiesReadback(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := categoryDefaultFake()
	deps := categoryDefaultDeps(fake)

	_, preview, err := deleteCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, DeleteCategoryParameterDefaultInput{LinkID: 91})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, preview.Status)
	a.False(fake.deleted[91])

	_, deleted, err := deleteCategoryParameterDefault(deps)(ctx, &mcp.CallToolRequest{}, DeleteCategoryParameterDefaultInput{LinkID: 91, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, deleted.Status)
	a.True(deleted.Verified)
	a.True(fake.deleted[91])
}

func TestDeleteCategoryParameterDefaultFailsClosedOnUnverifiedReadback(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	persisted := categoryDefaultFake()
	persisted.persistDelete = true
	_, _, err := deleteCategoryParameterDefault(categoryDefaultDeps(persisted))(ctx, &mcp.CallToolRequest{}, DeleteCategoryParameterDefaultInput{LinkID: 91, Confirm: true})
	r.ErrorContains(err, "still exists")

	failedReadback := categoryDefaultFake()
	failedReadback.deleteReadbackErr = errors.New("readback unavailable")
	_, _, err = deleteCategoryParameterDefault(categoryDefaultDeps(failedReadback))(ctx, &mcp.CallToolRequest{}, DeleteCategoryParameterDefaultInput{LinkID: 91, Confirm: true})
	r.ErrorContains(err, "verify category parameter default")
}

func TestCategoryParameterDefaultResponseAndDetailIdentityMismatchesFailClosed(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	createMismatch := categoryDefaultFake()
	createMismatch.createIdentityMismatch = true
	_, _, err := createCategoryParameterDefault(categoryDefaultDeps(createMismatch))(ctx, &mcp.CallToolRequest{}, CreateCategoryParameterDefaultInput{CategoryID: 21, TemplateID: 70})
	r.ErrorContains(err, "created category parameter default identity mismatch")

	updateMismatch := categoryDefaultFake()
	updateMismatch.updateIdentityMismatch = true
	_, _, err = updateCategoryParameterDefault(categoryDefaultDeps(updateMismatch))(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: 91, DefaultValue: dvgoutils.Ptr("changed")})
	r.ErrorContains(err, "updated category parameter default identity mismatch")

	detailMismatch := categoryDefaultFake()
	link := detailMismatch.links[91]
	link.CategoryDetail = &inventree.Category{PK: 999, Name: "wrong"}
	detailMismatch.links[91] = link
	_, _, err = searchCategoryParameterDefaults(categoryDefaultDeps(detailMismatch))(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{CategoryID: 21})
	r.ErrorContains(err, "category detail identity mismatch")
}

type fakeCategoryParameterDefaultClient struct {
	categories             map[int]inventree.Category
	templates              map[int]inventree.ParameterTemplate
	links                  map[int]inventree.CategoryParameterTemplate
	deleted                map[int]bool
	lastPatch              inventree.PatchFields
	lastQuery              inventree.CategoryParameterTemplateQuery
	pageCalls              int
	nextID                 int
	emptyPageWithNext      bool
	unexpectedExactSource  bool
	persistDelete          bool
	deleteReadbackErr      error
	createIdentityMismatch bool
	updateIdentityMismatch bool
}

func categoryDefaultFake() *fakeCategoryParameterDefaultClient {
	return &fakeCategoryParameterDefaultClient{
		categories: map[int]inventree.Category{20: {PK: 20, Name: "Parent"}, 21: {PK: 21, Name: "Child"}},
		templates:  map[int]inventree.ParameterTemplate{70: {PK: 70, Name: "Resistance"}, 71: {PK: 71, Name: "Tolerance"}},
		links:      map[int]inventree.CategoryParameterTemplate{90: {PK: 90, Category: 20, Template: 70, DefaultValue: "10k"}, 91: {PK: 91, Category: 21, Template: 71, DefaultValue: "5%"}},
		deleted:    map[int]bool{}, nextID: 100,
	}
}

func categoryDefaultDeps(fake *fakeCategoryParameterDefaultClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func (f *fakeCategoryParameterDefaultClient) GetPartCategory(_ context.Context, id int) (inventree.Category, error) {
	record, ok := f.categories[id]
	if !ok {
		return inventree.Category{}, notFoundCategoryDefault()
	}
	return record, nil
}

func (f *fakeCategoryParameterDefaultClient) GetParameterTemplate(_ context.Context, id int) (inventree.ParameterTemplate, error) {
	record, ok := f.templates[id]
	if !ok {
		return inventree.ParameterTemplate{}, notFoundCategoryDefault()
	}
	return record, nil
}

func (f *fakeCategoryParameterDefaultClient) SearchCategoryParameterTemplatesPage(_ context.Context, query inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error) {
	f.lastQuery, f.pageCalls = query, f.pageCalls+1
	if f.emptyPageWithNext {
		next := "next"
		return inventree.Page[inventree.CategoryParameterTemplate]{Next: &next}, nil
	}
	ids := make([]int, 0, len(f.links))
	for id := range f.links {
		if !f.deleted[id] {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	results := make([]inventree.CategoryParameterTemplate, 0, len(ids))
	for _, id := range ids {
		link := f.links[id]
		if query.CategoryID == 0 || link.Category == query.CategoryID || (query.FetchParent != nil && *query.FetchParent && query.CategoryID == 21 && link.Category == 20) {
			results = append(results, link)
		}
	}
	if f.unexpectedExactSource && query.CategoryID > 0 && query.FetchParent != nil && !*query.FetchParent {
		results = []inventree.CategoryParameterTemplate{f.links[90]}
	}
	start := min(query.Offset, len(results))
	limit := query.Limit
	if limit <= 0 {
		limit = len(results)
	}
	end := min(start+limit, len(results))
	page := inventree.Page[inventree.CategoryParameterTemplate]{Count: len(results), Results: results[start:end]}
	if end < len(results) {
		next := "next"
		page.Next = &next
	}
	return page, nil
}

func (f *fakeCategoryParameterDefaultClient) GetCategoryParameterTemplate(_ context.Context, id int) (inventree.CategoryParameterTemplate, error) {
	if f.deleted[id] && f.deleteReadbackErr != nil {
		return inventree.CategoryParameterTemplate{}, f.deleteReadbackErr
	}
	record, ok := f.links[id]
	if !ok || f.deleted[id] {
		return inventree.CategoryParameterTemplate{}, notFoundCategoryDefault()
	}
	return record, nil
}

func (f *fakeCategoryParameterDefaultClient) CreateCategoryParameterTemplate(_ context.Context, input inventree.CategoryParameterTemplateCreate) (inventree.CategoryParameterTemplate, error) {
	record := inventree.CategoryParameterTemplate{PK: f.nextID, Category: input.Category, Template: input.Template, DefaultValue: input.DefaultValue}
	if f.createIdentityMismatch {
		record.Category++
		return record, nil
	}
	f.links[record.PK] = record
	f.nextID++
	return record, nil
}

func (f *fakeCategoryParameterDefaultClient) UpdateCategoryParameterTemplate(_ context.Context, id int, fields inventree.PatchFields) (inventree.CategoryParameterTemplate, error) {
	f.lastPatch = fields
	record, ok := f.links[id]
	if !ok {
		return inventree.CategoryParameterTemplate{}, notFoundCategoryDefault()
	}
	raw, _ := json.Marshal(fields)
	values := map[string]any{}
	_ = json.Unmarshal(raw, &values)
	if value, ok := values["category"]; ok {
		record.Category = int(value.(float64))
	}
	if value, ok := values["template"]; ok {
		record.Template = int(value.(float64))
	}
	if value, ok := values["default_value"]; ok {
		record.DefaultValue = value.(string)
	}
	f.links[id] = record
	if f.updateIdentityMismatch {
		record.PK++
	}
	return record, nil
}

func (f *fakeCategoryParameterDefaultClient) DeleteCategoryParameterTemplate(_ context.Context, id int) error {
	if !f.persistDelete {
		f.deleted[id] = true
	}
	return nil
}

func notFoundCategoryDefault() error {
	return &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
}
