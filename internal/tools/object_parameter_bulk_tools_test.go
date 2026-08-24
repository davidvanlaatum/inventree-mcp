package tools

import (
	"context"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testObjectParameterBulkDeps(client *fakeObjectParameterClient) Dependencies {
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }}
	deps.objectParameterBulkPlanStore = mustBulkStore(batch.Options[objectParameterBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p objectParameterBulkPlan) string { return p.supersedeKey() },
	})
	return deps
}

func TestBulkUpdateObjectParametersDryRunThenConfirmCreates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	items := []BulkUpdateObjectParameterItem{{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("10mm")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, dryOut.Status)
	r.Len(dryOut.Items, 1)
	a.Equal(objectParameterBulkOutcomePlanned, dryOut.Items[0].Outcome)
	a.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, confirmOut.Status)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("10mm", confirmOut.Items[0].Record.Value)
	a.Equal(1, client.createCalls)
}

func TestBulkUpdateObjectParametersOverwriteExistingUpdatesInPlace(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	client.parameters[100] = inventree.Parameter{PK: 100, Template: 1, ModelType: "stock.stocklocation", ModelID: 5, Data: "old"}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	items := []BulkUpdateObjectParameterItem{{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("new"), OverwriteExisting: true}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, confirmOut.Status)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("new", confirmOut.Items[0].Record.Value)
	a.Equal(1, client.updateCalls)
	a.Equal(0, client.createCalls)
}

func TestBulkUpdateObjectParametersRejectsExistingDifferingValueWithoutOverwrite(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	client.parameters[100] = inventree.Parameter{PK: 100, Template: 1, ModelType: "stock.stocklocation", ModelID: 5, Data: "old"}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	items := []BulkUpdateObjectParameterItem{{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("new")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 1)
	a.Equal(objectParameterBulkOutcomeFailed, dryOut.Items[0].Outcome)
	a.Contains(dryOut.Items[0].Message, "overwrite_existing")
}

func TestBulkUpdateObjectParametersSkipsAlreadyAtTargetState(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	client.parameters[100] = inventree.Parameter{PK: 100, Template: 1, ModelType: "stock.stocklocation", ModelID: 5, Data: "same"}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	items := []BulkUpdateObjectParameterItem{{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("same")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeSkipped), confirmOut.Items[0].Outcome)
	a.Equal(0, client.updateCalls)
	a.Equal(0, client.createCalls)
}

func TestBulkUpdateObjectParametersRejectsDuplicateKeyWithinBatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	items := []BulkUpdateObjectParameterItem{
		{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("a")},
		{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("b")},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 2)
	a.Equal(objectParameterBulkOutcomeFailed, dryOut.Items[0].Outcome)
	a.Equal(objectParameterBulkOutcomeFailed, dryOut.Items[1].Outcome)
	a.Equal(objectParameterBulkReasonDuplicateKey, dryOut.Items[0].Message)
}

func TestBulkUpdateObjectParametersRejectsCrossItemGlobalUniquenessConflict(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Serial", Enabled: true, Unique: inventree.ParameterUniquenessGlobal}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	// Two items with different targets but the same globally-unique
	// template+value would each build a clean plan independently (neither
	// row exists yet), then race each other in batch.Execute's concurrent
	// Mutate calls unless plan-build rejects the pair up front.
	items := []BulkUpdateObjectParameterItem{
		{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("dup-value")},
		{ModelType: "company.company", ModelID: 6, TemplateID: 1, Value: dvgoutils.Ptr("dup-value")},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 2)
	a.Equal(objectParameterBulkOutcomeFailed, dryOut.Items[0].Outcome)
	a.Equal(objectParameterBulkOutcomeFailed, dryOut.Items[1].Outcome)
	a.Equal(objectParameterBulkReasonUniquenessConflictInBatch, dryOut.Items[0].Message)
	a.Equal(objectParameterBulkReasonUniquenessConflictInBatch, dryOut.Items[1].Message)
}

func TestBulkUpdateObjectParametersModelTypeUniquenessAllowsSameValueAcrossModelTypes(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Bin", Enabled: true, Unique: inventree.ParameterUniquenessModelType}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	// Model-type-scoped uniqueness only conflicts within the same
	// model_type: two different stock.stocklocation targets sharing a value
	// conflict, but a company.company target with the same value does not.
	items := []BulkUpdateObjectParameterItem{
		{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("bin-a")},
		{ModelType: "stock.stocklocation", ModelID: 6, TemplateID: 1, Value: dvgoutils.Ptr("bin-a")},
		{ModelType: "company.company", ModelID: 7, TemplateID: 1, Value: dvgoutils.Ptr("bin-a")},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 3)
	a.Equal(objectParameterBulkOutcomeFailed, dryOut.Items[0].Outcome)
	a.Equal(objectParameterBulkOutcomeFailed, dryOut.Items[1].Outcome)
	a.Equal(objectParameterBulkOutcomePlanned, dryOut.Items[2].Outcome)
}

