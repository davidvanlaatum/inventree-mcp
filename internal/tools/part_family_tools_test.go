package tools

import (
	"context"
	"encoding/json"
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

func TestUpdatePartFamilyRelationshipsPlansAndConfirmsBothRelationships(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	revisionCode := "C"
	fake := newFakePartFamilyClient(
		inventree.PartDetail{PK: 10, Revision: &revisionCode, RevisionOf: dvgoutils.Ptr(1), VariantOf: dvgoutils.Ptr(2)},
		inventree.PartDetail{PK: 20, RevisionOf: dvgoutils.Ptr(3), VariantOf: dvgoutils.Ptr(30)},
		inventree.PartDetail{PK: 30, IsTemplate: true, VariantOf: dvgoutils.Ptr(4)},
		inventree.PartDetail{PK: 3, VariantOf: dvgoutils.Ptr(30)},
		inventree.PartDetail{PK: 4, IsTemplate: true},
	)
	deps := partFamilyDeps(fake)
	input := UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), VariantOfID: dvgoutils.Ptr(30), DryRun: true}

	_, preview, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, preview.Status)
	a.True(preview.DryRun)
	a.NotEmpty(preview.PlanHash)
	r.NotNil(preview.Plan)
	a.Equal(dvgoutils.Ptr(1), preview.Plan.Before.RevisionOf)
	a.Equal(dvgoutils.Ptr(2), preview.Plan.Before.VariantOf)
	a.Equal(dvgoutils.Ptr(20), preview.Plan.After.RevisionOf)
	a.Equal(dvgoutils.Ptr(30), preview.Plan.After.VariantOf)
	a.Equal([]int{3, 4, 10, 20, 30}, topologyIDs(preview.Plan.TopologyEvidence))
	a.Empty(fake.updates)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = preview.PlanHash
	_, output, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	r.NotNil(output.Record)
	a.Equal(dvgoutils.Ptr(20), output.Record.RevisionOf)
	a.Equal(dvgoutils.Ptr(30), output.Record.VariantOf)
	r.Len(fake.updates, 1)
	a.Equal(20, fake.updates[0]["revision_of"].Value())
	a.Equal(30, fake.updates[0]["variant_of"].Value())

	fake.records[10] = inventree.PartDetail{PK: 10, Revision: &revisionCode, RevisionOf: dvgoutils.Ptr(1), VariantOf: dvgoutils.Ptr(2)}
	_, reused, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, reused.Status)
	a.Equal("confirmation", reused.Clarification.Field)
	r.Len(fake.updates, 1)
}

func TestUpdatePartFamilyRelationshipsRejectsSelfCycleAndChangedTopology(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	revisionCode := "B"
	fake := newFakePartFamilyClient(
		inventree.PartDetail{PK: 10, Revision: &revisionCode},
		inventree.PartDetail{PK: 20, RevisionOf: dvgoutils.Ptr(30)},
		inventree.PartDetail{PK: 30},
	)
	deps := partFamilyDeps(fake)

	_, self, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(10), DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, self.Status)
	a.Equal("revision_of_id", self.Clarification.Field)

	fake.records[30] = inventree.PartDetail{PK: 30, RevisionOf: dvgoutils.Ptr(10)}
	_, cycle, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, cycle.Status)
	a.Contains(cycle.Clarification.Reason, "cycle")

	fake.records[30] = inventree.PartDetail{PK: 30}
	input := UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true}
	_, preview, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	fake.records[20] = inventree.PartDetail{PK: 20, RevisionOf: dvgoutils.Ptr(40)}
	fake.records[40] = inventree.PartDetail{PK: 40}
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = preview.PlanHash
	_, stale, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Empty(fake.updates)
}

