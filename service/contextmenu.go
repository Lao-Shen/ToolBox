package service

import (
	"errors"
	"fmt"
	"sort"

	"changeme/core"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 鼠标右键菜单管理

// ContextMenuEntry 右键菜单项视图
type ContextMenuEntry struct {
	Key      string `json:"key"`      // 注册表键名
	Name     string `json:"name"`     // 显示名称
	Command  string `json:"command"`  // 启动命令
	Extended bool   `json:"extended"` // Shift+右键 才显示
	Enabled  bool   `json:"enabled"`  // 是否启用
	System   bool   `json:"system"`   // 所有用户（HKLM）项：只能启停，删除/编辑需管理员
}

// ContextMenuService 右键菜单管理服务。
// 读用 HKCR 合并视图（HKCU 覆盖 + HKLM），写统一走 HKCU\Software\Classes 覆盖键免管理员：
// 禁用 = 写 LegacyDisable 到覆盖键（系统项同样生效），删除/编辑仅限当前用户项。
type ContextMenuService struct {
	win WindowToggle // 管理窗口单实例开关
}

// List 指定分区的右键菜单项：file 文件 / folder 文件夹 / desktop 桌面空白
func (s *ContextMenuService) List(section string) ([]ContextMenuEntry, error) {
	var menus []core.RightClickMenu
	var err error
	switch section {
	case "file":
		menus, err = core.GetAnyFileMenu()
	case "folder":
		menus, err = core.GetFolderMenu()
	case "desktop":
		menus, err = core.GetDesktopBlockMenu()
	default:
		return nil, fmt.Errorf("未知分区 %s", section)
	}
	if err != nil {
		return nil, err
	}

	entries := make([]ContextMenuEntry, 0, len(menus))
	for _, m := range menus {
		name := m.MUIVerb
		if name == "" {
			name = m.DisplayName
		}
		if name == "" {
			name = m.Key
		}
		entries = append(entries, ContextMenuEntry{
			Key:      m.Key,
			Name:     name,
			Command:  m.Command,
			Extended: m.Extended,
			Enabled:  m.Enabled,
			System:   m.Source == "HKLM",
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// SetEnabled 启用/禁用菜单项
func (s *ContextMenuService) SetEnabled(section, key string, enabled bool) error {
	return core.SetRightClickMenuEnabled(section, key, enabled)
}

// Delete 删除菜单项（仅当前用户项，系统项返回错误）
func (s *ContextMenuService) Delete(section, key string) error {
	return core.DeleteRightClickMenu(section, key)
}

// Save 新增（key 为空，键名自动生成）或修改（key 非空）菜单项
func (s *ContextMenuService) Save(section, key, name, command string) error {
	if name == "" {
		return errors.New("名称不能为空")
	}
	if command == "" {
		return errors.New("命令不能为空")
	}
	return core.SaveRightClickMenu(section, key, name, command)
}

// OpenManager 打开右键菜单管理窗口（已开则聚焦，不重复创建）
func (s *ContextMenuService) OpenManager() {
	s.win.Open(s.newManagerWindow)
}

// ToggleManager 热键触发：管理窗口开/关切换
func (s *ContextMenuService) ToggleManager() {
	s.win.Toggle(s.newManagerWindow)
}

// newManagerWindow 创建右键菜单管理窗口（未显示）
func (s *ContextMenuService) newManagerWindow() *application.WebviewWindow {
	return application.Get().Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "右键菜单管理",
		Width:            720,
		Height:           560,
		URL:              "/?page=contextmenu",
		BackgroundColour: application.NewRGB(6, 7, 15),
	})
}
