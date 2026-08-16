// Package inventreemcp is the root package for the InvenTree MCP server module.
package inventreemcp

import _ "embed"

//go:embed README.md
var readmeMarkdown string

func ReadmeMarkdown() string {
	return readmeMarkdown
}
