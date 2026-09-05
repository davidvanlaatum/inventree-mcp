package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultUserSearchMaxPageDepth mirrors F-S99's search_barcode_scan_history
// paging contract, reused here per the F-S104 operator decision: an
// operator-configurable finite depth (in pages of at most MaxLookupLimit
// results each), defaulting to 50, with zero/negative always rejected.
// Unlike scan-history's internal multi-page walk (needed there because
// endpoint/from/to are not real upstream query parameters), search_users
// forwards search/is_active/is_staff/is_superuser directly to InvenTree's
// own list filters in one upstream call per MCP call; this bound instead
// caps how deep a caller may page via offset, guarding against unbounded
// directory enumeration through repeated calls.
const defaultUserSearchMaxPageDepth = 50

func effectiveUserSearchMaxPageDepth(deps Dependencies) int {
	if deps.UserSearchMaxPageDepth > 0 {
		return deps.UserSearchMaxPageDepth
	}
	return defaultUserSearchMaxPageDepth
}

// UserLookupClient is the narrow client surface search_users/get_user need.
type UserLookupClient interface {
	SearchUsersPage(context.Context, inventree.UserQuery) (inventree.UserPage, error)
	GetUser(context.Context, int) (inventree.User, error)
}

// UserView is the F-S104 operator-approved safe user identity projection.
// Email, groups, permissions, and profile details are never included; the
// administrative is_staff/is_superuser flags are search filters only (see
// SearchUsersInput) and are deliberately absent here too.
type UserView struct {
	inventree.WebLinkFields
	PK        int    `json:"pk"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	IsActive  bool   `json:"is_active"`
}

func userView(record inventree.User) UserView {
	return UserView{PK: record.PK, Username: record.Username, FirstName: record.FirstName, LastName: record.LastName, IsActive: record.IsActive}
}

type SearchUsersInput struct {
	Query           string `json:"query" jsonschema:"Required non-empty search text matched as a case-insensitive substring across username, first name, and last name. Regular expressions are not supported."`
	IncludeInactive bool   `json:"include_inactive,omitempty" jsonschema:"When true, also return disabled (is_active=false) users. Defaults to active users only."`
	IsStaff         *bool  `json:"is_staff,omitempty" jsonschema:"Optional filter for staff status. Administrative flags are filters only and are never returned in results."`
	IsSuperuser     *bool  `json:"is_superuser,omitempty" jsonschema:"Optional filter for superuser status. Administrative flags are filters only and are never returned in results."`
	Limit           int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset          int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries. Bounded by the configured maximum page depth (default 50 pages)."`
}

type SearchUsersOutput struct {
	Status     string             `json:"status"`
	Count      int                `json:"count,omitempty"`
	HasMore    bool               `json:"has_more,omitempty"`
	Results    []UserView         `json:"results,omitempty"`
	Validation *ValidationFailure `json:"validation,omitempty"`
}

func registerUserLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchUsersToolName, "Search users", "Searches InvenTree user accounts by username, first name, or last name substring. Separate from search_owners, which covers users and groups for ownership workflows.", searchUsers(deps))
	addReadOnlyTool(server, deps, GetUserToolName, "Get user", "Retrieves one InvenTree user account by stable ID. Never falls back to get_owner.", getUser(deps))
}

func searchUsers(deps Dependencies) mcp.ToolHandlerFor[SearchUsersInput, SearchUsersOutput] {
	return LookupHandler[UserLookupClient, SearchUsersInput, SearchUsersOutput](deps, SearchUsersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client UserLookupClient, input SearchUsersInput) (*mcp.CallToolResult, SearchUsersOutput, error) {
			query := strings.TrimSpace(input.Query)
			if query == "" {
				return userSearchValidation("query must not be empty")
			}
			if input.Offset < 0 {
				return userSearchValidation("offset must not be negative")
			}
			limit := NormalizeLookupLimit(input.Limit)
			maxDepth := effectiveUserSearchMaxPageDepth(deps)
			if input.Offset/limit >= maxDepth {
				return userSearchValidation(fmt.Sprintf("offset exceeds the configured maximum page depth (%d pages of up to %d results)", maxDepth, MaxLookupLimit))
			}
			var isActive *bool
			if !input.IncludeInactive {
				active := true
				isActive = &active
			}
			page, err := client.SearchUsersPage(ctx, inventree.UserQuery{
				Search:      query,
				IsActive:    isActive,
				IsStaff:     input.IsStaff,
				IsSuperuser: input.IsSuperuser,
				Limit:       limit,
				Offset:      input.Offset,
			})
			if err != nil {
				return nil, SearchUsersOutput{}, err
			}
			results := make([]UserView, 0, len(page.Results))
			for _, record := range page.Results {
				results = append(results, userView(record))
			}
			if len(results) == 0 {
				return TextResult(StatusNotFound), SearchUsersOutput{Status: StatusNotFound}, nil
			}
			return TextResult(StatusOK), SearchUsersOutput{Status: StatusOK, Count: page.Count, HasMore: page.HasMore, Results: results}, nil
		})
}

func userSearchValidation(message string) (*mcp.CallToolResult, SearchUsersOutput, error) {
	return TextResult(StatusValidationFailed), SearchUsersOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "search_users", Messages: []string{message}}}}}, nil
}

func getUser(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[UserView]] {
	return LookupHandler[UserLookupClient, IDInput, RecordOutput[UserView]](deps, GetUserToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client UserLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[UserView], error) {
			record, err := client.GetUser(ctx, input.ID)
			if err != nil {
				return recordOutput(UserView{}, err)
			}
			return recordOutput(userView(record), nil)
		})
}
