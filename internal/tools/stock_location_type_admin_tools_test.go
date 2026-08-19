package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateStockLocationTypeCreatesAfterDuplicatePreflight(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockLocationTypeAdminFake()
	fake.createResult = inventree.StockLocationType{PK: 5, Name: "Shelf", Description: "d", Icon: "ti:box:outline"}

	_, out, err := createStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateStockLocationTypeInput{Name: "  Shelf ", Description: dvgoutils.Ptr("d"), Icon: dvgoutils.Ptr("ti:box:outline")})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	r.NotNil(out.Record)
	a.Equal(5, out.Record.PK)
	a.Equal("Shelf", fake.lastCreate.Name)
	a.Equal("d", fake.lastCreate.Description)
	a.Equal("ti:box:outline", fake.lastCreate.Icon)
}

func TestCreateStockLocationTypeRequiresNonblankName(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newStockLocationTypeAdminFake()
	_, out, err := createStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateStockLocationTypeInput{Name: "   "})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.createCalls)
}

func TestCreateStockLocationTypeRefusesLaterPageDuplicateAndFailsClosed(t *testing.T) {
	t.Parallel()
	t.Run("later page duplicate", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockLocationTypeAdminFake()
		fake.pages[0] = inventree.StockLocationTypePage{HasMore: true}
		fake.pages[100] = inventree.StockLocationTypePage{Results: []inventree.StockLocationType{{PK: 2, Name: " sHelf "}}}
		_, out, err := createStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateStockLocationTypeInput{Name: "Shelf"})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
		assert.Zero(t, fake.createCalls)
	})
	t.Run("scan bound", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newStockLocationTypeAdminFake()
		for offset := 0; offset < stockLocationTypeScanLimit; offset += stockLocationTypePageSize {
			fake.pages[offset] = inventree.StockLocationTypePage{HasMore: true}
		}
		_, out, err := createStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateStockLocationTypeInput{Name: "Shelf"})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
		assert.Contains(t, out.Clarification.Reason, "safety limit")
	})
}

func TestCreateStockLocationTypeRecoversPostPersistResponseLoss(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newStockLocationTypeAdminFake()
	fake.createResult = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	fake.createErr = errors.New("connection reset after persist")
	_, out, err := createStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreateStockLocationTypeInput{Name: "Shelf"})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, out.Status)
	assert.True(t, out.Recovered)
	assert.Equal(t, 5, out.Record.PK)
}

func TestUpdateStockLocationTypePatchesExplicitEmptyStringFields(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf", Description: "old", Icon: "ti:box:outline"}

	_, out, err := updateStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockLocationTypeInput{ID: 5, Description: dvgoutils.Ptr(""), Icon: dvgoutils.Ptr("")})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(fake.lastPatchIs("description", ""))
	a.True(fake.lastPatchIs("icon", ""))
	a.False(fake.lastPatchHas("name"))
}

func TestUpdateStockLocationTypeNotFound(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newStockLocationTypeAdminFake()
	_, out, err := updateStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockLocationTypeInput{ID: 5, Description: dvgoutils.Ptr("x")})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestUpdateStockLocationTypeRejectsCollidingName(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	fake.types[6] = inventree.StockLocationType{PK: 6, Name: "Bin"}
	_, out, err := updateStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockLocationTypeInput{ID: 5, Name: dvgoutils.Ptr("Bin")})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCalls)
}

func TestDeleteStockLocationTypePreviewsExecutesAndVerifiesAbsence(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	fake.referencing[5] = []int{11, 12}

	_, preview, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, preview.Status)
	r.NotEmpty(preview.PlanHash)
	a.Equal([]int{11, 12}, preview.ReferencingLocationIDs)
	a.Zero(fake.deleteCalls)

	_, executed, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.True(executed.Verified)
	a.Equal(1, fake.deleteCalls)
	_, stillPresent := fake.types[5]
	a.False(stillPresent)
}

func TestDeleteStockLocationTypeRejectsStaleOrMissingPlan(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}

	_, out, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5, Confirm: true, PlanHash: "not-a-real-token"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.deleteCalls)
}

