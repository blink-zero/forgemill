package version

// These are set at build time via -ldflags.
// Example: go build -ldflags "-X github.com/forgemill/forgemill/internal/version.Version=0.1.0"
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)
