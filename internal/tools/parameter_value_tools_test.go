package tools

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchPartParametersUsesExactFiltersAndStablePagination(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	otherCategoryID := 21
	units := "ohm"
	fake := &fakePartParameterClient{
		parameters: []inventree.Parameter{
			{PK: 62, Template: 70, ModelType: "part.part", ModelID: 12, Data: "10k"},
			{PK: 60, Template: 70, ModelType: "part.part", ModelID: 10, Data: "10k"},
			{PK: 61, Template: 70, ModelType: "part.part", ModelID: 11, Data: "10k"},
		},
		parts: map[int]inventree.Part{
			10: {PK: 10, Name: "resistor", Category: &categoryID},
			11: {PK: 11, Name: "second resistor", Category: &categoryID},
			12: {PK: 12, Name: "other category", Category: &otherCategoryID},
		},
		templates: map[int]inventree.ParameterTemplate{70: {PK: 70, Name: "Resistance", Units: &units}},
	}
	handler := searchPartParameterValues(parameterValueDeps(fake))

	value := "10k"
	result, output, err := handler(ctx, &mcp.CallToolRequest{}, SearchPartParametersInput{TemplateID: 70, Value: &value, CategoryID: 20, Limit: 1, Offset: 1})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	r.Len(output.Results, 1)
	a.Equal(61, output.Results[0].ParameterID)
	a.Equal(11, output.Results[0].PartID)
	a.Equal(dvgoutils.Ptr(20), output.Results[0].CategoryID)
	a.Equal(70, output.Results[0].TemplateID)
	a.Equal("Resistance", output.Results[0].TemplateName)
	a.Equal("10k", output.Results[0].Value)
	a.Equal(inventree.PartParameterQuery{Search: "10k", TemplateID: 70, Limit: 100}, fake.lastQuery)

	partFake := &fakePartParameterClient{parameters: fake.parameters, parts: fake.parts, templates: fake.templates}
	_, partOutput, err := searchPartParameterValues(parameterValueDeps(partFake))(ctx, &mcp.CallToolRequest{}, SearchPartParametersInput{PartID: 10})
	r.NoError(err)
	r.Len(partOutput.Results, 1)
	a.Equal(10, partOutput.Results[0].PartID)
	a.Equal(inventree.PartParameterQuery{PartID: 10, Limit: 100}, partFake.lastQuery)
}

