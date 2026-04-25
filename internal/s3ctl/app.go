// Package s3ctl implements the s3ctl CLI application and provisioning logic.
package s3ctl

import (
	"context"
	"crypto/tls"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/spf13/pflag"

	"github.com/soakes/s3ctl/internal/buildinfo"
)

const binaryName = "s3ctl"

const (
	defaultRegion                   = "us-east-1"
	defaultIAMUserPrefix            = ""
	defaultIAMPath                  = ""
	defaultCredentialPolicyTemplate = "bucket-readwrite"
	defaultConfigFileName           = "config.json"
	defaultOutputFormat             = "text"
	defaultProvisionTimeout         = 30 * time.Second
)

var bucketNotFoundPattern = regexp.MustCompile(`\b(NotFound|NoSuchBucket)\b`)

var bucketPolicyTemplates = map[string]string{
	"public-read":             "Allow public read access to objects in the bucket",
	"deny-insecure-transport": "Deny requests that do not use TLS",
}

var envAliases = struct {
	ConfigFile              []string
	BucketName              []string
	BucketNames             []string
	BatchFile               []string
	EndpointURL             []string
	Region                  []string
	Profile                 []string
	AccessKeyID             []string
	SecretAccessKey         []string
	SessionToken            []string
	InsecureSkipTLSVerify   []string
	EnableVersioning        []string
	BucketPolicyFile        []string
	BucketPolicyTemplate    []string
	CreateScopedCredentials []string
	IAMEndpointURL          []string
	IAMUserName             []string
	IAMUserPrefix           []string
	IAMPath                 []string
	CredentialPolicyTmpl    []string
	OutputFormat            []string
	DryRun                  []string
}{
	ConfigFile:              []string{"S3CTL_CONFIG_FILE", "S3CTL_CONFIG"},
	BucketName:              []string{"S3CTL_BUCKET_NAME", "S3CTL_BUCKET"},
	BucketNames:             []string{"S3CTL_BUCKET_NAMES"},
	BatchFile:               []string{"S3CTL_BATCH_FILE"},
	EndpointURL:             []string{"S3CTL_ENDPOINT_URL", "S3CTL_ENDPOINT", "AWS_ENDPOINT_URL", "S3_ENDPOINT"},
	Region:                  []string{"S3CTL_REGION", "AWS_REGION", "AWS_DEFAULT_REGION"},
	Profile:                 []string{"S3CTL_PROFILE", "AWS_PROFILE"},
	AccessKeyID:             []string{"S3CTL_ACCESS_KEY_ID", "S3CTL_ACCESS_KEY", "AWS_ACCESS_KEY_ID"},
	SecretAccessKey:         []string{"S3CTL_SECRET_ACCESS_KEY", "S3CTL_SECRET_KEY", "AWS_SECRET_ACCESS_KEY"},
	SessionToken:            []string{"S3CTL_SESSION_TOKEN", "AWS_SESSION_TOKEN"},
	InsecureSkipTLSVerify:   []string{"S3CTL_INSECURE_SKIP_TLS_VERIFY", "S3CTL_INSECURE"},
	EnableVersioning:        []string{"S3CTL_ENABLE_VERSIONING"},
	BucketPolicyFile:        []string{"S3CTL_BUCKET_POLICY_FILE"},
	BucketPolicyTemplate:    []string{"S3CTL_BUCKET_POLICY_TEMPLATE"},
	CreateScopedCredentials: []string{"S3CTL_CREATE_SCOPED_CREDENTIALS"},
	IAMEndpointURL:          []string{"S3CTL_IAM_ENDPOINT_URL"},
	IAMUserName:             []string{"S3CTL_IAM_USER_NAME"},
	IAMUserPrefix:           []string{"S3CTL_IAM_USER_PREFIX"},
	IAMPath:                 []string{"S3CTL_IAM_PATH"},
	CredentialPolicyTmpl:    []string{"S3CTL_CREDENTIAL_POLICY_TEMPLATE"},
	OutputFormat:            []string{"S3CTL_OUTPUT_FORMAT", "S3CTL_OUTPUT"},
	DryRun:                  []string{"S3CTL_DRY_RUN"},
}

type settings struct {
	ConfigPath               string   `json:"-"`
	Bucket                   string   `json:"bucket"`
	Buckets                  []string `json:"buckets"`
	BatchFile                string   `json:"batch_file"`
	Endpoint                 string   `json:"endpoint"`
	Region                   string   `json:"region"`
	Profile                  string   `json:"profile"`
	AccessKey                string   `json:"access_key"`
	SecretKey                string   `json:"secret_key"`
	SessionToken             string   `json:"session_token"`
	Insecure                 bool     `json:"insecure"`
	EnableVersioning         bool     `json:"enable_versioning"`
	BucketPolicyFile         string   `json:"bucket_policy_file"`
	BucketPolicyTemplate     string   `json:"bucket_policy_template"`
	CreateScopedCredentials  bool     `json:"create_scoped_credentials"`
	IAMEndpoint              string   `json:"iam_endpoint"`
	IAMUserName              string   `json:"iam_user_name"`
	IAMUserPrefix            string   `json:"iam_user_prefix"`
	IAMPath                  string   `json:"iam_path"`
	CredentialPolicyTemplate string   `json:"credential_policy_template"`
	Output                   string   `json:"output"`
	DryRun                   bool     `json:"dry_run"`
}

type source struct {
	Buckets                  *[]string
	BatchFile                *string
	Endpoint                 *string
	Region                   *string
	Profile                  *string
	AccessKey                *string
	SecretKey                *string
	SessionToken             *string
	Insecure                 *bool
	EnableVersioning         *bool
	BucketPolicyFile         *string
	BucketPolicyTemplate     *string
	CreateScopedCredentials  *bool
	IAMEndpoint              *string
	IAMUserName              *string
	IAMUserPrefix            *string
	IAMPath                  *string
	CredentialPolicyTemplate *string
	Output                   *string
	DryRun                   *bool
}

