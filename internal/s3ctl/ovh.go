package s3ctl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	ovhapi "github.com/ovh/go-ovh/ovh"
)

const (
	generatedOnApply                    = "(generated on apply)"
	ovhStorageEncryptionAlgorithmAES256 = "AES256"
	ovhStorageEncryptionPlaintext       = "plaintext"
	ovhUserReadyPollPeriod              = 2 * time.Second
)

var newOVHAPIClient = newOVHClient

type ovhAPI interface {
	GetWithContext(context.Context, string, any) error
	PostWithContext(context.Context, string, any, any) error
	PutWithContext(context.Context, string, any, any) error
	DeleteWithContext(context.Context, string, any) error
}

type ovhProjectUserCreation struct {
	Description string   `json:"description,omitempty"`
	Roles       []string `json:"roles,omitempty"`
}

type ovhUserDetail struct {
	Description string `json:"description"`
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Status      string `json:"status"`
}

type ovhS3Credentials struct {
	Access string `json:"access"`
}

type ovhS3CredentialsWithSecret struct {
	Access string `json:"access"`
	Secret string `json:"secret"`
}

type ovhStorageContainerCreation struct {
	Name       string                      `json:"name"`
	OwnerID    int64                       `json:"ownerId,omitempty"`
	Encryption *ovhStorageEncryptionObject `json:"encryption,omitempty"`
	Tags       map[string]string           `json:"tags,omitempty"`
	Versioning *ovhStorageVersioningObject `json:"versioning,omitempty"`
}

type ovhStorageEncryptionObject struct {
	SSEAlgorithm string `json:"sseAlgorithm,omitempty"`
}

type ovhStorageVersioningObject struct {
	Status string `json:"status,omitempty"`
}

type ovhStorageContainer struct {
	Name    string `json:"name"`
	OwnerID int64  `json:"ownerId"`
}

type ovhAddContainerPolicy struct {
	RoleName string `json:"roleName"`
}

func validateOVHSettings(cfg settings) error {
	if strings.TrimSpace(cfg.OVHServiceName) == "" {
		return errors.New("OVH provider requires --ovh-service-name, config ovh_service_name, or S3CTL_OVH_SERVICE_NAME")
	}
	if strings.TrimSpace(cfg.Region) == "" || strings.TrimSpace(cfg.Region) == defaultRegion {
		return errors.New("OVH provider requires --region to be an OVH Public Cloud region such as GRA, BHS, SBG, UK, or EU-WEST-PAR")
	}
	if cfg.BucketPolicyFile != "" || cfg.BucketPolicyTemplate != "" {
		return errors.New("OVH provider manages access with OVH container policies; bucket policy files and templates are not supported")
	}
	if strings.TrimSpace(cfg.OVHUserRole) == "" {
		return errors.New("OVH provider requires a non-empty OVH user role")
	}
	if err := validateOVHAuthSettings(cfg); err != nil {
		return err
	}
	if !validOVHStoragePolicyRole(normalizeOVHStoragePolicyRole(cfg.OVHStoragePolicyRole)) {
		return fmt.Errorf("--ovh-storage-policy-role must be one of admin, deny, readOnly, or readWrite, got %q", cfg.OVHStoragePolicyRole)
	}
	return nil
}

func validateOVHAuthSettings(cfg settings) error {
	legacyFields := map[string]string{
		"ovh_application_key":    strings.TrimSpace(cfg.OVHApplicationKey),
		"ovh_application_secret": strings.TrimSpace(cfg.OVHApplicationSecret),
		"ovh_consumer_key":       strings.TrimSpace(cfg.OVHConsumerKey),
	}
	legacySet, err := validateOVHAuthGroup("classic OVH API application credentials", legacyFields)
	if err != nil {
		return err
	}

	oauthFields := map[string]string{
		"ovh_client_id":     strings.TrimSpace(cfg.OVHClientID),
		"ovh_client_secret": strings.TrimSpace(cfg.OVHClientSecret),
	}
	oauthSet, err := validateOVHAuthGroup("OVH OAuth2 client credentials", oauthFields)
	if err != nil {
		return err
	}

	accessTokenSet := strings.TrimSpace(cfg.OVHAccessToken) != ""
	authModes := 0
	for _, set := range []bool{legacySet, oauthSet, accessTokenSet} {
		if set {
			authModes++
		}
	}
	if authModes > 1 {
		return errors.New("OVH provider accepts only one configured auth mode: classic OVH API application credentials, OAuth2 client credentials, or access token")
	}

	return nil
}