func TestBulkUpdateObjectParametersMixedOutcomesIncompatibleTemplateWithHealthyItem(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	restricted := "company.company"
	client.templates[2] = inventree.ParameterTemplate{PK: 2, Name: "TaxCode", Enabled: true, ModelType: &restricted}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	items := []BulkUpdateObjectParameterItem{
		{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("ok")},
		// template 2 is restricted to company.company, not stock.stocklocation.
		{ModelType: "stock.stocklocation", ModelID: 6, TemplateID: 2, Value: dvgoutils.Ptr("incompatible")},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 2)
	a.Equal(objectParameterBulkOutcomePlanned, dryOut.Items[0].Outcome)
	a.Equal(5, dryOut.Items[0].ModelID)
	a.Equal(objectParameterBulkOutcomeFailed, dryOut.Items[1].Outcome)
	a.Equal(6, dryOut.Items[1].ModelID)
	a.Equal(2, dryOut.Items[1].TemplateID)
	a.Contains(dryOut.Items[1].Message, "must be enabled and unrestricted or restricted to")

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, confirmOut.Status)
	r.Len(confirmOut.Items, 2)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	a.Equal(string(batch.OutcomeFailed), confirmOut.Items[1].Outcome)
}

func TestBulkUpdateObjectParametersRejectsUnsupportedModelType(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	items := []BulkUpdateObjectParameterItem{{ModelType: "part.part", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("x")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 1)
	a.Equal(objectParameterBulkOutcomeFailed, dryOut.Items[0].Outcome)
}

func TestBulkUpdateObjectParametersRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	client.parameters[100] = inventree.Parameter{PK: 100, Template: 1, ModelType: "stock.stocklocation", ModelID: 5, Data: "old"}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	items := []BulkUpdateObjectParameterItem{{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("new"), OverwriteExisting: true}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)

	client.parameters[100] = inventree.Parameter{PK: 100, Template: 1, ModelType: "stock.stocklocation", ModelID: 5, Data: "someone-else-changed-it"}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.updateCalls)
}

func TestBulkUpdateObjectParametersMixedOutcomesAreIndependent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	items := []BulkUpdateObjectParameterItem{
		{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("ok")},
		{ModelType: "stock.stocklocation", ModelID: 6, TemplateID: 999, Value: dvgoutils.Ptr("missing-template")},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)
	r.Len(dryOut.Items, 2)
	a.Equal(objectParameterBulkOutcomePlanned, dryOut.Items[0].Outcome)
	a.Equal(objectParameterBulkOutcomeFailed, dryOut.Items[1].Outcome)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, confirmOut.Status)
	r.Len(confirmOut.Items, 2)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	a.Equal(string(batch.OutcomeFailed), confirmOut.Items[1].Outcome)
}

func TestBulkUpdateObjectParametersRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	client.parameters[100] = inventree.Parameter{PK: 100, Template: 1, ModelType: "stock.stocklocation", ModelID: 5, Data: "old"}
	client.updateErr = map[int]error{100: &inventree.APIError{StatusCode: 500}}
	client.applyBeforeErr = map[int]bool{100: true}
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	items := []BulkUpdateObjectParameterItem{{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("new"), OverwriteExisting: true}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal("new", confirmOut.Items[0].Record.Value)
}

func TestBulkUpdateObjectParametersRejectsEmptyAndOversizedBatches(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	deps := testObjectParameterBulkDeps(client)
	handler := bulkUpdateObjectParameters(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)

	items := make([]BulkUpdateObjectParameterItem, bulkUpdateMaxItems+1)
	for i := range items {
		items[i] = BulkUpdateObjectParameterItem{ModelType: "stock.stocklocation", ModelID: i + 1, TemplateID: 1, Value: dvgoutils.Ptr("x")}
	}
	_, out, err = handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: items})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestBulkUpdateObjectParametersRejectsWhenOutstandingPlanCapacityIsExceeded(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	deps := testObjectParameterBulkDeps(client)
	deps.objectParameterBulkPlanStore = mustBulkStore(batch.Options[objectParameterBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey:           func(p objectParameterBulkPlan) string { return p.supersedeKey() },
		MaxEntriesPerPrincipal: 1,
	})
	handler := bulkUpdateObjectParameters(deps)

	_, first, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: []BulkUpdateObjectParameterItem{{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("a")}}, DryRun: true})
	r.NoError(err)
	a.NotEmpty(first.PlanHash)

	_, second, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateObjectParametersInput{Items: []BulkUpdateObjectParameterItem{{ModelType: "stock.stocklocation", ModelID: 6, TemplateID: 1, Value: dvgoutils.Ptr("b")}}, DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, second.Status)
	a.Empty(second.PlanHash)
}

func TestObjectParameterBulkAdapterPreflightDetectsAmbiguousExistingRows(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newFakeObjectParameterClient()
	client.templates[1] = inventree.ParameterTemplate{PK: 1, Name: "Length", Enabled: true}
	client.parameters[100] = inventree.Parameter{PK: 100, Template: 1, ModelType: "stock.stocklocation", ModelID: 5, Data: "one"}

	item := buildObjectParameterBulkPlanItem(ctx, client, BulkUpdateObjectParameterItem{ModelType: "stock.stocklocation", ModelID: 5, TemplateID: 1, Value: dvgoutils.Ptr("one")})
	r.Empty(item.FailReason)

	// A second row for the same model_type/model_id/template_id appears
	// between plan-build and confirm.
	client.parameters[101] = inventree.Parameter{PK: 101, Template: 1, ModelType: "stock.stocklocation", ModelID: 5, Data: "two"}
	adapter := &objectParameterBulkAdapter{client: client}
	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Equal(objectParameterBulkReasonAmbiguous, reason)
	r.Error(err)
}
