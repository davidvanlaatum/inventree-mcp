package tools

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	stocktakeGenerationTimeout          = 30 * time.Second
	stocktakeGenerationDefaultWait      = 10 * time.Second
	stocktakeGenerationInterval         = 500 * time.Millisecond
	stocktakeGenerationRetryAfter       = 1
	stocktakeTaskLifetime               = 24 * time.Hour
	stocktakeTaskMaxEntries             = 4096
	stocktakeTaskMaxEntriesPerPrincipal = 64
)

type StocktakeGenerationClient interface {
	GeneratePartStocktake(context.Context, inventree.PartStocktakeGenerate) (inventree.PartStocktakeGenerate, error)
	GetDataOutput(context.Context, int) (inventree.DataOutput, error)
	DownloadDataOutput(context.Context, string, int64) (inventree.DownloadedDataOutput, error)
}

type GenerateStocktakeInput struct {
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Return the complete current generation plan without enqueueing work."`
	Confirm        bool   `json:"confirm,omitempty" jsonschema:"Required true to enqueue the reviewed plan."`
	PlanHash       string `json:"plan_hash,omitempty" jsonschema:"Opaque principal-bound single-use token returned by dry_run."`
	PartID         int    `json:"part_id,omitempty" jsonschema:"Exactly one of part_id, category_id, or location_id."`
	CategoryID     int    `json:"category_id,omitempty" jsonschema:"Exactly one of part_id, category_id, or location_id."`
	LocationID     int    `json:"location_id,omitempty" jsonschema:"Exactly one of part_id, category_id, or location_id."`
	GenerateEntry  bool   `json:"generate_entry,omitempty" jsonschema:"Ask InvenTree to create PartStocktake entries."`
	GenerateReport bool   `json:"generate_report,omitempty" jsonschema:"Ask InvenTree to generate a report artifact."`
}

type PollStocktakeGenerationInput struct {
	TaskID      int `json:"task_id" jsonschema:"Existing InvenTree DataOutput primary key returned by generate_stocktake."`
	WaitSeconds int `json:"wait_seconds,omitempty" jsonschema:"Maximum seconds to poll this existing task during this call; defaults to 10 and is capped at 30."`
}

type StocktakeGenerationPlan struct {
	Selector       string `json:"selector"`
	SelectorID     int    `json:"selector_id"`
	GenerateEntry  bool   `json:"generate_entry"`
	GenerateReport bool   `json:"generate_report"`
}

type StocktakeGenerationOutput struct {
	Status            string                   `json:"status"`
	DryRun            bool                     `json:"dry_run,omitempty"`
	Plan              *StocktakeGenerationPlan `json:"plan,omitempty"`
	PlanHash          string                   `json:"plan_hash,omitempty"`
	Task              *StocktakeTaskOutput     `json:"task,omitempty"`
	Report            *DownloadOutput          `json:"report,omitempty"`
	RetryAfterSeconds int                      `json:"retry_after_seconds,omitempty"`
	Clarification     *ClarificationResponse   `json:"clarification,omitempty"`
	RecoveryPlan      string                   `json:"recovery_plan,omitempty"`
}

type StocktakeTaskOutput struct {
	PK              int    `json:"pk"`
	Created         string `json:"created"`
	Total           int    `json:"total"`
	Progress        int    `json:"progress"`
	Complete        bool   `json:"complete"`
	OutputType      string `json:"output_type,omitempty"`
	TemplateName    string `json:"template_name,omitempty"`
	OutputAvailable bool   `json:"output_available,omitempty"`
	HasErrors       bool   `json:"has_errors,omitempty"`
}

type stocktakePlanStore struct {
	mu                     sync.Mutex
	entries                map[string]stocktakePlanEntry
	now                    func() time.Time
	token                  func() (string, error)
	maxEntries             int
	maxEntriesPerPrincipal int
}

type stocktakePlanEntry struct {
	digest    string
	principal string
	expiresAt time.Time
}

