package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

func TestCreateParameterTemplateRequiresExplicitFieldsAndRefusesCollision(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	deps := parameterTemplateDeps(fake)

	_, missing, err := createParameterTemplate(deps)(ctx, &mcp.CallToolRequest{}, CreateParameterTemplateInput{Name: "Resistance"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, missing.Status)

	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "resistance"}
	_, collision, err := createParameterTemplate(deps)(ctx, &mcp.CallToolRequest{}, explicitTemplateInput("Resistance"))
	r.NoError(err)
	a.Equal(StatusClarificationRequired, collision.Status)
	r.NotNil(collision.Clarification)
	a.Equal("template_id", collision.Clarification.Retry)

	delete(fake.templates, 70)
	_, created, err := createParameterTemplate(deps)(ctx, &mcp.CallToolRequest{}, explicitTemplateInput("Resistance"))
	r.NoError(err)
	a.Equal(StatusOK, created.Status)
	r.NotNil(created.Record)
	a.Equal("Resistance", created.Record.Name)
}

func TestUpdateParameterTemplatePreservesOmittedAndExplicitValues(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Choices: "10k,22k", Checkbox: true, SelectionList: dvgoutils.Ptr(9), Enabled: true}

	_, output, err := updateParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{
		TemplateID: 70, Units: dvgoutils.Ptr(""), Checkbox: dvgoutils.Ptr(false), ClearSelectionList: true,
	})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Contains(fake.lastPatch, "units")
	a.Contains(fake.lastPatch, "checkbox")
	a.Contains(fake.lastPatch, "selectionlist")
	a.NotContains(fake.lastPatch, "choices")
	a.Equal("10k,22k", output.Record.Choices)
	a.False(output.Record.Checkbox)
	a.Nil(output.Record.SelectionList)
}

func TestDeleteParameterTemplateRefusesReferencesAndVerifiesUnreferencedDelete(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance"}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "part.part", ModelID: 10, Data: "10k"}
	fake.links[90] = inventree.CategoryParameterTemplate{PK: 90, Category: 20, Template: 70}
	deps := parameterTemplateDeps(fake)

	_, blocked, err := deleteParameterTemplate(deps)(ctx, &mcp.CallToolRequest{}, DeleteParameterTemplateInput{TemplateID: 70, Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, blocked.Status)
	a.Equal([]int{80}, blocked.References.ParameterIDs)
	a.Equal([]int{90}, blocked.References.CategoryLinkIDs)
	a.False(fake.deletedTemplates[70])

	delete(fake.parameters, 80)
	delete(fake.links, 90)
	_, preview, err := deleteParameterTemplate(deps)(ctx, &mcp.CallToolRequest{}, DeleteParameterTemplateInput{TemplateID: 70})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, preview.Status)
	_, deleted, err := deleteParameterTemplate(deps)(ctx, &mcp.CallToolRequest{}, DeleteParameterTemplateInput{TemplateID: 70, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, deleted.Status)
	a.True(deleted.Verified)
	a.True(fake.deletedTemplates[70])
}

func TestMergeParameterTemplatesNormalizesOnlyMappedValuesAndSkipsConflicts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := mergeFake()
	fake.parameters[82] = inventree.Parameter{PK: 82, Template: 71, ModelType: "part.part", ModelID: 11, Data: "existing"}
	deps := parameterTemplateDeps(fake)
	input := MergeParameterTemplatesInput{SourceTemplateID: 70, TargetTemplateID: 71, ValueMap: map[string]string{"10 K": "10k"}, DryRun: true}

	_, plan, err := mergeParameterTemplates(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, plan.Status)
	r.Len(plan.Actions, 2)
	a.Equal("10k", plan.Actions[0].ToValue)
	a.Equal("planned", plan.Actions[0].Status)
	a.Equal("skipped", plan.Actions[1].Status)
	a.Contains(plan.Actions[1].Reason, "neither value will be overwritten")
	a.NotEmpty(plan.PlanHash)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = plan.PlanHash
	_, applied, err := mergeParameterTemplates(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, applied.Status)
	a.Equal("applied", applied.Actions[0].Status)
	a.Equal(71, fake.parameters[80].Template)
	a.Equal("10k", fake.parameters[80].Data)
	a.False(applied.SourceDeleted)
	a.NotEmpty(applied.ResidualDecisions)
}