func TestUpdatePartFamilyRelationshipsFailsClosedAtSharedTraversalBudget(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	records := []inventree.PartDetail{{PK: 1}}
	for id := 2; id <= partFamilyTopologyMaxRecords+2; id++ {
		record := inventree.PartDetail{PK: id}
		record.IsTemplate = true
		if id < partFamilyTopologyMaxRecords+2 {
			record.VariantOf = dvgoutils.Ptr(id + 1)
		}
		records = append(records, record)
	}
	fake := newFakePartFamilyClient(records...)
	_, output, err := updatePartFamilyRelationships(partFamilyDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartFamilyRelationshipsInput{ID: 1, VariantOfID: dvgoutils.Ptr(2), DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("topology", output.Clarification.Field)
	a.Contains(output.Clarification.Reason, "budget")
	a.Empty(fake.updates)
}

func TestUpdatePartFamilyRelationshipsClarifiesMissingAndMalformedTopology(t *testing.T) {
	t.Parallel()
	revision := "B"
	for _, test := range []struct {
		name  string
		input UpdatePartFamilyRelationshipsInput
		fake  *fakePartFamilyClient
		field string
	}{
		{name: "missing revision target", input: UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true}, fake: newFakePartFamilyClient(inventree.PartDetail{PK: 10, Revision: &revision}), field: "revision_of_id"},
		{name: "missing variant ancestor", input: UpdatePartFamilyRelationshipsInput{ID: 10, VariantOfID: dvgoutils.Ptr(20), DryRun: true}, fake: newFakePartFamilyClient(inventree.PartDetail{PK: 10}, inventree.PartDetail{PK: 20, IsTemplate: true, VariantOf: dvgoutils.Ptr(30)}), field: "variant_of_id"},
		{name: "invalid revision ancestor ID", input: UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true}, fake: newFakePartFamilyClient(inventree.PartDetail{PK: 10, Revision: &revision}, inventree.PartDetail{PK: 20, RevisionOf: dvgoutils.Ptr(0)}), field: "revision_of_id"},
		{name: "invalid variant ancestor ID", input: UpdatePartFamilyRelationshipsInput{ID: 10, VariantOfID: dvgoutils.Ptr(20), DryRun: true}, fake: newFakePartFamilyClient(inventree.PartDetail{PK: 10}, inventree.PartDetail{PK: 20, IsTemplate: true, VariantOf: dvgoutils.Ptr(-1)}), field: "variant_of_id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			_, output, err := updatePartFamilyRelationships(partFamilyDeps(test.fake))(ctx, &mcp.CallToolRequest{}, test.input)
			r.NoError(err)
			a.Equal(StatusClarificationRequired, output.Status)
			r.NotNil(output.Clarification)
			a.Equal(test.field, output.Clarification.Field)
			a.Empty(test.fake.updates)
		})
	}

	t.Run("missing subject", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		_, output, err := updatePartFamilyRelationships(partFamilyDeps(newFakePartFamilyClient()))(ctx, &mcp.CallToolRequest{}, UpdatePartFamilyRelationshipsInput{ID: 10, ClearRevisionOf: true, DryRun: true})
		r.NoError(err)
		a.Equal(StatusNotFound, output.Status)
	})
}

func TestUpdatePartFamilyRelationshipsEnforcesPinnedRevisionAndVariantRules(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	templateID := 30
	for _, test := range []struct {
		name   string
		fake   *fakePartFamilyClient
		input  UpdatePartFamilyRelationshipsInput
		field  string
		reason string
	}{
		{
			name:  "revision code required",
			fake:  newFakePartFamilyClient(inventree.PartDetail{PK: 10}, inventree.PartDetail{PK: 20}),
			input: UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true},
			field: "revision", reason: "revision code",
		},
		{
			name:  "revision target cannot be template",
			fake:  newFakePartFamilyClient(inventree.PartDetail{PK: 10, Revision: dvgoutils.Ptr("B")}, inventree.PartDetail{PK: 20, IsTemplate: true}),
			input: UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true},
			field: "revision_of_id", reason: "template",
		},
		{
			name:  "revision family must share template",
			fake:  newFakePartFamilyClient(inventree.PartDetail{PK: 10, Revision: dvgoutils.Ptr("B")}, inventree.PartDetail{PK: 20, VariantOf: &templateID}, inventree.PartDetail{PK: templateID, IsTemplate: true}),
			input: UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true},
			field: "revision_of_id", reason: "same variant template",
		},
		{
			name:  "variant target must be template",
			fake:  newFakePartFamilyClient(inventree.PartDetail{PK: 10}, inventree.PartDetail{PK: 20}),
			input: UpdatePartFamilyRelationshipsInput{ID: 10, VariantOfID: dvgoutils.Ptr(20), DryRun: true},
			field: "variant_of_id", reason: "template",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			_, output, err := updatePartFamilyRelationships(partFamilyDeps(test.fake))(ctx, &mcp.CallToolRequest{}, test.input)
			r.NoError(err)
			a.Equal(StatusClarificationRequired, output.Status)
			r.NotNil(output.Clarification)
			a.Equal(test.field, output.Clarification.Field)
			a.Contains(output.Clarification.Reason, test.reason)
			a.Empty(test.fake.updates)
		})
	}
}

