package service

import (
	"sync"
	"time"

	"changeme/core"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 截屏业务

// ScreenshotResult 截图结果
type ScreenshotResult struct {
	Path   string `json:"path"`   // 保存的文件路径（用户取消时为空）
	Width  int    `json:"width"`  // 图像宽（像素）
	Height int    `json:"height"` // 图像高（像素）
}

// ScreenshotService 截屏服务：隐藏主窗口 → 抓屏 → 框选 → 保存+剪贴板 → 恢复窗口
type ScreenshotService struct {
	mu        sync.Mutex
	window    *application.WebviewWindow
	capturing bool // 防重入：蒙层使用包级全局状态，同一时刻只允许一个截图流程
}

// AttachWindow 注入主窗口（main.go 在窗口创建后调用）
func (s *ScreenshotService) AttachWindow(w *application.WebviewWindow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.window = w
}

// CaptureRegion 区域截图全流程；workdir 为保存目录（即工具卡片的工作路径，空则退回默认目录）；用户取消时返回空结果。
// 已在截图流程中再触发（快捷键再按/卡片重复点击）= 取消当前截图，与开/关切换语义一致。
func (s *ScreenshotService) CaptureRegion(workdir string) (ScreenshotResult, error) {
	s.mu.Lock()
	if s.capturing {
		s.mu.Unlock()
		core.CancelOverlay() // 截图进行中：取消当前蒙层
		return ScreenshotResult{}, nil
	}
	s.capturing = true
	w := s.window
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.capturing = false
		s.mu.Unlock()
	}()

	// 隐藏主窗口并等待合成器刷新，避免窗口残影出现在截图里
	if w != nil {
		w.Hide()
		defer w.Show()
	}
	time.Sleep(300 * time.Millisecond)

	background, err := core.CaptureVirtualScreen()
	if err != nil {
		return ScreenshotResult{}, err
	}

	rect, strokes, rects, ok := core.SelectRegion(background)
	if !ok {
		return ScreenshotResult{}, nil // 用户取消
	}

	// 画笔笔画与方框标注合成到原图后再裁切（文字已在蒙层内栅格化）
	core.DrawStrokes(background, strokes)
	core.DrawRects(background, rects)
	img := core.Crop(background, rect)
	path, err := core.SavePNG(img, workdir)
	if err != nil {
		return ScreenshotResult{}, err
	}
	if err := core.WriteImageToClipboard(img); err != nil {
		return ScreenshotResult{}, err
	}

	return ScreenshotResult{Path: path, Width: img.Bounds().Dx(), Height: img.Bounds().Dy()}, nil
}
