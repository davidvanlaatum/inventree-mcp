package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePartRelationClient struct {
	relations                map[int]inventree.PartRelation
	nextID                   int
	createErr                error
	applyCreateOnError       bool
	updateErr                error
	deleteErr                error
	keepAfterDelete          bool
	queries                  []inventree.PartRelationQuery
	pageErr                  error
	forceHasMore             bool
	pageFunc                 func(inventree.PartRelationQuery) (inventree.PartRelationPage, error)
	getErr                   error
	getErrAfterMutation      error
	getOverrideAfterMutation *inventree.PartRelation
	mutationDone             bool
}

func newFakePartRelationClient(records ...inventree.PartRelation) *fakePartRelationClient {
	f := &fakePartRelationClient{relations: map[int]inventree.PartRelation{}, nextID: 100}
	for _, record := range records {
		f.relations[record.PK] = record
		if record.PK >= f.nextID {
			f.nextID = record.PK + 1
		}
	}
	return f
}

func (f *fakePartRelationClient) SearchPartRelations(_ context.Context, query inventree.PartRelationQuery) ([]inventree.PartRelation, error) {
	f.queries = append(f.queries, query)
	result := f.filtered(query)
	start := query.Offset
	if start > len(result) {
		start = len(result)
	}
	end := len(result)
	if query.Limit > 0 && start+query.Limit < end {
		end = start + query.Limit
	}
	return result[start:end], nil
}

func (f *fakePartRelationClient) SearchPartRelationsPage(_ context.Context, query inventree.PartRelationQuery) (inventree.PartRelationPage, error) {
	f.queries = append(f.queries, query)
	if f.pageFunc != nil {
		return f.pageFunc(query)
	}
	if f.pageErr != nil {
		return inventree.PartRelationPage{}, f.pageErr
	}
	all := f.filtered(query)
	start := query.Offset
	if start > len(all) {
		start = len(all)
	}
	end := len(all)
	if query.Limit > 0 && start+query.Limit < end {
		end = start + query.Limit
	}
	return inventree.PartRelationPage{Count: len(all), Results: all[start:end], HasMore: f.forceHasMore || end < len(all)}, nil
}

func (f *fakePartRelationClient) filtered(query inventree.PartRelationQuery) []inventree.PartRelation {
	result := []inventree.PartRelation{}
	for _, record := range f.relations {
		if query.Part > 0 && record.Part1 != query.Part && record.Part2 != query.Part {
			continue
		}
		if query.Part1 > 0 && record.Part1 != query.Part1 {
			continue
		}
		if query.Part2 > 0 && record.Part2 != query.Part2 {
			continue
		}
		result = append(result, record)
	}
	return result
}

func (f *fakePartRelationClient) GetPartRelation(_ context.Context, id int) (inventree.PartRelation, error) {
	if f.mutationDone && f.getErrAfterMutation != nil {
		return inventree.PartRelation{}, f.getErrAfterMutation
	}
	if f.mutationDone && f.getOverrideAfterMutation != nil {
		return *f.getOverrideAfterMutation, nil
	}
	if f.getErr != nil {
		return inventree.PartRelation{}, f.getErr
	}
	record, ok := f.relations[id]
	if !ok {
		return inventree.PartRelation{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	return record, nil
}

func (f *fakePartRelationClient) CreatePartRelation(_ context.Context, input inventree.PartRelationCreate) (inventree.PartRelation, error) {
	record := inventree.PartRelation{PK: f.nextID, Part1: input.Part1, Part2: input.Part2, Note: input.Note}
	if f.createErr == nil || f.applyCreateOnError {
		f.relations[record.PK] = record
		f.nextID++
	}
	f.mutationDone = true
	return record, f.createErr
}

func (f *fakePartRelationClient) UpdatePartRelation(_ context.Context, id int, fields inventree.PatchFields) (inventree.PartRelation, error) {
	record := f.relations[id]
	if value, ok := fields["note"]; ok && value.Value() != nil {
		record.Note = value.Value().(string)
		f.relations[id] = record
	}
	f.mutationDone = true
	return record, f.updateErr
}

func (f *fakePartRelationClient) DeletePartRelation(_ context.Context, id int) error {
	if !f.keepAfterDelete {
		delete(f.relations, id)
	}
	f.mutationDone = true
	return f.deleteErr
}

func partRelationDeps(fake *fakePartRelationClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }, partRelationPlanStore: newPartRelationPlanStore(time.Now, randomStockPlanToken)}
}