func TestMergeParameterTemplatesDeletesEmptySourceAfterCurrentPlanConfirmation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := mergeFake()
	delete(fake.parameters, 81)
	deps := parameterTemplateDeps(fake)
	input := MergeParameterTemplatesInput{SourceTemplateID: 70, TargetTemplateID: 71, DryRun: true}
	_, plan, err := mergeParameterTemplates(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = plan.PlanHash
	_, applied, err := mergeParameterTemplates(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, applied.Status)
	a.True(applied.SourceDeleted)
	a.True(applied.ReadbackVerified)
	a.True(fake.deletedTemplates[70])
}

func TestParameterTemplateAdministrationClarifiesUnsafeInputs(t *testing.T) {
	t.Parallel()

	tests := map[string]func(context.Context, *fakeParameterTemplateAdminClient) string{
		"create with invalid selection list": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			input := explicitTemplateInput("Resistance")
			input.SelectionListID = dvgoutils.Ptr(0)
			_, output, err := createParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			require.NoError(t, err)
			return output.Status
		},
		"create with invalid model type": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			input := explicitTemplateInput("Resistance")
			input.ModelType = dvgoutils.Ptr("part")
			_, output, err := createParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			require.NoError(t, err)
			return output.Status
		},
		"update with invalid template": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := updateParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{})
			require.NoError(t, err)
			return output.Status
		},
		"update with contradictory selection list": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := updateParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{TemplateID: 70, SelectionListID: dvgoutils.Ptr(9), ClearSelectionList: true})
			require.NoError(t, err)
			return output.Status
		},
		"update with invalid selection list": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := updateParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{TemplateID: 70, SelectionListID: dvgoutils.Ptr(0)})
			require.NoError(t, err)
			return output.Status
		},
		"update with blank name": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := updateParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{TemplateID: 70, Name: dvgoutils.Ptr("  ")})
			require.NoError(t, err)
			return output.Status
		},
		"update with invalid model type": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := updateParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{TemplateID: 70, ModelType: dvgoutils.Ptr("part")})
			require.NoError(t, err)
			return output.Status
		},
		"update missing template": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := updateParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{TemplateID: 999, Name: dvgoutils.Ptr("new")})
			require.NoError(t, err)
			return output.Status
		},
		"update without fields": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := updateParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{TemplateID: 70})
			require.NoError(t, err)
			return output.Status
		},
		"update with colliding name": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			fake.templates[71] = inventree.ParameterTemplate{PK: 71, Name: "resistance"}
			_, output, err := updateParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{TemplateID: 70, Name: dvgoutils.Ptr("Resistance")})
			require.NoError(t, err)
			return output.Status
		},
		"delete with invalid template": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := deleteParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteParameterTemplateInput{})
			require.NoError(t, err)
			return output.Status
		},
		"delete missing template": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := deleteParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, DeleteParameterTemplateInput{TemplateID: 999})
			require.NoError(t, err)
			return output.Status
		},
		"merge with same template": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := mergeParameterTemplates(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, MergeParameterTemplatesInput{SourceTemplateID: 70, TargetTemplateID: 70})
			require.NoError(t, err)
			return output.Status
		},
		"merge without confirmation": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := mergeParameterTemplates(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, MergeParameterTemplatesInput{SourceTemplateID: 70, TargetTemplateID: 71})
			require.NoError(t, err)
			return output.Status
		},
		"merge with stale plan": func(ctx context.Context, fake *fakeParameterTemplateAdminClient) string {
			_, output, err := mergeParameterTemplates(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, MergeParameterTemplatesInput{SourceTemplateID: 70, TargetTemplateID: 71, Confirm: true, PlanHash: "stale"})
			require.NoError(t, err)
			return output.Status
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := mergeFake()
			expected := StatusClarificationRequired
			if strings.Contains(name, "missing template") {
				expected = StatusNotFound
			}
			assert.Equal(t, expected, test(ctx, fake))
		})
	}
}