type stocktakeTaskStore struct {
	mu                     sync.Mutex
	entries                map[int]stocktakeTaskEntry
	now                    func() time.Time
	principal              func(context.Context) string
	reservations           map[string]int
	maxEntries             int
	maxEntriesPerPrincipal int
}

type stocktakeTaskEntry struct {
	principal      string
	reportRequired bool
	expiresAt      time.Time
}

func newStocktakeTaskStore(now func() time.Time) *stocktakeTaskStore {
	return &stocktakeTaskStore{entries: map[int]stocktakeTaskEntry{}, now: now, principal: stockPlanPrincipal, reservations: map[string]int{}, maxEntries: stocktakeTaskMaxEntries, maxEntriesPerPrincipal: stocktakeTaskMaxEntriesPerPrincipal}
}

func (s *stocktakeTaskStore) removeExpired(now time.Time) {
	for id, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			delete(s.entries, id)
		}
	}
}

func (s *stocktakeTaskStore) principalCount(principal string) int {
	count := 0
	for _, entry := range s.entries {
		if entry.principal == principal {
			count++
		}
	}
	return count
}

func (s *stocktakeTaskStore) reservedTotal() int {
	total := 0
	for _, count := range s.reservations {
		total += count
	}
	return total
}

func (s *stocktakeTaskStore) reserve(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeExpired(s.now())
	principal := s.principal(ctx)
	if len(s.entries)+s.reservedTotal() >= s.maxEntries || s.principalCount(principal)+s.reservations[principal] >= s.maxEntriesPerPrincipal {
		return errors.New("stocktake task capacity reached; wait for existing handles to expire before starting another generation")
	}
	s.reservations[principal]++
	return nil
}

func (s *stocktakeTaskStore) release(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	principal := s.principal(ctx)
	if s.reservations[principal] > 1 {
		s.reservations[principal]--
	} else {
		delete(s.reservations, principal)
	}
}

func (s *stocktakeTaskStore) bind(ctx context.Context, taskID int, reportRequired bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.removeExpired(now)
	if existing, ok := s.entries[taskID]; ok {
		if existing.principal != s.principal(ctx) {
			return errors.New("stocktake task handle is already bound to another principal")
		}
		return errors.New("InvenTree reused an existing stocktake task ID")
	}
	s.entries[taskID] = stocktakeTaskEntry{principal: s.principal(ctx), reportRequired: reportRequired, expiresAt: now.Add(stocktakeTaskLifetime)}
	s.releaseReservationLocked(s.principal(ctx))
	return nil
}

func (s *stocktakeTaskStore) releaseReservationLocked(principal string) {
	if s.reservations[principal] > 1 {
		s.reservations[principal]--
	} else {
		delete(s.reservations, principal)
	}
}

func (s *stocktakeTaskStore) lookup(ctx context.Context, taskID int) (stocktakeTaskEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[taskID]
	if !ok || !s.now().Before(entry.expiresAt) || entry.principal != s.principal(ctx) {
		if ok && !s.now().Before(entry.expiresAt) {
			delete(s.entries, taskID)
		}
		return stocktakeTaskEntry{}, false
	}
	return entry, true
}

func newStocktakePlanStore(now func() time.Time, token func() (string, error)) *stocktakePlanStore {
	return &stocktakePlanStore{entries: map[string]stocktakePlanEntry{}, now: now, token: token, maxEntries: stockPlanMaxEntries, maxEntriesPerPrincipal: stockPlanMaxEntriesPerPrincipal}
}

func stocktakePlanDigest(plan StocktakeGenerationPlan) (string, error) {
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(h[:]), nil
}

func (s *stocktakePlanStore) issue(ctx context.Context, plan StocktakeGenerationPlan) (string, error) {
	digest, err := stocktakePlanDigest(plan)
	if err != nil {
		return "", err
	}
	token, err := s.token()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	principal := stockPlanPrincipal(ctx)
	principalEntries := 0
	for existing, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			delete(s.entries, existing)
			continue
		}
		if entry.principal == principal {
			principalEntries++
		}
	}
	if len(s.entries) >= s.maxEntries || principalEntries >= s.maxEntriesPerPrincipal {
		return "", errStockPlanCapacity
	}
	s.entries[token] = stocktakePlanEntry{digest: digest, principal: principal, expiresAt: now.Add(stockPlanLifetime)}
	return token, nil
}

