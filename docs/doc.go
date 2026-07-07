package docs

import _ "embed"

//go:embed TASKS.md
var tasksMarkdown string

//go:embed api-schema.md
var apiSchemaMarkdown string

//go:embed api-schema.yaml
var apiSchemaYAML []byte

//go:embed endpoint-manifest.yaml
var endpointManifestYAML []byte

//go:embed tool-manifest.json
var toolManifestJSON []byte

//go:embed tool-reference.md
var toolReferenceMarkdown string

func TasksMarkdown() string {
	return tasksMarkdown
}

func APISchemaMarkdown() string {
	return apiSchemaMarkdown
}

func APISchemaYAML() []byte {
	return append([]byte(nil), apiSchemaYAML...)
}

func EndpointManifestYAML() []byte {
	return append([]byte(nil), endpointManifestYAML...)
}

func ToolManifestJSON() []byte {
	return append([]byte(nil), toolManifestJSON...)
}

func ToolReferenceMarkdown() string {
	return toolReferenceMarkdown
}
