package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeletePartCategoryInvalidID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newCategoryDeleteFake()
	_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 0})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestDeletePartCategoryNotFound(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newCategoryDeleteFake()
	_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestDeletePartCategoryRefusesDirectParts(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	fake.parts[5] = []int{101}

	_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{101}, out.Blocking.PartIDs)
	a.Empty(out.PlanHash)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartCategoryRefusesChildCategories(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	fake.children[5] = []int{6}

	_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{6}, out.Blocking.ChildCategoryIDs)
	a.Empty(out.PlanHash)
}

func TestDeletePartCategoryRefusesParameterTemplateLinks(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	fake.templateLinks[5] = []int{7}

	_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{7}, out.Blocking.ParameterTemplateLinkIDs)
	a.Empty(out.PlanHash)
}

func TestDeletePartCategoryRefusesGenericParameterValues(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	fake.parameterValues[5] = []int{9}

	_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{9}, out.Blocking.ParameterValueIDs)
	a.Empty(out.PlanHash)
}

func TestDeletePartCategoryRefusesMultipleBlockingSurfacesSimultaneously(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	fake.parts[5] = []int{101}
	fake.children[5] = []int{6}
	fake.templateLinks[5] = []int{7}
	fake.parameterValues[5] = []int{9}

	_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Equal([]int{101}, out.Blocking.PartIDs)
	a.Equal([]int{6}, out.Blocking.ChildCategoryIDs)
	a.Equal([]int{7}, out.Blocking.ParameterTemplateLinkIDs)
	a.Equal([]int{9}, out.Blocking.ParameterValueIDs)
}

func TestDeletePartCategoryDirectPartsLaterPage(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	firstPage := make([]inventree.Part, categoryDeleteReferencePageSize)
	for i := range firstPage {
		firstPage[i].PK = 1000 + i
	}
	fake.partPages[0] = inventree.PartPage{Count: categoryDeleteReferencePageSize + 1, Results: firstPage, HasMore: true}
	fake.partPages[categoryDeleteReferencePageSize] = inventree.PartPage{Count: categoryDeleteReferencePageSize + 1, Results: []inventree.Part{{PK: 202}}}

	_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Blocking)
	a.Contains(out.Blocking.PartIDs, 202)
}

func TestDeletePartCategoryLaterPagesAndExactCategoryFiltering(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	tests := []struct {
		name  string
		setup func(*categoryDeleteFake)
		check func(*testing.T, PartCategoryDeleteBlockingReferences)
	}{
		{
			name: "child categories",
			setup: func(fake *categoryDeleteFake) {
				firstPage := make([]inventree.Category, categoryDeleteReferencePageSize)
				for i := range firstPage {
					firstPage[i].PK = 1000 + i
				}
				fake.childPages[0] = inventree.CategoryPage{Count: categoryDeleteReferencePageSize + 1, Results: firstPage, HasMore: true}
				fake.childPages[categoryDeleteReferencePageSize] = inventree.CategoryPage{Count: categoryDeleteReferencePageSize + 1, Results: []inventree.Category{{PK: 206}}}
			},
			check: func(t *testing.T, blocking PartCategoryDeleteBlockingReferences) {
				assert.Contains(t, blocking.ChildCategoryIDs, 206)
			},
		},
		{
			name: "category parameter links",
			setup: func(fake *categoryDeleteFake) {
				firstPage := make([]inventree.CategoryParameterTemplate, categoryDeleteReferencePageSize)
				for i := range firstPage {
					firstPage[i].PK = 1000 + i
					firstPage[i].Category = 99
				}
				fake.templateLinkPages[0] = inventree.Page[inventree.CategoryParameterTemplate]{Count: categoryDeleteReferencePageSize + 2, Results: firstPage, Next: strPtr("next")}
				fake.templateLinkPages[categoryDeleteReferencePageSize] = inventree.Page[inventree.CategoryParameterTemplate]{Count: categoryDeleteReferencePageSize + 2, Results: []inventree.CategoryParameterTemplate{{PK: 207, Category: 99}, {PK: 208, Category: 5}}}
			},
			check: func(t *testing.T, blocking PartCategoryDeleteBlockingReferences) {
				assert.Equal(t, []int{208}, blocking.ParameterTemplateLinkIDs)
			},
		},
		{
			name: "generic parameter values",
			setup: func(fake *categoryDeleteFake) {
				firstPage := make([]inventree.Parameter, categoryDeleteReferencePageSize)
				for i := range firstPage {
					firstPage[i].PK = 1000 + i
				}
				fake.parameterPages[0] = inventree.PartParameterPage{Count: categoryDeleteReferencePageSize + 1, Results: firstPage, HasMore: true}
				fake.parameterPages[categoryDeleteReferencePageSize] = inventree.PartParameterPage{Count: categoryDeleteReferencePageSize + 1, Results: []inventree.Parameter{{PK: 209}}}
			},
			check: func(t *testing.T, blocking PartCategoryDeleteBlockingReferences) {
				assert.Contains(t, blocking.ParameterValueIDs, 209)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newCategoryDeleteFake()
			fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
			tt.setup(fake)
			_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
			require.NoError(t, err)
			require.NotNil(t, out.Blocking)
			tt.check(t, *out.Blocking)
		})
	}
}

