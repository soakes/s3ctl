#!/usr/bin/env bash
set -euo pipefail

repo="${S3CTL_INSTALL_REPO:-soakes/s3ctl}"
project="${S3CTL_INSTALL_PROJECT:-s3ctl}"
binary_name="${S3CTL_INSTALL_BINARY_NAME:-s3ctl}"
install_dir="${INSTALL_DIR:-}"
install_dir_explicit=false
version="${VERSION:-latest}"

if [ -n "${INSTALL_DIR:-}" ]; then
  install_dir_explicit=true
fi

usage() {
  cat <<EOF
Usage: install.sh [options]

Install the published s3ctl binary for the current platform.

Options:
  --version <tag|latest>      Release version to install (default: latest)
  --install-dir <path>        Destination directory for the binary
                               (default: macOS home bin dir, otherwise /usr/local/bin)
  --binary-name <name>        Installed binary name (default: s3ctl)
  --repo <owner/repo>         GitHub repository to install from (default: soakes/s3ctl)
  --project <name>            Release asset name prefix (default: s3ctl)
  -h, --help                  Show this help text

Environment overrides:
  VERSION
  INSTALL_DIR
  S3CTL_INSTALL_BINARY_NAME
  S3CTL_INSTALL_REPO
  S3CTL_INSTALL_PROJECT
EOF
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --version)
        [ "$#" -ge 2 ] || {
          printf 'missing value for %s\n' "$1" >&2
          exit 1
        }
        version="$2"
        shift 2
        ;;
      --install-dir)
        [ "$#" -ge 2 ] || {
          printf 'missing value for %s\n' "$1" >&2
          exit 1
        }
        install_dir="$2"
        [ -n "${install_dir}" ] || {
          printf 'empty value for %s\n' "$1" >&2
          exit 1
        }
        install_dir_explicit=true
        shift 2
        ;;
      --binary-name)
        [ "$#" -ge 2 ] || {
          printf 'missing value for %s\n' "$1" >&2
          exit 1
        }
        binary_name="$2"
        shift 2
        ;;
      --repo)
        [ "$#" -ge 2 ] || {
          printf 'missing value for %s\n' "$1" >&2
          exit 1
        }
        repo="$2"
        shift 2
        ;;
      --project)
        [ "$#" -ge 2 ] || {
          printf 'missing value for %s\n' "$1" >&2
          exit 1
        }
        project="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      --)
        shift
        break
        ;;
      *)
        printf 'unknown option: %s\n\n' "$1" >&2
        usage >&2
        exit 1
        ;;
    esac
  done

  if [ "$#" -gt 0 ]; then
    printf 'unexpected positional arguments: %s\n\n' "$*" >&2
    usage >&2
    exit 1
  fi
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'missing required command: %s\n' "$1" >&2
    exit 1
  }
}

detect_os() {
  case "$(uname -s)" in
    Linux) printf 'linux\n' ;;
    Darwin) printf 'darwin\n' ;;
    *)
      printf 'unsupported operating system: %s\n' "$(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    armv7l|armv7|armhf) printf 'armv7\n' ;;
    *)
      printf 'unsupported architecture: %s\n' "$(uname -m)" >&2
      exit 1
      ;;
  esac
}

path_contains_dir() {
  local dir="$1"
  case ":${PATH:-}:" in
    *:"${dir}":*) return 0 ;;
    *) return 1 ;;
  esac
}

default_user_install_dir() {
  if [ -z "${HOME:-}" ]; then
    printf 'unable to resolve a user install directory because HOME is unset\n' >&2
    exit 1
  fi

  local candidate
  for candidate in "${HOME}/.local/bin" "${HOME}/bin" "${HOME}/.bin"; do
    if path_contains_dir "${candidate}"; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  for candidate in "${HOME}/.local/bin" "${HOME}/bin" "${HOME}/.bin"; do
    if [ -d "${candidate}" ] && [ -w "${candidate}" ]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  printf '%s\n' "${HOME}/.local/bin"
}

default_install_dir() {
  case "$1" in
    darwin) default_user_install_dir ;;
    *) printf '/usr/local/bin\n' ;;
  esac
}