func TestListPartRelationsRequiresStablePartAndSorts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient(inventree.PartRelation{PK: 2, Part1: 20, Part2: 10}, inventree.PartRelation{PK: 1, Part1: 10, Part2: 30})
	_, invalid, err := listPartRelations(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, ListPartRelationsInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, invalid.Status)
	_, out, err := listPartRelations(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, ListPartRelationsInput{PartID: 10})
	r.NoError(err)
	a.Equal([]int{1, 2}, []int{out.Results[0].PK, out.Results[1].PK})
	a.Equal(10, fake.queries[len(fake.queries)-1].Part)
}

func TestCreatePartRelationRejectsSelfAndReversedDuplicate(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 2, Part2: 1, Note: "existing"})
	_, self, err := createPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: 1, Part2ID: 1})
	r.NoError(err)
	a.Equal(StatusValidationFailed, self.Status)
	_, duplicate, err := createPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: 1, Part2ID: 2})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, duplicate.Status)
	r.Len(duplicate.Candidates, 1)
	a.Equal(7, duplicate.Candidates[0].PK)
}

func TestUpdatePartRelationUsesStateBoundSingleUsePlan(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 1, Part2: 2, Note: "old"})
	deps := partRelationDeps(fake)
	note := "new"
	_, preview, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, Note: &note})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, preview.Status)
	a.NotEmpty(preview.PlanHash)
	_, applied, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, Note: &note, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, applied.Status)
	a.True(applied.Verified)
	a.Equal("new", applied.Record.Note)
	_, reused, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, ClearNote: true, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, reused.Status)
}

func TestUpdatePartRelationRejectsStalePlan(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 1, Part2: 2, Note: "old"})
	deps := partRelationDeps(fake)
	note := "new"
	_, preview, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, Note: &note})
	r.NoError(err)
	fake.relations[7] = inventree.PartRelation{PK: 7, Part1: 1, Part2: 2, Note: "concurrent"}
	_, stale, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, Note: &note, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Equal("concurrent", fake.relations[7].Note)
}

func TestDeletePartRelationPreviewsAndVerifies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 1, Part2: 2, Note: "remove"})
	deps := partRelationDeps(fake)
	_, preview, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 7})
	r.NoError(err)
	a.NotEmpty(preview.PlanHash)
	r.NotNil(preview.Plan)
	_, out, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 7, Confirm: true, PlanHash: preview.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(out.Verified)
	a.Equal(7, out.Record.PK)
}

func TestGetAndCreatePartRelationRecovery(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 1, Part2: 2})
	_, exact, err := getPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 7})
	r.NoError(err)
	a.Equal(StatusOK, exact.Status)
	a.Equal(7, exact.Record.PK)
	_, missing, err := getPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 8})
	r.NoError(err)
	a.Equal(StatusNotFound, missing.Status)

	fake = newFakePartRelationClient()
	_, created, err := createPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: 3, Part2ID: 4, Note: "mate"})
	r.NoError(err)
	a.Equal(StatusOK, created.Status)
	a.True(created.Verified)
	a.False(created.Recovered)
	fake = newFakePartRelationClient()
	fake.createErr = errors.New("response lost")
	fake.applyCreateOnError = true
	_, recovered, err := createPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: 5, Part2ID: 6, Note: "mate"})
	r.NoError(err)
	a.Equal(StatusOK, recovered.Status)
	a.True(recovered.Verified)
	a.True(recovered.Recovered)
}

func TestAmbiguousPartRelationMutationsUseReadBack(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 1, Part2: 2, Note: "old"})
	deps := partRelationDeps(fake)
	note := "new"
	fake.updateErr = errors.New("response lost")
	_, plan, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, Note: &note})
	r.NoError(err)
	_, updated, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, Note: &note, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, updated.Status)
	a.True(updated.Recovered)
	fake.deleteErr = errors.New("response lost")
	_, deletePlan, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 7})
	r.NoError(err)
	_, deleted, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 7, Confirm: true, PlanHash: deletePlan.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, deleted.Status)
	a.True(deleted.Recovered)
	a.True(deleted.Verified)
}