func validateOVHAuthGroup(label string, fields map[string]string) (bool, error) {
	anySet := false
	for _, value := range fields {
		if value != "" {
			anySet = true
			break
		}
	}
	if !anySet {
		return false, nil
	}

	for name, value := range fields {
		if value == "" {
			return true, fmt.Errorf("OVH provider requires %s when any %s are configured", name, label)
		}
	}
	return true, nil
}

func provisionWithOVH(ctx context.Context, cfg settings, targets []provisionTarget, result provisionResult) (provisionResult, error) {
	var client ovhAPI
	var err error
	if !cfg.DryRun {
		client, err = newOVHAPIClient(cfg)
		if err != nil {
			return provisionResult{}, err
		}
	}

	endpoint := effectiveOVHS3Endpoint(cfg)
	if cfg.DeleteBucket {
		return deleteOVHBuckets(ctx, cfg, targets, result, client, endpoint)
	}
	if cfg.OVHRotateCredentials {
		return rotateOVHCredentials(ctx, cfg, targets, result, client, endpoint)
	}

	for _, target := range targets {
		resource := resourceResult{
			BucketName: target.Bucket,
			Endpoint:   endpoint,
			Region:     cfg.Region,
		}

		if cfg.DryRun {
			resource.Created = true
			resource.VersioningEnabled = target.EnableVersioning
			resource.EncryptionEnabled = cfg.OVHEncryptData
			resource.ScopedCredentials = &scopedCredentialResult{
				Provider:        providerOVH,
				UserID:          generatedOnApply,
				UserName:        generatedOnApply,
				PolicyTemplate:  normalizeOVHStoragePolicyRole(cfg.OVHStoragePolicyRole),
				AccessKeyID:     generatedOnApply,
				SecretAccessKey: generatedOnApply,
			}
			result.Resources = append(result.Resources, resource)
			continue
		}

		user, err := createOVHUser(ctx, client, cfg, target)
		if err != nil {
			return provisionResult{}, err
		}
		userID := strconv.FormatInt(user.ID, 10)

		credentials, err := createOVHS3Credentials(ctx, client, cfg, user.ID, target.Bucket)
		if err != nil {
			return provisionResult{}, wrapCleanupError(err, cleanupOVHUser(ctx, client, cfg, user.ID))
		}

		if err := createOVHContainer(ctx, client, cfg, target, user.ID); err != nil {
			cleanupErr := cleanupOVHUserAndCredentials(ctx, client, cfg, user.ID, credentials.Access)
			return provisionResult{}, wrapCleanupError(err, cleanupErr)
		}
		resource.Created = true
		resource.VersioningEnabled = target.EnableVersioning
		resource.EncryptionEnabled = cfg.OVHEncryptData

		if err := attachOVHContainerPolicy(ctx, client, cfg, target.Bucket, userID); err != nil {
			cleanupErr := errors.Join(
				cleanupOVHContainer(ctx, client, cfg, target.Bucket),
				cleanupOVHUserAndCredentials(ctx, client, cfg, user.ID, credentials.Access),
			)
			return provisionResult{}, wrapCleanupError(err, cleanupErr)
		}

		resource.ScopedCredentials = &scopedCredentialResult{
			Provider:        providerOVH,
			UserID:          userID,
			UserName:        emptyFallback(user.Username, userID),
			PolicyTemplate:  normalizeOVHStoragePolicyRole(cfg.OVHStoragePolicyRole),
			AccessKeyID:     credentials.Access,
			SecretAccessKey: credentials.Secret,
		}

		result.Resources = append(result.Resources, resource)
	}

	return result, nil
}

