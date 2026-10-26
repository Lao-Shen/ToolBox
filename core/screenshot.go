package core

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/kbinani/screenshot"
)

// 截屏底层功能（抓屏/裁切/保存），选区交互见 region_select.go

// CaptureVirtualScreen 抓取整个虚拟屏幕（多显示器拼接），返回物理像素图像
func CaptureVirtualScreen() (*image.RGBA, error) {
	n := screenshot.NumActiveDisplays()
	if n <= 0 {
		return nil, fmt.Errorf("no active display")
	}

	// 计算所有显示器 bounds 的并集作为画布
	bounds := screenshot.GetDisplayBounds(0)
	for i := 1; i < n; i++ {
		bounds = bounds.Union(screenshot.GetDisplayBounds(i))
	}
	canvas := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))

	for i := 0; i < n; i++ {
		img, err := screenshot.CaptureDisplay(i)
		if err != nil {
			return nil, err
		}
		b := screenshot.GetDisplayBounds(i)
		// 各屏图像贴到虚拟屏幕坐标对应位置
		dst := image.Rect(b.Min.X-bounds.Min.X, b.Min.Y-bounds.Min.Y,
			b.Min.X-bounds.Min.X+b.Dx(), b.Min.Y-bounds.Min.Y+b.Dy())
		drawImage(canvas, img, dst)
	}
	return canvas, nil
}

// drawImage 将 src 原样画到 dst 区域（要求尺寸一致，直接复制像素）
func drawImage(dst, src *image.RGBA, rect image.Rectangle) {
	for y := 0; y < rect.Dy(); y++ {
		copy(dst.Pix[dst.PixOffset(rect.Min.X, rect.Min.Y+y):], src.Pix[src.PixOffset(0, y):src.PixOffset(rect.Dx(), y)])
	}
}

// Crop 按矩形裁切图像
func Crop(img *image.RGBA, rect image.Rectangle) *image.RGBA {
	return img.SubImage(rect).(*image.RGBA)
}

// SavePNG 保存图像到指定目录（dir 为空时退回 Pictures\ToolBox），返回文件路径
func SavePNG(img *image.RGBA, dir string) (string, error) {
	if dir == "" {
		pictures, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(pictures, "Pictures", "ToolBox")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "screenshot_"+time.Now().Format("20060102_150405")+".png")

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", err
	}
	return path, nil
}
