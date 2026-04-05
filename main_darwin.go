// live-ops-keyboard - 直播运营小键盘守护程序 (macOS)
// 系统级全局键盘钩子 → WebSocket Server → Chrome Extension Background
//
// 架构：
//   守护进程启动后，在 localhost:27531 监听 WebSocket 连接。
//   Chrome 扩展主动连接，守护进程实时推送键盘事件。
//   守护进程永久在后台运行（开机自启）。
//
// macOS 实现：直接使用 CGEvent API（CGO）
//   - 需要用户授予"辅助功能"权限
//
// 消息格式（JSON）：
//   {"type":"KEY_EVENT","key":"Numpad1","code":"Numpad1","digit":1,"action":"digit"}
//   {"type":"KEYBOARD_READY","key":"","code":"","digit":-1,"action":"ready"}
//   {"type":"PING"} / {"type":"PONG"}

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"
)

// ─────────────────────────────────────────────────────────────────
// CGO: macOS CoreGraphics
// ─────────────────────────────────────────────────────────────────

/*
#cgo darwin CFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
#include <mach/mach.h>

// 按键码定义
#define KEYCODE_NUMPAD0    0x52
#define KEYCODE_NUMPAD1    0x53
#define KEYCODE_NUMPAD2    0x54
#define KEYCODE_NUMPAD3    0x55
#define KEYCODE_NUMPAD4    0x56
#define KEYCODE_NUMPAD5    0x57
#define KEYCODE_NUMPAD6    0x58
#define KEYCODE_NUMPAD7    0x59
#define KEYCODE_NUMPAD8    0x5A
#define KEYCODE_NUMPAD9    0x5B
#define KEYCODE_NUMPAD_ENTER 0x4C
#define KEYCODE_NUMPAD_DIVIDE 0x54
#define KEYCODE_ESCAPE     0x35
#define KEYCODE_DELETE     0x33
#define KEYCODE_FORWARD_DELETE 0x75

// 全局回调函数指针
static void (*g_keyboardCallback)(int) = NULL;
static CGEventTapRef g_tap = NULL;

// CGEvent tap 回调
CGEventRef myTapCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon) {
    // 处理 tap 被禁用的情况
    if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
        if (g_tap != NULL) {
            CGEventTapEnable(g_tap, true);
        }
        return event;
    }

    // 只处理键盘按下事件
    if (type != kCGEventKeyDown) {
        return event;
    }

    // 获取按键码
    int64_t keyCode = CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);

    // 调用 Go 回调
    if (g_keyboardCallback != NULL) {
        g_keyboardCallback((int)keyCode);
    }

    return event;
}

// 导出函数：设置回调
void setKeyboardCallback(void* cb) {
    g_keyboardCallback = (void (*)(int))cb;
}

// 导出函数：创建并启动 tap
void* createEventTap() {
    // 需要辅助功能权限才能监控键盘事件
    CGEventMask eventMask = CGEventMaskBit(kCGEventKeyDown);

    // kCGHIDEventTap 需要 SIP 关闭或辅助功能权限
    // 如果失败，尝试 kCGSessionEventTap
    g_tap = CGEventTapCreate(
        kCGHIDEventTap,
        kCGHeadInsertEventTap,
        kCGDefaultTapCreationPolicy,
        eventMask,
        myTapCallback,
        NULL
    );

    if (g_tap == NULL) {
        // 尝试 session tap
        g_tap = CGEventTapCreate(
            kCGSessionEventTap,
            kCGHeadInsertEventTap,
            kCGDefaultTapCreationPolicy,
            eventMask,
            myTapCallback,
            NULL
        );
    }

    if (g_tap != NULL) {
        CGEventTapEnable(g_tap, true);
    }

    return g_tap;
}

// 导出函数：启用/禁用 tap
void enableTap(bool enable) {
    if (g_tap != NULL) {
        CGEventTapEnable(g_tap, enable);
    }
}

// 导出函数：获取 tap 状态
bool isTapEnabled() {
    if (g_tap == NULL) return false;
    return CGEventTapIsEnabled(g_tap);
}

// 导出函数：添加到 runloop
void addToRunLoop() {
    if (g_tap != NULL) {
        CFRunLoopSourceRef source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, g_tap, 0);
        CFRunLoopAddSource(CFRunLoopGetCurrent(), source, kCFRunLoopCommonModes);
        CFRunLoopRun();
    }
}
*/
import "C"