func (s *stocktakePlanStore) consume(ctx context.Context, token string, plan StocktakeGenerationPlan) bool {
	digest, err := stocktakePlanDigest(plan)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[token]
	if !ok || !s.now().Before(entry.expiresAt) || entry.digest != digest || entry.principal != stockPlanPrincipal(ctx) {
		return false
	}
	delete(s.entries, token)
	return true
}

func registerStocktakeGenerationTool(server *mcp.Server, deps Dependencies) {
	if deps.stocktakePlanStore == nil {
		deps.stocktakePlanStore = newStocktakePlanStore(time.Now, randomStockPlanToken)
	}
	if deps.stocktakeTaskStore == nil {
		deps.stocktakeTaskStore = newStocktakeTaskStore(time.Now)
	}
	addWriteTool(server, deps, GenerateStocktakeToolName, "Generate stocktake", "Previews or confirms one bounded asynchronous stocktake generation and returns its DataOutput task handle without waiting for completion.", generateStocktake(deps))
}

func registerStocktakePollingTool(server *mcp.Server, deps Dependencies) {
	if deps.stocktakeTaskStore == nil {
		deps.stocktakeTaskStore = newStocktakeTaskStore(time.Now)
	}
	addReadOnlyTool(server, deps, PollStocktakeGenerationToolName, "Poll stocktake generation", "Polls a task handle issued by this workflow for a bounded interval and returns pending, complete, or failed status without starting new work.", pollStocktakeGeneration(deps))
}

func generateStocktake(deps Dependencies) mcp.ToolHandlerFor[GenerateStocktakeInput, StocktakeGenerationOutput] {
	return LookupHandler[StocktakeGenerationClient, GenerateStocktakeInput, StocktakeGenerationOutput](deps, GenerateStocktakeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StocktakeGenerationClient, input GenerateStocktakeInput) (*mcp.CallToolResult, StocktakeGenerationOutput, error) {
			plan, clarification := stocktakeGenerationPlan(input)
			if clarification != nil {
				return TextResult(StatusClarificationRequired), StocktakeGenerationOutput{Status: StatusClarificationRequired, Clarification: clarification}, nil
			}
			if input.DryRun {
				token, err := deps.stocktakePlanStore.issue(ctx, plan)
				if err != nil {
					return nil, StocktakeGenerationOutput{}, err
				}
				return TextResult(StatusOK), StocktakeGenerationOutput{Status: StatusOK, DryRun: true, Plan: &plan, PlanHash: token}, nil
			}
			if !input.Confirm || !deps.stocktakePlanStore.consume(ctx, input.PlanHash, plan) {
				clarification := NewClarification("Which current stocktake generation plan should be confirmed?", "confirmation", "run dry_run:true first, then confirm the complete unchanged selector and generation flags with its single-use plan_hash", "plan_hash", true, nil, map[string]any{"dry_run": true, "part_id": input.PartID, "category_id": input.CategoryID, "location_id": input.LocationID, "generate_entry": input.GenerateEntry, "generate_report": input.GenerateReport})
				return TextResult(StatusClarificationRequired), StocktakeGenerationOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}

			request := inventree.PartStocktakeGenerate{GenerateEntry: plan.GenerateEntry, GenerateReport: plan.GenerateReport}
			switch plan.Selector {
			case "part":
				request.Part = &plan.SelectorID
			case "category":
				request.Category = &plan.SelectorID
			case "location":
				request.Location = &plan.SelectorID
			}
			if err := deps.stocktakeTaskStore.reserve(ctx); err != nil {
				return TextResult(StatusPartialFailure), StocktakeGenerationOutput{Status: StatusPartialFailure, RecoveryPlan: err.Error()}, nil
			}
			queued, err := client.GeneratePartStocktake(ctx, request)
			if err != nil {
				deps.stocktakeTaskStore.release(ctx)
				return TextResult(StatusPartialFailure), StocktakeGenerationOutput{Status: StatusPartialFailure, RecoveryPlan: "The enqueue result is ambiguous. Do not retry or start a new generation yet; inspect InvenTree's task queue or stocktake history, and only prepare a fresh dry-run after proving that no task was accepted."}, nil
			}
			if queued.Output == nil || queued.Output.PK <= 0 {
				deps.stocktakeTaskStore.release(ctx)
				return TextResult(StatusPartialFailure), StocktakeGenerationOutput{Status: StatusPartialFailure, RecoveryPlan: "Generation was accepted without a usable DataOutput ID; inspect InvenTree's stocktake history and task queue before retrying."}, nil
			}
			if err := deps.stocktakeTaskStore.bind(ctx, queued.Output.PK, plan.GenerateReport); err != nil {
				deps.stocktakeTaskStore.release(ctx)
				return TextResult(StatusPartialFailure), StocktakeGenerationOutput{Status: StatusPartialFailure, RecoveryPlan: "InvenTree returned a task ID that cannot be safely bound to this principal; inspect the task queue before retrying."}, nil
			}
			projected := stocktakeTaskOutput(*queued.Output)
			return TextResult(StatusPending), StocktakeGenerationOutput{Status: StatusPending, Plan: &plan, Task: &projected, RetryAfterSeconds: stocktakeGenerationRetryAfter}, nil
		})
}

