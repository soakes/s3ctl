package s3ctl

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type mockS3Client struct {
	calls                 []string
	configs               []settings
	headBucketErr         error
	listVersionsOutputs   []*s3.ListObjectVersionsOutput
	listObjectsV2Outputs  []*s3.ListObjectsV2Output
	deleteObjectsOutputs  []*s3.DeleteObjectsOutput
	deleteBucketErr       error
	createBucketCalled    bool
	putVersioningCalled   bool
	putBucketPolicyCalled bool
}

func (m *mockS3Client) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	m.calls = append(m.calls, "HeadBucket")
	return &s3.HeadBucketOutput{}, m.headBucketErr
}

func (m *mockS3Client) CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	m.calls = append(m.calls, "CreateBucket")
	m.createBucketCalled = true
	return &s3.CreateBucketOutput{}, nil
}

func (m *mockS3Client) PutBucketVersioning(context.Context, *s3.PutBucketVersioningInput, ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	m.calls = append(m.calls, "PutBucketVersioning")
	m.putVersioningCalled = true
	return &s3.PutBucketVersioningOutput{}, nil
}

func (m *mockS3Client) PutBucketPolicy(context.Context, *s3.PutBucketPolicyInput, ...func(*s3.Options)) (*s3.PutBucketPolicyOutput, error) {
	m.calls = append(m.calls, "PutBucketPolicy")
	m.putBucketPolicyCalled = true
	return &s3.PutBucketPolicyOutput{}, nil
}

func (m *mockS3Client) ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	m.calls = append(m.calls, "ListObjectVersions")
	if len(m.listVersionsOutputs) == 0 {
		return &s3.ListObjectVersionsOutput{}, nil
	}
	output := m.listVersionsOutputs[0]
	m.listVersionsOutputs = m.listVersionsOutputs[1:]
	return output, nil
}

func (m *mockS3Client) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	m.calls = append(m.calls, "ListObjectsV2")
	if len(m.listObjectsV2Outputs) == 0 {
		return &s3.ListObjectsV2Output{}, nil
	}
	output := m.listObjectsV2Outputs[0]
	m.listObjectsV2Outputs = m.listObjectsV2Outputs[1:]
	return output, nil
}

func (m *mockS3Client) DeleteObjects(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	m.calls = append(m.calls, "DeleteObjects")
	if len(m.deleteObjectsOutputs) == 0 {
		return &s3.DeleteObjectsOutput{}, nil
	}
	output := m.deleteObjectsOutputs[0]
	m.deleteObjectsOutputs = m.deleteObjectsOutputs[1:]
	return output, nil
}

func (m *mockS3Client) DeleteBucket(context.Context, *s3.DeleteBucketInput, ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	m.calls = append(m.calls, "DeleteBucket")
	return &s3.DeleteBucketOutput{}, m.deleteBucketErr
}

func withMockS3Client(t *testing.T, client *mockS3Client) {
	t.Helper()

	previous := newS3APIClient
	newS3APIClient = func(_ context.Context, cfg settings) (s3API, error) {
		client.configs = append(client.configs, cfg)
		return client, nil
	}
	t.Cleanup(func() {
		newS3APIClient = previous
	})
}

func TestValidateSettingsRequiresForceForDelete(t *testing.T) {
	err := validateSettings(settings{
		Buckets:      []string{"app-data"},
		DeleteBucket: true,
	})
	if err == nil {
		t.Fatal("expected delete without force to fail validation")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected force validation error, got %q", err)
	}

	if err := validateSettings(settings{
		Buckets:      []string{"app-data"},
		DeleteBucket: true,
		DryRun:       true,
	}); err != nil {
		t.Fatalf("expected dry-run delete without force to validate, got %v", err)
	}
}

func TestLoadConfigSupportsDeleteAlias(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "s3ctl.json")
	if err := os.WriteFile(configPath, []byte(`{"bucket":"app-data","delete":true,"force":true,"timeout":"15m"}`), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	src, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}
	if src.DeleteBucket == nil || !*src.DeleteBucket {
		t.Fatalf("expected delete alias to enable delete_bucket, got %#v", src.DeleteBucket)
	}
	if src.Force == nil || !*src.Force {
		t.Fatalf("expected force from config, got %#v", src.Force)
	}
	if src.Timeout == nil || *src.Timeout != 15*time.Minute {
		t.Fatalf("expected timeout from config, got %#v", src.Timeout)
	}
}

