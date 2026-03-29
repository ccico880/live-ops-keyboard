// live-ops-keyboard - 直播运营小键盘守护程序
// 系统级全局键盘钩子 → WebSocket Server → Chrome Extension Background
//
// 架构：
//   守护进程启动后，在 localhost:27531 监听 WebSocket 连接。
//   Chrome 扩展主动连接，守护进程实时推送键盘事件。
//   守护进程永久在后台运行（开机自启），不依赖 Chrome 来启动它。
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
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/net/websocket"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ─────────────────────────────────────────────────────────────────
// Windows API 常量 & 结构体
// ─────────────────────────────────────────────────────────────────

const (
	WH_KEYBOARD_LL = 13
	WM_KEYDOWN     = 0x0100
	WM_SYSKEYDOWN  = 0x0104

	VK_NUMPAD0 = 0x60
	VK_NUMPAD9 = 0x69
	VK_DECIMAL = 0x6E
	VK_ADD     = 0x6B // 小键盘 +（确认）
	VK_RETURN  = 0x0D // Enter
	VK_DELETE  = 0x2E // Delete（清空）
	VK_BACK    = 0x08 // Backspace（清空）

	LLKHF_EXTENDED = 0x01 // 小键盘 Enter 特有标志

	WS_PORT = "27531" // WebSocket 监听端口

	// WebSocket ping/pong 间隔 & 读超时
	wsPingInterval = 20 * time.Second
	wsReadTimeout  = 60 * time.Second
)

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// ─────────────────────────────────────────────────────────────────
// 键盘事件结构
// ─────────────────────────────────────────────────────────────────

type KeyEvent struct {
	Type   string `json:"type"`
	Key    string `json:"key"`
	Code   string `json:"code"`
	Digit  int    `json:"digit"`  // -1 表示非数字键
	Action string `json:"action"` // "digit" | "confirm" | "clear" | "ready"
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

// broadcast 向所有已连接的 Chrome 推送键盘事件
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
			log.Printf("[keyboard] WS 发送失败，关闭该连接: %v", err)
			c.Close()
			h.remove(c)
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// WebSocket 处理器（含 ping/pong 心跳）
// ─────────────────────────────────────────────────────────────────

func wsHandler(conn *websocket.Conn) {
	// 全局 panic 保护：任何连接 handler 的 panic 都记录日志，不崩进程
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[keyboard] WS handler panic recovered: %v\n%s", r, debug.Stack())
		}
	}()

	hub.add(conn)
	defer hub.remove(conn)
	defer conn.Close()

	// 发送就绪信号
	readyEvt := KeyEvent{Type: "KEYBOARD_READY", Key: "", Code: "", Digit: -1, Action: "ready"}
	readyData, _ := json.Marshal(readyEvt)
	conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	websocket.Message.Send(conn, string(readyData))

	// ── ping goroutine：每 20s 发一次 ping ─────────────────────────
	pingStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				pingMsg := `{"type":"PING"}`
				conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
				if err := websocket.Message.Send(conn, pingMsg); err != nil {
					log.Printf("[keyboard] ping 失败，关闭连接: %v", err)
					conn.Close()
					return
				}
			case <-pingStop:
				return
			}
		}
	}()
	defer close(pingStop)

	// ── 读循环：等待客户端 pong / 任意消息，超时 60s ──────────────
	for {
		var msg string
		conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		err := websocket.Message.Receive(conn, &msg)
		if err != nil {
			log.Printf("[keyboard] WS 读取断开: %v", err)
			break
		}
		// 收到 pong 或任意消息，重置计时（SetReadDeadline 下次循环会更新）
		log.Printf("[keyboard] WS 收到消息: %s", msg)
	}
}

// ─────────────────────────────────────────────────────────────────
// 键盘钩子全局状态
// ─────────────────────────────────────────────────────────────────

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessage          = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessage     = user32.NewProc("DispatchMessageW")

	hookHandle uintptr

	// 键盘事件队列：钩子回调只入队，绝不做 IO，防止阻塞消息泵
	eventCh = make(chan KeyEvent, 128)
)

