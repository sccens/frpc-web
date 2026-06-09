#!/usr/bin/env sh
set -eu

APP_NAME="frpc-web"
APP_USER="frpc-web"
APP_GROUP="frpc-web"
INSTALL_DIR="/opt/frpc-web"
BIN_DIR="$INSTALL_DIR/bin"
DATA_DIR="$INSTALL_DIR/data"
SCRIPTS_DIR="$INSTALL_DIR/scripts"
ENV_FILE="$INSTALL_DIR/frpc-web.env"
SERVICE_FILE="/etc/systemd/system/frpc-web.service"
ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
SOURCE_BIN="${SOURCE_BIN:-$ROOT_DIR/bin/frpc-web}"

if [ "$(id -u)" -ne 0 ]; then
  echo "install-linux.sh must be run as root" >&2
  exit 1
fi

if [ ! -x "$SOURCE_BIN" ]; then
  echo "binary not found or not executable: $SOURCE_BIN" >&2
  echo "run 'make build' first, or set SOURCE_BIN=/path/to/frpc-web" >&2
  exit 1
fi

if ! getent group "$APP_GROUP" >/dev/null 2>&1; then
  groupadd --system "$APP_GROUP"
fi

if ! id "$APP_USER" >/dev/null 2>&1; then
  nologin="/usr/sbin/nologin"
  if [ ! -x "$nologin" ]; then
    nologin="/sbin/nologin"
  fi
  if [ ! -x "$nologin" ]; then
    nologin="/bin/false"
  fi
  useradd --system --gid "$APP_GROUP" --home-dir "$INSTALL_DIR" --shell "$nologin" "$APP_USER"
fi

install -d -m 0755 -o root -g root "$INSTALL_DIR" "$BIN_DIR" "$SCRIPTS_DIR"
install -d -m 0700 -o "$APP_USER" -g "$APP_GROUP" "$DATA_DIR"
install -m 0755 -o root -g root "$SOURCE_BIN" "$BIN_DIR/frpc-web"

if [ ! -f "$ENV_FILE" ]; then
  cat > "$ENV_FILE" <<EOF
FRPC_WEB_ADDR=127.0.0.1:8080
FRPC_WEB_DATA_DIR=$DATA_DIR
FRPC_WEB_GITHUB_PROXY=
FRPC_WEB_ACCESS_KEY=
FRPC_WEB_JWT_SECRET=
FRPC_WEB_TRUSTED_PROXY=
EOF
fi
chown root:root "$ENV_FILE"
chmod 0640 "$ENV_FILE"

install -m 0755 -o root -g root "$ROOT_DIR/scripts/install-linux.sh" "$SCRIPTS_DIR/install-linux.sh"
install -m 0755 -o root -g root "$ROOT_DIR/scripts/install-oneclick-linux.sh" "$SCRIPTS_DIR/install-oneclick-linux.sh"
install -m 0755 -o root -g root "$ROOT_DIR/scripts/uninstall-linux.sh" "$SCRIPTS_DIR/uninstall-linux.sh"
install -m 0644 -o root -g root "$ROOT_DIR/deploy/frpc-web.service" "$SERVICE_FILE"

systemctl daemon-reload
systemctl enable frpc-web.service

echo "installed $APP_NAME to $INSTALL_DIR"
echo "edit $ENV_FILE if needed, then run: systemctl start frpc-web"
