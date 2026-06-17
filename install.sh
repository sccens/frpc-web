#!/usr/bin/env bash
set -e

# FRPC-Web 安装/卸载脚本（唯一入口）
#
# 安装（在线，自动配置 systemd）:
#   curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash
#
# 安装（使用本地构建的二进制，如 make build 产物）:
#   SOURCE_BIN=bin/frpc-web ./install.sh
#
# 卸载（保留数据）:
#   ./install.sh --uninstall
#   curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash -s -- --uninstall
#
# 卸载（连数据一起删除）:
#   ./install.sh --uninstall --purge-data
#
# 可选环境变量:
#   INSTALL_DIR=/usr/local/bin   二进制安装目录
#   SKIP_SERVICE=1               只装二进制，不配置 systemd
#   SOURCE_BIN=path/to/frpc-web  跳过下载，安装本地二进制

REPO="sccens/frpc-web"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="frpc-web"
SERVICE_DIR="/opt/frpc-web"
SERVICE_ENV_FILE="$SERVICE_DIR/frpc-web.env"
SERVICE_FILE="/etc/systemd/system/frpc-web.service"
APP_USER="frpc-web"
APP_GROUP="frpc-web"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# 需要写系统位置时返回 sudo 前缀（root 下为空）
require_root() {
    if [ "$(id -u)" -eq 0 ]; then
        SUDO=""
    elif command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
    else
        error "本操作需要 root 权限，且未找到 sudo 命令"
    fi
}

# 检测操作系统
detect_os() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$OS" in
        linux*)
            OS="linux"
            ;;
        darwin*)
            OS="darwin"
            ;;
        *)
            error "不支持的操作系统: $OS（当前仅支持 Linux 和 macOS）"
            ;;
    esac
    info "检测到操作系统: $OS"
}

# 检测架构
detect_arch() {
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64 | amd64)
            ARCH="amd64"
            ;;
        aarch64 | arm64)
            ARCH="arm64"
            ;;
        *)
            error "不支持的架构: $ARCH（当前仅支持 amd64 和 arm64）"
            ;;
    esac
    info "检测到架构: $ARCH"
}

# 下载文件到指定路径
fetch() {
    # $1: URL, $2: 输出路径
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$2" "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$2" "$1"
    else
        error "需要 curl 或 wget 来下载文件"
    fi
}

