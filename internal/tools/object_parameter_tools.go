package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	objectParameterPageSize = 100
	maxObjectParameterScan  = 1000

	objectParameterDeletePlanLifetime               = 5 * time.Minute
	objectParameterDeletePlanMaxEntries             = 4096
	objectParameterDeletePlanMaxEntriesPerPrincipal = 64
)

// objectParameterModelTypes lists F-S64's supported non-part-row object
// families. part.part remains under the existing search_part_parameters /
// delete_part_parameter tools; build.build and the order.* variants beyond
// order.purchaseorder remain out of scope for this story.
const objectParameterModelTypes = "company.company, company.manufacturerpart, company.supplierpart, order.purchaseorder, part.partcategory, or stock.stocklocation"

var errObjectParameterScanLimit = errors.New("object-parameter scan safety limit exceeded")

func validObjectParameterModelType(modelType string) bool {
	switch modelType {
	case "company.company", "company.manufacturerpart", "company.supplierpart", "order.purchaseorder", "part.partcategory", "stock.stocklocation":
		return true
	default:
		return false
	}
}

type ObjectParameterSearchClient interface {
	SearchObjectParametersPage(context.Context, inventree.ObjectParameterQuery) (inventree.PartParameterPage, error)
	SearchParameterTemplates(context.Context, inventree.SearchQuery) ([]inventree.ParameterTemplate, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
}

type ObjectParameterWriteClient interface {
	SearchObjectParametersPage(context.Context, inventree.ObjectParameterQuery) (inventree.PartParameterPage, error)
	SearchTemplateParametersPage(context.Context, inventree.TemplateParameterQuery) (inventree.PartParameterPage, error)
	SearchParameterTemplates(context.Context, inventree.SearchQuery) ([]inventree.ParameterTemplate, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
	CreatePartParameter(context.Context, inventree.ParameterCreate) (inventree.Parameter, error)
	UpdatePartParameter(context.Context, int, inventree.PatchFields) (inventree.Parameter, error)
}

type ObjectParameterDeleteClient interface {
	GetPartParameter(context.Context, int) (inventree.Parameter, error)
	GetParameterTemplate(context.Context, int) (inventree.ParameterTemplate, error)
	DeletePartParameter(context.Context, int) error
}

type SearchObjectParametersInput struct {
	ModelType    string  `json:"model_type" jsonschema:"Qualified InvenTree app.model value being searched. One of company.company, company.manufacturerpart, company.supplierpart, order.purchaseorder, part.partcategory, or stock.stocklocation."`
	ModelID      int     `json:"model_id,omitempty" jsonschema:"Optional exact object primary key filter."`
	TemplateID   int     `json:"template_id,omitempty" jsonschema:"Optional stable parameter-template primary key."`
	TemplateName string  `json:"template_name,omitempty" jsonschema:"Optional exact template name; ambiguous matches require template_id."`
	Value        *string `json:"value,omitempty" jsonschema:"Optional exact parameter value filter, including an explicitly empty value."`
	Limit        int     `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset       int     `json:"offset,omitempty" jsonschema:"Pagination offset applied after all exact filters."`
}

type ObjectParameterResult struct {
	inventree.WebLinkFields
	ParameterID  int     `json:"parameter_id"`
	ModelType    string  `json:"model_type"`
	ModelID      int     `json:"model_id"`
	TemplateID   int     `json:"template_id"`
	TemplateName string  `json:"template_name"`
	Units        *string `json:"units,omitempty"`
	Value        string  `json:"value"`
}

type CreateObjectParameterInput struct {
	ModelType    string  `json:"model_type" jsonschema:"Qualified InvenTree app.model value of the target object. One of company.company, company.manufacturerpart, company.supplierpart, order.purchaseorder, part.partcategory, or stock.stocklocation."`
	ModelID      int     `json:"model_id" jsonschema:"Stable primary key of the target object."`
	TemplateID   int     `json:"template_id,omitempty" jsonschema:"Existing parameter-template primary key."`
	TemplateName string  `json:"template_name,omitempty" jsonschema:"Existing parameter-template name when template_id is not supplied."`
	Value        *string `json:"value" jsonschema:"Explicit new parameter value, including an explicit empty string. When the object already has a value under this template it is updated instead of duplicated."`
}

type DeleteObjectParameterInput struct {
	ParameterID int    `json:"parameter_id" jsonschema:"Stable primary key of exactly one non-part object parameter row."`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"Required true after reviewing the exact current row, together with plan_hash."`
	PlanHash    string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token from the preview."`
}

// ObjectParameterDeletePlan binds the exact current parameter row and its
// template. Any change to either between preview and confirm changes this
// plan's digest, so a stale token is rejected rather than silently deleting
// against state the operator never reviewed.
type ObjectParameterDeletePlan struct {
	Action    string                      `json:"action"`
	Parameter inventree.Parameter         `json:"parameter"`
	Template  inventree.ParameterTemplate `json:"template"`
}

type ObjectParameterDeleteOutput struct {
	Status        string                 `json:"status"`
	Record        *ObjectParameterResult `json:"record,omitempty"`
	PlanHash      string                 `json:"plan_hash,omitempty"`
	Verified      bool                   `json:"verified,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
}

type objectParameterDeleteConfirmation struct {
	digest      string
	principal   string
	parameterID int
	expiresAt   time.Time
}

type objectParameterDeletePlanStore struct {
	mu                     sync.Mutex
	entries                map[string]objectParameterDeleteConfirmation
	now                    func() time.Time
	token                  func() (string, error)
	principal              func(context.Context) string
	maxEntries             int
	maxEntriesPerPrincipal int
}

func newObjectParameterDeletePlanStore(now func() time.Time, token func() (string, error)) *objectParameterDeletePlanStore {
	return &objectParameterDeletePlanStore{
		entries: map[string]objectParameterDeleteConfirmation{}, now: now, token: token, principal: stockPlanPrincipal,
		maxEntries: objectParameterDeletePlanMaxEntries, maxEntriesPerPrincipal: objectParameterDeletePlanMaxEntriesPerPrincipal,
	}
}

func (s *objectParameterDeletePlanStore) issue(ctx context.Context, plan ObjectParameterDeletePlan) (string, error) {
	digest, err := objectParameterDeletePlanDigest(plan)
	if err != nil {
		return "", err
	}
	token, err := s.token()
	if err != nil {
		return "", err
	}
	now, principal := s.now(), s.principal(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	principalEntries := 0
	for key, entry := range s.entries {
		if !now.Before(entry.expiresAt) || entry.principal == principal && entry.parameterID == plan.Parameter.PK {
			delete(s.entries, key)
			continue
		}
		if entry.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errors.New("too many outstanding object-parameter deletion confirmation plans")
	}
	s.entries[token] = objectParameterDeleteConfirmation{digest: digest, principal: principal, parameterID: plan.Parameter.PK, expiresAt: now.Add(objectParameterDeletePlanLifetime)}
	return token, nil
}

func (s *objectParameterDeletePlanStore) consume(ctx context.Context, token string, plan ObjectParameterDeletePlan) bool {
	digest, err := objectParameterDeletePlanDigest(plan)
	if err != nil {
		return false
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			delete(s.entries, key)
		}
	}
	entry, ok := s.entries[token]
	if !ok || entry.digest != digest || entry.principal != s.principal(ctx) || entry.parameterID != plan.Parameter.PK || !now.Before(entry.expiresAt) {
		return false
	}
	delete(s.entries, token)
	return true
}

func objectParameterDeletePlanDigest(plan ObjectParameterDeletePlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registerObjectParameterWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.objectParameterDeletePlanStore == nil {
		deps.objectParameterDeletePlanStore = newObjectParameterDeletePlanStore(time.Now, randomStockPlanToken)
	}
	addWriteTool(server, deps, CreateObjectParameterToolName, "Create object parameter", "Creates or updates one parameter value on a purchase order, stock location, company, supplier part, manufacturer part, or part category, honoring the template's uniqueness policy.", createObjectParameter(deps))
	addWriteTool(server, deps, DeleteObjectParameterToolName, "Delete object parameter", "Previews and confirms deleting one non-part object parameter row by stable ID.", deleteObjectParameter(deps))
}

func searchObjectParameterValues(deps Dependencies) mcp.ToolHandlerFor[SearchObjectParametersInput, LookupOutput[ObjectParameterResult]] {
	return LookupHandler[ObjectParameterSearchClient, SearchObjectParametersInput, LookupOutput[ObjectParameterResult]](deps, SearchObjectParametersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ObjectParameterSearchClient, input SearchObjectParametersInput) (*mcp.CallToolResult, LookupOutput[ObjectParameterResult], error) {
			modelType := strings.TrimSpace(input.ModelType)
			if !validObjectParameterModelType(modelType) {
				return objectParameterSearchClarification("Which supported object type should be searched?", "model_type", "model_type must be one of "+objectParameterModelTypes, input)
			}
			if input.ModelID < 0 || input.TemplateID < 0 || input.Offset < 0 {
				return objectParameterSearchClarification("Which valid search filters should be used?", "filters", "IDs and offset cannot be negative", input)
			}
			limit := NormalizeLookupLimit(input.Limit)
			if input.Offset > maxObjectParameterScan-limit {
				return objectParameterSearchClarification("Which result page should be searched?", "offset", "offset plus limit cannot exceed the 1,000-row scan bound", input)
			}
			templateID, result, output, ok, err := resolveObjectSearchTemplate(ctx, client, input)
			if err != nil || !ok {
				return result, output, err
			}
			enriched := make([]ObjectParameterResult, 0, input.Offset+limit)
			templates := map[int]inventree.ParameterTemplate{}
			serverOffset := 0
			hasMore := true
			for hasMore && serverOffset < maxObjectParameterScan {
				pageLimit := min(objectParameterPageSize, maxObjectParameterScan-serverOffset)
				page, err := client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{
					ModelType: modelType, ModelID: input.ModelID, TemplateID: templateID,
					Search: objectParameterValueSearch(input.Value), Limit: pageLimit, Offset: serverOffset,
				})
				if err != nil {
					return nil, LookupOutput[ObjectParameterResult]{}, err
				}
				for _, row := range page.Results {
					if row.ModelType != modelType || (input.Value != nil && row.Data != *input.Value) {
						continue
					}
					template, ok := templates[row.Template]
					if !ok {
						template, err = client.GetParameterTemplate(ctx, row.Template)
						if err != nil {
							return nil, LookupOutput[ObjectParameterResult]{}, err
						}
						if template.PK != row.Template {
							return nil, LookupOutput[ObjectParameterResult]{}, fmt.Errorf("parameter template detail identity mismatch: requested %d, received %d", row.Template, template.PK)
						}
						templates[row.Template] = template
					}
					enriched = append(enriched, objectParameterResult(row, template))
				}
				serverOffset += len(page.Results)
				hasMore = page.HasMore
				if len(page.Results) == 0 {
					hasMore = false
				}
			}
			if hasMore {
				return objectParameterSearchClarification("Which narrower object-parameter search should be used?", "filters", "search exceeded the 1,000-row scan bound before complete row-ID ordering could be established", input)
			}
			slices.SortFunc(enriched, func(a, b ObjectParameterResult) int { return a.ParameterID - b.ParameterID })
			enriched = paginateObjectParameters(enriched, input.Offset, limit)
			return listOutput(enriched, nil)
		})
}

func resolveObjectSearchTemplate(ctx context.Context, client ObjectParameterSearchClient, input SearchObjectParametersInput) (int, *mcp.CallToolResult, LookupOutput[ObjectParameterResult], bool, error) {
	name := strings.TrimSpace(input.TemplateName)
	if input.TemplateID > 0 {
		if name == "" {
			return input.TemplateID, nil, LookupOutput[ObjectParameterResult]{}, true, nil
		}
		template, err := client.GetParameterTemplate(ctx, input.TemplateID)
		if err != nil {
			return 0, nil, LookupOutput[ObjectParameterResult]{}, false, err
		}
		if !templateNameMatches(template.Name, name) {
			result, output, err := objectParameterSearchClarification("Which parameter template should be searched?", "template", "template_id does not match template_name", input)
			return 0, result, output, false, err
		}
		return input.TemplateID, nil, LookupOutput[ObjectParameterResult]{}, true, nil
	}
	if name == "" {
		return 0, nil, LookupOutput[ObjectParameterResult]{}, true, nil
	}
	templates, err := client.SearchParameterTemplates(ctx, inventree.SearchQuery{Search: name, Limit: MaxLookupLimit})
	if err != nil {
		return 0, nil, LookupOutput[ObjectParameterResult]{}, false, err
	}
	exact := make([]inventree.ParameterTemplate, 0, len(templates))
	for _, template := range templates {
		if templateNameMatches(template.Name, name) {
			exact = append(exact, template)
		}
	}
	if len(exact) == 1 {
		return exact[0].PK, nil, LookupOutput[ObjectParameterResult]{}, true, nil
	}
	reason := "no parameter template has that exact name"
	if len(exact) > 1 {
		reason = "multiple parameter templates have that exact name"
	}
	clarification := NewClarification("Which parameter template should be searched?", "template", reason, "template_id", len(exact) == 0, templateCandidates(exact, nil, nil), objectParameterSearchRetryValues(input))
	return 0, TextResult(StatusClarificationRequired), LookupOutput[ObjectParameterResult]{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
}

func objectParameterResult(parameter inventree.Parameter, template inventree.ParameterTemplate) ObjectParameterResult {
	return ObjectParameterResult{ParameterID: parameter.PK, ModelType: parameter.ModelType, ModelID: parameter.ModelID, TemplateID: template.PK, TemplateName: template.Name, Units: template.Units, Value: parameter.Data}
}

func paginateObjectParameters(records []ObjectParameterResult, offset, limit int) []ObjectParameterResult {
	if offset >= len(records) {
		return nil
	}
	end := min(len(records), offset+limit)
	return records[offset:end]
}

func objectParameterValueSearch(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func objectParameterSearchClarification(question, field, reason string, input SearchObjectParametersInput) (*mcp.CallToolResult, LookupOutput[ObjectParameterResult], error) {
	clarification := NewClarification(question, field, reason, field, true, nil, objectParameterSearchRetryValues(input))
	return TextResult(StatusClarificationRequired), LookupOutput[ObjectParameterResult]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func objectParameterSearchRetryValues(input SearchObjectParametersInput) map[string]any {
	return map[string]any{"model_type": input.ModelType, "model_id": input.ModelID, "template_id": input.TemplateID, "template_name": input.TemplateName, "value": input.Value, "limit": input.Limit, "offset": input.Offset}
}

func createObjectParameter(deps Dependencies) mcp.ToolHandlerFor[CreateObjectParameterInput, WriteRecordOutput[ObjectParameterResult]] {
	return LookupHandler[ObjectParameterWriteClient, CreateObjectParameterInput, WriteRecordOutput[ObjectParameterResult]](deps, CreateObjectParameterToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ObjectParameterWriteClient, input CreateObjectParameterInput) (*mcp.CallToolResult, WriteRecordOutput[ObjectParameterResult], error) {
			modelType := strings.TrimSpace(input.ModelType)
			if !validObjectParameterModelType(modelType) {
				return hardClarification[ObjectParameterResult]("Which supported object type should receive this parameter?", "model_type", "model_type must be one of "+objectParameterModelTypes, "model_type", objectParameterWriteRetry(input))
			}
			if input.ModelID <= 0 {
				return hardClarification[ObjectParameterResult]("Which object should receive this parameter?", "model_id", "model_id must be positive", "model_id", objectParameterWriteRetry(input))
			}
			if input.Value == nil {
				return hardClarification[ObjectParameterResult]("Which explicit parameter value should be set?", "value", "value must be explicitly supplied, including for an empty value", "value", objectParameterWriteRetry(input))
			}
			if len(*input.Value) > 500 {
				return hardClarification[ObjectParameterResult]("Which schema-valid parameter value should be set?", "value", "value exceeds the InvenTree 500-character parameter data limit", "value", objectParameterWriteRetry(input))
			}
			template, result, output, ok, err := resolveObjectWriteTemplate(ctx, client, modelType, input)
			if err != nil || !ok {
				return result, output, err
			}
			existing, err := client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: modelType, ModelID: input.ModelID, TemplateID: template.PK, Limit: 3})
			if err != nil {
				return nil, WriteRecordOutput[ObjectParameterResult]{}, err
			}
			if len(existing.Results) > 1 {
				clarification := NewClarification("Which existing object parameter should be updated?", "parameter_id", "multiple existing parameter rows already use this template on this object", "parameter_id", false, objectParameterCandidates(existing.Results, template), objectParameterWriteRetry(input))
				return TextResult(StatusClarificationRequired), WriteRecordOutput[ObjectParameterResult]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			if template.Unique != inventree.ParameterUniquenessNone {
				excludeID := 0
				if len(existing.Results) == 1 {
					excludeID = existing.Results[0].PK
				}
				conflict, err := objectParameterUniquenessConflict(ctx, client, modelType, template.PK, template.Unique, *input.Value, excludeID)
				if err != nil {
					if errors.Is(err, errObjectParameterScanLimit) {
						return hardClarification[ObjectParameterResult]("How should this completeness-sensitive uniqueness scan be narrowed or reviewed?", "value", err.Error(), "value", objectParameterWriteRetry(input))
					}
					return nil, WriteRecordOutput[ObjectParameterResult]{}, err
				}
				if conflict != nil {
					clarification := NewClarification("Which value should be used to satisfy this template's uniqueness policy?", "value", fmt.Sprintf("value already exists on parameter row %d under this template's uniqueness policy", conflict.PK), "value", true, []ClarificationCandidate{candidateFor(objectParameterResult(*conflict, template))}, objectParameterWriteRetry(input))
					return TextResult(StatusClarificationRequired), WriteRecordOutput[ObjectParameterResult]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
				}
			}
			var record inventree.Parameter
			if len(existing.Results) == 1 {
				record, err = client.UpdatePartParameter(ctx, existing.Results[0].PK, inventree.PatchFields{"data": inventree.Set(*input.Value)})
			} else {
				record, err = client.CreatePartParameter(ctx, inventree.ParameterCreate{Template: template.PK, ModelType: modelType, ModelID: input.ModelID, Data: *input.Value})
			}
			if err != nil {
				return writeRecordOutput(ObjectParameterResult{}, err)
			}
			if record.ModelType != modelType || record.ModelID != input.ModelID || record.Template != template.PK || record.Data != *input.Value {
				return TextResult(StatusPartialFailure), WriteRecordOutput[ObjectParameterResult]{Status: StatusPartialFailure, RecoveryPlan: "The written row does not match every requested field; call search_object_parameters before retrying."}, nil
			}
			return TextResult(StatusOK), WriteRecordOutput[ObjectParameterResult]{Status: StatusOK, Record: objectParameterResult(record, template)}, nil
		})
}

func resolveObjectWriteTemplate(ctx context.Context, client ObjectParameterWriteClient, modelType string, input CreateObjectParameterInput) (inventree.ParameterTemplate, *mcp.CallToolResult, WriteRecordOutput[ObjectParameterResult], bool, error) {
	name := strings.TrimSpace(input.TemplateName)
	if input.TemplateID > 0 {
		template, err := client.GetParameterTemplate(ctx, input.TemplateID)
		if err != nil {
			if isNotFound(err) {
				result, output, clarifyErr := hardClarification[ObjectParameterResult]("Which existing parameter template should be used?", "template_id", "template_id does not exist", "template_id", objectParameterWriteRetry(input))
				return inventree.ParameterTemplate{}, result, output, false, clarifyErr
			}
			return inventree.ParameterTemplate{}, nil, WriteRecordOutput[ObjectParameterResult]{}, false, err
		}
		if template.PK != input.TemplateID {
			return inventree.ParameterTemplate{}, nil, WriteRecordOutput[ObjectParameterResult]{}, false, fmt.Errorf("parameter template identity mismatch: requested %d, received %d", input.TemplateID, template.PK)
		}
		if name != "" && !templateNameMatches(template.Name, name) {
			result, output, err := hardClarification[ObjectParameterResult]("Which parameter template should be used?", "template_id", "template_id does not match the supplied template name", "template_id", objectParameterWriteRetry(input))
			return inventree.ParameterTemplate{}, result, output, false, err
		}
		if !objectParameterTemplateCompatible(template, modelType) {
			result, output, err := hardClarification[ObjectParameterResult]("Which enabled compatible parameter template should be used?", "template_id", "template must be enabled and unrestricted or restricted to "+modelType, "template_id", objectParameterWriteRetry(input))
			return inventree.ParameterTemplate{}, result, output, false, err
		}
		return template, nil, WriteRecordOutput[ObjectParameterResult]{}, true, nil
	}
	if name == "" {
		result, output, err := hardClarification[ObjectParameterResult]("Which existing parameter template should be used?", "template", "provide template_id or template_name", "template_id", objectParameterWriteRetry(input))
		return inventree.ParameterTemplate{}, result, output, false, err
	}
	templates, err := client.SearchParameterTemplates(ctx, inventree.SearchQuery{Search: name, Limit: MaxLookupLimit})
	if err != nil {
		return inventree.ParameterTemplate{}, nil, WriteRecordOutput[ObjectParameterResult]{}, false, err
	}
	compatible := make([]inventree.ParameterTemplate, 0)
	for _, template := range templates {
		if templateNameMatches(template.Name, name) && objectParameterTemplateCompatible(template, modelType) {
			compatible = append(compatible, template)
		}
	}
	if len(compatible) == 1 {
		return compatible[0], nil, WriteRecordOutput[ObjectParameterResult]{}, true, nil
	}
	reason := fmt.Sprintf("no enabled template named %q is compatible with %s", name, modelType)
	if len(compatible) > 1 {
		reason = "multiple enabled compatible templates match this name"
	}
	clarification := NewClarification("Which existing compatible parameter template should be used?", "template", reason, "template_id", len(compatible) == 0, candidatesFor(compatible), objectParameterWriteRetry(input))
	return inventree.ParameterTemplate{}, TextResult(StatusClarificationRequired), WriteRecordOutput[ObjectParameterResult]{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
}

func objectParameterTemplateCompatible(template inventree.ParameterTemplate, modelType string) bool {
	return template.Enabled && (template.ModelType == nil || *template.ModelType == "" || *template.ModelType == modelType)
}

func objectParameterWriteRetry(input CreateObjectParameterInput) map[string]any {
	return map[string]any{"model_type": input.ModelType, "model_id": input.ModelID, "template_id": input.TemplateID, "template_name": input.TemplateName, "value": input.Value}
}

func objectParameterCandidates(rows []inventree.Parameter, template inventree.ParameterTemplate) []ClarificationCandidate {
	candidates := make([]ClarificationCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, candidateFor(objectParameterResult(row, template)))
	}
	return candidates
}

// objectParameterUniquenessConflict scans every other row sharing templateID
// under the scope implied by unique (model-type-scoped or global) for a
// value match, bounded and failing closed so an incomplete scan can never
// silently approve a value that upstream would have rejected as a conflict.
func objectParameterUniquenessConflict(ctx context.Context, client ObjectParameterWriteClient, modelType string, templateID int, unique inventree.ParameterUniqueness, value string, excludeID int) (*inventree.Parameter, error) {
	offset := 0
	scanned := 0
	for {
		var page inventree.PartParameterPage
		var err error
		if unique == inventree.ParameterUniquenessGlobal {
			page, err = client.SearchTemplateParametersPage(ctx, inventree.TemplateParameterQuery{TemplateID: templateID, Limit: objectParameterPageSize, Offset: offset})
		} else {
			page, err = client.SearchObjectParametersPage(ctx, inventree.ObjectParameterQuery{ModelType: modelType, TemplateID: templateID, Limit: objectParameterPageSize, Offset: offset})
		}
		if err != nil {
			return nil, err
		}
		if page.Count > maxObjectParameterScan {
			return nil, errObjectParameterScanLimit
		}
		for i := range page.Results {
			row := page.Results[i]
			if row.PK == excludeID {
				continue
			}
			if row.Data == value {
				return &row, nil
			}
		}
		offset += len(page.Results)
		scanned += len(page.Results)
		if !page.HasMore || len(page.Results) == 0 {
			return nil, nil
		}
		if scanned >= maxObjectParameterScan {
			return nil, errObjectParameterScanLimit
		}
	}
}

func deleteObjectParameter(deps Dependencies) mcp.ToolHandlerFor[DeleteObjectParameterInput, ObjectParameterDeleteOutput] {
	return LookupHandler[ObjectParameterDeleteClient, DeleteObjectParameterInput, ObjectParameterDeleteOutput](deps, DeleteObjectParameterToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client ObjectParameterDeleteClient, input DeleteObjectParameterInput) (*mcp.CallToolResult, ObjectParameterDeleteOutput, error) {
			if input.ParameterID <= 0 {
				clarification := NewClarification("Which object parameter row should be deleted?", "parameter_id", "parameter_id must be positive", "parameter_id", true, nil, map[string]any{"parameter_id": input.ParameterID})
				return TextResult(StatusClarificationRequired), ObjectParameterDeleteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			existing, err := client.GetPartParameter(ctx, input.ParameterID)
			if err != nil {
				if isNotFound(err) {
					return TextResult(StatusNotFound), ObjectParameterDeleteOutput{Status: StatusNotFound}, nil
				}
				return nil, ObjectParameterDeleteOutput{}, err
			}
			if existing.PK != input.ParameterID {
				return nil, ObjectParameterDeleteOutput{}, fmt.Errorf("parameter detail identity mismatch: requested %d, received %d", input.ParameterID, existing.PK)
			}
			if existing.ModelType == "part.part" {
				clarification := NewClarification("Which tool should delete this parameter row?", "parameter_id", "parameter row belongs to part.part; use delete_part_parameter instead", "parameter_id", true, nil, map[string]any{"parameter_id": input.ParameterID})
				return TextResult(StatusClarificationRequired), ObjectParameterDeleteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			if !validObjectParameterModelType(existing.ModelType) {
				clarification := NewClarification("Which object parameter row should be deleted?", "parameter_id", fmt.Sprintf("parameter row %d belongs to unsupported model type %q; only %s are supported", existing.PK, existing.ModelType, objectParameterModelTypes), "parameter_id", true, nil, map[string]any{"parameter_id": input.ParameterID})
				return TextResult(StatusClarificationRequired), ObjectParameterDeleteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			template, err := client.GetParameterTemplate(ctx, existing.Template)
			if err != nil {
				return nil, ObjectParameterDeleteOutput{}, err
			}
			record := objectParameterResult(existing, template)
			plan := ObjectParameterDeletePlan{Action: DeleteObjectParameterToolName, Parameter: existing, Template: template}
			if !input.Confirm {
				return issueObjectParameterDeletePlan(ctx, deps.objectParameterDeletePlanStore, plan, record)
			}
			if deps.objectParameterDeletePlanStore == nil || input.PlanHash == "" || !deps.objectParameterDeletePlanStore.consume(ctx, input.PlanHash, plan) {
				return objectParameterDeleteStaleClarification(record)
			}
			if err := client.DeletePartParameter(ctx, existing.PK); err != nil {
				return nil, ObjectParameterDeleteOutput{}, err
			}
			_, err = client.GetPartParameter(ctx, existing.PK)
			if err == nil {
				return nil, ObjectParameterDeleteOutput{}, fmt.Errorf("parameter row %d still exists after deletion", existing.PK)
			}
			if !isNotFound(err) {
				return nil, ObjectParameterDeleteOutput{}, fmt.Errorf("verify parameter row %d deletion: %w", existing.PK, err)
			}
			return TextResult(StatusOK), ObjectParameterDeleteOutput{Status: StatusOK, Record: &record, Verified: true}, nil
		})
}

func issueObjectParameterDeletePlan(ctx context.Context, store *objectParameterDeletePlanStore, plan ObjectParameterDeletePlan, record ObjectParameterResult) (*mcp.CallToolResult, ObjectParameterDeleteOutput, error) {
	if store == nil {
		return nil, ObjectParameterDeleteOutput{}, errors.New("object-parameter deletion confirmation store is unavailable")
	}
	token, err := store.issue(ctx, plan)
	if err != nil {
		return nil, ObjectParameterDeleteOutput{}, err
	}
	clarification := NewClarification("Delete this exact object parameter row?", "confirmation", "review the exact current parameter row and template, then provide confirm:true with this plan_hash", "plan_hash", false, []ClarificationCandidate{candidateFor(record)}, map[string]any{"parameter_id": plan.Parameter.PK, "confirm": true, "plan_hash": token})
	return TextResult(StatusClarificationRequired), ObjectParameterDeleteOutput{Status: StatusClarificationRequired, Record: &record, PlanHash: token, Clarification: &clarification}, nil
}

func objectParameterDeleteStaleClarification(record ObjectParameterResult) (*mcp.CallToolResult, ObjectParameterDeleteOutput, error) {
	clarification := NewClarification("Which current deletion plan should authorize this object-parameter deletion?", "confirmation", "the plan is missing, stale, expired, reused, or belongs to another principal; preview the current row again", "plan_hash", false, nil, map[string]any{"parameter_id": record.ParameterID})
	return TextResult(StatusClarificationRequired), ObjectParameterDeleteOutput{Status: StatusClarificationRequired, Record: &record, Clarification: &clarification}, nil
}
