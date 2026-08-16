package tools

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInstanceInfoClient struct {
	server     inventree.ServerInfo
	serverErr  error
	version    inventree.VersionInfo
	versionErr error

	globalSettings map[string]inventree.SettingValue
	globalErrs     map[string]error
	userSettings   map[string]inventree.SettingValue
	userErrs       map[string]error
}

func (f *fakeInstanceInfoClient) GetServerInfo(context.Context) (inventree.ServerInfo, error) {
	return f.server, f.serverErr
}

func (f *fakeInstanceInfoClient) GetVersionInfo(context.Context) (inventree.VersionInfo, error) {
	return f.version, f.versionErr
}

func (f *fakeInstanceInfoClient) GetGlobalSetting(_ context.Context, key string) (inventree.SettingValue, error) {
	if err, ok := f.globalErrs[key]; ok {
		return inventree.SettingValue{}, err
	}
	if setting, ok := f.globalSettings[key]; ok {
		return setting, nil
	}
	return inventree.SettingValue{}, notFoundErr()
}

func (f *fakeInstanceInfoClient) GetUserSetting(_ context.Context, key string) (inventree.SettingValue, error) {
	if err, ok := f.userErrs[key]; ok {
		return inventree.SettingValue{}, err
	}
	if setting, ok := f.userSettings[key]; ok {
		return setting, nil
	}
	return inventree.SettingValue{}, notFoundErr()
}

func depsForInstanceInfoFake(fake *fakeInstanceInfoClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func notFoundErr() error {
	return &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
}

func permissionErr() error {
	return &inventree.APIError{StatusCode: http.StatusForbidden, Kind: inventree.ErrorKindPermission}
}

func TestGetInstanceInfoReturnsAllowlistedSettings(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	globalSettings := make(map[string]inventree.SettingValue, len(allowlistedGlobalSettings))
	for _, key := range allowlistedGlobalSettings {
		globalSettings[key] = inventree.SettingValue{Key: key, Value: "value-" + key}
	}
	userSettings := make(map[string]inventree.SettingValue, len(allowlistedUserSettings))
	for _, key := range allowlistedUserSettings {
		userSettings[key] = inventree.SettingValue{Key: key, Value: "value-" + key}
	}
	fake := &fakeInstanceInfoClient{
		server: inventree.ServerInfo{
			Server: "InvenTree", Version: "1.5.0", APIVersion: 530, Instance: "InvenTree", PluginsEnabled: true,
		},
		version:        inventree.VersionInfo{CommitHash: "6a6736b", CommitDate: "2026-08-11", UpToDate: true},
		globalSettings: globalSettings,
		userSettings:   userSettings,
	}
	handler := getInstanceInfo(depsForInstanceInfoFake(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, InstanceInfoInput{})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, output.Status)
	a.Equal("InvenTree", output.Server)
	a.Equal("1.5.0", output.Version)
	a.Equal(530, output.APIVersion)
	a.Equal("InvenTree", output.Instance)
	a.True(output.PluginsEnabled)
	a.Equal("6a6736b", output.CommitHash)
	a.Equal("2026-08-11", output.CommitDate)
	r.NotNil(output.UpToDate)
	a.True(*output.UpToDate)
	a.Empty(output.OmittedGlobalSettings)
	a.Empty(output.OmittedUserSettings)

	for _, key := range allowlistedGlobalSettings {
		a.Equal("value-"+key, output.GlobalSettings[key])
	}
	for _, key := range allowlistedUserSettings {
		a.Equal("value-"+key, output.UserSettings[key])
	}
}

func TestGetInstanceInfoOmitsMissingOrForbiddenAllowlistedKeys(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeInstanceInfoClient{
		server:     inventree.ServerInfo{Server: "InvenTree", Version: "1.5.0", APIVersion: 530},
		versionErr: permissionErr(),
		globalErrs: map[string]error{
			"INVENTREE_INSTANCE":    notFoundErr(),
			"INVENTREE_INSTANCE_ID": permissionErr(),
		},
	}
	handler := getInstanceInfo(depsForInstanceInfoFake(fake))

	_, output, err := handler(ctx, &mcp.CallToolRequest{}, InstanceInfoInput{})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Empty(output.CommitHash)
	a.Empty(output.CommitDate)
	a.Nil(output.UpToDate)
	a.Contains(output.OmittedGlobalSettings, "INVENTREE_INSTANCE")
	a.Contains(output.OmittedGlobalSettings, "INVENTREE_INSTANCE_ID")
	_, ok := output.GlobalSettings["INVENTREE_INSTANCE"]
	a.False(ok)
}

func TestGetInstanceInfoFailsOnServerInfoError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeInstanceInfoClient{serverErr: errors.New("transport failure")}
	handler := getInstanceInfo(depsForInstanceInfoFake(fake))

	_, _, err := handler(ctx, &mcp.CallToolRequest{}, InstanceInfoInput{})
	r.Error(err)
}

