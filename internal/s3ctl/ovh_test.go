package s3ctl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	ovhapi "github.com/ovh/go-ovh/ovh"
)

type recordedOVHCall struct {
	Method string
	Path   string
	Body   any
}

type mockOVHClient struct {
	calls             []recordedOVHCall
	createdUser       ovhUserDetail
	containersByPath  map[string]ovhStorageContainer
	credentialsByPath map[string][]ovhS3Credentials
	getUserByPath     map[string]ovhUserDetail
	usersByPath       map[string][]ovhUserDetail
	postErrByPath     map[string]error
	deleteErrByPath   map[string]error
}

func (m *mockOVHClient) GetWithContext(_ context.Context, path string, response any) error {
	m.calls = append(m.calls, recordedOVHCall{
		Method: "GET",
		Path:   path,
	})
	switch typed := response.(type) {
	case *ovhUserDetail:
		user := m.getUserByPath[path]
		if user.ID == 0 {
			user = ovhUserDetail{ID: 1234, Username: "user-abcd", Status: "ok"}
		}
		*typed = user
	case *[]ovhUserDetail:
		*typed = m.usersByPath[path]
	case *[]ovhS3Credentials:
		*typed = m.credentialsByPath[path]
	case *ovhStorageContainer:
		container := m.containersByPath[path]
		if container.Name == "" {
			container = ovhStorageContainer{Name: "app-data", OwnerID: 1234}
		}
		*typed = container
	default:
		return errors.New("unexpected response type")
	}
	return nil
}

func (m *mockOVHClient) PostWithContext(_ context.Context, path string, body, response any) error {
	m.calls = append(m.calls, recordedOVHCall{
		Method: "POST",
		Path:   path,
		Body:   body,
	})
	if err := m.postErrByPath[path]; err != nil {
		return err
	}

	switch typed := response.(type) {
	case *ovhUserDetail:
		user := m.createdUser
		if user.ID == 0 {
			user = ovhUserDetail{ID: 1234, Username: "user-abcd", Status: "ok"}
		}
		*typed = user
	case *ovhS3CredentialsWithSecret:
		*typed = ovhS3CredentialsWithSecret{Access: "OVHACCESS", Secret: "OVHSECRET"}
	case *ovhStorageContainer:
		*typed = ovhStorageContainer{Name: "app-data"}
	case nil:
	default:
		return errors.New("unexpected response type")
	}
	return nil
}

func (m *mockOVHClient) PutWithContext(context.Context, string, any, any) error {
	return nil
}

func (m *mockOVHClient) DeleteWithContext(_ context.Context, path string, _ any) error {
	m.calls = append(m.calls, recordedOVHCall{
		Method: "DELETE",
		Path:   path,
	})
	return m.deleteErrByPath[path]
}

func withMockOVHClient(t *testing.T, client *mockOVHClient) {
	t.Helper()

	previous := newOVHAPIClient
	newOVHAPIClient = func(settings) (ovhAPI, error) {
		return client, nil
	}
	t.Cleanup(func() {
		newOVHAPIClient = previous
	})
}

