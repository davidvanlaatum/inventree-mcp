package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

func TestBulkPropagatePartParametersPlansAndExecutesVerifiedCreatesAndUpdates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	fake.parts[1] = inventree.Part{PK: 1, Name: "missing", Category: dvgoutils.Ptr(20)}
	fake.parts[2] = inventree.Part{PK: 2, Name: "different", Category: dvgoutils.Ptr(20)}
	fake.parts[3] = inventree.Part{PK: 3, Name: "same", Category: dvgoutils.Ptr(20)}
	fake.rows[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "part.part", ModelID: 2, Data: "old"}
	fake.rows[82] = inventree.Parameter{PK: 82, Template: 70, ModelType: "part.part", ModelID: 3, Data: "new"}
	deps := parameterBulkDeps(fake, "plan-1")
	value := "new"
	input := BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &value, PartIDs: []int{3, 1, 2}, OverwriteExisting: true}

	_, dryRun, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, dryRun.Status)
	a.Equal("plan-1", dryRun.PlanHash)
	r.NotNil(dryRun.Plan)
	r.Len(dryRun.Plan.Actions, 3)
	a.Equal([]int{1, 2, 3}, dryRun.Plan.PartIDs)
	a.Equal("create", dryRun.Plan.Actions[0].Action)
	a.Equal("update", dryRun.Plan.Actions[1].Action)
	a.Equal("skipped", dryRun.Plan.Actions[2].Status)
	a.Zero(fake.createCalls)
	a.Zero(fake.updateCalls)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = dryRun.PlanHash
	_, executed, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.Equal(2, executed.Applied)
	a.Equal(1, executed.Skipped)
	a.Zero(executed.Failed)
	a.Zero(executed.ManualRequired)
	a.Equal(1, fake.createCalls)
	a.Equal(1, fake.updateCalls)
	for _, action := range executed.Plan.Actions {
		if action.Status == "applied" {
			a.True(action.Verified)
		}
	}

	_, replay, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, replay.Status)
}

func TestBulkPropagatePartParametersKeepsDifferingAndDuplicateRowsManual(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	fake.parts[1] = inventree.Part{PK: 1, Name: "different", Category: dvgoutils.Ptr(20)}
	fake.parts[2] = inventree.Part{PK: 2, Name: "duplicate", Category: dvgoutils.Ptr(20)}
	fake.rows[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "part.part", ModelID: 1, Data: "old"}
	fake.rows[82] = inventree.Parameter{PK: 82, Template: 70, ModelType: "part.part", ModelID: 2, Data: "a"}
	fake.rows[83] = inventree.Parameter{PK: 83, Template: 70, ModelType: "part.part", ModelID: 2, Data: "b"}
	value := "new"
	_, output, err := bulkPropagatePartParameters(parameterBulkDeps(fake, "unused"))(ctx, &mcp.CallToolRequest{}, BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &value, PartIDs: []int{1, 2}})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Empty(output.PlanHash)
	a.Equal(2, output.ManualRequired)
	a.Contains(output.Plan.Actions[0].Reason, "overwrite_existing is false")
	a.Contains(output.Plan.Actions[1].Reason, "multiple existing rows")
	a.Zero(fake.createCalls)
	a.Zero(fake.updateCalls)
}