func deleteOVHBuckets(ctx context.Context, cfg settings, targets []provisionTarget, result provisionResult, client ovhAPI, endpoint string) (provisionResult, error) {
	for _, target := range targets {
		resource := resourceResult{
			BucketName: target.Bucket,
			Endpoint:   endpoint,
			Region:     cfg.Region,
			Deleted:    true,
		}

		if cfg.DryRun {
			result.Resources = append(result.Resources, resource)
			continue
		}

		user, err := findOVHBucketUser(ctx, client, cfg, target.Bucket)
		if err != nil {
			return provisionResult{}, fmt.Errorf("failed to find OVH user for bucket %q before delete: %w", target.Bucket, err)
		}
		user, err = waitForOVHUserReady(ctx, client, cfg, user)
		if err != nil {
			return provisionResult{}, fmt.Errorf("failed waiting for OVH user %d for bucket %q before delete: %w", user.ID, target.Bucket, err)
		}

		existingCredentials, err := listOVHS3Credentials(ctx, client, cfg, user.ID, target.Bucket)
		if err != nil {
			return provisionResult{}, err
		}

		temporaryCredentials, err := createOVHS3Credentials(ctx, client, cfg, user.ID, target.Bucket)
		if err != nil {
			return provisionResult{}, err
		}

		s3Client, err := newOVHS3Client(ctx, cfg, endpoint, temporaryCredentials)
		if err != nil {
			return provisionResult{}, wrapCleanupError(err, cleanupOVHS3Credentials(ctx, client, cfg, user.ID, temporaryCredentials.Access))
		}

		if cfg.Force {
			deletedObjects, err := emptyS3Bucket(ctx, s3Client, target.Bucket)
			if err != nil {
				return provisionResult{}, wrapCleanupError(err, cleanupOVHS3Credentials(ctx, client, cfg, user.ID, temporaryCredentials.Access))
			}
			resource.ObjectsDeleted = deletedObjects
		} else {
			if err := ensureS3BucketEmpty(ctx, s3Client, target.Bucket); err != nil {
				return provisionResult{}, wrapCleanupError(err, cleanupOVHS3Credentials(ctx, client, cfg, user.ID, temporaryCredentials.Access))
			}
		}

		if err := cleanupOVHContainer(ctx, client, cfg, target.Bucket); err != nil {
			return provisionResult{}, wrapCleanupError(err, cleanupOVHS3Credentials(ctx, client, cfg, user.ID, temporaryCredentials.Access))
		}

		deletedCredentials, warnings := cleanupOVHBucketUserCredentials(ctx, client, cfg, user.ID, appendOVHCredential(existingCredentials, temporaryCredentials.Access))
		resource.CredentialsDeleted = deletedCredentials
		resource.Warnings = append(resource.Warnings, warnings...)
		if err := cleanupOVHUser(ctx, client, cfg, user.ID); err != nil {
			resource.Warnings = append(resource.Warnings, err.Error())
		}

		result.Resources = append(result.Resources, resource)
	}

	return result, nil
}

func newOVHS3Client(ctx context.Context, cfg settings, endpoint string, credentials ovhS3CredentialsWithSecret) (s3API, error) {
	s3Config := cfg
	s3Config.Provider = providerS3
	s3Config.Endpoint = endpoint
	s3Config.Region = effectiveOVHS3Region(cfg)
	s3Config.Profile = ""
	s3Config.AccessKey = credentials.Access
	s3Config.SecretKey = credentials.Secret
	s3Config.SessionToken = ""

	client, err := newS3APIClient(ctx, s3Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary OVH S3 client for bucket cleanup: %w", err)
	}
	return client, nil
}