func TestUpdateParameterTemplateAppliesEveryExplicitField(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance"}

	_, output, err := updateParameterTemplate(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{
		TemplateID: 70,
		Name:       dvgoutils.Ptr("Resistance precise"), Units: dvgoutils.Ptr("ohm"), Description: dvgoutils.Ptr("Electrical resistance"),
		ModelType: dvgoutils.Ptr("part.part"), Checkbox: dvgoutils.Ptr(false), Choices: dvgoutils.Ptr("10,22"),
		SelectionListID: dvgoutils.Ptr(9), Enabled: dvgoutils.Ptr(true),
	})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal("Resistance precise", output.Record.Name)
	a.Equal("ohm", *output.Record.Units)
	a.Equal("Electrical resistance", output.Record.Description)
	a.Equal("part.part", *output.Record.ModelType)
	a.Equal("10,22", output.Record.Choices)
	a.Equal(9, *output.Record.SelectionList)
	a.True(output.Record.Enabled)
}

func TestMergeParameterTemplatesReportsUnsupportedRowsAndCategoryLinks(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := mergeFake()
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "company.company", ModelID: 10, Data: "legacy"}
	fake.links[90] = inventree.CategoryParameterTemplate{PK: 90, Category: 20, Template: 70}

	_, plan, err := mergeParameterTemplates(parameterTemplateDeps(fake))(ctx, &mcp.CallToolRequest{}, MergeParameterTemplatesInput{SourceTemplateID: 70, TargetTemplateID: 71, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, plan.Status)
	a.Equal("skipped", plan.Actions[0].Status)
	a.Contains(plan.Actions[0].Reason, "unsupported model type")
	a.Equal("company.company", plan.Actions[0].ModelType)
	a.Equal(10, plan.Actions[0].ModelID)
	a.Zero(plan.Actions[0].PartID)
	a.Equal([]int{90}, plan.CategoryLinkIDs)
	a.Equal([]CategoryTemplateReference{{LinkID: 90, CategoryID: 20}}, plan.CategoryLinks)
	a.Len(plan.ResidualDecisions, 2)
}

func TestMergeParameterTemplatesRequiresFreshPlanAfterStateChange(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := mergeFake()
	deps := parameterTemplateDeps(fake)
	input := MergeParameterTemplatesInput{SourceTemplateID: 70, TargetTemplateID: 71, DryRun: true}
	_, plan, err := mergeParameterTemplates(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)

	record := fake.parameters[80]
	record.Data = "changed after planning"
	fake.parameters[80] = record
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = plan.PlanHash
	_, output, err := mergeParameterTemplates(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("plan_hash", output.Clarification.Retry)
	a.Equal(70, fake.parameters[80].Template)
}

func TestMergeParameterTemplatesReturnsSanitizedPartialFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		configure func(*fakeParameterTemplateAdminClient)
		reason    string
	}{
		"source precondition changed": {
			configure: func(fake *fakeParameterTemplateAdminClient) { fake.changeSourceOnRead = true },
			reason:    "source row changed after planning",
		},
		"upstream update failed": {
			configure: func(fake *fakeParameterTemplateAdminClient) {
				fake.updatePartParameterErr = errors.New("secret backend detail")
			},
			reason: "upstream row update failed",
		},
		"readback mismatch": {
			configure: func(fake *fakeParameterTemplateAdminClient) { fake.corruptUpdateReadback = true },
			reason:    "did not match the planned target",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := mergeFake()
			delete(fake.parameters, 81)
			deps := parameterTemplateDeps(fake)
			input := MergeParameterTemplatesInput{SourceTemplateID: 70, TargetTemplateID: 71, DryRun: true}
			_, plan, err := mergeParameterTemplates(deps)(ctx, &mcp.CallToolRequest{}, input)
			require.NoError(t, err)
			test.configure(fake)
			input.DryRun = false
			input.Confirm = true
			input.PlanHash = plan.PlanHash
			_, output, err := mergeParameterTemplates(deps)(ctx, &mcp.CallToolRequest{}, input)
			require.NoError(t, err)
			assert.Equal(t, "partial_failure", output.Status)
			require.Len(t, output.Actions, 1)
			assert.Equal(t, "failed", output.Actions[0].Status)
			assert.Contains(t, output.Actions[0].Reason, test.reason)
			assert.NotContains(t, output.Actions[0].Reason, "secret backend detail")
			assert.NotEmpty(t, output.ResidualDecisions)
		})
	}
}