func TestBulkPropagatePartParametersRejectsUnsafeSelectorsAndChangedPlans(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	fake.parts[1] = inventree.Part{PK: 1, Name: "part", Category: dvgoutils.Ptr(20)}
	deps := parameterBulkDeps(fake, "plan-1")
	value := "new"

	for _, input := range []BulkPropagatePartParametersInput{
		{DryRun: true, Value: &value, PartIDs: []int{1}},
		{DryRun: true, TemplateID: 70, PartIDs: []int{1}},
		{DryRun: true, TemplateID: 70, Value: &value},
		{DryRun: true, TemplateID: 70, Value: &value, PartIDs: []int{1}, CategoryID: 20},
		{DryRun: true, TemplateID: 70, Value: &value, PartIDs: []int{1}, IncludeSubcategories: true},
	} {
		_, output, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusClarificationRequired, output.Status)
	}
	tooLong := strings.Repeat("x", 501)
	_, boundedValue, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &tooLong, PartIDs: []int{1}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, boundedValue.Status)
	a.Equal("value", boundedValue.Clarification.Field)
	for _, accepted := range []string{"", strings.Repeat("x", 500)} {
		accepted := accepted
		_, boundary, err := bulkPropagatePartParameters(parameterBulkDeps(fake, "boundary"))(ctx, &mcp.CallToolRequest{}, BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &accepted, PartIDs: []int{1}})
		r.NoError(err)
		a.Equal(StatusOK, boundary.Status)
		a.Equal(accepted, boundary.Plan.Value)
	}

	input := BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &value, PartIDs: []int{1}}
	_, dryRun, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	r.NotEmpty(dryRun.PlanHash)
	fake.rows[99] = inventree.Parameter{PK: 99, Template: 70, ModelType: "part.part", ModelID: 1, Data: "someone-else"}
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = dryRun.PlanHash
	_, changed, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, changed.Status)
	a.Zero(fake.createCalls)
	a.Zero(fake.updateCalls)
}

func TestBulkPropagatePartParametersSendsExplicitCategoryCascadeAndCachesLinks(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	fake.categories[21] = inventree.Category{PK: 21, Name: "Child"}
	fake.parts[1] = inventree.Part{PK: 1, Name: "parent", Category: dvgoutils.Ptr(20)}
	fake.parts[2] = inventree.Part{PK: 2, Name: "child", Category: dvgoutils.Ptr(21)}
	value := "new"

	_, exact, err := bulkPropagatePartParameters(parameterBulkDeps(fake, "exact"))(ctx, &mcp.CallToolRequest{}, BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &value, CategoryID: 20})
	r.NoError(err)
	r.NotNil(fake.lastPartQuery.Cascade)
	a.False(*fake.lastPartQuery.Cascade)
	r.Len(exact.Plan.Actions, 1)
	a.Equal(1, exact.Plan.Actions[0].PartID)
	a.Equal(1, fake.linkSearchCalls)

	fake.linkSearchCalls = 0
	_, descendants, err := bulkPropagatePartParameters(parameterBulkDeps(fake, "descendants"))(ctx, &mcp.CallToolRequest{}, BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &value, CategoryID: 20, IncludeSubcategories: true})
	r.NoError(err)
	r.NotNil(fake.lastPartQuery.Cascade)
	a.True(*fake.lastPartQuery.Cascade)
	r.Len(descendants.Plan.Actions, 2)
	a.Equal(2, fake.linkSearchCalls, "one effective-link scan per distinct category")
}

func TestBulkPropagatePartParametersRejectsMismatchedUpdateIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	fake.parts[1] = inventree.Part{PK: 1, Name: "part", Category: dvgoutils.Ptr(20)}
	fake.rows[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "part.part", ModelID: 1, Data: "old"}
	fake.updateResponsePK = 999
	value := "new"
	deps := parameterBulkDeps(fake, "plan")
	input := BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &value, PartIDs: []int{1}, OverwriteExisting: true}
	_, dryRun, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = dryRun.PlanHash
	_, executed, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, executed.Status)
	a.Equal(81, executed.Plan.Actions[0].ParameterID)
	a.Contains(executed.Plan.Actions[0].Reason, "unexpected parameter ID")
}

func TestBulkPropagatePartParametersStopsWhenLaterActionDrifts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	fake.parts[1] = inventree.Part{PK: 1, Name: "create-first", Category: dvgoutils.Ptr(20)}
	fake.parts[2] = inventree.Part{PK: 2, Name: "update-second", Category: dvgoutils.Ptr(20)}
	fake.rows[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "part.part", ModelID: 2, Data: "old"}
	fake.afterCreate = func() {
		drifted := fake.rows[81]
		drifted.Data = "concurrent"
		fake.rows[81] = drifted
	}
	value := "new"
	deps := parameterBulkDeps(fake, "plan")
	input := BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &value, PartIDs: []int{1, 2}, OverwriteExisting: true}
	_, dryRun, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = dryRun.PlanHash
	_, executed, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, executed.Status)
	a.Equal("applied", executed.Plan.Actions[0].Status)
	a.Equal("failed", executed.Plan.Actions[1].Status)
	a.Contains(executed.Plan.Actions[1].Reason, "drifted after review")
	a.Zero(fake.updateCalls)
}

