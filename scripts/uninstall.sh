#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "This uninstaller must run as root: sudo ./scripts/uninstall.sh" >&2
  exit 1
fi

systemctl disable --now local-parental-control.service 2>/dev/null || true
rm -f /etc/systemd/system/local-parental-control.service
rm -f /usr/local/sbin/local-parental-control /usr/local/sbin/lpctl
systemctl daemon-reload

echo "Service and binaries removed. Configuration and usage data were preserved."

