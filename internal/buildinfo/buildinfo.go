// Package buildinfo contains build metadata injected at link time.
package buildinfo

import (
	"fmt"
	"strings"
)

var (
	// Version is the application version embedded at build time.
	Version = "dev"
	// Commit is the source revision embedded at build time.
	Commit = "unknown"
	// BuildDate is the UTC build timestamp embedded at build time.
	BuildDate = "unknown"
)

// Info is the normalized build metadata surface for CLI and machine-readable output.
type Info struct {
	Binary    string `json:"binary"`
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
}

// Current returns the normalized build metadata for the current binary.
func Current(binary string) Info {
	info := Info{
		Binary:  strings.TrimSpace(binary),
		Version: displayVersion(Version),
	}

	if shouldRenderCommit(Commit) {
		info.Commit = strings.TrimSpace(Commit)
	}

	if shouldRenderBuildDate(BuildDate) {
		info.BuildDate = strings.TrimSpace(BuildDate)
	}

	return info
}

// Summary returns a human-readable multi-line build summary for the binary.
func Summary(binary string) string {
	info := Current(binary)
	lines := []string{
		info.Binary,
		fmt.Sprintf("  Version: %s", info.Version),
	}

	if info.Commit != "" {
		lines = append(lines, fmt.Sprintf("  Commit: %s", info.Commit))
	}

	if info.BuildDate != "" {
		lines = append(lines, fmt.Sprintf("  Built: %s", info.BuildDate))
	}

	return strings.Join(lines, "\n")
}

func displayVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "unknown" {
		return "dev"
	}

	return value
}

func shouldRenderCommit(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "unknown", "packaged", "local", "none":
		return false
	default:
		return true
	}
}

func shouldRenderBuildDate(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "unknown"
}
