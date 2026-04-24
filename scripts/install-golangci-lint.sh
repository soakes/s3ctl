#!/usr/bin/env bash
set -euo pipefail

repo_root="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
version="${1:-$(tr -d '[:space:]' < "${repo_root}/.golangci-lint-version")}"
bin_dir="${2:-${repo_root}/bin}"

if [ -z "${version}" ]; then
  printf 'golangci-lint version is required\n' >&2
  exit 1
fi

temp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${temp_dir}"
}
trap cleanup EXIT

install_script="${temp_dir}/install.sh"

if command -v curl >/dev/null 2>&1; then
  curl -fsSL https://golangci-lint.run/install.sh -o "${install_script}"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "${install_script}" https://golangci-lint.run/install.sh
else
  printf 'either curl or wget is required to install golangci-lint\n' >&2
  exit 1
fi

mkdir -p "${bin_dir}"
sh "${install_script}" -b "${bin_dir}" "${version}"
