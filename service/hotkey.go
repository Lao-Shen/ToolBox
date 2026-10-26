package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lxn/win"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// 全局快捷键（热键）业务

// ShortcutBinding 快捷键绑定
type ShortcutBinding struct {
	Name     string `json:"name"`     // 工具名
	Action   string `json:"action"`   // 行为标识（screenshot 等）
	Workdir  string `json:"workdir"`  // 工作路径
	Shortcut string `json:"shortcut"` // 如 "Ctrl+Alt+S"（空=不注册）
}

// lxn/win 与 x/sys 均无 RegisterHotKey，直接 syscall 调 user32
var (
	user32DLL             = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey    = user32DLL.NewProc("RegisterHotKey")
	procUnregisterHotKey  = user32DLL.NewProc("UnregisterHotKey")
	procPostThreadMessage = user32DLL.NewProc("PostThreadMessageW")
)

// Windows MOD_* 修饰键位掩码
const (
	modAlt      = 0x1
	modControl  = 0x2
	modShift    = 0x4
	modWin      = 0x8
	modNoRepeat = 0x4000
)

const (
	wmHotkey        = 0x0312 // WM_HOTKEY
	wmHotkeyCommand = 0x8001 // WM_APP+1：唤醒热键线程执行注册命令
)

// ToolActionRunner 工具行为执行器（workdir 为工具工作路径）
type ToolActionRunner func(workdir string)

// HotkeyService 全局热键服务。
// 关键约束：WM_HOTKEY 只会投递到调用 RegisterHotKey 的那个线程的消息队列，
// 因此注册/注销必须由专用热键线程自己执行——SetBindings 通过
// PostThreadMessage 唤醒线程处理（wmHotkeyCommand），注册结果经 channel 回传。
type HotkeyService struct {
	mu        sync.Mutex
	bindings  map[int32]ShortcutBinding
	pending   []ShortcutBinding
	actions   map[string]ToolActionRunner // action → 后端执行器
	startOnce sync.Once
	ready     chan struct{}
	result    chan error
	threadID  uint32
}

// registerAction 注册 action 对应的后端行为（新增工具只需一行注册）
func (h *HotkeyService) registerAction(action string, r ToolActionRunner) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.actions == nil {
		h.actions = make(map[string]ToolActionRunner)
	}
	h.actions[action] = r
}

