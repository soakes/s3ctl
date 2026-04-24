#!/usr/bin/env bash
set -euo pipefail

binary_path="${1:?binary path is required}"
output_dir="${2:?output directory is required}"
version_input="${3:?version is required}"
deb_arch="${4:?debian architecture is required}"

package_name="${PACKAGE_NAME:-s3ctl}"
maintainer="${PACKAGE_MAINTAINER:-s3ctl release automation <noreply@github.com>}"
description="${PACKAGE_DESCRIPTION:-Professional S3 bucket provisioning CLI with scoped credential automation}"
homepage="${PACKAGE_HOMEPAGE:-https://github.com/soakes/s3ctl}"

normalize_deb_version() {
  local value="${1#v}"
  if [[ "${value}" =~ ^([0-9]+\.[0-9]+\.[0-9]+)-rc\.([0-9]+)$ ]]; then
    printf '%s~rc%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}"
    return 0
  fi
  printf '%s\n' "${value}"
}

deb_version="$(normalize_deb_version "${version_input}")"
staging_root="$(mktemp -d)"
package_root="${staging_root}/${package_name}_${deb_version}_${deb_arch}"
control_dir="${package_root}/DEBIAN"
bin_dir="${package_root}/usr/bin"
doc_dir="${package_root}/usr/share/doc/${package_name}"
output_path="${output_dir}/${package_name}_${deb_version}_${deb_arch}.deb"

cleanup() {
  rm -rf "${staging_root}"
}
trap cleanup EXIT

mkdir -p "${control_dir}" "${bin_dir}" "${doc_dir}" "${output_dir}"
install -m 0755 "${binary_path}" "${bin_dir}/${package_name}"

cat > "${control_dir}/control" <<EOF
Package: ${package_name}
Version: ${deb_version}
Section: utils
Priority: optional
Architecture: ${deb_arch}
Maintainer: ${maintainer}
Homepage: ${homepage}
Description: ${description}
EOF

cat > "${doc_dir}/copyright" <<EOF
Upstream-Name: ${package_name}
Source: ${homepage}
EOF

dpkg-deb --root-owner-group --build "${package_root}" "${output_path}" >/dev/null
printf '%s\n' "${output_path}"
