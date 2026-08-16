package tools

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const GetInstanceInfoToolName = "get_inventree_instance_info"

// allowlistedGlobalSettings and allowlistedUserSettings are the F-S71 operator-approved
// allowlists, drafted by checking each candidate key's live value against a running InvenTree
// 1.5.0 instance and explicitly approved by the operator on 2026-08-16. See docs/api-schema.md
// and docs/TASKS.md's F-S71 Decisions for the full rationale and the excluded categories.
// This is the first inventree.read tool that touches staff-scoped administrative data rather
// than ordinary business data; keep any future admin-surface allowlist to the same scrutiny.
var allowlistedGlobalSettings = []string{
	"INVENTREE_INSTANCE",
	"INVENTREE_INSTANCE_ID",
	"INVENTREE_COMPANY_NAME",
	"INVENTREE_DEFAULT_CURRENCY",
	"CURRENCY_CODES",
	"BUILDORDER_REFERENCE_PATTERN",
	"SALESORDER_REFERENCE_PATTERN",
	"PURCHASEORDER_REFERENCE_PATTERN",
	"RETURNORDER_REFERENCE_PATTERN",
	"TRANSFERORDER_REFERENCE_PATTERN",
	"RETURNORDER_ENABLED",
	"TRANSFERORDER_ENABLED",
	"STOCKTAKE_ENABLE",
	"PROJECT_CODES_ENABLED",
	"PART_ENABLE_REVISION",
	"PART_ENABLE_LOCKING",
	"SERIAL_NUMBER_GLOBALLY_UNIQUE",
	"PARAMETER_ENFORCE_UNITS",
	"BARCODE_ENABLE",
	"LABEL_ENABLE",
	"REPORT_ENABLE",
	"CURRENCY_UPDATE_PLUGIN",
	"BARCODE_GENERATION_PLUGIN",
}

var allowlistedUserSettings = []string{
	"DATE_DISPLAY_FORMAT",
}

// sensitiveUserSettingKeyPattern matches user-setting keys this tool must never return, even if
// a future InvenTree version reuses one of the names above for something credential-shaped.
// No key observed on the pinned InvenTree 1.5.0 instance matches this pattern; it is defensive
// insurance per F-S71's acceptance criteria, not a response to an observed risk. Broader than the
// acceptance criteria's literal TOKEN|SECRET|PASSWORD|KEY example so a few likely naming variants
// (PASSWD, CREDENTIAL, PRIVATE) are also caught.
var sensitiveUserSettingKeyPattern = regexp.MustCompile(`(?i)TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|PRIVATE|KEY`)

type InstanceInfoInput struct{}

type InstanceInfoOutput struct {
	Status                string            `json:"status"`
	Server                string            `json:"server"`
	Version               string            `json:"version"`
	APIVersion            int               `json:"api_version"`
	Instance              string            `json:"instance"`
	PluginsEnabled        bool              `json:"plugins_enabled"`
	CommitHash            string            `json:"commit_hash,omitempty"`
	CommitDate            string            `json:"commit_date,omitempty"`
	UpToDate              *bool             `json:"up_to_date,omitempty"`
	GlobalSettings        map[string]string `json:"global_settings"`
	UserSettings          map[string]string `json:"user_settings"`
	OmittedGlobalSettings []string          `json:"omitted_global_settings,omitempty"`
	OmittedUserSettings   []string          `json:"omitted_user_settings,omitempty"`
}

type InstanceInfoClient interface {
	GetServerInfo(context.Context) (inventree.ServerInfo, error)
	GetVersionInfo(context.Context) (inventree.VersionInfo, error)
	GetGlobalSetting(context.Context, string) (inventree.SettingValue, error)
	GetUserSetting(context.Context, string) (inventree.SettingValue, error)
}

func registerInstanceInfoTool(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(
		server, deps, GetInstanceInfoToolName, "Get InvenTree instance info",
		"Returns InvenTree server/API version and instance identity, plus an operator-approved allowlisted subset of global and user settings. "+
			"A nonempty omitted_global_settings or omitted_user_settings list is expected, normal degradation (a renamed/removed key or insufficient InvenTree privilege), not an error.",
		getInstanceInfo(deps),
	)
}

func getInstanceInfo(deps Dependencies) mcp.ToolHandlerFor[InstanceInfoInput, InstanceInfoOutput] {
	return LookupHandler[InstanceInfoClient, InstanceInfoInput, InstanceInfoOutput](
		deps, GetInstanceInfoToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client InstanceInfoClient, _ InstanceInfoInput) (
			*mcp.CallToolResult, InstanceInfoOutput, error,
		) {
			logger := logging.FromContext(ctx)

			server, err := client.GetServerInfo(ctx)
			if err != nil {
				return nil, InstanceInfoOutput{}, err
			}

			out := InstanceInfoOutput{
				Status:         StatusOK,
				Server:         server.Server,
				Version:        server.Version,
				APIVersion:     server.APIVersion,
				Instance:       server.Instance,
				PluginsEnabled: server.PluginsEnabled,
				GlobalSettings: map[string]string{},
				UserSettings:   map[string]string{},
			}

			if version, versionErr := client.GetVersionInfo(ctx); versionErr != nil {
				if !inventree.IsOmittableFetchError(versionErr) {
					return nil, InstanceInfoOutput{}, versionErr
				}
				logger.WarnContext(ctx, "omitting InvenTree version detail", logging.Err(versionErr))
			} else {
				out.CommitHash = version.CommitHash
				out.CommitDate = version.CommitDate
				upToDate := version.UpToDate
				out.UpToDate = &upToDate
			}

			// Each allowlisted key is fetched with its own sequential request rather than
			// concurrently. This keeps the tool from bursting 20+ simultaneous requests at
			// InvenTree (this client has no 429/backoff handling) and keeps failure attribution
			// per-key straightforward; the tradeoff is higher tail latency than other tools'
			// single-round-trip shape, which is accepted for this admin-introspection tool.
			for _, key := range allowlistedGlobalSettings {
				setting, settingErr := client.GetGlobalSetting(ctx, key)
				if settingErr != nil {
					if !inventree.IsOmittableFetchError(settingErr) {
						return nil, InstanceInfoOutput{}, settingErr
					}
					out.OmittedGlobalSettings = append(out.OmittedGlobalSettings, key)
					logger.WarnContext(ctx, "omitting allowlisted global setting", slog.String("key", key), logging.Err(settingErr))
					continue
				}
				if setting.Key != key {
					return nil, InstanceInfoOutput{}, fmt.Errorf("InvenTree returned a mismatched global setting identity for %s", key)
				}
				out.GlobalSettings[key] = setting.Value
			}

			for _, key := range allowlistedUserSettings {
				if sensitiveUserSettingKeyPattern.MatchString(key) {
					out.OmittedUserSettings = append(out.OmittedUserSettings, key)
					continue
				}
				setting, settingErr := client.GetUserSetting(ctx, key)
				if settingErr != nil {
					if !inventree.IsOmittableFetchError(settingErr) {
						return nil, InstanceInfoOutput{}, settingErr
					}
					out.OmittedUserSettings = append(out.OmittedUserSettings, key)
					logger.WarnContext(ctx, "omitting allowlisted user setting", slog.String("key", key), logging.Err(settingErr))
					continue
				}
				if setting.Key != key {
					return nil, InstanceInfoOutput{}, fmt.Errorf("InvenTree returned a mismatched user setting identity for %s", key)
				}
				out.UserSettings[key] = setting.Value
			}

			return TextResult(StatusOK), out, nil
		},
	)
}
