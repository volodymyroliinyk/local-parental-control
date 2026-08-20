#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "This installer must run as root: sudo ./scripts/install.sh" >&2
  exit 1
fi

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
build_dir=$(mktemp -d)
trap 'rm -rf -- "$build_dir"' EXIT

OUTPUT_DIR="${build_dir}" LPC_VERSION="${LPC_VERSION:-${VERSION:-development}}" "${project_dir}/scripts/build.sh"

install -D -o root -g root -m 0755 "${build_dir}/local-parental-control" /usr/local/sbin/local-parental-control
install -D -o root -g root -m 0755 "${build_dir}/lpctl" /usr/local/sbin/lpctl
install -D -o root -g root -m 0644 "${project_dir}/packaging/local-parental-control.service" /etc/systemd/system/local-parental-control.service
install -D -o root -g root -m 0644 "${project_dir}/README.md" /usr/share/doc/local-parental-control/README.md
install -d -o root -g root -m 0700 /etc/local-parental-control /var/lib/local-parental-control

if [[ ! -e /etc/local-parental-control/config.json ]]; then
  install -o root -g root -m 0600 "${project_dir}/example/config.json" /etc/local-parental-control/config.json
  echo "Created /etc/local-parental-control/config.json from the example."
else
  chown root:root /etc/local-parental-control/config.json
  chmod 0600 /etc/local-parental-control/config.json
  echo "Preserved the existing configuration."
fi

systemctl daemon-reload
if /usr/local/sbin/lpctl validate; then
  systemctl enable --now local-parental-control.service
  echo "Installation complete. Run: sudo lpctl status"
else
  echo "Files are installed, but the service was not started because the configuration is invalid." >&2
  echo "Edit /etc/local-parental-control/config.json, validate it, then enable the service." >&2
  exit 2
fi