func TestPartRelationPlanStoreBindsPrincipalAndCapacity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	now := time.Unix(100, 0)
	next := 0
	store := newPartRelationPlanStore(func() time.Time { return now }, func() (string, error) { next++; return string(rune('a' + next)), nil })
	principal := "alice"
	store.principal = func(context.Context) string { return principal }
	store.maxEntriesPerPrincipal = 1
	first := PartRelationPlan{Action: "delete", Before: inventree.PartRelation{PK: 1, Part1: 1, Part2: 2}}
	token, err := store.issue(context.Background(), first)
	r.NoError(err)
	_, err = store.issue(context.Background(), PartRelationPlan{Action: "delete", Before: inventree.PartRelation{PK: 2, Part1: 2, Part2: 3}})
	r.Error(err)
	principal = "bob"
	a.False(store.consume(context.Background(), token, first))
	principal = "alice"
	a.True(store.consume(context.Background(), token, first))
	a.False(store.consume(context.Background(), token, first))
	store.maxEntriesPerPrincipal = partRelationPlanMaxEntriesPerPrincipal
	expiring, err := store.issue(context.Background(), first)
	r.NoError(err)
	now = now.Add(partRelationPlanLifetime)
	a.False(store.consume(context.Background(), expiring, first), "a token expires exactly at the documented five-minute boundary")
}

func TestPartRelationValidationNoopClearAndDeletePartial(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 1, Part2: 2, Note: "old"})
	deps := partRelationDeps(fake)
	tooLong := string(make([]rune, 501))
	_, invalidPair, err := createPartRelation(deps)(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: -1, Part2ID: 2})
	r.NoError(err)
	a.Equal(StatusValidationFailed, invalidPair.Status)
	_, invalidNote, err := createPartRelation(deps)(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: 1, Part2ID: 2, Note: tooLong})
	r.NoError(err)
	a.Equal(StatusValidationFailed, invalidNote.Status)
	_, invalidUpdate, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7})
	r.NoError(err)
	a.Equal(StatusValidationFailed, invalidUpdate.Status)
	old := "old"
	_, noop, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, Note: &old})
	r.NoError(err)
	a.Equal(StatusValidationFailed, noop.Status)
	_, clearPlan, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, ClearNote: true})
	r.NoError(err)
	_, cleared, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, ClearNote: true, Confirm: true, PlanHash: clearPlan.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, cleared.Status)
	a.Empty(cleared.Record.Note)
	_, missing, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 99, ClearNote: true})
	r.NoError(err)
	a.Equal(StatusNotFound, missing.Status)
	_, invalidDelete, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{})
	r.NoError(err)
	a.Equal(StatusValidationFailed, invalidDelete.Status)
	fake.relations[7] = inventree.PartRelation{PK: 7, Part1: 1, Part2: 2}
	fake.keepAfterDelete = true
	_, deletePlan, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 7})
	r.NoError(err)
	_, partial, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 7, Confirm: true, PlanHash: deletePlan.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, partial.Status)
	_, missingDelete, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 99})
	r.NoError(err)
	a.Equal(StatusNotFound, missingDelete.Status)
}

func TestPartRelationScansFailClosedOnErrorsAndBudgets(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient()
	fake.pageErr = errors.New("unavailable")
	_, _, err := listPartRelations(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, ListPartRelationsInput{PartID: 1})
	r.Error(err)
	_, _, err = createPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: 1, Part2ID: 2})
	r.Error(err)
	fake.pageErr = nil
	fake.forceHasMore = true
	_, _, err = listPartRelations(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, ListPartRelationsInput{PartID: 1})
	r.ErrorContains(err, "budget")
	r.Len(fake.queries, partRelationMaxRequests+2, "two earlier error calls plus exactly the 20-request list budget")
	fake.queries = nil
	_, _, err = createPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: 1, Part2ID: 2})
	r.ErrorContains(err, "budget")
	r.Len(fake.queries, partRelationMaxRequests)
}

