#!/bin/sh
set -e

REPO="wuhanyuhan/ks-devkit"
INSTALL_DIR="${KS_INSTALL_DIR:-$HOME/.ks/bin}"

# 检测系统
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "不支持的架构: $ARCH"; exit 1 ;;
esac

# 获取最新版本
if [ -z "$KS_VERSION" ]; then
    KS_VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed 's/.*"v//' | sed 's/".*//')
fi

if [ -z "$KS_VERSION" ]; then
    echo "无法获取最新版本号"
    exit 1
fi

# 下载
FILENAME="ks_${KS_VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/v${KS_VERSION}/${FILENAME}"

echo "下载 ks v${KS_VERSION} (${OS}/${ARCH})..."
TMP=$(mktemp -d)
curl -fsSL "$URL" -o "$TMP/$FILENAME"

# 校验（如果 checksums 可用）
CHECKSUM_URL="https://github.com/$REPO/releases/download/v${KS_VERSION}/checksums.txt"
if curl -fsSL "$CHECKSUM_URL" -o "$TMP/checksums.txt" 2>/dev/null; then
    cd "$TMP"
    if command -v sha256sum >/dev/null 2>&1; then
        grep "$FILENAME" checksums.txt | sha256sum -c --quiet
        echo "SHA256 校验通过"
    fi
    cd - >/dev/null
fi

# 解压安装
mkdir -p "$INSTALL_DIR"
tar -xzf "$TMP/$FILENAME" -C "$INSTALL_DIR"
rm -rf "$TMP"

echo ""
echo "✓ ks v${KS_VERSION} 已安装到 ${INSTALL_DIR}/ks"

# 检查 PATH
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) echo ""
       echo "请将以下内容添加到你的 shell 配置文件："
       echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
       ;;
esac