type cliFlags struct {
	Config                   string
	Buckets                  []string
	BatchFile                string
	Endpoint                 string
	Region                   string
	Profile                  string
	AccessKey                string
	SecretKey                string
	SessionToken             string
	Insecure                 bool
	EnableVersioning         bool
	BucketPolicyFile         string
	BucketPolicyTemplate     string
	CreateScopedCredentials  bool
	IAMEndpoint              string
	IAMUserName              string
	IAMUserPrefix            string
	IAMPath                  string
	CredentialPolicyTemplate string
	Output                   string
	DryRun                   bool
	Help                     bool
	Version                  bool
}

type parseResult struct {
	source      source
	showHelp    bool
	showVersion bool
}

type provisionTarget struct {
	Bucket                   string
	EnableVersioning         bool
	BucketPolicyFile         string
	BucketPolicyTemplate     string
	CreateScopedCredentials  bool
	IAMUserName              string
	CredentialPolicyTemplate string
}

type provisionResult struct {
	DryRun        bool             `json:"dry_run"`
	ConfigFile    string           `json:"config_file,omitempty"`
	ResourceCount int              `json:"resource_count"`
	Resources     []resourceResult `json:"resources"`
}

type resourceResult struct {
	BucketName          string                  `json:"bucket_name"`
	Endpoint            string                  `json:"endpoint,omitempty"`
	Region              string                  `json:"region"`
	Created             bool                    `json:"created"`
	VersioningEnabled   bool                    `json:"versioning_enabled"`
	BucketPolicyApplied bool                    `json:"bucket_policy_applied"`
	BucketPolicySource  string                  `json:"bucket_policy_source,omitempty"`
	ScopedCredentials   *scopedCredentialResult `json:"scoped_credentials,omitempty"`
}

type bucketExistsError struct {
	Name string
}

func (e bucketExistsError) Error() string {
	return fmt.Sprintf("bucket %q already exists", e.Name)
}

// Main runs the CLI using the current process environment.
func Main(args []string, stdout, stderr io.Writer) int {
	return MainWithEnv(args, envMap(os.Environ()), stdout, stderr)
}

// MainWithEnv runs the CLI with an explicit environment map.
func MainWithEnv(args []string, env map[string]string, stdout, stderr io.Writer) int {
	if shouldShowIntroHelp(args, env) {
		if err := writeUsage(stdout); err != nil {
			return 1
		}
		return 0
	}

	cfg, parsed, err := resolveSettings(args, env)
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) || parsed.showHelp {
			if writeErr := writeUsage(stdout); writeErr != nil {
				return 1
			}
			return 0
		}
		if _, writeErr := fmt.Fprintf(stderr, "Error: %s\n\n", err); writeErr != nil {
			return 1
		}
		if writeErr := writeUsage(stderr); writeErr != nil {
			return 1
		}
		return 1
	}

	if parsed.showHelp {
		if err := writeUsage(stdout); err != nil {
			return 1
		}
		return 0
	}

	if parsed.showVersion {
		if cfg.Output == "json" {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(buildinfo.Current(binaryName)); err != nil {
				return 1
			}
			return 0
		}

		if _, err := fmt.Fprintln(stdout, buildinfo.Summary(binaryName)); err != nil {
			return 1
		}
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultProvisionTimeout)
	defer cancel()

	result, err := provision(ctx, cfg)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Error: %s\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	if cfg.Output == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Error: %s\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		return 0
	}

	if _, err := fmt.Fprintln(stdout, renderText(result)); err != nil {
		return 1
	}
	return 0
}

func resolveSettings(args []string, env map[string]string) (settings, parseResult, error) {
	cliParsed, err := parseFlags(args)
	if err != nil {
		return settings{}, parseResult{}, err
	}

	if cliParsed.showHelp {
		return settings{}, cliParsed, nil
	}

	envSource, err := loadEnv(env)
	if err != nil {
		return settings{}, parseResult{}, err
	}

	if cliParsed.showVersion {
		return mergeSources(source{}, envSource, cliParsed.source), cliParsed, nil
	}

	configPath, err := resolveConfigPath(args, env)
	if err != nil {
		return settings{}, parseResult{}, err
	}

	configSource, err := loadConfig(configPath)
	if err != nil {
		return settings{}, parseResult{}, err
	}

	cfg := mergeSources(configSource, envSource, cliParsed.source)
	cfg.ConfigPath = configPath
	if err := validateSettings(cfg); err != nil {
		return settings{}, cliParsed, err
	}

	return cfg, cliParsed, nil
}

func parseFlags(args []string) (parseResult, error) {
	flags := cliFlags{}
	fs := newFlagSet(&flags)

	if err := fs.Parse(args); err != nil {
		return parseResult{}, err
	}

	return parseResult{
		source: source{
			Buckets:                  changedStringSlice(fs, "bucket", flags.Buckets),
			BatchFile:                changedString(fs, "batch-file", flags.BatchFile),
			Endpoint:                 changedString(fs, "endpoint", flags.Endpoint),
			Region:                   changedString(fs, "region", flags.Region),
			Profile:                  changedString(fs, "profile", flags.Profile),
			AccessKey:                changedString(fs, "access-key", flags.AccessKey),
			SecretKey:                changedString(fs, "secret-key", flags.SecretKey),
			SessionToken:             changedString(fs, "session-token", flags.SessionToken),
			Insecure:                 changedBool(fs, "insecure", flags.Insecure),
			EnableVersioning:         changedBool(fs, "enable-versioning", flags.EnableVersioning),
			BucketPolicyFile:         changedString(fs, "bucket-policy-file", flags.BucketPolicyFile),
			BucketPolicyTemplate:     changedString(fs, "bucket-policy-template", flags.BucketPolicyTemplate),
			CreateScopedCredentials:  changedBool(fs, "create-scoped-credentials", flags.CreateScopedCredentials),
			IAMEndpoint:              changedString(fs, "iam-endpoint", flags.IAMEndpoint),
			IAMUserName:              changedString(fs, "iam-user-name", flags.IAMUserName),
			IAMUserPrefix:            changedString(fs, "iam-user-prefix", flags.IAMUserPrefix),
			IAMPath:                  changedString(fs, "iam-path", flags.IAMPath),
			CredentialPolicyTemplate: changedString(fs, "credential-policy-template", flags.CredentialPolicyTemplate),
			Output:                   changedString(fs, "output", flags.Output),
			DryRun:                   changedBool(fs, "dry-run", flags.DryRun),
		},
		showHelp:    flags.Help,
		showVersion: flags.Version,
	}, nil
}