func TestUpdatePartFamilyRelationshipsClearsAndRecoversLostResponse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartFamilyClient(inventree.PartDetail{PK: 10, RevisionOf: dvgoutils.Ptr(1), VariantOf: dvgoutils.Ptr(2)})
	fake.updateErr = errors.New("response lost")
	deps := partFamilyDeps(fake)
	input := UpdatePartFamilyRelationshipsInput{ID: 10, ClearRevisionOf: true, ClearVariantOf: true, DryRun: true}
	_, preview, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = preview.PlanHash
	_, output, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.Recovered)
	r.NotNil(output.Record)
	a.Nil(output.Record.RevisionOf)
	a.Nil(output.Record.VariantOf)
	r.Len(fake.updates, 1)
	a.Nil(fake.updates[0]["revision_of"].Value())
	a.Nil(fake.updates[0]["variant_of"].Value())
}

func TestUpdatePartFamilyRelationshipsValidatesInputAndConfirmation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	for _, input := range []UpdatePartFamilyRelationshipsInput{
		{ID: 10, RevisionOfID: dvgoutils.Ptr(20), ClearRevisionOf: true},
		{ID: 10, VariantOfID: dvgoutils.Ptr(20), ClearVariantOf: true},
		{ID: 10, RevisionOfID: dvgoutils.Ptr(-1)},
	} {
		_, output, err := updatePartFamilyRelationships(partFamilyDeps(newFakePartFamilyClient()))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusValidationFailed, output.Status)
		r.NotNil(output.Validation)
	}
	for _, input := range []UpdatePartFamilyRelationshipsInput{{ID: 0}, {ID: 10}} {
		_, output, err := updatePartFamilyRelationships(partFamilyDeps(newFakePartFamilyClient()))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusClarificationRequired, output.Status)
		r.NotNil(output.Clarification)
	}

	revision := "B"
	fake := newFakePartFamilyClient(inventree.PartDetail{PK: 10, Revision: &revision}, inventree.PartDetail{PK: 20})
	deps := partFamilyDeps(fake)
	input := UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20)}
	_, unconfirmed, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, unconfirmed.Status)
	a.Equal("confirmation", unconfirmed.Clarification.Field)
	input.Confirm = true
	_, missingToken, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, missingToken.Status)
	a.Equal("confirmation", missingToken.Clarification.Field)
}

func TestUpdatePartFamilyRelationshipsHandlesDefiniteAndAmbiguousFailures(t *testing.T) {
	t.Parallel()
	revision := "B"
	for _, test := range []struct {
		name          string
		updateErr     error
		skipApply     bool
		readErr       error
		wantStatus    string
		wantRecord    bool
		wantRecovered bool
	}{
		{name: "definite validation", updateErr: &inventree.APIError{StatusCode: 400, FieldErrors: map[string][]string{"revision_of": {"invalid"}}}, skipApply: true, wantStatus: StatusValidationFailed},
		{name: "ambiguous divergent readback", updateErr: errors.New("response lost"), skipApply: true, wantStatus: StatusPartialFailure, wantRecord: true},
		{name: "successful response with failed readback", readErr: errors.New("readback unavailable"), wantStatus: StatusPartialFailure},
		{name: "timeout applied", updateErr: &inventree.APIError{StatusCode: 408}, wantStatus: StatusOK, wantRecord: true, wantRecovered: true},
		{name: "too early applied", updateErr: &inventree.APIError{StatusCode: 425}, wantStatus: StatusOK, wantRecord: true, wantRecovered: true},
		{name: "rate limit applied", updateErr: &inventree.APIError{StatusCode: 429}, wantStatus: StatusOK, wantRecord: true, wantRecovered: true},
		{name: "timeout not applied", updateErr: &inventree.APIError{StatusCode: 408}, skipApply: true, wantStatus: StatusPartialFailure, wantRecord: true},
		{name: "too early not applied", updateErr: &inventree.APIError{StatusCode: 425}, skipApply: true, wantStatus: StatusPartialFailure, wantRecord: true},
		{name: "rate limit not applied", updateErr: &inventree.APIError{StatusCode: 429}, skipApply: true, wantStatus: StatusPartialFailure, wantRecord: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakePartFamilyClient(inventree.PartDetail{PK: 10, Revision: &revision}, inventree.PartDetail{PK: 20})
			deps := partFamilyDeps(fake)
			input := UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true}
			_, preview, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			fake.updateErr = test.updateErr
			fake.skipApply = test.skipApply
			fake.readErrAfterUpdate = test.readErr
			input.DryRun = false
			input.Confirm = true
			input.PlanHash = preview.PlanHash
			_, output, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			a.Equal(test.wantStatus, output.Status)
			a.Equal(test.wantRecord, output.Record != nil)
			a.Equal(test.wantRecovered, output.Recovered)
			if output.Status == StatusPartialFailure {
				a.Contains(output.RecoveryPlan, "Do not blindly retry")
				r.NotNil(output.Recovery)
				a.Nil(output.Plan)
				wire, marshalErr := json.Marshal(output.Recovery)
				r.NoError(marshalErr)
				a.NotContains(string(wire), "revision_count")
				a.NotContains(string(wire), "topology_evidence")
				a.NotContains(string(wire), "is_template")
				a.NotContains(string(wire), "has_revision_code")
			}
		})
	}
}