func TestProvisionWithOVHCreatesUserCredentialsContainerAndPolicy(t *testing.T) {
	client := &mockOVHClient{}
	withMockOVHClient(t, client)

	result, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		EnableVersioning:     true,
		OVHEncryptData:       true,
		OVHEncryptDataSet:    true,
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
	})
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected one resource, got %#v", result.Resources)
	}

	resource := result.Resources[0]
	if resource.Endpoint != "https://s3.gra.io.cloud.ovh.net" {
		t.Fatalf("unexpected OVH S3 endpoint: %q", resource.Endpoint)
	}
	if !resource.Created || !resource.VersioningEnabled {
		t.Fatalf("expected created versioned resource, got %#v", resource)
	}
	if !resource.EncryptionEnabled {
		t.Fatalf("expected encrypted OVH resource, got %#v", resource)
	}
	if resource.ScopedCredentials == nil {
		t.Fatal("expected OVH scoped credentials")
	}
	if resource.ScopedCredentials.Provider != providerOVH {
		t.Fatalf("expected OVH provider in credentials, got %#v", resource.ScopedCredentials)
	}
	if resource.ScopedCredentials.UserID != "1234" || resource.ScopedCredentials.UserName != "user-abcd" {
		t.Fatalf("unexpected OVH user identity: %#v", resource.ScopedCredentials)
	}
	if resource.ScopedCredentials.AccessKeyID != "OVHACCESS" || resource.ScopedCredentials.SecretAccessKey != "OVHSECRET" {
		t.Fatalf("unexpected OVH credentials: %#v", resource.ScopedCredentials)
	}

	gotPaths := make([]string, 0, len(client.calls))
	for _, call := range client.calls {
		gotPaths = append(gotPaths, call.Method+" "+call.Path)
	}
	wantPaths := []string{
		"POST /cloud/project/project123/user",
		"POST /cloud/project/project123/user/1234/s3Credentials",
		"POST /cloud/project/project123/region/GRA/storage",
		"POST /cloud/project/project123/region/GRA/storage/app-data/policy/1234",
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("unexpected OVH API calls:\nwant %#v\ngot  %#v", wantPaths, gotPaths)
	}

	userBody, ok := client.calls[0].Body.(ovhProjectUserCreation)
	if !ok {
		t.Fatalf("unexpected user body type: %#v", client.calls[0].Body)
	}
	if !reflect.DeepEqual(userBody.Roles, []string{defaultOVHUserRole}) {
		t.Fatalf("unexpected OVH user roles: %#v", userBody)
	}
	if userBody.Description != "app-data" {
		t.Fatalf("expected OVH user description to match bucket name, got %q", userBody.Description)
	}

	containerBody, ok := client.calls[2].Body.(ovhStorageContainerCreation)
	if !ok {
		t.Fatalf("unexpected container body type: %#v", client.calls[2].Body)
	}
	if containerBody.Name != "app-data" || containerBody.OwnerID != 1234 {
		t.Fatalf("unexpected container body: %#v", containerBody)
	}
	if containerBody.Versioning == nil || containerBody.Versioning.Status != "enabled" {
		t.Fatalf("expected enabled OVH versioning, got %#v", containerBody.Versioning)
	}
	if containerBody.Encryption == nil || containerBody.Encryption.SSEAlgorithm != ovhStorageEncryptionAlgorithmAES256 {
		t.Fatalf("expected enabled OVH encryption, got %#v", containerBody.Encryption)
	}

	policyBody, ok := client.calls[3].Body.(ovhAddContainerPolicy)
	if !ok {
		t.Fatalf("unexpected policy body type: %#v", client.calls[3].Body)
	}
	if policyBody.RoleName != defaultOVHStoragePolicyRole {
		t.Fatalf("unexpected OVH policy body: %#v", policyBody)
	}
}

func TestProvisionWithOVHDryRunPlansCredentials(t *testing.T) {
	result, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "BHS",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
		DryRun:               true,
	})
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}
	credentials := result.Resources[0].ScopedCredentials
	if credentials == nil {
		t.Fatal("expected dry-run OVH credentials")
	}
	if credentials.Provider != providerOVH || credentials.AccessKeyID != generatedOnApply {
		t.Fatalf("unexpected dry-run credentials: %#v", credentials)
	}
}