func newFlagSet(flags *cliFlags) *pflag.FlagSet {
	fs := pflag.NewFlagSet(binaryName, pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.SortFlags = false

	fs.StringVarP(&flags.Config, "config", "c", "", "Path to a JSON config file")
	fs.StringArrayVarP(&flags.Buckets, "bucket", "b", nil, "Bucket name to create; may be specified more than once")
	fs.StringVar(&flags.BatchFile, "batch-file", "", "Path to a CSV file describing multiple bucket requests")
	fs.StringVar(&flags.Endpoint, "endpoint", "", "S3 endpoint URL for S3-compatible services")
	fs.StringVar(&flags.Region, "region", defaultRegion, "Bucket region")
	fs.StringVar(&flags.Profile, "profile", "", "AWS profile name for SDK configuration")
	fs.StringVar(&flags.AccessKey, "access-key", "", "Access key for the S3 API client")
	fs.StringVar(&flags.SecretKey, "secret-key", "", "Secret key for the S3 API client")
	fs.StringVar(&flags.SessionToken, "session-token", "", "Optional session token for the S3 API client")
	fs.BoolVar(&flags.Insecure, "insecure", false, "Disable TLS certificate verification")
	fs.BoolVar(&flags.EnableVersioning, "enable-versioning", false, "Enable bucket versioning after creation")
	fs.StringVar(&flags.BucketPolicyFile, "bucket-policy-file", "", "Path to a JSON bucket policy document")
	fs.StringVar(&flags.BucketPolicyTemplate, "bucket-policy-template", "", "Built-in bucket policy template")
	fs.BoolVar(&flags.CreateScopedCredentials, "create-scoped-credentials", false, "Create a new scoped IAM-style user and access key for each bucket")
	fs.StringVar(&flags.IAMEndpoint, "iam-endpoint", "", "Override the IAM API endpoint used for scoped credential provisioning")
	fs.StringVar(&flags.IAMUserName, "iam-user-name", "", "Explicit IAM user name for a single bucket run")
	fs.StringVar(&flags.IAMUserPrefix, "iam-user-prefix", defaultIAMUserPrefix, "Optional prefix used when generating IAM user names automatically")
	fs.StringVar(&flags.IAMPath, "iam-path", defaultIAMPath, "Optional IAM path used for generated users")
	fs.StringVar(&flags.CredentialPolicyTemplate, "credential-policy-template", defaultCredentialPolicyTemplate, "Built-in scoped credential policy template")
	fs.StringVarP(&flags.Output, "output", "o", defaultOutputFormat, "Output format: text or json")
	fs.BoolVar(&flags.DryRun, "dry-run", false, "Show the planned actions without making changes")
	fs.BoolVarP(&flags.Help, "help", "h", false, "Show help")
	fs.BoolVar(&flags.Version, "version", false, "Show version information")

	return fs
}

func writeUsage(w io.Writer) error {
	_, err := io.WriteString(w, usageText())
	return err
}

func usageText() string {
	flags := cliFlags{}
	fs := newFlagSet(&flags)
	var builder strings.Builder

	_, _ = fmt.Fprintf(&builder, `%s provisions S3 buckets and can automatically create scoped access credentials.

Usage:
  %s [flags]

Examples:
  %s --bucket app-data --endpoint https://objects.example.com --region us-east-1
  %s --bucket app-data --create-scoped-credentials --credential-policy-template bucket-readwrite
  %s --bucket app-data --bucket logs --create-scoped-credentials --dry-run --output json
  %s --batch-file ./examples/s3ctl-batch.csv --create-scoped-credentials
  %s --bucket app-data --dry-run
  %s --config ./examples/s3ctl.json --dry-run --output json

Flags:
`, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName)
	builder.WriteString(fs.FlagUsagesWrapped(100))

	builder.WriteString(`
Configuration precedence:
  1. CLI flags
  2. Environment variables
  3. JSON config file
  4. Built-in defaults

Default user config file:
  $XDG_CONFIG_HOME/s3ctl/config.json
  $HOME/.config/s3ctl/config.json

Primary environment variables:
  S3CTL_CONFIG_FILE
  S3CTL_BUCKET_NAME
  S3CTL_BUCKET_NAMES
  S3CTL_BATCH_FILE
  S3CTL_ENDPOINT_URL
  S3CTL_REGION
  S3CTL_PROFILE
  S3CTL_ACCESS_KEY_ID
  S3CTL_SECRET_ACCESS_KEY
  S3CTL_SESSION_TOKEN
  S3CTL_INSECURE_SKIP_TLS_VERIFY
  S3CTL_ENABLE_VERSIONING
  S3CTL_BUCKET_POLICY_FILE
  S3CTL_BUCKET_POLICY_TEMPLATE
  S3CTL_CREATE_SCOPED_CREDENTIALS
  S3CTL_IAM_ENDPOINT_URL
  S3CTL_IAM_USER_NAME
  S3CTL_IAM_USER_PREFIX
  S3CTL_IAM_PATH
  S3CTL_CREDENTIAL_POLICY_TEMPLATE
  S3CTL_OUTPUT_FORMAT
  S3CTL_DRY_RUN

Built-in bucket policy templates:
`)
	for _, name := range sortedKeys(bucketPolicyTemplates) {
		_, _ = fmt.Fprintf(&builder, "  %s\n      %s\n", name, bucketPolicyTemplates[name])
	}

	builder.WriteString(`
Built-in scoped credential policy templates:
`)
	for _, name := range sortedKeys(credentialPolicyTemplates) {
		_, _ = fmt.Fprintf(&builder, "  %s\n      %s\n", name, credentialPolicyTemplates[name])
	}

	builder.WriteString(`
Batch CSV columns:
  bucket
  iam_user_name
  enable_versioning
  bucket_policy_file
  bucket_policy_template
  create_scoped_credentials
  credential_policy_template

Notes:
  Scoped credential provisioning uses the IAM API. By default this targets AWS IAM.
  Use --iam-endpoint when you need a different IAM-compatible endpoint.
`)

	return builder.String()
}

func loadConfig(path string) (source, error) {
	if path == "" {
		return source{}, nil
	}

	if filepath.Ext(path) != ".json" {
		return source{}, fmt.Errorf("config file must end with .json: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return source{}, err
	}

	var cfg settings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return source{}, err
	}

	configDir := filepath.Dir(path)
	batchFile := resolveRelativePathIfSet(configDir, cfg.BatchFile)
	bucketPolicyFile := resolveRelativePathIfSet(configDir, cfg.BucketPolicyFile)

	buckets := make([]string, 0, len(cfg.Buckets)+1)
	if strings.TrimSpace(cfg.Bucket) != "" {
		buckets = append(buckets, cfg.Bucket)
	}
	buckets = append(buckets, cfg.Buckets...)

	return source{
		Buckets:                  stringSlicePtrIfSet(data, []string{"bucket", "buckets"}, buckets),
		BatchFile:                stringPtrIfField(data, "batch_file", batchFile),
		Endpoint:                 stringPtrIfField(data, "endpoint", cfg.Endpoint),
		Region:                   stringPtrIfField(data, "region", cfg.Region),
		Profile:                  stringPtrIfField(data, "profile", cfg.Profile),
		AccessKey:                stringPtrIfField(data, "access_key", cfg.AccessKey),
		SecretKey:                stringPtrIfField(data, "secret_key", cfg.SecretKey),
		SessionToken:             stringPtrIfField(data, "session_token", cfg.SessionToken),
		Insecure:                 boolPtrIfSet(data, "insecure", cfg.Insecure),
		EnableVersioning:         boolPtrIfSet(data, "enable_versioning", cfg.EnableVersioning),
		BucketPolicyFile:         stringPtrIfField(data, "bucket_policy_file", bucketPolicyFile),
		BucketPolicyTemplate:     stringPtrIfField(data, "bucket_policy_template", cfg.BucketPolicyTemplate),
		CreateScopedCredentials:  boolPtrIfSet(data, "create_scoped_credentials", cfg.CreateScopedCredentials),
		IAMEndpoint:              stringPtrIfField(data, "iam_endpoint", cfg.IAMEndpoint),
		IAMUserName:              stringPtrIfField(data, "iam_user_name", cfg.IAMUserName),
		IAMUserPrefix:            stringPtrIfField(data, "iam_user_prefix", cfg.IAMUserPrefix),
		IAMPath:                  stringPtrIfField(data, "iam_path", cfg.IAMPath),
		CredentialPolicyTemplate: stringPtrIfField(data, "credential_policy_template", cfg.CredentialPolicyTemplate),
		Output:                   stringPtrIfField(data, "output", cfg.Output),
		DryRun:                   boolPtrIfSet(data, "dry_run", cfg.DryRun),
	}, nil
}

func loadEnv(env map[string]string) (source, error) {
	insecure, err := envBoolAliases(env, envAliases.InsecureSkipTLSVerify...)
	if err != nil {
		return source{}, err
	}

	enableVersioning, err := envBoolAliases(env, envAliases.EnableVersioning...)
	if err != nil {
		return source{}, err
	}

	createScopedCredentials, err := envBoolAliases(env, envAliases.CreateScopedCredentials...)
	if err != nil {
		return source{}, err
	}

	dryRun, err := envBoolAliases(env, envAliases.DryRun...)
	if err != nil {
		return source{}, err
	}

	buckets := make([]string, 0, 4)
	if singleBucket := envValue(env, envAliases.BucketName...); singleBucket != "" {
		buckets = append(buckets, singleBucket)
	}
	buckets = append(buckets, parseCommaSeparatedValues(envValue(env, envAliases.BucketNames...))...)

	return source{
		Buckets:                  stringSlicePtrIfValue(dedupeStringsPreserveOrder(buckets)),
		BatchFile:                strPtrIfSet(envValue(env, envAliases.BatchFile...)),
		Endpoint:                 strPtrIfSet(envValue(env, envAliases.EndpointURL...)),
		Region:                   strPtrIfSet(envValue(env, envAliases.Region...)),
		Profile:                  strPtrIfSet(envValue(env, envAliases.Profile...)),
		AccessKey:                strPtrIfSet(envValue(env, envAliases.AccessKeyID...)),
		SecretKey:                strPtrIfSet(envValue(env, envAliases.SecretAccessKey...)),
		SessionToken:             strPtrIfSet(envValue(env, envAliases.SessionToken...)),
		Insecure:                 insecure,
		EnableVersioning:         enableVersioning,
		BucketPolicyFile:         strPtrIfSet(envValue(env, envAliases.BucketPolicyFile...)),
		BucketPolicyTemplate:     strPtrIfSet(envValue(env, envAliases.BucketPolicyTemplate...)),
		CreateScopedCredentials:  createScopedCredentials,
		IAMEndpoint:              strPtrIfSet(envValue(env, envAliases.IAMEndpointURL...)),
		IAMUserName:              strPtrIfSet(envValue(env, envAliases.IAMUserName...)),
		IAMUserPrefix:            envStringPtr(env, envAliases.IAMUserPrefix...),
		IAMPath:                  envStringPtr(env, envAliases.IAMPath...),
		CredentialPolicyTemplate: strPtrIfSet(envValue(env, envAliases.CredentialPolicyTmpl...)),
		Output:                   strPtrIfSet(envValue(env, envAliases.OutputFormat...)),
		DryRun:                   dryRun,
	}, nil
}

func mergeSources(sources ...source) settings {
	cfg := settings{
		Region:                   defaultRegion,
		IAMUserPrefix:            defaultIAMUserPrefix,
		IAMPath:                  defaultIAMPath,
		CredentialPolicyTemplate: defaultCredentialPolicyTemplate,
		Output:                   defaultOutputFormat,
	}

	for _, src := range sources {
		if src.Profile != nil {
			cfg.Profile = *src.Profile
			cfg.AccessKey = ""
			cfg.SecretKey = ""
			cfg.SessionToken = ""
		}
		if src.AccessKey != nil || src.SecretKey != nil || src.SessionToken != nil {
			cfg.Profile = ""
		}
		if src.Buckets != nil {
			cfg.Buckets = append([]string{}, (*src.Buckets)...)
		}
		if src.BatchFile != nil {
			cfg.BatchFile = *src.BatchFile
		}
		if src.Endpoint != nil {
			cfg.Endpoint = *src.Endpoint
		}
		if src.Region != nil {
			cfg.Region = *src.Region
		}
		if src.AccessKey != nil {
			cfg.AccessKey = *src.AccessKey
		}
		if src.SecretKey != nil {
			cfg.SecretKey = *src.SecretKey
		}
		if src.SessionToken != nil {
			cfg.SessionToken = *src.SessionToken
		}
		if src.Insecure != nil {
			cfg.Insecure = *src.Insecure
		}
		if src.EnableVersioning != nil {
			cfg.EnableVersioning = *src.EnableVersioning
		}
		if src.BucketPolicyFile != nil {
			cfg.BucketPolicyFile = *src.BucketPolicyFile
		}
		if src.BucketPolicyTemplate != nil {
			cfg.BucketPolicyTemplate = *src.BucketPolicyTemplate
		}
		if src.CreateScopedCredentials != nil {
			cfg.CreateScopedCredentials = *src.CreateScopedCredentials
		}
		if src.IAMEndpoint != nil {
			cfg.IAMEndpoint = *src.IAMEndpoint
		}
		if src.IAMUserName != nil {
			cfg.IAMUserName = *src.IAMUserName
		}
		if src.IAMUserPrefix != nil {
			cfg.IAMUserPrefix = *src.IAMUserPrefix
		}
		if src.IAMPath != nil {
			cfg.IAMPath = *src.IAMPath
		}
		if src.CredentialPolicyTemplate != nil {
			cfg.CredentialPolicyTemplate = *src.CredentialPolicyTemplate
		}
		if src.Output != nil {
			cfg.Output = *src.Output
		}
		if src.DryRun != nil {
			cfg.DryRun = *src.DryRun
		}
	}

	cfg.Buckets = dedupeStringsPreserveOrder(cfg.Buckets)
	return cfg
}

func validateSettings(cfg settings) error {
	if len(cfg.Buckets) == 0 && cfg.BatchFile == "" {
		return errors.New("at least one --bucket or a --batch-file is required unless provided via environment or config")
	}
	if cfg.BucketPolicyFile != "" && cfg.BucketPolicyTemplate != "" {
		return errors.New("use either --bucket-policy-file or --bucket-policy-template, not both")
	}
	if cfg.AccessKey != "" && cfg.SecretKey == "" {
		return errors.New("--access-key and --secret-key must be provided together")
	}
	if cfg.AccessKey == "" && cfg.SecretKey != "" {
		return errors.New("--access-key and --secret-key must be provided together")
	}
	if cfg.SessionToken != "" && (cfg.AccessKey == "" || cfg.SecretKey == "") {
		return errors.New("--session-token requires --access-key and --secret-key")
	}
	if cfg.Profile != "" && (cfg.AccessKey != "" || cfg.SecretKey != "" || cfg.SessionToken != "") {
		return errors.New("use either --profile or explicit credentials, not both")
	}
	if cfg.Output != "text" && cfg.Output != "json" {
		return fmt.Errorf("--output must be either text or json, got %q", cfg.Output)
	}
	if cfg.BucketPolicyTemplate != "" {
		if _, ok := bucketPolicyTemplates[cfg.BucketPolicyTemplate]; !ok {
			return fmt.Errorf("unsupported bucket policy template %q", cfg.BucketPolicyTemplate)
		}
	}
	if cfg.CredentialPolicyTemplate != "" {
		if _, ok := credentialPolicyTemplates[cfg.CredentialPolicyTemplate]; !ok {
			return fmt.Errorf("unsupported credential policy template %q", cfg.CredentialPolicyTemplate)
		}
	}
	if cfg.IAMUserName != "" && !cfg.CreateScopedCredentials {
		return errors.New("--iam-user-name requires --create-scoped-credentials")
	}
	return nil
}

func provision(ctx context.Context, cfg settings) (provisionResult, error) {
	targets, err := buildProvisionTargets(cfg)
	if err != nil {
		return provisionResult{}, err
	}

	result := provisionResult{
		DryRun:        cfg.DryRun,
		ConfigFile:    cfg.ConfigPath,
		ResourceCount: len(targets),
		Resources:     make([]resourceResult, 0, len(targets)),
	}

	var s3Client *s3.Client
	var iamClient iamAPI

	if !cfg.DryRun {
		s3Client, err = newS3Client(ctx, cfg)
		if err != nil {
			return provisionResult{}, err
		}
	}

	for _, target := range targets {
		resource := resourceResult{
			BucketName: target.Bucket,
			Endpoint:   cfg.Endpoint,
			Region:     cfg.Region,
		}

		bucketPolicyDocument, bucketPolicySource, err := resolveBucketPolicy(target)
		if err != nil {
			return provisionResult{}, err
		}

		if cfg.DryRun {
			resource.Created = true
			resource.VersioningEnabled = target.EnableVersioning
			resource.BucketPolicyApplied = bucketPolicyDocument != ""
			resource.BucketPolicySource = bucketPolicySource

			if target.CreateScopedCredentials {
				userName, err := resolvedIAMUserName(target, cfg.IAMUserPrefix)
				if err != nil {
					return provisionResult{}, err
				}
				resource.ScopedCredentials = &scopedCredentialResult{
					UserName:        userName,
					PolicyTemplate:  target.CredentialPolicyTemplate,
					AccessKeyID:     "(generated on apply)",
					SecretAccessKey: "(generated on apply)",
				}
			}

			result.Resources = append(result.Resources, resource)
			continue
		}

		exists, err := bucketExists(ctx, s3Client, target.Bucket)
		if err != nil {
			return provisionResult{}, err
		}
		if exists {
			return provisionResult{}, bucketExistsError{Name: target.Bucket}
		}

		if err := createBucket(ctx, s3Client, target.Bucket, cfg.Region); err != nil {
			return provisionResult{}, err
		}
		resource.Created = true

		if target.EnableVersioning {
			if err := enableVersioning(ctx, s3Client, target.Bucket); err != nil {
				return provisionResult{}, err
			}
			resource.VersioningEnabled = true
		}

		if bucketPolicyDocument != "" {
			if err := applyBucketPolicy(ctx, s3Client, target.Bucket, bucketPolicyDocument); err != nil {
				return provisionResult{}, err
			}
			resource.BucketPolicyApplied = true
			resource.BucketPolicySource = bucketPolicySource
		}

		if target.CreateScopedCredentials {
			if iamClient == nil {
				iamClient, err = newIAMClient(ctx, cfg)
				if err != nil {
					return provisionResult{}, err
				}
			}

			credentials, err := createScopedCredentials(ctx, iamClient, target, cfg)
			if err != nil {
				return provisionResult{}, err
			}
			resource.ScopedCredentials = &credentials
		}

		result.Resources = append(result.Resources, resource)
	}

	return result, nil
}

func buildProvisionTargets(cfg settings) ([]provisionTarget, error) {
	targets := make([]provisionTarget, 0, len(cfg.Buckets))
	for _, bucket := range dedupeStringsPreserveOrder(cfg.Buckets) {
		if strings.TrimSpace(bucket) == "" {
			continue
		}

		targets = append(targets, provisionTarget{
			Bucket:                   bucket,
			EnableVersioning:         cfg.EnableVersioning,
			BucketPolicyFile:         cfg.BucketPolicyFile,
			BucketPolicyTemplate:     cfg.BucketPolicyTemplate,
			CreateScopedCredentials:  cfg.CreateScopedCredentials,
			IAMUserName:              cfg.IAMUserName,
			CredentialPolicyTemplate: cfg.CredentialPolicyTemplate,
		})
	}

	if cfg.BatchFile != "" {
		batchTargets, err := loadBatchTargets(cfg.BatchFile, cfg)
		if err != nil {
			return nil, err
		}
		targets = append(targets, batchTargets...)
	}

	if len(targets) == 0 {
		return nil, errors.New("no bucket targets were resolved from flags, environment, config, or batch file")
	}

	if cfg.IAMUserName != "" && len(targets) > 1 {
		return nil, errors.New("--iam-user-name can only be used when provisioning a single bucket")
	}

	seenBuckets := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if _, exists := seenBuckets[target.Bucket]; exists {
			return nil, fmt.Errorf("bucket target %q was specified more than once; each bucket must only appear once per run", target.Bucket)
		}
		seenBuckets[target.Bucket] = struct{}{}
	}

	return targets, nil
}

