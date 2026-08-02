package tools

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxCategoryDefaultScan      = 1000
	maxCategoryDefaultScanPages = maxCategoryDefaultScan/MaxLookupLimit + 1
)

var errCategoryDefaultScanLimit = errors.New("category-default scan safety limit exceeded")

type CategoryParameterDefaultClient interface {
	GetPartCategory(context.Context, int) (inventree.Category, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
	SearchCategoryParameterTemplatesPage(context.Context, inventree.CategoryParameterTemplateQuery) (inventree.Page[inventree.CategoryParameterTemplate], error)
	GetCategoryParameterTemplate(context.Context, int) (inventree.CategoryParameterTemplate, error)
	CreateCategoryParameterTemplate(context.Context, inventree.CategoryParameterTemplateCreate) (inventree.CategoryParameterTemplate, error)
	UpdateCategoryParameterTemplate(context.Context, int, inventree.PatchFields) (inventree.CategoryParameterTemplate, error)
	DeleteCategoryParameterTemplate(context.Context, int) error
}

type SearchCategoryParameterDefaultsInput struct {
	CategoryID            int  `json:"category_id,omitempty" jsonschema:"Optional category primary key. Results are exact-category links unless include_parent_defaults is true."`
	TemplateID            int  `json:"template_id,omitempty" jsonschema:"Optional existing parameter-template primary key."`
	IncludeParentDefaults bool `json:"include_parent_defaults,omitempty" jsonschema:"Include defaults inherited from ancestor categories; requires category_id."`
	Limit                 int  `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset                int  `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type CategoryParameterDefaultRecord struct {
	LinkID              int    `json:"link_id"`
	CategoryID          int    `json:"category_id"`
	CategoryName        string `json:"category_name"`
	TemplateID          int    `json:"template_id"`
	TemplateName        string `json:"template_name"`
	DefaultValue        string `json:"default_value"`
	RequestedCategoryID int    `json:"requested_category_id,omitempty"`
	Inherited           bool   `json:"inherited"`
}

type CategoryParameterDefaultsOutput struct {
	Status        string                           `json:"status"`
	Count         int                              `json:"count"`
	HasMore       bool                             `json:"has_more"`
	Records       []CategoryParameterDefaultRecord `json:"records"`
	Clarification *ClarificationResponse           `json:"clarification,omitempty"`
}

type CreateCategoryParameterDefaultInput struct {
	CategoryID   int    `json:"category_id" jsonschema:"Existing source category primary key."`
	TemplateID   int    `json:"template_id" jsonschema:"Existing parameter-template primary key; templates are never created implicitly."`
	DefaultValue string `json:"default_value" jsonschema:"Default value for the category link; use an empty string for no populated default."`
}

type UpdateCategoryParameterDefaultInput struct {
	LinkID       int     `json:"link_id" jsonschema:"Stable direct category-default link primary key."`
	CategoryID   *int    `json:"category_id,omitempty" jsonschema:"Optional replacement source category primary key."`
	TemplateID   *int    `json:"template_id,omitempty" jsonschema:"Optional replacement existing parameter-template primary key."`
	DefaultValue *string `json:"default_value,omitempty" jsonschema:"Optional replacement default, including an explicit empty string."`
}

type DeleteCategoryParameterDefaultInput struct {
	LinkID  int  `json:"link_id" jsonschema:"Stable direct category-default link primary key."`
	Confirm bool `json:"confirm,omitempty" jsonschema:"Required true after reviewing the direct source category and template."`
}

type CategoryParameterDefaultOutput struct {
	Status        string                          `json:"status"`
	Record        *CategoryParameterDefaultRecord `json:"record,omitempty"`
	Verified      bool                            `json:"verified,omitempty"`
	Clarification *ClarificationResponse          `json:"clarification,omitempty"`
}

func registerCategoryParameterLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchCategoryParameterDefaultsToolName, "Search category parameter defaults", "Lists exact category defaults or an explicitly requested inherited view using stable link IDs.", searchCategoryParameterDefaults(deps))
}

func registerCategoryParameterWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, CreateCategoryParameterDefaultToolName, "Create category parameter default", "Links an existing parameter template to one category after duplicate preflight.", createCategoryParameterDefault(deps))
	addWriteTool(server, deps, UpdateCategoryParameterDefaultToolName, "Update category parameter default", "Partially updates one direct category-default link by stable ID.", updateCategoryParameterDefault(deps))
	addWriteTool(server, deps, DeleteCategoryParameterDefaultToolName, "Delete category parameter default", "Deletes one direct category-default link after confirm:true and read-back verification.", deleteCategoryParameterDefault(deps))
}

func searchCategoryParameterDefaults(deps Dependencies) mcp.ToolHandlerFor[SearchCategoryParameterDefaultsInput, CategoryParameterDefaultsOutput] {
	return LookupHandler[CategoryParameterDefaultClient, SearchCategoryParameterDefaultsInput, CategoryParameterDefaultsOutput](deps, SearchCategoryParameterDefaultsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CategoryParameterDefaultClient, input SearchCategoryParameterDefaultsInput) (*mcp.CallToolResult, CategoryParameterDefaultsOutput, error) {
			if input.CategoryID < 0 || input.TemplateID < 0 || input.Offset < 0 {
				return categoryDefaultClarification("Which valid filters should be used?", "filters", "category_id, template_id, and offset cannot be negative", "filters", nil)
			}
			if input.IncludeParentDefaults && input.CategoryID == 0 {
				return categoryDefaultClarification("Which category should provide inherited defaults?", "category_id", "include_parent_defaults requires category_id", "category_id", nil)
			}
			if input.CategoryID > 0 {
				if _, err := client.GetPartCategory(ctx, input.CategoryID); err != nil {
					if isNotFound(err) {
						return categoryDefaultClarification("Which existing category should be searched?", "category_id", "category_id does not exist", "category_id", map[string]any{"category_id": input.CategoryID})
					}
					return nil, CategoryParameterDefaultsOutput{}, err
				}
			}
			if input.TemplateID > 0 {
				if _, err := client.GetParameterTemplate(ctx, input.TemplateID); err != nil {
					if isNotFound(err) {
						return categoryDefaultClarification("Which existing parameter template should be searched?", "template_id", "template_id does not exist", "template_id", map[string]any{"template_id": input.TemplateID})
					}
					return nil, CategoryParameterDefaultsOutput{}, err
				}
			}
			limit := NormalizeLookupLimit(input.Limit)
			links, count, hasMore, err := categoryDefaultPage(ctx, client, input, limit)
			if err != nil {
				if errors.Is(err, errCategoryDefaultScanLimit) {
					return categoryDefaultClarification("Which narrower category-default search should be used?", "category_id", "the filtered search exceeds the 1,000-link safety bound; provide a narrower category_id or reconcile the category's direct links", "category_id", map[string]any{"category_id": input.CategoryID, "template_id": input.TemplateID})
				}
				return nil, CategoryParameterDefaultsOutput{}, err
			}
			records, err := enrichCategoryDefaults(ctx, client, links, input.CategoryID)
			if err != nil {
				return nil, CategoryParameterDefaultsOutput{}, err
			}
			return TextResult(StatusOK), CategoryParameterDefaultsOutput{Status: StatusOK, Count: count, HasMore: hasMore, Records: records}, nil
		})
}

