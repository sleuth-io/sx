package buildinfo

import "fmt"

var (
	// Version will be set via ldflags during build
	Version = "dev"
	// Commit will be set via ldflags during build
	Commit = "none"
	// Date will be set via ldflags during build
	Date = "unknown"
)

// GetUserAgent returns a user agent string for HTTP requests
func GetUserAgent() string {
	return fmt.Sprintf("skills/%s", Version)
}

// GetCreatedBy returns the "created-by" string for lock files
func GetCreatedBy() string {
	return fmt.Sprintf("skills/%s", Version)
}
