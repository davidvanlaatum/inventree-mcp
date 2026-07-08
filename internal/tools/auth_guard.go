package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	MetaSecuritySchemesKey       = "securitySchemes"
	MetaOpenAISecuritySchemesKey = "openai/securitySchemes"
	MetaWWWAuthenticateKey       = "mcp/www_authenticate"
)

type OAuthSecurityScheme struct {
	Type   string   `json:"type"`
	Scopes []string `json:"scopes,omitempty"`
}

type AuthorizationMode string

const (
	AuthorizationModeNone  AuthorizationMode = "none"
	AuthorizationModeOAuth AuthorizationMode = "oauth"
)

func ToolDescriptor(name string, title string, description string) *mcp.Tool {
	authz := ToolAuthorizations[name]
	tool := &mcp.Tool{
		Name:        name,
		Title:       title,
		Description: description,
		Annotations: ToolAnnotations(authz.Annotations),
	}
	if len(authz.Scopes) > 0 {
		schemes := []OAuthSecurityScheme{{Type: "oauth2", Scopes: append([]string(nil), authz.Scopes...)}}
		tool.Meta = mcp.Meta{
			MetaSecuritySchemesKey:       schemes,
			MetaOpenAISecuritySchemesKey: schemes,
		}
	}
	return tool
}

func GuardTool[In, Out any](deps Dependencies, toolName string, handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		if deps.AuthorizationMode != AuthorizationModeOAuth {
			return handler(ctx, req, input)
		}
		if result, denied := authorizeTool(ctx, deps.ResourceMetadataURL, toolName); denied {
			var zero Out
			return result, zero, nil
		}
		return handler(ctx, req, input)
	}
}

func authorizeTool(ctx context.Context, resourceMetadataURL string, toolName string) (*mcp.CallToolResult, bool) {
	authz, ok := ToolAuthorizations[toolName]
	if !ok {
		return authChallengeResult(resourceMetadataURL, nil, "tool authorization metadata is missing"), true
	}
	if len(authz.Scopes) == 0 {
		return nil, false
	}
	tokenInfo := auth.TokenInfoFromContext(ctx)
	if tokenInfo == nil {
		return authChallengeResult(resourceMetadataURL, authz.Scopes, "OAuth bearer token is required for this tool"), true
	}
	for _, required := range authz.Scopes {
		if !hasScope(tokenInfo.Scopes, required) {
			return authChallengeResult(resourceMetadataURL, authz.Scopes, fmt.Sprintf("OAuth bearer token is missing required scope %q", required)), true
		}
	}
	return nil, false
}

func authChallengeResult(resourceMetadataURL string, scopes []string, message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Meta: mcp.Meta{
			MetaWWWAuthenticateKey: bearerChallenge(resourceMetadataURL, scopes, "insufficient_scope", message),
		},
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
		IsError: true,
	}
}

func bearerChallenge(resourceMetadataURL string, scopes []string, errorCode string, errorDescription string) string {
	params := make([]string, 0, 4)
	if resourceMetadataURL != "" {
		params = append(params, fmt.Sprintf("resource_metadata=%q", resourceMetadataURL))
	}
	if len(scopes) > 0 {
		params = append(params, fmt.Sprintf("scope=%q", strings.Join(scopes, " ")))
	}
	if errorCode != "" {
		params = append(params, fmt.Sprintf("error=%q", errorCode))
	}
	if errorDescription != "" {
		params = append(params, fmt.Sprintf("error_description=%q", errorDescription))
	}
	if len(params) == 0 {
		return "Bearer"
	}
	return "Bearer " + strings.Join(params, ", ")
}

func hasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}
