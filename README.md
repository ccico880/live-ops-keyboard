# 直播运营小键盘守护程序

## 功能
系统级全局键盘监听 → Chrome 扩展 Native Messaging → 双平台弹窗

无论浏览器是否激活、最小化、标签在后台，按小键盘均可稳定触发弹窗。

## 安装（一次性）

1. 把 `live-ops-keyboard.exe` 和 `install.bat` 放在同一目录
2. 双击 `install.bat`
3. 若无法自动读取扩展 ID，打开 `chrome://extensions` 找到"直播运营自动化助手"的 ID 手动粘贴
4. 重新加载 Chrome 插件

## 键位说明

| 按键 | 功能 |
|------|------|
| 小键盘 1-9 | 输入商品序号（支持多位数，如 1→2→5 = 第125号） |
| 小键盘 0 | 输入 0（用于组合多位数） |
| 小键盘 + | 立即确认（不等超时） |
| 小键盘 Delete | 清空当前输入缓冲 |
| 无操作 1.5s | 自动确认当前缓冲 |

## 日志位置
`%APPDATA%\LiveOpsKeyboard\keyboard.log`

## 卸载
双击 `uninstall.bat`