// ─────────────────────────────────────────────────────────────────
// 常量
// ─────────────────────────────────────────────────────────────────

const (
	WS_PORT = "27531"

	wsPingInterval = 20 * time.Second
	wsReadTimeout  = 60 * time.Second
)

// ─────────────────────────────────────────────────────────────────
// 键盘事件结构
// ─────────────────────────────────────────────────────────────────

type KeyEvent struct {
	Type   string `json:"type"`
	Key    string `json:"key"`
	Code   string `json:"code"`
	Digit  int    `json:"digit"`
	Action string `json:"action"`
}

// ─────────────────────────────────────────────────────────────────
// WebSocket 客户端管理
// ─────────────────────────────────────────────────────────────────

type wsHub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
}

var hub = &wsHub{
	clients: make(map[*websocket.Conn]struct{}),
}

func (h *wsHub) add(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
	log.Printf("[keyboard] WS 客户端连接，当前连接数: %d", h.count())
}

func (h *wsHub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	log.Printf("[keyboard] WS 客户端断开，当前连接数: %d", h.count())
}

func (h *wsHub) count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *wsHub) broadcast(evt KeyEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		log.Printf("[keyboard] JSON 序列化失败: %v", err)
		return
	}

	h.mu.RLock()
	conns := make([]*websocket.Conn, 0, len(h.clients))
	for c := range h.clients {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	for _, c := range conns {
		c.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		if err := websocket.Message.Send(c, string(data)); err != nil {
			log.Printf("[keyboard] WS 发送失败: %v", err)
			c.Close()
			h.remove(c)
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// WebSocket 处理器
// ─────────────────────────────────────────────────────────────────

func wsHandler(conn *websocket.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[keyboard] WS handler panic: %v", r)
		}
	}()

	hub.add(conn)
	defer hub.remove(conn)
	defer conn.Close()

	// 发送就绪信号
	readyEvt := KeyEvent{Type: "KEYBOARD_READY", Digit: -1, Action: "ready"}
	readyData, _ := json.Marshal(readyEvt)
	conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	websocket.Message.Send(conn, string(readyData))
	log.Printf("[keyboard] 发送 READY 信号")

	// ping goroutine
	pingStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
				if err := websocket.Message.Send(conn, `{"type":"PING"}`); err != nil {
					conn.Close()
					return
				}
			case <-pingStop:
				return
			}
		}
	}()
	defer close(pingStop)

	// 读循环
	for {
		var msg string
		conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		if err := websocket.Message.Receive(conn, &msg); err != nil {
			break
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// CGEvent 回调处理
// ─────────────────────────────────────────────────────────────────

var (
	eventCh   = make(chan KeyEvent, 128)
	keyCount  uint32
)

//export keyboardCallback
func keyboardCallback(keyCode C.int) {
	n := int(keyCode)

	if atomic.AddUint32(&keyCount, 1) <= 30 {
		log.Printf("[keyboard] 按键: keyCode=%d", n)
	}

	var evt *KeyEvent

	switch n {
	case 0x52: // Numpad0
		evt = &KeyEvent{Type: "KEY_EVENT", Digit: 0, Action: "digit"}
	case 0x53: // Numpad1
		evt = &KeyEvent{Type: "KEY_EVENT", Digit: 1, Action: "digit"}
	case 0x54: // Numpad2 或 NumpadDivide
		evt = &KeyEvent{Type: "KEY_EVENT", Digit: 2, Action: "digit"}
	case 0x55: // Numpad3
		evt = &KeyEvent{Type: "KEY_EVENT", Digit: 3, Action: "digit"}
	case 0x56: // Numpad4
		evt = &KeyEvent{Type: "KEY_EVENT", Digit: 4, Action: "digit"}
	case 0x57: // Numpad5
		evt = &KeyEvent{Type: "KEY_EVENT", Digit: 5, Action: "digit"}
	case 0x58: // Numpad6
		evt = &KeyEvent{Type: "KEY_EVENT", Digit: 6, Action: "digit"}
	case 0x59: // Numpad7
		evt = &KeyEvent{Type: "KEY_EVENT", Digit: 7, Action: "digit"}
	case 0x5A: // Numpad8
		evt = &KeyEvent{Type: "KEY_EVENT", Digit: 8, Action: "digit"}
	case 0x5B: // Numpad9
		evt = &KeyEvent{Type: "KEY_EVENT", Digit: 9, Action: "digit"}
	case 0x4C: // NumpadEnter
		evt = &KeyEvent{Type: "KEY_EVENT", Key: "NumpadEnter", Code: "NumpadEnter", Digit: -1, Action: "confirm"}
	case 0x35: // Escape
		log.Printf("[keyboard] Escape 被捕获，发送 stop")
		evt = &KeyEvent{Type: "KEY_EVENT", Key: "Escape", Code: "Escape", Digit: -1, Action: "stop"}
	case 0x33, 0x75: // Delete, ForwardDelete
		evt = &KeyEvent{Type: "KEY_EVENT", Key: "Delete", Code: "Delete", Digit: -1, Action: "clear"}
	}

	if evt != nil {
		select {
		case eventCh <- *evt:
		default:
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// 事件消费 Worker
// ─────────────────────────────────────────────────────────────────

func eventWorker() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[keyboard] eventWorker panic: %v\n%s", r, debug.Stack())
			go eventWorker()
		}
	}()

	for evt := range eventCh {
		if evt.Digit >= 0 {
			evt.Key = fmt.Sprintf("Numpad%d", evt.Digit)
			evt.Code = fmt.Sprintf("Numpad%d", evt.Digit)
		}
		hub.broadcast(evt)
	}
}

// ─────────────────────────────────────────────────────────────────
// 端口检测
// ─────────────────────────────────────────────────────────────────

func isPortInUse(port string) bool {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return true
	}
	ln.Close()
	return false
}

// ─────────────────────────────────────────────────────────────────
// launchd plist
// ─────────────────────────────────────────────────────────────────

func getPlistPath() string {
	usr, _ := user.Current()
	return filepath.Join(usr.HomeDir, "Library", "LaunchAgents", "com.liveops.keyboard.plist")
}

func registerStartup() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取执行路径失败: %v", err)
	}
	absPath, _ := filepath.Abs(exePath)

	plistDir := filepath.Dir(getPlistPath())
	os.MkdirAll(plistDir, 0755)

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.liveops.keyboard</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/live-ops-keyboard.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/live-ops-keyboard.error.log</string>
</dict>
</plist>
`, absPath)

	if err := os.WriteFile(getPlistPath(), []byte(plist), 0644); err != nil {
		return fmt.Errorf("写入 plist 失败: %v", err)
	}

	fmt.Printf("✅ 开机自启已配置: %s\n", getPlistPath())
	fmt.Printf("📋 加载命令: launchctl load %s\n", getPlistPath())
	fmt.Printf("📋 重启后自动生效\n")

	return nil
}

func uninstallStartup() {
	plistPath := getPlistPath()
	if _, err := os.Stat(plistPath); err == nil {
		os.Remove(plistPath)
		fmt.Println("✅ 已移除开机自启")
	}
}

// ─────────────────────────────────────────────────────────────────
// main
// ─────────────────────────────────────────────────────────────────

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[keyboard] FATAL: %v\n%s", r, debug.Stack())
		}
	}()

	// 日志
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		usr, _ := user.Current()
		homeDir = usr.HomeDir
	}
	logDir := filepath.Join(homeDir, "Library", "Logs", "LiveOpsKeyboard")
	os.MkdirAll(logDir, 0755)
	logFile, _ := os.OpenFile(
		filepath.Join(logDir, "keyboard.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if logFile != nil {
		log.SetOutput(logFile)
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("[keyboard] ========== 启动 pid=%d ==========", os.Getpid())

	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║   LiveOps Keyboard Daemon (macOS)            ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")

	// 参数处理
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "--register-startup":
			if err := registerStartup(); err != nil {
				fmt.Fprintf(os.Stderr, "❌ 失败: %v\n", err)
				os.Exit(1)
			}
			return
		case "--uninstall-startup":
			uninstallStartup()
			return
		case "--check-permission":
			// 检查辅助功能权限
			C.setKeyboardCallback(C.CGoCallbackProc(keyboardCallback))
			C.createEventTap()
			if C.g_tap != nil {
				C.enableTap(false)
				fmt.Println("PERMISSION_OK")
			} else {
				fmt.Println("PERMISSION_DENIED")
			}
			return
		case "--help":
			fmt.Println("\n📖 用法:")
			fmt.Println("  live-ops-keyboard              运行守护程序")
			fmt.Println("  live-ops-keyboard --register-startup   注册开机自启")
			fmt.Println("  live-ops-keyboard --uninstall-startup  移除开机自启")
			fmt.Println("  live-ops-keyboard --check-permission   检查权限状态")
			fmt.Println("  live-ops-keyboard --help              显示帮助")
			return
		}
	}

	// 单实例检测
	if isPortInUse(WS_PORT) {
		fmt.Println("ℹ️  键盘守护已在运行（端口 27531 被占用）")
		return
	}

	// 设置键盘回调
	C.setKeyboardCallback(C.CGoCallbackProc(keyboardCallback))

	// 安装 CGEvent tap
	C.createEventTap()
	if C.g_tap == nil {
		fmt.Println("")
		fmt.Println("⚠️  需要辅助功能权限！")
		fmt.Println("")
		fmt.Println("请按以下步骤操作：")
		fmt.Println("1. 系统偏好设置 → 隐私与安全 → 隐私 → 辅助功能")
		fmt.Println("2. 点击🔒解锁")
		fmt.Println("3. 点击「+」添加 live-ops-keyboard")
		fmt.Println("4. 确保允许")
		fmt.Println("5. 重新运行此程序")
		fmt.Println("")
		os.Exit(1)
	}
	defer C.enableTap(false)

	log.Printf("[keyboard] CGEvent tap 启动成功")
	fmt.Printf("✅ 键盘监控已启动\n")

	// 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, os.Kill)
	go func() {
		<-sigCh
		fmt.Println("\n👋 已停止")
		os.Exit(0)
	}()

	// 事件 Worker
	go eventWorker()

	// WebSocket 服务器
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/keyboard", websocket.Handler(wsHandler))
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"status":"ok","clients":%d}`, hub.count())
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "LiveOps Keyboard Daemon\nWS: ws://127.0.0.1:%s/keyboard", WS_PORT)
		})

		addr := "127.0.0.1:" + WS_PORT
		log.Printf("[keyboard] WebSocket: ws://%s/keyboard", addr)
		http.ListenAndServe(addr, mux)
	}()

	fmt.Printf("🌐 WebSocket: ws://127.0.0.1:%s/keyboard\n", WS_PORT)
	fmt.Println("📋 支持: 小键盘0-9, Enter, /, Escape, Backspace")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("按 Ctrl+C 退出")

	// 运行 run loop
	C.addToRunLoop()
}