func createCategoryParameterDefault(deps Dependencies) mcp.ToolHandlerFor[CreateCategoryParameterDefaultInput, CategoryParameterDefaultOutput] {
	return LookupHandler[CategoryParameterDefaultClient, CreateCategoryParameterDefaultInput, CategoryParameterDefaultOutput](deps, CreateCategoryParameterDefaultToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CategoryParameterDefaultClient, input CreateCategoryParameterDefaultInput) (*mcp.CallToolResult, CategoryParameterDefaultOutput, error) {
			if input.CategoryID <= 0 || input.TemplateID <= 0 || !validCategoryDefaultValue(input.DefaultValue) {
				return categoryDefaultWriteClarification("Which existing category, template, and valid default should be linked?", "category_default", "category_id and template_id must be positive and default_value cannot exceed 500 characters", "category_default", nil)
			}
			if _, err := client.GetPartCategory(ctx, input.CategoryID); err != nil {
				if isNotFound(err) {
					return categoryDefaultWriteClarification("Which existing category should own this default?", "category_id", "category_id does not exist", "category_id", map[string]any{"category_id": input.CategoryID, "template_id": input.TemplateID, "default_value": input.DefaultValue})
				}
				return nil, CategoryParameterDefaultOutput{}, err
			}
			if _, err := client.GetParameterTemplate(ctx, input.TemplateID); err != nil {
				if isNotFound(err) {
					return categoryDefaultWriteClarification("Which existing parameter template should be linked?", "template_id", "template_id does not exist", "template_id", map[string]any{"category_id": input.CategoryID, "template_id": input.TemplateID, "default_value": input.DefaultValue})
				}
				return nil, CategoryParameterDefaultOutput{}, err
			}
			duplicate, err := findCategoryDefaultDuplicate(ctx, client, input.CategoryID, input.TemplateID, 0)
			if err != nil {
				if errors.Is(err, errCategoryDefaultScanLimit) {
					return categoryDefaultWriteClarification("How should this unusually large category be reconciled before creating a default?", "category_id", "duplicate preflight exceeds the 1,000-link safety bound, so uniqueness cannot be proven", "category_id", map[string]any{"category_id": input.CategoryID, "template_id": input.TemplateID, "default_value": input.DefaultValue})
				}
				return nil, CategoryParameterDefaultOutput{}, err
			}
			if duplicate != nil {
				return duplicateCreateCategoryDefaultClarification(*duplicate, input)
			}
			link, err := client.CreateCategoryParameterTemplate(ctx, inventree.CategoryParameterTemplateCreate{Category: input.CategoryID, Template: input.TemplateID, DefaultValue: input.DefaultValue})
			if err != nil {
				return nil, CategoryParameterDefaultOutput{}, err
			}
			if link.PK <= 0 || link.Category != input.CategoryID || link.Template != input.TemplateID {
				return nil, CategoryParameterDefaultOutput{}, fmt.Errorf("created category parameter default identity mismatch: requested category %d template %d, received link %d category %d template %d", input.CategoryID, input.TemplateID, link.PK, link.Category, link.Template)
			}
			record, err := enrichCategoryDefault(ctx, client, link, 0)
			if err != nil {
				return nil, CategoryParameterDefaultOutput{}, err
			}
			return TextResult(StatusOK), CategoryParameterDefaultOutput{Status: StatusOK, Record: &record}, nil
		})
}

