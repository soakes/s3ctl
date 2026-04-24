package s3ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
)

var credentialPolicyTemplates = map[string]string{
	"bucket-readonly":  "Allow list and read access to a single bucket",
	"bucket-readwrite": "Allow list, read, write, and delete access to a single bucket",
	"bucket-admin":     "Allow full S3 access to a single bucket and its objects",
}

const scopedCredentialPolicyName = "s3ctl-bucket-access"

type iamAPI interface {
	CreateUser(context.Context, *iam.CreateUserInput, ...func(*iam.Options)) (*iam.CreateUserOutput, error)
	PutUserPolicy(context.Context, *iam.PutUserPolicyInput, ...func(*iam.Options)) (*iam.PutUserPolicyOutput, error)
	DeleteUserPolicy(context.Context, *iam.DeleteUserPolicyInput, ...func(*iam.Options)) (*iam.DeleteUserPolicyOutput, error)
	DeleteUser(context.Context, *iam.DeleteUserInput, ...func(*iam.Options)) (*iam.DeleteUserOutput, error)
	CreateAccessKey(context.Context, *iam.CreateAccessKeyInput, ...func(*iam.Options)) (*iam.CreateAccessKeyOutput, error)
}

type scopedCredentialResult struct {
	UserName        string `json:"user_name"`
	PolicyTemplate  string `json:"policy_template"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

func newIAMClient(ctx context.Context, cfg settings) (iamAPI, error) {
	awsCfg, err := newAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return iam.NewFromConfig(awsCfg, func(options *iam.Options) {
		if cfg.IAMEndpoint != "" {
			options.BaseEndpoint = aws.String(cfg.IAMEndpoint)
		}
	}), nil
}

func createScopedCredentials(ctx context.Context, client iamAPI, target provisionTarget, cfg settings) (scopedCredentialResult, error) {
	userName, err := resolvedIAMUserName(target, cfg.IAMUserPrefix)
	if err != nil {
		return scopedCredentialResult{}, err
	}

	_, err = client.CreateUser(ctx, &iam.CreateUserInput{
		UserName: aws.String(userName),
		Path:     aws.String(cfg.IAMPath),
		Tags: []iamtypes.Tag{
			{
				Key:   aws.String("ManagedBy"),
				Value: aws.String("s3ctl"),
			},
			{
				Key:   aws.String("Bucket"),
				Value: aws.String(target.Bucket),
			},
		},
	})
	if err != nil {
		return scopedCredentialResult{}, fmt.Errorf("failed to create IAM user %q for bucket %q: %w", userName, target.Bucket, err)
	}

	policyDocument, err := buildCredentialPolicy(target.Bucket, target.CredentialPolicyTemplate)
	if err != nil {
		cleanupErr := cleanupIAMUser(ctx, client, userName, false)
		return scopedCredentialResult{}, wrapCleanupError(err, cleanupErr)
	}

	_, err = client.PutUserPolicy(ctx, &iam.PutUserPolicyInput{
		UserName:       aws.String(userName),
		PolicyName:     aws.String(scopedCredentialPolicyName),
		PolicyDocument: aws.String(policyDocument),
	})
	if err != nil {
		cleanupErr := cleanupIAMUser(ctx, client, userName, false)
		return scopedCredentialResult{}, wrapCleanupError(
			fmt.Errorf("failed to attach scoped policy to IAM user %q: %w", userName, err),
			cleanupErr,
		)
	}

	accessKeyOutput, err := client.CreateAccessKey(ctx, &iam.CreateAccessKeyInput{
		UserName: aws.String(userName),
	})
	if err != nil {
		cleanupErr := cleanupIAMUser(ctx, client, userName, true)
		return scopedCredentialResult{}, wrapCleanupError(
			fmt.Errorf("failed to create access key for IAM user %q: %w", userName, err),
			cleanupErr,
		)
	}

	return scopedCredentialResult{
		UserName:        userName,
		PolicyTemplate:  target.CredentialPolicyTemplate,
		AccessKeyID:     aws.ToString(accessKeyOutput.AccessKey.AccessKeyId),
		SecretAccessKey: aws.ToString(accessKeyOutput.AccessKey.SecretAccessKey),
	}, nil
}

func buildCredentialPolicy(bucket, template string) (string, error) {
	bucketARN := fmt.Sprintf("arn:aws:s3:::%s", bucket)
	objectARN := bucketARN + "/*"

	var document map[string]any
	switch template {
	case "bucket-readonly":
		document = map[string]any{
			"Version": "2012-10-17",
			"Statement": []map[string]any{
				{
					"Sid":      "BucketReadOnlyBucketMeta",
					"Effect":   "Allow",
					"Action":   []string{"s3:GetBucketLocation", "s3:ListBucket"},
					"Resource": []string{bucketARN},
				},
				{
					"Sid":      "BucketReadOnlyObjects",
					"Effect":   "Allow",
					"Action":   []string{"s3:GetObject"},
					"Resource": []string{objectARN},
				},
			},
		}
	case "bucket-readwrite":
		document = map[string]any{
			"Version": "2012-10-17",
			"Statement": []map[string]any{
				{
					"Sid":      "BucketReadWriteBucketMeta",
					"Effect":   "Allow",
					"Action":   []string{"s3:GetBucketLocation", "s3:ListBucket", "s3:ListBucketMultipartUploads"},
					"Resource": []string{bucketARN},
				},
				{
					"Sid":    "BucketReadWriteObjects",
					"Effect": "Allow",
					"Action": []string{
						"s3:GetObject",
						"s3:PutObject",
						"s3:DeleteObject",
						"s3:AbortMultipartUpload",
						"s3:ListMultipartUploadParts",
					},
					"Resource": []string{objectARN},
				},
			},
		}
	case "bucket-admin":
		document = map[string]any{
			"Version": "2012-10-17",
			"Statement": []map[string]any{
				{
					"Sid":      "BucketAdminAccess",
					"Effect":   "Allow",
					"Action":   []string{"s3:*"},
					"Resource": []string{bucketARN, objectARN},
				},
			},
		}
	default:
		return "", fmt.Errorf("unsupported credential policy template %q", template)
	}

	bytes, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func resolvedIAMUserName(target provisionTarget, prefix string) (string, error) {
	if explicit := strings.TrimSpace(target.IAMUserName); explicit != "" {
		return explicit, nil
	}

	if strings.TrimSpace(prefix) == "" {
		prefix = defaultIAMUserPrefix
	}

	candidate := prefix + sanitizeIAMUserComponent(target.Bucket)
	candidate = strings.Trim(candidate, "-_.")
	if candidate == "" {
		return "", fmt.Errorf("unable to derive an IAM user name from bucket %q", target.Bucket)
	}
	if len(candidate) > 64 {
		candidate = strings.Trim(candidate[:64], "-_.")
	}
	if candidate == "" {
		return "", fmt.Errorf("derived IAM user name for bucket %q is empty after sanitization", target.Bucket)
	}
	return candidate, nil
}

func sanitizeIAMUserComponent(value string) string {
	var builder strings.Builder
	lastWasDash := false
	for _, character := range strings.TrimSpace(value) {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			builder.WriteRune(character)
			lastWasDash = false
			continue
		}
		switch character {
		case '+', '=', ',', '.', '@', '_', '-':
			builder.WriteRune(character)
			lastWasDash = false
		default:
			if !lastWasDash {
				builder.WriteRune('-')
				lastWasDash = true
			}
		}
	}
	return builder.String()
}

func cleanupIAMUser(ctx context.Context, client iamAPI, userName string, deleteInlinePolicy bool) error {
	var cleanupErrors []error

	if deleteInlinePolicy {
		_, err := client.DeleteUserPolicy(ctx, &iam.DeleteUserPolicyInput{
			UserName:   aws.String(userName),
			PolicyName: aws.String(scopedCredentialPolicyName),
		})
		if err != nil && !isAWSNoSuchEntity(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("delete inline policy: %w", err))
		}
	}

	_, err := client.DeleteUser(ctx, &iam.DeleteUserInput{
		UserName: aws.String(userName),
	})
	if err != nil && !isAWSNoSuchEntity(err) {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("delete IAM user: %w", err))
	}

	return errors.Join(cleanupErrors...)
}

func wrapCleanupError(actionErr, cleanupErr error) error {
	if cleanupErr == nil {
		return actionErr
	}
	return errors.Join(actionErr, fmt.Errorf("cleanup failed: %w", cleanupErr))
}

func isAWSNoSuchEntity(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchEntity"
}