func loadBatchTargets(path string, cfg settings) ([]provisionTarget, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	headers, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("batch file %s is empty", path)
		}
		return nil, err
	}

	headerIndex := make(map[string]int, len(headers))
	for index, header := range headers {
		headerIndex[normalizeCSVHeader(header)] = index
	}

	if !hasCSVHeader(headerIndex, "bucket", "bucket_name", "name") {
		return nil, fmt.Errorf("batch file %s must include a bucket column", path)
	}

	batchDir := filepath.Dir(path)
	targets := make([]provisionTarget, 0)
	lineNumber := 1

	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		lineNumber++

		if csvRecordBlank(record) || csvRecordComment(record) {
			continue
		}

		bucket := csvField(record, headerIndex, "bucket", "bucket_name", "name")
		if bucket == "" {
			return nil, fmt.Errorf("batch file %s line %d is missing a bucket value", path, lineNumber)
		}

		target := provisionTarget{
			Bucket:                   bucket,
			EnableVersioning:         cfg.EnableVersioning,
			BucketPolicyFile:         cfg.BucketPolicyFile,
			BucketPolicyTemplate:     cfg.BucketPolicyTemplate,
			CreateScopedCredentials:  cfg.CreateScopedCredentials,
			IAMUserName:              cfg.IAMUserName,
			CredentialPolicyTemplate: cfg.CredentialPolicyTemplate,
		}

		if value := csvField(record, headerIndex, "iam_user_name", "iam_user", "user_name"); value != "" {
			target.IAMUserName = value
		}
		if value := csvField(record, headerIndex, "bucket_policy_file"); value != "" {
			target.BucketPolicyFile = resolveRelativePath(batchDir, value)
			target.BucketPolicyTemplate = ""
		}
		if value := csvField(record, headerIndex, "bucket_policy_template"); value != "" {
			target.BucketPolicyTemplate = value
			target.BucketPolicyFile = ""
		}
		if value := csvField(record, headerIndex, "credential_policy_template", "iam_policy_template"); value != "" {
			target.CredentialPolicyTemplate = value
		}
		if value := csvField(record, headerIndex, "enable_versioning", "versioning"); value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("batch file %s line %d has invalid enable_versioning value %q", path, lineNumber, value)
			}
			target.EnableVersioning = parsed
		}
		if value := csvField(record, headerIndex, "create_scoped_credentials", "create_credentials", "create_iam_user"); value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("batch file %s line %d has invalid create_scoped_credentials value %q", path, lineNumber, value)
			}
			target.CreateScopedCredentials = parsed
		}

		if target.BucketPolicyTemplate != "" {
			if _, ok := bucketPolicyTemplates[target.BucketPolicyTemplate]; !ok {
				return nil, fmt.Errorf("batch file %s line %d uses unsupported bucket policy template %q", path, lineNumber, target.BucketPolicyTemplate)
			}
		}
		if target.CredentialPolicyTemplate != "" {
			if _, ok := credentialPolicyTemplates[target.CredentialPolicyTemplate]; !ok {
				return nil, fmt.Errorf("batch file %s line %d uses unsupported credential policy template %q", path, lineNumber, target.CredentialPolicyTemplate)
			}
		}

		targets = append(targets, target)
	}

	return targets, nil
}