func updateCategoryParameterDefault(deps Dependencies) mcp.ToolHandlerFor[UpdateCategoryParameterDefaultInput, CategoryParameterDefaultOutput] {
	return LookupHandler[CategoryParameterDefaultClient, UpdateCategoryParameterDefaultInput, CategoryParameterDefaultOutput](deps, UpdateCategoryParameterDefaultToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CategoryParameterDefaultClient, input UpdateCategoryParameterDefaultInput) (*mcp.CallToolResult, CategoryParameterDefaultOutput, error) {
			if input.LinkID <= 0 || (input.CategoryID == nil && input.TemplateID == nil && input.DefaultValue == nil) || (input.CategoryID != nil && *input.CategoryID <= 0) || (input.TemplateID != nil && *input.TemplateID <= 0) || (input.DefaultValue != nil && !validCategoryDefaultValue(*input.DefaultValue)) {
				return categoryDefaultWriteClarification("Which direct category-default link and fields should be updated?", "fields", "link_id must be positive; provide at least one valid field and keep default_value within 500 characters", "fields", map[string]any{"link_id": input.LinkID})
			}
			existing, err := client.GetCategoryParameterTemplate(ctx, input.LinkID)
			if err != nil {
				if isNotFound(err) {
					return categoryDefaultWriteClarification("Which direct category-default link should be updated?", "link_id", "link_id does not exist", "link_id", map[string]any{"link_id": input.LinkID})
				}
				return nil, CategoryParameterDefaultOutput{}, err
			}
			if existing.PK != input.LinkID {
				return nil, CategoryParameterDefaultOutput{}, fmt.Errorf("category parameter default identity mismatch: requested %d, received %d", input.LinkID, existing.PK)
			}
			categoryID, templateID := existing.Category, existing.Template
			if input.CategoryID != nil {
				categoryID = *input.CategoryID
			}
			if input.TemplateID != nil {
				templateID = *input.TemplateID
			}
			if _, err := client.GetPartCategory(ctx, categoryID); err != nil {
				if isNotFound(err) {
					return categoryDefaultWriteClarification("Which existing category should own this default?", "category_id", "category_id does not exist", "category_id", map[string]any{"link_id": input.LinkID, "category_id": categoryID, "template_id": templateID})
				}
				return nil, CategoryParameterDefaultOutput{}, err
			}
			if _, err := client.GetParameterTemplate(ctx, templateID); err != nil {
				if isNotFound(err) {
					return categoryDefaultWriteClarification("Which existing parameter template should be linked?", "template_id", "template_id does not exist", "template_id", map[string]any{"link_id": input.LinkID, "category_id": categoryID, "template_id": templateID})
				}
				return nil, CategoryParameterDefaultOutput{}, err
			}
			duplicate, err := findCategoryDefaultDuplicate(ctx, client, categoryID, templateID, input.LinkID)
			if err != nil {
				if errors.Is(err, errCategoryDefaultScanLimit) {
					return categoryDefaultWriteClarification("How should this unusually large category be reconciled before updating a default?", "category_id", "duplicate preflight exceeds the 1,000-link safety bound, so uniqueness cannot be proven", "category_id", map[string]any{"link_id": input.LinkID, "category_id": categoryID, "template_id": templateID})
				}
				return nil, CategoryParameterDefaultOutput{}, err
			}
			if duplicate != nil {
				return duplicateUpdateCategoryDefaultClarification(*duplicate, input.LinkID, categoryID, templateID)
			}
			fields := inventree.PatchFields{}
			if input.CategoryID != nil {
				fields["category"] = inventree.Set(*input.CategoryID)
			}
			if input.TemplateID != nil {
				fields["template"] = inventree.Set(*input.TemplateID)
			}
			if input.DefaultValue != nil {
				fields["default_value"] = inventree.Set(*input.DefaultValue)
			}
			link, err := client.UpdateCategoryParameterTemplate(ctx, input.LinkID, fields)
			if err != nil {
				return nil, CategoryParameterDefaultOutput{}, err
			}
			if link.PK != input.LinkID || link.Category != categoryID || link.Template != templateID {
				return nil, CategoryParameterDefaultOutput{}, fmt.Errorf("updated category parameter default identity mismatch: requested link %d category %d template %d, received link %d category %d template %d", input.LinkID, categoryID, templateID, link.PK, link.Category, link.Template)
			}
			record, err := enrichCategoryDefault(ctx, client, link, 0)
			if err != nil {
				return nil, CategoryParameterDefaultOutput{}, err
			}
			return TextResult(StatusOK), CategoryParameterDefaultOutput{Status: StatusOK, Record: &record}, nil
		})
}