func TestPartRelationDuplicateScanSharesRecordBudgetAndDeduplicatesIDs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient()
	fake.pageFunc = func(query inventree.PartRelationQuery) (inventree.PartRelationPage, error) {
		rows := make([]inventree.PartRelation, partRelationPageSize)
		for i := range rows {
			rows[i] = inventree.PartRelation{PK: query.Offset + i + 1, Part1: 1, Part2: 2}
		}
		return inventree.PartRelationPage{Count: partRelationMaxRecords, Results: rows, HasMore: query.Offset+partRelationPageSize < partRelationMaxRecords}, nil
	}
	_, err := findPartRelationPair(ctx, fake, 1, 2)
	r.ErrorContains(err, "budget")
	r.Len(fake.queries, partRelationMaxRecords/partRelationPageSize, "the first direction consumes the shared 1000-record budget before direction two")

	fake.queries = nil
	fake.pageFunc = func(query inventree.PartRelationQuery) (inventree.PartRelationPage, error) {
		if query.Part1 == 1 {
			rows := make([]inventree.PartRelation, partRelationPageSize)
			for i := range rows {
				rows[i] = inventree.PartRelation{PK: query.Offset + i + 1, Part1: 1, Part2: 2}
			}
			return inventree.PartRelationPage{Count: 900, Results: rows, HasMore: query.Offset+partRelationPageSize < 900}, nil
		}
		rows := make([]inventree.PartRelation, partRelationPageSize)
		for i := range rows {
			rows[i] = inventree.PartRelation{PK: 901 + i, Part1: 2, Part2: 1}
		}
		return inventree.PartRelationPage{Count: 100, Results: rows}, nil
	}
	matches, err := findPartRelationPair(ctx, fake, 1, 2)
	r.NoError(err)
	r.Len(matches, partRelationMaxRecords, "a complete two-direction scan totaling exactly 1000 records must succeed")
	r.Equal(1, matches[0].PK)
	r.Equal(partRelationMaxRecords, matches[len(matches)-1].PK)

	fake.queries = nil
	fake.pageFunc = func(query inventree.PartRelationQuery) (inventree.PartRelationPage, error) {
		if query.Part1 == 1 {
			return inventree.PartRelationPage{Results: []inventree.PartRelation{{PK: 9, Part1: 1, Part2: 2}, {PK: 7, Part1: 1, Part2: 2}}}, nil
		}
		return inventree.PartRelationPage{Results: []inventree.PartRelation{{PK: 8, Part1: 2, Part2: 1}, {PK: 7, Part1: 1, Part2: 2}}}, nil
	}
	matches, err = findPartRelationPair(ctx, fake, 1, 2)
	r.NoError(err)
	r.Equal([]int{7, 8, 9}, []int{matches[0].PK, matches[1].PK, matches[2].PK}, "overlapping directional results must be deduplicated and sorted by stable ID")
}

func TestPartRelationToolsMCPWireContract(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 1, Part2: 2, Note: "old"})
	session, closeSession := plannedChangesSession(t, ctx, fake)
	defer closeSession()

	wanted := map[string]ToolAuthorization{
		ListPartRelationsToolName:  ToolAuthorizations[ListPartRelationsToolName],
		GetPartRelationToolName:    ToolAuthorizations[GetPartRelationToolName],
		CreatePartRelationToolName: ToolAuthorizations[CreatePartRelationToolName],
		UpdatePartRelationToolName: ToolAuthorizations[UpdatePartRelationToolName],
		DeletePartRelationToolName: ToolAuthorizations[DeletePartRelationToolName],
	}
	listed, err := session.ListTools(ctx, nil)
	r.NoError(err)
	for _, tool := range listed.Tools {
		auth, ok := wanted[tool.Name]
		if !ok {
			continue
		}
		delete(wanted, tool.Name)
		r.NotNil(tool.Annotations)
		a.Equal(auth.Annotations.ReadOnly, tool.Annotations.ReadOnlyHint)
		r.NotNil(tool.Annotations.DestructiveHint)
		a.Equal(auth.Annotations.Destructive, *tool.Annotations.DestructiveHint)
		a.Equal(auth.Annotations.Idempotent, tool.Annotations.IdempotentHint)
		r.NotNil(tool.Annotations.OpenWorldHint)
		a.Equal(auth.Annotations.OpenWorld, *tool.Annotations.OpenWorldHint)
		input := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		output := tool.OutputSchema.(map[string]any)["properties"].(map[string]any)
		a.NotEmpty(input)
		a.Contains(output, "status")
		meta, marshalErr := json.Marshal(tool.Meta)
		r.NoError(marshalErr)
		for _, scope := range auth.Scopes {
			a.Contains(string(meta), scope)
		}
	}
	a.Empty(wanted, "all five related-part descriptors must cross the MCP ListTools boundary")

	for _, call := range []struct {
		name      string
		arguments map[string]any
		status    string
	}{
		{ListPartRelationsToolName, map[string]any{"part_id": 1}, StatusOK},
		{GetPartRelationToolName, map[string]any{"id": 7}, StatusOK},
		{CreatePartRelationToolName, map[string]any{"part_1_id": 3, "part_2_id": 4, "note": "wire"}, StatusOK},
		{UpdatePartRelationToolName, map[string]any{"id": 7, "clear_note": true}, StatusClarificationRequired},
		{DeletePartRelationToolName, map[string]any{"id": 7}, StatusClarificationRequired},
	} {
		result, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.arguments})
		r.NoError(callErr)
		a.False(result.IsError)
		structured := result.StructuredContent.(map[string]any)
		a.Equal(call.status, structured["status"])
	}
}