// ─────────────────────────────────────────────────────────────────
// 键盘钩子回调
// ─────────────────────────────────────────────────────────────────

func keyboardProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 && (wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN) {
		ks := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		vk := ks.VkCode

		var evt *KeyEvent

		switch {
		case vk >= VK_NUMPAD0 && vk <= VK_NUMPAD9:
			digit := int(vk - VK_NUMPAD0)
			evt = &KeyEvent{Type: "KEY_EVENT", Digit: digit, Action: "digit"}

		case vk == VK_ADD:
			evt = &KeyEvent{Type: "KEY_EVENT", Key: "NumpadAdd", Code: "NumpadAdd", Digit: -1, Action: "confirm"}

		case vk == VK_RETURN && (ks.Flags&LLKHF_EXTENDED) != 0:
			// 小键盘 Enter（extended flag），普通 Enter 不触发
			evt = &KeyEvent{Type: "KEY_EVENT", Key: "NumpadEnter", Code: "NumpadEnter", Digit: -1, Action: "confirm"}

		case vk == VK_DELETE || vk == VK_BACK:
			evt = &KeyEvent{Type: "KEY_EVENT", Key: "Delete", Code: "Delete", Digit: -1, Action: "clear"}
		}

		if evt != nil {
			// 钩子回调必须极速返回（< 300ms），只入队，不做任何 IO
			select {
			case eventCh <- *evt:
			default:
				// channel 满则丢弃，绝不阻塞
			}
		}
	}

	ret, _, _ := procCallNextHookEx.Call(hookHandle, uintptr(nCode), wParam, lParam)
	return ret
}

// ─────────────────────────────────────────────────────────────────
// 事件消费 Worker（独立 goroutine，广播到 WebSocket 客户端）
// ─────────────────────────────────────────────────────────────────

func eventWorker() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[keyboard] eventWorker panic recovered: %v\n%s", r, debug.Stack())
			// 重启 worker
			go eventWorker()
		}
	}()
	for evt := range eventCh {
		// 补全 Key/Code 字段
		if evt.Digit >= 0 {
			evt.Key = fmt.Sprintf("Numpad%d", evt.Digit)
			evt.Code = fmt.Sprintf("Numpad%d", evt.Digit)
		}
		hub.broadcast(evt)
	}
}

// ─────────────────────────────────────────────────────────────────
// 安装/卸载 Windows 键盘钩子
// ─────────────────────────────────────────────────────────────────

func installHook() error {
	cb := windows.NewCallback(keyboardProc)
	h, _, err := procSetWindowsHookEx.Call(WH_KEYBOARD_LL, cb, 0, 0)
	if h == 0 {
		return fmt.Errorf("SetWindowsHookEx failed: %v", err)
	}
	hookHandle = h
	log.Printf("[keyboard] 键盘钩子安装成功 handle=0x%X", hookHandle)
	return nil
}

func uninstallHook() {
	if hookHandle != 0 {
		procUnhookWindowsHookEx.Call(hookHandle)
		hookHandle = 0
		log.Printf("[keyboard] 键盘钩子已卸载")
	}
}

// ─────────────────────────────────────────────────────────────────
// 消息泵（必须在主线程运行，WH_KEYBOARD_LL 依赖消息循环）
// 永不退出版：GetMessage 返回 0/-1 时记录日志并重新安装钩子，而非退出
// ─────────────────────────────────────────────────────────────────

