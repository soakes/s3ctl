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
	providerS3                      = "s3"
	providerOVH                     = "ovh"
	operationProvision              = "provision"
	operationDelete                 = "delete"
	defaultProvider                 = providerS3
	defaultRegion                   = "us-east-1"
	defaultIAMUserPrefix            = ""
	defaultIAMPath                  = ""
	defaultCredentialPolicyTemplate = "bucket-readwrite"
	defaultOVHUserRole              = "objectstore_operator"
	defaultOVHStoragePolicyRole     = "readWrite"
	defaultConfigFileName           = "config.json"
	defaultOutputFormat             = "text"
	defaultProvisionTimeout         = 10 * time.Minute
	s3DeleteObjectsBatchSize        = 1000
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
	Provider                []string
	IAMEndpointURL          []string
	IAMUserName             []string
	IAMUserPrefix           []string
	IAMPath                 []string
	CredentialPolicyTmpl    []string
	OVHAPIEndpoint          []string
	OVHAccessToken          []string
	OVHApplicationKey       []string
	OVHApplicationSecret    []string
	OVHConsumerKey          []string
	OVHClientID             []string
	OVHClientSecret         []string
	OVHS3Endpoint           []string
	OVHServiceName          []string
	OVHUserRole             []string
	OVHStoragePolicyRole    []string
	OVHEncryptData          []string
	OVHRotateCredentials    []string
	DeleteBucket            []string
	Force                   []string
	Timeout                 []string
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
	Provider:                []string{"S3CTL_PROVIDER"},
	IAMEndpointURL:          []string{"S3CTL_IAM_ENDPOINT_URL"},
	IAMUserName:             []string{"S3CTL_IAM_USER_NAME"},
	IAMUserPrefix:           []string{"S3CTL_IAM_USER_PREFIX"},
	IAMPath:                 []string{"S3CTL_IAM_PATH"},
	CredentialPolicyTmpl:    []string{"S3CTL_CREDENTIAL_POLICY_TEMPLATE"},
	OVHAPIEndpoint:          []string{"S3CTL_OVH_API_ENDPOINT", "OVH_ENDPOINT"},
	OVHAccessToken:          []string{"S3CTL_OVH_ACCESS_TOKEN", "OVH_ACCESS_TOKEN"},
	OVHApplicationKey:       []string{"S3CTL_OVH_APPLICATION_KEY", "OVH_APPLICATION_KEY"},
	OVHApplicationSecret:    []string{"S3CTL_OVH_APPLICATION_SECRET", "OVH_APPLICATION_SECRET"},
	OVHConsumerKey:          []string{"S3CTL_OVH_CONSUMER_KEY", "OVH_CONSUMER_KEY"},
	OVHClientID:             []string{"S3CTL_OVH_CLIENT_ID", "OVH_CLIENT_ID"},
	OVHClientSecret:         []string{"S3CTL_OVH_CLIENT_SECRET", "OVH_CLIENT_SECRET"},
	OVHS3Endpoint:           []string{"S3CTL_OVH_S3_ENDPOINT"},
	OVHServiceName:          []string{"S3CTL_OVH_SERVICE_NAME", "S3CTL_OVH_PROJECT_ID", "OVH_CLOUD_PROJECT_ID"},
	OVHUserRole:             []string{"S3CTL_OVH_USER_ROLE"},
	OVHStoragePolicyRole:    []string{"S3CTL_OVH_STORAGE_POLICY_ROLE"},
	OVHEncryptData:          []string{"S3CTL_OVH_ENCRYPT_DATA"},
	OVHRotateCredentials:    []string{"S3CTL_OVH_ROTATE_CREDENTIALS"},
	DeleteBucket:            []string{"S3CTL_DELETE_BUCKET", "S3CTL_DELETE"},
	Force:                   []string{"S3CTL_FORCE"},
	Timeout:                 []string{"S3CTL_TIMEOUT", "S3CTL_PROVISION_TIMEOUT"},
	OutputFormat:            []string{"S3CTL_OUTPUT_FORMAT", "S3CTL_OUTPUT"},
	DryRun:                  []string{"S3CTL_DRY_RUN"},
}

type settings struct {
	ConfigPath               string   `json:"-"`
	Provider                 string   `json:"provider"`
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
	OVHAPIEndpoint           string   `json:"ovh_api_endpoint"`
	OVHAccessToken           string   `json:"ovh_access_token"`
	OVHApplicationKey        string   `json:"ovh_application_key"`
	OVHApplicationSecret     string   `json:"ovh_application_secret"`
	OVHConsumerKey           string   `json:"ovh_consumer_key"`
	OVHClientID              string   `json:"ovh_client_id"`
	OVHClientSecret          string   `json:"ovh_client_secret"`
	OVHS3Endpoint            string   `json:"ovh_s3_endpoint"`
	OVHServiceName           string   `json:"ovh_service_name"`
	OVHUserRole              string   `json:"ovh_user_role"`
	OVHStoragePolicyRole     string   `json:"ovh_storage_policy_role"`
	OVHEncryptData           bool     `json:"ovh_encrypt_data"`
	OVHEncryptDataSet        bool     `json:"-"`
	OVHRotateCredentials     bool     `json:"ovh_rotate_credentials"`
	DeleteBucket             bool     `json:"delete_bucket"`
	Force                    bool     `json:"force"`
	Timeout                  string   `json:"timeout"`
	Output                   string   `json:"output"`
	DryRun                   bool     `json:"dry_run"`
	ParsedTimeout            time.Duration
}

