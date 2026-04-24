package buildinfo

import (
	"strings"
	"testing"
)

func TestSummaryRendersStructuredOutput(t *testing.T) {
	originalVersion := Version
	originalCommit := Commit
	originalBuildDate := BuildDate
	t.Cleanup(func() {
		Version = originalVersion
		Commit = originalCommit
		BuildDate = originalBuildDate
	})

	Version = "v1.2.3"
	Commit = "abc1234"
	BuildDate = "2026-04-24T17:35:08Z"

	summary := Summary("s3ctl")

	for _, fragment := range []string{
		"s3ctl",
		"  Version: v1.2.3",
		"  Commit: abc1234",
		"  Built: 2026-04-24T17:35:08Z",
	} {
		if !strings.Contains(summary, fragment) {
			t.Fatalf("expected summary to contain %q, got %q", fragment, summary)
		}
	}
}

func TestSummaryOmitsPlaceholderCommit(t *testing.T) {
	originalVersion := Version
	originalCommit := Commit
	originalBuildDate := BuildDate
	t.Cleanup(func() {
		Version = originalVersion
		Commit = originalCommit
		BuildDate = originalBuildDate
	})

	Version = "dev"
	Commit = "packaged"
	BuildDate = "2026-04-24T17:35:08Z"

	summary := Summary("s3ctl")

	if strings.Contains(summary, "Commit:") {
		t.Fatalf("expected placeholder commit to be omitted, got %q", summary)
	}
	if !strings.Contains(summary, "  Version: dev") {
		t.Fatalf("expected version line, got %q", summary)
	}
}

func TestCurrentReturnsNormalizedMetadata(t *testing.T) {
	originalVersion := Version
	originalCommit := Commit
	originalBuildDate := BuildDate
	t.Cleanup(func() {
		Version = originalVersion
		Commit = originalCommit
		BuildDate = originalBuildDate
	})

	Version = "v2.0.0"
	Commit = "packaged"
	BuildDate = "2026-04-24T17:35:08Z"

	info := Current("s3ctl")

	if info.Binary != "s3ctl" {
		t.Fatalf("expected binary name, got %#v", info)
	}
	if info.Version != "v2.0.0" {
		t.Fatalf("expected version, got %#v", info)
	}
	if info.Commit != "" {
		t.Fatalf("expected placeholder commit to be omitted, got %#v", info)
	}
	if info.BuildDate != "2026-04-24T17:35:08Z" {
		t.Fatalf("expected build date, got %#v", info)
	}
}
