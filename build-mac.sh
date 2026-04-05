#!/bin/bash
# build-mac.sh - 编译 macOS 版键盘守护程序
# 用法: ./build-mac.sh

set -e

echo "=========================================="
echo "  LiveOps Keyboard Daemon - macOS Build"
echo "=========================================="

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# 清理旧文件
echo "🧹 清理旧文件..."
rm -f live-ops-keyboard-mac

# 安装依赖
echo "📦 安装 Go 依赖..."
go get golang.org/x/net/websocket

# 编译（macOS, amd64 + arm64 通用二进制）
echo "🔨 编译 macOS 版本..."
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -o live-ops-keyboard-mac .

# 签名（可选，如果有开发者签名的话）
if [ -f "live-ops-keyboard-mac" ]; then
    echo "✅ 编译成功: live-ops-keyboard-mac"

    # 显示文件信息
    ls -lh live-ops-keyboard-mac
    file live-ops-keyboard-mac
fi

echo ""
echo "=========================================="
echo "✅ macOS 版本编译完成！"
echo "=========================================="
echo ""
echo "📋 使用方法："
echo "   ./live-ops-keyboard-mac                    # 运行守护程序"
echo "   ./live-ops-keyboard-mac --register-startup  # 注册开机自启"
echo "   ./live-ops-keyboard-mac --help              # 显示帮助"
echo ""