func appendOVHCredential(existing []ovhS3Credentials, access string) []ovhS3Credentials {
	if strings.TrimSpace(access) == "" {
		return existing
	}
	return append(existing, ovhS3Credentials{Access: access})
}

func cleanupOVHBucketUserCredentials(ctx context.Context, client ovhAPI, cfg settings, userID int64, credentials []ovhS3Credentials) (int, []string) {
	seen := make(map[string]struct{}, len(credentials))
	deleted := 0
	warnings := make([]string, 0)
	for _, credential := range credentials {
		access := strings.TrimSpace(credential.Access)
		if access == "" {
			continue
		}
		if _, ok := seen[access]; ok {
			continue
		}
		seen[access] = struct{}{}

		if err := cleanupOVHS3Credentials(ctx, client, cfg, userID, access); err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		deleted++
	}
	return deleted, warnings
}

func rotateOVHCredentials(ctx context.Context, cfg settings, targets []provisionTarget, result provisionResult, client ovhAPI, endpoint string) (provisionResult, error) {
	for _, target := range targets {
		resource := resourceResult{
			BucketName:         target.Bucket,
			Endpoint:           endpoint,
			Region:             cfg.Region,
			CredentialsRotated: true,
		}

		if cfg.DryRun {
			resource.ScopedCredentials = &scopedCredentialResult{
				Provider:        providerOVH,
				UserID:          generatedOnApply,
				UserName:        target.Bucket,
				PolicyTemplate:  normalizeOVHStoragePolicyRole(cfg.OVHStoragePolicyRole),
				AccessKeyID:     generatedOnApply,
				SecretAccessKey: generatedOnApply,
			}
			result.Resources = append(result.Resources, resource)
			continue
		}

		credentials, user, deleted, warnings, err := rotateOVHBucketCredentials(ctx, client, cfg, target.Bucket)
		if err != nil {
			return provisionResult{}, err
		}

		resource.CredentialsDeleted = deleted
		resource.Warnings = warnings
		resource.ScopedCredentials = &scopedCredentialResult{
			Provider:        providerOVH,
			UserID:          strconv.FormatInt(user.ID, 10),
			UserName:        emptyFallback(user.Description, emptyFallback(user.Username, strconv.FormatInt(user.ID, 10))),
			PolicyTemplate:  normalizeOVHStoragePolicyRole(cfg.OVHStoragePolicyRole),
			AccessKeyID:     credentials.Access,
			SecretAccessKey: credentials.Secret,
		}

		result.Resources = append(result.Resources, resource)
	}

	return result, nil
}

func rotateOVHBucketCredentials(ctx context.Context, client ovhAPI, cfg settings, bucket string) (ovhS3CredentialsWithSecret, ovhUserDetail, int, []string, error) {
	user, err := findOVHBucketUser(ctx, client, cfg, bucket)
	if err != nil {
		return ovhS3CredentialsWithSecret{}, ovhUserDetail{}, 0, nil, err
	}
	user, err = waitForOVHUserReady(ctx, client, cfg, user)
	if err != nil {
		return ovhS3CredentialsWithSecret{}, ovhUserDetail{}, 0, nil, fmt.Errorf("failed waiting for OVH user %d for bucket %q to become ready: %w", user.ID, bucket, err)
	}

	existing, err := listOVHS3Credentials(ctx, client, cfg, user.ID, bucket)
	if err != nil {
		return ovhS3CredentialsWithSecret{}, ovhUserDetail{}, 0, nil, err
	}

	credentials, err := createOVHS3Credentials(ctx, client, cfg, user.ID, bucket)
	if err != nil {
		return ovhS3CredentialsWithSecret{}, ovhUserDetail{}, 0, nil, err
	}

	deleted := 0
	warnings := make([]string, 0)
	for _, previous := range existing {
		if previous.Access == "" || previous.Access == credentials.Access {
			continue
		}
		if err := cleanupOVHS3Credentials(ctx, client, cfg, user.ID, previous.Access); err != nil {
			warnings = append(warnings, fmt.Sprintf("created new access key %s but failed to delete previous access key %s: %v", credentials.Access, previous.Access, err))
			continue
		}
		deleted++
	}

	return credentials, user, deleted, warnings, nil
}

