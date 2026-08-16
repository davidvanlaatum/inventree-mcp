package inventree

import (
	"context"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstanceInfoMethodsUseExpectedEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		call     func(context.Context, *Client) error
		wantPath string
		response string
	}{
		{
			name: "get server info",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetServerInfo(ctx)
				return err
			},
			wantPath: "/api/",
			response: `{"server":"InvenTree","version":"1.5.0","apiVersion":530,"instance":"InvenTree","plugins_enabled":false}`,
		},
		{
			name: "get version info",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetVersionInfo(ctx)
				return err
			},
			wantPath: "/api/version/",
			response: `{"dev":false,"up_to_date":true,"version":{"server":"1.5.0","api":530,"commit_hash":"6a6736b","commit_date":"2026-08-11"},"links":{}}`,
		},
		{
			name: "get global setting",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetGlobalSetting(ctx, "INVENTREE_INSTANCE")
				return err
			},
			wantPath: "/api/settings/global/INVENTREE_INSTANCE/",
			response: `{"key":"INVENTREE_INSTANCE","value":"InvenTree"}`,
		},
		{
			name: "get user setting",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetUserSetting(ctx, "DATE_DISPLAY_FORMAT")
				return err
			},
			wantPath: "/api/settings/user/DATE_DISPLAY_FORMAT/",
			response: `{"key":"DATE_DISPLAY_FORMAT","value":"YYYY-MM-DD"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)

			client, err := NewClient(Config{
				BaseURL:    "https://inventory.example.test",
				Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
				HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					a.Equal(http.MethodGet, req.Method)
					a.Equal(tt.wantPath, req.URL.Path)
					return jsonResponse(req, http.StatusOK, tt.response), nil
				})},
			})
			r.NoError(err)

			r.NoError(tt.call(ctx, client))
		})
	}
}

// TestSettingValueDecodesNonStringScalars documents a live-InvenTree-1.5.0 finding from the
// F-S71 allowlist review: docs/api-schema.yaml declares GlobalSettings.value and
// UserSettings.value as type: string, but the runtime API returns boolean- and integer-typed
// settings as native JSON scalars rather than their string form.
func TestSettingValueDecodesNonStringScalars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response string
		want     string
	}{
		{name: "string value", response: `{"key":"K","value":"BO-{ref:04d}"}`, want: "BO-{ref:04d}"},
		{name: "boolean false value", response: `{"key":"K","value":false}`, want: "false"},
		{name: "boolean true value", response: `{"key":"K","value":true}`, want: "true"},
		{name: "integer value", response: `{"key":"K","value":12}`, want: "12"},
		{name: "null value", response: `{"key":"K","value":null}`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)

			client, err := NewClient(Config{
				BaseURL:    "https://inventory.example.test",
				Credential: Credential{Scheme: AuthSchemeToken, Token: "secret"},
				HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return jsonResponse(req, http.StatusOK, tt.response), nil
				})},
			})
			r.NoError(err)

			setting, err := client.GetGlobalSetting(ctx, "K")
			r.NoError(err)
			a.Equal(tt.want, setting.Value)
		})
	}
}

func TestIsOmittableFetchErrorClassifiesStatusCodes(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	a.True(IsOmittableFetchError(&APIError{StatusCode: http.StatusNotFound, Kind: ErrorKindNotFound}))
	a.True(IsOmittableFetchError(&APIError{StatusCode: http.StatusForbidden, Kind: ErrorKindPermission}))
	a.False(IsOmittableFetchError(&APIError{StatusCode: http.StatusInternalServerError, Kind: ErrorKindServer}))
	a.False(IsOmittableFetchError(&APIError{StatusCode: http.StatusUnauthorized, Kind: ErrorKindAuthentication}))
	a.False(IsOmittableFetchError(context.DeadlineExceeded))
}