func TestUpdatePartFamilyRelationshipsFailsClosedWhenPlanStoreCannotIssue(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	revision := "B"
	fake := newFakePartFamilyClient(inventree.PartDetail{PK: 10, Revision: &revision}, inventree.PartDetail{PK: 20})
	store := newPartFamilyPlanStore(time.Now, randomStockPlanToken)
	store.maxEntries = 0
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }, partFamilyPlanStore: store}
	input := UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true}
	_, capacity, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, capacity.Status)
	a.Equal("confirmation", capacity.Clarification.Field)

	store = newPartFamilyPlanStore(time.Now, func() (string, error) { return "", errors.New("random unavailable") })
	deps.partFamilyPlanStore = store
	_, _, err = updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.Error(err)
}

func TestUpdatePartFamilyRelationshipsPreservesContextErrors(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(sentinel.Error()+" mutation", func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			revision := "B"
			fake := newFakePartFamilyClient(inventree.PartDetail{PK: 10, Revision: &revision}, inventree.PartDetail{PK: 20})
			deps := partFamilyDeps(fake)
			input := UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true}
			_, preview, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			fake.updateErr = sentinel
			input.DryRun = false
			input.Confirm = true
			input.PlanHash = preview.PlanHash
			_, _, err = updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
			r.ErrorIs(err, sentinel)
		})
		t.Run(sentinel.Error()+" readback", func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			revision := "B"
			fake := newFakePartFamilyClient(inventree.PartDetail{PK: 10, Revision: &revision}, inventree.PartDetail{PK: 20})
			deps := partFamilyDeps(fake)
			input := UpdatePartFamilyRelationshipsInput{ID: 10, RevisionOfID: dvgoutils.Ptr(20), DryRun: true}
			_, preview, err := updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			fake.readErrAfterUpdate = sentinel
			input.DryRun = false
			input.Confirm = true
			input.PlanHash = preview.PlanHash
			_, _, err = updatePartFamilyRelationships(deps)(ctx, &mcp.CallToolRequest{}, input)
			r.ErrorIs(err, sentinel)
		})
	}
}

func TestPartFamilyPlanStoreBindsPrincipalExpiryAndSupersession(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	tokens := []string{"one", "two", "three"}
	store := newPartFamilyPlanStore(func() time.Time { return now }, func() (string, error) {
		token := tokens[0]
		tokens = tokens[1:]
		return token, nil
	})
	principal := "first"
	store.principal = func(context.Context) string { return principal }
	plan := PartFamilyPlan{Before: PartFamilyState{PartID: 10}, After: PartFamilyState{PartID: 10, RevisionOf: dvgoutils.Ptr(20)}}

	first, err := store.issue(context.Background(), plan)
	r.NoError(err)
	second, err := store.issue(context.Background(), plan)
	r.NoError(err)
	a.False(store.consume(context.Background(), first, plan), "a newer plan must supersede the older token")
	principal = "second"
	a.False(store.consume(context.Background(), second, plan), "tokens must be principal-bound")
	principal = "first"
	a.True(store.consume(context.Background(), second, plan))
	a.False(store.consume(context.Background(), second, plan), "tokens must be single-use")

	third, err := store.issue(context.Background(), plan)
	r.NoError(err)
	now = now.Add(partFamilyPlanLifetime)
	a.False(store.consume(context.Background(), third, plan), "tokens expire at the lifetime boundary")
}

func TestPartFamilyToolAuthorizationIsDestructiveOperational(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	auth := ToolAuthorizations[UpdatePartFamilyRelationshipsToolName]
	a.Equal("destructive", auth.MutationClass)
	a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational, ScopeInventreeDestructive}, auth.Scopes)
	a.True(auth.Annotations.Destructive)
	a.False(auth.Annotations.Idempotent)
	a.False(auth.Annotations.OpenWorld)
}