func pollStocktakeGeneration(deps Dependencies) mcp.ToolHandlerFor[PollStocktakeGenerationInput, StocktakeGenerationOutput] {
	return LookupHandler[StocktakeGenerationClient, PollStocktakeGenerationInput, StocktakeGenerationOutput](deps, PollStocktakeGenerationToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StocktakeGenerationClient, input PollStocktakeGenerationInput) (*mcp.CallToolResult, StocktakeGenerationOutput, error) {
			if input.TaskID <= 0 {
				clarification := NewClarification("Which stocktake generation task should be polled?", "task_id", "task_id must be positive", "task_id", true, nil, map[string]any{})
				return TextResult(StatusClarificationRequired), StocktakeGenerationOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			taskBinding, ok := deps.stocktakeTaskStore.lookup(ctx, input.TaskID)
			if !ok {
				return TextResult(StatusPartialFailure), StocktakeGenerationOutput{Status: StatusPartialFailure, RecoveryPlan: "This task handle is unknown, expired, or belongs to another principal; use a task_id returned by generate_stocktake and do not guess or reuse another task ID."}, nil
			}
			wait := stocktakeGenerationDefaultWait
			if input.WaitSeconds > 0 {
				wait = time.Duration(input.WaitSeconds) * time.Second
			}
			if wait > stocktakeGenerationTimeout {
				wait = stocktakeGenerationTimeout
			}
			task, err := waitForStocktakeOutput(ctx, client, input.TaskID, wait)
			projected := stocktakeTaskOutput(task)
			if errors.Is(err, errStocktakeGenerationTimeout) {
				return TextResult(StatusPending), StocktakeGenerationOutput{Status: StatusPending, Task: &projected, RetryAfterSeconds: stocktakeGenerationRetryAfter}, nil
			}
			if err != nil {
				return TextResult(StatusPartialFailure), StocktakeGenerationOutput{Status: StatusPartialFailure, Task: &projected, RecoveryPlan: "The existing generation task could not be read safely; retry polling the same task_id before starting any new generation."}, nil
			}
			if hasDataOutputErrors(task.Errors) {
				return TextResult(StatusPartialFailure), StocktakeGenerationOutput{Status: StatusPartialFailure, Task: &projected, RecoveryPlan: "InvenTree reported generation errors; inspect this task and stocktake history before starting any new generation."}, nil
			}
			if taskBinding.reportRequired && (task.Output == nil || strings.TrimSpace(*task.Output) == "") {
				return TextResult(StatusPartialFailure), StocktakeGenerationOutput{Status: StatusPartialFailure, Task: &projected, RecoveryPlan: "Generation completed without the requested report artifact; continue polling the same task_id or inspect InvenTree's worker/report configuration before starting any new generation."}, nil
			}
			output := StocktakeGenerationOutput{Status: StatusOK, Task: &projected}
			if task.Output != nil && *task.Output != "" {
				report, err := client.DownloadDataOutput(ctx, *task.Output, defaultDownloadMaxBytes)
				if err != nil {
					return TextResult(StatusPartialFailure), StocktakeGenerationOutput{Status: StatusPartialFailure, Task: &projected, RecoveryPlan: "Generation completed but the same-instance report artifact could not be safely downloaded; retry polling the same task_id before starting any new generation."}, nil
				}
				contentResult, reportOutput, err := downloadOutput(task.PK, "stocktake-report", "original", report.ContentType, report.SourceURL, report.Content)
				if err != nil {
					return nil, StocktakeGenerationOutput{}, err
				}
				output.Report = &reportOutput
				return contentResult, output, nil
			}
			return TextResult(StatusOK), output, nil
		})
}