func TestGetInstanceInfoFailsOnNonOmittableSettingError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeInstanceInfoClient{
		server: inventree.ServerInfo{Server: "InvenTree", Version: "1.5.0", APIVersion: 530},
		globalErrs: map[string]error{
			"INVENTREE_INSTANCE": &inventree.APIError{StatusCode: http.StatusInternalServerError, Kind: inventree.ErrorKindServer},
		},
	}
	handler := getInstanceInfo(depsForInstanceInfoFake(fake))

	_, _, err := handler(ctx, &mcp.CallToolRequest{}, InstanceInfoInput{})
	r.Error(err)
}

// TestGetInstanceInfoAppliesDefensiveUserSettingFilter proves the TOKEN|SECRET|PASSWORD|KEY
// filter actually works, even though no key in the current operator-approved allowlist matches
// it. It temporarily swaps in a hypothetical sensitive-looking key rather than relying on the
// real allowlist ever containing one.
//
// Deliberately not t.Parallel(): it mutates the package-level allowlistedUserSettings var. Go's
// testing package runs every non-parallel top-level test to completion, including t.Cleanup,
// before any parallel top-level test's body resumes past its t.Parallel() call, so this mutation
// window never overlaps a running parallel test. That safety depends on this test staying
// non-parallel and remaining the only test that mutates allowlistedUserSettings/
// allowlistedGlobalSettings.
func TestGetInstanceInfoAppliesDefensiveUserSettingFilter(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	original := allowlistedUserSettings
	allowlistedUserSettings = []string{"API_TOKEN", "DATE_DISPLAY_FORMAT"}
	t.Cleanup(func() { allowlistedUserSettings = original })

	fake := &fakeInstanceInfoClient{
		server: inventree.ServerInfo{Server: "InvenTree", Version: "1.5.0", APIVersion: 530},
		userSettings: map[string]inventree.SettingValue{
			"API_TOKEN":           {Key: "API_TOKEN", Value: "should-never-be-returned"},
			"DATE_DISPLAY_FORMAT": {Key: "DATE_DISPLAY_FORMAT", Value: "YYYY-MM-DD"},
		},
	}
	handler := getInstanceInfo(depsForInstanceInfoFake(fake))

	_, output, err := handler(ctx, &mcp.CallToolRequest{}, InstanceInfoInput{})
	r.NoError(err)
	_, ok := output.UserSettings["API_TOKEN"]
	a.False(ok, "sensitive-looking key must never be fetched or returned")
	a.Contains(output.OmittedUserSettings, "API_TOKEN")
	a.Equal("YYYY-MM-DD", output.UserSettings["DATE_DISPLAY_FORMAT"])
}

func TestInstanceInfoToolIsRegisteredReadOnlyWithInventreeReadScope(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	auth, ok := ToolAuthorizations[GetInstanceInfoToolName]
	r.True(ok)
	a.Equal("read_only", auth.MutationClass)
	a.Equal([]string{ScopeInventreeRead}, auth.Scopes)
	a.Equal(ReadOnlyAnnotations, auth.Annotations)
}