func newOVHClient(cfg settings) (ovhAPI, error) {
	endpoint := strings.TrimSpace(cfg.OVHAPIEndpoint)
	accessToken := strings.TrimSpace(cfg.OVHAccessToken)
	applicationKey := strings.TrimSpace(cfg.OVHApplicationKey)
	applicationSecret := strings.TrimSpace(cfg.OVHApplicationSecret)
	consumerKey := strings.TrimSpace(cfg.OVHConsumerKey)
	clientID := strings.TrimSpace(cfg.OVHClientID)
	clientSecret := strings.TrimSpace(cfg.OVHClientSecret)
	if accessToken != "" {
		return ovhapi.NewAccessTokenClient(endpoint, accessToken)
	}
	if clientID != "" || clientSecret != "" {
		return ovhapi.NewOAuth2Client(endpoint, clientID, clientSecret)
	}
	if applicationKey != "" || applicationSecret != "" || consumerKey != "" {
		return ovhapi.NewClient(endpoint, applicationKey, applicationSecret, consumerKey)
	}
	if endpoint != "" {
		return ovhapi.NewEndpointClient(endpoint)
	}
	return ovhapi.NewDefaultClient()
}

func createOVHUser(ctx context.Context, client ovhAPI, cfg settings, target provisionTarget) (ovhUserDetail, error) {
	body := ovhProjectUserCreation{
		Description: target.Bucket,
		Roles:       []string{cfg.OVHUserRole},
	}

	var user ovhUserDetail
	path := ovhProjectPath(cfg, "user")
	if err := client.PostWithContext(ctx, path, body, &user); err != nil {
		return ovhUserDetail{}, fmt.Errorf("failed to create OVH Public Cloud user for bucket %q: %w", target.Bucket, annotateOVHServiceNotFound(cfg, err))
	}
	if user.ID == 0 {
		return ovhUserDetail{}, fmt.Errorf("OVH user create response did not include an id for bucket %q", target.Bucket)
	}
	user, err := waitForOVHUserReady(ctx, client, cfg, user)
	if err != nil {
		return ovhUserDetail{}, fmt.Errorf("failed waiting for OVH Public Cloud user %d for bucket %q to become ready: %w", user.ID, target.Bucket, err)
	}
	return user, nil
}

func waitForOVHUserReady(ctx context.Context, client ovhAPI, cfg settings, user ovhUserDetail) (ovhUserDetail, error) {
	if normalizeOVHUserStatus(user.Status) == "ok" {
		return user, nil
	}

	path := ovhProjectPath(cfg, "user", strconv.FormatInt(user.ID, 10))
	for {
		var current ovhUserDetail
		if err := client.GetWithContext(ctx, path, &current); err != nil {
			return ovhUserDetail{}, fmt.Errorf("get OVH user %d status: %w", user.ID, err)
		}
		user = mergeOVHUserDetail(user, current)

		switch normalizeOVHUserStatus(user.Status) {
		case "ok":
			return user, nil
		case "deleted", "deleting", "disabled":
			return ovhUserDetail{}, fmt.Errorf("OVH user %d entered status %q", user.ID, user.Status)
		}

		timer := time.NewTimer(ovhUserReadyPollPeriod)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ovhUserDetail{}, fmt.Errorf("last status %q: %w", user.Status, ctx.Err())
		case <-timer.C:
		}
	}
}

func mergeOVHUserDetail(previous, current ovhUserDetail) ovhUserDetail {
	if current.ID == 0 {
		current.ID = previous.ID
	}
	if current.Username == "" {
		current.Username = previous.Username
	}
	if current.Description == "" {
		current.Description = previous.Description
	}
	if current.Status == "" {
		current.Status = previous.Status
	}
	return current
}

func normalizeOVHUserStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func annotateOVHServiceNotFound(cfg settings, err error) error {
	var apiErr *ovhapi.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != http.StatusNotFound || !strings.Contains(apiErr.Message, "service does not exist") {
		return err
	}

	endpoint := strings.TrimSpace(cfg.OVHAPIEndpoint)
	if endpoint == "" {
		endpoint = "ovh-eu"
	}

	return fmt.Errorf("%w; OVH returned 404 for Public Cloud project %q on endpoint %q. Check ovh_service_name is the Public Cloud project ID, not the display name, and that the OAuth2 service account has an OVH IAM policy granting Public Cloud project API access to this project", err, cfg.OVHServiceName, endpoint)
}

func findOVHBucketUser(ctx context.Context, client ovhAPI, cfg settings, bucket string) (ovhUserDetail, error) {
	container, err := getOVHContainer(ctx, client, cfg, bucket)
	if err == nil && container.OwnerID != 0 {
		user, err := getOVHUser(ctx, client, cfg, container.OwnerID)
		if err != nil {
			return ovhUserDetail{}, fmt.Errorf("failed to look up OVH owner user %d for bucket %q: %w", container.OwnerID, bucket, err)
		}
		if !ovhUserMatchesBucket(user, bucket) {
			return ovhUserDetail{}, fmt.Errorf("refusing to manage OVH bucket %q because owner user %d does not look bucket-dedicated; expected user description or username to match the bucket name", bucket, container.OwnerID)
		}
		return user, nil
	}
	if err != nil {
		return ovhUserDetail{}, fmt.Errorf("failed to look up OVH container %q: %w", bucket, err)
	}

	users, err := listOVHUsers(ctx, client, cfg)
	if err != nil {
		return ovhUserDetail{}, err
	}

	matches := matchingOVHBucketUsers(users, bucket)
	if len(matches) == 0 {
		return ovhUserDetail{}, fmt.Errorf("no OVH Public Cloud user was found for bucket %q; expected the container owner or a user description matching the bucket name", bucket)
	}
	if len(matches) > 1 {
		return ovhUserDetail{}, fmt.Errorf("multiple OVH Public Cloud users match bucket %q; delete duplicate stale users or rotate credentials manually", bucket)
	}
	return matches[0], nil
}

func getOVHUser(ctx context.Context, client ovhAPI, cfg settings, userID int64) (ovhUserDetail, error) {
	var user ovhUserDetail
	path := ovhProjectPath(cfg, "user", strconv.FormatInt(userID, 10))
	if err := client.GetWithContext(ctx, path, &user); err != nil {
		return ovhUserDetail{}, err
	}
	if user.ID == 0 {
		user.ID = userID
	}
	return user, nil
}

func getOVHContainer(ctx context.Context, client ovhAPI, cfg settings, bucket string) (ovhStorageContainer, error) {
	var container ovhStorageContainer
	path := ovhRegionStoragePath(cfg, bucket)
	if err := client.GetWithContext(ctx, path, &container); err != nil {
		return ovhStorageContainer{}, err
	}
	return container, nil
}

func listOVHUsers(ctx context.Context, client ovhAPI, cfg settings) ([]ovhUserDetail, error) {
	var users []ovhUserDetail
	path := ovhProjectPath(cfg, "user")
	if err := client.GetWithContext(ctx, path, &users); err != nil {
		return nil, fmt.Errorf("failed to list OVH Public Cloud users: %w", err)
	}
	return users, nil
}