func TestProvisionWithOVHRotatesCredentialsByContainerOwner(t *testing.T) {
	client := &mockOVHClient{
		containersByPath: map[string]ovhStorageContainer{
			"/cloud/project/project123/region/GRA/storage/app-data": {Name: "app-data", OwnerID: 1234},
		},
		getUserByPath: map[string]ovhUserDetail{
			"/cloud/project/project123/user/1234": {ID: 1234, Description: "app-data", Username: "user-abcd", Status: "ok"},
		},
		credentialsByPath: map[string][]ovhS3Credentials{
			"/cloud/project/project123/user/1234/s3Credentials": {
				{Access: "OLDACCESS1"},
				{Access: "OLDACCESS2"},
			},
		},
	}
	withMockOVHClient(t, client)

	result, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
		OVHRotateCredentials: true,
	})
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}

	resource := result.Resources[0]
	if !resource.CredentialsRotated || resource.CredentialsDeleted != 2 {
		t.Fatalf("expected rotated credentials and two deletions, got %#v", resource)
	}
	if resource.ScopedCredentials == nil {
		t.Fatal("expected rotated OVH credentials")
	}
	if resource.ScopedCredentials.AccessKeyID != "OVHACCESS" || resource.ScopedCredentials.SecretAccessKey != "OVHSECRET" {
		t.Fatalf("unexpected rotated credentials: %#v", resource.ScopedCredentials)
	}
	if resource.ScopedCredentials.UserName != "app-data" {
		t.Fatalf("expected bucket label as OVH user name, got %q", resource.ScopedCredentials.UserName)
	}

	gotPaths := make([]string, 0, len(client.calls))
	for _, call := range client.calls {
		gotPaths = append(gotPaths, call.Method+" "+call.Path)
	}
	wantPaths := []string{
		"GET /cloud/project/project123/region/GRA/storage/app-data",
		"GET /cloud/project/project123/user/1234",
		"GET /cloud/project/project123/user/1234/s3Credentials",
		"POST /cloud/project/project123/user/1234/s3Credentials",
		"DELETE /cloud/project/project123/user/1234/s3Credentials/OLDACCESS1",
		"DELETE /cloud/project/project123/user/1234/s3Credentials/OLDACCESS2",
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("unexpected OVH API calls:\nwant %#v\ngot  %#v", wantPaths, gotPaths)
	}
}

func TestProvisionWithOVHRotationWarnsWhenOldCredentialDeleteFails(t *testing.T) {
	client := &mockOVHClient{
		getUserByPath: map[string]ovhUserDetail{
			"/cloud/project/project123/user/1234": {ID: 1234, Description: "app-data", Username: "user-abcd", Status: "ok"},
		},
		credentialsByPath: map[string][]ovhS3Credentials{
			"/cloud/project/project123/user/1234/s3Credentials": {{Access: "OLDACCESS"}},
		},
		deleteErrByPath: map[string]error{
			"/cloud/project/project123/user/1234/s3Credentials/OLDACCESS": errors.New("delete failed"),
		},
	}
	withMockOVHClient(t, client)

	result, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
		OVHRotateCredentials: true,
	})
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}
	resource := result.Resources[0]
	if resource.CredentialsDeleted != 0 || len(resource.Warnings) != 1 {
		t.Fatalf("expected delete warning without deletion count, got %#v", resource)
	}
	if !strings.Contains(resource.Warnings[0], "failed to delete previous access key OLDACCESS") {
		t.Fatalf("unexpected warning: %#v", resource.Warnings)
	}
	if resource.ScopedCredentials == nil || resource.ScopedCredentials.SecretAccessKey != "OVHSECRET" {
		t.Fatalf("expected new secret to be returned despite cleanup warning, got %#v", resource.ScopedCredentials)
	}
}

func TestProvisionWithOVHRotationRefusesSharedOwner(t *testing.T) {
	client := &mockOVHClient{
		containersByPath: map[string]ovhStorageContainer{
			"/cloud/project/project123/region/GRA/storage/app-data": {Name: "app-data", OwnerID: 1234},
		},
		getUserByPath: map[string]ovhUserDetail{
			"/cloud/project/project123/user/1234": {ID: 1234, Description: "shared-storage-user", Username: "shared-user", Status: "ok"},
		},
	}
	withMockOVHClient(t, client)

	_, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
		OVHRotateCredentials: true,
	})
	if err == nil {
		t.Fatal("expected rotation to refuse a shared owner")
	}
	if !strings.Contains(err.Error(), "does not look bucket-dedicated") {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, call := range client.calls {
		if call.Method == "POST" || call.Method == "DELETE" {
			t.Fatalf("expected no mutating OVH calls, got %#v", client.calls)
		}
	}
}