func deleteCategoryParameterDefault(deps Dependencies) mcp.ToolHandlerFor[DeleteCategoryParameterDefaultInput, CategoryParameterDefaultOutput] {
	return LookupHandler[CategoryParameterDefaultClient, DeleteCategoryParameterDefaultInput, CategoryParameterDefaultOutput](deps, DeleteCategoryParameterDefaultToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CategoryParameterDefaultClient, input DeleteCategoryParameterDefaultInput) (*mcp.CallToolResult, CategoryParameterDefaultOutput, error) {
			if input.LinkID <= 0 {
				return categoryDefaultWriteClarification("Which direct category-default link should be deleted?", "link_id", "link_id must be positive", "link_id", nil)
			}
			link, err := client.GetCategoryParameterTemplate(ctx, input.LinkID)
			if err != nil {
				if isNotFound(err) {
					return categoryDefaultWriteClarification("Which direct category-default link should be deleted?", "link_id", "link_id does not exist", "link_id", map[string]any{"link_id": input.LinkID})
				}
				return nil, CategoryParameterDefaultOutput{}, err
			}
			if link.PK != input.LinkID {
				return nil, CategoryParameterDefaultOutput{}, fmt.Errorf("category parameter default identity mismatch: requested %d, received %d", input.LinkID, link.PK)
			}
			record, err := enrichCategoryDefault(ctx, client, link, 0)
			if err != nil {
				return nil, CategoryParameterDefaultOutput{}, err
			}
			if !input.Confirm {
				clarification := NewClarification("Delete this direct category-default link?", "confirm", "delete_category_parameter_default requires confirm:true after reviewing its source category and template", "confirm", true, []ClarificationCandidate{categoryDefaultCandidate(record)}, map[string]any{"link_id": input.LinkID, "confirm": true})
				return TextResult(StatusClarificationRequired), CategoryParameterDefaultOutput{Status: StatusClarificationRequired, Record: &record, Clarification: &clarification}, nil
			}
			if err := client.DeleteCategoryParameterTemplate(ctx, input.LinkID); err != nil {
				return nil, CategoryParameterDefaultOutput{}, err
			}
			_, err = client.GetCategoryParameterTemplate(ctx, input.LinkID)
			if err == nil {
				return nil, CategoryParameterDefaultOutput{}, fmt.Errorf("category parameter default %d still exists after deletion", input.LinkID)
			}
			if !isNotFound(err) {
				return nil, CategoryParameterDefaultOutput{}, fmt.Errorf("verify category parameter default %d deletion: %w", input.LinkID, err)
			}
			return TextResult(StatusOK), CategoryParameterDefaultOutput{Status: StatusOK, Record: &record, Verified: true}, nil
		})
}

func categoryDefaultPage(ctx context.Context, client CategoryParameterDefaultClient, input SearchCategoryParameterDefaultsInput, limit int) ([]inventree.CategoryParameterTemplate, int, bool, error) {
	if input.TemplateID == 0 {
		page, err := client.SearchCategoryParameterTemplatesPage(ctx, inventree.CategoryParameterTemplateQuery{CategoryID: input.CategoryID, FetchParent: dvgoutils.Ptr(input.IncludeParentDefaults), Limit: limit, Offset: input.Offset})
		if err == nil && input.CategoryID > 0 && !input.IncludeParentDefaults {
			for _, link := range page.Results {
				if link.Category != input.CategoryID {
					return nil, 0, false, fmt.Errorf("exact category-default response contains source category %d for requested category %d", link.Category, input.CategoryID)
				}
			}
		}
		return page.Results, page.Count, page.Next != nil && *page.Next != "", err
	}
	all, err := scanCategoryDefaults(ctx, client, input.CategoryID, input.IncludeParentDefaults)
	if err != nil {
		return nil, 0, false, err
	}
	filtered := make([]inventree.CategoryParameterTemplate, 0, len(all))
	for _, link := range all {
		if link.Template == input.TemplateID {
			filtered = append(filtered, link)
		}
	}
	start := min(input.Offset, len(filtered))
	end := min(start+limit, len(filtered))
	return filtered[start:end], len(filtered), end < len(filtered), nil
}