# 获取最新版本
get_latest_version() {
    info "获取最新版本..."

    # 尝试使用 curl
    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": *"[^"]*"' | sed 's/"tag_name": *"\(.*\)"/\1/')
    # 尝试使用 wget
    elif command -v wget >/dev/null 2>&1; then
        VERSION=$(wget -qO- "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": *"[^"]*"' | sed 's/"tag_name": *"\(.*\)"/\1/')
    else
        error "需要 curl 或 wget 来下载文件"
    fi

    if [ -z "$VERSION" ]; then
        error "无法获取最新版本，请检查网络连接"
    fi

    info "最新版本: $VERSION"
}

# 构造下载 URL
get_download_url() {
    FILENAME="${BINARY_NAME}_${OS}_${ARCH}"
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$FILENAME"
    CHECKSUMS_URL="https://github.com/$REPO/releases/download/$VERSION/SHA256SUMS"
    info "下载地址: $DOWNLOAD_URL"
}

# 下载二进制文件
download_binary() {
    info "正在下载 $FILENAME..."

    TMP_FILE=$(mktemp)

    if ! fetch "$DOWNLOAD_URL" "$TMP_FILE"; then
        error "下载失败"
    fi

    info "下载完成"
}

# 验证二进制文件
verify_binary() {
    if [ ! -f "$TMP_FILE" ]; then
        error "下载的文件不存在"
    fi

    FILE_SIZE=$(stat -f%z "$TMP_FILE" 2>/dev/null || stat -c%s "$TMP_FILE" 2>/dev/null)
    if [ -z "$FILE_SIZE" ]; then
        error "无法读取下载文件大小"
    fi
    if [ "$FILE_SIZE" -lt 1000000 ]; then
        error "下载的文件太小，可能下载失败"
    fi

    if command -v bc >/dev/null 2>&1; then
        info "文件大小: $(echo "scale=2; $FILE_SIZE/1024/1024" | bc) MB"
    else
        info "文件大小: $FILE_SIZE bytes"
    fi

    verify_checksum
}

# 校验 SHA256（发布工作流会随版本发布 SHA256SUMS）
verify_checksum() {
    TMP_SUMS=$(mktemp)
    if ! fetch "$CHECKSUMS_URL" "$TMP_SUMS" 2>/dev/null; then
        warn "无法下载 SHA256SUMS，跳过校验"
        rm -f "$TMP_SUMS"
        return
    fi

    EXPECTED=$(grep " $FILENAME\$" "$TMP_SUMS" | awk '{print $1}')
    rm -f "$TMP_SUMS"
    if [ -z "$EXPECTED" ]; then
        warn "SHA256SUMS 中没有 $FILENAME 的记录，跳过校验"
        return
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL=$(sha256sum "$TMP_FILE" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        ACTUAL=$(shasum -a 256 "$TMP_FILE" | awk '{print $1}')
    else
        warn "未找到 sha256sum/shasum，跳过校验"
        return
    fi

    if [ "$ACTUAL" != "$EXPECTED" ]; then
        rm -f "$TMP_FILE"
        error "SHA256 校验失败：文件可能损坏或被篡改"
    fi
    info "SHA256 校验通过"
}

# 使用本地二进制（SOURCE_BIN）代替下载
use_local_binary() {
    if [ ! -x "$SOURCE_BIN" ]; then
        error "本地二进制不存在或不可执行: $SOURCE_BIN（先运行 make build，或检查 SOURCE_BIN 路径）"
    fi
    TMP_FILE="$SOURCE_BIN"
    KEEP_SOURCE=1
    info "使用本地二进制: $SOURCE_BIN"
}

# 安装二进制文件
install_binary() {
    info "正在安装到 $INSTALL_DIR/$BINARY_NAME..."

    # 检查是否需要 sudo
    if [ -w "$INSTALL_DIR" ]; then
        SUDO=""
    else
        require_root
        [ -n "$SUDO" ] && warn "需要管理员权限安装到 $INSTALL_DIR"
    fi

    # install 会同时设置属主（sudo 下为 root）和权限，避免留下普通用户可改写的系统二进制；
    # 先显式删除目标（可能是旧版安装留下的软链接），确保落盘的是真实文件
    if ! { $SUDO rm -f "$INSTALL_DIR/$BINARY_NAME" && $SUDO install -m 0755 "$TMP_FILE" "$INSTALL_DIR/$BINARY_NAME"; }; then
        [ "${KEEP_SOURCE:-0}" = "1" ] || rm -f "$TMP_FILE"
        error "安装失败"
    fi
    [ "${KEEP_SOURCE:-0}" = "1" ] || rm -f "$TMP_FILE"

    info "安装成功"
}

# 验证安装
verify_installation() {
    INSTALLED_BIN="$INSTALL_DIR/$BINARY_NAME"
    if [ ! -x "$INSTALLED_BIN" ]; then
        error "安装后的二进制不可执行: $INSTALLED_BIN"
    fi

    info "验证安装..."
    INSTALLED_VERSION=$("$INSTALLED_BIN" --version 2>&1 || echo "unknown")
    info "已安装版本: $INSTALLED_VERSION"

    if ! command -v "$BINARY_NAME" >/dev/null 2>&1; then
        warn "$BINARY_NAME 不在 PATH 中"
        echo ""
        echo "请将以下内容添加到你的 shell 配置文件（~/.bashrc 或 ~/.zshrc）："
        echo "export PATH=\"$INSTALL_DIR:\$PATH\""
        echo ""
        echo "或者直接运行: $INSTALL_DIR/$BINARY_NAME"
        return
    fi
}

# 安装 systemd 服务（仅 Linux + systemd + root/sudo 时执行；SKIP_SERVICE=1 跳过）
SERVICE_INSTALLED=0

setup_service() {
    if [ "$OS" != "linux" ] || [ "${SKIP_SERVICE:-0}" = "1" ]; then
        return
    fi
    if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
        return
    fi

    if [ "$(id -u)" -eq 0 ]; then
        SVC_SUDO=""
    elif command -v sudo >/dev/null 2>&1; then
        SVC_SUDO="sudo"
    else
        warn "无 root 权限，跳过 systemd 服务安装（可稍后用 root 重跑本脚本）"
        return
    fi

    info "检测到 systemd，正在安装系统服务..."

    if ! getent group "$APP_GROUP" >/dev/null 2>&1; then
        $SVC_SUDO groupadd --system "$APP_GROUP"
    fi
    if ! id -u "$APP_USER" >/dev/null 2>&1; then
        NOLOGIN=$(command -v nologin 2>/dev/null || echo /usr/sbin/nologin)
        [ -x "$NOLOGIN" ] || NOLOGIN=/bin/false
        $SVC_SUDO useradd --system --gid "$APP_GROUP" --home-dir "$SERVICE_DIR" --shell "$NOLOGIN" "$APP_USER"
    fi

    $SVC_SUDO install -d -m 0755 "$SERVICE_DIR"
    $SVC_SUDO install -d -m 0755 -o "$APP_USER" -g "$APP_GROUP" "$SERVICE_DIR/bin"
    $SVC_SUDO install -d -m 0700 -o "$APP_USER" -g "$APP_GROUP" "$SERVICE_DIR/data"

    # 服务模式下二进制归服务用户所有，Web 控制台的一键自更新才能替换它；
    # /usr/local/bin 保留软链接方便命令行调用
    SERVICE_BIN="$SERVICE_DIR/bin/$BINARY_NAME"
    $SVC_SUDO install -m 0755 -o "$APP_USER" -g "$APP_GROUP" "$INSTALL_DIR/$BINARY_NAME" "$SERVICE_BIN"
    $SVC_SUDO rm -f "$INSTALL_DIR/$BINARY_NAME"
    $SVC_SUDO ln -s "$SERVICE_BIN" "$INSTALL_DIR/$BINARY_NAME"

    # 不覆盖已有配置，便于升级时保留用户设置
    if ! $SVC_SUDO test -f "$SERVICE_ENV_FILE"; then
        printf '%s\n' \
            "FRPC_WEB_ADDR=127.0.0.1:8080" \
            "FRPC_WEB_DATA_DIR=$SERVICE_DIR/data" \
            "FRPC_WEB_GITHUB_PROXY=" \
            "FRPC_WEB_ACCESS_KEY=" \
            "FRPC_WEB_TRUSTED_PROXY=" \
            | $SVC_SUDO tee "$SERVICE_ENV_FILE" >/dev/null
        $SVC_SUDO chmod 0640 "$SERVICE_ENV_FILE"
    fi

    cat <<EOF | $SVC_SUDO tee "$SERVICE_FILE" >/dev/null
[Unit]
Description=FRPC Web
Documentation=https://github.com/$REPO
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=$APP_USER
Group=$APP_GROUP
WorkingDirectory=$SERVICE_DIR
EnvironmentFile=-$SERVICE_ENV_FILE
ExecStart=$SERVICE_BIN
Restart=on-failure
RestartSec=3
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=$SERVICE_DIR/data $SERVICE_DIR/bin
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

    $SVC_SUDO systemctl daemon-reload
    $SVC_SUDO systemctl enable frpc-web.service >/dev/null 2>&1
    if $SVC_SUDO systemctl is-active --quiet frpc-web.service; then
        # 升级场景：重启以加载新二进制
        $SVC_SUDO systemctl restart frpc-web.service
    else
        $SVC_SUDO systemctl start frpc-web.service
    fi

    SERVICE_INSTALLED=1
    info "systemd 服务已启动并设为开机自启"
}

# 卸载
uninstall() {
    require_root

    if command -v systemctl >/dev/null 2>&1; then
        $SUDO systemctl stop frpc-web.service >/dev/null 2>&1 || true
        $SUDO systemctl disable frpc-web.service >/dev/null 2>&1 || true
        $SUDO rm -f "$SERVICE_FILE"
        $SUDO systemctl daemon-reload >/dev/null 2>&1 || true
    fi

    $SUDO rm -f "$INSTALL_DIR/$BINARY_NAME"
    # 兼容旧版仓库安装布局
    $SUDO rm -rf "$SERVICE_DIR/bin" "$SERVICE_DIR/scripts"
    $SUDO rm -f "$SERVICE_ENV_FILE"

    if [ "$PURGE_DATA" -eq 1 ]; then
        $SUDO rm -rf "$SERVICE_DIR"
        if id "$APP_USER" >/dev/null 2>&1; then
            $SUDO userdel "$APP_USER" >/dev/null 2>&1 || true
        fi
        if getent group "$APP_GROUP" >/dev/null 2>&1; then
            $SUDO groupdel "$APP_GROUP" >/dev/null 2>&1 || true
        fi
        info "FRPC-Web 已卸载（包括数据）"
    else
        info "FRPC-Web 已卸载"
        if [ -d "$SERVICE_DIR/data" ]; then
            echo "数据保留在 $SERVICE_DIR/data，使用 --uninstall --purge-data 可一并删除"
        fi
    fi
}

# 显示使用说明
show_usage() {
    echo ""
    echo "======================================"
    echo "  ✅ FRPC-Web 安装完成！"
    echo "======================================"
    echo ""
    if [ "$SERVICE_INSTALLED" -eq 1 ]; then
        echo "📖 服务已在后台运行（开机自启）:"
        echo ""
        echo "  打开浏览器访问:  http://127.0.0.1:8080"
        echo "  （远程服务器可用 SSH 隧道: ssh -L 8080:127.0.0.1:8080 user@server）"
        echo ""
        echo "  用初始密钥登录（出厂默认 FrpcWeb-Init-9527），首次登录后按提示设置你自己的密码"
        echo "  （如需自定义初始密钥，在 $SERVICE_ENV_FILE 设置 FRPC_WEB_ACCESS_KEY 后重启）"
        echo ""
        echo "🔧 常用命令:"
        echo ""
        echo "  systemctl status frpc-web        # 查看状态"
        echo "  journalctl -u frpc-web -f        # 查看日志"
        echo "  sudo nano $SERVICE_ENV_FILE      # 修改配置"
        echo "  sudo systemctl restart frpc-web  # 改配置后重启生效"
        echo ""
        echo "  升级: 重跑本安装命令"
        echo "  卸载: curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | bash -s -- --uninstall"
    else
        echo "📖 快速开始:"
        echo ""
        echo "  1. 启动服务:"
        echo "     frpc-web"
        echo ""
        echo "  2. 打开浏览器访问:"
        echo "     http://127.0.0.1:8080"
        echo ""
        echo "  3. 用初始密钥登录（出厂默认 FrpcWeb-Init-9527），首次登录后设置你自己的密码"
        echo ""
        echo "🔧 环境变量配置（可选）:"
        echo ""
        echo "  export FRPC_WEB_ADDR=127.0.0.1:8080"
        echo "  export FRPC_WEB_DATA_DIR=/opt/frpc-web/data"
        echo "  export FRPC_WEB_ACCESS_KEY=your-secret-key"
    fi
    echo ""
    echo "📚 完整文档:"
    echo "  https://github.com/$REPO"
    echo ""
    echo "💬 反馈问题:"
    echo "  https://github.com/$REPO/issues"
    echo ""
}

# 主函数
main() {
    ACTION="install"
    PURGE_DATA=0
    for arg in "$@"; do
        case "$arg" in
            --uninstall)
                ACTION="uninstall"
                ;;
            --purge-data)
                PURGE_DATA=1
                ;;
            --help | -h)
                sed -n '3,22p' "$0" 2>/dev/null || true
                exit 0
                ;;
            *)
                error "未知参数: $arg（支持 --uninstall / --purge-data）"
                ;;
        esac
    done

    echo ""
    echo "======================================"
    echo "  FRPC-Web 安装脚本"
    echo "======================================"
    echo ""

    if [ "$ACTION" = "uninstall" ]; then
        uninstall
        return
    fi

    detect_os
    detect_arch
    if [ -n "${SOURCE_BIN:-}" ]; then
        use_local_binary
    else
        get_latest_version
        get_download_url
        download_binary
        verify_binary
    fi
    install_binary
    verify_installation
    setup_service
    show_usage
}

# 执行主函数
main "$@"