func TestMergeParameterTemplatesFailsClosedOnDeleteAndConcurrentLinkChanges(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		configure func(*fakeParameterTemplateAdminClient)
		residual  string
	}{
		"delete failure": {
			configure: func(fake *fakeParameterTemplateAdminClient) { fake.deleteTemplateErr = errors.New("delete failed") },
			residual:  "source-template deletion failed",
		},
		"delete verification failure": {
			configure: func(fake *fakeParameterTemplateAdminClient) { fake.retainDeletedTemplate = true },
			residual:  "deletion could not be verified",
		},
		"new category link": {
			configure: func(fake *fakeParameterTemplateAdminClient) { fake.addCategoryLinkOnUpdate = true },
			residual:  "current source category-default links prevent deletion",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := mergeFake()
			delete(fake.parameters, 81)
			deps := parameterTemplateDeps(fake)
			input := MergeParameterTemplatesInput{SourceTemplateID: 70, TargetTemplateID: 71, DryRun: true}
			_, plan, err := mergeParameterTemplates(deps)(ctx, &mcp.CallToolRequest{}, input)
			require.NoError(t, err)
			test.configure(fake)
			input.DryRun = false
			input.Confirm = true
			input.PlanHash = plan.PlanHash
			_, output, err := mergeParameterTemplates(deps)(ctx, &mcp.CallToolRequest{}, input)
			require.NoError(t, err)
			if name == "new category link" {
				assert.Equal(t, StatusOK, output.Status)
				assert.False(t, output.SourceDeleted)
				assert.Equal(t, []CategoryTemplateReference{{LinkID: 99, CategoryID: 44}}, output.CategoryLinks)
			} else {
				assert.Equal(t, "partial_failure", output.Status)
			}
			assert.Contains(t, strings.Join(output.ResidualDecisions, " "), test.residual)
		})
	}
}

func TestSourceCategoryLinksFailsClosedAboveBound(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	for id := 1; id <= maxTemplateReferenceScan+1; id++ {
		fake.links[id] = inventree.CategoryParameterTemplate{PK: id, Category: id, Template: 70}
	}

	_, err := sourceCategoryLinks(ctx, fake, 70)
	r.Error(err)
	r.ErrorContains(err, "1000-row safety bound")
}

func explicitTemplateInput(name string) CreateParameterTemplateInput {
	return CreateParameterTemplateInput{Name: name, Units: dvgoutils.Ptr("ohm"), Description: dvgoutils.Ptr("resistance"), ModelType: dvgoutils.Ptr("part.part"), Checkbox: dvgoutils.Ptr(false), Choices: dvgoutils.Ptr(""), Enabled: dvgoutils.Ptr(true)}
}

func mergeFake() *fakeParameterTemplateAdminClient {
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Old Resistance"}
	fake.templates[71] = inventree.ParameterTemplate{PK: 71, Name: "Resistance"}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "part.part", ModelID: 10, Data: "10 K"}
	fake.parameters[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "part.part", ModelID: 11, Data: "22k"}
	return fake
}

