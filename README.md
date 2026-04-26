# 🪣 s3ctl

> A single-binary CLI for creating S3-compatible buckets and issuing bucket-scoped credentials.

[![Validate](https://img.shields.io/github/actions/workflow/status/soakes/s3ctl/build-and-validate.yml?branch=main&style=flat-square&label=validate)](https://github.com/soakes/s3ctl/actions/workflows/build-and-validate.yml)
[![Container](https://img.shields.io/github/actions/workflow/status/soakes/s3ctl/container-image.yml?branch=main&style=flat-square&label=container)](https://github.com/soakes/s3ctl/actions/workflows/container-image.yml)
[![Release](https://img.shields.io/github/v/release/soakes/s3ctl?sort=semver&style=flat-square)](https://github.com/soakes/s3ctl/releases)
[![APT Repository](https://img.shields.io/badge/APT-signed%20repo-A81D33?style=flat-square&logo=debian&logoColor=white)](https://soakes.github.io/s3ctl/)
[![GHCR](https://img.shields.io/badge/GHCR-published-2088FF?style=flat-square&logo=github)](https://ghcr.io/soakes/s3ctl)
[![Go](https://img.shields.io/badge/Go-1.26.2-00ADD8.svg?style=flat-square&logo=go&logoColor=white)](https://go.dev/)

Built for operators who want bucket provisioning to be predictable,
scriptable, and easy to hand to automation: create buckets, generate scoped
credentials, rotate keys, delete empty buckets safely, and publish the tool
through the same release paths every time.

**Quick links:** [📦 Releases](https://github.com/soakes/s3ctl/releases) · [🐳 GHCR](https://ghcr.io/soakes/s3ctl) · [🔐 Release Hub / APT](https://soakes.github.io/s3ctl/) · [🧰 Examples](examples)

## 🧭 Table of Contents

- [📖 Overview](#overview)
- [✨ Capabilities](#capabilities)
- [🚀 Quick Start](#quick-start)
- [📦 Distribution](#distribution)
- [🖥️ Website Preview](#website-preview)
- [🗃️ Batch Provisioning](#batch-provisioning)
- [⚙️ Configuration](#configuration)
- [🧩 Built-In Templates](#built-in-templates)
- [🔑 IAM Notes](#iam-notes)
- [🧹 Deleting Buckets](#deleting-buckets)
- [☁️ OVHcloud Notes](#ovhcloud-notes)
- [🐳 Container](#container)
- [🤖 Maintenance Automation](#maintenance-automation)
- [🛠️ Development](#development)
- [🚢 Release Automation](#release-automation)

---

## 📖 Overview

`s3ctl` provisions S3-compatible buckets and automatically issues
bucket-scoped access credentials. It can work with a normal S3/IAM-compatible
provider, or with OVHcloud Public Cloud Object Storage where buckets are exposed
as S3-compatible containers.

It is designed for the common operational workflow:

- create one or many buckets
- optionally enable versioning
- optionally apply a bucket policy from a built-in template or JSON file
- create a fresh access key and secret key for each bucket
- attach a generated policy so each credential only has access to its own bucket
- rotate existing OVHcloud S3 credentials by bucket name
- delete empty buckets safely, or delete non-empty buckets with an explicit force guard
- drive the same workflow from flags, JSON config, or CSV batch input

### First Bucket Checklist

1. Put shared provider settings in `~/.config/s3ctl/config.json`.
2. Run `s3ctl --bucket app-data --dry-run --output json`.
3. Confirm the endpoint, region, and credential scope in the plan.
4. Run `s3ctl --bucket app-data --output json`.
5. Store the returned access key and secret securely; secrets are only printed once.

---

## ✨ Capabilities

- **Bucket provisioning**: creates one bucket, many buckets, or CSV-driven batches
- **Scoped credentials**: creates bucket-specific IAM-style users and access keys
- **OVHcloud support**: creates containers, Public Cloud users, S3 keys, policies, and optional encryption
- **Credential rotation**: rotates OVHcloud S3 keypairs by bucket/user name
- **Safe deletion**: deletes empty buckets without `--force` and requires `--force` for non-empty buckets
- **Automation output**: emits JSON success and error payloads for machine workflows
- **Distribution**: publishes release archives, Debian packages, a signed APT repository, and GHCR images
- **Release hygiene**: validates release candidates before promoting the same commit to stable

---

## 🚀 Quick Start

Build locally:

```bash
make build
./dist/s3ctl --help
```

`s3ctl --help` is a short operator quick reference. Use `s3ctl --help-full`
when you need the complete flag, template, and batch CSV reference.

Install the latest published binary:

```bash
curl -fsSL https://soakes.github.io/s3ctl/install.sh | bash
```

On macOS, use the installer instead of manually unpacking the release archive.
The published macOS binaries are not Apple-notarized yet, so manually extracted
downloads may be blocked by Gatekeeper unless the quarantine marker is removed.
The installer handles that step after placing the binary in a user-owned bin
directory.

Plan a single bucket with generated scoped credentials:

```bash
s3ctl \
  --bucket app-data \
  --endpoint https://objects.example.com \
  --region us-east-1 \
  --create-scoped-credentials \
  --credential-policy-template bucket-readwrite \
  --dry-run
```

Provision an OVHcloud Object Storage container and a dedicated S3 key:

```bash
s3ctl \
  --provider ovh \
  --bucket app-data \
  --region UK \
  --ovh-service-name PUBLIC_CLOUD_PROJECT_ID \
  --output json
```

Rotate an existing OVHcloud bucket keypair:

```bash
s3ctl \
  --provider ovh \
  --bucket app-data \
  --ovh-rotate-credentials \
  --output json
```

Preview a bucket delete:

```bash
s3ctl \
  --provider ovh \
  --bucket app-data \
  --delete \
  --dry-run
```

Delete an empty bucket after checking the dry-run output:

```bash
s3ctl \
  --provider ovh \
  --bucket app-data \
  --delete
```

Delete a non-empty bucket after checking the dry-run output:

```bash
s3ctl \
  --provider ovh \
  --bucket app-data \
  --delete \
  --force
```

Show focused bucket workflow help:

```bash
s3ctl --bucket app-data --help
```

Show the full CLI reference:

```bash
s3ctl --help-full
```

Plan multiple buckets from repeated flags:

```bash
s3ctl \
  --bucket app-data \
  --bucket logs-archive \
  --create-scoped-credentials \
  --dry-run \
  --output json
```

Plan a batch from CSV:

```bash
s3ctl \
  --batch-file ./examples/s3ctl-batch.csv \
  --endpoint https://objects.example.com \
  --region us-east-1 \
  --create-scoped-credentials \
  --dry-run \
  --output json
```

---

## 📦 Distribution

Published release channels are designed to cover the normal operator paths:

- GitHub release archives for `linux/amd64`, `linux/arm64`, `linux/arm/v7`, `darwin/amd64`, and `darwin/arm64`
- Debian `.deb` packages for `amd64`, `arm64`, and `armhf`
- a GitHub Pages release hub with install commands and release metadata
- a signed APT repository when archive signing secrets are configured
- a multi-arch GHCR image

Release candidates use tags like `v1.2.3-rc.1`. They publish the same GitHub
release assets and a GHCR `:rc` image for validation, but they do not move the
stable APT channel or the `:latest` container tag.

Direct installer, recommended for macOS:

```bash
curl -fsSL https://soakes.github.io/s3ctl/install.sh | bash
```

On macOS, install via this script unless you specifically need to handle the
archive yourself. The installer defaults to a user-owned bin directory, prefers
an existing home bin path already present in `PATH` such as `$HOME/.local/bin`,
`$HOME/bin`, or `$HOME/.bin`, and otherwise uses `$HOME/.local/bin` with a PATH
hint. It also clears the macOS download quarantine marker from the installed
binary.

If you download and extract a macOS archive manually, Finder may block the binary
because the release is not Apple-notarized yet. Prefer the installer, or clear
the quarantine marker yourself after verifying the checksum:

```bash
xattr -d com.apple.quarantine ./s3ctl-darwin-arm64
```

Pinned installer run:

```bash
curl -fsSL https://soakes.github.io/s3ctl/install.sh | bash -s -- --version v1.2.3
```

Custom install location:

```bash
curl -fsSL https://soakes.github.io/s3ctl/install.sh | bash -s -- --install-dir "$HOME/.local/bin"
```

Direct Debian package:

```bash
curl -fsSLO https://github.com/soakes/s3ctl/releases/latest/download/s3ctl_1.2.3_amd64.deb
sudo apt install ./s3ctl_1.2.3_amd64.deb
```

Signed APT repository:

```bash
sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://soakes.github.io/s3ctl/apt/s3ctl-archive-keyring.gpg \
  | sudo tee /etc/apt/keyrings/s3ctl-archive-keyring.gpg >/dev/null

sudo tee /etc/apt/sources.list.d/s3ctl.sources >/dev/null <<'EOF'
Types: deb
URIs: https://soakes.github.io/s3ctl/apt/
Suites: stable
Components: main
Signed-By: /etc/apt/keyrings/s3ctl-archive-keyring.gpg
EOF

sudo apt update && sudo apt install s3ctl
```

The Pages site and APT repository are published by workflow. The APT path requires the
repository secrets `APT_GPG_PRIVATE_KEY`, `APT_GPG_KEY_ID`, and optionally
`APT_GPG_PASSPHRASE` so the repository metadata can be signed for apt-secure.

---

## 🖥️ Website Preview

Render the release hub locally with real browser screenshots:

```bash
make website-install
make website-check
make website-build
make website-capture
```

Desktop and mobile captures are written to `website/.captures/`.
The website is built with Vite and the local preview flow falls back to
`website/preview-metadata.json` when generated release metadata is not present yet.

---

## 🗃️ Batch Provisioning

For bulk runs, the normal pattern is:

1. Define the shared provider settings once with flags or config.
2. Feed the bucket list in with repeated `--bucket` flags or `--batch-file`.
3. Let `s3ctl` generate one scoped user and one access key pair per bucket.

Supported batch CSV columns:

- `bucket`
- `iam_user_name`
- `enable_versioning`
- `bucket_policy_file`
- `bucket_policy_template`
- `create_scoped_credentials`
- `credential_policy_template`

Example CSV:

```csv
bucket,create_scoped_credentials,credential_policy_template,enable_versioning
app-data,true,bucket-readwrite,true
logs-archive,true,bucket-readonly,false
```

---

## ⚙️ Configuration

Configuration is resolved in this order:

1. CLI flags
2. JSON config file
3. Built-in defaults

Example config:

```json
{
  "endpoint": "https://objects.example.com",
  "region": "us-east-1",
  "enable_versioning": true,
  "create_scoped_credentials": true,
  "credential_policy_template": "bucket-readwrite",
  "bucket_policy_template": "deny-insecure-transport",
  "batch_file": "./s3ctl-batch.csv"
}
```

Run it:

```bash
s3ctl --config ./examples/s3ctl.json --dry-run --output json
```

When `--output json` or `"output": "json"` is set, command failures are also
written to stdout as JSON. The process still exits non-zero, but automation can
read the `error.code`, `error.message`, and
optional `error.detail` fields instead of scraping text:

```json
{
  "operation": "delete",
  "dry_run": false,
  "config_file": "/home/operator/.config/s3ctl/config.json",
  "resource_count": 1,
  "error": {
    "code": "not_found",
    "message": "OVH bucket/container \"app-data\" does not exist in region \"UK\"; nothing was deleted",
    "detail": "OVHcloud API error ..."
  }
}
```

Example OVHcloud config with OAuth2 service account credentials:

```json
{
  "provider": "ovh",
  "ovh_service_name": "PUBLIC_CLOUD_PROJECT_ID",
  "ovh_client_id": "CLIENT_ID",
  "ovh_client_secret": "CLIENT_SECRET",
  "region": "UK",
  "enable_versioning": true,
  "ovh_encrypt_data": true,
  "ovh_storage_policy_role": "readWrite",
  "output": "json"
}
```

Classic OVH API application credentials are also supported:

```json
{
  "provider": "ovh",
  "ovh_service_name": "PROJECT_ID",
  "ovh_application_key": "APPLICATION_KEY",
  "ovh_application_secret": "APPLICATION_SECRET",
  "ovh_consumer_key": "CONSUMER_KEY",
  "region": "GRA"
}
```

With that saved in your default config, this is enough:

```bash
s3ctl --bucket app-data
```

Relative paths inside the config file are resolved from the config file directory, so config-local batch files and policy documents work cleanly.

Default user config path:

- `$XDG_CONFIG_HOME/s3ctl/config.json`
- `$HOME/.config/s3ctl/config.json`

When `--config` is unset, `s3ctl` will automatically load that default file if
it exists. This is the right place for shared operator settings such as
provider, endpoint, region, profile, credentials, IAM/OVH defaults, and output
preferences.

Example default user config:

```json
{
  "endpoint": "https://objects.example.com",
  "region": "us-east-1",
  "access_key": "MASTER_ACCESS_KEY_ID",
  "secret_key": "MASTER_SECRET_ACCESS_KEY",
  "create_scoped_credentials": true,
  "credential_policy_template": "bucket-readwrite"
}
```

Use either `profile` or explicit `access_key` and `secret_key` values, not both.
Add `session_token` when your master credentials are temporary. If those values
are not set in `s3ctl`, the AWS SDK still uses its normal credential and profile
discovery. If you keep secrets in the default user config, store that file
outside the repository and restrict its permissions.

Install that as your per-user default:

```bash
install -d -m 700 "${XDG_CONFIG_HOME:-$HOME/.config}/s3ctl"
install -m 600 ./examples/user-config.json "${XDG_CONFIG_HOME:-$HOME/.config}/s3ctl/config.json"
```

---

## 🧩 Built-In Templates

Bucket policy templates:

| Template | Coverage |
| --- | --- |
| `deny-insecure-transport` | Denies all S3 actions against the bucket and objects when requests do not use secure transport. |
| `public-read` | Allows public `s3:GetObject` access to objects in the bucket. |

Scoped credential policy templates:

| Template | Coverage |
| --- | --- |
| `bucket-readonly` | Allows bucket location lookup, bucket listing, and object reads for one bucket. |
| `bucket-readwrite` | Allows bucket location lookup, bucket listing, object reads, writes, deletes, and multipart upload operations for one bucket. |
| `bucket-admin` | Allows all S3 actions against one bucket and its objects. |

By default, generated scoped credentials use `bucket-readwrite`, generated IAM
user names are derived directly from bucket names, and no IAM path is set.
Configure `iam_user_prefix` or `--iam-user-prefix` when generated user names
should share a prefix. Configure `iam_path` or `--iam-path` when generated
users should be created under an IAM path.

---

## 🔑 IAM Notes

Scoped credential provisioning uses the IAM API in addition to the S3 API. The principal running `s3ctl` therefore needs permission to:

- create buckets and apply bucket configuration in S3
- create IAM users
- attach inline IAM policies
- create IAM access keys

AWS IAM is the default target. When you need an IAM-compatible alternative, use
`--iam-endpoint` or `iam_endpoint` in JSON config.

---

## 🧹 Deleting Buckets

Use `--delete` with one or more `--bucket` values to remove buckets instead of
creating them. Empty buckets can be deleted without `--force`. Non-empty
buckets require `--force`; without it, `s3ctl` lists the bucket contents and
refuses to delete the bucket if objects, object versions, or delete markers are
present. Use `--dry-run` to preview the target.

```bash
s3ctl --bucket app-data --delete --dry-run
s3ctl --bucket app-data --delete
s3ctl --bucket app-data --delete --force --timeout 30m
```

Without `--force`, the S3 provider only lists object versions, delete markers,
and current objects to confirm the bucket is empty before deleting it. With
`--force`, it deletes object versions and delete markers when the endpoint
supports version listing, deletes any remaining current objects, and finally
deletes the bucket.
The S3 principal running the delete needs the matching list, object delete,
object version delete, and bucket delete permissions.

JSON config can also drive this mode:

```json
{
  "bucket": "app-data",
  "delete_bucket": true
}
```

The shorter `"delete": true` config key is accepted as an alias for
`"delete_bucket": true`.

Keep `"force": true` out of shared default configs unless every run using that
config should be allowed to remove bucket contents before deleting buckets.

Use `--timeout` or `"timeout": "30m"` for large buckets or slower
object-storage endpoints. The default timeout is `10m`.

---

## ☁️ OVHcloud Notes

Use `--provider ovh` to create OVHcloud Object Storage through the Public Cloud
API. OVHcloud calls buckets "containers"; `s3ctl` keeps the CLI wording as
bucket because the resulting credentials are S3-compatible.

The OVHcloud provider creates one Public Cloud user and one S3 credential pair
per bucket, creates the container in `--region`, and attaches the user to that
container with `--ovh-storage-policy-role` (`readWrite` by default). It does not
apply S3 bucket policy documents; access is controlled through OVHcloud
container policies.

Required OVHcloud settings:

- `provider`: `ovh`
- `ovh_service_name`: the Public Cloud project ID/service name
- one OVHcloud auth mode: OAuth2 service account credentials, an access token,
  classic OVH API application credentials, or standard go-ovh client discovery
  such as `ovh.conf`
- `region`: an OVHcloud Public Cloud/Object Storage region such as `UK`, `GRA`, `BHS`, `SBG`, or `EU-WEST-PAR`.
  Use the uppercase region returned by OVHcloud's Public Cloud API. `s3ctl`
  also accepts lowercase S3 endpoint regions such as `uk` and normalizes them
  for OVHcloud API calls.

Optional OVHcloud settings:

- `ovh_api_endpoint`: endpoint name such as `ovh-eu`, `ovh-ca`, `ovh-us`, or a custom API URL
- `ovh_client_id` and `ovh_client_secret`: OAuth2 service account credentials
- `ovh_access_token`: short-lived OVHcloud access token
- `ovh_application_key`, `ovh_application_secret`, and `ovh_consumer_key`: classic OVH API application credentials
- `ovh_s3_endpoint`: override the returned S3 endpoint when the default
  `https://s3.<region>.io.cloud.ovh.net` form is not right for your project
- `ovh_user_role`: defaults to `objectstore_operator`
- `ovh_storage_policy_role`: one of `admin`, `deny`, `readOnly`, or `readWrite`
- `ovh_encrypt_data`: set to `true` to enable OVHcloud server-side encryption
  with OVH-managed keys (`AES256` / SSE-OMK). When explicitly set to `false`,
  `s3ctl` requests OVHcloud `plaintext` container storage.
- `ovh_tags`: optional tags to apply to new OVHcloud containers. `s3ctl` does
  not add tags by default. Use JSON config such as
  `"ovh_tags": {"environment": "prod", "owner": "platform"}`, repeat
  `--ovh-tag environment=prod --ovh-tag owner=platform`.
- `ovh_rotate_credentials`: set to `true` to rotate S3 credentials for the
  existing OVHcloud container owner instead of creating a new container. Keep it
  out of the normal provisioning config unless every run should be a rotation.

### 🔐 OVHcloud OAuth2 and IAM Setup

Create the OAuth2 service account first. The official `ovhcloud` CLI is the
cleanest route:

Install the CLI from OVHcloud's official guide:
`https://help.ovhcloud.com/csm/en-cli-getting-started?id=kb_article_view&sysparm_article=KB0072704`

```bash
brew install --cask ovh/tap/ovhcloud-cli
```

Without Homebrew:

```bash
curl -fsSL https://raw.githubusercontent.com/ovh/ovhcloud-cli/main/install.sh | sh
```

Authenticate it with your OVHcloud account:

```bash
ovhcloud login
```

Then create the service account credentials for `s3ctl`:

```bash
ovhcloud account api oauth2 client create \
  --name "s3ctl" \
  --description "s3ctl bucket provisioning" \
  --flow "CLIENT_CREDENTIALS"
```

OVHcloud returns a `clientId` and `clientSecret`; use those as `ovh_client_id`
and `ovh_client_secret` in the `s3ctl` config.

You can also create the OAuth2 client from the OVHcloud API console. Open the
console for your account region, go to `POST /me/api/oauth2/client`, and submit
this body:

- EU: `https://eu.api.ovh.com/console/?branch=v1&section=%2Fme`
- CA: `https://ca.api.ovh.com/console/?branch=v1&section=%2Fme`
- US: `https://api.us.ovhcloud.com/console/?branch=v1&section=%2Fme`

```json
{
  "callbackUrls": [],
  "flow": "CLIENT_CREDENTIALS",
  "name": "s3ctl",
  "description": "s3ctl bucket provisioning"
}
```

Next, grant that service account access to the Public Cloud project. The service
account cannot grant access to itself; use the OVHcloud account/admin user or an
existing identity with IAM administration rights.

In OVHcloud Manager:

1. Open **Identity, Security & Operations**.
2. Open **Policies**.
3. Create a policy named `s3ctl-object-storage`.
4. Under **Identities**, select the `s3ctl` service account.
5. Under **Product types**, select **Public Cloud Project**.
6. Under **Resources**, select the project long ID shown under the project name,
   for example `51ab2732562648349de40f72ac51c1c8`. Use this same value as
   `ovh_service_name`; do not use the display name.
7. For the first smoke test, authorise all actions on that selected project
   resource. After confirming it works, tighten the policy to the actions below.

Least-privilege actions for `s3ctl`:

- `publicCloudProject:apiovh:get`
- `publicCloudProject:apiovh:user/create`
- `publicCloudProject:apiovh:user/delete`
- `publicCloudProject:apiovh:user/get`
- `publicCloudProject:apiovh:user/s3Credentials/create`
- `publicCloudProject:apiovh:user/s3Credentials/delete`
- `publicCloudProject:apiovh:user/s3Credentials/get`
- `publicCloudProject:apiovh:region/storage/create`
- `publicCloudProject:apiovh:region/storage/delete`
- `publicCloudProject:apiovh:region/storage/edit`
- `publicCloudProject:apiovh:region/storage/get`
- `publicCloudProject:apiovh:region/storage/policy/create`

The policy body in `examples/ovh-iam-policy.json` is a starting point for the
API route, `POST /iam/policy`. Get the service account identity URN from
`GET /me/api/oauth2/client/{clientId}`. OVHcloud documents the format as
`urn:v1:<eu|ca>:identity:credential:<account-id>/oauth2-<clientId>`. Get the
project resource URN from `GET /iam/resource` by selecting the
`publicCloudProject` resource matching your Public Cloud project ID.

Verify the policy before running `s3ctl`. With the same OAuth2 credentials,
`GET /cloud/project` should list the project ID:

```bash
token="$(curl -fsS \
  -d grant_type=client_credentials \
  --data-urlencode "client_id=$OVH_CLIENT_ID" \
  --data-urlencode "client_secret=$OVH_CLIENT_SECRET" \
  -d scope=all \
  https://www.ovh.com/auth/oauth2/token | jq -r .access_token)"

curl -fsS -H "Authorization: Bearer $token" \
  https://eu.api.ovh.com/1.0/cloud/project | jq .
```

Expected output should include the Public Cloud project ID:

```json
[
  "51ab2732562648349de40f72ac51c1c8"
]
```

If OVHcloud returns `This service does not exist` while the project ID is
correct, the service account usually cannot see the project yet. Recheck the IAM
policy identity, resource, and actions.

### 🔄 OVHcloud Credential Rotation

Use `--ovh-rotate-credentials` or `"ovh_rotate_credentials": true` when a bucket
already exists and you only want a fresh S3 access key and secret:

```bash
s3ctl --provider ovh --bucket app-data --ovh-rotate-credentials --output json
```

If using JSON config for a rotation run:

```json
{
  "provider": "ovh",
  "ovh_service_name": "PUBLIC_CLOUD_PROJECT_ID",
  "ovh_client_id": "CLIENT_ID",
  "ovh_client_secret": "CLIENT_SECRET",
  "region": "UK",
  "ovh_rotate_credentials": true,
  "output": "json"
}
```

Rotation looks up the existing container by bucket name, reads its `ownerId`,
creates a new S3 credential pair for that OVH Public Cloud user, then deletes
the previous S3 credentials for that user. The new secret is only returned once,
so store the command output securely. If an old key cannot be deleted after the
new key is created, `s3ctl` still prints the new credentials and includes a
warning so the stale key can be removed manually.

### 🗑️ OVHcloud Bucket Deletion

OVHcloud buckets are containers, but the delete command still uses the bucket
name:

```bash
s3ctl --provider ovh --bucket app-data --delete
```

For OVHcloud deletes, `s3ctl` looks up the container owner, creates a temporary
S3 credential for that OVH Public Cloud user, and checks whether the container
is empty through the S3-compatible API. Empty containers are deleted without
`--force`. Non-empty containers require `--force`, which allows `s3ctl` to empty
the container through the S3-compatible API before deleting it through the
OVHcloud Public Cloud API. After the container is removed, `s3ctl` deletes the
user's S3 credentials and the OVH Public Cloud user. If the container is removed
but a final credential/user cleanup call fails, the command prints a warning so
the stale identity can be removed manually.

For safety, OVHcloud delete and credential rotation only continue when the
container owner looks bucket-dedicated: the OVH Public Cloud user description
or username must match the bucket name, or the legacy description
`s3ctl bucket <bucket>`. This prevents deleting all credentials from a shared
manual OVH user.

The application key, application secret, and consumer key flow is still
supported as OVHcloud's classic API authentication path and can be used directly
with `s3ctl` as well.

For classic OVH API application credentials, use OVHcloud's token creation
page. These links pre-fill the API rights `s3ctl` needs for Public Cloud bucket
provisioning, but they do not create OAuth2 service account credentials:

- EU: `https://eu.api.ovh.com/createToken/?GET=%2Fcloud%2Fproject%2F%2A&POST=%2Fcloud%2Fproject%2F%2A&DELETE=%2Fcloud%2Fproject%2F%2A`
- CA: `https://ca.api.ovh.com/createToken/?GET=%2Fcloud%2Fproject%2F%2A&POST=%2Fcloud%2Fproject%2F%2A&DELETE=%2Fcloud%2Fproject%2F%2A`
- US: `https://api.us.ovhcloud.com/createToken/?GET=%2Fcloud%2Fproject%2F%2A&POST=%2Fcloud%2Fproject%2F%2A&DELETE=%2Fcloud%2Fproject%2F%2A`

After creating the token, paste the returned application key, application
secret, and consumer key into `ovh_application_key`, `ovh_application_secret`,
and `ovh_consumer_key`. To create `ovh_client_id` and `ovh_client_secret`,
use `POST /me/api/oauth2/client` instead.

---

## 🐳 Container

Build locally:

```bash
make docker-build
docker run --rm s3ctl:dev --help
```

Use the published image:

```bash
docker run --rm ghcr.io/soakes/s3ctl:latest --help
```

Run against the bundled examples from the host:

```bash
docker run --rm \
  -v "$PWD/examples:/examples:ro" \
  ghcr.io/soakes/s3ctl:latest \
  --config /examples/s3ctl.json \
  --dry-run \
  --output json
```

---

## 🤖 Maintenance Automation

Repository maintenance is automated so the pinned toolchain and delivery inputs do not drift:

- Dependabot opens daily pull requests for Go modules, GitHub Actions dependencies, and Docker inputs.
- `go.mod` carries the preferred Go toolchain, and CI reads it directly with `actions/setup-go`.
- `golangci-lint` is pinned in `.golangci-lint-version` so local lint runs and CI enforce the same formatter and linter behavior.
- A scheduled workflow tracks the latest stable Go release from `go.dev` and the latest `golangci-lint` release and opens a pull request when those pins need to move forward.
- After `Build and Validate` succeeds, eligible Dependabot pull requests are approved and squash-merged automatically.

The GitHub Pages release hub is also generated from the latest stable release metadata so operators get copy-ready install commands, checksum links, release assets, and signed APT details from one place.

---

## 🛠️ Development

Common targets:

```bash
make lint-install
make fmt
make lint
make vet
make test
make build
make refresh-go-toolchain
make build-release
make package-deb BINARY_PATH=dist/s3ctl-linux-amd64 DEB_ARCH=amd64
```

Recommended Go quality workflow:

```bash
make lint-install
make fmt
make lint
make vet
make test
make build
```

`gofmt` remains the Go baseline. For the stricter, more product-grade equivalent of `black` plus `ruff` plus `pylint`, this repository standardises on pinned `golangci-lint` with `gofumpt`, `goimports`, `staticcheck`, `errcheck`, and `revive`.

Website validation follows the same principle: the Vite site is checked and built in CI so the published Pages artifact matches the local release hub preview flow.

`make build-release` produces release archives in `dist/release/` for:

- `linux/amd64`
- `linux/arm64`
- `linux/arm/v7`
- `darwin/amd64`
- `darwin/arm64`

The Linux binaries are built with `CGO_ENABLED=0`, so releases are architecture-specific rather than distro-specific and should run across most mainstream distributions for the same CPU family.

---

## 🚢 Release Automation

This repository includes:

- validation CI for formatting, vetting, tests, build output, and CLI smoke checks
- automated release-candidate creation after successful `main` validation
- manual promotion from a validated `vX.Y.Z-rc.N` tag to the matching stable tag
- multi-arch container validation and GHCR publishing
- tag-driven release builds for `linux/amd64`, `linux/arm64`, `linux/arm/v7`, `darwin/amd64`, and `darwin/arm64`
- Debian package publication for `amd64`, `arm64`, and `armhf`
- GitHub Pages release site publication with installer metadata
- signed APT repository publication when signing secrets are configured
- release drafter, labels, and dependency automation

Release tags are calculated from conventional commit subjects by
`scripts/next-release.sh`. `feat:` creates a minor release, breaking changes
create a major release, and `fix:`, `perf:`, `deps:`, `packaging:`,
`container:`, or `release:` create a patch release. Documentation, CI, tests,
and maintenance commits can still appear in release notes when they ship with
operator-facing work, but they do not create a release by themselves.

The automated release workflow requires a `RELEASE_AUTOMATION_TOKEN` repository
secret with permission to push tags and trigger workflows. Without it, the
workflow records a skip summary instead of creating tags. The APT repository
path also requires `APT_GPG_PRIVATE_KEY`, `APT_GPG_KEY_ID`, and optionally
`APT_GPG_PASSPHRASE`.
