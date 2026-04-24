#!/usr/bin/env bash
set -euo pipefail

repo_root="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

requested_version="${GO_TOOLCHAIN_VERSION:-}"
if [ -z "${requested_version}" ] && [ "${1:-}" != "" ]; then
  requested_version="$1"
fi

if [ -z "${requested_version}" ]; then
  requested_version="$(curl -fsSL 'https://go.dev/VERSION?m=text' | awk 'NR == 1 { print; exit }')"
fi

requested_version="${requested_version#go}"

if ! printf '%s\n' "${requested_version}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  printf 'invalid Go toolchain version: %s\n' "${requested_version}" >&2
  exit 1
fi

language_version="$(printf '%s\n' "${requested_version}" | awk -F. '{printf "%s.%s.0\n", $1, $2}')"

go mod edit -go="${language_version}" -toolchain="go${requested_version}"

perl -0pi -e 's/^ARG GO_VERSION=.*/ARG GO_VERSION='"${requested_version}"'/m' Dockerfile
perl -0pi -e 's#(\[!\[Go\]\(https://img\.shields\.io/badge/Go-)[^-]+(-00ADD8\.svg\?style=flat-square&logo=go&logoColor=white\)\]\(https://go\.dev/\))#${1}'"${requested_version}"'${2}#' README.md
