package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeObjectParameterClient struct {
	templates       map[int]inventree.ParameterTemplate
	parameters      map[int]inventree.Parameter
	nextParameterID int
	createCalls     int
	updateCalls     int
	deleteCalls     int
	lastPatch       inventree.PatchFields
	forceCount      int  // when nonzero, overrides the reported Count on every SearchObjectParametersPage/SearchTemplateParametersPage response.
	forceHasMore    bool // when true, SearchObjectParametersPage always reports HasMore:true with a full synthetic page, simulating a scan that never terminates within the bound.
}

func newFakeObjectParameterClient() *fakeObjectParameterClient {
	return &fakeObjectParameterClient{templates: map[int]inventree.ParameterTemplate{}, parameters: map[int]inventree.Parameter{}, nextParameterID: 100}
}

func objectParameterDeps(fake *fakeObjectParameterClient) Dependencies {
	return Dependencies{
		ClientFromContext:              func(context.Context) (any, error) { return fake, nil },
		objectParameterDeletePlanStore: newObjectParameterDeletePlanStore(time.Now, randomStockPlanToken),
	}
}

func (f *fakeObjectParameterClient) SearchObjectParametersPage(_ context.Context, query inventree.ObjectParameterQuery) (inventree.PartParameterPage, error) {
	if f.forceHasMore {
		results := make([]inventree.Parameter, 0, query.Limit)
		for i := 0; i < query.Limit; i++ {
			results = append(results, inventree.Parameter{PK: query.Offset + i + 1, Template: query.TemplateID, ModelType: query.ModelType, ModelID: 1, Data: "scan-limit-filler"})
		}
		return inventree.PartParameterPage{Count: maxObjectParameterScan + 1, Results: results, HasMore: true}, nil
	}
	var rows []inventree.Parameter
	for _, row := range f.parameters {
		if row.ModelType != query.ModelType {
			continue
		}
		if query.ModelID != 0 && row.ModelID != query.ModelID {
			continue
		}
		if query.TemplateID != 0 && row.Template != query.TemplateID {
			continue
		}
		if query.Search != "" && !strings.Contains(row.Data, query.Search) {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].PK < rows[j].PK })
	start := min(query.Offset, len(rows))
	end := min(start+query.Limit, len(rows))
	if query.Limit == 0 {
		end = len(rows)
	}
	count := len(rows)
	if f.forceCount != 0 {
		count = f.forceCount
	}
	return inventree.PartParameterPage{Count: count, Results: rows[start:end], HasMore: end < len(rows)}, nil
}

func (f *fakeObjectParameterClient) SearchTemplateParametersPage(_ context.Context, query inventree.TemplateParameterQuery) (inventree.PartParameterPage, error) {
	var rows []inventree.Parameter
	for _, row := range f.parameters {
		if row.Template == query.TemplateID {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].PK < rows[j].PK })
	start := min(query.Offset, len(rows))
	end := min(start+query.Limit, len(rows))
	if query.Limit == 0 {
		end = len(rows)
	}
	count := len(rows)
	if f.forceCount != 0 {
		count = f.forceCount
	}
	return inventree.PartParameterPage{Count: count, Results: rows[start:end], HasMore: end < len(rows)}, nil
}

func (f *fakeObjectParameterClient) SearchParameterTemplates(_ context.Context, query inventree.SearchQuery) ([]inventree.ParameterTemplate, error) {
	var out []inventree.ParameterTemplate
	for _, record := range f.templates {
		if query.Search == "" || strings.Contains(strings.ToLower(record.Name), strings.ToLower(query.Search)) {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PK < out[j].PK })
	return out, nil
}

func (f *fakeObjectParameterClient) GetParameterTemplate(_ context.Context, id int) (inventree.ParameterTemplate, error) {
	record, ok := f.templates[id]
	if !ok {
		return inventree.ParameterTemplate{}, notFoundError()
	}
	return record, nil
}

func (f *fakeObjectParameterClient) GetPartParameter(_ context.Context, id int) (inventree.Parameter, error) {
	record, ok := f.parameters[id]
	if !ok {
		return inventree.Parameter{}, notFoundError()
	}
	return record, nil
}

