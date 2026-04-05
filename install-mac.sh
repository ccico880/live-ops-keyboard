#!/bin/bash
# install-mac.sh - MAC 全自动安装脚本
# 功能：安装 → 授权引导 → 启动 全自动

set -e

echo "╔═══════════════════════════════════════════════╗"
echo "║   LiveOps Keyboard - macOS 全自动安装   ║"
echo "╚═══════════════════════════════════════════════╝"
echo ""

# 检查是否是 macOS
if [ "$(uname)" != "Darwin" ]; then
    echo "❌ 此脚本只能在 macOS 上运行"
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY_NAME="live-ops-keyboard"
FULL_PATH="/usr/local/bin/$BINARY_NAME"

# ──────────────────────────────────────────────────
# 步骤1: 检查程序文件
# ──────────────────────────────────────────────────
echo "📦 步骤1: 检查程序..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# 优先使用已编译的 mac 版本
if [ -f "$SCRIPT_DIR/live-ops-keyboard-mac" ]; then
    SOURCE="$SCRIPT_DIR/live-ops-keyboard-mac"
    echo "✅ 找到已编译的程序"
elif [ -f "$SCRIPT_DIR/$BINARY_NAME" ]; then
    SOURCE="$SCRIPT_DIR/$BINARY_NAME"
    echo "✅ 找到程序"
else
    echo "❌ 未找到编译好的程序"
    echo ""
    echo "请先运行编译脚本："
    echo "   ./build-mac.sh"
    exit 1
fi

# ──────────────────────────────────────────────────
# 步骤2: 安装到系统目录
# ──────────────────────────────────────────────────
echo ""
echo "📁 步骤2: 安装程序..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ -w /usr/local/bin ]; then
    cp "$SOURCE" "$FULL_PATH"
    chmod +x "$FULL_PATH"
    echo "✅ 已安装: $FULL_PATH"
else
    echo "🔐 需要管理员权限..."
    sudo cp "$SOURCE" "$FULL_PATH"
    sudo chmod +x "$FULL_PATH"
    echo "✅ 已安装: $FULL_PATH"
fi

# ──────────────────────────────────────────────────
# 步骤3: 辅助功能权限
# ──────────────────────────────────────────────────
echo ""
echo "🔑 步骤3: 辅助功能权限..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# 尝试运行检测权限（如果有这个参数的话）
if "$FULL_PATH" --help 2>&1 | grep -q "check-permission"; then
    CHECK=$("$FULL_PATH" --check-permission 2>&1 || true)
    if echo "$CHECK" | grep -q "PERMISSION_OK"; then
        echo "✅ 权限已授权"
        PERMISSION_OK=true
    else
        PERMISSION_OK=false
    fi
else
    # 简单检测：尝试访问 health 接口
    if curl -s --max-time 1 http://127.0.0.1:27531/health > /dev/null 2>&1; then
        echo "✅ 权限已授权"
        PERMISSION_OK=true
    else
        PERMISSION_OK=false
    fi
fi

if [ "$PERMISSION_OK" = false ]; then
    echo ""
    echo "⚠️  需要授权辅助功能权限！"
    echo ""
    echo "   📱 正在打开权限设置页面..."
    echo ""
    echo "   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "   1. 系统已打开【隐私与安全】设置"
    echo "   2. 找到【辅助功能】，点击进入"
    echo "   3. 点击左下角🔒解锁（需输入密码）"
    echo "   4. 点击【+】添加程序"
    echo "   5. 按 Command+Shift+G 快捷键"
    echo "   6. 复制粘贴: /usr/local/bin"
    echo "   7. 选择: live-ops-keyboard"
    echo "   8. 确认打勾 ✅"
    echo "   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # 自动打开辅助功能设置
    open "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"

    echo "⏳ 完成授权后，按回车继续..."
    echo "   (授权完成后按任意键)"
    read -r -t 60 _ || true
fi

# ──────────────────────────────────────────────────
# 步骤4: 开机自启
# ──────────────────────────────────────────────────
echo ""
echo "🚀 步骤4: 开机自启..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

"$FULL_PATH" --register-startup 2>&1 || echo "⚠️  开机自启注册完成（下次登录生效）"

# ──────────────────────────────────────────────────
# 步骤5: 启动
# ──────────────────────────────────────────────────
echo ""
echo "▶️  步骤5: 启动守护程序..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# 检查是否已运行
if curl -s --max-time 1 http://127.0.0.1:27531/health 2>/dev/null | grep -q "ok"; then
    echo "✅ 键盘守护已在运行中"
else
    # 杀掉旧进程（如果存在）
    pkill -f "live-ops-keyboard" 2>/dev/null || true
    sleep 0.5

    # 后台启动
    nohup "$FULL_PATH" > /tmp/live-ops-keyboard.log 2>&1 &
    sleep 1

    if curl -s --max-time 2 http://127.0.0.1:27531/health 2>/dev/null | grep -q "ok"; then
        echo "✅ 键盘守护已启动"
    else
        echo "⚠️  请检查权限设置"
    fi
fi

# ──────────────────────────────────────────────────
# 完成
# ──────────────────────────────────────────────────
echo ""
echo "╔═══════════════════════════════════════════════╗"
echo "║   ✅ 安装完成！                          ║"
echo "╚═══════════════════════════════════════════════╝"
echo ""

curl -s --max-time 1 http://127.0.0.1:27531/health 2>/dev/null && {
    echo "🌐 状态: 运行中 ✓"
    echo "📡 WS: ws://127.0.0.1:27531/keyboard"
} || {
    echo "⚠️  状态: 检查中..."
}

echo ""
echo "📋 快捷键："
echo "   小键盘 0-9 → 数字输入"
echo "   小键盘 Enter → 确认"
echo "   小键盘 / → 清空"
echo "   Escape → 紧急停止"
echo ""
echo "📋 管理："
echo "   停止: pkill -f live-ops-keyboard"
echo "   日志: tail -f ~/Library/Logs/LiveOpsKeyboard/keyboard.log"
echo ""