func TestBulkPropagatePartParametersStopsCreateWhenRowAppearsAfterConfirmationPreflight(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	fake.parts[1] = inventree.Part{PK: 1, Name: "part", Category: dvgoutils.Ptr(20)}
	fake.createDriftAtSearch = 3
	value := "new"
	deps := parameterBulkDeps(fake, "plan")
	input := BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &value, PartIDs: []int{1}}
	_, dryRun, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = dryRun.PlanHash
	_, executed, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, executed.Status)
	a.Contains(executed.Plan.Actions[0].Reason, "drifted after review")
	a.Zero(fake.createCalls)
}

func TestExecuteBulkParameterPlanFailsClosedAtWriteVerificationBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configure  func(*parameterBulkFake)
		wantReason string
	}{
		{name: "missing stable id", configure: func(fake *parameterBulkFake) { fake.createResponsePK = dvgoutils.Ptr(0) }, wantReason: "no stable parameter ID"},
		{name: "read back error", configure: func(fake *parameterBulkFake) { fake.getParameterErrAt = 1 }, wantReason: "read-back failed"},
		{name: "read back mismatch", configure: func(fake *parameterBulkFake) { fake.readbackMismatch = true }, wantReason: "did not match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, handler, _ := testhandler.SetupTestHandler(t)
			fake := newParameterBulkFake()
			tt.configure(fake)
			out := BulkParameterOutput{Plan: &BulkParameterPlan{Actions: []BulkParameterAction{{PartID: 1, TemplateID: 70, Action: "create", Status: "planned", ProposedValue: "new"}}}}
			executeBulkParameterPlan(ctx, fake, &out)
			r.Len(out.Failures, 1)
			a.Equal("failed", out.Plan.Actions[0].Status)
			a.Contains(out.Plan.Actions[0].Reason, tt.wantReason)
			a.NotContains(out.Plan.Actions[0].Reason, "secret")
			a.NotContains(out.Failures[0].Message, "secret")
			a.False(out.Plan.Actions[0].Verified)
			for _, record := range handler.Logs() {
				a.NotContains(record.String(), "secret")
			}
		})
	}
}

func TestBulkPropagatePartParametersBoundsCategoryPlansAndStopsAfterFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	value := "new"
	for id := 1; id <= parameterBulkPartLimit+1; id++ {
		fake.parts[id] = inventree.Part{PK: id, Name: fmt.Sprintf("part-%d", id), Category: dvgoutils.Ptr(20)}
	}
	_, bounded, err := bulkPropagatePartParameters(parameterBulkDeps(fake, "plan-1"))(ctx, &mcp.CallToolRequest{}, BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &value, CategoryID: 20})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, bounded.Status)

	fake = newParameterBulkFake()
	fake.parts[1] = inventree.Part{PK: 1, Name: "first", Category: dvgoutils.Ptr(20)}
	fake.parts[2] = inventree.Part{PK: 2, Name: "second", Category: dvgoutils.Ptr(20)}
	fake.createErrAt = 1
	deps := parameterBulkDeps(fake, "plan-2")
	input := BulkPropagatePartParametersInput{DryRun: true, TemplateID: 70, Value: &value, PartIDs: []int{1, 2}}
	_, dryRun, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = dryRun.PlanHash
	_, executed, err := bulkPropagatePartParameters(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, executed.Status)
	a.Equal(1, executed.Failed)
	a.Equal(1, executed.ManualRequired)
	a.Equal(1, fake.createCalls)
	a.Contains(executed.Plan.Actions[1].Reason, "not attempted")
}