func parameterTemplateDeps(fake *fakeParameterTemplateAdminClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

type fakeParameterTemplateAdminClient struct {
	templates               map[int]inventree.ParameterTemplate
	parameters              map[int]inventree.Parameter
	links                   map[int]inventree.CategoryParameterTemplate
	deletedTemplates        map[int]bool
	nextTemplateID          int
	lastPatch               inventree.PatchFields
	changeSourceOnRead      bool
	updatePartParameterErr  error
	corruptUpdateReadback   bool
	corruptNextReadback     bool
	deleteTemplateErr       error
	retainDeletedTemplate   bool
	addCategoryLinkOnUpdate bool
}

func newFakeParameterTemplateAdminClient() *fakeParameterTemplateAdminClient {
	return &fakeParameterTemplateAdminClient{templates: map[int]inventree.ParameterTemplate{}, parameters: map[int]inventree.Parameter{}, links: map[int]inventree.CategoryParameterTemplate{}, deletedTemplates: map[int]bool{}, nextTemplateID: 100}
}

func (f *fakeParameterTemplateAdminClient) SearchParameterTemplates(_ context.Context, query inventree.SearchQuery) ([]inventree.ParameterTemplate, error) {
	var out []inventree.ParameterTemplate
	for _, record := range f.templates {
		if query.Search == "" || strings.Contains(strings.ToLower(record.Name), strings.ToLower(query.Search)) {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PK < out[j].PK })
	return out, nil
}

func (f *fakeParameterTemplateAdminClient) GetParameterTemplate(_ context.Context, id int) (inventree.ParameterTemplate, error) {
	record, ok := f.templates[id]
	if !ok {
		return inventree.ParameterTemplate{}, notFoundError()
	}
	return record, nil
}

func (f *fakeParameterTemplateAdminClient) CreateParameterTemplate(_ context.Context, input inventree.ParameterTemplateCreate) (inventree.ParameterTemplate, error) {
	id := f.nextTemplateID
	f.nextTemplateID++
	record := inventree.ParameterTemplate{PK: id, Name: input.Name, Units: dvgoutils.Ptr(input.Units), Description: input.Description, ModelType: dvgoutils.Ptr(input.ModelType), Checkbox: input.Checkbox, Choices: input.Choices, SelectionList: input.SelectionList, Enabled: input.Enabled}
	if input.Unique != nil {
		record.Unique = *input.Unique
	}
	f.templates[id] = record
	return record, nil
}

func (f *fakeParameterTemplateAdminClient) UpdateParameterTemplate(_ context.Context, id int, fields inventree.PatchFields) (inventree.ParameterTemplate, error) {
	f.lastPatch = fields
	record, ok := f.templates[id]
	if !ok {
		return inventree.ParameterTemplate{}, notFoundError()
	}
	payload, _ := fields.MarshalJSON()
	var values map[string]any
	_ = json.Unmarshal(payload, &values)
	if value, ok := values["name"].(string); ok {
		record.Name = value
	}
	if value, ok := values["units"].(string); ok {
		record.Units = dvgoutils.Ptr(value)
	}
	if value, ok := values["description"].(string); ok {
		record.Description = value
	}
	if value, ok := values["model_type"].(string); ok {
		record.ModelType = dvgoutils.Ptr(value)
	}
	if value, ok := values["checkbox"].(bool); ok {
		record.Checkbox = value
	}
	if value, ok := values["choices"].(string); ok {
		record.Choices = value
	}
	if value, exists := values["selectionlist"]; exists {
		if value == nil {
			record.SelectionList = nil
		} else {
			v := int(value.(float64))
			record.SelectionList = &v
		}
	}
	if value, ok := values["enabled"].(bool); ok {
		record.Enabled = value
	}
	if value, ok := values["unique"].(float64); ok {
		record.Unique = inventree.ParameterUniqueness(value)
	}
	f.templates[id] = record
	return record, nil
}

func (f *fakeParameterTemplateAdminClient) DeleteParameterTemplate(_ context.Context, id int) error {
	if f.deleteTemplateErr != nil {
		return f.deleteTemplateErr
	}
	if !f.retainDeletedTemplate {
		delete(f.templates, id)
	}
	f.deletedTemplates[id] = true
	return nil
}

func (f *fakeParameterTemplateAdminClient) SearchTemplateParametersPage(_ context.Context, query inventree.TemplateParameterQuery) (inventree.PartParameterPage, error) {
	var rows []inventree.Parameter
	for _, row := range f.parameters {
		if row.Template == query.TemplateID {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].PK < rows[j].PK })
	start := min(query.Offset, len(rows))
	end := min(start+query.Limit, len(rows))
	return inventree.PartParameterPage{Count: len(rows), Results: rows[start:end], HasMore: end < len(rows)}, nil
}