func (f *fakeObjectParameterClient) CreatePartParameter(_ context.Context, input inventree.ParameterCreate) (inventree.Parameter, error) {
	f.createCalls++
	id := f.nextParameterID
	f.nextParameterID++
	record := inventree.Parameter{PK: id, Template: input.Template, ModelType: input.ModelType, ModelID: input.ModelID, Data: input.Data}
	f.parameters[id] = record
	return record, nil
}

func (f *fakeObjectParameterClient) UpdatePartParameter(_ context.Context, id int, fields inventree.PatchFields) (inventree.Parameter, error) {
	f.updateCalls++
	f.lastPatch = fields
	record, ok := f.parameters[id]
	if !ok {
		return inventree.Parameter{}, notFoundError()
	}
	if patch, ok := fields["data"]; ok {
		record.Data = patch.Value().(string)
	}
	f.parameters[id] = record
	return record, nil
}

func (f *fakeObjectParameterClient) DeletePartParameter(_ context.Context, id int) error {
	f.deleteCalls++
	delete(f.parameters, id)
	return nil
}

func TestSearchObjectParametersRequiresSupportedModelType(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()

	_, out, err := searchObjectParameterValues(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchObjectParametersInput{ModelType: "part.part"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestSearchObjectParametersFiltersByModelTypeAndObject(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "5V"}
	fake.parameters[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "company.company", ModelID: 6, Data: "9V"}

	_, out, err := searchObjectParameterValues(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchObjectParametersInput{ModelType: "stock.stocklocation"})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	r.Len(out.Results, 1)
	a.Equal(80, out.Results[0].ParameterID)
	a.Equal("stock.stocklocation", out.Results[0].ModelType)
	a.Equal(5, out.Results[0].ModelID)
	a.Equal("5V", out.Results[0].Value)
}

func TestCreateObjectParameterCreatesNewValue(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage", Enabled: true}

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 70, Value: dvgoutils.Ptr("5V")})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal("5V", out.Record.Value)
	a.Equal(1, fake.createCalls)
	a.Zero(fake.updateCalls)
}

func TestCreateObjectParameterUpdatesExistingValue(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "5V"}

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 70, Value: dvgoutils.Ptr("9V")})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(80, out.Record.ParameterID)
	a.Equal("9V", out.Record.Value)
	a.Zero(fake.createCalls)
	a.Equal(1, fake.updateCalls)
}

func TestCreateObjectParameterRejectsIncompatibleTemplate(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "PartOnly", Enabled: true, ModelType: dvgoutils.Ptr("part.part")}

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 70, Value: dvgoutils.Ptr("5V")})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestCreateObjectParameterRefusesModelTypeUniquenessConflict(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "SerialTag", Enabled: true, Unique: inventree.ParameterUniquenessModelType}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "DUPLICATE"}

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 6, TemplateID: 70, Value: dvgoutils.Ptr("DUPLICATE")})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.createCalls)
}

func TestCreateObjectParameterAllowsSameValueAcrossModelTypesWhenScopedToModelType(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "SerialTag", Enabled: true, Unique: inventree.ParameterUniquenessModelType}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "company.company", ModelID: 5, Data: "SAME"}

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 6, TemplateID: 70, Value: dvgoutils.Ptr("SAME")})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
}

func TestCreateObjectParameterRefusesGlobalUniquenessConflictAcrossModelTypes(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "GlobalTag", Enabled: true, Unique: inventree.ParameterUniquenessGlobal}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "company.company", ModelID: 5, Data: "SAME"}

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 6, TemplateID: 70, Value: dvgoutils.Ptr("SAME")})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.createCalls)
}

func TestDeleteObjectParameterGuardedFlow(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "5V"}
	deps := objectParameterDeps(fake)

	_, preview, err := deleteObjectParameter(deps)(ctx, &mcp.CallToolRequest{}, DeleteObjectParameterInput{ParameterID: 80})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, preview.Status)
	r.NotEmpty(preview.PlanHash)
	a.Zero(fake.deleteCalls)

	_, out, err := deleteObjectParameter(deps)(ctx, &mcp.CallToolRequest{}, DeleteObjectParameterInput{ParameterID: 80, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(out.Verified)
	a.Equal(1, fake.deleteCalls)
}

func TestDeleteObjectParameterRejectsStaleToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "5V"}
	deps := objectParameterDeps(fake)

	_, preview, err := deleteObjectParameter(deps)(ctx, &mcp.CallToolRequest{}, DeleteObjectParameterInput{ParameterID: 80})
	r.NoError(err)
	r.NotEmpty(preview.PlanHash)

	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "9V"}

	_, out, err := deleteObjectParameter(deps)(ctx, &mcp.CallToolRequest{}, DeleteObjectParameterInput{ParameterID: 80, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.deleteCalls)
}