warn_if_install_dir_not_on_path() {
  local dir="$1"
  if ! path_contains_dir "${dir}"; then
    printf 'warning: %s is not on PATH; add it with:\n  export PATH="%s:$PATH"\n' "${dir}" "${dir}" >&2
  fi
}

clear_macos_quarantine() {
  local binary_path="$1"
  if [ "${resolved_os}" != "darwin" ] || ! command -v xattr >/dev/null 2>&1; then
    return 0
  fi

  xattr -d com.apple.quarantine "${binary_path}" >/dev/null 2>&1 || true
}

download_to() {
  local url="$1"
  local output_path="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "${url}" -o "${output_path}"
    return 0
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "${output_path}" "${url}"
    return 0
  fi
  printf 'either curl or wget is required\n' >&2
  exit 1
}

resolve_version() {
  if [ "${version}" != "latest" ]; then
    printf '%s\n' "${version}"
    return 0
  fi

  local api_url="https://api.github.com/repos/${repo}/releases/latest"
  local response
  if command -v curl >/dev/null 2>&1; then
    response="$(curl -fsSL "${api_url}")"
  elif command -v wget >/dev/null 2>&1; then
    response="$(wget -qO- "${api_url}")"
  else
    printf 'either curl or wget is required to resolve the latest release\n' >&2
    exit 1
  fi

  printf '%s\n' "${response}" \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -n1
}

verify_checksum() {
  local asset_name="$1"
  local asset_path="$2"
  local checksums_path="$3"

  local expected
  expected="$(grep " ${asset_name}\$" "${checksums_path}" | awk '{print $1}' | head -n1 || true)"
  if [ -z "${expected}" ]; then
    printf 'warning: no checksum entry found for %s\n' "${asset_name}" >&2
    return 0
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    local actual
    actual="$(sha256sum "${asset_path}" | awk '{print $1}')"
    [ "${actual}" = "${expected}" ] || {
      printf 'checksum verification failed for %s\n' "${asset_name}" >&2
      exit 1
    }
    return 0
  fi

  if command -v shasum >/dev/null 2>&1; then
    local actual
    actual="$(shasum -a 256 "${asset_path}" | awk '{print $1}')"
    [ "${actual}" = "${expected}" ] || {
      printf 'checksum verification failed for %s\n' "${asset_name}" >&2
      exit 1
    }
  fi
}

require_command tar
parse_args "$@"
resolved_os="$(detect_os)"
resolved_arch="$(detect_arch)"
if [ -z "${install_dir}" ]; then
  install_dir="$(default_install_dir "${resolved_os}")"
fi
resolved_version="$(resolve_version)"

if [ -z "${resolved_version}" ]; then
  printf 'unable to resolve a release version\n' >&2
  exit 1
fi

asset_name="${project}-${resolved_os}-${resolved_arch}.tar.gz"
release_base_url="https://github.com/${repo}/releases/download/${resolved_version}"
asset_url="${release_base_url}/${asset_name}"
checksums_url="${release_base_url}/${project}_SHA256SUMS"

temp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${temp_dir}"
}
trap cleanup EXIT

archive_path="${temp_dir}/${asset_name}"
checksums_path="${temp_dir}/${project}_SHA256SUMS"

printf 'downloading %s\n' "${asset_url}"
download_to "${asset_url}" "${archive_path}"
download_to "${checksums_url}" "${checksums_path}" || true
if [ -f "${checksums_path}" ]; then
  verify_checksum "${asset_name}" "${archive_path}" "${checksums_path}"
fi

tar -xzf "${archive_path}" -C "${temp_dir}"

extracted_binary="$(
  find "${temp_dir}" -maxdepth 1 -type f -perm -u+x -name "${project}-*" ! -name '*.tar.gz' | head -n1 || true
)"
if [ -z "${extracted_binary}" ]; then
  printf 'unable to locate extracted binary in %s\n' "${archive_path}" >&2
  exit 1
fi

install -d "${install_dir}"
install -m 0755 "${extracted_binary}" "${install_dir}/${binary_name}"
clear_macos_quarantine "${install_dir}/${binary_name}"

printf 'installed %s %s to %s/%s\n' "${project}" "${resolved_version}" "${install_dir}" "${binary_name}"
if [ "${install_dir_explicit}" = false ]; then
  warn_if_install_dir_not_on_path "${install_dir}"
fi
