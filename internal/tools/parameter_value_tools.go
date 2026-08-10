package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PartParameterSearchClient interface {
	SearchPartParametersPage(context.Context, inventree.PartParameterQuery) (inventree.PartParameterPage, error)
	SearchParameterTemplates(context.Context, inventree.SearchQuery) ([]inventree.ParameterTemplate, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
	GetPart(context.Context, int) (inventree.Part, error)
}

type PartParameterDeleteClient interface {
	GetPartParameter(context.Context, int) (inventree.Parameter, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
	GetPart(context.Context, int) (inventree.Part, error)
	DeletePartParameter(context.Context, int) error
}

type SearchPartParametersInput struct {
	TemplateID   int     `json:"template_id,omitempty" jsonschema:"Optional stable parameter-template primary key."`
	TemplateName string  `json:"template_name,omitempty" jsonschema:"Optional exact template name; ambiguous matches require template_id."`
	Value        *string `json:"value,omitempty" jsonschema:"Optional exact parameter value filter, including an explicitly empty value."`
	CategoryID   int     `json:"category_id,omitempty" jsonschema:"Optional exact part-category primary key filter."`
	PartID       int     `json:"part_id,omitempty" jsonschema:"Optional exact part primary key filter."`
	Limit        int     `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset       int     `json:"offset,omitempty" jsonschema:"Pagination offset applied after all exact filters."`
}

const (
	partParameterPageSize = 100
	maxPartParameterScan  = 1000
)

type PartParameterSearchResult struct {
	inventree.WebLinkFields
	ParameterID  int     `json:"parameter_id"`
	PartID       int     `json:"part_id"`
	PartName     string  `json:"part_name,omitempty"`
	CategoryID   *int    `json:"category_id,omitempty"`
	TemplateID   int     `json:"template_id"`
	TemplateName string  `json:"template_name"`
	Units        *string `json:"units,omitempty"`
	Value        string  `json:"value"`
}

type DeletePartParameterInput struct {
	ParameterID int  `json:"parameter_id" jsonschema:"Stable primary key of exactly one part parameter row."`
	Confirm     bool `json:"confirm,omitempty" jsonschema:"Required true before deleting the selected parameter row."`
}

type DeletePartParameterOutput struct {
	Status        string                    `json:"status"`
	Record        PartParameterSearchResult `json:"record,omitempty"`
	Verified      bool                      `json:"verified"`
	Clarification *ClarificationResponse    `json:"clarification,omitempty"`
}

func searchPartParameterValues(deps Dependencies) mcp.ToolHandlerFor[SearchPartParametersInput, LookupOutput[PartParameterSearchResult]] {
	return LookupHandler[PartParameterSearchClient, SearchPartParametersInput, LookupOutput[PartParameterSearchResult]](deps, SearchPartParametersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartParameterSearchClient, input SearchPartParametersInput) (*mcp.CallToolResult, LookupOutput[PartParameterSearchResult], error) {
			if input.TemplateID < 0 || input.CategoryID < 0 || input.PartID < 0 || input.Offset < 0 {
				return parameterSearchClarification("Which valid search filters should be used?", "filters", "IDs and offset cannot be negative", input)
			}
			if !hasPartParameterSearchFilter(input) {
				return parameterSearchClarification("Which parameter values should be searched?", "filters", "search_part_parameters requires at least one template, value, category, or part filter", input)
			}
			limit := NormalizeLookupLimit(input.Limit)
			if input.Offset > maxPartParameterScan-limit {
				return parameterSearchClarification("Which result page should be searched?", "offset", "offset plus limit cannot exceed the 1,000-row scan bound", input)
			}
			templateID, result, output, ok, err := resolvePartParameterTemplate(ctx, client, input)
			if err != nil || !ok {
				return result, output, err
			}
			needed := input.Offset + limit
			enriched := make([]PartParameterSearchResult, 0, needed)
			parts := map[int]inventree.Part{}
			templates := map[int]inventree.ParameterTemplate{}
			serverOffset := 0
			hasMore := true
			for hasMore && serverOffset < maxPartParameterScan {
				pageLimit := min(partParameterPageSize, maxPartParameterScan-serverOffset)
				page, err := client.SearchPartParametersPage(ctx, inventree.PartParameterQuery{
					Search:     partParameterValueSearch(input.Value),
					PartID:     input.PartID,
					TemplateID: templateID,
					Limit:      pageLimit,
					Offset:     serverOffset,
				})
				if err != nil {
					return nil, LookupOutput[PartParameterSearchResult]{}, err
				}
				pageResults, err := enrichPartParameters(ctx, client, page.Results, input.Value, input.CategoryID, parts, templates)
				if err != nil {
					return nil, LookupOutput[PartParameterSearchResult]{}, err
				}
				enriched = append(enriched, pageResults...)
				serverOffset += len(page.Results)
				hasMore = page.HasMore
				if len(page.Results) == 0 {
					hasMore = false
				}
			}
			if hasMore {
				return parameterSearchClarification("Which narrower parameter search should be used?", "filters", "search exceeded the 1,000-row scan bound before complete row-ID ordering could be established", input)
			}
			slices.SortFunc(enriched, func(a, b PartParameterSearchResult) int { return a.ParameterID - b.ParameterID })
			enriched = paginatePartParameters(enriched, input.Offset, limit)
			return listOutput(enriched, nil)
		})
}

func deletePartParameter(deps Dependencies) mcp.ToolHandlerFor[DeletePartParameterInput, DeletePartParameterOutput] {
	return LookupHandler[PartParameterDeleteClient, DeletePartParameterInput, DeletePartParameterOutput](deps, DeletePartParameterToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartParameterDeleteClient, input DeletePartParameterInput) (*mcp.CallToolResult, DeletePartParameterOutput, error) {
			if input.ParameterID <= 0 {
				return deletePartParameterClarification(PartParameterSearchResult{}, "Which parameter row should be deleted?", "parameter_id", "delete_part_parameter requires one positive parameter_id", input.ParameterID)
			}
			existing, err := client.GetPartParameter(ctx, input.ParameterID)
			if err != nil {
				if isNotFound(err) {
					return TextResult(StatusNotFound), DeletePartParameterOutput{Status: StatusNotFound}, nil
				}
				return nil, DeletePartParameterOutput{}, err
			}
			if existing.PK != input.ParameterID {
				return nil, DeletePartParameterOutput{}, fmt.Errorf("parameter detail identity mismatch: requested %d, received %d", input.ParameterID, existing.PK)
			}
			if existing.ModelType != "part.part" {
				record := PartParameterSearchResult{ParameterID: existing.PK, PartID: existing.ModelID, TemplateID: existing.Template, Value: existing.Data}
				return deletePartParameterClarification(record, "Which part parameter row should be deleted?", "parameter_id", fmt.Sprintf("parameter row %d belongs to unsupported model type %q; only one part parameter row can be deleted", existing.PK, existing.ModelType), input.ParameterID)
			}
			record, err := enrichPartParameter(ctx, client, existing)
			if err != nil {
				return nil, DeletePartParameterOutput{}, err
			}
			if !input.Confirm {
				return deletePartParameterClarification(record, "Confirm deletion of this part parameter row?", "confirm", "delete_part_parameter requires confirm:true after reviewing the stable row ID", input.ParameterID)
			}
			if err := client.DeletePartParameter(ctx, input.ParameterID); err != nil {
				return nil, DeletePartParameterOutput{}, err
			}
			_, err = client.GetPartParameter(ctx, input.ParameterID)
			if err == nil {
				return nil, DeletePartParameterOutput{}, fmt.Errorf("parameter row %d still exists after deletion", input.ParameterID)
			}
			if !isNotFound(err) {
				return nil, DeletePartParameterOutput{}, fmt.Errorf("verify parameter row %d deletion: %w", input.ParameterID, err)
			}
			return TextResult(StatusOK), DeletePartParameterOutput{Status: StatusOK, Record: record, Verified: true}, nil
		})
}

func resolvePartParameterTemplate(ctx context.Context, client PartParameterSearchClient, input SearchPartParametersInput) (int, *mcp.CallToolResult, LookupOutput[PartParameterSearchResult], bool, error) {
	name := strings.TrimSpace(input.TemplateName)
	if input.TemplateID > 0 {
		if name == "" {
			return input.TemplateID, nil, LookupOutput[PartParameterSearchResult]{}, true, nil
		}
		template, err := client.GetParameterTemplate(ctx, input.TemplateID)
		if err != nil {
			return 0, nil, LookupOutput[PartParameterSearchResult]{}, false, err
		}
		if !strings.EqualFold(strings.TrimSpace(template.Name), name) {
			result, output, err := parameterSearchClarification("Which parameter template should be searched?", "template", "template_id does not match template_name", input)
			return 0, result, output, false, err
		}
		return input.TemplateID, nil, LookupOutput[PartParameterSearchResult]{}, true, nil
	}
	if name == "" {
		return 0, nil, LookupOutput[PartParameterSearchResult]{}, true, nil
	}
	templates, err := client.SearchParameterTemplates(ctx, inventree.SearchQuery{Search: name, Limit: MaxLookupLimit})
	if err != nil {
		return 0, nil, LookupOutput[PartParameterSearchResult]{}, false, err
	}
	exact := make([]inventree.ParameterTemplate, 0, len(templates))
	for _, template := range templates {
		if strings.EqualFold(strings.TrimSpace(template.Name), name) {
			exact = append(exact, template)
		}
	}
	if len(exact) == 1 {
		return exact[0].PK, nil, LookupOutput[PartParameterSearchResult]{}, true, nil
	}
	reason := "no parameter template has that exact name"
	if len(exact) > 1 {
		reason = "multiple parameter templates have that exact name"
	}
	clarification := NewClarification("Which parameter template should be searched?", "template", reason, "template_id", len(exact) == 0, templateCandidates(exact, nil, nil), searchPartParameterRetryValues(input))
	return 0, TextResult(StatusClarificationRequired), LookupOutput[PartParameterSearchResult]{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
}

func enrichPartParameters(ctx context.Context, client PartParameterSearchClient, records []inventree.Parameter, exactValue *string, categoryID int, parts map[int]inventree.Part, templates map[int]inventree.ParameterTemplate) ([]PartParameterSearchResult, error) {
	results := make([]PartParameterSearchResult, 0, len(records))
	for _, parameter := range records {
		if parameter.ModelType != "part.part" || (exactValue != nil && parameter.Data != *exactValue) {
			continue
		}
		part, ok := parts[parameter.ModelID]
		if !ok {
			var err error
			part, err = client.GetPart(ctx, parameter.ModelID)
			if err != nil {
				return nil, err
			}
			if part.PK != parameter.ModelID {
				return nil, fmt.Errorf("part detail identity mismatch: requested %d, received %d", parameter.ModelID, part.PK)
			}
			parts[parameter.ModelID] = part
		}
		if categoryID > 0 && (part.Category == nil || *part.Category != categoryID) {
			continue
		}
		template, ok := templates[parameter.Template]
		if !ok {
			var err error
			template, err = client.GetParameterTemplate(ctx, parameter.Template)
			if err != nil {
				return nil, err
			}
			if template.PK != parameter.Template {
				return nil, fmt.Errorf("parameter template detail identity mismatch: requested %d, received %d", parameter.Template, template.PK)
			}
			templates[parameter.Template] = template
		}
		results = append(results, partParameterSearchResult(parameter, part, template))
	}
	return results, nil
}

func enrichPartParameter(ctx context.Context, client PartParameterDeleteClient, parameter inventree.Parameter) (PartParameterSearchResult, error) {
	part, err := client.GetPart(ctx, parameter.ModelID)
	if err != nil {
		return PartParameterSearchResult{}, err
	}
	template, err := client.GetParameterTemplate(ctx, parameter.Template)
	if err != nil {
		return PartParameterSearchResult{}, err
	}
	if part.PK != parameter.ModelID {
		return PartParameterSearchResult{}, fmt.Errorf("part detail identity mismatch: requested %d, received %d", parameter.ModelID, part.PK)
	}
	if template.PK != parameter.Template {
		return PartParameterSearchResult{}, fmt.Errorf("parameter template detail identity mismatch: requested %d, received %d", parameter.Template, template.PK)
	}
	return partParameterSearchResult(parameter, part, template), nil
}

func partParameterSearchResult(parameter inventree.Parameter, part inventree.Part, template inventree.ParameterTemplate) PartParameterSearchResult {
	return PartParameterSearchResult{ParameterID: parameter.PK, PartID: part.PK, PartName: part.Name, CategoryID: part.Category, TemplateID: template.PK, TemplateName: template.Name, Units: template.Units, Value: parameter.Data}
}

func paginatePartParameters(records []PartParameterSearchResult, offset, limit int) []PartParameterSearchResult {
	if offset >= len(records) {
		return nil
	}
	end := min(len(records), offset+limit)
	return records[offset:end]
}

func parameterSearchClarification(question, field, reason string, input SearchPartParametersInput) (*mcp.CallToolResult, LookupOutput[PartParameterSearchResult], error) {
	clarification := NewClarification(question, field, reason, field, true, nil, searchPartParameterRetryValues(input))
	return TextResult(StatusClarificationRequired), LookupOutput[PartParameterSearchResult]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func searchPartParameterRetryValues(input SearchPartParametersInput) map[string]any {
	return map[string]any{"template_id": input.TemplateID, "template_name": input.TemplateName, "value": input.Value, "category_id": input.CategoryID, "part_id": input.PartID, "limit": input.Limit, "offset": input.Offset}
}

func hasPartParameterSearchFilter(input SearchPartParametersInput) bool {
	return input.TemplateID > 0 || strings.TrimSpace(input.TemplateName) != "" || input.Value != nil || input.CategoryID > 0 || input.PartID > 0
}

func partParameterValueSearch(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func deletePartParameterClarification(record PartParameterSearchResult, question, field, reason string, parameterID int) (*mcp.CallToolResult, DeletePartParameterOutput, error) {
	candidates := []ClarificationCandidate(nil)
	if record.ParameterID > 0 {
		candidates = []ClarificationCandidate{{ID: fmt.Sprint(record.ParameterID), Label: record.TemplateName, APIURL: fmt.Sprintf("/api/parameter/%d/", record.ParameterID), Fields: map[string]any{"part_id": record.PartID, "category_id": record.CategoryID, "template_id": record.TemplateID, "value": record.Value}}}
	}
	clarification := NewClarification(question, field, reason, field, true, candidates, map[string]any{"parameter_id": parameterID})
	return TextResult(StatusClarificationRequired), DeletePartParameterOutput{Status: StatusClarificationRequired, Record: record, Clarification: &clarification}, nil
}