func TestProvisionWithOVHRotationDryRunDoesNotCallAPI(t *testing.T) {
	client := &mockOVHClient{}
	withMockOVHClient(t, client)

	result, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
		OVHRotateCredentials: true,
		DryRun:               true,
	})
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected dry-run rotation not to call OVH API, got %#v", client.calls)
	}
	resource := result.Resources[0]
	if !resource.CredentialsRotated || resource.ScopedCredentials.AccessKeyID != generatedOnApply {
		t.Fatalf("unexpected dry-run rotation resource: %#v", resource)
	}
}

func TestProvisionWithOVHDeleteRefusesSharedOwner(t *testing.T) {
	ovhClient := &mockOVHClient{
		containersByPath: map[string]ovhStorageContainer{
			"/cloud/project/project123/region/GRA/storage/app-data": {Name: "app-data", OwnerID: 1234},
		},
		getUserByPath: map[string]ovhUserDetail{
			"/cloud/project/project123/user/1234": {ID: 1234, Description: "shared-storage-user", Username: "shared-user", Status: "ok"},
		},
	}
	withMockOVHClient(t, ovhClient)

	s3Client := &mockS3Client{}
	withMockS3Client(t, s3Client)

	_, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
		DeleteBucket:         true,
		Force:                true,
	})
	if err == nil {
		t.Fatal("expected delete to refuse a shared owner")
	}
	if !strings.Contains(err.Error(), "does not look bucket-dedicated") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s3Client.configs) != 0 || len(s3Client.calls) != 0 {
		t.Fatalf("expected no temporary S3 client use, got configs=%#v calls=%#v", s3Client.configs, s3Client.calls)
	}
	for _, call := range ovhClient.calls {
		if call.Method == "POST" || call.Method == "DELETE" {
			t.Fatalf("expected no mutating OVH calls, got %#v", ovhClient.calls)
		}
	}
}

func TestProvisionWithOVHDeleteEmptiesContainerAndDeletesUser(t *testing.T) {
	ovhClient := &mockOVHClient{
		containersByPath: map[string]ovhStorageContainer{
			"/cloud/project/project123/region/GRA/storage/app-data": {Name: "app-data", OwnerID: 1234},
		},
		getUserByPath: map[string]ovhUserDetail{
			"/cloud/project/project123/user/1234": {ID: 1234, Description: "app-data", Username: "user-abcd", Status: "ok"},
		},
		credentialsByPath: map[string][]ovhS3Credentials{
			"/cloud/project/project123/user/1234/s3Credentials": {
				{Access: "OLDACCESS"},
			},
		},
	}
	withMockOVHClient(t, ovhClient)

	s3Client := &mockS3Client{}
	withMockS3Client(t, s3Client)

	result, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
		DeleteBucket:         true,
		Force:                true,
	})
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}

	resource := result.Resources[0]
	if !resource.Deleted || resource.CredentialsDeleted != 2 {
		t.Fatalf("expected deleted OVH bucket and two credential deletions, got %#v", resource)
	}
	if len(resource.Warnings) != 0 {
		t.Fatalf("expected no cleanup warnings, got %#v", resource.Warnings)
	}
	if len(s3Client.configs) != 1 {
		t.Fatalf("expected one temporary S3 client config, got %#v", s3Client.configs)
	}
	s3Config := s3Client.configs[0]
	if s3Config.Endpoint != "https://s3.gra.io.cloud.ovh.net" || s3Config.Region != "gra" {
		t.Fatalf("unexpected temporary OVH S3 config: %#v", s3Config)
	}
	if s3Config.AccessKey != "OVHACCESS" || s3Config.SecretKey != "OVHSECRET" {
		t.Fatalf("expected temporary OVH S3 credentials, got %#v", s3Config)
	}

	gotPaths := make([]string, 0, len(ovhClient.calls))
	for _, call := range ovhClient.calls {
		gotPaths = append(gotPaths, call.Method+" "+call.Path)
	}
	wantPaths := []string{
		"GET /cloud/project/project123/region/GRA/storage/app-data",
		"GET /cloud/project/project123/user/1234",
		"GET /cloud/project/project123/user/1234/s3Credentials",
		"POST /cloud/project/project123/user/1234/s3Credentials",
		"DELETE /cloud/project/project123/region/GRA/storage/app-data",
		"DELETE /cloud/project/project123/user/1234/s3Credentials/OLDACCESS",
		"DELETE /cloud/project/project123/user/1234/s3Credentials/OVHACCESS",
		"GET /cloud/project/project123/user/1234",
		"DELETE /cloud/project/project123/user/1234",
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("unexpected OVH API calls:\nwant %#v\ngot  %#v", wantPaths, gotPaths)
	}

	wantS3Calls := []string{"ListObjectVersions", "ListObjectsV2"}
	if !reflect.DeepEqual(s3Client.calls, wantS3Calls) {
		t.Fatalf("unexpected temporary S3 calls:\nwant %#v\ngot  %#v", wantS3Calls, s3Client.calls)
	}
}

