package service

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 工具通用业务

// ToolService 工具通用服务（图标选择等）
type ToolService struct{}

// IconsDir 图标数据目录：%APPDATA%\ToolBox\icons
func IconsDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ToolBox", "icons"), nil
}

// IconAssetsMiddleware 资产服务中间件：/icons/ 请求映射到图标数据目录，其余走默认资产服务。
// 图标复制到软件数据目录后，即使源文件被删除图标依然可用。
func IconAssetsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/icons/") {
			dir, err := IconsDir()
			if err != nil {
				http.Error(w, "icons dir unavailable", http.StatusInternalServerError)
				return
			}
			// filepath.Base 防目录穿越
			name := strings.TrimPrefix(r.URL.Path, "/icons/")
			http.ServeFile(w, r, filepath.Join(dir, filepath.Base(name)))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PickIcon 打开文件选择器选图标，复制到软件数据目录（icons），
// 返回可直接用于 <img> 的服务路径（如 /icons/icon_xxx.png）；用户取消返回空串
func (t *ToolService) PickIcon() (string, error) {
	dlg := application.Get().Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:          "选择图标",
		CanChooseFiles: true,
		Filters: []application.FileFilter{
			{DisplayName: "图片文件 (*.png;*.jpg;*.jpeg;*.ico;*.gif;*.bmp;*.svg)", Pattern: "*.png;*.jpg;*.jpeg;*.ico;*.gif;*.bmp;*.svg"},
		},
	})
	path, err := dlg.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // 用户取消
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > 2*1024*1024 {
		return "", fmt.Errorf("图标文件不能超过 2MB")
	}

	dir, err := IconsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	// 保留原扩展名，时间戳命名保证唯一
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		ext = ".png"
	}
	dest := filepath.Join(dir, fmt.Sprintf("icon_%d%s", time.Now().UnixNano(), ext))
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", err
	}
	return "/icons/" + filepath.Base(dest), nil
}

// PickDirectory 打开目录选择器（工具的工作目录等）；用户取消返回空串
func (t *ToolService) PickDirectory() (string, error) {
	dlg := application.Get().Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:                "选择目录",
		CanChooseDirectories: true,
		CanChooseFiles:       false,
	})
	return dlg.PromptForSingleSelection()
}
