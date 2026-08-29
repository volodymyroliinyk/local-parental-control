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
if [[ ! -x /usr/sbin/nft ]]; then
  echo "nftables is required. Install the nftables package first." >&2
  exit 1
fi
if [[ -z ${SUDO_UID:-} || ${SUDO_UID} -eq 0 ]]; then
  echo "Run this script through sudo from the administrator account." >&2
  exit 1
fi
for binary in local-parental-control lpctl local-parental-control-indicator; do
  path="${build_dir}/${binary}"
  if [[ ! -f ${path} || -L ${path} || $(stat -c %u -- "${path}") -ne ${SUDO_UID} || -n $(find "${path}" -perm /022 -print -quit) ]]; then
    echo "Unsafe or missing ${path}. Run ./scripts/build.sh without sudo first." >&2
    exit 1
  fi
done

# Install the new standalone CLI first so migration and discovery commands are
# available without replacing the running daemon. Only commit the daemon,
# service, and confinement update after the new version accepts the existing
# configuration.
install -D -o root -g root -m 0755 "${build_dir}/lpctl" /usr/local/sbin/lpctl
if ! /usr/local/sbin/lpctl validate; then
  echo "The new lpctl was installed, but the daemon update was deferred because the existing configuration is incompatible." >&2
  echo "The installed daemon, systemd unit, AppArmor profile, configuration, and usage data were left unchanged." >&2
  echo "Use 'sudo lpctl discover KEYWORD' and copy its executable paths into the matching application rule." >&2
  echo "After correcting and validating /etc/local-parental-control/config.json, rerun this updater." >&2
  exit 2
fi

install -D -o root -g root -m 0755 "${build_dir}/local-parental-control" /usr/local/sbin/local-parental-control
install -D -o root -g root -m 0755 "${build_dir}/local-parental-control-indicator" /usr/local/bin/local-parental-control-indicator
install -D -o root -g root -m 0644 "${project_dir}/packaging/local-parental-control-indicator.desktop" /etc/xdg/autostart/local-parental-control-indicator.desktop
install -D -o root -g root -m 0644 "${project_dir}/packaging/local-parental-control.service" /etc/systemd/system/local-parental-control.service
install -D -o root -g root -m 0644 "${project_dir}/packaging/apparmor/local-parental-control" /etc/apparmor.d/local-parental-control
install -D -o root -g root -m 0644 "${project_dir}/README.md" /usr/share/doc/local-parental-control/README.md
chown root:root /etc/local-parental-control/config.json
chmod 0600 /etc/local-parental-control/config.json
apparmor_parser -r /etc/apparmor.d/local-parental-control

systemctl daemon-reload
systemctl restart local-parental-control.service
systemctl is-active --quiet local-parental-control.service
wait_for_daemon
echo "Update complete; configuration and usage data were preserved."