func TestAuditParameterConsistencyReportsRepresentativeNoWriteFindings(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	ohm := "ohm"
	volt := "V"
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: " Resistance ", Units: &ohm, Enabled: true}
	fake.templates[71] = inventree.ParameterTemplate{PK: 71, Name: "resistance", Units: &volt, Enabled: true}
	fake.templates[72] = inventree.ParameterTemplate{PK: 72, Name: "Tolerance", Enabled: true}
	fake.parts[1] = inventree.Part{PK: 1, Name: "part", Category: dvgoutils.Ptr(20)}
	fake.rows[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "part.part", ModelID: 1, Data: "22k"}
	fake.rows[82] = inventree.Parameter{PK: 82, Template: 70, ModelType: "part.part", ModelID: 1, Data: "33k"}
	fake.rows[83] = inventree.Parameter{PK: 83, Template: 71, ModelType: "part.part", ModelID: 1, Data: "x"}
	fake.rows[84] = inventree.Parameter{PK: 84, Template: 72, ModelType: "part.part", ModelID: 1, Data: "5%"}
	fake.links = []inventree.CategoryParameterTemplate{{PK: 90, Category: 20, Template: 70, DefaultValue: "10k"}}

	_, output, err := auditParameterConsistency(parameterBulkDeps(fake, "unused"))(ctx, &mcp.CallToolRequest{}, AuditParameterConsistencyInput{})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	kinds := make([]string, 0, len(output.Findings))
	for _, finding := range output.Findings {
		kinds = append(kinds, finding.Kind)
	}
	for _, kind := range []string{"duplicate_template_name", "incompatible_template_definition", "duplicate_parameter_row", "overloaded_parameter_name", "unlinked_parameter", "category_default_mismatch"} {
		a.Contains(kinds, kind)
	}
	a.Zero(fake.createCalls)
	a.Zero(fake.updateCalls)
}

func TestAuditParameterConsistencyValidatesStableFiltersBeforeScanning(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	_, category, err := auditParameterConsistency(parameterBulkDeps(fake, "unused"))(ctx, &mcp.CallToolRequest{}, AuditParameterConsistencyInput{CategoryID: 999})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, category.Status)
	a.Equal("category_id", category.Clarification.Field)
	_, template, err := auditParameterConsistency(parameterBulkDeps(fake, "unused"))(ctx, &mcp.CallToolRequest{}, AuditParameterConsistencyInput{TemplateID: 999})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, template.Status)
	a.Equal("template_id", template.Clarification.Field)
}

func TestAuditParameterConsistencyTemplateFilterIncludesNormalizedPeers(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	ohm, volt := "ohm", "V"
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: " Resistance ", Units: &ohm, Enabled: true}
	fake.templates[71] = inventree.ParameterTemplate{PK: 71, Name: "resistance", Units: &volt, Enabled: true}
	fake.parts[1] = inventree.Part{PK: 1, Name: "part", Category: dvgoutils.Ptr(20)}
	fake.rows[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "part.part", ModelID: 1, Data: "a"}
	fake.rows[82] = inventree.Parameter{PK: 82, Template: 71, ModelType: "part.part", ModelID: 1, Data: "b"}

	_, output, err := auditParameterConsistency(parameterBulkDeps(fake, "unused"))(ctx, &mcp.CallToolRequest{}, AuditParameterConsistencyInput{TemplateID: 70})
	r.NoError(err)
	kinds := make([]string, 0, len(output.Findings))
	for _, finding := range output.Findings {
		kinds = append(kinds, finding.Kind)
	}
	a.Contains(kinds, "duplicate_template_name")
	a.Contains(kinds, "incompatible_template_definition")
	a.Contains(kinds, "overloaded_parameter_name")
}

func TestAuditParameterConsistencyCategoryFilterAvoidsUnrelatedGlobalRows(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	fake.parts[1] = inventree.Part{PK: 1, Name: "selected", Category: dvgoutils.Ptr(20)}
	fake.rows[1] = inventree.Parameter{PK: 1, Template: 70, ModelType: "part.part", ModelID: 1, Data: "selected"}
	for id := 2; id <= parameterAuditScanLimit+2; id++ {
		fake.rows[id] = inventree.Parameter{PK: id, Template: 70, ModelType: "part.part", ModelID: 10_000 + id, Data: "unrelated"}
	}

	_, output, err := auditParameterConsistency(parameterBulkDeps(fake, "unused"))(ctx, &mcp.CallToolRequest{}, AuditParameterConsistencyInput{CategoryID: 20})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(1, output.RowsRead)
}

