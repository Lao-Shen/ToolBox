package service

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"earwind.top/forge/peicon"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 图标提取工具（从 exe/dll 提取图标，纯 Go PE 资源解析，复用 Forge 的 peicon）

// IconExtractService 图标提取服务
type IconExtractService struct {
	win WindowToggle // 管理窗口单实例开关
}

// PickExe 选择程序文件（exe/dll/ico）；用户取消返回空串
func (s *IconExtractService) PickExe() (string, error) {
	dlg := application.Get().Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:          "选择程序",
		CanChooseFiles: true,
		Filters: []application.FileFilter{
			{DisplayName: "程序文件 (*.exe;*.dll;*.ico)", Pattern: "*.exe;*.dll;*.ico"},
		},
	})
	return dlg.PromptForSingleSelection()
}

// Preview 提取主图标（256×256）→ PNG data URL 供 <img> 预览；exePath 为空返回空串
func (s *IconExtractService) Preview(exePath string) (string, error) {
	if exePath == "" {
		return "", nil
	}
	tmp, err := os.CreateTemp("", "toolbox-icon-*.png")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := peicon.ExtractSize(exePath, 256, 256, tmpPath); err != nil {
		return "", err
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// Export 导出主图标 PNG（256×256）到指定目录，文件名 = 程序名 + "-icon.png"；返回保存路径
func (s *IconExtractService) Export(exePath, dir string) (string, error) {
	if exePath == "" {
		return "", fmt.Errorf("未选择程序")
	}
	if dir == "" {
		return "", fmt.Errorf("未选择导出目录")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))
	dst := filepath.Join(dir, base+"-icon.png")
	if err := peicon.ExtractSize(exePath, 256, 256, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// OpenManager 打开图标提取窗口（已开则聚焦，不重复创建）
func (s *IconExtractService) OpenManager() {
	s.win.Open(s.newManagerWindow)
}

// ToggleManager 热键触发：窗口开/关切换
func (s *IconExtractService) ToggleManager() {
	s.win.Toggle(s.newManagerWindow)
}

// newManagerWindow 创建图标提取窗口（未显示）
func (s *IconExtractService) newManagerWindow() *application.WebviewWindow {
	return application.Get().Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "图标提取",
		Width:            480,
		Height:            400,
		URL:              "/?page=iconextract",
		BackgroundColour: application.NewRGB(6, 7, 15),
	})
}
