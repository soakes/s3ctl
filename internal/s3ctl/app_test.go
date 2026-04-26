package s3ctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestResolveSettingsReadsConfig(t *testing.T) {
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
		isolatedEnv(t, nil),
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}
	if parsed.showVersion || parsed.showHelp {
		t.Fatal("expected neither help nor version output")
	}
	if len(cfg.Buckets) != 1 || cfg.Buckets[0] != "config-bucket" {
		t.Fatalf("expected config bucket, got %#v", cfg.Buckets)
	}
	if cfg.Endpoint != "https://config.example" {
		t.Fatalf("expected config endpoint, got %q", cfg.Endpoint)
	}
	if !cfg.CreateScopedCredentials {
		t.Fatal("expected create_scoped_credentials from config to be true")
	}
}

func TestResolveSettingsIgnoresCustomS3CTLEnvironment(t *testing.T) {
	cfg, _, err := resolveSettings(
		nil,
		isolatedEnv(t, map[string]string{
			"S3CTL_BUCKET_NAME":   "legacy-bucket",
			"S3CTL_OUTPUT_FORMAT": "json",
		}),
	)
	if err == nil {
		t.Fatal("expected missing bucket error when only custom legacy environment values are set")
	}
	if !strings.Contains(err.Error(), "at least one --bucket") {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if cfg.Output == "json" {
		t.Fatalf("expected custom output environment value to be ignored, got %#v", cfg)
	}
	if output := detectOutputFormat(nil, isolatedEnv(t, map[string]string{"S3CTL_OUTPUT_FORMAT": "json"})); output != defaultOutputFormat {
		t.Fatalf("expected custom output environment value to be ignored, got %q", output)
	}
}

func TestResolveSettingsSupportsOVHTags(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "ovh.json")
	if err := os.WriteFile(
		configPath,
		[]byte(`{"provider":"ovh","bucket":"config-bucket","region":"UK","ovh_service_name":"project123","ovh_tags":{"team":"storage"}}`),
		0o644,
	); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg, _, err := resolveSettings(
		[]string{"--config", configPath},
		isolatedEnv(t, nil),
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}
	if !reflect.DeepEqual(cfg.OVHTags, map[string]string{"team": "storage"}) {
		t.Fatalf("expected config OVH tags, got %#v", cfg.OVHTags)
	}

	cfg, _, err = resolveSettings(
		[]string{
			"--config", configPath,
			"--ovh-tag", "environment=prod",
			"--ovh-tag", "owner=platform",
		},
		isolatedEnv(t, nil),
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}

	wantTags := map[string]string{
		"environment": "prod",
		"owner":       "platform",
	}
	if !reflect.DeepEqual(cfg.OVHTags, wantTags) {
		t.Fatalf("expected CLI OVH tags to win, got %#v", cfg.OVHTags)
	}
}

func TestResolveSettingsRejectsInvalidOVHTag(t *testing.T) {
	_, _, err := resolveSettings(
		[]string{
			"--provider", "ovh",
			"--bucket", "app-data",
			"--region", "UK",
			"--ovh-service-name", "project123",
			"--ovh-tag", "missing-equals",
		},
		isolatedEnv(t, nil),
	)
	if err == nil {
		t.Fatal("expected invalid OVH tag to fail")
	}
	if !strings.Contains(err.Error(), "--ovh-tag must be key=value") {
		t.Fatalf("unexpected error: %v", err)
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

func TestResolveSettingsReadsTimeoutFromConfigAndFlag(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "s3ctl.json")
	if err := os.WriteFile(configPath, []byte(`{"bucket":"app-data","timeout":"10m"}`), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg, _, err := resolveSettings(
		[]string{"--config", configPath},
		isolatedEnv(t, nil),
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}
	if cfg.ParsedTimeout != 10*time.Minute {
		t.Fatalf("expected config timeout, got %s", cfg.ParsedTimeout)
	}

	cfg, _, err = resolveSettings(
		[]string{"--config", configPath, "--timeout", "2m"},
		isolatedEnv(t, nil),
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
		"Standard AWS SDK credential and profile discovery",
		"Built-in scoped credential policy templates:",
		"Batch CSV columns:",
	} {
		if !strings.Contains(stdout.String(), fragment) {
			t.Fatalf("expected help output to contain %q, got %q", fragment, stdout.String())
		}
	}
}

func TestMainWithEnvShowsContextualBucketHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := MainWithEnv([]string{"--bucket", "app-data", "--help"}, map[string]string{}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr to be empty, got %q", stderr.String())
	}
	for _, fragment := range []string{
		"bucket workflow help",
		"--bucket stringArray",
		"--delete",
		"--force",
		"--ovh-rotate-credentials",
		"Run s3ctl --help for every provider",
	} {
		if !strings.Contains(stdout.String(), fragment) {
			t.Fatalf("expected bucket help to contain %q, got %q", fragment, stdout.String())
		}
	}
	for _, fragment := range []string{
		"S3CTL_",
		"--ovh-client-id",
		"--access-key",
		"--iam-endpoint",
	} {
		if strings.Contains(stdout.String(), fragment) {
			t.Fatalf("expected bucket help to omit %q, got %q", fragment, stdout.String())
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

func TestMainWithEnvShowsJSONValidationErrorWhenRequested(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := MainWithEnv([]string{"--create-scoped-credentials", "--output", "json"}, isolatedEnv(t, nil), &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr to be empty, got %q", stderr.String())
	}

	var decoded commandErrorResult
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if decoded.Error.Code != "configuration_error" {
		t.Fatalf("expected configuration error code, got %#v", decoded.Error)
	}
	if !strings.Contains(decoded.Error.Message, "at least one --bucket") {
		t.Fatalf("expected validation message, got %#v", decoded.Error)
	}
	if strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("expected JSON error without usage text, got %q", stdout.String())
	}
}

func TestMainWithEnvShowsJSONRuntimeErrorFromConfigOutput(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "s3ctl.json")
	if err := os.WriteFile(configPath, []byte(`{"bucket":"app-data","output":"json"}`), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	previous := newS3APIClient
	newS3APIClient = func(context.Context, settings) (s3API, error) {
		return nil, errors.New("client unavailable")
	}
	t.Cleanup(func() {
		newS3APIClient = previous
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := MainWithEnv([]string{"--config", configPath}, isolatedEnv(t, nil), &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr to be empty, got %q", stderr.String())
	}

	var decoded commandErrorResult
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if decoded.Operation != operationProvision || decoded.Error.Code != "operation_failed" {
		t.Fatalf("unexpected JSON error: %#v", decoded)
	}
	if decoded.ConfigFile != configPath {
		t.Fatalf("expected config path %q, got %q", configPath, decoded.ConfigFile)
	}
	if decoded.Error.Message != "client unavailable" {
		t.Fatalf("expected runtime message, got %#v", decoded.Error)
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
