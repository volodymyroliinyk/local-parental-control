#!/usr/bin/env bash
set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
output_dir=${OUTPUT_DIR:-"${project_dir}/bin"}
project_version=${LPC_VERSION:-${VERSION:-development}}
# VERSION has special meaning in some Go toolchain distributions. Do not leak
# the release label into child go processes.
unset VERSION

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required to build this project." >&2
  exit 1
fi

mkdir -p "${output_dir}"
echo "Building Local Parental Control ${project_version}..."
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${project_version}" -o "${output_dir}/local-parental-control" "${project_dir}/cmd/local-parental-control"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${project_version}" -o "${output_dir}/lpctl" "${project_dir}/cmd/lpctl"
chmod 0755 "${output_dir}/local-parental-control" "${output_dir}/lpctl"
echo "Binaries written to ${output_dir}"
