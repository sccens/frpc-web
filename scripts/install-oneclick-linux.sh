#!/usr/bin/env sh
set -eu

APP_NAME="frpc-web"
INSTALL_DIR="/opt/frpc-web"
ENV_FILE="$INSTALL_DIR/frpc-web.env"
ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DEFAULT_ADDR="127.0.0.1:8080"

if [ "$(uname -s)" != "Linux" ]; then
  echo "This installer only supports Linux." >&2
  exit 1
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root, for example:" >&2
  echo "  sudo scripts/install-oneclick-linux.sh" >&2
  exit 1
fi

command_exists() {
  command -v "$1" >/dev/null 2>&1
}

read_env_value() {
  key="$1"
  file="$2"
  if [ ! -f "$file" ]; then
    return 0
  fi
  awk -F= -v key="$key" '$1 == key {print substr($0, length(key) + 2); exit}' "$file"
}

escape_sed_value() {
  printf '%s' "$1" | sed 's/[\/&]/\\&/g'
}

set_env_value() {
  key="$1"
  value="$2"
  file="$3"
  escaped=$(escape_sed_value "$value")
  if grep -q "^$key=" "$file"; then
    sed -i "s/^$key=.*/$key=$escaped/" "$file"
  else
    printf '%s=%s\n' "$key" "$value" >> "$file"
  fi
}

generate_access_key() {
  if command_exists openssl; then
    openssl rand -hex 24
    return
  fi
  LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 40
  printf '\n'
}

build_with_local_toolchain() {
  if command_exists make; then
    make build
    return
  fi
  if ! command_exists npm || ! command_exists go; then
    return 1
  fi
  (cd "$ROOT_DIR/web" && npm ci && npm run build)
  mkdir -p "$ROOT_DIR/bin"
  (cd "$ROOT_DIR" && go build -tags embed -trimpath -o bin/frpc-web ./cmd/frpc-web)
}

build_with_docker() {
  if ! command_exists docker; then
    return 1
  fi
  image="frpc-web:oneclick-build"
  docker build -t "$image" "$ROOT_DIR"
  container=$(docker create "$image")
  mkdir -p "$ROOT_DIR/bin"
  docker cp "$container:/usr/local/bin/frpc-web" "$ROOT_DIR/bin/frpc-web"
  docker rm "$container" >/dev/null
  chmod 0755 "$ROOT_DIR/bin/frpc-web"
}

print_missing_build_tools() {
  cat >&2 <<'EOF'
Unable to build frpc-web.

Install one of these toolchains, then rerun this script:
  1. Go + Node.js + npm + make
  2. Docker
EOF
}

cd "$ROOT_DIR"

echo "==> Building $APP_NAME"
if ! build_with_local_toolchain; then
  echo "Local Go/Node toolchain is unavailable; trying Docker build."
  if ! build_with_docker; then
    print_missing_build_tools
    exit 1
  fi
fi

if [ ! -x "$ROOT_DIR/bin/frpc-web" ]; then
  echo "Build did not produce $ROOT_DIR/bin/frpc-web" >&2
  exit 1
fi

echo "==> Installing systemd service"
SOURCE_BIN="$ROOT_DIR/bin/frpc-web" "$ROOT_DIR/scripts/install-linux.sh"

addr="${FRPC_WEB_ADDR:-}"
if [ -z "$addr" ]; then
  addr=$(read_env_value FRPC_WEB_ADDR "$ENV_FILE")
fi
if [ -z "$addr" ]; then
  addr="$DEFAULT_ADDR"
fi

access_key="${FRPC_WEB_ACCESS_KEY:-}"
existing_key=$(read_env_value FRPC_WEB_ACCESS_KEY "$ENV_FILE")
if [ -z "$access_key" ] && [ -n "$existing_key" ]; then
  access_key="$existing_key"
fi
generated_key=0
if [ -z "$access_key" ]; then
  access_key=$(generate_access_key)
  generated_key=1
fi

set_env_value FRPC_WEB_ADDR "$addr" "$ENV_FILE"
set_env_value FRPC_WEB_ACCESS_KEY "$access_key" "$ENV_FILE"
set_env_value FRPC_WEB_DATA_DIR "$INSTALL_DIR/data" "$ENV_FILE"
set_env_value FRPC_WEB_GITHUB_PROXY "" "$ENV_FILE"
chown root:root "$ENV_FILE"
chmod 0640 "$ENV_FILE"

echo "==> Starting $APP_NAME"
systemctl daemon-reload
systemctl enable frpc-web.service >/dev/null
systemctl restart frpc-web.service

host="127.0.0.1"
port="${addr##*:}"
case "$addr" in
  0.0.0.0:*|:*)
    host="$(hostname -I 2>/dev/null | awk '{print $1}')"
    if [ -z "$host" ]; then
      host="127.0.0.1"
    fi
    ;;
  "[::]:"*)
    host="$(hostname -I 2>/dev/null | awk '{print $1}')"
    if [ -z "$host" ]; then
      host="127.0.0.1"
    fi
    ;;
  127.0.0.1:*|localhost:*)
    host="127.0.0.1"
    ;;
  *)
    host="${addr%:*}"
    ;;
esac

url="http://$host:$port"

if command_exists curl; then
  i=0
  until curl -fsS "http://127.0.0.1:$port/api/health" >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -ge 20 ]; then
      echo "Service started, but health check did not pass within 20 seconds." >&2
      break
    fi
    sleep 1
  done
fi

if command_exists xdg-open && [ -n "${DISPLAY:-}" ]; then
  xdg-open "$url" >/dev/null 2>&1 || true
fi

cat <<EOF

$APP_NAME is installed and running.

URL:        $url
Access Key: $access_key
Env file:   $ENV_FILE

Useful commands:
  systemctl status frpc-web
  journalctl -u frpc-web -f
  systemctl restart frpc-web
EOF

if [ "$host" = "127.0.0.1" ]; then
  cat <<'EOF'

Remote server tip:
  ssh -L 8080:127.0.0.1:8080 user@your-server
  then open http://127.0.0.1:8080 locally.
EOF
fi

if [ "$generated_key" -eq 1 ]; then
  cat <<'EOF'

The Access Key was generated automatically. Save it now.
EOF
fi
