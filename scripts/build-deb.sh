#!/usr/bin/env bash
set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
package_version=${LPC_VERSION:-0.1.0}
output_dir=${OUTPUT_DIR:-"${project_dir}/dist"}
architecture=$(dpkg --print-architecture)

if ! command -v dpkg-deb >/dev/null 2>&1; then
  echo "dpkg-deb is required (provided by the dpkg package)." >&2
  exit 1
fi
if ! dpkg --validate-version "${package_version}"; then
  echo "Invalid Debian package version: ${package_version}" >&2
  exit 1
fi
case "${architecture}" in
  amd64|arm64) ;;
  *) echo "Unsupported package architecture: ${architecture}" >&2; exit 1 ;;
esac

staging_dir=$(mktemp -d)
build_dir=$(mktemp -d)
trap 'rm -rf -- "$staging_dir" "$build_dir"' EXIT

OUTPUT_DIR="${build_dir}" LPC_VERSION="${package_version}" "${project_dir}/scripts/build.sh"

install -D -m 0755 "${build_dir}/local-parental-control" "${staging_dir}/usr/sbin/local-parental-control"
install -D -m 0755 "${build_dir}/lpctl" "${staging_dir}/usr/sbin/lpctl"
install -D -m 0644 "${project_dir}/packaging/debian/local-parental-control.service" "${staging_dir}/lib/systemd/system/local-parental-control.service"
install -D -m 0600 "${project_dir}/example/config.json" "${staging_dir}/etc/local-parental-control/config.json"
install -D -m 0644 "${project_dir}/README.md" "${staging_dir}/usr/share/doc/local-parental-control/README.md"
install -D -m 0644 "${project_dir}/packaging/debian/copyright" "${staging_dir}/usr/share/doc/local-parental-control/copyright"
install -d -m 0755 "${staging_dir}/DEBIAN"

sed "s/@VERSION@/${package_version}/g; s/@ARCHITECTURE@/${architecture}/g" \
  "${project_dir}/packaging/debian/control.in" >"${staging_dir}/DEBIAN/control"
install -m 0755 "${project_dir}/packaging/debian/postinst" "${staging_dir}/DEBIAN/postinst"
install -m 0755 "${project_dir}/packaging/debian/prerm" "${staging_dir}/DEBIAN/prerm"
install -m 0755 "${project_dir}/packaging/debian/postrm" "${staging_dir}/DEBIAN/postrm"
install -m 0644 "${project_dir}/packaging/debian/conffiles" "${staging_dir}/DEBIAN/conffiles"

mkdir -p "${output_dir}"
package_path="${output_dir}/local-parental-control_${package_version}_${architecture}.deb"
dpkg-deb --build --root-owner-group "${staging_dir}" "${package_path}"
echo "Debian package written to ${package_path}"