func newAWSConfig(ctx context.Context, cfg settings) (aws.Config, error) {
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}

	if cfg.Profile != "" {
		loadOptions = append(loadOptions, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}

	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		loadOptions = append(
			loadOptions,
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken),
			),
		)
	}

	if cfg.Insecure {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		loadOptions = append(loadOptions, awsconfig.WithHTTPClient(&http.Client{Transport: transport}))
	}

	return awsconfig.LoadDefaultConfig(ctx, loadOptions...)
}

func newS3Client(ctx context.Context, cfg settings) (*s3.Client, error) {
	awsCfg, err := newAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = true
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	}), nil
}

func bucketExists(ctx context.Context, client *s3.Client, bucket string) (bool, error) {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	if err == nil {
		return true, nil
	}

	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusNotFound {
		return false, nil
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchBucket":
			return false, nil
		}
	}

	if bucketNotFoundPattern.MatchString(err.Error()) {
		return false, nil
	}

	return false, fmt.Errorf("unable to determine whether bucket exists: %w", err)
}

func createBucket(ctx context.Context, client *s3.Client, bucket, region string) error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	}
	if region != defaultRegion {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}

	if _, err := client.CreateBucket(ctx, input); err != nil {
		return fmt.Errorf("failed to create bucket %q: %w", bucket, err)
	}

	return nil
}