func TestAuditParameterConsistencyCombinedFiltersNarrowRowsServerSide(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	fake.templates[72] = inventree.ParameterTemplate{PK: 72, Name: "Unrelated", Enabled: true}
	fake.parts[1] = inventree.Part{PK: 1, Name: "selected", Category: dvgoutils.Ptr(20)}
	fake.rows[1] = inventree.Parameter{PK: 1, Template: 70, ModelType: "part.part", ModelID: 1, Data: "selected"}
	for id := 2; id <= parameterAuditScanLimit+2; id++ {
		fake.rows[id] = inventree.Parameter{PK: id, Template: 72, ModelType: "part.part", ModelID: 1, Data: "unrelated"}
	}

	_, output, err := auditParameterConsistency(parameterBulkDeps(fake, "unused"))(ctx, &mcp.CallToolRequest{}, AuditParameterConsistencyInput{CategoryID: 20, TemplateID: 70})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(1, output.RowsRead)
}

func TestAuditParameterConsistencyCategoryUsesAuditBudgetBeyondPropagationLimit(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newParameterBulkFake()
	for id := 1; id <= parameterBulkPartLimit+1; id++ {
		fake.parts[id] = inventree.Part{PK: id, Name: fmt.Sprintf("part-%d", id), Category: dvgoutils.Ptr(20)}
	}

	_, output, err := auditParameterConsistency(parameterBulkDeps(fake, "unused"))(ctx, &mcp.CallToolRequest{}, AuditParameterConsistencyInput{CategoryID: 20})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Zero(output.RowsRead)
}

func TestParameterAuditBudgetClarificationUsesSupportedNarrowing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		input      AuditParameterConsistencyInput
		wantReason string
	}{
		{name: "unfiltered", wantReason: "supply template_id or exact category_id"},
		{name: "template", input: AuditParameterConsistencyInput{TemplateID: 70}, wantReason: "add one exact category_id"},
		{name: "category", input: AuditParameterConsistencyInput{CategoryID: 20}, wantReason: "add one existing template_id"},
		{name: "combined", input: AuditParameterConsistencyInput{TemplateID: 70, CategoryID: 20}, wantReason: "exposes no narrower slice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			_, output, err := parameterAuditResultForError(tt.input, errParameterAuditScanLimit)
			r.NoError(err)
			r.NotNil(output.Clarification)
			a.Contains(output.Clarification.Reason, "1,000-unit upstream request-and-record budget")
			a.Contains(output.Clarification.Reason, tt.wantReason)
		})
	}
}

func TestParameterPlanStoreBindsPrincipalExpirySupersessionAndCapacity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	tokens := []string{"token-a", "token-b", "token-c", "token-d", "token-e"}
	store := newParameterPlanStore(func() time.Time { return now }, func() (string, error) {
		token := tokens[0]
		tokens = tokens[1:]
		return token, nil
	})
	principal := "alice"
	store.principal = func(context.Context) string { return principal }
	plan := BulkParameterPlan{TemplateID: 70, PartIDs: []int{1}, Value: "new", Actions: []BulkParameterAction{{PartID: 1, TemplateID: 70, ProposedValue: "new", Action: "create", Status: "planned"}}}

	first, err := store.issue(t.Context(), plan)
	r.NoError(err)
	second, err := store.issue(t.Context(), plan)
	r.NoError(err)
	a.False(store.consume(t.Context(), first, plan), "new matching plan must supersede the old token")
	principal = "bob"
	a.False(store.consume(t.Context(), second, plan), "another principal must not consume the token")
	principal = "alice"
	a.True(store.consume(t.Context(), second, plan))
	a.False(store.consume(t.Context(), second, plan), "token must be single use")

	expiring, err := store.issue(t.Context(), plan)
	r.NoError(err)
	now = now.Add(parameterPlanLifetime)
	a.False(store.consume(t.Context(), expiring, plan), "token must expire at the lifetime boundary")

	store.maxEntries = 1
	_, err = store.issue(t.Context(), plan)
	r.NoError(err)
	other := plan
	other.PartIDs = []int{2}
	_, err = store.issue(t.Context(), other)
	a.ErrorIs(err, errParameterPlanCapacity)
}