// GetShortcuts 读取已持久化的快捷键（工具名 → 快捷键；文件不存在返回空表）
func (h *HotkeyService) GetShortcuts() (map[string]string, error) {
	path, err := shortcutsFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

// saveShortcuts 持久化快捷键到 %APPDATA%\ToolBox\shortcuts.json
func saveShortcuts(save map[string]string) error {
	path, err := shortcutsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(save, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// shortcutsFilePath 快捷键持久化文件路径
func shortcutsFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ToolBox", "shortcuts.json"), nil
}

// SetBindings 全量同步快捷键绑定：先注销全部旧的，再注册新的
func (h *HotkeyService) SetBindings(bindings []ShortcutBinding) error {
	h.startOnce.Do(func() {
		h.ready = make(chan struct{})
		h.result = make(chan error, 1)
		go h.runLoop()
	})
	<-h.ready // 等热键线程就绪（消息队列已建立）

	h.mu.Lock()
	h.pending = bindings
	h.mu.Unlock()

	procPostThreadMessage.Call(uintptr(h.threadID), wmHotkeyCommand, 0, 0)

	select {
	case err := <-h.result:
		return err
	case <-time.After(3 * time.Second):
		return fmt.Errorf("热键线程无响应")
	}
}

// runLoop 热键专用线程：接收 WM_HOTKEY 分发、处理注册命令
func (h *HotkeyService) runLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	h.threadID = win.GetCurrentThreadId()

	var msg win.MSG
	win.PeekMessage(&msg, 0, 0, 0, win.PM_NOREMOVE) // 先建立消息队列，保证 PostThreadMessage 可达
	close(h.ready)

	for win.GetMessage(&msg, 0, 0, 0) > 0 {
		switch {
		case msg.Message == wmHotkeyCommand:
			h.applyBindings()
		case msg.Message == wmHotkey:
			h.mu.Lock()
			b, ok := h.bindings[int32(msg.WParam)]
			h.mu.Unlock()
			if ok {
				h.dispatch(b)
			}
		default:
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
	}
}

// applyBindings 在热键线程上执行注册/注销（与 WM_HOTKEY 投递同线程）
func (h *HotkeyService) applyBindings() {
	h.mu.Lock()
	pending := h.pending
	h.mu.Unlock()

	// 注销旧的
	for id := range h.bindings {
		procUnregisterHotKey.Call(0, uintptr(id))
	}
	h.mu.Lock()
	h.bindings = make(map[int32]ShortcutBinding)
	h.mu.Unlock()

	var resultErr error
	for i, b := range pending {
		if b.Shortcut == "" {
			continue
		}
		mods, vk, err := parseShortcut(b.Shortcut)
		if err != nil {
			resultErr = fmt.Errorf("「%s」快捷键 %s 无效: %w", b.Name, b.Shortcut, err)
			break
		}
		id := int32(i + 1)
		ret, _, _ := procRegisterHotKey.Call(0, uintptr(id), uintptr(mods), uintptr(vk))
		if ret == 0 {
			resultErr = fmt.Errorf("「%s」快捷键 %s 注册失败（可能已被其他程序占用）", b.Name, b.Shortcut)
			break
		}
		h.bindings[id] = b
	}

	// 持久化已成功注册的快捷键（清除快捷键 = 绑定里没有该工具）
	save := make(map[string]string, len(h.bindings))
	for _, b := range h.bindings {
		save[b.Name] = b.Shortcut
	}
	if err := saveShortcuts(save); err != nil {
		log.Printf("save shortcuts failed: %v", err)
	}

	h.result <- resultErr
}

// dispatch 热键触发工具：按 action 查注册表分发，未注册的走前端事件提示
func (h *HotkeyService) dispatch(b ShortcutBinding) {
	h.mu.Lock()
	run := h.actions[b.Action]
	h.mu.Unlock()
	if run != nil {
		// 异步执行避免阻塞热键线程（截图/开窗口流程耗时长）
		go run(b.Workdir)
		return
	}
	application.Get().Event.Emit("hotkey", b.Name)
}

// ConfigureHotkeyActions 注册各工具的后端触发行为（main.go 启动时调用）。
// 新增工具只需在此加一行 registerAction；未注册的 action 自动落前端 hotkey 事件提示。
func ConfigureHotkeyActions(h *HotkeyService, screenshot *ScreenshotService, startup *StartupService, contextmenu *ContextMenuService, iconExtract *IconExtractService) {
	h.registerAction("screenshot", func(workdir string) {
		// 截图走后端全流程（隐藏窗口→抓屏→蒙层→保存）
		if _, err := screenshot.CaptureRegion(workdir); err != nil {
			log.Printf("hotkey screenshot failed: %v", err)
		}
	})
	h.registerAction("startup", func(string) {
		startup.ToggleManager() // 开/关切换，避免重复启动
	})
	h.registerAction("contextmenu", func(string) {
		contextmenu.ToggleManager() // 开/关切换，避免重复启动
	})
	h.registerAction("iconextract", func(string) {
		iconExtract.ToggleManager() // 开/关切换，避免重复启动
	})
}

// parseShortcut 解析 "Ctrl+Alt+S" → 修饰键位掩码 + 虚拟键码
func parseShortcut(s string) (mods, vk uint32, err error) {
	for _, part := range strings.Split(s, "+") {
		switch part {
		case "Ctrl":
			mods |= modControl
		case "Alt":
			mods |= modAlt
		case "Shift":
			mods |= modShift
		case "Win":
			mods |= modWin
		default:
			if vk != 0 {
				return 0, 0, fmt.Errorf("多余主键")
			}
			vk, err = keyVK(part)
			if err != nil {
				return 0, 0, err
			}
		}
	}
	if vk == 0 || mods == 0 {
		return 0, 0, fmt.Errorf("需要 修饰键+主键 组合")
	}
	return mods | modNoRepeat, vk, nil
}

// keyVK 主键显示名 → 虚拟键码（与前端 ShortcutDialog 的 normalizeKey 对应）
func keyVK(name string) (uint32, error) {
	rs := []rune(name)
	if len(rs) == 1 {
		switch {
		case rs[0] >= 'A' && rs[0] <= 'Z':
			return uint32(rs[0]), nil
		case rs[0] >= 'a' && rs[0] <= 'z':
			return uint32(rs[0] - 32), nil
		case rs[0] >= '0' && rs[0] <= '9':
			return uint32(rs[0]), nil
		}
	}
	switch name {
	case "Space":
		return 0x20, nil
	case "Enter":
		return 0x0D, nil
	case "Backspace":
		return 0x08, nil
	case "Delete":
		return 0x2E, nil
	case "Esc":
		return 0x1B, nil
	case "↑":
		return 0x26, nil // VK_UP
	case "↓":
		return 0x28, nil // VK_DOWN
	case "←":
		return 0x25, nil // VK_LEFT
	case "→":
		return 0x27, nil // VK_RIGHT
	case "Home":
		return 0x24, nil
	case "End":
		return 0x23, nil
	case "PageUp":
		return 0x21, nil
	case "PageDown":
		return 0x22, nil
	case "Tab":
		return 0x09, nil
	}
	if strings.HasPrefix(name, "F") {
		var n int
		if _, e := fmt.Sscanf(name, "F%d", &n); e == nil && n >= 1 && n <= 24 {
			return uint32(0x70 + n - 1), nil
		}
	}
	return 0, fmt.Errorf("不支持的按键 %s", name)
}