func enableVersioning(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: types.BucketVersioningStatusEnabled,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to enable versioning on bucket %q: %w", bucket, err)
	}
	return nil
}

func applyBucketPolicy(ctx context.Context, client *s3.Client, bucket, policy string) error {
	_, err := client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket),
		Policy: aws.String(policy),
	})
	if err != nil {
		return fmt.Errorf("failed to apply bucket policy to %q: %w", bucket, err)
	}
	return nil
}

func resolveBucketPolicy(target provisionTarget) (string, string, error) {
	if target.BucketPolicyFile != "" {
		data, err := os.ReadFile(target.BucketPolicyFile)
		if err != nil {
			return "", "", err
		}
		if !json.Valid(data) {
			return "", "", fmt.Errorf("bucket policy file is not valid JSON: %s", target.BucketPolicyFile)
		}
		return string(data), target.BucketPolicyFile, nil
	}

	if target.BucketPolicyTemplate != "" {
		document, err := buildBucketPolicy(target.Bucket, target.BucketPolicyTemplate)
		if err != nil {
			return "", "", err
		}
		return document, target.BucketPolicyTemplate, nil
	}

	return "", "", nil
}

func buildBucketPolicy(bucket, template string) (string, error) {
	bucketARN := fmt.Sprintf("arn:aws:s3:::%s", bucket)
	objectARN := bucketARN + "/*"

	var document map[string]any
	switch template {
	case "public-read":
		document = map[string]any{
			"Version": "2012-10-17",
			"Statement": []map[string]any{
				{
					"Sid":       "PublicReadObjects",
					"Effect":    "Allow",
					"Principal": "*",
					"Action":    []string{"s3:GetObject"},
					"Resource":  []string{objectARN},
				},
			},
		}
	case "deny-insecure-transport":
		document = map[string]any{
			"Version": "2012-10-17",
			"Statement": []map[string]any{
				{
					"Sid":       "DenyInsecureTransport",
					"Effect":    "Deny",
					"Principal": "*",
					"Action":    "s3:*",
					"Resource":  []string{bucketARN, objectARN},
					"Condition": map[string]any{
						"Bool": map[string]string{
							"aws:SecureTransport": "false",
						},
					},
				},
			},
		}
	default:
		return "", fmt.Errorf("unsupported bucket policy template %q", template)
	}

	bytes, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func renderText(result provisionResult) string {
	lines := []string{
		"S3 Provisioning Result",
		"====================",
		fmt.Sprintf("Resources: %d", result.ResourceCount),
	}

	if result.ConfigFile != "" {
		lines = append(lines, fmt.Sprintf("Config file: %s", result.ConfigFile))
	}
	if result.DryRun {
		lines = append(lines, "Mode: dry-run")
	}

	for _, resource := range result.Resources {
		bucketCreateLabel := "Bucket created"
		versioningLabel := "Versioning enabled"
		bucketPolicyLabel := "Bucket policy applied"
		scopedCredentialLabel := "Scoped credentials created"
		if result.DryRun {
			bucketCreateLabel = "Bucket create planned"
			versioningLabel = "Versioning requested"
			bucketPolicyLabel = "Bucket policy planned"
			scopedCredentialLabel = "Scoped credentials planned"
		}

		lines = append(lines,
			"",
			fmt.Sprintf("Bucket: %s", resource.BucketName),
			fmt.Sprintf("Endpoint: %s", emptyFallback(resource.Endpoint, "(default AWS SDK resolution)")),
			fmt.Sprintf("Region: %s", resource.Region),
			fmt.Sprintf("%s: %s", bucketCreateLabel, yesNo(resource.Created)),
			fmt.Sprintf("%s: %s", versioningLabel, yesNo(resource.VersioningEnabled)),
			fmt.Sprintf("%s: %s", bucketPolicyLabel, yesNo(resource.BucketPolicyApplied)),
		)

		if resource.BucketPolicySource != "" {
			lines = append(lines, fmt.Sprintf("Bucket policy source: %s", resource.BucketPolicySource))
		}

		if resource.ScopedCredentials != nil {
			lines = append(lines,
				fmt.Sprintf("%s: %s", scopedCredentialLabel, yesNo(true)),
				fmt.Sprintf("IAM user: %s", resource.ScopedCredentials.UserName),
				fmt.Sprintf("Credential policy template: %s", resource.ScopedCredentials.PolicyTemplate),
				fmt.Sprintf("Access key ID: %s", resource.ScopedCredentials.AccessKeyID),
				fmt.Sprintf("Secret access key: %s", resource.ScopedCredentials.SecretAccessKey),
			)
		}
	}

	return strings.Join(lines, "\n")
}

func envMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, item := range values {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func envValue(env map[string]string, names ...string) string {
	if value := envFirst(env, names...); value != nil {
		return *value
	}
	return ""
}

func envFirst(env map[string]string, names ...string) *string {
	for _, name := range names {
		if value, ok := env[name]; ok && strings.TrimSpace(value) != "" {
			valueCopy := value
			return &valueCopy
		}
	}
	return nil
}

func envBoolAliases(env map[string]string, names ...string) (*bool, error) {
	for _, name := range names {
		value, ok := env[name]
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}

		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("invalid boolean value for %s: %q", name, value)
		}
		return &parsed, nil
	}
	return nil, nil
}

