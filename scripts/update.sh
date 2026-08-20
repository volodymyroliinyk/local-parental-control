#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "This updater must run as root: sudo ./scripts/update.sh" >&2
  exit 1
fi

if [[ ! -f /etc/local-parental-control/config.json ]]; then
  echo "No existing installation found. Use sudo ./scripts/install.sh instead." >&2
  exit 1
fi

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
build_dir=$(mktemp -d)
trap 'rm -rf -- "$build_dir"' EXIT

# Validate before replacing a running daemon. The update never overwrites config or state.
if command -v lpctl >/dev/null 2>&1; then
  lpctl validate
fi
OUTPUT_DIR="${build_dir}" LPC_VERSION="${LPC_VERSION:-${VERSION:-development}}" "${project_dir}/scripts/build.sh"

install -D -o root -g root -m 0755 "${build_dir}/local-parental-control" /usr/local/sbin/local-parental-control
install -D -o root -g root -m 0755 "${build_dir}/lpctl" /usr/local/sbin/lpctl
install -D -o root -g root -m 0644 "${project_dir}/packaging/local-parental-control.service" /etc/systemd/system/local-parental-control.service
install -D -o root -g root -m 0644 "${project_dir}/README.md" /usr/share/doc/local-parental-control/README.md
chown root:root /etc/local-parental-control/config.json
chmod 0600 /etc/local-parental-control/config.json

systemctl daemon-reload
/usr/local/sbin/lpctl validate
systemctl restart local-parental-control.service
systemctl is-active --quiet local-parental-control.service
echo "Update complete; configuration and usage data were preserved."
