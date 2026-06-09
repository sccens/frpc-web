#!/usr/bin/env sh
set -eu

APP_USER="frpc-web"
APP_GROUP="frpc-web"
INSTALL_DIR="/opt/frpc-web"
SERVICE_FILE="/etc/systemd/system/frpc-web.service"
PURGE_DATA=0

if [ "${1:-}" = "--purge-data" ]; then
  PURGE_DATA=1
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "uninstall-linux.sh must be run as root" >&2
  exit 1
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl stop frpc-web.service >/dev/null 2>&1 || true
  systemctl disable frpc-web.service >/dev/null 2>&1 || true
fi

rm -f "$SERVICE_FILE"

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
fi

rm -rf "$INSTALL_DIR/bin" "$INSTALL_DIR/scripts"
rm -f "$INSTALL_DIR/frpc-web.env"

if [ "$PURGE_DATA" -eq 1 ]; then
  rm -rf "$INSTALL_DIR"
  if id "$APP_USER" >/dev/null 2>&1; then
    userdel "$APP_USER" >/dev/null 2>&1 || true
  fi
  if getent group "$APP_GROUP" >/dev/null 2>&1; then
    groupdel "$APP_GROUP" >/dev/null 2>&1 || true
  fi
  echo "frpc-web removed, including data"
else
  echo "frpc-web removed; data kept at $INSTALL_DIR/data"
  echo "run with --purge-data to remove data and the frpc-web system user"
fi
