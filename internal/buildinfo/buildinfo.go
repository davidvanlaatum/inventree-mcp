package buildinfo

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func IsDevelopment() bool {
	return Version == "dev"
}