type source struct {
	Provider                 *string
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
	OVHAPIEndpoint           *string
	OVHAccessToken           *string
	OVHApplicationKey        *string
	OVHApplicationSecret     *string
	OVHConsumerKey           *string
	OVHClientID              *string
	OVHClientSecret          *string
	OVHS3Endpoint            *string
	OVHServiceName           *string
	OVHUserRole              *string
	OVHStoragePolicyRole     *string
	OVHEncryptData           *bool
	OVHRotateCredentials     *bool
	DeleteBucket             *bool
	Force                    *bool
	Timeout                  *time.Duration
	Output                   *string
	DryRun                   *bool
}

type cliFlags struct {
	Config                   string
	Provider                 string
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
	OVHAPIEndpoint           string
	OVHAccessToken           string
	OVHApplicationKey        string
	OVHApplicationSecret     string
	OVHConsumerKey           string
	OVHClientID              string
	OVHClientSecret          string
	OVHS3Endpoint            string
	OVHServiceName           string
	OVHUserRole              string
	OVHStoragePolicyRole     string
	OVHEncryptData           bool
	OVHRotateCredentials     bool
	DeleteBucket             bool
	Force                    bool
	Timeout                  string
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
	Operation     string           `json:"operation"`
	DryRun        bool             `json:"dry_run"`
	ConfigFile    string           `json:"config_file,omitempty"`
	ResourceCount int              `json:"resource_count"`
	Resources     []resourceResult `json:"resources"`
}

type commandErrorResult struct {
	Operation     string             `json:"operation"`
	DryRun        bool               `json:"dry_run"`
	ConfigFile    string             `json:"config_file,omitempty"`
	ResourceCount int                `json:"resource_count"`
	Error         commandErrorDetail `json:"error"`
}

type commandErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

type resourceResult struct {
	BucketName          string                  `json:"bucket_name"`
	Endpoint            string                  `json:"endpoint,omitempty"`
	Region              string                  `json:"region"`
	Created             bool                    `json:"created"`
	Deleted             bool                    `json:"deleted,omitempty"`
	ObjectsDeleted      int                     `json:"objects_deleted,omitempty"`
	VersioningEnabled   bool                    `json:"versioning_enabled"`
	EncryptionEnabled   bool                    `json:"encryption_enabled"`
	BucketPolicyApplied bool                    `json:"bucket_policy_applied"`
	BucketPolicySource  string                  `json:"bucket_policy_source,omitempty"`
	CredentialsRotated  bool                    `json:"credentials_rotated,omitempty"`
	CredentialsDeleted  int                     `json:"credentials_deleted,omitempty"`
	ScopedCredentials   *scopedCredentialResult `json:"scoped_credentials,omitempty"`
	Warnings            []string                `json:"warnings,omitempty"`
}

type bucketExistsError struct {
	Name string
}

func (e bucketExistsError) Error() string {
	return fmt.Sprintf("bucket %q already exists", e.Name)
}

type bucketNotFoundError struct {
	Name     string
	Provider string
	Region   string
	Cause    error
}

func (e bucketNotFoundError) Error() string {
	if e.Provider == providerOVH {
		if strings.TrimSpace(e.Region) != "" {
			return fmt.Sprintf("OVH bucket/container %q does not exist in region %q; nothing was deleted", e.Name, e.Region)
		}
		return fmt.Sprintf("OVH bucket/container %q does not exist; nothing was deleted", e.Name)
	}
	return fmt.Sprintf("bucket %q does not exist; nothing was deleted", e.Name)
}

func (e bucketNotFoundError) Unwrap() error {
	return e.Cause
}

type s3API interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	PutBucketVersioning(context.Context, *s3.PutBucketVersioningInput, ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error)
	PutBucketPolicy(context.Context, *s3.PutBucketPolicyInput, ...func(*s3.Options)) (*s3.PutBucketPolicyOutput, error)
	ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	DeleteObjects(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	DeleteBucket(context.Context, *s3.DeleteBucketInput, ...func(*s3.Options)) (*s3.DeleteBucketOutput, error)
}

