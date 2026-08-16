package inventree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ServerInfo captures the subset of InvenTree's /api/ response this project treats as
// non-sensitive instance identity: product name, version, API version, configured instance
// name, and whether the plugin system is enabled. Other /api/ fields (platform, database
// engine, admin path, worker internals, debug mode, the installed-plugin roster, etc.) are
// deliberately not modeled here; see docs/api-schema.md for the F-S71 allowlist rationale.
type ServerInfo struct {
	Server         string `json:"server"`
	Version        string `json:"version"`
	APIVersion     int    `json:"apiVersion"`
	Instance       string `json:"instance"`
	PluginsEnabled bool   `json:"plugins_enabled"`
}

// VersionInfo captures the subset of InvenTree's /api/version/ response this project treats
// as non-sensitive: precise commit identity and update status. The django/python runtime
// versions returned by that endpoint are deliberately excluded (dependency-CVE fingerprinting
// risk); see docs/api-schema.md for the F-S71 allowlist rationale.
type VersionInfo struct {
	CommitHash string `json:"commit_hash"`
	CommitDate string `json:"commit_date"`
	UpToDate   bool   `json:"up_to_date"`
}

type versionAPIResponse struct {
	UpToDate bool `json:"up_to_date"`
	Version  struct {
		CommitHash string `json:"commit_hash"`
		CommitDate string `json:"commit_date"`
	} `json:"version"`
}

// SettingValue holds one InvenTree global or user setting's key and normalized value.
type SettingValue struct {
	Key   string
	Value string
}

type settingResponse struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// normalizeSettingValue converts a raw settings-endpoint value to a display string. Despite
// docs/api-schema.yaml declaring GlobalSettings.value and UserSettings.value as type: string,
// the live InvenTree 1.5.0 API returns boolean and integer settings as native JSON scalars
// (for example {"value": false}, not {"value": "false"}), so callers must decode into
// json.RawMessage rather than assume the schema's declared string type.
func normalizeSettingValue(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return trimmed
}

// GetServerInfo fetches non-sensitive instance identity from /api/.
func (c *Client) GetServerInfo(ctx context.Context) (ServerInfo, error) {
	var out ServerInfo
	err := c.get(ctx, "/api/", &out)
	return out, err
}

// GetVersionInfo fetches non-sensitive version detail from /api/version/. InvenTree declares
// this endpoint's OAuth security scope as a:staff, so callers should treat a permission error
// as omittable rather than failing outright; use IsOmittableFetchError to classify the error.
func (c *Client) GetVersionInfo(ctx context.Context) (VersionInfo, error) {
	var raw versionAPIResponse
	if err := c.get(ctx, "/api/version/", &raw); err != nil {
		return VersionInfo{}, err
	}
	return VersionInfo{
		CommitHash: raw.Version.CommitHash,
		CommitDate: raw.Version.CommitDate,
		UpToDate:   raw.UpToDate,
	}, nil
}

// GetGlobalSetting fetches one global setting by key from /api/settings/global/{key}/.
// InvenTree's schema description states this endpoint requires staff status even though its
// declared OAuth scope is only g:read; use IsOmittableFetchError to classify a resulting
// permission or not-found error as omittable rather than a hard failure.
func (c *Client) GetGlobalSetting(ctx context.Context, key string) (SettingValue, error) {
	var out settingResponse
	if err := c.get(ctx, fmt.Sprintf("/api/settings/global/%s/", key), &out); err != nil {
		return SettingValue{}, err
	}
	return SettingValue{Key: out.Key, Value: normalizeSettingValue(out.Value)}, nil
}

// GetUserSetting fetches one of the caller's own user settings by key from
// /api/settings/user/{key}/.
func (c *Client) GetUserSetting(ctx context.Context, key string) (SettingValue, error) {
	var out settingResponse
	if err := c.get(ctx, fmt.Sprintf("/api/settings/user/%s/", key), &out); err != nil {
		return SettingValue{}, err
	}
	return SettingValue{Key: out.Key, Value: normalizeSettingValue(out.Value)}, nil
}

// IsOmittableFetchError reports whether err represents an upstream condition the instance-info
// tool must degrade gracefully on rather than fail the whole call: the requested key or
// endpoint is missing/renamed (404), or the configured credential lacks sufficient InvenTree
// permission (403) for a resource whose declared OAuth scope alone would suggest it should
// succeed.
func IsOmittableFetchError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Kind == ErrorKindNotFound || apiErr.Kind == ErrorKindPermission
}
