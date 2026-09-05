package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeUserClient struct {
	users             map[int]inventree.User
	userSearchResults []inventree.User
	userSearchHasMore bool
	searchErr         error
	lastUserQuery     inventree.UserQuery
}

func newFakeUserClient() *fakeUserClient {
	return &fakeUserClient{users: map[int]inventree.User{}}
}

func (f *fakeUserClient) SearchUsersPage(_ context.Context, query inventree.UserQuery) (inventree.UserPage, error) {
	f.lastUserQuery = query
	if f.searchErr != nil {
		return inventree.UserPage{}, f.searchErr
	}
	return inventree.UserPage{Count: len(f.userSearchResults), Results: f.userSearchResults, HasMore: f.userSearchHasMore}, nil
}

func (f *fakeUserClient) GetUser(_ context.Context, id int) (inventree.User, error) {
	value, ok := f.users[id]
	if !ok {
		return inventree.User{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func userDeps(fake *fakeUserClient) Dependencies {
	return Dependencies{
		ClientFromContext: func(context.Context) (any, error) { return fake, nil },
	}
}

func TestSearchUsersRequiresNonEmptyQuery(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeUserClient()

	_, out, err := searchUsers(userDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchUsersInput{})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
	require.NotNil(t, out.Validation)

	_, out, err = searchUsers(userDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchUsersInput{Query: "   "})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestSearchUsersRejectsNegativeOffset(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeUserClient()

	_, out, err := searchUsers(userDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchUsersInput{Query: "j", Offset: -1})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)
}

func TestSearchUsersRejectsOffsetBeyondConfiguredMaxPageDepth(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeUserClient()
	deps := userDeps(fake)
	deps.UserSearchMaxPageDepth = 2

	// limit defaults to DefaultLookupLimit(20); page depth 2 allows offsets
	// 0 and 20 (pages 0 and 1), but not 40 (page 2).
	_, out, err := searchUsers(deps)(ctx, &mcp.CallToolRequest{}, SearchUsersInput{Query: "j", Offset: 40})
	require.NoError(t, err)
	assert.Equal(t, StatusValidationFailed, out.Status)

	fake.userSearchResults = []inventree.User{{PK: 1, Username: "jdoe"}}
	_, ok, err := searchUsers(deps)(ctx, &mcp.CallToolRequest{}, SearchUsersInput{Query: "j", Offset: 20})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, ok.Status)
}

func TestSearchUsersProjectsPrivacySafeFieldsAndDefaultsToActiveOnly(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeUserClient()
	fake.userSearchResults = []inventree.User{
		{PK: 1, Username: "jdoe", FirstName: "Jane", LastName: "Doe", IsActive: true},
	}
	fake.userSearchHasMore = true

	_, out, err := searchUsers(userDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchUsersInput{Query: "jdoe"})
	require.NoError(t, err)
	require.Equal(t, StatusOK, out.Status)
	require.True(t, out.HasMore)
	require.Len(t, out.Results, 1)
	assert.Equal(t, 1, out.Results[0].PK)
	assert.Equal(t, "jdoe", out.Results[0].Username)
	assert.Equal(t, "Jane", out.Results[0].FirstName)
	assert.Equal(t, "Doe", out.Results[0].LastName)
	assert.True(t, out.Results[0].IsActive)

	require.NotNil(t, fake.lastUserQuery.IsActive)
	assert.True(t, *fake.lastUserQuery.IsActive)
}

func TestSearchUsersIncludeInactiveOmitsActiveFilter(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeUserClient()

	_, _, err := searchUsers(userDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchUsersInput{Query: "jdoe", IncludeInactive: true})
	require.NoError(t, err)
	assert.Nil(t, fake.lastUserQuery.IsActive)
}

func TestSearchUsersPassesStaffAndSuperuserFiltersToClient(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeUserClient()
	staff := true
	superuser := false

	_, _, err := searchUsers(userDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchUsersInput{Query: "jdoe", IsStaff: &staff, IsSuperuser: &superuser})
	require.NoError(t, err)
	require.NotNil(t, fake.lastUserQuery.IsStaff)
	assert.True(t, *fake.lastUserQuery.IsStaff)
	require.NotNil(t, fake.lastUserQuery.IsSuperuser)
	assert.False(t, *fake.lastUserQuery.IsSuperuser)
}

func TestSearchUsersReturnsNotFoundForEmptyResults(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeUserClient()

	_, out, err := searchUsers(userDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchUsersInput{Query: "nobody"})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestSearchUsersWireOutputExcludesAdministrativeAndSensitiveFields(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeUserClient()
	fake.userSearchResults = []inventree.User{
		{PK: 1, Username: "jdoe", FirstName: "Jane", LastName: "Doe", IsActive: true},
	}

	_, out, err := searchUsers(userDeps(fake))(ctx, &mcp.CallToolRequest{}, SearchUsersInput{Query: "jdoe"})
	require.NoError(t, err)
	require.Equal(t, StatusOK, out.Status)

	wire, marshalErr := json.Marshal(out)
	require.NoError(t, marshalErr)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(wire, &decoded))
	results, ok := decoded["results"].([]any)
	require.True(t, ok)
	require.Len(t, results, 1)
	record, ok := results[0].(map[string]any)
	require.True(t, ok)
	for _, forbidden := range []string{"email", "groups", "permissions", "profile", "is_staff", "is_superuser"} {
		_, present := record[forbidden]
		assert.False(t, present, "wire output must never include %q", forbidden)
	}
	for _, expected := range []string{"pk", "username", "first_name", "last_name", "is_active"} {
		_, present := record[expected]
		assert.True(t, present, "wire output must include %q", expected)
	}
}

func TestGetUserReturnsNotFoundForMissingRecordAndNeverFallsBackToOwner(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeUserClient()

	_, out, err := getUser(userDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 99})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
	assert.Zero(t, out.Record)

	fake.users[7] = inventree.User{PK: 7, Username: "jdoe", IsActive: false}
	_, found, err := getUser(userDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 7})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, found.Status)
	assert.Equal(t, 7, found.Record.PK)
	assert.False(t, found.Record.IsActive)
}
