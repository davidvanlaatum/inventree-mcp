package buildinfo

const applicationName = "inventree-mcp"

// PinnedInvenTreeVersion and PinnedInvenTreeAPIVersion are the InvenTree baseline this
// build targets. internal/testenv references these same constants so the Testcontainers
// pin and the CLI's reported baseline cannot drift from each other independently.
const (
	PinnedInvenTreeVersion    = "1.5.1"
	PinnedInvenTreeAPIVersion = "530"
)

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// UserAgent returns the product identity used for outbound HTTP requests.
func UserAgent() string {
	return applicationName + "/" + Version
}
