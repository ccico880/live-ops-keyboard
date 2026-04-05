# LiveOps Keyboard - macOS 版

## 功能说明

macOS 版键盘守护程序，与 Windows 版功能完全一致：

| 功能 | 描述 |
|------|------|
| 全局键盘监控 | 即使浏览器未激活也能捕获按键 |
| WebSocket 通信 | 端口 27531，实时推送键盘事件 |
| 后台运行 | 无窗口、无Dock图标 |
| 开机自启 | 使用 launchd plist |

## 支持的快捷键

| 快捷键 | 功能 |
|--------|------|
| 小键盘 0-9 | 数字输入 |
| 小键盘 Enter | 确认 |
| 小键盘 / | 清空缓冲（连按3次=全局停止） |
| Escape | 紧急停止 |
| Backspace/Delete | 清空缓冲 |

---

## 一键安装指南（小白版）

### 第一步：编译 macOS 版本

**在 Mac 上打开终端，执行以下命令：**

```bash
# 1. 进入项目目录（请根据实际情况调整路径）
cd ~/path/to/live-ops-keyboard

# 2. 给编译脚本添加执行权限
chmod +x build-mac.sh

# 3. 运行编译脚本
./build-mac.sh
```

**预期输出：**
```
==========================================
  LiveOps Keyboard Daemon - macOS Build
==========================================
🧹 清理旧文件...
📦 安装 Go 依赖...
🔨 编译 macOS 版本...
✅ 编译成功: live-ops-keyboard-mac
==========================================
```

### 第二步：一键安装

```bash
# 给安装脚本添加执行权限
chmod +x install-mac.sh

# 运行安装脚本
./install-mac.sh
```

**预期输出：**
```
╔═══════════════════════════════════════════════╗
║   LiveOps Keyboard - macOS 安装向导        ║
╚═══════════════════════════════════════════════╝

📦 步骤1: 检查二进制文件... ✅
🔑 步骤2: 添加执行权限... ✅
📁 步骤3: 安装到系统目录... ✅
🚀 步骤4: 注册开机自启... ✅
✅ 开机自启已配置: /Users/xxx/Library/LaunchAgents/com.liveops.keyboard.plist
📋 加载命令: launchctl load ...

╔═══════════════════════════════════════════════╗
║   ✅ 安装成功！键盘守护已启动              ║
╚═══════════════════════════════════════════════╝
```

### 第三步：授权辅助功能权限（重要！）

⚠️ **这一步必须做，否则键盘监控不工作！**

1. 点击屏幕左上角 🍎 → **系统设置**
2. 选择 **隐私与安全**（或 **系统偏好设置** → **安全性与隐私**）
3. 滚动到 **辅助功能**，点击它
4. 点击左下角的 🔒 图标，输入密码解锁
5. 点击 **「+」** 按钮
6. 在弹出的文件选择器中，按 `Command + Shift + G`
7. 输入 `/usr/local/bin/`，点击前往
8. 选择 `live-ops-keyboard`，点击打开
9. 确保列表中 `live-ops-keyboard` 前面打勾 ✅

### 第四步：验证是否正常工作

在终端执行：

```bash
# 检查守护进程状态
curl http://127.0.0.1:27531/health

# 预期输出：
{"status":"ok","clients":0}
```

---

## 常见问题

### Q: 编译失败，提示 "go: command not found"
**A:** 需要安装 Go SDK
```bash
# 使用 Homebrew 安装
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
brew install go
```

### Q: 键盘不工作，按键没反应
**A:** 检查辅助功能权限 - 必须授权才能监控键盘！

### Q: 提示 "端口 27531 被占用"
**A:** 已有实例在运行，无需再次启动

---

## 手动命令参考

```bash
# 启动守护程序
/usr/local/bin/live-ops-keyboard &

# 注册开机自启
/usr/local/bin/live-ops-keyboard --register-startup

# 检查状态
curl http://127.0.0.1:27531/health

# 完全卸载
pkill -f live-ops-keyboard
/usr/local/bin/live-ops-keyboard --uninstall-startup
sudo rm /usr/local/bin/live-ops-keyboard
```

---

**作者**: LiveOps Team
**日期**: 2026-04-06