func matchingOVHBucketUsers(users []ovhUserDetail, bucket string) []ovhUserDetail {
	exactDescriptionMatches := make([]ovhUserDetail, 0)
	legacyDescriptionMatches := make([]ovhUserDetail, 0)
	usernameMatches := make([]ovhUserDetail, 0)
	for _, user := range users {
		switch {
		case ovhUserDescriptionMatchesBucket(user.Description, bucket):
			exactDescriptionMatches = append(exactDescriptionMatches, user)
		case ovhUserDescriptionMatchesLegacyBucket(user.Description, bucket):
			legacyDescriptionMatches = append(legacyDescriptionMatches, user)
		case ovhUserNameMatchesBucket(user.Username, bucket):
			usernameMatches = append(usernameMatches, user)
		}
	}
	if len(exactDescriptionMatches) > 0 {
		return exactDescriptionMatches
	}
	if len(legacyDescriptionMatches) > 0 {
		return legacyDescriptionMatches
	}
	return usernameMatches
}

func ovhUserMatchesBucket(user ovhUserDetail, bucket string) bool {
	return ovhUserDescriptionMatchesBucket(user.Description, bucket) ||
		ovhUserDescriptionMatchesLegacyBucket(user.Description, bucket) ||
		ovhUserNameMatchesBucket(user.Username, bucket)
}

func ovhUserDescriptionMatchesBucket(description, bucket string) bool {
	return strings.TrimSpace(description) == bucket
}

func ovhUserDescriptionMatchesLegacyBucket(description, bucket string) bool {
	return strings.TrimSpace(description) == "s3ctl bucket "+bucket
}

func ovhUserNameMatchesBucket(username, bucket string) bool {
	return strings.TrimSpace(username) == bucket
}

func createOVHS3Credentials(ctx context.Context, client ovhAPI, cfg settings, userID int64, bucket string) (ovhS3CredentialsWithSecret, error) {
	var credentials ovhS3CredentialsWithSecret
	path := ovhProjectPath(cfg, "user", strconv.FormatInt(userID, 10), "s3Credentials")
	if err := client.PostWithContext(ctx, path, nil, &credentials); err != nil {
		return ovhS3CredentialsWithSecret{}, fmt.Errorf("failed to create OVH S3 credentials for user %d and bucket %q: %w", userID, bucket, err)
	}
	if credentials.Access == "" || credentials.Secret == "" {
		return ovhS3CredentialsWithSecret{}, fmt.Errorf("OVH S3 credential create response for user %d and bucket %q did not include access and secret", userID, bucket)
	}
	return credentials, nil
}

func listOVHS3Credentials(ctx context.Context, client ovhAPI, cfg settings, userID int64, bucket string) ([]ovhS3Credentials, error) {
	var credentials []ovhS3Credentials
	path := ovhProjectPath(cfg, "user", strconv.FormatInt(userID, 10), "s3Credentials")
	if err := client.GetWithContext(ctx, path, &credentials); err != nil {
		return nil, fmt.Errorf("failed to list OVH S3 credentials for user %d and bucket %q: %w", userID, bucket, err)
	}
	return credentials, nil
}

func createOVHContainer(ctx context.Context, client ovhAPI, cfg settings, target provisionTarget, ownerID int64) error {
	body := ovhStorageContainerCreation{
		Name:    target.Bucket,
		OwnerID: ownerID,
		Tags: map[string]string{
			"managed-by": "s3ctl",
		},
	}
	if target.EnableVersioning {
		body.Versioning = &ovhStorageVersioningObject{Status: "enabled"}
	}
	if cfg.OVHEncryptDataSet {
		body.Encryption = &ovhStorageEncryptionObject{SSEAlgorithm: ovhStorageEncryptionPlaintext}
		if cfg.OVHEncryptData {
			body.Encryption.SSEAlgorithm = ovhStorageEncryptionAlgorithmAES256
		}
	}

	var container ovhStorageContainer
	path := ovhRegionStoragePath(cfg)
	if err := client.PostWithContext(ctx, path, body, &container); err != nil {
		return fmt.Errorf("failed to create OVH container %q in region %q: %w", target.Bucket, cfg.Region, err)
	}
	return nil
}

