package tools

import (
	"context"
	"errors"

	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	LocalUploadReasonAllowlistRequired = "allowlist_not_configured"
	LocalUploadReasonOutsideAllowlist  = "path_outside_allowlist"
)

type LocalUploadPolicyOutput struct {
	Status               string   `json:"status"`
	LocalPathEnabled     bool     `json:"local_path_enabled"`
	AllowedRoots         []string `json:"allowed_roots"`
	AttachmentMaxBytes   int64    `json:"attachment_max_bytes"`
	CompanyImageMaxBytes int64    `json:"company_image_max_bytes"`
	Requirements         []string `json:"requirements"`
}

type LocalUploadPolicyInput struct{}

type LocalUploadRecovery struct {
	Reason       string `json:"reason"`
	PolicyTool   string `json:"policy_tool"`
	RecoveryPlan string `json:"recovery_plan"`
}

func registerLocalUploadPolicyTool(server *mcp.Server, deps Dependencies) {
	if deps.UploadMode != upload.ModeStdio {
		return
	}
	addReadOnlyTool(server, deps, GetLocalUploadPolicyToolName, "Get local upload policy", "Returns the local STDIO upload roots and size policy so a local agent can stage a file safely.", getLocalUploadPolicy(deps))
}

func getLocalUploadPolicy(deps Dependencies) mcp.ToolHandlerFor[LocalUploadPolicyInput, LocalUploadPolicyOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ LocalUploadPolicyInput) (*mcp.CallToolResult, LocalUploadPolicyOutput, error) {
		roots, err := upload.CanonicalAllowRoots(deps.UploadFS, deps.UploadAllowRoots)
		if err != nil {
			return nil, LocalUploadPolicyOutput{}, err
		}
		out := LocalUploadPolicyOutput{
			Status:               StatusOK,
			LocalPathEnabled:     len(roots) > 0,
			AllowedRoots:         roots,
			AttachmentMaxBytes:   uploadMaxBytes(deps),
			CompanyImageMaxBytes: companyImageMaxBytes(&deps),
			Requirements: []string{
				"The file must already exist under an allowed root before the upload tool is called.",
				"An allowed root means the MCP server may read the file; it does not guarantee that the calling agent can write there.",
				"The resolved path must remain inside an allowed root and must identify a regular file; symlink escapes are rejected.",
			},
		}
		return TextResult(StatusOK), out, nil
	}
}

func localUploadRecovery(err error) (*LocalUploadRecovery, bool) {
	switch {
	case errors.Is(err, upload.ErrLocalUploadAllowlistRequired):
		return &LocalUploadRecovery{
			Reason:       LocalUploadReasonAllowlistRequired,
			PolicyTool:   GetLocalUploadPolicyToolName,
			RecoveryPlan: "Local-path staging is unavailable because no trusted roots are configured. Ask the operator to configure a trusted root or retry with inline content.",
		}, true
	case errors.Is(err, upload.ErrLocalUploadOutsideAllowlist):
		return &LocalUploadRecovery{
			Reason:       LocalUploadReasonOutsideAllowlist,
			PolicyTool:   GetLocalUploadPolicyToolName,
			RecoveryPlan: "Call get_local_upload_policy, place the file under a returned root only when caller permissions allow it, then retry with that local_path.",
		}, true
	default:
		return nil, false
	}
}