func TestProvisionWithOVHDeleteEmptyContainerWithoutForce(t *testing.T) {
	ovhClient := &mockOVHClient{
		containersByPath: map[string]ovhStorageContainer{
			"/cloud/project/project123/region/GRA/storage/app-data": {Name: "app-data", OwnerID: 1234},
		},
		getUserByPath: map[string]ovhUserDetail{
			"/cloud/project/project123/user/1234": {ID: 1234, Description: "app-data", Username: "user-abcd", Status: "ok"},
		},
		credentialsByPath: map[string][]ovhS3Credentials{
			"/cloud/project/project123/user/1234/s3Credentials": {
				{Access: "OLDACCESS"},
			},
		},
	}
	withMockOVHClient(t, ovhClient)

	s3Client := &mockS3Client{}
	withMockS3Client(t, s3Client)

	result, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
		DeleteBucket:         true,
	})
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}

	resource := result.Resources[0]
	if !resource.Deleted || resource.ObjectsDeleted != 0 || resource.CredentialsDeleted != 2 {
		t.Fatalf("expected empty OVH bucket delete without force, got %#v", resource)
	}

	wantS3Calls := []string{"ListObjectVersions", "ListObjectsV2"}
	if !reflect.DeepEqual(s3Client.calls, wantS3Calls) {
		t.Fatalf("unexpected temporary S3 calls:\nwant %#v\ngot  %#v", wantS3Calls, s3Client.calls)
	}
}

func TestProvisionWithOVHDeleteRefusesNonEmptyContainerWithoutForce(t *testing.T) {
	ovhClient := &mockOVHClient{
		containersByPath: map[string]ovhStorageContainer{
			"/cloud/project/project123/region/GRA/storage/app-data": {Name: "app-data", OwnerID: 1234},
		},
		getUserByPath: map[string]ovhUserDetail{
			"/cloud/project/project123/user/1234": {ID: 1234, Description: "app-data", Username: "user-abcd", Status: "ok"},
		},
	}
	withMockOVHClient(t, ovhClient)

	s3Client := &mockS3Client{
		listObjectsV2Outputs: []*s3.ListObjectsV2Output{
			{Contents: []types.Object{{Key: aws.String("current.txt")}}},
		},
	}
	withMockS3Client(t, s3Client)

	_, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
		DeleteBucket:         true,
	})
	if err == nil {
		t.Fatal("expected non-empty OVH delete without force to fail")
	}
	if !strings.Contains(err.Error(), "non-empty bucket") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("unexpected error: %v", err)
	}

	gotPaths := make([]string, 0, len(ovhClient.calls))
	for _, call := range ovhClient.calls {
		gotPaths = append(gotPaths, call.Method+" "+call.Path)
	}
	wantPaths := []string{
		"GET /cloud/project/project123/region/GRA/storage/app-data",
		"GET /cloud/project/project123/user/1234",
		"GET /cloud/project/project123/user/1234/s3Credentials",
		"POST /cloud/project/project123/user/1234/s3Credentials",
		"DELETE /cloud/project/project123/user/1234/s3Credentials/OVHACCESS",
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("unexpected OVH API calls:\nwant %#v\ngot  %#v", wantPaths, gotPaths)
	}

	wantS3Calls := []string{"ListObjectVersions", "ListObjectsV2"}
	if !reflect.DeepEqual(s3Client.calls, wantS3Calls) {
		t.Fatalf("unexpected temporary S3 calls:\nwant %#v\ngot  %#v", wantS3Calls, s3Client.calls)
	}
}