func TestDeleteStockLocationTypeRejectsPlanStaleAfterReferenceChange(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	fake.referencing[5] = []int{11}

	_, preview, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5})
	r.NoError(err)
	r.NotEmpty(preview.PlanHash)

	// A new location starts referencing the type between preview and
	// confirm; the bound reference snapshot changes so the token must be
	// rejected as stale rather than silently deleting against unreviewed
	// state.
	fake.referencing[5] = []int{11, 12}
	_, executed, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	assert.Equal(t, StatusClarificationRequired, executed.Status)
	assert.Zero(t, fake.deleteCalls)
}

func TestDeleteStockLocationTypeReferenceScanFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	for offset := 0; offset < stockLocationTypeReferenceScanLimit; offset += stockLocationTypeReferencePageSize {
		fake.locationPages[offset] = inventree.StockLocationPage{HasMore: true}
	}
	_, out, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Contains(t, out.Clarification.Reason, "safety limit")
}

func TestDeleteStockLocationTypeNotFound(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newStockLocationTypeAdminFake()
	_, out, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestDeleteStockLocationTypeRecoversAmbiguousMutationByReadBack(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	fake.deleteErr = errors.New("connection reset after delete")
	fake.deleteErrDeletesAnyway = true

	_, preview, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5})
	r.NoError(err)
	_, executed, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.True(executed.Verified)
	a.True(executed.Recovered)
}

func TestDeleteStockLocationTypeStopsOnDefiniteMutationRejection(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	fake.deleteErr = &inventree.APIError{StatusCode: 409, Kind: inventree.ErrorKindConflict}

	_, preview, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5})
	r.NoError(err)
	_, _, err = deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deletion")
}

func TestDeleteStockLocationTypeReportsSurvivalAfterDelete(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	fake.deleteNoOp = true

	_, preview, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5})
	r.NoError(err)
	_, executed, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, executed.Status)
	a.Contains(executed.RecoveryPlan, "still exists")
}

func TestDeleteStockLocationTypeReportsUnverifiableReadBack(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}

	_, preview, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5})
	r.NoError(err)
	fake.getErrAfterDelete = errors.New("network blip")
	_, executed, err := deleteStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteStockLocationTypeInput{ID: 5, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, executed.Status)
	a.Contains(executed.RecoveryPlan, "could not prove absence")
}

func TestUpdateStockLocationTypeRequiresAtLeastOneField(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	_, out, err := updateStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockLocationTypeInput{ID: 5})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCalls)
}

func TestUpdateStockLocationTypeRejectsBlankNameAfterTrim(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	_, out, err := updateStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockLocationTypeInput{ID: 5, Name: dvgoutils.Ptr("   ")})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Zero(t, fake.updateCalls)
}

func TestUpdateStockLocationTypeReportsUnverifiedReadBackAfterPatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	fake.getErrAfterUpdate = errors.New("network blip")

	_, out, err := updateStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockLocationTypeInput{ID: 5, Description: dvgoutils.Ptr("new")})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	a.Contains(out.RecoveryPlan, "may have changed")
}

func TestUpdateStockLocationTypeReportsPostWriteDuplicateConflict(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newStockLocationTypeAdminFake()
	fake.types[5] = inventree.StockLocationType{PK: 5, Name: "Shelf"}
	fake.duplicateAfterWrite = inventree.StockLocationType{PK: 6, Name: "Shelf"}

	_, out, err := updateStockLocationType(stockLocationTypeAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateStockLocationTypeInput{ID: 5, Description: dvgoutils.Ptr("new")})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	a.Contains(out.RecoveryPlan, "duplicate-name uniqueness")
}

type stockLocationTypeAdminFake struct {
	types                  map[int]inventree.StockLocationType
	pages                  map[int]inventree.StockLocationTypePage
	locationPages          map[int]inventree.StockLocationPage
	referencing            map[int][]int
	createResult           inventree.StockLocationType
	createErr              error
	createCalls            int
	updateCalls            int
	deleteCalls            int
	deleteErr              error
	deleteErrDeletesAnyway bool
	deleteNoOp             bool
	deleted                bool
	updated                bool
	getErrAfterDelete      error
	getErrAfterUpdate      error
	duplicateAfterWrite    inventree.StockLocationType
	lastCreate             inventree.StockLocationTypeCreate
	lastPatch              inventree.PatchFields
	planStore              *stockLocationTypeDeletePlanStore
}