var newS3APIClient = func(ctx context.Context, cfg settings) (s3API, error) {
	return newS3Client(ctx, cfg)
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

	requestedOutput := detectOutputFormat(args, env)
	cfg, parsed, err := resolveSettings(args, env)
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) || parsed.showHelp {
			if writeErr := writeUsage(stdout); writeErr != nil {
				return 1
			}
			return 0
		}
		if requestedOutput == "json" {
			if writeErr := writeJSONError(stdout, cfg, err, "configuration_error"); writeErr != nil {
				return 1
			}
			return 1
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

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ParsedTimeout)
	defer cancel()

	result, err := provision(ctx, cfg)
	if err != nil {
		if cfg.Output == "json" {
			if writeErr := writeJSONError(stdout, cfg, err, "operation_failed"); writeErr != nil {
				return 1
			}
			return 1
		}
		if _, writeErr := fmt.Fprintf(stderr, "Error: %s\n", renderErrorMessage(err)); writeErr != nil {
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

func writeJSONError(w io.Writer, cfg settings, err error, fallbackCode string) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(commandErrorResult{
		Operation:     operationFromSettings(cfg),
		DryRun:        cfg.DryRun,
		ConfigFile:    cfg.ConfigPath,
		ResourceCount: len(cfg.Buckets),
		Error: commandErrorDetail{
			Code:    errorCode(err, fallbackCode),
			Message: renderErrorMessage(err),
			Detail:  renderErrorDetail(err),
		},
	})
}

func renderErrorMessage(err error) string {
	var notFound bucketNotFoundError
	if errors.As(err, &notFound) {
		return notFound.Error()
	}
	return err.Error()
}

func renderErrorDetail(err error) string {
	var notFound bucketNotFoundError
	if errors.As(err, &notFound) && notFound.Cause != nil {
		return notFound.Cause.Error()
	}
	return ""
}

func errorCode(err error, fallback string) string {
	var notFound bucketNotFoundError
	if errors.As(err, &notFound) {
		return "not_found"
	}
	var exists bucketExistsError
	if errors.As(err, &exists) {
		return "already_exists"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if fallback != "" {
		return fallback
	}
	return "error"
}

func operationFromSettings(cfg settings) string {
	switch {
	case cfg.DeleteBucket:
		return operationDelete
	default:
		return operationProvision
	}
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
		return cfg, cliParsed, err
	}

	return cfg, cliParsed, nil
}

func parseFlags(args []string) (parseResult, error) {
	flags := cliFlags{}
	fs := newFlagSet(&flags)

	if err := fs.Parse(args); err != nil {
		return parseResult{}, err
	}
	timeout, err := changedDuration(fs, "timeout", flags.Timeout)
	if err != nil {
		return parseResult{}, err
	}

	return parseResult{
		source: source{
			Provider:                 changedString(fs, "provider", flags.Provider),
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
			OVHAPIEndpoint:           changedString(fs, "ovh-api-endpoint", flags.OVHAPIEndpoint),
			OVHAccessToken:           changedString(fs, "ovh-access-token", flags.OVHAccessToken),
			OVHApplicationKey:        changedString(fs, "ovh-application-key", flags.OVHApplicationKey),
			OVHApplicationSecret:     changedString(fs, "ovh-application-secret", flags.OVHApplicationSecret),
			OVHConsumerKey:           changedString(fs, "ovh-consumer-key", flags.OVHConsumerKey),
			OVHClientID:              changedString(fs, "ovh-client-id", flags.OVHClientID),
			OVHClientSecret:          changedString(fs, "ovh-client-secret", flags.OVHClientSecret),
			OVHS3Endpoint:            changedString(fs, "ovh-s3-endpoint", flags.OVHS3Endpoint),
			OVHServiceName:           changedString(fs, "ovh-service-name", flags.OVHServiceName),
			OVHUserRole:              changedString(fs, "ovh-user-role", flags.OVHUserRole),
			OVHStoragePolicyRole:     changedString(fs, "ovh-storage-policy-role", flags.OVHStoragePolicyRole),
			OVHEncryptData:           changedBool(fs, "ovh-encrypt-data", flags.OVHEncryptData),
			OVHRotateCredentials:     changedBool(fs, "ovh-rotate-credentials", flags.OVHRotateCredentials),
			DeleteBucket:             changedBool(fs, "delete", flags.DeleteBucket),
			Force:                    changedBool(fs, "force", flags.Force),
			Timeout:                  timeout,
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
	fs.StringVar(&flags.Provider, "provider", defaultProvider, "Provisioning provider: s3 or ovh")
	fs.StringArrayVarP(&flags.Buckets, "bucket", "b", nil, "Bucket name to create or delete; may be specified more than once")
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
	fs.StringVar(&flags.OVHAPIEndpoint, "ovh-api-endpoint", "", "OVHcloud API endpoint name or URL for the OVH provider")
	fs.StringVar(&flags.OVHAccessToken, "ovh-access-token", "", "OVHcloud access token for the OVH provider")
	fs.StringVar(&flags.OVHApplicationKey, "ovh-application-key", "", "OVHcloud application key for the OVH provider")
	fs.StringVar(&flags.OVHApplicationSecret, "ovh-application-secret", "", "OVHcloud application secret for the OVH provider")
	fs.StringVar(&flags.OVHConsumerKey, "ovh-consumer-key", "", "OVHcloud consumer key for the OVH provider")
	fs.StringVar(&flags.OVHClientID, "ovh-client-id", "", "OVHcloud OAuth2 client ID for the OVH provider")
	fs.StringVar(&flags.OVHClientSecret, "ovh-client-secret", "", "OVHcloud OAuth2 client secret for the OVH provider")
	fs.StringVar(&flags.OVHS3Endpoint, "ovh-s3-endpoint", "", "Override the returned OVHcloud S3 endpoint URL")
	fs.StringVar(&flags.OVHServiceName, "ovh-service-name", "", "OVHcloud Public Cloud project service name for the OVH provider")
	fs.StringVar(&flags.OVHUserRole, "ovh-user-role", defaultOVHUserRole, "OVHcloud Public Cloud user role for created object storage users")
	fs.StringVar(&flags.OVHStoragePolicyRole, "ovh-storage-policy-role", defaultOVHStoragePolicyRole, "OVHcloud container policy role: admin, deny, readOnly, or readWrite")
	fs.BoolVar(&flags.OVHEncryptData, "ovh-encrypt-data", false, "Enable OVHcloud server-side encryption with OVH-managed keys")
	fs.BoolVar(&flags.OVHRotateCredentials, "ovh-rotate-credentials", false, "Rotate existing OVHcloud S3 credentials for each bucket instead of creating containers")
	fs.BoolVar(&flags.DeleteBucket, "delete", false, "Delete each bucket instead of creating buckets")
	fs.BoolVar(&flags.Force, "force", false, "Allow delete operations to remove bucket contents before deleting buckets")
	fs.StringVar(&flags.Timeout, "timeout", defaultProvisionTimeout.String(), "Overall operation timeout, for example 30s, 5m, or 1h")
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
  %s --provider ovh --bucket app-data --region GRA --ovh-service-name PROJECT_ID
  %s --provider ovh --bucket app-data --ovh-rotate-credentials --output json
  %s --provider ovh --bucket app-data --delete
  %s --provider ovh --bucket app-data --delete --force
  %s --bucket app-data --create-scoped-credentials --credential-policy-template bucket-readwrite
  %s --bucket app-data --bucket logs --create-scoped-credentials --dry-run --output json
  %s --batch-file ./examples/s3ctl-batch.csv --create-scoped-credentials
  %s --bucket app-data --dry-run
  %s --config ./examples/s3ctl.json --dry-run --output json

Flags:
`, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName)
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
  S3CTL_PROVIDER
  S3CTL_IAM_ENDPOINT_URL
  S3CTL_IAM_USER_NAME
  S3CTL_IAM_USER_PREFIX
  S3CTL_IAM_PATH
  S3CTL_CREDENTIAL_POLICY_TEMPLATE
  S3CTL_OVH_API_ENDPOINT
  S3CTL_OVH_ACCESS_TOKEN
  S3CTL_OVH_APPLICATION_KEY
  S3CTL_OVH_APPLICATION_SECRET
  S3CTL_OVH_CONSUMER_KEY
  S3CTL_OVH_CLIENT_ID
  S3CTL_OVH_CLIENT_SECRET
  S3CTL_OVH_S3_ENDPOINT
  S3CTL_OVH_SERVICE_NAME
  S3CTL_OVH_PROJECT_ID
  S3CTL_OVH_USER_ROLE
  S3CTL_OVH_STORAGE_POLICY_ROLE
  S3CTL_OVH_ENCRYPT_DATA
  S3CTL_OVH_ROTATE_CREDENTIALS
  S3CTL_DELETE_BUCKET
  S3CTL_FORCE
  S3CTL_TIMEOUT
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
  The default provider is s3, which provisions through the S3 API.
  Scoped credential provisioning for the s3 provider uses the IAM API. By default this targets AWS IAM.
  Use --iam-endpoint when you need a different IAM-compatible endpoint.
  Use --provider ovh to create OVHcloud Public Cloud users, S3 credentials, and containers through the OVHcloud API.
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
	deleteBucket := boolPtrIfSet(data, "delete_bucket", cfg.DeleteBucket)
	if deleteBucket == nil {
		var err error
		deleteBucket, err = boolPtrFromJSONField(data, "delete")
		if err != nil {
			return source{}, err
		}
	}
	timeout, err := durationPtrFromJSONFields(data, "timeout", "provision_timeout")
	if err != nil {
		return source{}, err
	}

	return source{
		Provider:                 stringPtrIfField(data, "provider", cfg.Provider),
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
		OVHAPIEndpoint:           stringPtrIfField(data, "ovh_api_endpoint", cfg.OVHAPIEndpoint),
		OVHAccessToken:           stringPtrIfField(data, "ovh_access_token", cfg.OVHAccessToken),
		OVHApplicationKey:        stringPtrIfField(data, "ovh_application_key", cfg.OVHApplicationKey),
		OVHApplicationSecret:     stringPtrIfField(data, "ovh_application_secret", cfg.OVHApplicationSecret),
		OVHConsumerKey:           stringPtrIfField(data, "ovh_consumer_key", cfg.OVHConsumerKey),
		OVHClientID:              stringPtrIfField(data, "ovh_client_id", cfg.OVHClientID),
		OVHClientSecret:          stringPtrIfField(data, "ovh_client_secret", cfg.OVHClientSecret),
		OVHS3Endpoint:            stringPtrIfField(data, "ovh_s3_endpoint", cfg.OVHS3Endpoint),
		OVHServiceName:           stringPtrIfField(data, "ovh_service_name", cfg.OVHServiceName),
		OVHUserRole:              stringPtrIfField(data, "ovh_user_role", cfg.OVHUserRole),
		OVHStoragePolicyRole:     stringPtrIfField(data, "ovh_storage_policy_role", cfg.OVHStoragePolicyRole),
		OVHEncryptData:           boolPtrIfSet(data, "ovh_encrypt_data", cfg.OVHEncryptData),
		OVHRotateCredentials:     boolPtrIfSet(data, "ovh_rotate_credentials", cfg.OVHRotateCredentials),
		DeleteBucket:             deleteBucket,
		Force:                    boolPtrIfSet(data, "force", cfg.Force),
		Timeout:                  timeout,
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

	ovhEncryptData, err := envBoolAliases(env, envAliases.OVHEncryptData...)
	if err != nil {
		return source{}, err
	}

	ovhRotateCredentials, err := envBoolAliases(env, envAliases.OVHRotateCredentials...)
	if err != nil {
		return source{}, err
	}

	deleteBucket, err := envBoolAliases(env, envAliases.DeleteBucket...)
	if err != nil {
		return source{}, err
	}

	force, err := envBoolAliases(env, envAliases.Force...)
	if err != nil {
		return source{}, err
	}

	timeout, err := envDurationAliases(env, envAliases.Timeout...)
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
		Provider:                 strPtrIfSet(envValue(env, envAliases.Provider...)),
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
		OVHAPIEndpoint:           strPtrIfSet(envValue(env, envAliases.OVHAPIEndpoint...)),
		OVHAccessToken:           strPtrIfSet(envValue(env, envAliases.OVHAccessToken...)),
		OVHApplicationKey:        strPtrIfSet(envValue(env, envAliases.OVHApplicationKey...)),
		OVHApplicationSecret:     strPtrIfSet(envValue(env, envAliases.OVHApplicationSecret...)),
		OVHConsumerKey:           strPtrIfSet(envValue(env, envAliases.OVHConsumerKey...)),
		OVHClientID:              strPtrIfSet(envValue(env, envAliases.OVHClientID...)),
		OVHClientSecret:          strPtrIfSet(envValue(env, envAliases.OVHClientSecret...)),
		OVHS3Endpoint:            strPtrIfSet(envValue(env, envAliases.OVHS3Endpoint...)),
		OVHServiceName:           strPtrIfSet(envValue(env, envAliases.OVHServiceName...)),
		OVHUserRole:              strPtrIfSet(envValue(env, envAliases.OVHUserRole...)),
		OVHStoragePolicyRole:     strPtrIfSet(envValue(env, envAliases.OVHStoragePolicyRole...)),
		OVHEncryptData:           ovhEncryptData,
		OVHRotateCredentials:     ovhRotateCredentials,
		DeleteBucket:             deleteBucket,
		Force:                    force,
		Timeout:                  timeout,
		Output:                   strPtrIfSet(envValue(env, envAliases.OutputFormat...)),
		DryRun:                   dryRun,
	}, nil
}

func mergeSources(sources ...source) settings {
	cfg := settings{
		Provider:                 defaultProvider,
		Region:                   defaultRegion,
		IAMUserPrefix:            defaultIAMUserPrefix,
		IAMPath:                  defaultIAMPath,
		CredentialPolicyTemplate: defaultCredentialPolicyTemplate,
		OVHUserRole:              defaultOVHUserRole,
		OVHStoragePolicyRole:     defaultOVHStoragePolicyRole,
		Output:                   defaultOutputFormat,
		ParsedTimeout:            defaultProvisionTimeout,
	}

	for _, src := range sources {
		if src.Provider != nil {
			cfg.Provider = *src.Provider
		}
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
		if src.OVHAPIEndpoint != nil {
			cfg.OVHAPIEndpoint = *src.OVHAPIEndpoint
		}
		if src.OVHAccessToken != nil {
			cfg.OVHAccessToken = *src.OVHAccessToken
		}
		if src.OVHApplicationKey != nil {
			cfg.OVHApplicationKey = *src.OVHApplicationKey
		}
		if src.OVHApplicationSecret != nil {
			cfg.OVHApplicationSecret = *src.OVHApplicationSecret
		}
		if src.OVHConsumerKey != nil {
			cfg.OVHConsumerKey = *src.OVHConsumerKey
		}
		if src.OVHClientID != nil {
			cfg.OVHClientID = *src.OVHClientID
		}
		if src.OVHClientSecret != nil {
			cfg.OVHClientSecret = *src.OVHClientSecret
		}
		if src.OVHS3Endpoint != nil {
			cfg.OVHS3Endpoint = *src.OVHS3Endpoint
		}
		if src.OVHServiceName != nil {
			cfg.OVHServiceName = *src.OVHServiceName
		}
		if src.OVHUserRole != nil {
			cfg.OVHUserRole = *src.OVHUserRole
		}
		if src.OVHStoragePolicyRole != nil {
			cfg.OVHStoragePolicyRole = *src.OVHStoragePolicyRole
		}
		if src.OVHEncryptData != nil {
			cfg.OVHEncryptData = *src.OVHEncryptData
			cfg.OVHEncryptDataSet = true
		}
		if src.OVHRotateCredentials != nil {
			cfg.OVHRotateCredentials = *src.OVHRotateCredentials
		}
		if src.DeleteBucket != nil {
			cfg.DeleteBucket = *src.DeleteBucket
		}
		if src.Force != nil {
			cfg.Force = *src.Force
		}
		if src.Timeout != nil {
			cfg.ParsedTimeout = *src.Timeout
			cfg.Timeout = src.Timeout.String()
		}
		if src.Output != nil {
			cfg.Output = *src.Output
		}
		if src.DryRun != nil {
			cfg.DryRun = *src.DryRun
		}
	}

	cfg.Buckets = dedupeStringsPreserveOrder(cfg.Buckets)
	cfg.Provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	cfg.OVHStoragePolicyRole = normalizeOVHStoragePolicyRole(cfg.OVHStoragePolicyRole)
	return cfg
}

func validateSettings(cfg settings) error {
	if len(cfg.Buckets) == 0 && cfg.BatchFile == "" {
		return errors.New("at least one --bucket or a --batch-file is required unless provided via environment or config")
	}
	provider := cfg.Provider
	if provider == "" {
		provider = defaultProvider
	}
	switch provider {
	case providerS3, providerOVH:
	default:
		return fmt.Errorf("--provider must be either s3 or ovh, got %q", cfg.Provider)
	}
	if cfg.OVHRotateCredentials && provider != providerOVH {
		return errors.New("--ovh-rotate-credentials requires --provider ovh")
	}
	if cfg.DeleteBucket && cfg.OVHRotateCredentials {
		return errors.New("use either --delete or --ovh-rotate-credentials, not both")
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
	output := cfg.Output
	if output == "" {
		output = defaultOutputFormat
	}
	if output != "text" && output != "json" {
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
	if provider == providerOVH {
		if err := validateOVHSettings(cfg); err != nil {
			return err
		}
	}
	return nil
}

func provision(ctx context.Context, cfg settings) (provisionResult, error) {
	targets, err := buildProvisionTargets(cfg)
	if err != nil {
		return provisionResult{}, err
	}

	result := provisionResult{
		Operation:     operationProvision,
		DryRun:        cfg.DryRun,
		ConfigFile:    cfg.ConfigPath,
		ResourceCount: len(targets),
		Resources:     make([]resourceResult, 0, len(targets)),
	}
	if cfg.DeleteBucket {
		result.Operation = operationDelete
	}

	if cfg.Provider == providerOVH {
		return provisionWithOVH(ctx, cfg, targets, result)
	}

	var s3Client s3API
	var iamClient iamAPI

	if !cfg.DryRun {
		s3Client, err = newS3APIClient(ctx, cfg)
		if err != nil {
			return provisionResult{}, err
		}
	}

	if cfg.DeleteBucket {
		return deleteS3Buckets(ctx, cfg, targets, result, s3Client)
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

func bucketExists(ctx context.Context, client s3API, bucket string) (bool, error) {
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

func createBucket(ctx context.Context, client s3API, bucket, region string) error {
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

func enableVersioning(ctx context.Context, client s3API, bucket string) error {
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

func applyBucketPolicy(ctx context.Context, client s3API, bucket, policy string) error {
	_, err := client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket),
		Policy: aws.String(policy),
	})
	if err != nil {
		return fmt.Errorf("failed to apply bucket policy to %q: %w", bucket, err)
	}
	return nil
}

func deleteS3Buckets(ctx context.Context, cfg settings, targets []provisionTarget, result provisionResult, client s3API) (provisionResult, error) {
	for _, target := range targets {
		resource := resourceResult{
			BucketName: target.Bucket,
			Endpoint:   cfg.Endpoint,
			Region:     cfg.Region,
			Deleted:    true,
		}

		if cfg.DryRun {
			result.Resources = append(result.Resources, resource)
			continue
		}

		exists, err := bucketExists(ctx, client, target.Bucket)
		if err != nil {
			return provisionResult{}, err
		}
		if !exists {
			return provisionResult{}, bucketNotFoundError{Name: target.Bucket, Provider: providerS3}
		}

		if cfg.Force {
			deleted, err := emptyS3Bucket(ctx, client, target.Bucket)
			if err != nil {
				return provisionResult{}, err
			}
			resource.ObjectsDeleted = deleted
		} else {
			if err := ensureS3BucketEmpty(ctx, client, target.Bucket); err != nil {
				return provisionResult{}, err
			}
		}

		if err := deleteS3Bucket(ctx, client, target.Bucket); err != nil {
			return provisionResult{}, err
		}

		result.Resources = append(result.Resources, resource)
	}

	return result, nil
}

func ensureS3BucketEmpty(ctx context.Context, client s3API, bucket string) error {
	hasObjectVersions, err := s3BucketHasObjectVersions(ctx, client, bucket)
	if err != nil {
		return err
	}
	if hasObjectVersions {
		return fmt.Errorf("refusing to delete non-empty bucket %q without --force; rerun with --delete --force to remove objects, versions, and delete markers before deleting the bucket", bucket)
	}

	hasCurrentObjects, err := s3BucketHasCurrentObjects(ctx, client, bucket)
	if err != nil {
		return err
	}
	if hasCurrentObjects {
		return fmt.Errorf("refusing to delete non-empty bucket %q without --force; rerun with --delete --force to remove objects before deleting the bucket", bucket)
	}

	return nil
}

func emptyS3Bucket(ctx context.Context, client s3API, bucket string) (int, error) {
	versionedDeleted, err := deleteS3ObjectVersions(ctx, client, bucket)
	if err != nil {
		return 0, err
	}

	currentDeleted, err := deleteS3CurrentObjects(ctx, client, bucket)
	if err != nil {
		return 0, err
	}

	return versionedDeleted + currentDeleted, nil
}

func s3BucketHasObjectVersions(ctx context.Context, client s3API, bucket string) (bool, error) {
	input := &s3.ListObjectVersionsInput{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int32(1),
	}

	for {
		output, err := client.ListObjectVersions(ctx, input)
		if err != nil {
			if isUnsupportedObjectVersionListing(err) {
				return false, nil
			}
			return false, fmt.Errorf("failed to list object versions in bucket %q: %w", bucket, err)
		}

		for _, version := range output.Versions {
			if version.Key != nil {
				return true, nil
			}
		}
		for _, marker := range output.DeleteMarkers {
			if marker.Key != nil {
				return true, nil
			}
		}

		if !aws.ToBool(output.IsTruncated) {
			return false, nil
		}
		if output.NextKeyMarker == nil && output.NextVersionIdMarker == nil {
			return false, fmt.Errorf("failed to continue listing object versions in bucket %q: truncated response did not include a next marker", bucket)
		}
		input.KeyMarker = output.NextKeyMarker
		input.VersionIdMarker = output.NextVersionIdMarker
	}
}

func s3BucketHasCurrentObjects(ctx context.Context, client s3API, bucket string) (bool, error) {
	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int32(1),
	}

	for {
		output, err := client.ListObjectsV2(ctx, input)
		if err != nil {
			return false, fmt.Errorf("failed to list current objects in bucket %q: %w", bucket, err)
		}

		for _, object := range output.Contents {
			if object.Key != nil {
				return true, nil
			}
		}

		if !aws.ToBool(output.IsTruncated) {
			return false, nil
		}
		if output.NextContinuationToken == nil {
			return false, fmt.Errorf("failed to continue listing current objects in bucket %q: truncated response did not include a continuation token", bucket)
		}
		input.ContinuationToken = output.NextContinuationToken
	}
}

func deleteS3ObjectVersions(ctx context.Context, client s3API, bucket string) (int, error) {
	input := &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucket),
	}

	deleted := 0
	for {
		output, err := client.ListObjectVersions(ctx, input)
		if err != nil {
			if isUnsupportedObjectVersionListing(err) {
				return deleted, nil
			}
			return deleted, fmt.Errorf("failed to list object versions in bucket %q: %w", bucket, err)
		}

		objects := make([]types.ObjectIdentifier, 0, len(output.Versions)+len(output.DeleteMarkers))
		for _, version := range output.Versions {
			if version.Key == nil {
				continue
			}
			objects = append(objects, types.ObjectIdentifier{
				Key:       version.Key,
				VersionId: version.VersionId,
			})
		}
		for _, marker := range output.DeleteMarkers {
			if marker.Key == nil {
				continue
			}
			objects = append(objects, types.ObjectIdentifier{
				Key:       marker.Key,
				VersionId: marker.VersionId,
			})
		}

		count, err := deleteS3ObjectBatch(ctx, client, bucket, objects)
		if err != nil {
			return deleted, err
		}
		deleted += count

		if !aws.ToBool(output.IsTruncated) {
			return deleted, nil
		}
		if output.NextKeyMarker == nil && output.NextVersionIdMarker == nil {
			return deleted, fmt.Errorf("failed to continue listing object versions in bucket %q: truncated response did not include a next marker", bucket)
		}
		input.KeyMarker = output.NextKeyMarker
		input.VersionIdMarker = output.NextVersionIdMarker
	}
}

func deleteS3CurrentObjects(ctx context.Context, client s3API, bucket string) (int, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	}

	deleted := 0
	for {
		output, err := client.ListObjectsV2(ctx, input)
		if err != nil {
			return deleted, fmt.Errorf("failed to list current objects in bucket %q: %w", bucket, err)
		}

		objects := make([]types.ObjectIdentifier, 0, len(output.Contents))
		for _, object := range output.Contents {
			if object.Key == nil {
				continue
			}
			objects = append(objects, types.ObjectIdentifier{Key: object.Key})
		}

		count, err := deleteS3ObjectBatch(ctx, client, bucket, objects)
		if err != nil {
			return deleted, err
		}
		deleted += count

		if !aws.ToBool(output.IsTruncated) {
			return deleted, nil
		}
		if output.NextContinuationToken == nil {
			return deleted, fmt.Errorf("failed to continue listing current objects in bucket %q: truncated response did not include a continuation token", bucket)
		}
		input.ContinuationToken = output.NextContinuationToken
	}
}

func deleteS3ObjectBatch(ctx context.Context, client s3API, bucket string, objects []types.ObjectIdentifier) (int, error) {
	deleted := 0
	for len(objects) > 0 {
		batchSize := min(len(objects), s3DeleteObjectsBatchSize)
		batch := objects[:batchSize]
		objects = objects[batchSize:]

		output, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &types.Delete{
				Objects: batch,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return deleted, fmt.Errorf("failed to delete objects from bucket %q: %w", bucket, err)
		}
		if len(output.Errors) > 0 {
			return deleted, fmt.Errorf("failed to delete %d object(s) from bucket %q: %s", len(output.Errors), bucket, renderS3DeleteErrors(output.Errors))
		}
		deleted += len(batch)
	}
	return deleted, nil
}

func deleteS3Bucket(ctx context.Context, client s3API, bucket string) error {
	if _, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	}); err != nil {
		return fmt.Errorf("failed to delete bucket %q: %w", bucket, err)
	}
	return nil
}

func renderS3DeleteErrors(deleteErrors []types.Error) string {
	parts := make([]string, 0, min(len(deleteErrors), 3))
	for _, deleteErr := range deleteErrors {
		key := aws.ToString(deleteErr.Key)
		code := aws.ToString(deleteErr.Code)
		message := aws.ToString(deleteErr.Message)
		switch {
		case key != "" && code != "" && message != "":
			parts = append(parts, fmt.Sprintf("%s (%s: %s)", key, code, message))
		case key != "" && code != "":
			parts = append(parts, fmt.Sprintf("%s (%s)", key, code))
		case key != "":
			parts = append(parts, key)
		case code != "":
			parts = append(parts, code)
		default:
			parts = append(parts, "unknown delete error")
		}
		if len(parts) == 3 {
			break
		}
	}
	if len(deleteErrors) > len(parts) {
		parts = append(parts, fmt.Sprintf("and %d more", len(deleteErrors)-len(parts)))
	}
	return strings.Join(parts, "; ")
}

func isUnsupportedObjectVersionListing(err error) bool {
	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) {
		switch responseErr.HTTPStatusCode() {
		case http.StatusMethodNotAllowed, http.StatusNotImplemented:
			return true
		}
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "MethodNotAllowed", "NotImplemented", "NotSupported", "XNotImplemented":
			return true
		}
	}

	return false
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
	title := "S3 Provisioning Result"
	if result.Operation == operationDelete {
		title = "S3 Delete Result"
	}
	lines := []string{
		title,
		strings.Repeat("=", len(title)),
		fmt.Sprintf("Resources: %d", result.ResourceCount),
	}

	if result.ConfigFile != "" {
		lines = append(lines, fmt.Sprintf("Config file: %s", result.ConfigFile))
	}
	if result.DryRun {
		lines = append(lines, "Mode: dry-run")
	}

	for _, resource := range result.Resources {
		if result.Operation == operationDelete {
			bucketDeleteLabel := "Bucket deleted"
			if result.DryRun {
				bucketDeleteLabel = "Bucket delete planned"
			}
			lines = append(lines,
				"",
				fmt.Sprintf("Bucket: %s", resource.BucketName),
				fmt.Sprintf("Endpoint: %s", emptyFallback(resource.Endpoint, "(default AWS SDK resolution)")),
				fmt.Sprintf("Region: %s", resource.Region),
				fmt.Sprintf("%s: %s", bucketDeleteLabel, yesNo(resource.Deleted)),
			)
			if !result.DryRun {
				lines = append(lines, fmt.Sprintf("Objects deleted: %d", resource.ObjectsDeleted))
				if resource.CredentialsDeleted > 0 {
					lines = append(lines, fmt.Sprintf("Credentials deleted: %d", resource.CredentialsDeleted))
				}
			}
			for _, warning := range resource.Warnings {
				lines = append(lines, fmt.Sprintf("Warning: %s", warning))
			}
			continue
		}

		bucketCreateLabel := "Bucket created"
		versioningLabel := "Versioning enabled"
		encryptionLabel := "Encryption enabled"
		bucketPolicyLabel := "Bucket policy applied"
		scopedCredentialLabel := "Scoped credentials created"
		if result.DryRun {
			bucketCreateLabel = "Bucket create planned"
			versioningLabel = "Versioning requested"
			encryptionLabel = "Encryption requested"
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
			fmt.Sprintf("%s: %s", encryptionLabel, yesNo(resource.EncryptionEnabled)),
			fmt.Sprintf("%s: %s", bucketPolicyLabel, yesNo(resource.BucketPolicyApplied)),
		)

		if resource.BucketPolicySource != "" {
			lines = append(lines, fmt.Sprintf("Bucket policy source: %s", resource.BucketPolicySource))
		}

		if resource.ScopedCredentials != nil {
			identityLabel := "IAM user"
			policyLabel := "Credential policy template"
			if resource.ScopedCredentials.Provider == providerOVH {
				identityLabel = "OVH user"
				policyLabel = "OVH storage policy role"
			}

			lines = append(lines,
				fmt.Sprintf("%s: %s", scopedCredentialLabel, yesNo(true)),
				fmt.Sprintf("%s: %s", identityLabel, resource.ScopedCredentials.UserName),
				fmt.Sprintf("%s: %s", policyLabel, resource.ScopedCredentials.PolicyTemplate),
				fmt.Sprintf("Access key ID: %s", resource.ScopedCredentials.AccessKeyID),
				fmt.Sprintf("Secret access key: %s", resource.ScopedCredentials.SecretAccessKey),
			)
			if resource.ScopedCredentials.UserID != "" {
				lines = append(lines, fmt.Sprintf("User ID: %s", resource.ScopedCredentials.UserID))
			}
		}

		if resource.CredentialsRotated {
			rotationLabel := "Credentials rotated"
			if result.DryRun {
				rotationLabel = "Credential rotation planned"
			}
			lines = append(lines, fmt.Sprintf("%s: yes", rotationLabel))
			if !result.DryRun {
				lines = append(lines, fmt.Sprintf("Previous credentials deleted: %d", resource.CredentialsDeleted))
			}
		}
		for _, warning := range resource.Warnings {
			lines = append(lines, fmt.Sprintf("Warning: %s", warning))
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

func envDurationAliases(env map[string]string, names ...string) (*time.Duration, error) {
	for _, name := range names {
		value, ok := env[name]
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}

		parsed, err := parsePositiveDuration(value)
		if err != nil {
			return nil, fmt.Errorf("invalid duration value for %s: %q", name, value)
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

func boolPtrFromJSONField(data []byte, field string) (*bool, error) {
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	raw, ok := decoded[field]
	if !ok {
		return nil, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("config field %s must be a boolean", field)
	}
	return &value, nil
}

func durationPtrFromJSONFields(data []byte, fields ...string) (*time.Duration, error) {
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	for _, field := range fields {
		raw, ok := decoded[field]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("config field %s must be a duration string", field)
		}
		parsed, err := parsePositiveDuration(value)
		if err != nil {
			return nil, fmt.Errorf("config field %s must be a positive duration", field)
		}
		return &parsed, nil
	}
	return nil, nil
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

func changedDuration(fs *pflag.FlagSet, name, value string) (*time.Duration, error) {
	if !fs.Changed(name) {
		return nil, nil
	}
	parsed, err := parsePositiveDuration(value)
	if err != nil {
		return nil, fmt.Errorf("--%s must be a positive duration such as 30s, 5m, or 1h", name)
	}
	return &parsed, nil
}

func parsePositiveDuration(value string) (time.Duration, error) {
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, errors.New("duration must be positive")
	}
	return parsed, nil
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

func extractOutputFormat(args []string) string {
	output := ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--output" && index+1 < len(args):
			output = args[index+1]
		case arg == "-o" && index+1 < len(args):
			output = args[index+1]
		case strings.HasPrefix(arg, "--output="):
			output = strings.TrimPrefix(arg, "--output=")
		case strings.HasPrefix(arg, "-o="):
			output = strings.TrimPrefix(arg, "-o=")
		case strings.HasPrefix(arg, "-o") && !strings.HasPrefix(arg, "--") && len(arg) > len("-o"):
			output = strings.TrimPrefix(arg, "-o")
		}
	}
	return output
}

func detectOutputFormat(args []string, env map[string]string) string {
	output := defaultOutputFormat
	if configPath, err := resolveConfigPath(args, env); err == nil && configPath != "" {
		if configOutput := readConfigOutputFormat(configPath); configOutput != "" {
			output = configOutput
		}
	}
	if envOutput := envValue(env, envAliases.OutputFormat...); envOutput != "" {
		output = envOutput
	}
	if cliOutput := extractOutputFormat(args); cliOutput != "" {
		output = cliOutput
	}
	return strings.ToLower(strings.TrimSpace(output))
}

func readConfigOutputFormat(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		return ""
	}

	raw, ok := decoded["output"]
	if !ok {
		return ""
	}
	var output string
	if err := json.Unmarshal(raw, &output); err != nil {
		return ""
	}
	return output
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
