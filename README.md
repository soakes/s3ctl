# s3ctl

[![Validate](https://img.shields.io/github/actions/workflow/status/soakes/s3ctl/build-and-validate.yml?branch=main&style=flat-square&label=validate)](https://github.com/soakes/s3ctl/actions/workflows/build-and-validate.yml)
[![Container](https://img.shields.io/github/actions/workflow/status/soakes/s3ctl/container-image.yml?branch=main&style=flat-square&label=container)](https://github.com/soakes/s3ctl/actions/workflows/container-image.yml)
[![Release](https://img.shields.io/github/v/release/soakes/s3ctl?sort=semver&style=flat-square)](https://github.com/soakes/s3ctl/releases)
[![GHCR](https://img.shields.io/badge/GHCR-published-2088FF?style=flat-square&logo=github)](https://ghcr.io/soakes/s3ctl)
[![Go](https://img.shields.io/badge/Go-1.26.2-00ADD8.svg?style=flat-square&logo=go&logoColor=white)](https://go.dev/)

`s3ctl` is a single-binary CLI for provisioning S3 buckets and automatically issuing bucket-scoped access credentials.

It is designed for the common operational workflow:

- create one or many buckets
- optionally enable versioning
- optionally apply a bucket policy from a built-in template or JSON file
- create a fresh access key and secret key for each bucket
- attach a generated policy so each credential only has access to its own bucket
- drive the same workflow from flags, environment variables, JSON config, or CSV batch input

## Quick Start

Build locally:

```bash
make build
./dist/s3ctl --help
```

Install the latest published binary:

```bash
curl -fsSL https://soakes.github.io/s3ctl/install.sh | sh
```

Plan a single bucket with generated scoped credentials:

```bash
./dist/s3ctl \
  --bucket app-data \
  --endpoint https://objects.example.com \
  --region us-east-1 \
  --create-scoped-credentials \
  --credential-policy-template bucket-readwrite \
  --dry-run
```

Plan multiple buckets from repeated flags:

```bash
./dist/s3ctl \
  --bucket app-data \
  --bucket logs-archive \
  --create-scoped-credentials \
  --dry-run \
  --output json
```

Plan a batch from CSV:

```bash
./dist/s3ctl \
  --batch-file ./examples/s3ctl-batch.csv \
  --endpoint https://objects.example.com \
  --region us-east-1 \
  --create-scoped-credentials \
  --dry-run \
  --output json
```

## Distribution

Published release channels are designed to cover the normal operator paths:

- GitHub release archives for `linux/amd64`, `linux/arm64`, `linux/arm/v7`, `darwin/amd64`, and `darwin/arm64`
- Debian `.deb` packages for `amd64`, `arm64`, and `armhf`
- a GitHub Pages release hub with install commands and release metadata
- a signed APT repository when archive signing secrets are configured
- a multi-arch GHCR image

Direct installer:

```bash
curl -fsSL https://soakes.github.io/s3ctl/install.sh | sh
```

Pinned installer run:

```bash
curl -fsSL https://soakes.github.io/s3ctl/install.sh | sh -s -- --version v1.2.3
```

Custom install location:

```bash
curl -fsSL https://soakes.github.io/s3ctl/install.sh | sh -s -- --install-dir /usr/local/bin
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

## Website Preview

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

## Batch Provisioning

For bulk runs, the normal pattern is:

1. Define the shared S3 and IAM settings once with flags, env vars, or config.
2. Feed the bucket list in with repeated `--bucket` flags or `--batch-file`.
3. Let `s3ctl` generate one IAM user and one access key pair per bucket.

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

## Configuration

Configuration is resolved in this order:

1. CLI flags
2. Environment variables
3. JSON config file
4. Built-in defaults

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
./dist/s3ctl --config ./examples/s3ctl.json --dry-run --output json
```

Relative paths inside the config file are resolved from the config file directory, so config-local batch files and policy documents work cleanly.

Default user config path:

- `$XDG_CONFIG_HOME/s3ctl/config.json`
- `$HOME/.config/s3ctl/config.json`

When `--config` and `S3CTL_CONFIG_FILE` are unset, `s3ctl` will automatically load that
default file if it exists. This is the right place for shared operator settings such as
endpoint, region, profile, credentials, IAM defaults, and output preferences.

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

Use either `profile` or explicit `access_key` and `secret_key` values, not both. Add
`session_token` when your master credentials are temporary. Prefer environment
variables for real secrets when possible; if you keep them in the default user config,
store that file outside the repository and restrict its permissions.

Install that as your per-user default:

```bash
install -d -m 700 "${XDG_CONFIG_HOME:-$HOME/.config}/s3ctl"
install -m 600 ./examples/user-config.json "${XDG_CONFIG_HOME:-$HOME/.config}/s3ctl/config.json"
```

## Environment Variables

Primary variables:

- `S3CTL_CONFIG_FILE`
- `S3CTL_BUCKET_NAME`
- `S3CTL_BUCKET_NAMES`
- `S3CTL_BATCH_FILE`
- `S3CTL_ENDPOINT_URL`
- `S3CTL_REGION`
- `S3CTL_PROFILE`
- `S3CTL_ACCESS_KEY_ID`
- `S3CTL_SECRET_ACCESS_KEY`
- `S3CTL_SESSION_TOKEN`
- `S3CTL_ENABLE_VERSIONING`
- `S3CTL_BUCKET_POLICY_FILE`
- `S3CTL_BUCKET_POLICY_TEMPLATE`
- `S3CTL_CREATE_SCOPED_CREDENTIALS`
- `S3CTL_IAM_ENDPOINT_URL`
- `S3CTL_IAM_USER_NAME`
- `S3CTL_IAM_USER_PREFIX`
- `S3CTL_IAM_PATH`
- `S3CTL_CREDENTIAL_POLICY_TEMPLATE`
- `S3CTL_OUTPUT_FORMAT`
- `S3CTL_DRY_RUN`

AWS-standard variables such as `AWS_PROFILE`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_REGION`, and `AWS_DEFAULT_REGION` are also supported where appropriate.

## Built-In Templates

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

By default, generated scoped credentials use `bucket-readwrite`, generated IAM user names are derived directly from bucket names, and no IAM path is set. Configure `iam_user_prefix`, `--iam-user-prefix`, or `S3CTL_IAM_USER_PREFIX` when generated user names should share a prefix. Configure `iam_path`, `--iam-path`, or `S3CTL_IAM_PATH` when generated users should be created under an IAM path.

## IAM Notes

Scoped credential provisioning uses the IAM API in addition to the S3 API. The principal running `s3ctl` therefore needs permission to:

- create buckets and apply bucket configuration in S3
- create IAM users
- attach inline IAM policies
- create IAM access keys

AWS IAM is the default target. When you need an IAM-compatible alternative, use `--iam-endpoint` or `S3CTL_IAM_ENDPOINT_URL`.

## Container

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

## Maintenance Automation

Repository maintenance is automated so the pinned toolchain and delivery inputs do not drift:

- Dependabot opens daily pull requests for Go modules, GitHub Actions dependencies, and Docker inputs.
- `go.mod` carries the preferred Go toolchain, and CI reads it directly with `actions/setup-go`.
- `golangci-lint` is pinned in `.golangci-lint-version` so local lint runs and CI enforce the same formatter and linter behavior.
- A scheduled workflow tracks the latest stable Go release from `go.dev` and the latest `golangci-lint` release and opens a pull request when those pins need to move forward.
- After `Build and Validate` succeeds, eligible Dependabot pull requests are approved and squash-merged automatically.

The GitHub Pages release hub is also generated from the latest stable release metadata so operators get copy-ready install commands, checksum links, release assets, and signed APT details from one place.

## Development

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

## Release Automation

This repository includes:

- validation CI for formatting, vetting, tests, build output, and CLI smoke checks
- multi-arch container validation and GHCR publishing
- tag-driven release builds for `linux/amd64`, `linux/arm64`, `linux/arm/v7`, `darwin/amd64`, and `darwin/arm64`
- Debian package publication for `amd64`, `arm64`, and `armhf`
- GitHub Pages release site publication with installer metadata
- signed APT repository publication when signing secrets are configured
- release drafter, labels, and dependency automation