func runMessagePump() {
	var msg MSG
	consecutiveErrors := 0
	for {
		ret, _, lastErr := procGetMessage.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)

		if ret == 0 {
			// WM_QUIT：正常终止消息，但守护进程不应退出
			// 卸载旧钩子，重新安装，继续消息泵
			log.Printf("[keyboard] 收到 WM_QUIT，重新安装键盘钩子...")
			uninstallHook()
			time.Sleep(200 * time.Millisecond)
			if err := installHook(); err != nil {
				log.Printf("[keyboard] 重新安装钩子失败: %v，1s 后重试", err)
				time.Sleep(1 * time.Second)
				consecutiveErrors++
				if consecutiveErrors > 10 {
					log.Printf("[keyboard] 连续失败 10 次，休眠 5s")
					time.Sleep(5 * time.Second)
					consecutiveErrors = 0
				}
			} else {
				consecutiveErrors = 0
				log.Printf("[keyboard] 键盘钩子重新安装成功，继续消息泵")
			}
			continue
		}

		if ret == ^uintptr(0) {
			// GetMessage 返回 -1：错误，记录但不退出
			log.Printf("[keyboard] GetMessage 返回 -1 (err=%v)，继续...", lastErr)
			consecutiveErrors++
			if consecutiveErrors > 20 {
				log.Printf("[keyboard] 连续错误过多，重新安装钩子")
				uninstallHook()
				time.Sleep(500 * time.Millisecond)
				installHook()
				consecutiveErrors = 0
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// 正常消息
		consecutiveErrors = 0
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// ─────────────────────────────────────────────────────────────────
// 开机自启注册
// ─────────────────────────────────────────────────────────────────

func registerStartup() {
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("[keyboard] 获取执行路径失败: %v", err)
		return
	}
	exePath, _ = filepath.Abs(exePath)

	k, _, err := registry.CreateKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE,
	)
	if err != nil {
		log.Printf("[keyboard] 注册开机自启失败: %v", err)
		return
	}
	defer k.Close()

	if err := k.SetStringValue("LiveOpsKeyboard", exePath); err != nil {
		log.Printf("[keyboard] 写注册表失败: %v", err)
		return
	}
	log.Printf("[keyboard] 开机自启已注册: %s", exePath)
}

// ─────────────────────────────────────────────────────────────────
// 端口检测：如果已有实例在跑，退出
// ─────────────────────────────────────────────────────────────────

func isPortInUse(port string) bool {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return true // 端口已被占用
	}
	ln.Close()
	return false
}

// ─────────────────────────────────────────────────────────────────
// main（全局 panic recover 保护）
// ─────────────────────────────────────────────────────────────────

func main() {
	// 全局 panic 保护，确保任何未捕获的 panic 都记录日志
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[keyboard] FATAL panic: %v\n%s", r, debug.Stack())
			// 不 os.Exit，让 defer uninstallHook 等清理运行
		}
	}()

	// 日志写到文件
	logDir := filepath.Join(os.Getenv("APPDATA"), "LiveOpsKeyboard")
	os.MkdirAll(logDir, 0755)
	logFile, err := os.OpenFile(
		filepath.Join(logDir, "keyboard.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err == nil {
		log.SetOutput(logFile)
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("[keyboard] ========== 启动 pid=%d ==========", os.Getpid())

	// 命令行参数
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--register-startup" {
		registerStartup()
		return
	}

	// 单实例检测：端口已被占用说明已有实例在运行，直接退出
	if isPortInUse(WS_PORT) {
		log.Printf("[keyboard] 端口 %s 已被占用，已有实例运行，退出", WS_PORT)
		return
	}

	// 注意：此程序用 -H windowsgui 编译，天然无控制台窗口

	// 安装全局键盘钩子
	if err := installHook(); err != nil {
		log.Fatalf("[keyboard] 安装键盘钩子失败: %v", err)
	}
	defer uninstallHook()

	// 事件消费 Worker
	go eventWorker()

	// 启动 WebSocket 服务器（goroutine，不阻塞主线程）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[keyboard] HTTP server panic: %v\n%s", r, debug.Stack())
			}
		}()

		mux := http.NewServeMux()
		mux.Handle("/keyboard", websocket.Handler(wsHandler))

		// 健康检查端点
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"status":"ok","clients":%d}`, hub.count())
		})

		addr := "127.0.0.1:" + WS_PORT
		log.Printf("[keyboard] WebSocket 服务器启动: ws://%s/keyboard", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("[keyboard] WebSocket 服务器启动失败: %v", err)
		}
	}()

	// 消息泵（必须在主线程运行，永不退出）
	log.Printf("[keyboard] 消息泵启动，等待键盘事件...")
	runMessagePump()

	log.Printf("[keyboard] 消息泵退出（不应该到这里），进程继续保活...")
	// 如果消息泵意外退出，用阻塞 channel 保持进程存活
	select {}
}