func scanCategoryDefaults(ctx context.Context, client CategoryParameterDefaultClient, categoryID int, includeParents bool) ([]inventree.CategoryParameterTemplate, error) {
	all := make([]inventree.CategoryParameterTemplate, 0)
	for offset, pageNumber := 0, 1; ; offset, pageNumber = offset+MaxLookupLimit, pageNumber+1 {
		if pageNumber > maxCategoryDefaultScanPages {
			return nil, fmt.Errorf("%w: maximum %d pages", errCategoryDefaultScanLimit, maxCategoryDefaultScanPages)
		}
		page, err := client.SearchCategoryParameterTemplatesPage(ctx, inventree.CategoryParameterTemplateQuery{CategoryID: categoryID, FetchParent: dvgoutils.Ptr(includeParents), Limit: MaxLookupLimit, Offset: offset})
		if err != nil {
			return nil, err
		}
		all = append(all, page.Results...)
		if categoryID > 0 && !includeParents {
			for _, link := range page.Results {
				if link.Category != categoryID {
					return nil, fmt.Errorf("exact category-default response contains source category %d for requested category %d", link.Category, categoryID)
				}
			}
		}
		if len(all) > maxCategoryDefaultScan {
			return nil, fmt.Errorf("%w: maximum %d links", errCategoryDefaultScanLimit, maxCategoryDefaultScan)
		}
		if page.Next == nil || *page.Next == "" {
			return all, nil
		}
		if len(page.Results) == 0 {
			return nil, fmt.Errorf("%w: upstream returned an empty page with a next link", errCategoryDefaultScanLimit)
		}
	}
}

func findCategoryDefaultDuplicate(ctx context.Context, client CategoryParameterDefaultClient, categoryID, templateID, excludeLinkID int) (*inventree.CategoryParameterTemplate, error) {
	links, err := scanCategoryDefaults(ctx, client, categoryID, false)
	if err != nil {
		return nil, err
	}
	for i := range links {
		if links[i].Template == templateID && links[i].PK != excludeLinkID {
			return &links[i], nil
		}
	}
	return nil, nil
}

