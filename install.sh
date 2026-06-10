#!/usr/bin/env bash
set -e

# FRPC-Web 一键安装脚本
# 用法: curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash

REPO="sccens/frpc-web"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="frpc-web"

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

# 获取最新版本
get_latest_version() {
    info "获取最新版本..."

    # 尝试使用 curl
    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": *"[^"]*"' | sed 's/"tag_name": "\(.*\)"/\1/')
    # 尝试使用 wget
    elif command -v wget >/dev/null 2>&1; then
        VERSION=$(wget -qO- "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": *"[^"]*"' | sed 's/"tag_name": "\(.*\)"/\1/')
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
    info "下载地址: $DOWNLOAD_URL"
}

# 下载二进制文件
download_binary() {
    info "正在下载 $FILENAME..."

    TMP_FILE=$(mktemp)

    if command -v curl >/dev/null 2>&1; then
        if ! curl -fsSL -o "$TMP_FILE" "$DOWNLOAD_URL"; then
            error "下载失败"
        fi
    elif command -v wget >/dev/null 2>&1; then
        if ! wget -qO "$TMP_FILE" "$DOWNLOAD_URL"; then
            error "下载失败"
        fi
    fi

    info "下载完成"
}

# 验证二进制文件
verify_binary() {
    if [ ! -f "$TMP_FILE" ]; then
        error "下载的文件不存在"
    fi

    FILE_SIZE=$(stat -f%z "$TMP_FILE" 2>/dev/null || stat -c%s "$TMP_FILE" 2>/dev/null)
    if [ "$FILE_SIZE" -lt 1000000 ]; then
        error "下载的文件太小，可能下载失败"
    fi

    info "文件大小: $(echo "scale=2; $FILE_SIZE/1024/1024" | bc 2>/dev/null || echo "$FILE_SIZE bytes") MB"
}

# 安装二进制文件
install_binary() {
    info "正在安装到 $INSTALL_DIR/$BINARY_NAME..."

    # 检查是否需要 sudo
    if [ -w "$INSTALL_DIR" ]; then
        SUDO=""
    else
        if command -v sudo >/dev/null 2>&1; then
            SUDO="sudo"
            warn "需要管理员权限安装到 $INSTALL_DIR"
        else
            error "没有权限写入 $INSTALL_DIR，且未找到 sudo 命令"
        fi
    fi

    # 赋予执行权限
    chmod +x "$TMP_FILE"

    # 移动到安装目录
    if ! $SUDO mv "$TMP_FILE" "$INSTALL_DIR/$BINARY_NAME"; then
        error "安装失败"
    fi

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

# 显示使用说明
show_usage() {
    echo ""
    echo "======================================"
    echo "  ✅ FRPC-Web 安装完成！"
    echo "======================================"
    echo ""
    echo "📖 快速开始:"
    echo ""
    echo "  1. 启动服务:"
    echo "     $ frpc-web"
    echo ""
    echo "  2. 打开浏览器访问:"
    echo "     http://127.0.0.1:8080"
    echo ""
    echo "  3. 首次访问会进入初始化页面，设置访问密钥"
    echo ""
    echo "🔧 环境变量配置（可选）:"
    echo ""
    echo "  export FRPC_WEB_ADDR=127.0.0.1:8080"
    echo "  export FRPC_WEB_DATA_DIR=/opt/frpc-web/data"
    echo "  export FRPC_WEB_ACCESS_KEY=your-secret-key"
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
    echo ""
    echo "======================================"
    echo "  FRPC-Web 安装脚本"
    echo "======================================"
    echo ""

    detect_os
    detect_arch
    get_latest_version
    get_download_url
    download_binary
    verify_binary
    install_binary
    verify_installation
    show_usage
}

# 执行主函数
main
