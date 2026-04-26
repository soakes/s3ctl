package s3ctl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolatedEnv(t *testing.T, values map[string]string) map[string]string {
	t.Helper()

	tempDir := t.TempDir()
	env := map[string]string{
		"HOME":            filepath.Join(tempDir, "home"),
		"XDG_CONFIG_HOME": filepath.Join(tempDir, "xdg"),
	}
	for key, value := range values {
		env[key] = value
	}
	return env
}

func TestBuildBucketPolicyPublicRead(t *testing.T) {
	document, err := buildBucketPolicy("demo-bucket", "public-read")
	if err != nil {
		t.Fatalf("buildBucketPolicy returned error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}

	statements := decoded["Statement"].([]any)
	statement := statements[0].(map[string]any)
	if statement["Effect"] != "Allow" {
		t.Fatalf("expected Allow effect, got %v", statement["Effect"])
	}
}

func TestBuildCredentialPolicyReadWrite(t *testing.T) {
	document, err := buildCredentialPolicy("demo-bucket", "bucket-readwrite")
	if err != nil {
		t.Fatalf("buildCredentialPolicy returned error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}

	statements := decoded["Statement"].([]any)
	if len(statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(statements))
	}
}

func TestResolveSettingsReadsConfigAndEnv(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "s3ctl.json")
	if err := os.WriteFile(
		configPath,
		[]byte(`{"bucket":"config-bucket","endpoint":"https://config.example","create_scoped_credentials":true}`),
		0o644,
	); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg, parsed, err := resolveSettings(
		[]string{"--config", configPath},
		isolatedEnv(t, map[string]string{
			"S3CTL_BUCKET_NAME": "env-bucket",
		}),
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}
	if parsed.showVersion || parsed.showHelp {
		t.Fatal("expected neither help nor version output")
	}
	if len(cfg.Buckets) != 1 || cfg.Buckets[0] != "env-bucket" {
		t.Fatalf("expected env bucket to win, got %#v", cfg.Buckets)
	}
	if cfg.Endpoint != "https://config.example" {
		t.Fatalf("expected config endpoint, got %q", cfg.Endpoint)
	}
	if !cfg.CreateScopedCredentials {
		t.Fatal("expected create_scoped_credentials from config to be true")
	}
}

func TestLoadConfigResolvesRelativePaths(t *testing.T) {
	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "policy.json")
	batchPath := filepath.Join(tempDir, "targets.csv")
	configPath := filepath.Join(tempDir, "s3ctl.json")

	if err := os.WriteFile(policyPath, []byte(`{"Version":"2012-10-17"}`), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(batchPath, []byte("bucket\napp-data\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(
		configPath,
		[]byte(`{"bucket_policy_file":"policy.json","batch_file":"targets.csv"}`),
		0o644,
	); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	src, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}
	if src.BucketPolicyFile == nil || *src.BucketPolicyFile != policyPath {
		t.Fatalf("expected resolved bucket policy path %q, got %#v", policyPath, src.BucketPolicyFile)
	}
	if src.BatchFile == nil || *src.BatchFile != batchPath {
		t.Fatalf("expected resolved batch path %q, got %#v", batchPath, src.BatchFile)
	}
}

func TestResolveSettingsSupportsLegacyEnvAliases(t *testing.T) {
	cfg, parsed, err := resolveSettings(
		[]string{},
		isolatedEnv(t, map[string]string{
			"S3CTL_BUCKET":   "legacy-bucket",
			"S3CTL_ENDPOINT": "https://legacy.example.com",
		}),
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}
	if parsed.showVersion || parsed.showHelp {
		t.Fatal("expected neither help nor version output")
	}
	if len(cfg.Buckets) != 1 || cfg.Buckets[0] != "legacy-bucket" {
		t.Fatalf("expected legacy bucket alias to be supported, got %#v", cfg.Buckets)
	}
	if cfg.Endpoint != "https://legacy.example.com" {
		t.Fatalf("expected legacy endpoint alias to be supported, got %q", cfg.Endpoint)
	}
}

func TestResolveSettingsLoadsDefaultUserConfigPath(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "s3ctl")
	configPath := filepath.Join(configDir, "config.json")

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(
		configPath,
		[]byte(`{"endpoint":"https://config.example","create_scoped_credentials":true}`),
		0o644,
	); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg, parsed, err := resolveSettings(
		[]string{"--bucket", "default-config-bucket"},
		map[string]string{
			"XDG_CONFIG_HOME": tempDir,
		},
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}
	if parsed.showVersion || parsed.showHelp {
		t.Fatal("expected neither help nor version output")
	}
	if cfg.ConfigPath != configPath {
		t.Fatalf("expected config path %q, got %q", configPath, cfg.ConfigPath)
	}
	if cfg.Endpoint != "https://config.example" {
		t.Fatalf("expected endpoint from default config, got %q", cfg.Endpoint)
	}
	if !cfg.CreateScopedCredentials {
		t.Fatal("expected create_scoped_credentials from default config to be true")
	}
}

func TestBuildProvisionTargetsFromBucketsAndBatchFile(t *testing.T) {
	tempDir := t.TempDir()
	batchPath := filepath.Join(tempDir, "buckets.csv")
	if err := os.WriteFile(
		batchPath,
		[]byte("bucket,iam_user_name,create_scoped_credentials\nlogs,custom-logs,true\n"),
		0o644,
	); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	targets, err := buildProvisionTargets(settings{
		Buckets:                  []string{"app-data"},
		EnableVersioning:         true,
		CreateScopedCredentials:  true,
		CredentialPolicyTemplate: "bucket-readwrite",
		BatchFile:                batchPath,
	})
	if err != nil {
		t.Fatalf("buildProvisionTargets returned error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	if targets[1].IAMUserName != "custom-logs" {
		t.Fatalf("expected batch IAM user override, got %q", targets[1].IAMUserName)
	}
}

func TestBuildProvisionTargetsRejectsDuplicateBuckets(t *testing.T) {
	tempDir := t.TempDir()
	batchPath := filepath.Join(tempDir, "buckets.csv")
	if err := os.WriteFile(
		batchPath,
		[]byte("bucket\napp-data\n"),
		0o644,
	); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	_, err := buildProvisionTargets(settings{
		Buckets:   []string{"app-data"},
		BatchFile: batchPath,
	})
	if err == nil {
		t.Fatal("expected duplicate bucket error, got nil")
	}
	if !strings.Contains(err.Error(), "specified more than once") {
		t.Fatalf("expected duplicate bucket error, got %q", err)
	}
}

func TestResolvedIAMUserNameUsesPrefix(t *testing.T) {
	userName, err := resolvedIAMUserName(provisionTarget{Bucket: "my-bucket"}, "svc-")
	if err != nil {
		t.Fatalf("resolvedIAMUserName returned error: %v", err)
	}
	if userName != "svc-my-bucket" {
		t.Fatalf("expected generated IAM user name, got %q", userName)
	}
}

func TestResolvedIAMUserNameAllowsEmptyPrefix(t *testing.T) {
	userName, err := resolvedIAMUserName(provisionTarget{Bucket: "my-bucket"}, "")
	if err != nil {
		t.Fatalf("resolvedIAMUserName returned error: %v", err)
	}
	if userName != "my-bucket" {
		t.Fatalf("expected generated IAM user name without prefix, got %q", userName)
	}
}

func TestResolvedIAMUserNameTrimsExplicitValue(t *testing.T) {
	userName, err := resolvedIAMUserName(provisionTarget{IAMUserName: "  app-user  "}, "svc-")
	if err != nil {
		t.Fatalf("resolvedIAMUserName returned error: %v", err)
	}
	if userName != "app-user" {
		t.Fatalf("expected trimmed IAM user name, got %q", userName)
	}
}

func TestValidateSettingsRejectsStandaloneSessionToken(t *testing.T) {
	err := validateSettings(settings{
		Buckets:      []string{"app-data"},
		SessionToken: "session-token",
	})
	if err == nil {
		t.Fatal("expected validateSettings to reject a standalone session token")
	}
	if !strings.Contains(err.Error(), "--session-token requires --access-key and --secret-key") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestResolveSettingsReadsTimeoutFromEnvAndFlag(t *testing.T) {
	cfg, _, err := resolveSettings(
		[]string{"--bucket", "app-data"},
		isolatedEnv(t, map[string]string{
			"S3CTL_TIMEOUT": "10m",
		}),
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}
	if cfg.ParsedTimeout != 10*time.Minute {
		t.Fatalf("expected env timeout, got %s", cfg.ParsedTimeout)
	}

	cfg, _, err = resolveSettings(
		[]string{"--bucket", "app-data", "--timeout", "2m"},
		isolatedEnv(t, map[string]string{
			"S3CTL_TIMEOUT": "10m",
		}),
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}
	if cfg.ParsedTimeout != 2*time.Minute {
		t.Fatalf("expected CLI timeout to win, got %s", cfg.ParsedTimeout)
	}
}

func TestMainWithEnvShowsHelpWhenRunWithoutArguments(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := MainWithEnv([]string{}, map[string]string{}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr to be empty, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("expected usage output, got %q", stdout.String())
	}
}

func TestMainWithEnvShowsProfessionalHelpOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := MainWithEnv([]string{"--help"}, map[string]string{}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr to be empty, got %q", stderr.String())
	}
	for _, fragment := range []string{
		"Examples:",
		"Primary environment variables:",
		"Built-in scoped credential policy templates:",
		"Batch CSV columns:",
	} {
		if !strings.Contains(stdout.String(), fragment) {
			t.Fatalf("expected help output to contain %q, got %q", fragment, stdout.String())
		}
	}
}

func TestMainWithEnvShowsErrorAndUsageForValidationFailures(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := MainWithEnv([]string{"--create-scoped-credentials"}, isolatedEnv(t, nil), &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected stdout to be empty, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Error: at least one --bucket") {
		t.Fatalf("expected validation error, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected usage output, got %q", stderr.String())
	}
}

func TestMainWithEnvDryRunShowsScopedCredentials(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := MainWithEnv(
		[]string{"--bucket", "app-data", "--create-scoped-credentials", "--dry-run"},
		isolatedEnv(t, nil),
		&stdout,
		&stderr,
	)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr to be empty, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Bucket create planned: yes") {
		t.Fatalf("expected dry-run planning wording, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "IAM user: app-data") {
		t.Fatalf("expected scoped credential details, got %q", stdout.String())
	}
}

func TestMainWithEnvShowsStructuredVersionOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := MainWithEnv([]string{"--version"}, map[string]string{}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr to be empty, got %q", stderr.String())
	}
	for _, fragment := range []string{
		"s3ctl",
		"Version:",
	} {
		if !strings.Contains(stdout.String(), fragment) {
			t.Fatalf("expected version output to contain %q, got %q", fragment, stdout.String())
		}
	}
}

func TestMainWithEnvSupportsJSONVersionOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := MainWithEnv([]string{"--version", "--output", "json"}, map[string]string{}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr to be empty, got %q", stderr.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if decoded["binary"] != "s3ctl" {
		t.Fatalf("expected JSON version output to include binary name, got %#v", decoded)
	}
	if decoded["version"] == "" {
		t.Fatalf("expected JSON version output to include version, got %#v", decoded)
	}
}