func enrichCategoryDefaults(ctx context.Context, client CategoryParameterDefaultClient, links []inventree.CategoryParameterTemplate, requestedCategoryID int) ([]CategoryParameterDefaultRecord, error) {
	records := make([]CategoryParameterDefaultRecord, 0, len(links))
	categories := map[int]inventree.Category{}
	templates := map[int]inventree.ParameterTemplate{}
	for _, link := range links {
		category, ok := categories[link.Category]
		if link.CategoryDetail != nil {
			if link.CategoryDetail.PK != link.Category {
				return nil, fmt.Errorf("category detail identity mismatch for link %d", link.PK)
			}
			category, ok = *link.CategoryDetail, true
			categories[link.Category] = category
		}
		if !ok {
			var err error
			category, err = client.GetPartCategory(ctx, link.Category)
			if err != nil {
				return nil, err
			}
			categories[link.Category] = category
		}
		template, ok := templates[link.Template]
		if link.TemplateDetail != nil {
			if link.TemplateDetail.PK != link.Template {
				return nil, fmt.Errorf("template detail identity mismatch for link %d", link.PK)
			}
			template, ok = *link.TemplateDetail, true
			templates[link.Template] = template
		}
		if !ok {
			var err error
			template, err = client.GetParameterTemplate(ctx, link.Template)
			if err != nil {
				return nil, err
			}
			templates[link.Template] = template
		}
		record, err := categoryDefaultRecord(link, category, template, requestedCategoryID)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func enrichCategoryDefault(ctx context.Context, client CategoryParameterDefaultClient, link inventree.CategoryParameterTemplate, requestedCategoryID int) (CategoryParameterDefaultRecord, error) {
	var category inventree.Category
	var template inventree.ParameterTemplate
	var err error
	if link.CategoryDetail != nil {
		if link.CategoryDetail.PK != link.Category {
			return CategoryParameterDefaultRecord{}, fmt.Errorf("category detail identity mismatch for link %d", link.PK)
		}
		category = *link.CategoryDetail
	} else {
		category, err = client.GetPartCategory(ctx, link.Category)
	}
	if err != nil {
		return CategoryParameterDefaultRecord{}, err
	}
	if link.TemplateDetail != nil {
		if link.TemplateDetail.PK != link.Template {
			return CategoryParameterDefaultRecord{}, fmt.Errorf("template detail identity mismatch for link %d", link.PK)
		}
		template = *link.TemplateDetail
	} else {
		template, err = client.GetParameterTemplate(ctx, link.Template)
	}
	if err != nil {
		return CategoryParameterDefaultRecord{}, err
	}
	return categoryDefaultRecord(link, category, template, requestedCategoryID)
}

func categoryDefaultRecord(link inventree.CategoryParameterTemplate, category inventree.Category, template inventree.ParameterTemplate, requestedCategoryID int) (CategoryParameterDefaultRecord, error) {
	if link.PK <= 0 || category.PK != link.Category || template.PK != link.Template {
		return CategoryParameterDefaultRecord{}, fmt.Errorf("category parameter default detail identity mismatch for link %d", link.PK)
	}
	return CategoryParameterDefaultRecord{LinkID: link.PK, CategoryID: link.Category, CategoryName: category.Name, TemplateID: link.Template, TemplateName: template.Name, DefaultValue: link.DefaultValue, RequestedCategoryID: requestedCategoryID, Inherited: requestedCategoryID > 0 && link.Category != requestedCategoryID}, nil
}

func duplicateCreateCategoryDefaultClarification(link inventree.CategoryParameterTemplate, input CreateCategoryParameterDefaultInput) (*mcp.CallToolResult, CategoryParameterDefaultOutput, error) {
	record := CategoryParameterDefaultRecord{LinkID: link.PK, CategoryID: link.Category, TemplateID: link.Template, DefaultValue: link.DefaultValue}
	clarification := NewClarification("Should the existing direct category-default link be updated instead?", "link_id", "the category and template already have a direct link; create cannot accept link_id", "link_id", true, []ClarificationCandidate{categoryDefaultCandidate(record)}, map[string]any{"link_id": link.PK, "default_value": input.DefaultValue})
	clarification.RetryTool = UpdateCategoryParameterDefaultToolName
	return TextResult(StatusClarificationRequired), CategoryParameterDefaultOutput{Status: StatusClarificationRequired, Record: &record, Clarification: &clarification}, nil
}

func duplicateUpdateCategoryDefaultClarification(link inventree.CategoryParameterTemplate, requestedLinkID, categoryID, templateID int) (*mcp.CallToolResult, CategoryParameterDefaultOutput, error) {
	record := CategoryParameterDefaultRecord{LinkID: link.PK, CategoryID: link.Category, TemplateID: link.Template, DefaultValue: link.DefaultValue}
	clarification := NewClarification("Which direct category-default link should remain?", "link_id", fmt.Sprintf("link %d cannot move to category %d and template %d because that pair belongs to another direct link; choose different IDs or review deletion of the conflicting link", requestedLinkID, categoryID, templateID), "link_id", true, []ClarificationCandidate{categoryDefaultCandidate(record)}, map[string]any{"link_id": link.PK, "confirm": false})
	clarification.RetryTool = DeleteCategoryParameterDefaultToolName
	return TextResult(StatusClarificationRequired), CategoryParameterDefaultOutput{Status: StatusClarificationRequired, Record: &record, Clarification: &clarification}, nil
}

func categoryDefaultCandidate(record CategoryParameterDefaultRecord) ClarificationCandidate {
	label := strings.TrimSpace(record.TemplateName)
	if label == "" {
		label = "template " + strconv.Itoa(record.TemplateID)
	}
	return ClarificationCandidate{ID: strconv.Itoa(record.LinkID), Label: label, Summary: fmt.Sprintf("category %d, default %q", record.CategoryID, record.DefaultValue), Fields: map[string]any{"link_id": record.LinkID, "category_id": record.CategoryID, "template_id": record.TemplateID, "default_value": record.DefaultValue}}
}

func validCategoryDefaultValue(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= 500
}

func categoryDefaultClarification(question, field, reason, retry string, retryValues map[string]any) (*mcp.CallToolResult, CategoryParameterDefaultsOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, retryValues)
	return TextResult(StatusClarificationRequired), CategoryParameterDefaultsOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func categoryDefaultWriteClarification(question, field, reason, retry string, retryValues map[string]any) (*mcp.CallToolResult, CategoryParameterDefaultOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, retryValues)
	return TextResult(StatusClarificationRequired), CategoryParameterDefaultOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}
