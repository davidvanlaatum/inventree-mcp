//go:generate go run ../../cmd/generate-tool-manifest -out ../../docs/tool-manifest.json

package tools

import (
	"encoding/json"
	"slices"
)

const ToolMilestone1 = "milestone_1"

type ToolManifestDocument struct {
	SchemaVersion int                 `json:"schema_version"`
	GeneratedFrom []string            `json:"generated_from"`
	Tools         []ToolManifestEntry `json:"tools"`
	Prompts       []PromptManifestDoc `json:"prompts"`
}

type ToolManifestEntry struct {
	Name             string          `json:"name"`
	MilestoneStatus  string          `json:"milestone_status"`
	MutationClass    string          `json:"mutation_class"`
	Scopes           []string        `json:"scopes"`
	Annotations      AnnotationClass `json:"annotations"`
	UploadSources    []string        `json:"upload_sources"`
	HTTPRegistration string          `json:"http_registration"`
}

type PromptManifestDoc struct {
	Name            string `json:"name"`
	MilestoneStatus string `json:"milestone_status"`
	Registered      bool   `json:"registered"`
}

func GenerateToolManifest() ToolManifestDocument {
	entries := make([]ToolManifestEntry, 0, len(ToolAuthorizations))
	for _, auth := range ToolAuthorizations {
		entries = append(entries, ToolManifestEntry{
			Name:             auth.Name,
			MilestoneStatus:  ToolMilestone1,
			MutationClass:    auth.MutationClass,
			Scopes:           append([]string(nil), auth.Scopes...),
			Annotations:      auth.Annotations,
			UploadSources:    uploadSourcesForTool(auth.Name),
			HTTPRegistration: httpRegistrationForTool(auth.Name, auth.MutationClass),
		})
	}
	slices.SortFunc(entries, func(a, b ToolManifestEntry) int {
		return cmpString(a.Name, b.Name)
	})

	prompts := make([]PromptManifestDoc, 0, len(PromptManifest))
	for _, prompt := range PromptManifest {
		prompts = append(prompts, PromptManifestDoc{
			Name:            prompt.Name,
			MilestoneStatus: prompt.Status,
			Registered:      prompt.Status == PromptMilestone1,
		})
	}
	slices.SortFunc(prompts, func(a, b PromptManifestDoc) int {
		return cmpString(a.Name, b.Name)
	})

	return ToolManifestDocument{
		SchemaVersion: 1,
		GeneratedFrom: []string{
			"internal/tools.ToolAuthorizations",
			"internal/tools.PromptManifest",
		},
		Tools:   entries,
		Prompts: prompts,
	}
}

func GenerateToolManifestJSON() ([]byte, error) {
	return json.MarshalIndent(GenerateToolManifest(), "", "  ")
}

func uploadSourcesForTool(name string) []string {
	switch name {
	case UploadAttachmentToolName:
		return []string{"inline_base64", "stdio_local_path"}
	case UploadAttachmentFromURLToolName:
		return []string{"http_url_fetch"}
	case CreateLinkAttachmentToolName:
		return []string{"http_url_link"}
	case SetPrimaryImageToolName:
		return []string{"existing_attachment_image"}
	default:
		return nil
	}
}

func httpRegistrationForTool(name string, mutationClass string) string {
	if name == HealthVersionToolName || mutationClass == "read_only" {
		return "registered"
	}
	return "stdio_only_until_m1c_s04"
}

func cmpString(a string, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