func TestDeletePartCategoryRejectsInconsistentFinalPages(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	tests := []struct {
		name  string
		setup func(*categoryDeleteFake)
	}{
		{name: "parts", setup: func(fake *categoryDeleteFake) { fake.partPages[0] = inventree.PartPage{Count: 1} }},
		{name: "children", setup: func(fake *categoryDeleteFake) { fake.childPages[0] = inventree.CategoryPage{Count: 1} }},
		{name: "template links", setup: func(fake *categoryDeleteFake) {
			fake.templateLinkPages[0] = inventree.Page[inventree.CategoryParameterTemplate]{Count: 1}
		}},
		{name: "parameter values", setup: func(fake *categoryDeleteFake) { fake.parameterPages[0] = inventree.PartParameterPage{Count: 1} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newCategoryDeleteFake()
			fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
			tt.setup(fake)
			_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
			require.NoError(t, err)
			assert.Equal(t, StatusClarificationRequired, out.Status)
			assert.Nil(t, out.Blocking)
			assert.Contains(t, out.Clarification.Reason, "safety limit")
			assert.Zero(t, fake.deleteCalls)
		})
	}
}

func TestDeletePartCategoryReferenceScanFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	for offset := 0; offset < categoryDeleteReferenceScanLimit; offset += categoryDeleteReferencePageSize {
		fake.partPages[offset] = inventree.PartPage{HasMore: true}
	}
	_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Contains(t, out.Clarification.Reason, "safety limit")
}

func TestDeletePartCategoryPreviewExecutesAndVerifiesAbsence(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}

	_, preview, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, preview.Status)
	r.NotEmpty(preview.PlanHash)
	r.NotNil(preview.Blocking)
	a.True(preview.Blocking.empty())
	a.Zero(fake.deleteCalls)

	_, executed, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.True(executed.Verified)
	a.Equal(1, fake.deleteCalls)
	_, stillPresent := fake.categories[5]
	a.False(stillPresent)
}

func TestDeletePartCategoryRejectsStaleOrMissingPlan(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}

	_, out, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5, Confirm: true, PlanHash: "not-a-real-token"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.deleteCalls)
}

func TestDeletePartCategoryRejectsPlanStaleAfterCategoryChange(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}

	_, preview, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	r.NotEmpty(preview.PlanHash)

	// The category itself is renamed between preview and confirm; the bound
	// record changes so the token must be rejected as stale rather than
	// silently deleting against unreviewed state.
	fake.categories[5] = inventree.Category{PK: 5, Name: "Renamed"}
	_, executed, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	assert.Equal(t, StatusClarificationRequired, executed.Status)
	assert.Zero(t, fake.deleteCalls)
}

func TestDeletePartCategoryRefusesInsteadOfDeletingWhenReferenceReappears(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}

	_, preview, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	r.NotEmpty(preview.PlanHash)

	// A new part is filed under the category between preview and confirm.
	// Every blocker is always rechecked live, so this refuses with the fresh
	// blocking snapshot rather than ever reaching the plan-token check.
	fake.parts[5] = []int{303}
	_, executed, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, executed.Status)
	r.NotNil(executed.Blocking)
	a.Equal([]int{303}, executed.Blocking.PartIDs)
	a.Zero(fake.deleteCalls)
}

func TestDeletePartCategoryRecoversAmbiguousMutationByReadBack(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	fake.deleteErr = errors.New("connection reset after delete")
	fake.deleteErrDeletesAnyway = true

	_, preview, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	_, executed, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.True(executed.Verified)
	a.True(executed.Recovered)
}

func TestDeletePartCategoryStopsOnDefiniteMutationRejection(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	fake.deleteErr = &inventree.APIError{StatusCode: 409, Kind: inventree.ErrorKindConflict}

	_, preview, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	_, _, err = deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deletion")
}

func TestDeletePartCategoryReportsSurvivalAfterDelete(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	fake.deleteNoOp = true

	_, preview, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	_, executed, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, executed.Status)
	a.Contains(executed.RecoveryPlan, "still exists")
}

func TestDeletePartCategoryReportsUnverifiableReadBack(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}

	_, preview, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	fake.getErrAfterDelete = errors.New("network blip")
	_, executed, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, executed.Status)
	a.Contains(executed.RecoveryPlan, "could not prove absence")
}

func TestDeletePartCategoryPreservesContextCanceledOnMutation(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	fake := newCategoryDeleteFake()
	fake.categories[5] = inventree.Category{PK: 5, Name: "Resistors"}
	fake.deleteErr = context.Canceled

	_, preview, err := deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5})
	r.NoError(err)
	_, _, err = deletePartCategory(categoryDeleteDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartCategoryInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	assert.ErrorIs(t, err, context.Canceled)
}