func strPtrIfSet(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	valueCopy := value
	return &valueCopy
}

func envStringPtr(env map[string]string, names ...string) *string {
	for _, name := range names {
		if value, ok := env[name]; ok {
			valueCopy := value
			return &valueCopy
		}
	}
	return nil
}

func stringPtrIfField(data []byte, field, value string) *string {
	if !jsonFieldPresent(data, field) {
		return nil
	}
	valueCopy := value
	return &valueCopy
}

func stringSlicePtrIfValue(values []string) *[]string {
	values = dedupeStringsPreserveOrder(values)
	if len(values) == 0 {
		return nil
	}
	valueCopy := append([]string{}, values...)
	return &valueCopy
}

func stringSlicePtrIfSet(data []byte, fields, values []string) *[]string {
	for _, field := range fields {
		if jsonFieldPresent(data, field) {
			valueCopy := append([]string{}, dedupeStringsPreserveOrder(values)...)
			return &valueCopy
		}
	}
	return nil
}

func boolPtrIfSet(data []byte, field string, value bool) *bool {
	if !jsonFieldPresent(data, field) {
		return nil
	}
	valueCopy := value
	return &valueCopy
}

func jsonFieldPresent(data []byte, field string) bool {
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		return false
	}
	_, ok := decoded[field]
	return ok
}