func TestProvisionWithOVHUppercasesAPIRegionButKeepsLowercaseEndpoint(t *testing.T) {
	client := &mockOVHClient{}
	withMockOVHClient(t, client)

	result, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "uk",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
	})
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}
	if result.Resources[0].Endpoint != "https://s3.uk.io.cloud.ovh.net" {
		t.Fatalf("unexpected OVH S3 endpoint: %q", result.Resources[0].Endpoint)
	}

	gotPaths := make([]string, 0, len(client.calls))
	for _, call := range client.calls {
		gotPaths = append(gotPaths, call.Method+" "+call.Path)
	}
	wantPaths := []string{
		"POST /cloud/project/project123/user",
		"POST /cloud/project/project123/user/1234/s3Credentials",
		"POST /cloud/project/project123/region/UK/storage",
		"POST /cloud/project/project123/region/UK/storage/app-data/policy/1234",
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("unexpected OVH API calls:\nwant %#v\ngot  %#v", wantPaths, gotPaths)
	}
}

func TestProvisionWithOVHCanExplicitlyDisableEncryption(t *testing.T) {
	client := &mockOVHClient{}
	withMockOVHClient(t, client)

	_, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
		OVHEncryptData:       false,
		OVHEncryptDataSet:    true,
	})
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}

	containerBody, ok := client.calls[2].Body.(ovhStorageContainerCreation)
	if !ok {
		t.Fatalf("unexpected container body type: %#v", client.calls[2].Body)
	}
	if containerBody.Encryption == nil || containerBody.Encryption.SSEAlgorithm != ovhStorageEncryptionPlaintext {
		t.Fatalf("expected plaintext OVH encryption setting, got %#v", containerBody.Encryption)
	}
}

func TestProvisionWithOVHWaitsForUserReadyBeforeCredentials(t *testing.T) {
	client := &mockOVHClient{
		createdUser: ovhUserDetail{ID: 1234, Username: "user-abcd", Status: "creating"},
		getUserByPath: map[string]ovhUserDetail{
			"/cloud/project/project123/user/1234": {ID: 1234, Username: "user-abcd", Status: "ok"},
		},
	}
	withMockOVHClient(t, client)

	_, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
	})
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}

	gotPaths := make([]string, 0, len(client.calls))
	for _, call := range client.calls {
		gotPaths = append(gotPaths, call.Method+" "+call.Path)
	}
	wantPaths := []string{
		"POST /cloud/project/project123/user",
		"GET /cloud/project/project123/user/1234",
		"POST /cloud/project/project123/user/1234/s3Credentials",
		"POST /cloud/project/project123/region/GRA/storage",
		"POST /cloud/project/project123/region/GRA/storage/app-data/policy/1234",
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("unexpected OVH API calls:\nwant %#v\ngot  %#v", wantPaths, gotPaths)
	}
}

func TestProvisionWithOVHCleansUpCreatedResourcesWhenPolicyFails(t *testing.T) {
	policyPath := "/cloud/project/project123/region/GRA/storage/app-data/policy/1234"
	client := &mockOVHClient{
		postErrByPath: map[string]error{
			policyPath: errors.New("policy failed"),
		},
	}
	withMockOVHClient(t, client)

	_, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
	})
	if err == nil {
		t.Fatal("expected OVH provisioning to fail")
	}
	if !strings.Contains(err.Error(), "failed to attach OVH container policy") {
		t.Fatalf("expected policy failure context, got %q", err)
	}

	gotPaths := make([]string, 0, len(client.calls))
	for _, call := range client.calls {
		gotPaths = append(gotPaths, call.Method+" "+call.Path)
	}
	for _, want := range []string{
		"DELETE /cloud/project/project123/region/GRA/storage/app-data",
		"DELETE /cloud/project/project123/user/1234/s3Credentials/OVHACCESS",
		"DELETE /cloud/project/project123/user/1234",
	} {
		found := false
		for _, got := range gotPaths {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected cleanup call %q in %#v", want, gotPaths)
		}
	}
}

