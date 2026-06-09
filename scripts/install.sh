#!/usr/bin/env sh
set -eu

APP_NAME="frpc-web"
INSTALL_DIR="/opt/frpc-web"
ENV_FILE="$INSTALL_DIR/frpc-web.env"
ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DEFAULT_HOST="127.0.0.1"
DEFAULT_PORT="8080"
DEFAULT_ADDR="$DEFAULT_HOST:$DEFAULT_PORT"

if [ "$(uname -s)" != "Linux" ]; then
  echo "This installer only supports Linux." >&2
  exit 1
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root, for example:" >&2
  echo "  sudo scripts/install.sh" >&2
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

validate_port() {
  port="$1"
  case "$port" in
    ''|*[!0-9]*)
      return 1
      ;;
  esac
  [ "$port" -ge 1 ] && [ "$port" -le 65535 ]
}

addr_port() {
  input="$1"
  port="${input##*:}"
  if validate_port "$port"; then
    printf '%s\n' "$port"
  else
    printf '%s\n' "$DEFAULT_PORT"
  fi
}

addr_default_host() {
  input="$1"
  case "$input" in
    0.0.0.0:*|:*|"[::]:"*)
      printf '0.0.0.0\n'
      ;;
    127.0.0.1:*|localhost:*)
      printf '127.0.0.1\n'
      ;;
    *)
      printf '%s\n' "$DEFAULT_HOST"
      ;;
  esac
}

validate_addr_for_healthcheck() {
  input="$1"
  case "$input" in
    *:*)
      ;;
    *)
      echo "FRPC_WEB_ADDR must include a port, for example 127.0.0.1:8080" >&2
      exit 1
      ;;
  esac
  port="${input##*:}"
  if ! validate_port "$port"; then
    echo "Invalid FRPC_WEB_ADDR port: $input" >&2
    exit 1
  fi
}

choose_listen_addr() {
  addr="${FRPC_WEB_ADDR:-}"
  if [ -n "$addr" ]; then
    validate_addr_for_healthcheck "$addr"
    echo "==> Using FRPC_WEB_ADDR=$addr"
    return
  fi

  existing_addr=$(read_env_value FRPC_WEB_ADDR "$ENV_FILE")
  if [ -z "$existing_addr" ]; then
    existing_addr="$DEFAULT_ADDR"
  fi

  if [ ! -t 0 ]; then
    validate_addr_for_healthcheck "$existing_addr"
    addr="$existing_addr"
    echo "==> No interactive terminal; using $addr"
    return
  fi

  default_host=$(addr_default_host "$existing_addr")
  default_port=$(addr_port "$existing_addr")

  case "$default_host" in
    0.0.0.0)
      default_choice="2"
      ;;
    *)
      default_choice="1"
      ;;
  esac

  case "$existing_addr" in
    127.0.0.1:*|localhost:*|0.0.0.0:*|:*|"[::]:"*)
      ;;
    *)
      echo "Existing FRPC_WEB_ADDR is $existing_addr; this run will replace it with your selected listen address."
      ;;
  esac

  cat <<'EOF'
Choose listen address:
  1) 127.0.0.1 - local only, recommended behind SSH tunnel or reverse proxy
  2) 0.0.0.0   - reachable from the VM or server network
EOF
  printf 'Enter choice [%s]: ' "$default_choice"
  read -r choice || choice=""
  choice="${choice:-$default_choice}"

  case "$choice" in
    1)
      listen_host="127.0.0.1"
      ;;
    2)
      listen_host="0.0.0.0"
      ;;
    *)
      echo "Invalid choice: $choice" >&2
      exit 1
      ;;
  esac

  printf 'Listen port [%s]: ' "$default_port"
  read -r selected_port || selected_port=""
  selected_port="${selected_port:-$default_port}"
  if ! validate_port "$selected_port"; then
    echo "Invalid port: $selected_port" >&2
    exit 1
  fi

  addr="$listen_host:$selected_port"
  if [ "$listen_host" = "0.0.0.0" ]; then
    cat <<'EOF'
Note: 0.0.0.0 exposes the service on this machine's network interfaces.
Make sure your firewall, security group, and HTTPS/reverse proxy plan match your environment.
EOF
  fi
}

ensure_source_checkout() {
  if [ ! -f "$ROOT_DIR/go.mod" ] \
    || [ ! -f "$ROOT_DIR/web/package.json" ] \
    || [ ! -f "$ROOT_DIR/scripts/install-linux.sh" ] \
    || [ ! -f "$ROOT_DIR/deploy/frpc-web.service" ]; then
    cat >&2 <<EOF
This installer must be run from a frpc-web source checkout.

Expected source directory: $ROOT_DIR

For example:
  git clone https://github.com/sccens/frpc-web.git
  cd frpc-web
  sudo scripts/install.sh
EOF
    exit 1
  fi
}

ensure_systemd() {
  if ! command_exists systemctl; then
    echo "systemctl was not found. This installer targets systemd-based Linux servers." >&2
    exit 1
  fi
  if [ ! -d /run/systemd/system ]; then
    echo "systemd does not appear to be running. Run this installer on the target Linux server or VM." >&2
    exit 1
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
  image="frpc-web:install-build"
  docker build -t "$image" "$ROOT_DIR" || return 1
  container=$(docker create "$image") || return 1
  mkdir -p "$ROOT_DIR/bin"
  if ! docker cp "$container:/usr/local/bin/frpc-web" "$ROOT_DIR/bin/frpc-web"; then
    docker rm "$container" >/dev/null 2>&1 || true
    return 1
  fi
  docker rm "$container" >/dev/null 2>&1 || true
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

ensure_source_checkout
ensure_systemd
choose_listen_addr

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
github_proxy="${FRPC_WEB_GITHUB_PROXY:-}"
if [ -z "$github_proxy" ]; then
  github_proxy=$(read_env_value FRPC_WEB_GITHUB_PROXY "$ENV_FILE")
fi
set_env_value FRPC_WEB_GITHUB_PROXY "$github_proxy" "$ENV_FILE"
chown root:root "$ENV_FILE"
chmod 0640 "$ENV_FILE"

echo "==> Starting $APP_NAME"
systemctl daemon-reload
systemctl enable frpc-web.service >/dev/null
if ! systemctl restart frpc-web.service; then
  echo "Failed to start frpc-web.service. Recent service status:" >&2
  systemctl status --no-pager -l frpc-web.service >&2 || true
  echo "Recent logs:" >&2
  journalctl -u frpc-web.service -n 80 --no-pager >&2 || true
  exit 1
fi

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