func TestPartRelationExactIdentityRecoveryAndCancellation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	mismatch := inventree.PartRelation{PK: 999, Part1: 1, Part2: 2, Note: "mate"}
	fake := newFakePartRelationClient()
	fake.getOverrideAfterMutation = &mismatch
	_, out, err := createPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: 1, Part2ID: 2, Note: "mate"})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	a.False(out.Recovered)

	fake = newFakePartRelationClient()
	fake.createErr = errors.New("response lost")
	_, unresolved, err := createPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: 3, Part2ID: 4})
	r.NoError(err)
	a.Equal(StatusPartialFailure, unresolved.Status)
	a.False(unresolved.Recovered)
	a.Nil(unresolved.Record)
	for _, contextErr := range []error{context.Canceled, context.DeadlineExceeded} {
		fake = newFakePartRelationClient()
		fake.getErrAfterMutation = contextErr
		_, _, err = createPartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: 10, Part2ID: 11})
		r.ErrorIs(err, contextErr)
	}

	fake = newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 1, Part2: 2})
	fake.getErr = context.Canceled
	_, _, err = updatePartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, ClearNote: true})
	r.ErrorIs(err, context.Canceled)
	fake.getErr = context.DeadlineExceeded
	_, _, err = deletePartRelation(partRelationDeps(fake))(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 7})
	r.ErrorIs(err, context.DeadlineExceeded)

	fake = newFakePartRelationClient()
	fake.pageErr = context.Canceled
	_, _, err = recoverPartRelationCreate(ctx, fake, CreatePartRelationInput{Part1ID: 1, Part2ID: 2})
	r.ErrorIs(err, context.Canceled)

	fake = newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 1, Part2: 2, Note: "old"})
	fake.getErrAfterMutation = errors.New("read failed")
	deps := partRelationDeps(fake)
	note := "new"
	_, plan, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, Note: &note})
	r.NoError(err)
	_, partial, err := updatePartRelation(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: 7, Note: &note, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, partial.Status)
	a.Nil(partial.Record)
	a.False(partial.Recovered)

	fake = newFakePartRelationClient(inventree.PartRelation{PK: 8, Part1: 1, Part2: 3})
	fake.getErrAfterMutation = context.DeadlineExceeded
	deps = partRelationDeps(fake)
	_, deletePlan, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 8})
	r.NoError(err)
	_, _, err = deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 8, Confirm: true, PlanHash: deletePlan.PlanHash})
	r.ErrorIs(err, context.DeadlineExceeded)
}

func TestDeletePartRelationPartialReturnsOnlyVerifiedCurrentState(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	changed := inventree.PartRelation{PK: 7, Part1: 1, Part2: 2, Note: "concurrent"}
	fake := newFakePartRelationClient(inventree.PartRelation{PK: 7, Part1: 1, Part2: 2, Note: "before"})
	fake.keepAfterDelete = true
	fake.getOverrideAfterMutation = &changed
	deps := partRelationDeps(fake)
	_, plan, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 7})
	r.NoError(err)
	_, partial, err := deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 7, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, partial.Status)
	r.NotNil(partial.Record)
	a.Equal("concurrent", partial.Record.Note)

	fake = newFakePartRelationClient(inventree.PartRelation{PK: 8, Part1: 1, Part2: 3})
	fake.keepAfterDelete = true
	fake.getErrAfterMutation = errors.New("read failed")
	deps = partRelationDeps(fake)
	_, plan, err = deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 8})
	r.NoError(err)
	_, partial, err = deletePartRelation(deps)(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: 8, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, partial.Status)
	a.Nil(partial.Record)
	a.False(partial.Recovered)
}
