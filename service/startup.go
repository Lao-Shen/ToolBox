package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"changeme/core"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 开机自启业务

// StartupEntry 自启动项视图（含开关状态）
type StartupEntry struct {
	Source  string `json:"source"`  // HKCU（当前用户）/ HKLM（所有用户）
	Name    string `json:"name"`    // 注册表值名
	Command string `json:"command"` // 程序路径
	Enabled bool   `json:"enabled"` // 是否开启
}

// StartupService 开机自启管理服务。
// 关闭的项会从注册表删除，同时记录到本地文件（%APPDATA%\ToolBox\startup_items.json），
// 列表里仍展示为"关"，可随时重新开启。
type StartupService struct {
	mu  sync.Mutex
	win WindowToggle // 管理窗口单实例开关
}

// Enable 开启本应用开机自启
func (s *StartupService) Enable() error {
	return core.SetAutoStart(true)
}

// Disable 关闭本应用开机自启
func (s *StartupService) Disable() error {
	return core.SetAutoStart(false)
}

// IsEnabled 查询本应用开机自启状态
func (s *StartupService) IsEnabled() (bool, error) {
	return core.AutoStartEnabled()
}

// RefreshAutoStartEntry 已开启自启时重写注册表命令，补上 --autostart 参数
// （兼容旧版本写入的无参数项）。仅当自启项指向当前程序时才改；指向别的副本
// （开发版/另一份安装）时不动，避免把用户的自启项劫持到错误的 exe。
func (s *StartupService) RefreshAutoStartEntry() error {
	enabled, err := core.AutoStartEnabled()
	if err != nil || !enabled {
		return err
	}
	cmd, err := core.AutoStartCommand()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !sameWindowsPath(autoStartExe(cmd), exe) {
		return nil
	}
	return core.SetAutoStart(true)
}

// autoStartExe 从注册表命令中取出可执行文件路径（支持带引号与附加参数）
func autoStartExe(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if i := strings.Index(cmd[1:], `"`); i >= 0 {
			return cmd[1 : i+1]
		}
	}
	if i := strings.Index(cmd, " --"); i >= 0 {
		return strings.TrimSpace(cmd[:i])
	}
	return cmd
}

// sameWindowsPath Windows 路径比较（忽略大小写与分隔符差异）
func sameWindowsPath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// List 开机自启动程序列表（注册表中的开启项 + 本地记忆的关闭项）
func (s *StartupService) List() ([]StartupEntry, error) {
	reg, err := core.GetStartupItems()
	if err != nil {
		return nil, err
	}
	entries := make([]StartupEntry, 0, len(reg)+4)
	for _, it := range reg {
		entries = append(entries, StartupEntry{Source: it.Source, Name: it.Name, Command: it.Command, Enabled: true})
	}

	// 追加已关闭的项（注册表中已不存在、且本地有记忆的）
	disabled, err := s.loadDisabled()
	if err != nil {
		return nil, err
	}
	for _, d := range disabled {
		if !startupInList(reg, d.Source, d.Name) {
			entries = append(entries, StartupEntry{Source: d.Source, Name: d.Name, Command: d.Command, Enabled: false})
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// SetEnabled 开启/关闭某个自启动项
func (s *StartupService) SetEnabled(source, name, command string, enabled bool) error {
	if err := core.SetStartupItemEnabled(source, name, command, enabled); err != nil {
		return err
	}
	if enabled {
		return s.removeDisabled(source, name)
	}
	return s.addDisabled(source, name, command)
}

// OpenManager 打开开机自启管理窗口（已开则聚焦，不重复创建）
func (s *StartupService) OpenManager() {
	s.win.Open(s.newManagerWindow)
}

// ToggleManager 热键触发：管理窗口开/关切换
func (s *StartupService) ToggleManager() {
	s.win.Toggle(s.newManagerWindow)
}

// newManagerWindow 创建开机自启管理窗口（未显示，由调用方决定何时显示）
func (s *StartupService) newManagerWindow() *application.WebviewWindow {
	return application.Get().Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "开机自启管理",
		Width:            680,
		Height:           520,
		URL:              "/?page=startup",
		BackgroundColour: application.NewRGB(6, 7, 15),
	})
}

func startupInList(items []core.StartupItem, source, name string) bool {
	for _, it := range items {
		if it.Source == source && it.Name == name {
			return true
		}
	}
	return false
}

// ---- 已关闭项本地记忆 ----

func startupItemsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ToolBox", "startup_items.json"), nil
}

func (s *StartupService) loadDisabled() ([]StartupEntry, error) {
	path, err := startupItemsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var items []StartupEntry
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *StartupService) saveDisabled(items []StartupEntry) error {
	path, err := startupItemsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *StartupService) addDisabled(source, name, command string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadDisabled()
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.Source == source && it.Name == name {
			it.Command = command
			return s.saveDisabled(items)
		}
	}
	items = append(items, StartupEntry{Source: source, Name: name, Command: command, Enabled: false})
	return s.saveDisabled(items)
}

func (s *StartupService) removeDisabled(source, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadDisabled()
	if err != nil {
		return err
	}
	out := items[:0]
	for _, it := range items {
		if it.Source == source && it.Name == name {
			continue
		}
		out = append(out, it)
	}
	return s.saveDisabled(out)
}