func (f *fakeParameterTemplateAdminClient) SearchCategoryParameterTemplatesPage(_ context.Context, query inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error) {
	var out []inventree.CategoryParameterTemplate
	for _, link := range f.links {
		out = append(out, link)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PK < out[j].PK })
	start := min(query.Offset, len(out))
	end := min(start+query.Limit, len(out))
	var next *string
	if end < len(out) {
		next = dvgoutils.Ptr("next")
	}
	return inventree.Page[inventree.CategoryParameterTemplate]{Count: len(out), Results: out[start:end], Next: next}, nil
}

func (f *fakeParameterTemplateAdminClient) GetPartParameter(_ context.Context, id int) (inventree.Parameter, error) {
	record, ok := f.parameters[id]
	if !ok {
		return inventree.Parameter{}, notFoundError()
	}
	if f.changeSourceOnRead {
		f.changeSourceOnRead = false
		record.Data = "changed during execution"
	}
	if f.corruptNextReadback {
		f.corruptNextReadback = false
		record.Data = "unexpected readback"
	}
	return record, nil
}

func (f *fakeParameterTemplateAdminClient) UpdatePartParameter(_ context.Context, id int, fields inventree.PatchFields) (inventree.Parameter, error) {
	if f.updatePartParameterErr != nil {
		return inventree.Parameter{}, f.updatePartParameterErr
	}
	record, ok := f.parameters[id]
	if !ok {
		return inventree.Parameter{}, notFoundError()
	}
	payload, _ := fields.MarshalJSON()
	var values map[string]any
	_ = json.Unmarshal(payload, &values)
	if value, ok := values["template"].(float64); ok {
		record.Template = int(value)
	}
	if value, ok := values["data"].(string); ok {
		record.Data = value
	}
	f.parameters[id] = record
	if f.corruptUpdateReadback {
		f.corruptNextReadback = true
	}
	if f.addCategoryLinkOnUpdate {
		f.links[99] = inventree.CategoryParameterTemplate{PK: 99, Category: 44, Template: 70}
	}
	return record, nil
}

func notFoundError() error {
	return &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
}

func parameterTemplateUniquenessDeps(fake *fakeParameterTemplateAdminClient) Dependencies {
	deps := parameterTemplateDeps(fake)
	deps.parameterTemplateUniquenessPlanStore = newParameterTemplateUniquenessPlanStore(time.Now, randomStockPlanToken)
	return deps
}

func TestUpdateParameterTemplateUniquenessRequiresValidInput(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	deps := parameterTemplateUniquenessDeps(fake)

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 0, Unique: 1})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)

	_, out, err = updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 9})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestUpdateParameterTemplateUniquenessNotFound(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	deps := parameterTemplateUniquenessDeps(fake)

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 9999, Unique: 1, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestUpdateParameterTemplateUniquenessDryRunIssuesPlanWhenNoConflicts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 10, Data: "10 K"}
	fake.parameters[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "stock.stocklocation", ModelID: 11, Data: "22k"}
	deps := parameterTemplateUniquenessDeps(fake)

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(out.DryRun)
	a.NotEmpty(out.PlanHash)
	a.Equal(2, out.RowCount)
	a.Empty(out.Conflicts)
}

func TestUpdateParameterTemplateUniquenessDryRunReportsConflicts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "part.part", ModelID: 10, Data: "same"}
	fake.parameters[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "part.part", ModelID: 11, Data: "same"}
	deps := parameterTemplateUniquenessDeps(fake)

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Empty(out.PlanHash)
	r.Len(out.Conflicts, 1)
	r.Len(out.Conflicts[0].Rows, 2)
	a.ElementsMatch([]int{80, 81}, conflictParameterIDs(out.Conflicts[0]))
	for _, row := range out.Conflicts[0].Rows {
		a.Equal("part.part", row.ModelType)
	}
}

func TestUpdateParameterTemplateUniquenessConflictNeverDisclosesTheSharedValue(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "part.part", ModelID: 10, Data: "super-secret-value"}
	fake.parameters[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "part.part", ModelID: 11, Data: "super-secret-value"}
	deps := parameterTemplateUniquenessDeps(fake)

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, DryRun: true})
	r.NoError(err)
	r.Len(out.Conflicts, 1)
	payload, err := json.Marshal(out.Conflicts)
	r.NoError(err)
	a.NotContains(string(payload), "super-secret-value")
}

