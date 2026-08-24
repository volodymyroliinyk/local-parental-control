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
build_dir="${project_dir}/bin"

wait_for_daemon() {
  local deadline=$((SECONDS + 10))
  until /usr/local/sbin/lpctl status >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      echo "Service started but the control socket did not become ready." >&2
      echo "Check: systemctl status local-parental-control.service" >&2
      return 1
    fi
    sleep 0.1
  done
}

if ! command -v apparmor_parser >/dev/null 2>&1; then
  echo "AppArmor is required. Install the apparmor package first." >&2
  exit 1
fi
if [[ -z ${SUDO_UID:-} || ${SUDO_UID} -eq 0 ]]; then
  echo "Run this script through sudo from the administrator account." >&2
  exit 1
fi
for binary in local-parental-control lpctl; do
  path="${build_dir}/${binary}"
  if [[ ! -f ${path} || -L ${path} || $(stat -c %u -- "${path}") -ne ${SUDO_UID} || -n $(find "${path}" -perm /022 -print -quit) ]]; then
    echo "Unsafe or missing ${path}. Run ./scripts/build.sh without sudo first." >&2
    exit 1
  fi
done

install -D -o root -g root -m 0755 "${build_dir}/local-parental-control" /usr/local/sbin/local-parental-control
install -D -o root -g root -m 0755 "${build_dir}/lpctl" /usr/local/sbin/lpctl
install -D -o root -g root -m 0644 "${project_dir}/packaging/local-parental-control.service" /etc/systemd/system/local-parental-control.service
install -D -o root -g root -m 0644 "${project_dir}/packaging/apparmor/local-parental-control" /etc/apparmor.d/local-parental-control
install -D -o root -g root -m 0644 "${project_dir}/README.md" /usr/share/doc/local-parental-control/README.md
chown root:root /etc/local-parental-control/config.json
chmod 0600 /etc/local-parental-control/config.json
apparmor_parser -r /etc/apparmor.d/local-parental-control

systemctl daemon-reload
if /usr/local/sbin/lpctl validate; then
  systemctl restart local-parental-control.service
  systemctl is-active --quiet local-parental-control.service
  wait_for_daemon
  echo "Update complete; configuration and usage data were preserved."
else
  echo "Files were updated, but the service was not restarted because the existing configuration is invalid for this version." >&2
  echo "The previously running service, if any, was left running with its active configuration." >&2
  echo "Use 'sudo lpctl discover KEYWORD' and add only a path marked 'supported'." >&2
  echo "Paths marked 'unsupported launcher', including Snap launchers, must not be added." >&2
  echo "If no supported path is found, install a native ELF package or choose another native application." >&2
  echo "Update /etc/local-parental-control/config.json, then run:" >&2
  echo "  sudo lpctl validate" >&2
  echo "  sudo systemctl restart local-parental-control.service" >&2
  exit 2
fi