func newStockLocationTypeAdminFake() *stockLocationTypeAdminFake {
	return &stockLocationTypeAdminFake{
		types: map[int]inventree.StockLocationType{}, pages: map[int]inventree.StockLocationTypePage{},
		locationPages: map[int]inventree.StockLocationPage{}, referencing: map[int][]int{},
	}
}

func stockLocationTypeAdminDeps(fake *stockLocationTypeAdminFake) Dependencies {
	if fake.planStore == nil {
		fake.planStore = newStockLocationTypeDeletePlanStore(time.Now, randomStockPlanToken)
	}
	return Dependencies{
		ClientFromContext:                func(context.Context) (any, error) { return fake, nil },
		stockLocationTypeDeletePlanStore: fake.planStore,
	}
}

func (f *stockLocationTypeAdminFake) GetStockLocationType(_ context.Context, id int) (inventree.StockLocationType, error) {
	if f.deleted && f.getErrAfterDelete != nil {
		return inventree.StockLocationType{}, f.getErrAfterDelete
	}
	if f.updated && f.getErrAfterUpdate != nil {
		return inventree.StockLocationType{}, f.getErrAfterUpdate
	}
	value, ok := f.types[id]
	if !ok {
		return inventree.StockLocationType{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func (f *stockLocationTypeAdminFake) SearchStockLocationTypesPage(_ context.Context, query inventree.SearchQuery) (inventree.StockLocationTypePage, error) {
	if page, ok := f.pages[query.Offset]; ok {
		return page, nil
	}
	results := make([]inventree.StockLocationType, 0)
	for _, value := range f.types {
		results = append(results, value)
	}
	if f.duplicateAfterWrite.PK != 0 {
		results = append(results, f.duplicateAfterWrite)
	}
	return inventree.StockLocationTypePage{Count: len(results), Results: results}, nil
}

func (f *stockLocationTypeAdminFake) CreateStockLocationType(_ context.Context, input inventree.StockLocationTypeCreate) (inventree.StockLocationType, error) {
	f.createCalls++
	f.lastCreate = input
	f.types[f.createResult.PK] = f.createResult
	return f.createResult, f.createErr
}

func (f *stockLocationTypeAdminFake) UpdateStockLocationType(_ context.Context, id int, fields inventree.PatchFields) (inventree.StockLocationType, error) {
	f.updateCalls++
	f.updated = true
	f.lastPatch = fields
	value := f.types[id]
	for key, patch := range fields {
		switch key {
		case "name":
			value.Name = patch.Value().(string)
		case "description":
			value.Description = patch.Value().(string)
		case "icon":
			value.Icon = patch.Value().(string)
		}
	}
	f.types[id] = value
	return value, nil
}

func (f *stockLocationTypeAdminFake) DeleteStockLocationType(_ context.Context, id int) error {
	f.deleteCalls++
	f.deleted = true
	if f.deleteNoOp {
		return nil
	}
	if f.deleteErr != nil {
		if f.deleteErrDeletesAnyway {
			delete(f.types, id)
		}
		return f.deleteErr
	}
	delete(f.types, id)
	return nil
}

func (f *stockLocationTypeAdminFake) SearchStockLocationsPage(_ context.Context, query inventree.StockLocationQuery) (inventree.StockLocationPage, error) {
	if page, ok := f.locationPages[query.Offset]; ok {
		return page, nil
	}
	if query.LocationType == nil {
		return inventree.StockLocationPage{}, nil
	}
	ids := f.referencing[*query.LocationType]
	results := make([]inventree.StockLocation, 0, len(ids))
	for _, id := range ids {
		results = append(results, inventree.StockLocation{PK: id})
	}
	return inventree.StockLocationPage{Count: len(results), Results: results}, nil
}

func (f *stockLocationTypeAdminFake) lastPatchHas(key string) bool {
	_, ok := f.lastPatch[key]
	return ok
}

func (f *stockLocationTypeAdminFake) lastPatchIs(key, expected string) bool {
	value, ok := f.lastPatch[key]
	if !ok {
		return false
	}
	got, ok := value.Value().(string)
	return ok && got == expected
}
