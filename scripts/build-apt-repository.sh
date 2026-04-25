#!/usr/bin/env bash
set -euo pipefail

site_root="${1:?site output directory is required}"
packages_dir="${2:?directory containing .deb packages is required}"
suite="${3:-stable}"
component="${4:-main}"
package_name="${5:-s3ctl}"
package_group="${package_name:0:1}"
pool_dir="${site_root}/pool/${component}/${package_group}/${package_name}"
dist_relative_root="dists/${suite}"

mkdir -p "${pool_dir}"

shopt -s nullglob
packages=("${packages_dir}"/*.deb)
shopt -u nullglob

if [ "${#packages[@]}" -eq 0 ]; then
  echo "no .deb packages found in ${packages_dir}" >&2
  exit 1
fi

for package in "${packages[@]}"; do
  cp "${package}" "${pool_dir}/"
done

architectures=()
for package in "${pool_dir}"/*.deb; do
  architectures+=("$(dpkg-deb --field "${package}" Architecture)")
done

mapfile -t architectures < <(printf '%s\n' "${architectures[@]}" | sed '/^$/d' | sort -u)

if [ "${#architectures[@]}" -eq 0 ]; then
  echo "unable to determine package architectures" >&2
  exit 1
fi

pushd "${site_root}" >/dev/null
mkdir -p "${dist_relative_root}"
for architecture in "${architectures[@]}"; do
  binary_dir="dists/${suite}/${component}/binary-${architecture}"
  mkdir -p "${binary_dir}"
  dpkg-scanpackages -a "${architecture}" "pool" /dev/null > "${binary_dir}/Packages"
  gzip -9c "${binary_dir}/Packages" > "${binary_dir}/Packages.gz"
  xz -9c "${binary_dir}/Packages" > "${binary_dir}/Packages.xz"
done

release_architectures="$(printf '%s ' "${architectures[@]}")"
release_architectures="${release_architectures% }"

apt-ftparchive \
  -o "APT::FTPArchive::Release::Origin=${package_name}" \
  -o "APT::FTPArchive::Release::Label=${package_name}" \
  -o "APT::FTPArchive::Release::Suite=${suite}" \
  -o "APT::FTPArchive::Release::Codename=${suite}" \
  -o "APT::FTPArchive::Release::Components=${component}" \
  -o "APT::FTPArchive::Release::Architectures=${release_architectures}" \
  -o "APT::FTPArchive::Release::Acquire-By-Hash=yes" \
  release "${dist_relative_root}" > "${dist_relative_root}/Release"
popd >/dev/null