type categoryDeleteFake struct {
	categories             map[int]inventree.Category
	parts                  map[int][]int
	children               map[int][]int
	templateLinks          map[int][]int
	parameterValues        map[int][]int
	partPages              map[int]inventree.PartPage
	childPages             map[int]inventree.CategoryPage
	templateLinkPages      map[int]inventree.Page[inventree.CategoryParameterTemplate]
	parameterPages         map[int]inventree.PartParameterPage
	deleteCalls            int
	deleteErr              error
	deleteErrDeletesAnyway bool
	deleteNoOp             bool
	deleted                bool
	getErrAfterDelete      error
	planStore              *categoryDeletePlanStore
}

func newCategoryDeleteFake() *categoryDeleteFake {
	return &categoryDeleteFake{
		categories: map[int]inventree.Category{}, parts: map[int][]int{}, children: map[int][]int{},
		templateLinks: map[int][]int{}, parameterValues: map[int][]int{},
		partPages: map[int]inventree.PartPage{}, childPages: map[int]inventree.CategoryPage{},
		templateLinkPages: map[int]inventree.Page[inventree.CategoryParameterTemplate]{},
		parameterPages:    map[int]inventree.PartParameterPage{},
	}
}

func categoryDeleteDeps(fake *categoryDeleteFake) Dependencies {
	if fake.planStore == nil {
		fake.planStore = newCategoryDeletePlanStore(time.Now, randomStockPlanToken)
	}
	return Dependencies{
		ClientFromContext:       func(context.Context) (any, error) { return fake, nil },
		categoryDeletePlanStore: fake.planStore,
	}
}

func (f *categoryDeleteFake) GetPartCategory(_ context.Context, id int) (inventree.Category, error) {
	if f.deleted && f.getErrAfterDelete != nil {
		return inventree.Category{}, f.getErrAfterDelete
	}
	value, ok := f.categories[id]
	if !ok {
		return inventree.Category{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *categoryDeleteFake) SearchPartsPage(_ context.Context, query inventree.PartQuery) (inventree.PartPage, error) {
	if page, ok := f.partPages[query.Offset]; ok {
		return page, nil
	}
	ids := f.parts[query.CategoryID]
	results := make([]inventree.Part, 0, len(ids))
	for _, id := range ids {
		results = append(results, inventree.Part{PK: id})
	}
	return inventree.PartPage{Count: len(results), Results: results}, nil
}

func (f *categoryDeleteFake) SearchPartCategoriesPage(_ context.Context, query inventree.CategoryQuery) (inventree.CategoryPage, error) {
	if page, ok := f.childPages[query.Offset]; ok {
		return page, nil
	}
	if query.Parent == nil {
		return inventree.CategoryPage{}, nil
	}
	ids := f.children[*query.Parent]
	results := make([]inventree.Category, 0, len(ids))
	for _, id := range ids {
		results = append(results, inventree.Category{PK: id})
	}
	return inventree.CategoryPage{Count: len(results), Results: results}, nil
}

func (f *categoryDeleteFake) SearchCategoryParameterTemplatesPage(_ context.Context, query inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error) {
	if page, ok := f.templateLinkPages[query.Offset]; ok {
		return page, nil
	}
	results := make([]inventree.CategoryParameterTemplate, 0)
	if query.CategoryID > 0 {
		for _, id := range f.templateLinks[query.CategoryID] {
			results = append(results, inventree.CategoryParameterTemplate{PK: id, Category: query.CategoryID})
		}
	} else {
		for categoryID, ids := range f.templateLinks {
			for _, id := range ids {
				results = append(results, inventree.CategoryParameterTemplate{PK: id, Category: categoryID})
			}
		}
	}
	return inventree.Page[inventree.CategoryParameterTemplate]{Count: len(results), Results: results}, nil
}

func strPtr(value string) *string { return &value }

func (f *categoryDeleteFake) SearchObjectParametersPage(_ context.Context, query inventree.ObjectParameterQuery) (inventree.PartParameterPage, error) {
	if page, ok := f.parameterPages[query.Offset]; ok {
		return page, nil
	}
	ids := f.parameterValues[query.ModelID]
	results := make([]inventree.Parameter, 0, len(ids))
	for _, id := range ids {
		results = append(results, inventree.Parameter{PK: id})
	}
	return inventree.PartParameterPage{Count: len(results), Results: results}, nil
}

func (f *categoryDeleteFake) DeletePartCategory(_ context.Context, id int) error {
	f.deleteCalls++
	f.deleted = true
	if f.deleteNoOp {
		return nil
	}
	if f.deleteErr != nil {
		if f.deleteErrDeletesAnyway {
			delete(f.categories, id)
		}
		return f.deleteErr
	}
	delete(f.categories, id)
	return nil
}