func TestProvisionWithOVHAnnotatesServiceNotFound(t *testing.T) {
	client := &mockOVHClient{
		postErrByPath: map[string]error{
			"/cloud/project/project123/user": &ovhapi.APIError{
				Code:    404,
				Class:   "Client::NotFound",
				Message: "This service does not exist",
			},
		},
	}
	withMockOVHClient(t, client)

	_, err := provision(context.Background(), settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHAPIEndpoint:       "ovh-eu",
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
	})
	if err == nil {
		t.Fatal("expected OVH provisioning to fail")
	}
	for _, want := range []string{
		"Public Cloud project \"project123\"",
		"endpoint \"ovh-eu\"",
		"project ID, not the display name",
		"OVH IAM policy",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got %q", want, err)
		}
	}
}

func TestValidateOVHSettingsRequiresServiceNameAndOVHRegion(t *testing.T) {
	err := validateSettings(settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               defaultRegion,
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
	})
	if err == nil {
		t.Fatal("expected OVH validation to fail")
	}
	if !strings.Contains(err.Error(), "ovh-service-name") {
		t.Fatalf("expected service name validation error, got %q", err)
	}

	err = validateSettings(settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               defaultRegion,
		OVHServiceName:       "project123",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
	})
	if err == nil {
		t.Fatal("expected OVH region validation to fail")
	}
	if !strings.Contains(err.Error(), "OVH Public Cloud region") {
		t.Fatalf("expected region validation error, got %q", err)
	}
}

func TestResolveSettingsReadsOVHCredentialsFromConfigAndEnv(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "ovh.json")
	if err := os.WriteFile(
		configPath,
		[]byte(`{
			"provider": "ovh",
			"bucket": "app-data",
			"region": "GRA",
			"ovh_service_name": "project123",
			"ovh_application_key": "config-app-key",
			"ovh_application_secret": "config-app-secret",
			"ovh_consumer_key": "config-consumer-key"
		}`),
		0o644,
	); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg, _, err := resolveSettings(
		[]string{"--config", configPath},
		isolatedEnv(t, map[string]string{
			"S3CTL_OVH_APPLICATION_SECRET": "env-app-secret",
			"OVH_CONSUMER_KEY":             "env-consumer-key",
		}),
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}
	if cfg.OVHApplicationKey != "config-app-key" {
		t.Fatalf("expected OVH application key from config, got %q", cfg.OVHApplicationKey)
	}
	if cfg.OVHApplicationSecret != "env-app-secret" {
		t.Fatalf("expected OVH application secret from env, got %q", cfg.OVHApplicationSecret)
	}
	if cfg.OVHConsumerKey != "env-consumer-key" {
		t.Fatalf("expected OVH consumer key from env alias, got %q", cfg.OVHConsumerKey)
	}
}

func TestResolveSettingsReadsModernOVHAuthFromConfigAndEnv(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "ovh.json")
	if err := os.WriteFile(
		configPath,
		[]byte(`{
			"provider": "ovh",
			"bucket": "app-data",
			"region": "GRA",
			"ovh_service_name": "project123",
			"ovh_client_id": "config-client-id",
			"ovh_encrypt_data": true,
			"ovh_rotate_credentials": true
		}`),
		0o644,
	); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg, _, err := resolveSettings(
		[]string{"--config", configPath},
		isolatedEnv(t, map[string]string{
			"S3CTL_OVH_CLIENT_SECRET": "env-client-secret",
		}),
	)
	if err != nil {
		t.Fatalf("resolveSettings returned error: %v", err)
	}
	if cfg.OVHClientID != "config-client-id" {
		t.Fatalf("expected OVH client id from config, got %q", cfg.OVHClientID)
	}
	if cfg.OVHClientSecret != "env-client-secret" {
		t.Fatalf("expected OVH client secret from env, got %q", cfg.OVHClientSecret)
	}
	if !cfg.OVHEncryptData {
		t.Fatal("expected OVH encryption from config to be true")
	}
	if !cfg.OVHRotateCredentials {
		t.Fatal("expected OVH credential rotation from config to be true")
	}
}

