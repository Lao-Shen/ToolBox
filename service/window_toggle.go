package service

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// 窗口型工具的单实例开关

// WindowToggle 记录工具窗口实例并在开/关间切换：
// 热键第一次触发打开窗口，再次触发关闭（等价于点 X），避免重复启动；
// 用户自行关闭窗口时由 WindowClosing 钩子清掉引用，下次触发重新打开。
// 新增窗口型工具时嵌入此结构即可获得该行为。
type WindowToggle struct {
	mu       sync.Mutex
	window   *application.WebviewWindow
	creating bool
}

// Toggle 热键触发：开着→关闭，没开→打开
func (t *WindowToggle) Toggle(create func() *application.WebviewWindow) {
	t.mu.Lock()
	w := t.window
	t.mu.Unlock()
	if w != nil {
		w.Close() // 关闭时 WindowClosing 钩子清引用
		return
	}
	t.Open(create)
}

// Open 点击触发：开着→聚焦，没开→打开
func (t *WindowToggle) Open(create func() *application.WebviewWindow) {
	t.mu.Lock()
	if t.window == nil {
		if t.creating {
			t.mu.Unlock() // 正在创建中，忽略本次触发
			return
		}
		t.creating = true
		t.mu.Unlock()

		w := create()
		t.mu.Lock()
		t.window = w
		t.creating = false
		t.mu.Unlock()

		if w != nil {
			// 用户点 X 关闭后清掉引用（下次触发重建窗口）
			w.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
				t.mu.Lock()
				if t.window == w {
					t.window = nil
				}
				t.mu.Unlock()
			})
			w.Show()
		}
		return
	}
	w := t.window
	t.mu.Unlock()
	w.Show() // 已开则聚焦
}