func attachOVHContainerPolicy(ctx context.Context, client ovhAPI, cfg settings, bucket, userID string) error {
	role := normalizeOVHStoragePolicyRole(cfg.OVHStoragePolicyRole)
	body := ovhAddContainerPolicy{RoleName: role}
	path := ovhRegionStoragePath(cfg, bucket, "policy", userID)
	if err := client.PostWithContext(ctx, path, body, nil); err != nil {
		return fmt.Errorf("failed to attach OVH container policy %q for bucket %q to user %s: %w", role, bucket, userID, err)
	}
	return nil
}

func cleanupOVHUserAndCredentials(ctx context.Context, client ovhAPI, cfg settings, userID int64, accessKey string) error {
	return errors.Join(
		cleanupOVHS3Credentials(ctx, client, cfg, userID, accessKey),
		cleanupOVHUser(ctx, client, cfg, userID),
	)
}

func cleanupOVHS3Credentials(ctx context.Context, client ovhAPI, cfg settings, userID int64, accessKey string) error {
	if strings.TrimSpace(accessKey) == "" {
		return nil
	}

	path := ovhProjectPath(cfg, "user", strconv.FormatInt(userID, 10), "s3Credentials", accessKey)
	if err := client.DeleteWithContext(ctx, path, nil); err != nil {
		return fmt.Errorf("delete OVH S3 credentials for user %d: %w", userID, err)
	}
	return nil
}

func cleanupOVHUser(ctx context.Context, client ovhAPI, cfg settings, userID int64) error {
	if _, err := waitForOVHUserReady(ctx, client, cfg, ovhUserDetail{ID: userID}); err != nil {
		return fmt.Errorf("wait for OVH user %d before delete: %w", userID, err)
	}

	path := ovhProjectPath(cfg, "user", strconv.FormatInt(userID, 10))
	if err := client.DeleteWithContext(ctx, path, nil); err != nil {
		return fmt.Errorf("delete OVH user %d: %w", userID, err)
	}
	return nil
}

func cleanupOVHContainer(ctx context.Context, client ovhAPI, cfg settings, bucket string) error {
	path := ovhRegionStoragePath(cfg, bucket)
	if err := client.DeleteWithContext(ctx, path, nil); err != nil {
		return fmt.Errorf("delete OVH container %q: %w", bucket, err)
	}
	return nil
}

func ovhProjectPath(cfg settings, parts ...string) string {
	segments := []string{"cloud", "project", cfg.OVHServiceName}
	segments = append(segments, parts...)
	return "/" + joinEscapedPath(segments...)
}

func ovhRegionStoragePath(cfg settings, parts ...string) string {
	segments := []string{"cloud", "project", cfg.OVHServiceName, "region", effectiveOVHAPIRegion(cfg), "storage"}
	segments = append(segments, parts...)
	return "/" + joinEscapedPath(segments...)
}

func joinEscapedPath(parts ...string) string {
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		escaped = append(escaped, url.PathEscape(part))
	}
	return strings.Join(escaped, "/")
}

func effectiveOVHS3Endpoint(cfg settings) string {
	if strings.TrimSpace(cfg.OVHS3Endpoint) != "" {
		return cfg.OVHS3Endpoint
	}
	return fmt.Sprintf("https://s3.%s.io.cloud.ovh.net", effectiveOVHS3Region(cfg))
}

func effectiveOVHS3Region(cfg settings) string {
	return strings.ToLower(strings.TrimSpace(cfg.Region))
}

func effectiveOVHAPIRegion(cfg settings) string {
	return strings.ToUpper(strings.TrimSpace(cfg.Region))
}

func normalizeOVHStoragePolicyRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin":
		return "admin"
	case "deny":
		return "deny"
	case "readonly":
		return "readOnly"
	case "readwrite":
		return "readWrite"
	default:
		return strings.TrimSpace(role)
	}
}

func validOVHStoragePolicyRole(role string) bool {
	switch role {
	case "admin", "deny", "readOnly", "readWrite":
		return true
	default:
		return false
	}
}