func TestLoadConfigRejectsInvalidDeleteAlias(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "s3ctl.json")
	if err := os.WriteFile(configPath, []byte(`{"bucket":"app-data","delete":"yes"}`), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	_, err := loadConfig(configPath)
	if err == nil {
		t.Fatal("expected invalid delete alias to fail")
	}
	if !strings.Contains(err.Error(), "delete must be a boolean") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteS3BucketsEmptiesVersionsCurrentObjectsAndDeletesBucket(t *testing.T) {
	client := &mockS3Client{
		listVersionsOutputs: []*s3.ListObjectVersionsOutput{
			{
				Versions: []types.ObjectVersion{
					{Key: aws.String("versioned.txt"), VersionId: aws.String("v1")},
				},
				DeleteMarkers: []types.DeleteMarkerEntry{
					{Key: aws.String("deleted.txt"), VersionId: aws.String("m1")},
				},
			},
		},
		listObjectsV2Outputs: []*s3.ListObjectsV2Output{
			{
				Contents: []types.Object{
					{Key: aws.String("current.txt")},
				},
			},
		},
	}

	result, err := deleteS3Buckets(
		context.Background(),
		settings{DeleteBucket: true, Force: true, Region: "us-east-1"},
		[]provisionTarget{{Bucket: "app-data"}},
		provisionResult{Operation: operationDelete, ResourceCount: 1},
		client,
	)
	if err != nil {
		t.Fatalf("deleteS3Buckets returned error: %v", err)
	}

	resource := result.Resources[0]
	if !resource.Deleted || resource.ObjectsDeleted != 3 {
		t.Fatalf("expected deleted bucket and three object deletions, got %#v", resource)
	}

	wantCalls := []string{
		"HeadBucket",
		"ListObjectVersions",
		"DeleteObjects",
		"ListObjectsV2",
		"DeleteObjects",
		"DeleteBucket",
	}
	if strings.Join(client.calls, ",") != strings.Join(wantCalls, ",") {
		t.Fatalf("unexpected S3 calls:\nwant %#v\ngot  %#v", wantCalls, client.calls)
	}
}

func TestDeleteS3BucketsHandlesPaginatedListings(t *testing.T) {
	client := &mockS3Client{
		listVersionsOutputs: []*s3.ListObjectVersionsOutput{
			{
				Versions: []types.ObjectVersion{
					{Key: aws.String("versioned-1.txt"), VersionId: aws.String("v1")},
				},
				IsTruncated:         aws.Bool(true),
				NextKeyMarker:       aws.String("versioned-2.txt"),
				NextVersionIdMarker: aws.String("v2"),
			},
			{
				DeleteMarkers: []types.DeleteMarkerEntry{
					{Key: aws.String("deleted.txt"), VersionId: aws.String("m1")},
				},
			},
		},
		listObjectsV2Outputs: []*s3.ListObjectsV2Output{
			{
				Contents: []types.Object{
					{Key: aws.String("current-1.txt")},
				},
				IsTruncated:           aws.Bool(true),
				NextContinuationToken: aws.String("next-page"),
			},
			{
				Contents: []types.Object{
					{Key: aws.String("current-2.txt")},
				},
			},
		},
	}

	result, err := deleteS3Buckets(
		context.Background(),
		settings{DeleteBucket: true, Force: true, Region: "us-east-1"},
		[]provisionTarget{{Bucket: "app-data"}},
		provisionResult{Operation: operationDelete, ResourceCount: 1},
		client,
	)
	if err != nil {
		t.Fatalf("deleteS3Buckets returned error: %v", err)
	}
	if result.Resources[0].ObjectsDeleted != 4 {
		t.Fatalf("expected four deleted objects, got %#v", result.Resources[0])
	}

	wantCalls := []string{
		"HeadBucket",
		"ListObjectVersions",
		"DeleteObjects",
		"ListObjectVersions",
		"DeleteObjects",
		"ListObjectsV2",
		"DeleteObjects",
		"ListObjectsV2",
		"DeleteObjects",
		"DeleteBucket",
	}
	if strings.Join(client.calls, ",") != strings.Join(wantCalls, ",") {
		t.Fatalf("unexpected S3 calls:\nwant %#v\ngot  %#v", wantCalls, client.calls)
	}
}

func TestDeleteS3BucketsRejectsTruncatedVersionListingWithoutMarker(t *testing.T) {
	client := &mockS3Client{
		listVersionsOutputs: []*s3.ListObjectVersionsOutput{
			{IsTruncated: aws.Bool(true)},
		},
	}

	_, err := deleteS3Buckets(
		context.Background(),
		settings{DeleteBucket: true, Force: true, Region: "us-east-1"},
		[]provisionTarget{{Bucket: "app-data"}},
		provisionResult{Operation: operationDelete, ResourceCount: 1},
		client,
	)
	if err == nil {
		t.Fatal("expected truncated listing without marker to fail")
	}
	if !strings.Contains(err.Error(), "did not include a next marker") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteS3BucketsRejectsTruncatedCurrentListingWithoutToken(t *testing.T) {
	client := &mockS3Client{
		listObjectsV2Outputs: []*s3.ListObjectsV2Output{
			{IsTruncated: aws.Bool(true)},
		},
	}

	_, err := deleteS3Buckets(
		context.Background(),
		settings{DeleteBucket: true, Force: true, Region: "us-east-1"},
		[]provisionTarget{{Bucket: "app-data"}},
		provisionResult{Operation: operationDelete, ResourceCount: 1},
		client,
	)
	if err == nil {
		t.Fatal("expected truncated listing without token to fail")
	}
	if !strings.Contains(err.Error(), "did not include a continuation token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteS3BucketsReturnsObjectDeleteErrors(t *testing.T) {
	client := &mockS3Client{
		listObjectsV2Outputs: []*s3.ListObjectsV2Output{
			{Contents: []types.Object{{Key: aws.String("current.txt")}}},
		},
		deleteObjectsOutputs: []*s3.DeleteObjectsOutput{
			{
				Errors: []types.Error{
					{Key: aws.String("current.txt"), Code: aws.String("AccessDenied"), Message: aws.String("denied")},
				},
			},
		},
	}

	_, err := deleteS3Buckets(
		context.Background(),
		settings{DeleteBucket: true, Force: true, Region: "us-east-1"},
		[]provisionTarget{{Bucket: "app-data"}},
		provisionResult{Operation: operationDelete, ResourceCount: 1},
		client,
	)
	if err == nil {
		t.Fatal("expected object delete error")
	}
	if !strings.Contains(err.Error(), "current.txt (AccessDenied: denied)") {
		t.Fatalf("unexpected delete error: %v", err)
	}
}

func TestMainWithEnvDryRunDeleteDoesNotRequireForce(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := MainWithEnv(
		[]string{"--bucket", "app-data", "--delete", "--dry-run"},
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
	if !strings.Contains(stdout.String(), "Bucket delete planned: yes") {
		t.Fatalf("expected delete dry-run wording, got %q", stdout.String())
	}
}

func TestMainWithEnvDeleteWithoutForceFailsBeforeAPIUse(t *testing.T) {
	client := &mockS3Client{}
	withMockS3Client(t, client)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := MainWithEnv(
		[]string{"--bucket", "app-data", "--delete"},
		isolatedEnv(t, nil),
		&stdout,
		&stderr,
	)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "without --force") {
		t.Fatalf("expected force error, got %q", stderr.String())
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected validation to fail before S3 calls, got %#v", client.calls)
	}
}

func TestDeleteS3BucketsMissingBucketReturnsError(t *testing.T) {
	client := &mockS3Client{headBucketErr: errors.New("api error NoSuchBucket")}

	_, err := deleteS3Buckets(
		context.Background(),
		settings{DeleteBucket: true, Force: true, Region: "us-east-1"},
		[]provisionTarget{{Bucket: "app-data"}},
		provisionResult{Operation: operationDelete, ResourceCount: 1},
		client,
	)
	if err == nil {
		t.Fatal("expected missing bucket error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing bucket error, got %v", err)
	}
}