func changedString(fs *pflag.FlagSet, name, value string) *string {
	if fs.Changed(name) {
		valueCopy := value
		return &valueCopy
	}
	return nil
}

func changedStringSlice(fs *pflag.FlagSet, name string, values []string) *[]string {
	if fs.Changed(name) {
		valueCopy := append([]string{}, values...)
		return &valueCopy
	}
	return nil
}

func changedBool(fs *pflag.FlagSet, name string, value bool) *bool {
	if fs.Changed(name) {
		valueCopy := value
		return &valueCopy
	}
	return nil
}

func extractConfigPath(args []string) string {
	for index := 0; index < len(args); index++ {
		if args[index] == "--config" && index+1 < len(args) {
			return args[index+1]
		}
		if args[index] == "-c" && index+1 < len(args) {
			return args[index+1]
		}
		if strings.HasPrefix(args[index], "--config=") {
			return strings.TrimPrefix(args[index], "--config=")
		}
		if strings.HasPrefix(args[index], "-c=") {
			return strings.TrimPrefix(args[index], "-c=")
		}
	}
	return ""
}

func resolveConfigPath(args []string, env map[string]string) (string, error) {
	if configPath := extractConfigPath(args); configPath != "" {
		return configPath, nil
	}

	if configPath := envValue(env, envAliases.ConfigFile...); configPath != "" {
		return configPath, nil
	}

	configPath := defaultConfigPath(env)
	if configPath == "" {
		return "", nil
	}

	info, err := os.Stat(configPath)
	if err == nil {
		if info.IsDir() {
			return "", fmt.Errorf("default config path %s is a directory", configPath)
		}
		return configPath, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}

	return "", err
}

func defaultConfigPath(env map[string]string) string {
	baseDir := strings.TrimSpace(env["XDG_CONFIG_HOME"])
	if baseDir == "" {
		homeDir := strings.TrimSpace(env["HOME"])
		if homeDir == "" {
			resolvedHomeDir, err := os.UserHomeDir()
			if err != nil {
				return ""
			}
			homeDir = resolvedHomeDir
		}
		baseDir = filepath.Join(homeDir, ".config")
	}

	return filepath.Join(baseDir, binaryName, defaultConfigFileName)
}

func shouldShowIntroHelp(args []string, env map[string]string) bool {
	if len(args) != 0 {
		return false
	}
	if envValue(env, envAliases.ConfigFile...) != "" {
		return false
	}
	if envValue(env, envAliases.BatchFile...) != "" {
		return false
	}
	if len(parseCommaSeparatedValues(envValue(env, envAliases.BucketNames...))) > 0 {
		return false
	}
	if envValue(env, envAliases.BucketName...) != "" {
		return false
	}
	return true
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parseCommaSeparatedValues(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func dedupeStringsPreserveOrder(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizeCSVHeader(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	replacer := strings.NewReplacer(" ", "_", "-", "_")
	return replacer.Replace(value)
}

func hasCSVHeader(index map[string]int, aliases ...string) bool {
	for _, alias := range aliases {
		if _, ok := index[alias]; ok {
			return true
		}
	}
	return false
}

func csvField(record []string, index map[string]int, aliases ...string) string {
	for _, alias := range aliases {
		column, ok := index[alias]
		if !ok || column >= len(record) {
			continue
		}
		value := strings.TrimSpace(record[column])
		if value != "" {
			return value
		}
	}
	return ""
}

func csvRecordBlank(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func csvRecordComment(record []string) bool {
	for _, value := range record {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		return strings.HasPrefix(trimmed, "#")
	}
	return false
}

func resolveRelativePath(baseDir, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(baseDir, value)
}

func resolveRelativePathIfSet(baseDir, value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	return resolveRelativePath(baseDir, value)
}