func TestDeleteObjectParameterRefusesPartPartRow(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "part.part", ModelID: 5, Data: "5V"}

	_, out, err := deleteObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteObjectParameterInput{ParameterID: 80})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestSearchObjectParametersResolvesTemplateByExactName(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage"}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "5V"}

	_, out, err := searchObjectParameterValues(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchObjectParametersInput{ModelType: "stock.stocklocation", TemplateName: "Voltage"})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	r.Len(out.Results, 1)
	a.Equal(80, out.Results[0].ParameterID)
}

func TestSearchObjectParametersAmbiguousTemplateNameRequiresClarification(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage"}
	fake.templates[71] = inventree.ParameterTemplate{PK: 71, Name: "Voltage"}

	_, out, err := searchObjectParameterValues(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchObjectParametersInput{ModelType: "stock.stocklocation", TemplateName: "Voltage"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestSearchObjectParametersTemplateIDNameMismatchRequiresClarification(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage"}

	_, out, err := searchObjectParameterValues(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchObjectParametersInput{ModelType: "stock.stocklocation", TemplateID: 70, TemplateName: "Current"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestCreateObjectParameterResolvesTemplateByName(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage", Enabled: true}

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 5, TemplateName: "Voltage", Value: dvgoutils.Ptr("5V")})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(1, fake.createCalls)
}

func TestCreateObjectParameterAmbiguousTemplateNameRequiresClarification(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage", Enabled: true}
	fake.templates[71] = inventree.ParameterTemplate{PK: 71, Name: "Voltage", Enabled: true}

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 5, TemplateName: "Voltage", Value: dvgoutils.Ptr("5V")})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.createCalls)
}

func TestCreateObjectParameterUnknownTemplateNameRequiresClarification(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 5, TemplateName: "Missing", Value: dvgoutils.Ptr("5V")})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestCreateObjectParameterRefusesMultipleExistingRows(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "5V"}
	fake.parameters[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "6V"}

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 70, Value: dvgoutils.Ptr("7V")})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.createCalls)
	assert.Zero(t, fake.updateCalls)
}

func TestSearchObjectParametersFailsClosedWhenScanBoundExceeded(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage"}
	fake.forceHasMore = true

	_, out, err := searchObjectParameterValues(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchObjectParametersInput{ModelType: "stock.stocklocation", TemplateID: 70})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestCreateObjectParameterFailsClosedWhenModelTypeUniquenessScanBoundExceeded(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "SerialTag", Enabled: true, Unique: inventree.ParameterUniquenessModelType}
	fake.forceCount = maxObjectParameterScan + 1

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 70, Value: dvgoutils.Ptr("5V")})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.createCalls)
}

func TestCreateObjectParameterFailsClosedWhenGlobalUniquenessScanBoundExceeded(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "GlobalTag", Enabled: true, Unique: inventree.ParameterUniquenessGlobal}
	fake.forceCount = maxObjectParameterScan + 1

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 70, Value: dvgoutils.Ptr("5V")})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.createCalls)
}

func TestDeleteObjectParameterRejectsMalformedOrUnknownToken(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Voltage", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "5V"}

	_, out, err := deleteObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteObjectParameterInput{ParameterID: 80, Confirm: true, PlanHash: "not-a-real-token"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.deleteCalls)
}

func TestCreateObjectParameterConflictClarificationNeverDisclosesTheConflictingValue(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeObjectParameterClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "SerialTag", Enabled: true, Unique: inventree.ParameterUniquenessModelType}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 5, Data: "super-secret-value"}

	_, out, err := createObjectParameter(objectParameterDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: 6, TemplateID: 70, Value: dvgoutils.Ptr("super-secret-value")})
	r.NoError(err)
	r.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	payload, err := json.Marshal(out.Clarification.Candidates)
	r.NoError(err)
	a.NotContains(string(payload), "super-secret-value")
}