func TestSearchPartParametersRequiresNarrowingFilter(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePartParameterClient{}

	_, output, err := searchPartParameterValues(parameterValueDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchPartParametersInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("filters", output.Clarification.Field)
	a.False(fake.searchedParameters)
}

func TestSearchPartParametersRefusesWhenBoundedScanCannotFillPage(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	parameters := make([]inventree.Parameter, maxPartParameterScan+1)
	for i := range parameters {
		parameters[i] = inventree.Parameter{PK: i + 1, Template: 70, ModelType: "part.part", ModelID: 10, Data: "10k"}
	}
	fake := &fakePartParameterClient{
		parameters: parameters,
		parts:      map[int]inventree.Part{10: {PK: 10, Name: "resistor", Category: &categoryID}},
		templates:  map[int]inventree.ParameterTemplate{70: {PK: 70, Name: "Resistance"}},
	}

	_, output, err := searchPartParameterValues(parameterValueDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchPartParametersInput{CategoryID: 21})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Contains(output.Clarification.Reason, "1,000-row scan bound")
	a.Equal(10, fake.searchCalls)
}

func TestSearchPartParametersSortsAcrossServerPages(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	parameters := make([]inventree.Parameter, partParameterPageSize+1)
	for i := range partParameterPageSize {
		parameters[i] = inventree.Parameter{PK: 1000 + i, Template: 70, ModelType: "part.part", ModelID: 10, Data: "10k"}
	}
	parameters[partParameterPageSize] = inventree.Parameter{PK: 1, Template: 70, ModelType: "part.part", ModelID: 10, Data: "10k"}
	fake := &fakePartParameterClient{
		parameters: parameters,
		parts:      map[int]inventree.Part{10: {PK: 10, Name: "resistor", Category: &categoryID}},
		templates:  map[int]inventree.ParameterTemplate{70: {PK: 70, Name: "Resistance"}},
	}

	_, output, err := searchPartParameterValues(parameterValueDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchPartParametersInput{TemplateID: 70, Limit: 1})
	r.NoError(err)
	r.Len(output.Results, 1)
	a.Equal(1, output.Results[0].ParameterID)
	a.Equal(2, fake.searchCalls)
}

func TestSearchPartParametersRejectsWindowBeyondScanBound(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePartParameterClient{}

	_, output, err := searchPartParameterValues(parameterValueDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchPartParametersInput{PartID: 10, Offset: int(^uint(0) >> 1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("offset", output.Clarification.Field)
	a.Contains(output.Clarification.Reason, "offset plus limit")
	a.False(fake.searchedParameters)
}

func TestSearchPartParametersRequiresStableTemplateForAmbiguousName(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePartParameterClient{templateSearch: []inventree.ParameterTemplate{{PK: 70, Name: "Resistance"}, {PK: 71, Name: "resistance"}}}

	result, output, err := searchPartParameterValues(parameterValueDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchPartParametersInput{TemplateName: "Resistance"})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template_id", output.Clarification.Retry)
	r.Len(output.Clarification.Candidates, 2)
	a.False(fake.searchedParameters)
}

func TestDeletePartParameterConfirmsAndVerifiesReadBack(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakePartParameterClient{
		parameter: inventree.Parameter{PK: 60, Template: 70, ModelType: "part.part", ModelID: 10, Data: "10k"},
		parts:     map[int]inventree.Part{10: {PK: 10, Name: "resistor", Category: &categoryID}},
		templates: map[int]inventree.ParameterTemplate{70: {PK: 70, Name: "Resistance"}},
	}
	handler := deletePartParameter(parameterValueDeps(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, DeletePartParameterInput{ParameterID: 60})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusClarificationRequired, output.Status)
	a.False(fake.deleted)
	a.Equal(60, output.Record.ParameterID)
	r.NotNil(output.Clarification)
	a.Equal("confirm", output.Clarification.Retry)

	result, output, err = handler(ctx, &mcp.CallToolRequest{}, DeletePartParameterInput{ParameterID: 60, Confirm: true})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	a.True(output.Verified)
	a.True(fake.deleted)
	a.Equal(60, fake.deletedID)
	a.GreaterOrEqual(fake.getCalls, 3)
}

func TestDeletePartParameterRefusesNonPartRows(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePartParameterClient{parameter: inventree.Parameter{PK: 60, Template: 70, ModelType: "stock.stockitem", ModelID: 10, Data: "10k"}}

	_, output, err := deletePartParameter(parameterValueDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartParameterInput{ParameterID: 60, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Contains(output.Clarification.Reason, "unsupported model type")
	a.False(fake.deleted)
}

func TestDeletePartParameterFailsClosedOnIdentityMismatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePartParameterClient{parameter: inventree.Parameter{PK: 61, Template: 70, ModelType: "part.part", ModelID: 10, Data: "10k"}}

	_, output, err := deletePartParameter(parameterValueDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartParameterInput{ParameterID: 60, Confirm: true})
	r.ErrorContains(err, "identity mismatch")
	a.NotEqual(StatusOK, output.Status)
	a.False(output.Verified)
	a.False(fake.deleted)
}

func TestDeletePartParameterFailureBranchesDoNotReportVerified(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		deleteErr         error
		retainAfterDelete bool
		verifyErr         error
	}{
		{name: "delete fails", deleteErr: errors.New("delete failed")},
		{name: "row remains", retainAfterDelete: true},
		{name: "verification fails", verifyErr: errors.New("read-back failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			categoryID := 20
			fake := &fakePartParameterClient{
				parameter:         inventree.Parameter{PK: 60, Template: 70, ModelType: "part.part", ModelID: 10, Data: "10k"},
				parts:             map[int]inventree.Part{10: {PK: 10, Name: "resistor", Category: &categoryID}},
				templates:         map[int]inventree.ParameterTemplate{70: {PK: 70, Name: "Resistance"}},
				deleteErr:         tt.deleteErr,
				retainAfterDelete: tt.retainAfterDelete,
				verifyErr:         tt.verifyErr,
			}
			_, output, err := deletePartParameter(parameterValueDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartParameterInput{ParameterID: 60, Confirm: true})
			r.Error(err)
			a.NotEqual(StatusOK, output.Status)
			a.False(output.Verified)
		})
	}
}

type fakePartParameterClient struct {
	parameters         []inventree.Parameter
	parameter          inventree.Parameter
	parts              map[int]inventree.Part
	templates          map[int]inventree.ParameterTemplate
	templateSearch     []inventree.ParameterTemplate
	lastQuery          inventree.PartParameterQuery
	searchedParameters bool
	searchCalls        int
	deleted            bool
	deletedID          int
	getCalls           int
	deleteErr          error
	retainAfterDelete  bool
	verifyErr          error
}

func parameterValueDeps(fake *fakePartParameterClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func (f *fakePartParameterClient) SearchPartParametersPage(_ context.Context, query inventree.PartParameterQuery) (inventree.PartParameterPage, error) {
	f.searchedParameters = true
	f.searchCalls++
	f.lastQuery = query
	filtered := slices.DeleteFunc(append([]inventree.Parameter(nil), f.parameters...), func(parameter inventree.Parameter) bool {
		return query.PartID > 0 && parameter.ModelID != query.PartID || query.TemplateID > 0 && parameter.Template != query.TemplateID || query.Search != "" && parameter.Data != query.Search
	})
	if query.Offset >= len(filtered) {
		return inventree.PartParameterPage{Count: len(filtered)}, nil
	}
	end := min(len(filtered), query.Offset+query.Limit)
	return inventree.PartParameterPage{Count: len(filtered), Results: filtered[query.Offset:end], HasMore: end < len(filtered)}, nil
}

func (f *fakePartParameterClient) SearchParameterTemplates(_ context.Context, _ inventree.SearchQuery) ([]inventree.ParameterTemplate, error) {
	return f.templateSearch, nil
}

func (f *fakePartParameterClient) GetParameterTemplate(_ context.Context, id int) (inventree.ParameterTemplate, error) {
	return f.templates[id], nil
}

func (f *fakePartParameterClient) GetPart(_ context.Context, id int) (inventree.Part, error) {
	return f.parts[id], nil
}

func (f *fakePartParameterClient) GetPartParameter(_ context.Context, _ int) (inventree.Parameter, error) {
	f.getCalls++
	if f.deleted {
		if f.verifyErr != nil {
			return inventree.Parameter{}, f.verifyErr
		}
		if f.retainAfterDelete {
			return f.parameter, nil
		}
		return inventree.Parameter{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return f.parameter, nil
}

func (f *fakePartParameterClient) DeletePartParameter(_ context.Context, id int) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = true
	f.deletedID = id
	return nil
}
