package docs

import _ "embed"

//go:embed TASKS.md
var tasksMarkdown string

//go:embed api-schema.md
var apiSchemaMarkdown string

//go:embed tool-reference.md
var toolReferenceMarkdown string

func TasksMarkdown() string {
	return tasksMarkdown
}

func APISchemaMarkdown() string {
	return apiSchemaMarkdown
}

func ToolReferenceMarkdown() string {
	return toolReferenceMarkdown
}
