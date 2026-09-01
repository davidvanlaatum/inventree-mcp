package config

import (
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// templateKeyState classifies how a config setting appears in the packaged
// HTTP template's raw YAML text.
type templateKeyState int

const (
	keyActive templateKeyState = iota
	keyCommented
	keyAbsent
)

// classifyTemplateKey reports whether key appears in content as a real
// (uncommented) YAML mapping key, only as a commented-out example, or not at
// all. It matches at the start of a line, optionally preceded by "#"
// comment markers and whitespace, so it distinguishes an active example from
// a commented one without being fooled by prose that merely mentions the key
// name (prose never has the key immediately followed by a colon).
func classifyTemplateKey(content, key string) templateKeyState {
	quoted := regexp.QuoteMeta(key)
	if regexp.MustCompile(`(?m)^` + quoted + `:`).MatchString(content) {
		return keyActive
	}
	if regexp.MustCompile(`(?m)^#\s*` + quoted + `:`).MatchString(content) {
		return keyCommented
	}
	return keyAbsent
}

// fileConfigYAMLKeys returns every yaml tag on fileConfig, so the inventory
// test below is checked against the real, current field set rather than a
// hand-maintained list that could silently drift from it.
func fileConfigYAMLKeys(t *testing.T) []string {
	t.Helper()
	fields := reflect.TypeOf(fileConfig{})
	keys := make([]string, 0, fields.NumField())
	for i := range fields.NumField() {
		tag := fields.Field(i).Tag.Get("yaml")
		require.NotEmpty(t, tag, "fileConfig field %s has no yaml tag", fields.Field(i).Name)
		keys = append(keys, tag)
	}
	return keys
}

// TestPackagedHTTPTemplateSettingInventory is the F-S95 packaged-config
// audit's raw-template inventory test. It asserts every fileConfig setting
// falls into exactly one of four deterministic states in
// packaging/inventree-mcp.yml: an active HTTP example, a commented HTTP
// example, a development-only commented example, or a setting that must not
// appear at all because it is not a usable HTTP option. This is a complete
// partition (fileConfigYAMLKeys) so a newly added config field that isn't
// explicitly classified here fails the test rather than silently landing in
// none of the buckets.
func TestPackagedHTTPTemplateSettingInventory(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	content, err := os.ReadFile("../../packaging/inventree-mcp.yml")
	r.NoError(err)
	text := string(content)

	activeKeys := []string{"transport", "listen", "path", "environment", "log_level", "inventree_url"}
	commentedKeys := []string{
		"inventree_web_url", "inventree_timeout", "inventree_tls_skip_verify",
		"mcp_max_request_body_bytes", "upload_max_bytes", "debug_traffic_log",
		"bulk_max_items", "bulk_concurrency",
		"otel_enabled", "otel_service_name", "otel_exporter", "otel_endpoint",
		"otel_insecure", "otel_headers", "otel_sample_ratio", "otel_batch_timeout",
		"otel_export_timeout", "otel_metrics_enabled", "otel_metrics_path",
		"oauth_issuer_url", "oauth_resource_url", "oauth_keys", "oauth_client_ids",
		"oauth_access_lifetime", "oauth_refresh_lifetime", "oauth_session_lifetime",
		"trusted_proxy_cidrs", "bootstrap_enabled", "bootstrap_envelope_lifetime",
	}
	developmentOnlyKeys := []string{"dev_incomplete_oauth"}
	// Not usable HTTP settings: HTTP mode derives InvenTree credentials from
	// the caller's OAuth envelope per request, so a locally configured auth
	// scheme or static token has no effect; local-path upload staging is
	// STDIO-only.
	forbiddenKeys := []string{"inventree_auth_scheme", "inventree_token", "upload_allow_roots"}

	for _, key := range activeKeys {
		a.Equal(keyActive, classifyTemplateKey(text, key), "%s must be an active (uncommented) HTTP example", key)
	}
	for _, key := range commentedKeys {
		a.Equal(keyCommented, classifyTemplateKey(text, key), "%s must be a commented-out HTTP example", key)
	}
	for _, key := range developmentOnlyKeys {
		a.Equal(keyCommented, classifyTemplateKey(text, key), "%s must be a commented-out development-only example", key)
	}
	for _, key := range forbiddenKeys {
		a.Equal(keyAbsent, classifyTemplateKey(text, key), "%s must not appear as a usable HTTP setting, active or commented", key)
	}

	classified := make(map[string]bool, len(activeKeys)+len(commentedKeys)+len(developmentOnlyKeys)+len(forbiddenKeys))
	for _, group := range [][]string{activeKeys, commentedKeys, developmentOnlyKeys, forbiddenKeys} {
		for _, key := range group {
			a.False(classified[key], "%s is classified in more than one inventory bucket", key)
			classified[key] = true
		}
	}
	for _, key := range fileConfigYAMLKeys(t) {
		a.True(classified[key], "%s is a real config field with no inventory classification in this test; add it to exactly one bucket above", key)
	}

	a.Contains(text, "debug_traffic_log", "the debug traffic log example itself must be present")
	a.Regexp(`(?i)uri|url`, text, "the debug traffic log warning must call out sensitive URI/URL exposure")
	a.Regexp(`(?i)bod(y|ies)`, text, "the debug traffic log warning must call out request/response body exposure")
	a.Regexp(`(?i)response`, text, "the debug traffic log warning must call out response exposure")
	a.Regexp(`(?i)credential`, text, "the debug traffic log warning must call out credential exposure")
	a.Regexp(`(?i)0600|owner-only`, text, "the debug traffic log warning must state the file is owner-only")
	a.Regexp(`(?i)retention|rotate|delete`, text, "the debug traffic log warning must call for bounded retention/deletion")
	a.Regexp(`(?i)troubleshoot|diagnos`, text, "the debug traffic log warning must state it is for troubleshooting, not routine production use")
}
