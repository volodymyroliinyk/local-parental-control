#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "This installer must run as root: sudo ./scripts/install.sh" >&2
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
for binary in local-parental-control lpctl local-parental-control-indicator; do
  path="${build_dir}/${binary}"
  if [[ ! -f ${path} || -L ${path} || $(stat -c %u -- "${path}") -ne ${SUDO_UID} || -n $(find "${path}" -perm /022 -print -quit) ]]; then
    echo "Unsafe or missing ${path}. Run ./scripts/build.sh without sudo first." >&2
    exit 1
  fi
done

install -D -o root -g root -m 0755 "${build_dir}/local-parental-control" /usr/local/sbin/local-parental-control
install -D -o root -g root -m 0755 "${build_dir}/lpctl" /usr/local/sbin/lpctl
install -D -o root -g root -m 0755 "${build_dir}/local-parental-control-indicator" /usr/local/bin/local-parental-control-indicator
install -D -o root -g root -m 0644 "${project_dir}/packaging/local-parental-control-indicator.desktop" /etc/xdg/autostart/local-parental-control-indicator.desktop
install -D -o root -g root -m 0644 "${project_dir}/packaging/local-parental-control.service" /etc/systemd/system/local-parental-control.service
install -D -o root -g root -m 0644 "${project_dir}/packaging/apparmor/local-parental-control" /etc/apparmor.d/local-parental-control
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

apparmor_parser -r /etc/apparmor.d/local-parental-control

systemctl daemon-reload
if /usr/local/sbin/lpctl validate; then
  systemctl enable local-parental-control.service
  systemctl restart local-parental-control.service
  wait_for_daemon
  echo "Installation complete. Run: sudo lpctl status"
else
  echo "Files are installed, but the service was not started because the configuration is invalid." >&2
  echo "Edit /etc/local-parental-control/config.json, validate it, then enable the service." >&2
  exit 2
fi
