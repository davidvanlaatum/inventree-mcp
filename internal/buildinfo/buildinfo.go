package buildinfo

const applicationName = "inventree-mcp"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// UserAgent returns the product identity used for outbound HTTP requests.
func UserAgent() string {
	return applicationName + "/" + Version
}
