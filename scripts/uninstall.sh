#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "This uninstaller must run as root: sudo ./scripts/uninstall.sh" >&2
  exit 1
fi

systemctl disable --now local-parental-control.service 2>/dev/null || true
if command -v apparmor_parser >/dev/null 2>&1 && [[ -f /etc/apparmor.d/local-parental-control ]]; then
  apparmor_parser -R /etc/apparmor.d/local-parental-control 2>/dev/null || true
fi
rm -f /etc/systemd/system/local-parental-control.service
rm -f /etc/apparmor.d/local-parental-control
rm -f /usr/local/sbin/local-parental-control /usr/local/sbin/lpctl
rm -f /usr/local/bin/local-parental-control-indicator
rm -f /etc/xdg/autostart/local-parental-control-indicator.desktop
systemctl daemon-reload

echo "Service and binaries removed. Configuration and usage data were preserved."