func TestParameterBulkToolAuthorizations(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Equal([]string{ScopeInventreeRead}, ToolAuthorizations[AuditParameterConsistencyToolName].Scopes)
	a.Equal(ReadOnlyAnnotations, ToolAuthorizations[AuditParameterConsistencyToolName].Annotations)
	a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeDestructive}, ToolAuthorizations[BulkPropagatePartParametersToolName].Scopes)
	a.True(ToolAuthorizations[BulkPropagatePartParametersToolName].Annotations.Destructive)
}

type parameterBulkFake struct {
	templates            map[int]inventree.ParameterTemplate
	parts                map[int]inventree.Part
	categories           map[int]inventree.Category
	rows                 map[int]inventree.Parameter
	links                []inventree.CategoryParameterTemplate
	nextRowID            int
	createCalls          int
	updateCalls          int
	createErrAt          int
	updateResponsePK     int
	lastPartQuery        inventree.PartQuery
	linkSearchCalls      int
	afterCreate          func()
	searchParameterCalls int
	createDriftAtSearch  int
	createResponsePK     *int
	getParameterCalls    int
	getParameterErrAt    int
	readbackMismatch     bool
}

func newParameterBulkFake() *parameterBulkFake {
	return &parameterBulkFake{
		templates:  map[int]inventree.ParameterTemplate{70: {PK: 70, Name: "Resistance", Enabled: true}},
		parts:      map[int]inventree.Part{},
		categories: map[int]inventree.Category{20: {PK: 20, Name: "Passives"}},
		rows:       map[int]inventree.Parameter{},
		links:      []inventree.CategoryParameterTemplate{{PK: 90, Category: 20, Template: 70, DefaultValue: ""}},
		nextRowID:  100,
	}
}

func (f *parameterBulkFake) GetParameterTemplate(_ context.Context, id int) (inventree.ParameterTemplate, error) {
	record, ok := f.templates[id]
	if !ok {
		return inventree.ParameterTemplate{}, fakeNotFound("template")
	}
	return record, nil
}

func (f *parameterBulkFake) SearchParameterTemplatesPage(_ context.Context, query inventree.SearchQuery) (inventree.Page[inventree.ParameterTemplate], error) {
	records := make([]inventree.ParameterTemplate, 0, len(f.templates))
	for _, record := range f.templates {
		if query.Search != "" && !strings.Contains(normalizedParameterName(record.Name), normalizedParameterName(query.Search)) {
			continue
		}
		records = append(records, record)
	}
	slices.SortFunc(records, func(a, b inventree.ParameterTemplate) int { return a.PK - b.PK })
	return pageSlice(records, query.Offset, query.Limit), nil
}

func (f *parameterBulkFake) SearchTemplateParametersPage(_ context.Context, query inventree.TemplateParameterQuery) (inventree.PartParameterPage, error) {
	records := f.filteredRows(0, query.TemplateID)
	page := pageSlice(records, query.Offset, query.Limit)
	return inventree.PartParameterPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil}, nil
}

func (f *parameterBulkFake) SearchCategoryParameterTemplatesPage(_ context.Context, query inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error) {
	f.linkSearchCalls++
	records := make([]inventree.CategoryParameterTemplate, 0, len(f.links))
	for _, record := range f.links {
		if query.CategoryID == 0 || record.Category == query.CategoryID {
			records = append(records, record)
		}
	}
	return pageSlice(records, query.Offset, query.Limit), nil
}

func (f *parameterBulkFake) GetPart(_ context.Context, id int) (inventree.Part, error) {
	record, ok := f.parts[id]
	if !ok {
		return inventree.Part{}, fakeNotFound("part")
	}
	return record, nil
}

func (f *parameterBulkFake) GetPartCategory(_ context.Context, id int) (inventree.Category, error) {
	record, ok := f.categories[id]
	if !ok {
		return inventree.Category{}, fakeNotFound("category")
	}
	return record, nil
}