func TestValidateOVHSettingsRequiresCompleteLegacyCredentials(t *testing.T) {
	err := validateSettings(settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHApplicationKey:    "app-key",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
	})
	if err == nil {
		t.Fatal("expected partial OVH credentials to fail validation")
	}
	if !strings.Contains(err.Error(), "classic OVH API application credentials") {
		t.Fatalf("expected incomplete classic credentials error, got %q", err)
	}
}

func TestNewOVHClientUsesExplicitLegacyCredentials(t *testing.T) {
	client, err := newOVHClient(settings{
		OVHAPIEndpoint:       "http://example.test",
		OVHApplicationKey:    "app-key",
		OVHApplicationSecret: "app-secret",
		OVHConsumerKey:       "consumer-key",
	})
	if err != nil {
		t.Fatalf("newOVHClient returned error: %v", err)
	}

	typed, ok := client.(*ovhapi.Client)
	if !ok {
		t.Fatalf("expected *ovh.Client, got %T", client)
	}
	if typed.Endpoint() != "http://example.test" {
		t.Fatalf("expected custom endpoint, got %q", typed.Endpoint())
	}
	if typed.AppKey != "app-key" || typed.AppSecret != "app-secret" || typed.ConsumerKey != "consumer-key" {
		t.Fatalf("unexpected OVH credentials on client: %#v", typed)
	}
}

func TestValidateOVHSettingsRejectsMixedAuthModes(t *testing.T) {
	err := validateSettings(settings{
		Provider:             providerOVH,
		Buckets:              []string{"app-data"},
		Region:               "GRA",
		OVHServiceName:       "project123",
		OVHAccessToken:       "access-token",
		OVHClientID:          "client-id",
		OVHClientSecret:      "client-secret",
		OVHUserRole:          defaultOVHUserRole,
		OVHStoragePolicyRole: defaultOVHStoragePolicyRole,
	})
	if err == nil {
		t.Fatal("expected mixed OVH auth modes to fail")
	}
	if !strings.Contains(err.Error(), "only one configured auth mode") {
		t.Fatalf("expected mixed auth mode error, got %q", err)
	}
}

func TestNewOVHClientUsesAccessToken(t *testing.T) {
	client, err := newOVHClient(settings{
		OVHAPIEndpoint: "http://example.test",
		OVHAccessToken: "access-token",
	})
	if err != nil {
		t.Fatalf("newOVHClient returned error: %v", err)
	}

	typed, ok := client.(*ovhapi.Client)
	if !ok {
		t.Fatalf("expected *ovh.Client, got %T", client)
	}
	if typed.Endpoint() != "http://example.test" {
		t.Fatalf("expected custom endpoint, got %q", typed.Endpoint())
	}
	if typed.AccessToken != "access-token" {
		t.Fatalf("unexpected access token on client: %#v", typed)
	}
}

func TestNewOVHClientUsesOAuth2Credentials(t *testing.T) {
	client, err := newOVHClient(settings{
		OVHAPIEndpoint:  "ovh-eu",
		OVHClientID:     "client-id",
		OVHClientSecret: "client-secret",
	})
	if err != nil {
		t.Fatalf("newOVHClient returned error: %v", err)
	}

	typed, ok := client.(*ovhapi.Client)
	if !ok {
		t.Fatalf("expected *ovh.Client, got %T", client)
	}
	if typed.ClientID != "client-id" || typed.ClientSecret != "client-secret" {
		t.Fatalf("unexpected OAuth2 credentials on client: %#v", typed)
	}
}