func hasDataOutputErrors(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func stocktakeTaskOutput(task inventree.DataOutput) StocktakeTaskOutput {
	projected := StocktakeTaskOutput{PK: task.PK, Created: task.Created, Total: task.Total, Progress: task.Progress, Complete: task.Complete, OutputAvailable: task.Output != nil && strings.TrimSpace(*task.Output) != "", HasErrors: hasDataOutputErrors(task.Errors)}
	if task.OutputType != nil {
		projected.OutputType = *task.OutputType
	}
	if task.TemplateName != nil {
		projected.TemplateName = *task.TemplateName
	}
	return projected
}

func stocktakeGenerationPlan(input GenerateStocktakeInput) (StocktakeGenerationPlan, *ClarificationResponse) {
	selectors := []struct {
		name string
		id   int
	}{
		{"part", input.PartID}, {"category", input.CategoryID}, {"location", input.LocationID},
	}
	var selected StocktakeGenerationPlan
	count := 0
	for _, selector := range selectors {
		if selector.id > 0 {
			count++
			selected.Selector, selected.SelectorID = selector.name, selector.id
		}
	}
	if count != 1 {
		c := NewClarification("Which exactly one stocktake selector should be used?", "part_id", "provide exactly one positive part_id, category_id, or location_id", "part_id", true, nil, map[string]any{})
		return StocktakeGenerationPlan{}, &c
	}
	if !input.GenerateEntry && !input.GenerateReport {
		c := NewClarification("Which stocktake output should be generated?", "generate_entry", "set generate_entry and/or generate_report to true explicitly", "generate_entry", true, nil, map[string]any{"part_id": input.PartID, "category_id": input.CategoryID, "location_id": input.LocationID})
		return StocktakeGenerationPlan{}, &c
	}
	selected.GenerateEntry, selected.GenerateReport = input.GenerateEntry, input.GenerateReport
	return selected, nil
}

var errStocktakeGenerationTimeout = errors.New("stocktake generation task timed out")

func waitForStocktakeOutput(ctx context.Context, client StocktakeGenerationClient, id int, timeout time.Duration) (inventree.DataOutput, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(stocktakeGenerationInterval)
	defer ticker.Stop()
	for {
		task, err := client.GetDataOutput(ctx, id)
		if err != nil {
			return inventree.DataOutput{}, err
		}
		if task.PK != id {
			return task, errors.New("InvenTree returned a mismatched DataOutput identity")
		}
		if task.Complete {
			return task, nil
		}
		select {
		case <-ctx.Done():
			return task, ctx.Err()
		case <-deadline.C:
			return task, errStocktakeGenerationTimeout
		case <-ticker.C:
		}
	}
}