func (f *parameterBulkFake) SearchPartsPage(_ context.Context, query inventree.PartQuery) (inventree.PartPage, error) {
	f.lastPartQuery = query
	records := make([]inventree.Part, 0, len(f.parts))
	for _, record := range f.parts {
		if query.CategoryID == 0 || record.Category != nil && (*record.Category == query.CategoryID || query.Cascade != nil && *query.Cascade && *record.Category == 21) {
			records = append(records, record)
		}
	}
	slices.SortFunc(records, func(a, b inventree.Part) int { return a.PK - b.PK })
	page := pageSlice(records, query.Offset, query.Limit)
	return inventree.PartPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil}, nil
}

func (f *parameterBulkFake) SearchPartParametersPage(_ context.Context, query inventree.PartParameterQuery) (inventree.PartParameterPage, error) {
	f.searchParameterCalls++
	if f.createDriftAtSearch == f.searchParameterCalls {
		f.rows[999] = inventree.Parameter{PK: 999, Template: query.TemplateID, ModelType: "part.part", ModelID: query.PartID, Data: "concurrent"}
	}
	records := f.filteredRows(query.PartID, query.TemplateID)
	page := pageSlice(records, query.Offset, query.Limit)
	return inventree.PartParameterPage{Count: page.Count, Results: page.Results, HasMore: page.Next != nil}, nil
}

func (f *parameterBulkFake) GetPartParameter(_ context.Context, id int) (inventree.Parameter, error) {
	f.getParameterCalls++
	if f.getParameterErrAt == f.getParameterCalls {
		return inventree.Parameter{}, errors.New("internal endpoint token=secret")
	}
	record, ok := f.rows[id]
	if !ok {
		return inventree.Parameter{}, fakeNotFound("parameter")
	}
	if f.readbackMismatch {
		record.Data = "wrong"
	}
	return record, nil
}

func (f *parameterBulkFake) CreatePartParameter(_ context.Context, input inventree.ParameterCreate) (inventree.Parameter, error) {
	f.createCalls++
	if f.createErrAt == f.createCalls {
		return inventree.Parameter{}, errors.New("create failed")
	}
	f.nextRowID++
	record := inventree.Parameter{PK: f.nextRowID, Template: input.Template, ModelType: input.ModelType, ModelID: input.ModelID, Data: input.Data}
	f.rows[record.PK] = record
	if f.afterCreate != nil {
		f.afterCreate()
	}
	if f.createResponsePK != nil {
		record.PK = *f.createResponsePK
	}
	return record, nil
}

func (f *parameterBulkFake) UpdatePartParameter(_ context.Context, id int, fields inventree.PatchFields) (inventree.Parameter, error) {
	f.updateCalls++
	record, ok := f.rows[id]
	if !ok {
		return inventree.Parameter{}, fakeNotFound("parameter")
	}
	_, ok = fields["data"]
	if !ok {
		return inventree.Parameter{}, errors.New("missing data patch")
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return inventree.Parameter{}, err
	}
	var payload map[string]string
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return inventree.Parameter{}, err
	}
	record.Data = payload["data"]
	f.rows[id] = record
	if f.updateResponsePK > 0 {
		record.PK = f.updateResponsePK
	}
	return record, nil
}

func (f *parameterBulkFake) filteredRows(partID, templateID int) []inventree.Parameter {
	records := make([]inventree.Parameter, 0, len(f.rows))
	for _, record := range f.rows {
		if partID > 0 && record.ModelID != partID || templateID > 0 && record.Template != templateID {
			continue
		}
		records = append(records, record)
	}
	slices.SortFunc(records, func(a, b inventree.Parameter) int { return a.PK - b.PK })
	return records
}

func parameterBulkDeps(fake *parameterBulkFake, token string) Dependencies {
	store := newParameterPlanStore(time.Now, func() (string, error) { return token, nil })
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }, parameterPlanStore: store}
}

func fakeNotFound(subject string) error {
	return &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: 404, Detail: subject + " not found"}
}

func pageSlice[T any](records []T, offset, limit int) inventree.Page[T] {
	if limit <= 0 {
		limit = len(records)
	}
	if offset > len(records) {
		offset = len(records)
	}
	end := min(offset+limit, len(records))
	page := inventree.Page[T]{Count: len(records), Results: append([]T(nil), records[offset:end]...)}
	if end < len(records) {
		next := fmt.Sprintf("?offset=%d", end)
		page.Next = &next
	}
	return page
}
