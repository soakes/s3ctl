package s3ctl

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

type mockIAMClient struct {
	createUserErr          error
	putUserPolicyErr       error
	deleteUserPolicyErr    error
	createAccessKeyErr     error
	deleteUserErr          error
	createUserInput        *iam.CreateUserInput
	deleteUserCalled       bool
	deleteUserPolicyCalled bool
}

func (m *mockIAMClient) CreateUser(_ context.Context, input *iam.CreateUserInput, _ ...func(*iam.Options)) (*iam.CreateUserOutput, error) {
	m.createUserInput = input
	return &iam.CreateUserOutput{}, m.createUserErr
}

func (m *mockIAMClient) PutUserPolicy(context.Context, *iam.PutUserPolicyInput, ...func(*iam.Options)) (*iam.PutUserPolicyOutput, error) {
	return &iam.PutUserPolicyOutput{}, m.putUserPolicyErr
}

func (m *mockIAMClient) DeleteUserPolicy(context.Context, *iam.DeleteUserPolicyInput, ...func(*iam.Options)) (*iam.DeleteUserPolicyOutput, error) {
	m.deleteUserPolicyCalled = true
	return &iam.DeleteUserPolicyOutput{}, m.deleteUserPolicyErr
}

func (m *mockIAMClient) DeleteUser(context.Context, *iam.DeleteUserInput, ...func(*iam.Options)) (*iam.DeleteUserOutput, error) {
	m.deleteUserCalled = true
	return &iam.DeleteUserOutput{}, m.deleteUserErr
}

func (m *mockIAMClient) CreateAccessKey(context.Context, *iam.CreateAccessKeyInput, ...func(*iam.Options)) (*iam.CreateAccessKeyOutput, error) {
	if m.createAccessKeyErr != nil {
		return nil, m.createAccessKeyErr
	}

	return &iam.CreateAccessKeyOutput{
		AccessKey: &iamtypes.AccessKey{
			AccessKeyId:     aws.String("AKIAEXAMPLE"),
			SecretAccessKey: aws.String("secret"),
		},
	}, nil
}

func TestCreateScopedCredentialsCleansUpUserWhenPolicyAttachFails(t *testing.T) {
	client := &mockIAMClient{
		putUserPolicyErr: errors.New("boom"),
	}

	_, err := createScopedCredentials(context.Background(), client, provisionTarget{
		Bucket:                   "app-data",
		CredentialPolicyTemplate: "bucket-readwrite",
	}, settings{
		IAMPath:       defaultIAMPath,
		IAMUserPrefix: defaultIAMUserPrefix,
	})
	if err == nil {
		t.Fatal("expected createScopedCredentials to fail")
	}
	if !client.deleteUserCalled {
		t.Fatal("expected cleanup to delete IAM user")
	}
	if client.deleteUserPolicyCalled {
		t.Fatal("did not expect inline policy cleanup when policy attachment never succeeded")
	}
}

func TestCreateScopedCredentialsCleansUpPolicyAndUserWhenAccessKeyFails(t *testing.T) {
	client := &mockIAMClient{
		createAccessKeyErr: errors.New("boom"),
	}

	_, err := createScopedCredentials(context.Background(), client, provisionTarget{
		Bucket:                   "app-data",
		CredentialPolicyTemplate: "bucket-readwrite",
	}, settings{
		IAMPath:       defaultIAMPath,
		IAMUserPrefix: defaultIAMUserPrefix,
	})
	if err == nil {
		t.Fatal("expected createScopedCredentials to fail")
	}
	if !client.deleteUserPolicyCalled {
		t.Fatal("expected cleanup to delete inline policy")
	}
	if !client.deleteUserCalled {
		t.Fatal("expected cleanup to delete IAM user")
	}
}

func TestCreateScopedCredentialsOmitsDefaultIAMPath(t *testing.T) {
	client := &mockIAMClient{}

	_, err := createScopedCredentials(context.Background(), client, provisionTarget{
		Bucket:                   "app-data",
		CredentialPolicyTemplate: "bucket-readwrite",
	}, settings{
		IAMPath:                  defaultIAMPath,
		IAMUserPrefix:            defaultIAMUserPrefix,
		CredentialPolicyTemplate: defaultCredentialPolicyTemplate,
	})
	if err != nil {
		t.Fatalf("createScopedCredentials returned error: %v", err)
	}
	if client.createUserInput == nil {
		t.Fatal("expected CreateUser input to be captured")
	}
	if client.createUserInput.Path != nil {
		t.Fatalf("expected IAM path to be omitted by default, got %q", aws.ToString(client.createUserInput.Path))
	}
	if aws.ToString(client.createUserInput.UserName) != "app-data" {
		t.Fatalf("expected generated IAM user name without prefix, got %q", aws.ToString(client.createUserInput.UserName))
	}
}

func TestCreateScopedCredentialsUsesConfiguredIAMPath(t *testing.T) {
	client := &mockIAMClient{}

	_, err := createScopedCredentials(context.Background(), client, provisionTarget{
		Bucket:                   "app-data",
		CredentialPolicyTemplate: "bucket-readwrite",
	}, settings{
		IAMPath:                  "/s3ctl/",
		IAMUserPrefix:            "svc-",
		CredentialPolicyTemplate: defaultCredentialPolicyTemplate,
	})
	if err != nil {
		t.Fatalf("createScopedCredentials returned error: %v", err)
	}
	if client.createUserInput == nil {
		t.Fatal("expected CreateUser input to be captured")
	}
	if aws.ToString(client.createUserInput.Path) != "/s3ctl/" {
		t.Fatalf("expected configured IAM path, got %q", aws.ToString(client.createUserInput.Path))
	}
	if aws.ToString(client.createUserInput.UserName) != "svc-app-data" {
		t.Fatalf("expected generated IAM user name with prefix, got %q", aws.ToString(client.createUserInput.UserName))
	}
}
