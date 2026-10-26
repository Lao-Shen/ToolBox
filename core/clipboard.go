package core

import (
	"fmt"
	"image"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
)

// winErr 把 Win32 错误码转为 error
func winErr() error {
	return fmt.Errorf("win32 error: %s", syscall.Errno(win.GetLastError()))
}

// 剪贴板

// WriteImageToClipboard 将图像以 CF_DIB 格式写入剪贴板
func WriteImageToClipboard(img *image.RGBA) error {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	// BITMAPINFOHEADER（40 字节）+ 像素数据（BGRA，bottom-up）
	headerSize := int(unsafe.Sizeof(win.BITMAPINFOHEADER{}))
	total := headerSize + 4*w*h

	mem := win.GlobalAlloc(win.GMEM_MOVEABLE, uintptr(total))
	if mem == 0 {
		return winErr()
	}
	// SetClipboardData 成功后内存所有权移交系统，不能再 GlobalFree；仅在失败路径释放
	committed := false
	defer func() {
		if !committed {
			win.GlobalFree(mem)
		}
	}()

	p := win.GlobalLock(mem)
	if p == nil {
		return winErr()
	}

	data := unsafe.Slice((*byte)(p), total)

	// BITMAPINFOHEADER：BiHeight 正值 = bottom-up
	bmi := win.BITMAPINFOHEADER{
		BiSize:        uint32(headerSize),
		BiWidth:       int32(w),
		BiHeight:      int32(h),
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: win.BI_RGB,
	}
	header := unsafe.Slice((*byte)(unsafe.Pointer(&bmi)), headerSize)
	copy(data, header)

	// BGRA bottom-up：逐行翻转 + 颜色通道调换。
	// 注意：img 可能是 SubImage，img.Pix 是父图缓冲，
	// 必须用 img.PixOffset(b.Min.X, srcY) 按绝对坐标取行，不能用 (0, y)。
	for y := 0; y < h; y++ {
		srcY := b.Max.Y - 1 - y
		srcRow := img.Pix[img.PixOffset(b.Min.X, srcY) : img.PixOffset(b.Min.X, srcY)+4*w]
		dst := data[headerSize+4*w*y:]
		for i := 0; i < w; i++ {
			dst[4*i] = srcRow[4*i+2]   // B
			dst[4*i+1] = srcRow[4*i+1] // G
			dst[4*i+2] = srcRow[4*i]   // R
			dst[4*i+3] = 0
		}
	}
	win.GlobalUnlock(mem)

	if !win.OpenClipboard(0) {
		return winErr()
	}
	defer win.CloseClipboard()
	if !win.EmptyClipboard() {
		return winErr()
	}
	win.SetClipboardData(win.CF_DIB, win.HANDLE(mem))
	committed = true
	return nil
}