func TestUpdateParameterTemplateUniquenessGlobalScopeConflictSpansModelTypes(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 10, Data: "same"}
	fake.parameters[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "company.company", ModelID: 11, Data: "same"}
	deps := parameterTemplateUniquenessDeps(fake)

	// A global-scope target must catch this cross-model-type conflict; a model-type-scoped target must not.
	_, global, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, DryRun: true})
	r.NoError(err)
	r.Len(global.Conflicts, 1)
	a.ElementsMatch([]string{"stock.stocklocation", "company.company"}, conflictModelTypes(global.Conflicts[0]))

	_, scoped, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 1, DryRun: true})
	r.NoError(err)
	a.Empty(scoped.Conflicts)
}

func conflictParameterIDs(conflict ParameterTemplateUniquenessConflict) []int {
	ids := make([]int, 0, len(conflict.Rows))
	for _, row := range conflict.Rows {
		ids = append(ids, row.ParameterID)
	}
	return ids
}

func conflictModelTypes(conflict ParameterTemplateUniquenessConflict) []string {
	types := make([]string, 0, len(conflict.Rows))
	for _, row := range conflict.Rows {
		types = append(types, row.ModelType)
	}
	return types
}

func TestUpdateParameterTemplateUniquenessModelTypeScopeDoesNotConflictAcrossModelTypes(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "stock.stocklocation", ModelID: 10, Data: "same"}
	fake.parameters[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "company.company", ModelID: 11, Data: "same"}
	deps := parameterTemplateUniquenessDeps(fake)

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 1, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Empty(out.Conflicts)
	a.NotEmpty(out.PlanHash)
}

func TestUpdateParameterTemplateUniquenessConfirmRequiresPriorDryRun(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	deps := parameterTemplateUniquenessDeps(fake)

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
}

func TestUpdateParameterTemplateUniquenessConfirmAppliesAndVerifies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	deps := parameterTemplateUniquenessDeps(fake)

	_, plan, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, DryRun: true})
	r.NoError(err)
	r.Equal(StatusOK, plan.Status)
	r.NotEmpty(plan.PlanHash)

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(out.Verified)
	r.NotNil(out.Record)
	a.Equal(inventree.ParameterUniquenessGlobal, out.Record.Unique)
	a.Equal(inventree.ParameterUniquenessGlobal, fake.templates[70].Unique)
}

func TestUpdateParameterTemplateUniquenessConfirmRejectsStaleToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	deps := parameterTemplateUniquenessDeps(fake)

	_, plan, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, DryRun: true})
	r.NoError(err)
	r.NotEmpty(plan.PlanHash)

	// Rows referencing the template changed since the dry run, so the plan is now stale.
	fake.parameters[82] = inventree.Parameter{PK: 82, Template: 70, ModelType: "company.company", ModelID: 20, Data: "new"}

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestUpdateParameterTemplateUniquenessRejectsMalformedOrUnknownToken(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	deps := parameterTemplateUniquenessDeps(fake)

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, Confirm: true, PlanHash: "not-a-real-token"})
	require.NoError(t, err)
	assert.Equal(t, StatusClarificationRequired, out.Status)
	assert.Equal(t, inventree.ParameterUniquenessNone, fake.templates[70].Unique)
}

func TestUpdateParameterTemplateUniquenessConfirmRefusesRemainingConflicts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeParameterTemplateAdminClient()
	fake.templates[70] = inventree.ParameterTemplate{PK: 70, Name: "Resistance", Enabled: true}
	deps := parameterTemplateUniquenessDeps(fake)

	_, plan, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, DryRun: true})
	r.NoError(err)
	r.NotEmpty(plan.PlanHash)

	fake.parameters[80] = inventree.Parameter{PK: 80, Template: 70, ModelType: "part.part", ModelID: 10, Data: "same"}
	fake.parameters[81] = inventree.Parameter{PK: 81, Template: 70, ModelType: "part.part", ModelID: 11, Data: "same"}

	_, out, err := updateParameterTemplateUniqueness(deps)(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: 70, Unique: 2, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.Len(out.Conflicts, 1)
}