func TestPartFamilyToolMCPWireContract(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	revision := "B"
	fake := newFakePartFamilyClient(inventree.PartDetail{PK: 10, Revision: &revision}, inventree.PartDetail{PK: 20})
	session, closeSession := plannedChangesSession(t, ctx, fake)
	defer closeSession()

	listed, err := session.ListTools(ctx, nil)
	r.NoError(err)
	found := false
	for _, tool := range listed.Tools {
		if tool.Name != UpdatePartFamilyRelationshipsToolName {
			continue
		}
		found = true
		inputProperties := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		a.Contains(inputProperties, "revision_of_id")
		a.Contains(inputProperties, "clear_variant_of")
		outputProperties := tool.OutputSchema.(map[string]any)["properties"].(map[string]any)
		for _, property := range []string{"plan", "record", "recovery", "validation", "clarification", "recovered"} {
			a.Contains(outputProperties, property)
		}
		r.NotNil(tool.Annotations)
		r.NotNil(tool.Annotations.DestructiveHint)
		a.True(*tool.Annotations.DestructiveHint)
		a.False(tool.Annotations.IdempotentHint)
		meta, marshalErr := json.Marshal(tool.Meta)
		r.NoError(marshalErr)
		for _, scope := range []string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational, ScopeInventreeDestructive} {
			a.Contains(string(meta), scope)
		}
	}
	r.True(found)

	plannedResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: UpdatePartFamilyRelationshipsToolName, Arguments: map[string]any{"id": 10, "revision_of_id": 20, "dry_run": true}})
	r.NoError(err)
	a.False(plannedResult.IsError)
	planned := plannedResult.StructuredContent.(map[string]any)
	a.Equal(StatusOK, planned["status"])
	a.NotEmpty(planned["plan_hash"])
	a.Contains(planned, "plan")

	clarificationResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: UpdatePartFamilyRelationshipsToolName, Arguments: map[string]any{"id": 10, "revision_of_id": 404, "dry_run": true}})
	r.NoError(err)
	a.False(clarificationResult.IsError)
	clarification := clarificationResult.StructuredContent.(map[string]any)
	a.Equal(StatusClarificationRequired, clarification["status"])
	a.Equal("revision_of_id", clarification["clarification"].(map[string]any)["field"])
}

type fakePartFamilyClient struct {
	records            map[int]inventree.PartDetail
	updates            []inventree.PatchFields
	updateErr          error
	skipApply          bool
	updated            bool
	readErrAfterUpdate error
}

func newFakePartFamilyClient(records ...inventree.PartDetail) *fakePartFamilyClient {
	fake := &fakePartFamilyClient{records: map[int]inventree.PartDetail{}}
	for _, record := range records {
		fake.records[record.PK] = record
	}
	return fake
}

func (f *fakePartFamilyClient) GetPartDetail(_ context.Context, id int) (inventree.PartDetail, error) {
	if f.updated && f.readErrAfterUpdate != nil {
		return inventree.PartDetail{}, f.readErrAfterUpdate
	}
	record, ok := f.records[id]
	if !ok {
		return inventree.PartDetail{}, &inventree.APIError{StatusCode: 404}
	}
	return record, nil
}

func (f *fakePartFamilyClient) UpdatePart(_ context.Context, id int, fields inventree.PatchFields) (inventree.Part, error) {
	f.updates = append(f.updates, fields)
	f.updated = true
	record := f.records[id]
	if f.skipApply {
		return inventree.Part{PK: id}, f.updateErr
	}
	if field, ok := fields["revision_of"]; ok {
		if field.Value() == nil {
			record.RevisionOf = nil
		} else {
			value := field.Value().(int)
			record.RevisionOf = &value
		}
	}
	if field, ok := fields["variant_of"]; ok {
		if field.Value() == nil {
			record.VariantOf = nil
		} else {
			value := field.Value().(int)
			record.VariantOf = &value
		}
	}
	f.records[id] = record
	return inventree.Part{PK: id, VariantOf: record.VariantOf}, f.updateErr
}

func partFamilyDeps(fake PartFamilyClient) Dependencies {
	return Dependencies{
		ClientFromContext:   func(context.Context) (any, error) { return fake, nil },
		partFamilyPlanStore: newPartFamilyPlanStore(time.Now, randomStockPlanToken),
	}
}

func topologyIDs(nodes []PartFamilyTopologyNode) []int {
	ids := make([]int, len(nodes))
	for index, node := range nodes {
		ids[index] = node.PartID
	}
	return ids
}
