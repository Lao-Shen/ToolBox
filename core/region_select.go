package core

import (
	"fmt"
	"image"
	"image/color"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// 窗口枚举/查询：lxn/win 未导出，走 x/sys/windows
var procGetDesktopWindow = windows.NewLazySystemDLL("user32.dll").NewProc("GetDesktopWindow")
var procPostThreadMessage = windows.NewLazySystemDLL("user32.dll").NewProc("PostThreadMessageW")

func getDesktopWindow() win.HWND {
	h, _, _ := procGetDesktopWindow.Call()
	return win.HWND(h)
}

// 区域选择蒙层：覆盖整个虚拟屏幕的无边框顶层窗口，
// 背景为截图（画面"冻结"），拖动左键框选；
// 松开后锁定选区并在选区下方显示工具栏（保存/取消），点击保存才确认，
// Esc/右键取消，Enter 确认。参考微信截图交互。

const overlayClassName = "ToolBoxScreenshotOverlay"

// 工具栏按钮索引
const (
	tbPen = iota // 画笔
	tbRect       // 方框
	tbText       // 文字
	tbSave       // 保存
	tbCancel     // 取消
)

// 外部取消（快捷键切换语义：截图过程中再按一次快捷键 = 取消）
const wmOverlayCancel = 0x8002 // WM_APP+2：请求取消蒙层（线程消息，投递到蒙层线程队列）

var (
	overlayActive   atomic.Bool   // 蒙层消息循环运行中
	overlayCancel   atomic.Bool   // 已请求取消（蒙层未启动时置位，启动时立即生效）
	overlayThreadID atomic.Uint32 // 蒙层线程 ID
)

// CancelOverlay 取消正在进行的选区蒙层；未在截图时仅置位，下次蒙层启动立即取消
func CancelOverlay() {
	overlayCancel.Store(true)
	if overlayActive.Load() {
		if tid := overlayThreadID.Load(); tid != 0 {
			procPostThreadMessage.Call(uintptr(tid), wmOverlayCancel, 0, 0)
		}
	}
}

// 画笔调色板（默认红）
var penPalette = []color.RGBA{
	{255, 0, 0, 255},     // 红（默认）
	{255, 200, 0, 255},   // 黄
	{0, 200, 80, 255},    // 绿
	{0, 120, 255, 255},   // 蓝
	{255, 255, 255, 255}, // 白
	{0, 0, 0, 255},       // 黑
}

// 画笔粗细档位（默认 3）
var penWidths = []int{1, 3, 5, 7}

// 文字字号档位（小/中/大，默认中）
var textSizes = []int{16, 24, 36}

// 输入光标闪烁定时器 ID
const overlayTimerID = 1

// TextItem 文字标注
type TextItem struct {
	Pos   image.Point
	Text  string // 多行文本，\n 换行
	Color color.RGBA
	Size  int // 字号
}

// overlayState 选区交互状态（同一时刻只允许一个选区窗口）
type overlayState struct {
	img      *image.RGBA // 背景截图
	w, h     int         // 窗口尺寸
	screenX  int         // 窗口所在虚拟屏幕原点（窗口坐标 → 屏幕坐标偏移）
	screenY  int
	hwnd     win.HWND     // 蒙层自身句柄（窗口枚举时排除）
	start    win.POINT    // 左键按下点
	current  win.POINT    // 当前拖动点
	dragging bool         // 正在拖选
	selected bool         // 已锁定选区，等待保存/取消
	hover      int             // 工具栏悬停按钮（-1 无）
	hoverWin   image.Rectangle // 鼠标悬停命中的窗口（客户区坐标，空=无）
	downInWin  bool            // 左键按在窗口高亮区内（抬起无位移则选中该窗口）
	penMode     bool       // 画笔模式（工具栏画笔按钮激活）
	rectMode    bool       // 方框模式
	textMode    bool       // 文字模式
	penColor    color.RGBA // 当前标注颜色
	penWidth    int        // 当前线宽（画笔/方框共用）
	textSize    int        // 当前字号
	penDrawing  bool       // 正在画一笔
	rectDrawing bool       // 正在拖方框
	rectStart   image.Point
	rectCurrent image.Point
	strokes     []Stroke    // 已绘制的笔画（背景图坐标）
	rects       []RectShape // 已绘制的方框
	textItems   []TextItem  // 文字标注
	inputActive bool        // 正在输入文字（最后一项为当前编辑项）
	caretOn     bool        // 输入光标可见（定时器闪烁）
	result      image.Rectangle
	font        win.HFONT // 图标字形字体（Segoe MDL2 Assets，矢量 ClearType 抗锯齿）
	swatchFont  win.HFONT // 色点字形字体（Segoe UI Symbol，●/◯ 抗锯齿圆）
	textFonts   [3]win.HFONT // 文字字号字体（小/中/大，Segoe UI）
	// 三缓冲：memDC1 原始背景、memDC2 背景+整体变暗、memDC3 帧缓冲（组合后原子贴图，防闪烁）
	memDC1 win.HDC
	memDC2 win.HDC
	memDC3 win.HDC
}

var state *overlayState

// 窗口类进程内只注册一次：二次 RegisterClassEx 会因
// ERROR_CLASS_ALREADY_EXISTS 失败，导致第二次截图直接取消
const errClassAlreadyExists = 1410 // ERROR_CLASS_ALREADY_EXISTS

var (
	classRegisterOnce sync.Once
	classRegistered   bool
)

// registerOverlayClass 注册蒙层窗口类（幂等）
func registerOverlayClass(instance win.HINSTANCE) bool {
	classRegisterOnce.Do(func() {
		var wc win.WNDCLASSEX
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		wc.LpfnWndProc = syscall.NewCallback(overlayWndProc)
		wc.HInstance = instance
		wc.HCursor = win.HCURSOR(win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_CROSS)))
		wc.HbrBackground = win.HBRUSH(win.GetStockObject(win.BLACK_BRUSH))
		wc.LpszClassName = syscall.StringToUTF16Ptr(overlayClassName)
		classRegistered = win.RegisterClassEx(&wc) != 0 ||
			win.GetLastError() == errClassAlreadyExists
	})
	return classRegistered
}

// SelectRegion 打开全屏选区蒙层并等待用户框选。
// 返回选区、画笔笔画与方框标注（背景图坐标）；文字标注已在蒙层内
// 用 GDI 栅格化进背景图。用户取消时 ok=false。
//
// 蒙层必须在独立 OS 线程运行消息循环：wails 的服务方法在主线程执行，
// 若在主线程嵌套 GetMessage 循环，wails 的定时事件（invoke/execJS）会被
// 嵌套循环派发，WebView2 Eval 在模态循环中阻塞，拖拽时表现为卡死。
func SelectRegion(background *image.RGBA) (image.Rectangle, []Stroke, []RectShape, bool) {
	type overlayResult struct {
		rect    image.Rectangle
		strokes []Stroke
		rects   []RectShape
		ok      bool
	}
	done := make(chan overlayResult, 1)

	go func() {
		defer func() {
			// 回调内 panic 会让整个进程崩溃，兜底为"取消"
			if r := recover(); r != nil {
				done <- overlayResult{image.Rectangle{}, nil, nil, false}
			}
		}()
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		rect, strokes, rects, ok := runOverlay(background)
		done <- overlayResult{rect, strokes, rects, ok}
	}()

	res := <-done
	return res.rect, res.strokes, res.rects, res.ok
}

// runOverlay 在当前 OS 线程上创建蒙层窗口并运行消息循环
func runOverlay(background *image.RGBA) (image.Rectangle, []Stroke, []RectShape, bool) {
	state = &overlayState{img: background, hover: -1, penColor: penPalette[0], penWidth: 3, textSize: textSizes[1]}

	overlayThreadID.Store(win.GetCurrentThreadId())
	overlayActive.Store(true)
	defer func() {
		overlayActive.Store(false)
		overlayThreadID.Store(0)
	}()

	// 蒙层出现前已收到取消请求（截图过程中再按快捷键）→ 直接取消
	if overlayCancel.Swap(false) {
		return image.Rectangle{}, nil, nil, false
	}

	instance := win.GetModuleHandle(nil)

	if !registerOverlayClass(instance) {
		return image.Rectangle{}, nil, nil, false
	}

	// 覆盖整个虚拟屏幕（多显示器）
	x := win.GetSystemMetrics(win.SM_XVIRTUALSCREEN)
	y := win.GetSystemMetrics(win.SM_YVIRTUALSCREEN)
	w := int(win.GetSystemMetrics(win.SM_CXVIRTUALSCREEN))
	h := int(win.GetSystemMetrics(win.SM_CYVIRTUALSCREEN))
	state.screenX, state.screenY = int(x), int(y)

	hwnd := win.CreateWindowEx(
		win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW,
		syscall.StringToUTF16Ptr(overlayClassName),
		nil,
		win.WS_POPUP,
		x, y, int32(w), int32(h),
		0, 0, instance, nil)
	if hwnd == 0 {
		return image.Rectangle{}, nil, nil, false
	}
	state.hwnd = hwnd

	if !state.prepareBuffers(w, h) {
		win.DestroyWindow(hwnd)
		return image.Rectangle{}, nil, nil, false
	}

	win.ShowWindow(hwnd, win.SW_SHOW)
	win.SetForegroundWindow(hwnd)
	win.SetFocus(hwnd)

	// 消息循环（本线程的窗口消息只在这里派发）
	var msg win.MSG
	for win.GetMessage(&msg, 0, 0, 0) > 0 {
		if msg.Message == wmOverlayCancel {
			overlayCancel.Store(false)
			finishSelect(hwnd, image.Rectangle{}) // 与取消按钮/Esc 等价
			continue
		}
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}

	win.DestroyWindow(hwnd)
	// 文字标注用 GDI 合成到背景图（多行、按字号字体，中文走系统字体回退）
	if !state.result.Empty() && len(state.textItems) > 0 {
		rasterizeText(background, state.textItems)
	}
	state.releaseBuffers()
	r := state.result
	strokes := state.strokes
	rects := state.rects
	state = nil

	if r.Empty() {
		return image.Rectangle{}, nil, nil, false
	}
	return r, strokes, rects, true
}

// rasterizeText 用 GDI 把文字标注画到背景图（渲染在 DIB 内存上再拷回）
func rasterizeText(img *image.RGBA, items []TextItem) {
	if len(items) == 0 || state == nil {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	windowDC := win.GetDC(0)
	defer win.ReleaseDC(0, windowDC)

	memDC := win.CreateCompatibleDC(windowDC)
	if memDC == 0 {
		return
	}
	defer win.DeleteDC(memDC)

	bmi := win.BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})),
		BiWidth:       int32(w),
		BiHeight:      -int32(h), // top-down
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: win.BI_RGB,
	}
	var pvBits unsafe.Pointer
	dib := win.CreateDIBSection(memDC, &bmi, win.DIB_RGB_COLORS, &pvBits, 0, 0)
	if dib == 0 {
		return
	}
	defer win.DeleteObject(win.HGDIOBJ(dib))
	win.SelectObject(memDC, win.HGDIOBJ(dib))

	// RGBA → DIB（BGRA）
	dibPix := unsafe.Slice((*byte)(pvBits), 4*w*h)
	for i, j := 0, 0; i < len(img.Pix); i, j = i+4, j+4 {
		dibPix[j], dibPix[j+1], dibPix[j+2] = img.Pix[i+2], img.Pix[i+1], img.Pix[i]
	}

	for _, it := range items {
		drawTextLines(memDC, it)
	}

	// DIB → RGBA
	for i, j := 0, 0; i < len(img.Pix); i, j = i+4, j+4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = dibPix[j+2], dibPix[j+1], dibPix[j], 255
	}
}

// prepareBuffers 建立三缓冲
func (s *overlayState) prepareBuffers(w, h int) bool {
	s.w, s.h = w, h
	windowDC := win.GetDC(0)
	defer win.ReleaseDC(0, windowDC)

	s.memDC1 = win.CreateCompatibleDC(windowDC)
	s.memDC2 = win.CreateCompatibleDC(windowDC)
	s.memDC3 = win.CreateCompatibleDC(windowDC)
	if s.memDC1 == 0 || s.memDC2 == 0 || s.memDC3 == 0 {
		return false
	}

	bmp1 := win.CreateCompatibleBitmap(windowDC, int32(w), int32(h))
	bmp2 := win.CreateCompatibleBitmap(windowDC, int32(w), int32(h))
	bmp3 := win.CreateCompatibleBitmap(windowDC, int32(w), int32(h))
	if bmp1 == 0 || bmp2 == 0 || bmp3 == 0 {
		return false
	}
	win.SelectObject(s.memDC1, win.HGDIOBJ(bmp1))
	win.SelectObject(s.memDC2, win.HGDIOBJ(bmp2))
	win.SelectObject(s.memDC3, win.HGDIOBJ(bmp3))

	// 背景图写入 memDC1（top-down BGRA）
	if !drawRGBA(s.memDC1, s.img, 0, 0) {
		return false
	}
	// memDC2 = 背景 + 55% 暗化（1x1 黑位图 AlphaBlend 拉伸全屏）
	win.BitBlt(s.memDC2, 0, 0, int32(w), int32(h), s.memDC1, 0, 0, win.SRCCOPY)
	blackDC := win.CreateCompatibleDC(windowDC)
	black := win.CreateCompatibleBitmap(windowDC, 1, 1)
	win.SelectObject(blackDC, win.HGDIOBJ(black))
	win.BitBlt(blackDC, 0, 0, 1, 1, windowDC, 0, 0, win.BLACKNESS)
	blend := win.BLENDFUNCTION{
		BlendOp:             0, // AC_SRC_OVER
		SourceConstantAlpha: 140,
	}
	win.AlphaBlend(s.memDC2, 0, 0, int32(w), int32(h),
		blackDC, 0, 0, 1, 1, blend)
	win.DeleteDC(blackDC)
	win.DeleteObject(win.HGDIOBJ(black))

	// 工具栏图标字体：Segoe MDL2 Assets（Win10/11 自带图标字体，ClearType 抗锯齿）
	lf := win.LOGFONT{LfHeight: -18, LfWeight: win.FW_NORMAL, LfCharSet: win.DEFAULT_CHARSET}
	copy(lf.LfFaceName[:], utf16.Encode([]rune("Segoe MDL2 Assets")))
	s.font = win.CreateFontIndirect(&lf)
	if s.font == 0 {
		return false
	}
	// 色点字形字体：Segoe UI Symbol（● 实心圆 / ◯ 空心环，抗锯齿）
	lfSw := win.LOGFONT{LfHeight: -24, LfWeight: win.FW_NORMAL, LfCharSet: win.DEFAULT_CHARSET}
	copy(lfSw.LfFaceName[:], utf16.Encode([]rune("Segoe UI Symbol")))
	s.swatchFont = win.CreateFontIndirect(&lfSw)
	if s.swatchFont == 0 {
		return false
	}
	// 文字字号字体：小/中/大（Segoe UI，系统字体回退支持中文）
	for i, size := range textSizes {
		lfT := win.LOGFONT{LfHeight: int32(-size), LfWeight: win.FW_NORMAL, LfCharSet: win.DEFAULT_CHARSET}
		copy(lfT.LfFaceName[:], utf16.Encode([]rune("Segoe UI")))
		s.textFonts[i] = win.CreateFontIndirect(&lfT)
		if s.textFonts[i] == 0 {
			return false
		}
	}
	return true
}

func (s *overlayState) releaseBuffers() {
	if s.font != 0 {
		win.DeleteObject(win.HGDIOBJ(s.font))
	}
	if s.swatchFont != 0 {
		win.DeleteObject(win.HGDIOBJ(s.swatchFont))
	}
	for i := range s.textFonts {
		if s.textFonts[i] != 0 {
			win.DeleteObject(win.HGDIOBJ(s.textFonts[i]))
		}
	}
	win.DeleteDC(s.memDC1)
	win.DeleteDC(s.memDC2)
	win.DeleteDC(s.memDC3)
}

// drawRGBA 将 RGBA 图像画到 DC 指定位置（DIB section 转 BGRA，top-down）
func drawRGBA(hdc win.HDC, img *image.RGBA, x, y int) bool {
	b := img.Bounds()
	bgra := make([]byte, 0, 4*b.Dx()*b.Dy())
	for i := 0; i < len(img.Pix); i += 4 {
		bgra = append(bgra, img.Pix[i+2], img.Pix[i+1], img.Pix[i], 0) // B,G,R,pad
	}

	bmi := win.BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})),
		BiWidth:       int32(b.Dx()),
		BiHeight:      -int32(b.Dy()), // 负值 = top-down
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: win.BI_RGB,
	}

	var pvBits unsafe.Pointer
	dib := win.CreateDIBSection(hdc, &bmi, win.DIB_RGB_COLORS, &pvBits, 0, 0)
	if dib == 0 {
		return false
	}
	defer win.DeleteObject(win.HGDIOBJ(dib))

	tmpDC := win.CreateCompatibleDC(hdc)
	if tmpDC == 0 {
		return false
	}
	defer win.DeleteDC(tmpDC)
	win.SelectObject(tmpDC, win.HGDIOBJ(dib))

	copy(unsafe.Slice((*byte)(pvBits), len(bgra)), bgra)
	return win.BitBlt(hdc, int32(x), int32(y), int32(b.Dx()), int32(b.Dy()), tmpDC, 0, 0, win.SRCCOPY)
}

// overlayWndProc 蒙层窗口消息处理。
// 内层 recover：任何 panic 只丢弃当前消息，蒙层保持可用，
// 避免外层把 panic 转成"取消"导致整个截图丢失。
func overlayWndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) (ret uintptr) {
	defer func() {
		if recover() != nil {
			ret = 0
		}
	}()
	s := state
	if s == nil {
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}

	switch msg {
	case win.WM_ERASEBKGND:
		return 1 // 阻止默认擦除，避免整屏黑闪

	case win.WM_PAINT:
		var ps win.PAINTSTRUCT
		hdc := win.BeginPaint(hwnd, &ps)
		overlayPaint(hwnd, hdc)
		win.EndPaint(hwnd, &ps)
		return 0

	case win.WM_LBUTTONDOWN:
		p := pointFromLParam(lParam)
		// 已锁定选区：优先命中工具栏按钮
		if s.selected {
			bar, buttons := toolbarRects(selectionRect(s), s.w, s.h)
			if ptInRect(buttons[tbPen], p) {
				s.penMode = !s.penMode // 切换画笔模式（互斥）
				if s.penMode {
					s.rectMode, s.textMode = false, false
				}
				win.InvalidateRect(hwnd, nil, false)
				return 0
			}
			if ptInRect(buttons[tbRect], p) {
				s.rectMode = !s.rectMode // 切换方框模式（互斥）
				if s.rectMode {
					s.penMode, s.textMode = false, false
				}
				win.InvalidateRect(hwnd, nil, false)
				return 0
			}
			if ptInRect(buttons[tbText], p) {
				s.textMode = !s.textMode // 切换文字模式（互斥）
				if s.textMode {
					s.penMode, s.rectMode = false, false
				}
				win.InvalidateRect(hwnd, nil, false)
				return 0
			}
			if ptInRect(buttons[tbSave], p) {
				finishSelect(hwnd, selectionRect(s))
				return 0
			}
			if ptInRect(buttons[tbCancel], p) {
				finishSelect(hwnd, image.Rectangle{})
				return 0
			}
			// 选项面板：色点换色 / 第二行换挡（画笔方框=粗细，文字=字号）
			if s.penMode || s.rectMode || s.textMode {
				second := ""
				if s.penMode || s.rectMode {
					second = "thickness"
				}
				if s.textMode {
					second = "size"
				}
				_, swatches, cells := optionsPanelRects(bar, s.h, second)
				for i, sw := range swatches {
					if ptInRect(sw, p) && i < len(penPalette) {
						s.penColor = penPalette[i]
						win.InvalidateRect(hwnd, nil, false)
						return 0
					}
				}
				for i, cell := range cells {
					if ptInRect(cell, p) {
						switch second {
						case "thickness":
							if i < len(penWidths) {
								s.penWidth = penWidths[i]
							}
						case "size":
							if i < len(textSizes) {
								s.textSize = textSizes[i]
							}
						}
						win.InvalidateRect(hwnd, nil, false)
						return 0
					}
				}
			}
			// 画笔模式：在选区内按下 → 开始画一笔
			if s.penMode && ptInRect(selectionRect(s), p) {
				s.penDrawing = true
				s.strokes = append(s.strokes, Stroke{Color: s.penColor, Width: s.penWidth})
				s.strokes[len(s.strokes)-1].Points = append(s.strokes[len(s.strokes)-1].Points, pointToImage(p))
				win.SetCapture(hwnd)
				return 0
			}
			// 方框模式：在选区内按下 → 开始拖方框
			if s.rectMode && ptInRect(selectionRect(s), p) {
				s.rectDrawing = true
				s.rectStart = pointToImage(p)
				s.rectCurrent = s.rectStart
				s.rects = append(s.rects, RectShape{Color: s.penColor, Width: s.penWidth})
				win.SetCapture(hwnd)
				return 0
			}
			// 文字模式：在选区内按下 → 开始输入文字（上一项为空则丢弃）
			if s.textMode && ptInRect(selectionRect(s), p) {
				if s.inputActive && len(s.textItems) > 0 && s.textItems[len(s.textItems)-1].Text == "" {
					s.textItems = s.textItems[:len(s.textItems)-1]
				}
				s.textItems = append(s.textItems, TextItem{Pos: pointToImage(p), Color: s.penColor, Size: s.textSize})
				s.inputActive = true
				s.caretOn = true
				win.SetTimer(hwnd, overlayTimerID, 500, 0) // 光标呼吸闪烁
				win.InvalidateRect(hwnd, nil, false)
				return 0
			}
			s.selected = false // 点其他区域：重新框选（清空标注）
			s.strokes = nil
			s.rects = nil
			s.textItems = nil
			s.inputActive = false
			s.penDrawing = false
			s.rectDrawing = false
			stopCaret(hwnd)
		}
		s.start = p
		s.current = p
		s.hover = -1
		win.SetCapture(hwnd)
		// 按在高亮窗口内：等待抬起判断（无位移=选中窗口，位移=自定义拖选）
		if !s.hoverWin.Empty() && ptInRect(s.hoverWin, p) {
			s.downInWin = true
		} else {
			s.dragging = true
		}
		win.InvalidateRect(hwnd, nil, false)
		return 0

	case win.WM_MOUSEMOVE:
		p := pointFromLParam(lParam)
		if s.dragging {
			s.current = p
			win.InvalidateRect(hwnd, nil, false)
			return 0
		}
		if s.selected {
			// 画笔模式：正在画 → 追加轨迹点（限选区内）
			if s.penDrawing {
				if ptInRect(selectionRect(s), p) && len(s.strokes) > 0 {
					last := &s.strokes[len(s.strokes)-1]
					last.Points = append(last.Points, pointToImage(p))
				}
				win.InvalidateRect(hwnd, nil, false)
				return 0
			}
			// 方框模式：正在拖 → 更新矩形
			if s.rectDrawing {
				s.rectCurrent = pointToImage(p)
				if len(s.rects) > 0 {
					last := &s.rects[len(s.rects)-1]
					last.Rect = normImageRect(s.rectStart, s.rectCurrent)
				}
				win.InvalidateRect(hwnd, nil, false)
				return 0
			}
			// 工具栏按钮悬停高亮
			_, buttons := toolbarRects(selectionRect(s), s.w, s.h)
			newHover := -1
			for i, b := range buttons {
				if ptInRect(b, p) {
					newHover = i
				}
			}
			if newHover != s.hover {
				s.hover = newHover
				win.InvalidateRect(hwnd, nil, false)
			}
			return 0
		}
		if s.downInWin {
			// 位移超过阈值 → 转为自定义拖选
			if abs32(p.X-s.start.X) > 3 || abs32(p.Y-s.start.Y) > 3 {
				s.downInWin = false
				s.dragging = true
				s.hoverWin = image.Rectangle{}
				s.current = p
			}
			win.InvalidateRect(hwnd, nil, false)
			return 0
		}
		// 悬停窗口高亮（优先选窗口）
		if newHover := windowAtPoint(s, p); newHover != s.hoverWin {
			s.hoverWin = newHover
			win.InvalidateRect(hwnd, nil, false)
		}
		return 0

	case win.WM_LBUTTONUP:
		if s.penDrawing {
			// 结束一笔（单点笔画补一个点，保证可见）
			s.penDrawing = false
			win.ReleaseCapture()
			if len(s.strokes) > 0 {
				last := &s.strokes[len(s.strokes)-1]
				if len(last.Points) == 1 {
					last.Points = append(last.Points, last.Points[0])
				}
			}
			win.InvalidateRect(hwnd, nil, false)
			return 0
		}
		if s.rectDrawing {
			// 结束方框（过小则丢弃）
			s.rectDrawing = false
			win.ReleaseCapture()
			if len(s.rects) > 0 {
				last := s.rects[len(s.rects)-1]
				if last.Rect.Dx() <= 3 || last.Rect.Dy() <= 3 {
					s.rects = s.rects[:len(s.rects)-1]
				}
			}
			win.InvalidateRect(hwnd, nil, false)
			return 0
		}
		if s.downInWin {
			// 单击未拖动：确定截图该窗口
			s.downInWin = false
			s.selected = true
			s.start = win.POINT{X: int32(s.hoverWin.Min.X), Y: int32(s.hoverWin.Min.Y)}
			s.current = win.POINT{X: int32(s.hoverWin.Max.X), Y: int32(s.hoverWin.Max.Y)}
			s.hoverWin = image.Rectangle{}
			s.hover = -1
			win.ReleaseCapture()
			win.InvalidateRect(hwnd, nil, false)
			return 0
		}
		if s.dragging {
			s.dragging = false
			win.ReleaseCapture()
			if r := selectionRect(s); r.Dx() > 3 && r.Dy() > 3 {
				s.selected = true // 锁定选区，等待保存/取消
				s.hover = -1
			}
			win.InvalidateRect(hwnd, nil, false)
		}
		return 0

	case win.WM_CHAR:
		// 文字输入态：处理字符（中文由 IME 提交后同样经 WM_CHAR 到达）
		if s.inputActive && len(s.textItems) > 0 {
			item := &s.textItems[len(s.textItems)-1]
			switch wParam {
			case '\b': // 退格
				if r := []rune(item.Text); len(r) > 0 {
					item.Text = string(r[:len(r)-1])
				}
			case '\r', '\n':
				// 回车换行统一在 WM_KEYDOWN 处理，这里忽略避免重复
			default:
				if wParam >= 0x20 && len([]rune(item.Text)) < 200 {
					item.Text += string(rune(wParam))
				}
			}
			win.InvalidateRect(hwnd, nil, false)
			return 0
		}
		return win.DefWindowProc(hwnd, msg, wParam, lParam)

	case win.WM_TIMER:
		if wParam == overlayTimerID && s.inputActive {
			s.caretOn = !s.caretOn // 光标呼吸闪烁
			win.InvalidateRect(hwnd, nil, false)
			return 0
		}

	case win.WM_KEYDOWN:
		if wParam == win.VK_ESCAPE {
			if s.inputActive && len(s.textItems) > 0 {
				// 仅取消当前文字输入
				s.textItems = s.textItems[:len(s.textItems)-1]
				s.inputActive = false
				stopCaret(hwnd)
				win.InvalidateRect(hwnd, nil, false)
				return 0
			}
			finishSelect(hwnd, image.Rectangle{}) // 取消
			return 0
		}
		if wParam == win.VK_RETURN {
			if s.inputActive {
				if ctrlDown() {
					// Ctrl+Enter：确认输入（空文本则丢弃）
					if len(s.textItems) > 0 && s.textItems[len(s.textItems)-1].Text == "" {
						s.textItems = s.textItems[:len(s.textItems)-1]
					}
					s.inputActive = false
					stopCaret(hwnd)
				} else if len(s.textItems) > 0 {
					// 回车换行
					s.textItems[len(s.textItems)-1].Text += "\n"
				}
				win.InvalidateRect(hwnd, nil, false)
				return 0
			}
			if r := selectionRect(s); r.Dx() > 3 && r.Dy() > 3 {
				finishSelect(hwnd, r) // 确认
			}
			return 0
		}

	case win.WM_RBUTTONDOWN:
		finishSelect(hwnd, image.Rectangle{}) // 右键取消
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

// ctrlDown 检测 Ctrl 是否按住（高位置位时 GetKeyState 返回负数）
func ctrlDown() bool {
	return win.GetKeyState(win.VK_CONTROL) < 0
}

// stopCaret 停止输入光标闪烁定时器
func stopCaret(hwnd win.HWND) {
	if state != nil {
		state.caretOn = false
	}
	win.KillTimer(hwnd, overlayTimerID)
}

// finishSelect 写入选区结果并结束消息循环
func finishSelect(hwnd win.HWND, r image.Rectangle) {
	stopCaret(hwnd)
	if state != nil {
		state.result = r
	}
	win.PostQuitMessage(0)
}

// pointToImage 客户区点 → 图像点
func pointToImage(p win.POINT) image.Point {
	return image.Point{X: int(p.X), Y: int(p.Y)}
}

// normImageRect 由两个点归一化出矩形
func normImageRect(a, b image.Point) image.Rectangle {
	x0, x1 := a.X, b.X
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	y0, y1 := a.Y, b.Y
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	return image.Rect(x0, y0, x1, y1)
}

// optionsPanelRects 选项面板布局：色点行 + 第二行（"thickness" 粗细 / "size" 字号 / "" 无），
// 位置在工具栏上方，空间不足则放下方
func optionsPanelRects(bar image.Rectangle, screenH int, secondRow string) (
	panel image.Rectangle, swatches []image.Rectangle, cells []image.Rectangle) {
	const (
		swCell   = 32 // 色点格子
		thCell   = 36 // 粗细格子
		sizeCell = 48 // 字号格子
		gap      = 8
		colorRow = 38
		secondH  = 40
		padding  = 12
	)
	colorsW := len(penPalette)*swCell + (len(penPalette)-1)*gap
	var secondW, secondCount int
	switch secondRow {
	case "thickness":
		secondW, secondCount = len(penWidths)*thCell+(len(penWidths)-1)*gap, len(penWidths)
	case "size":
		secondW, secondCount = len(textSizes)*sizeCell+(len(textSizes)-1)*gap, len(textSizes)
	}
	panelW := colorsW + 2*padding
	if secondW > colorsW {
		panelW = secondW + 2*padding
	}
	panelH := padding + colorRow + padding
	if secondRow != "" {
		panelH += gap + secondH
	}
	x := (bar.Min.X+bar.Max.X)/2 - panelW/2
	y := bar.Min.Y - 6 - panelH
	if y < 0 {
		y = bar.Max.Y + 6
	}
	panel = image.Rect(x, y, x+panelW, y+panelH)

	// 色点行（水平居中）
	cx0 := x + (panelW-colorsW)/2
	swatches = make([]image.Rectangle, len(penPalette))
	for i := range swatches {
		swatches[i] = image.Rect(cx0+i*(swCell+gap), y+padding, cx0+i*(swCell+gap)+swCell, y+padding+colorRow)
	}
	// 第二行（水平居中）
	if secondCount > 0 {
		cellSize := thCell
		if secondRow == "size" {
			cellSize = sizeCell
		}
		cx1 := x + (panelW-secondW)/2
		cy := y + padding + colorRow + gap
		cells = make([]image.Rectangle, secondCount)
		for i := range cells {
			cells[i] = image.Rect(cx1+i*(cellSize+gap), cy, cx1+i*(cellSize+gap)+cellSize, cy+secondH)
		}
	}
	return
}

// selectionRect 由起止点归一化出选区
func selectionRect(s *overlayState) image.Rectangle {
	x0, x1 := s.start.X, s.current.X
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	y0, y1 := s.start.Y, s.current.Y
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	return image.Rect(int(x0), int(y0), int(x1), int(y1))
}

// ptInRect 判断点是否在矩形内
func ptInRect(r image.Rectangle, p win.POINT) bool {
	return int(p.X) >= r.Min.X && int(p.X) < r.Max.X && int(p.Y) >= r.Min.Y && int(p.Y) < r.Max.Y
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// windowAtPoint 返回光标下最顶层的可见普通窗口矩形（客户区坐标，空=未命中）。
// EnumWindows 按 Z 序自上而下枚举，第一个包含该点的窗口即最顶层。
func windowAtPoint(s *overlayState, p win.POINT) image.Rectangle {
	screenPt := win.POINT{X: p.X + int32(s.screenX), Y: p.Y + int32(s.screenY)}
	desktop := getDesktopWindow()

	var found win.HWND
	cb := syscall.NewCallback(func(hwnd win.HWND, lparam uintptr) uintptr {
		if hwnd == s.hwnd || hwnd == desktop || !win.IsWindowVisible(hwnd) {
			return 1 // 继续枚举
		}
		if style := win.GetWindowLongPtr(hwnd, win.GWL_EXSTYLE); style&win.WS_EX_TOOLWINDOW != 0 {
			return 1
		}
		// 跳过桌面/任务栏等系统窗口
		var buf [64]uint16
		n, _ := windows.GetClassName(windows.HWND(hwnd), &buf[0], int32(len(buf)))
		switch syscall.UTF16ToString(buf[:n]) {
		case "Shell_TrayWnd", "Shell_SecondaryTrayWnd", "Progman", "WorkerW", "Windows.UI.Core.CoreWindow":
			return 1
		}
		var rc win.RECT
		if !win.GetWindowRect(hwnd, &rc) {
			return 1
		}
		if screenPt.X < rc.Left || screenPt.X >= rc.Right || screenPt.Y < rc.Top || screenPt.Y >= rc.Bottom {
			return 1
		}
		found = hwnd
		return 0 // 命中，停止枚举
	})
	_ = windows.EnumWindows(cb, nil)

	if found == 0 {
		return image.Rectangle{}
	}
	var rc win.RECT
	if !win.GetWindowRect(found, &rc) {
		return image.Rectangle{}
	}
	// 屏幕坐标 → 客户区坐标
	return image.Rect(
		int(rc.Left)-s.screenX, int(rc.Top)-s.screenY,
		int(rc.Right)-s.screenX, int(rc.Bottom)-s.screenY)
}

// toolbarRects 计算工具栏位置（默认在选区下方，超出屏幕则放上方/内部）及按钮矩形
func toolbarRects(sel image.Rectangle, screenW, screenH int) (image.Rectangle, [5]image.Rectangle) {
	const (
		barW = 260
		barH = 50
		pad  = 8
	)
	x := sel.Max.X - barW // 贴选区右下角
	y := sel.Max.Y + pad
	if y+barH > screenH {
		y = sel.Min.Y - pad - barH // 超出下边缘 → 放选区上方
	}
	if y < 0 {
		y = sel.Min.Y + pad // 选区太高 → 放选区内左上
	}
	if x < 0 {
		x = 0
	}
	bar := image.Rect(x, y, x+barW, y+barH)
	cell := barW / 5
	var buttons [5]image.Rectangle
	for i := range buttons {
		buttons[i] = image.Rect(x+i*cell, y, x+(i+1)*cell, y+barH)
	}
	return bar, buttons
}

// overlayPaint 组合整个画面到帧缓冲，再一次性贴到窗口（原子更新，防闪烁）
func overlayPaint(hwnd win.HWND, hdc win.HDC) {
	s := state
	if s == nil {
		return
	}
	w, h := s.w, s.h
	fb := s.memDC3

	// 1. 原始背景
	win.BitBlt(fb, 0, 0, int32(w), int32(h), s.memDC1, 0, 0, win.SRCCOPY)

	r := selectionRect(s)
	hasSel := r.Dx() > 3 && r.Dy() > 3 && (s.dragging || s.selected)
	// 悬停窗口预览：未拖选且鼠标停在窗口上时，高亮该窗口为默认选区
	hoverPreview := !s.dragging && !s.selected && !s.hoverWin.Empty()
	if hasSel || hoverPreview {
		hole := r
		if hoverPreview {
			hole = s.hoverWin
		}
		// 2. 暗化遮罩：挖洞（选区/悬停窗口保持明亮）
		drawMaskExcluding(fb, hole, w, h)

		// 3. 标注：笔画 + 方框 + 文字（画在边框之下、遮罩之上）
		if len(s.strokes) > 0 {
			drawStrokesOnDC(fb, s.strokes)
		}
		if len(s.rects) > 0 {
			drawRectsOnDC(fb, s.rects)
		}
		if len(s.textItems) > 0 {
			drawTextItems(fb, s)
		}

		// 4. 绿色边框
		drawBorder(fb, hole)

		// 5. 尺寸标签（拖选/锁定后显示；悬停预览不显示）
		if hasSel {
			drawSizeLabel(fb, hole, w)
		}

		// 6. 工具栏（锁定选区后显示）
		if s.selected {
			drawToolbar(fb, hole, w, h, s.hover)
		}
	} else {
		// 未框选：全屏暗化 + 顶部提示
		win.BitBlt(fb, 0, 0, int32(w), int32(h), s.memDC2, 0, 0, win.SRCCOPY)
		tip, _ := syscall.UTF16PtrFromString("拖动鼠标框选截图区域，Esc 取消")
		tipRect := win.RECT{Left: 0, Top: int32(h/2 - 20), Right: int32(w), Bottom: int32(h/2 + 20)}
		win.SetBkMode(fb, win.TRANSPARENT)
		win.SetTextColor(fb, win.RGB(255, 255, 255))
		win.DrawTextEx(fb, tip, -1, &tipRect, win.DT_SINGLELINE|win.DT_CENTER|win.DT_VCENTER, nil)
	}

	// 原子贴图：整帧一次画到窗口
	win.BitBlt(hdc, 0, 0, int32(w), int32(h), fb, 0, 0, win.SRCCOPY)
}

// 微信绿 / 微信红（边框与工具栏图标用色）
var (
	wechatGreen = win.RGB(7, 193, 96)
	wechatRed   = win.RGB(250, 81, 81)
)

// createPen 几何画笔（画对号/叉图标用；圆帽圆角均为默认值 0）
func createPen(width int, color win.COLORREF) win.HPEN {
	return win.ExtCreatePen(
		win.PS_GEOMETRIC|win.PS_SOLID,
		uint32(width),
		&win.LOGBRUSH{LbStyle: win.BS_SOLID, LbColor: color},
		0, nil)
}

// drawStrokesOnDC 把笔画按各自粗细画到 DC
func drawStrokesOnDC(hdc win.HDC, strokes []Stroke) {
	for _, st := range strokes {
		if len(st.Points) < 2 {
			continue
		}
		width := st.Width
		if width < 1 {
			width = 1
		}
		pen := createPen(width, win.RGB(st.Color.R, st.Color.G, st.Color.B))
		oldPen := win.SelectObject(hdc, win.HGDIOBJ(pen))
		win.MoveToEx(hdc, st.Points[0].X, st.Points[0].Y, nil)
		for i := 1; i < len(st.Points); i++ {
			win.LineTo(hdc, int32(st.Points[i].X), int32(st.Points[i].Y))
		}
		win.SelectObject(hdc, oldPen)
		win.DeleteObject(win.HGDIOBJ(pen))
	}
}

// drawRectsOnDC 把方框按各自颜色/粗细画到 DC
func drawRectsOnDC(hdc win.HDC, rects []RectShape) {
	for _, rs := range rects {
		width := rs.Width
		if width < 1 {
			width = 1
		}
		pen := createPen(width, win.RGB(rs.Color.R, rs.Color.G, rs.Color.B))
		oldPen := win.SelectObject(hdc, win.HGDIOBJ(pen))
		oldBrush := win.SelectObject(hdc, win.GetStockObject(win.NULL_BRUSH))
		win.Rectangle_(hdc, int32(rs.Rect.Min.X), int32(rs.Rect.Min.Y), int32(rs.Rect.Max.X), int32(rs.Rect.Max.Y))
		win.SelectObject(hdc, oldBrush)
		win.SelectObject(hdc, oldPen)
		win.DeleteObject(win.HGDIOBJ(pen))
	}
}

// drawMaskExcluding 暗化遮罩：挖洞区域外四块贴 memDC2（保持明亮）
func drawMaskExcluding(fb win.HDC, hole image.Rectangle, w, h int) {
	if hole.Min.Y > 0 {
		win.BitBlt(fb, 0, 0, int32(w), int32(hole.Min.Y), state.memDC2, 0, 0, win.SRCCOPY)
	}
	if hole.Max.Y < h {
		win.BitBlt(fb, 0, int32(hole.Max.Y), int32(w), int32(h-hole.Max.Y), state.memDC2, 0, int32(hole.Max.Y), win.SRCCOPY)
	}
	if hole.Min.X > 0 {
		win.BitBlt(fb, 0, int32(hole.Min.Y), int32(hole.Min.X), int32(hole.Dy()), state.memDC2, 0, int32(hole.Min.Y), win.SRCCOPY)
	}
	if hole.Max.X < w {
		win.BitBlt(fb, int32(hole.Max.X), int32(hole.Min.Y), int32(w-hole.Max.X), int32(hole.Dy()), state.memDC2, int32(hole.Max.X), int32(hole.Min.Y), win.SRCCOPY)
	}
}

// drawBorder 绿色 3px 选区描边
func drawBorder(hdc win.HDC, r image.Rectangle) {
	green := win.HGDIOBJ(solidBrush(wechatGreen))
	oldBrush := win.SelectObject(hdc, green)
	oldPen := win.SelectObject(hdc, win.GetStockObject(win.NULL_PEN))
	win.Rectangle_(hdc, int32(r.Min.X), int32(r.Min.Y), int32(r.Max.X), int32(r.Min.Y)+3)
	win.Rectangle_(hdc, int32(r.Min.X), int32(r.Max.Y)-3, int32(r.Max.X), int32(r.Max.Y))
	win.Rectangle_(hdc, int32(r.Min.X), int32(r.Min.Y), int32(r.Min.X)+3, int32(r.Max.Y))
	win.Rectangle_(hdc, int32(r.Max.X)-3, int32(r.Min.Y), int32(r.Max.X), int32(r.Max.Y))
	win.SelectObject(hdc, oldPen)
	win.SelectObject(hdc, oldBrush)
}

// drawSizeLabel 选区尺寸标签（黑底白字，选区上方，出界放内部左上）
func drawSizeLabel(hdc win.HDC, r image.Rectangle, screenW int) {
	label := fmt.Sprintf("%d × %d", r.Dx(), r.Dy())
	lx, ly := r.Min.X, r.Min.Y-26
	if ly < 0 {
		ly = r.Min.Y + 4
	}
	if lx+120 > screenW {
		lx = screenW - 130
	}
	labelRect := win.RECT{Left: int32(lx), Top: int32(ly), Right: int32(lx + 120), Bottom: int32(ly + 22)}
	win.SetBkMode(hdc, win.OPAQUE)
	win.SetBkColor(hdc, win.RGB(0, 0, 0))
	win.SetTextColor(hdc, win.RGB(255, 255, 255))
	lp, _ := syscall.UTF16PtrFromString(label)
	win.DrawTextEx(hdc, lp, -1, &labelRect, win.DT_SINGLELINE|win.DT_CENTER|win.DT_VCENTER, nil)
}

// drawToolbar 选区下方白色工具栏：绿色对号保存 / 红色叉取消，悬停显示气泡提示
func drawToolbar(hdc win.HDC, sel image.Rectangle, screenW, screenH, hover int) {
	bar, buttons := toolbarRects(sel, screenW, screenH)

	// 白色圆角栏底
	barBrush := solidBrush(win.RGB(255, 255, 255))
	defer win.DeleteObject(win.HGDIOBJ(barBrush))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(barBrush))
	oldPen := win.SelectObject(hdc, win.GetStockObject(win.NULL_PEN))
	win.RoundRect(hdc, int32(bar.Min.X), int32(bar.Min.Y), int32(bar.Max.X), int32(bar.Max.Y), 6, 6)

	// 工具激活高亮（浅蓝圆角）
	activeBtn := -1
	switch {
	case state.penMode:
		activeBtn = tbPen
	case state.rectMode:
		activeBtn = tbRect
	}
	if activeBtn >= 0 {
		activeBrush := solidBrush(win.RGB(224, 236, 255))
		defer win.DeleteObject(win.HGDIOBJ(activeBrush))
		win.SelectObject(hdc, win.HGDIOBJ(activeBrush))
		b := buttons[activeBtn]
		win.RoundRect(hdc, int32(b.Min.X+2), int32(b.Min.Y+2), int32(b.Max.X-2), int32(b.Max.Y-2), 4, 4)
		win.SelectObject(hdc, win.HGDIOBJ(barBrush))
	}

	// 按钮悬停高亮（浅灰圆角）
	if hover >= 0 {
		hoverBrush := solidBrush(win.RGB(238, 241, 245))
		defer win.DeleteObject(win.HGDIOBJ(hoverBrush))
		win.SelectObject(hdc, win.HGDIOBJ(hoverBrush))
		b := buttons[hover]
		win.RoundRect(hdc, int32(b.Min.X+2), int32(b.Min.Y+2), int32(b.Max.X-2), int32(b.Max.Y-2), 4, 4)
		win.SelectObject(hdc, win.HGDIOBJ(barBrush))
	}

	// 按钮之间分隔线（浅灰）
	divPen := createPen(1, win.RGB(220, 224, 230))
	win.SelectObject(hdc, win.HGDIOBJ(divPen))
	win.SelectObject(hdc, win.GetStockObject(win.NULL_BRUSH))
	for i := 1; i < 5; i++ {
		divX := bar.Min.X + (bar.Max.X-bar.Min.X)*i/5
		win.MoveToEx(hdc, divX, bar.Min.Y+10, nil)
		win.LineTo(hdc, int32(divX), int32(bar.Max.Y-10))
	}
	win.DeleteObject(win.HGDIOBJ(divPen))
	win.SelectObject(hdc, oldPen)
	win.SelectObject(hdc, oldBrush)

	// 图标（Segoe MDL2 Assets 字形）：画笔 / 方框 / 文字 / 对号保存 / 叉取消
	glyphColor := func(active bool) win.COLORREF {
		if active {
			return win.RGB(37, 99, 235) // 激活态蓝色
		}
		return win.RGB(90, 96, 110)
	}
	drawIconGlyph(hdc, buttons[tbPen], "", glyphColor(state.penMode))
	drawIconGlyph(hdc, buttons[tbRect], "", glyphColor(state.rectMode))
	drawIconGlyph(hdc, buttons[tbText], "", glyphColor(state.textMode))
	drawIconGlyph(hdc, buttons[tbSave], "", wechatGreen)
	drawIconGlyph(hdc, buttons[tbCancel], "", wechatRed)

	// 悬停提示气泡
	if hover >= 0 {
		drawToolbarTip(hdc, buttons[hover], bar, [5]string{"画笔", "方框", "文字", "保存", "取消"}[hover])
	}

	// 画笔/方框/文字模式：选项面板（调色板 + 粗细/字号）
	if state.penMode || state.rectMode || state.textMode {
		drawOptionsPanel(hdc, bar, screenH, state)
	}
}

// drawOptionsPanel 选项面板：白底圆角面板，色点行 + 第二行（画笔/方框=粗细条，文字=字号）
func drawOptionsPanel(hdc win.HDC, bar image.Rectangle, screenH int, s *overlayState) {
	second := ""
	if s.penMode || s.rectMode {
		second = "thickness"
	}
	if s.textMode {
		second = "size"
	}
	panel, swatches, cells := optionsPanelRects(bar, screenH, second)

	// 白底圆角
	barBrush := solidBrush(win.RGB(255, 255, 255))
	defer win.DeleteObject(win.HGDIOBJ(barBrush))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(barBrush))
	oldPen := win.SelectObject(hdc, win.GetStockObject(win.NULL_PEN))
	win.RoundRect(hdc, int32(panel.Min.X), int32(panel.Min.Y), int32(panel.Max.X), int32(panel.Max.Y), 8, 8)

	// 色点：抗锯齿字形 ●（Segoe UI Symbol），白色点加灰环描边，当前色加高亮环
	for i, sw := range swatches {
		c := penPalette[i]
		if c == (color.RGBA{255, 255, 255, 255}) { // 白色点描边环
			drawSwatchGlyph(hdc, sw, "◯", win.RGB(190, 194, 202), s.swatchFont)
		}
		drawSwatchGlyph(hdc, sw, "●", win.RGB(c.R, c.G, c.B), s.swatchFont)
		if c == s.penColor { // 当前色高亮环（稍大一圈）
			ring := sw.Inset(-4)
			drawSwatchGlyph(hdc, ring, "◯", win.RGB(110, 118, 132), s.swatchFont)
		}
	}

	// 第二行
	switch second {
	case "thickness":
		// 粗细行：横条示意线宽，当前档蓝色高亮底
		for i, th := range cells {
			width := penWidths[i]
			cx := (th.Min.X + th.Max.X) / 2
			cy := (th.Min.Y + th.Max.Y) / 2
			if width == s.penWidth { // 当前档：浅蓝圆角底
				activeBrush := solidBrush(win.RGB(224, 236, 255))
				win.SelectObject(hdc, win.HGDIOBJ(activeBrush))
				win.RoundRect(hdc, int32(th.Min.X+1), int32(th.Min.Y+1), int32(th.Max.X-1), int32(th.Max.Y-1), 8, 8)
				win.SelectObject(hdc, win.HGDIOBJ(barBrush))
			}
			linePen := createPen(width, win.RGB(70, 76, 92))
			win.SelectObject(hdc, win.HGDIOBJ(linePen))
			win.MoveToEx(hdc, cx-16, cy, nil)
			win.LineTo(hdc, int32(cx+16), int32(cy))
			win.DeleteObject(win.HGDIOBJ(linePen))
		}
	case "size":
		// 字号行：小/中/大，当前档蓝色高亮底
		labels := [3]string{"小", "中", "大"}
		for i, cell := range cells {
			if textSizes[i] == s.textSize { // 当前档：浅蓝圆角底
				activeBrush := solidBrush(win.RGB(224, 236, 255))
				win.SelectObject(hdc, win.HGDIOBJ(activeBrush))
				win.RoundRect(hdc, int32(cell.Min.X+1), int32(cell.Min.Y+1), int32(cell.Max.X-1), int32(cell.Max.Y-1), 8, 8)
				win.SelectObject(hdc, win.HGDIOBJ(barBrush))
			}
			oldFont := win.SelectObject(hdc, win.HGDIOBJ(s.textFonts[1]))
			win.SetBkMode(hdc, win.TRANSPARENT)
			win.SetTextColor(hdc, win.RGB(70, 76, 92))
			lp, _ := syscall.UTF16PtrFromString(labels[i])
			rc := win.RECT{Left: int32(cell.Min.X), Top: int32(cell.Min.Y), Right: int32(cell.Max.X), Bottom: int32(cell.Max.Y)}
			win.DrawTextEx(hdc, lp, -1, &rc, win.DT_SINGLELINE|win.DT_CENTER|win.DT_VCENTER, nil)
			win.SelectObject(hdc, oldFont)
		}
	}
	win.SelectObject(hdc, oldPen)
	win.SelectObject(hdc, oldBrush)
}

// drawTextItems 文字标注（多行）+ 输入态反馈（下划线 + 呼吸光标）
func drawTextItems(hdc win.HDC, s *overlayState) {
	for _, it := range s.textItems {
		drawTextLines(hdc, it)
	}
	// 输入态：当前编辑项画下划线与闪烁光标
	if s.inputActive && len(s.textItems) > 0 {
		it := s.textItems[len(s.textItems)-1]
		font := fontForSize(it.Size)
		oldFont := win.SelectObject(hdc, win.HGDIOBJ(font))
		lines := strings.Split(it.Text, "\n")
		lastLine := lines[len(lines)-1]
		tw := measureText(hdc, lastLine)
		lineIdx := len(lines) - 1
		lastY := it.Pos.Y + lineIdx*lineHeight(it.Size)
		cx := it.Pos.X + tw + 1

		// 下划线（当前行下方，空文本也有最小长度提示）
		ulW := tw
		if ulW < 18 {
			ulW = 18
		}
		ulY := lastY + it.Size + 5
		ulPen := createPen(1, win.RGB(it.Color.R, it.Color.G, it.Color.B))
		oldPen := win.SelectObject(hdc, win.HGDIOBJ(ulPen))
		win.MoveToEx(hdc, it.Pos.X, ulY, nil)
		win.LineTo(hdc, int32(it.Pos.X+ulW), int32(ulY))
		win.DeleteObject(win.HGDIOBJ(ulPen))

		// 呼吸光标：黑描边 + 白芯，深浅背景都可见
		if s.caretOn {
			blackPen := createPen(3, win.RGB(0, 0, 0))
			win.SelectObject(hdc, win.HGDIOBJ(blackPen))
			win.MoveToEx(hdc, cx, lastY-2, nil)
			win.LineTo(hdc, int32(cx), int32(lastY+it.Size+2))
			win.DeleteObject(win.HGDIOBJ(blackPen))
			whitePen := createPen(1, win.RGB(255, 255, 255))
			win.SelectObject(hdc, win.HGDIOBJ(whitePen))
			win.MoveToEx(hdc, cx, lastY-1, nil)
			win.LineTo(hdc, int32(cx), int32(lastY+it.Size+1))
			win.DeleteObject(win.HGDIOBJ(whitePen))
		}
		win.SelectObject(hdc, oldPen)
		win.SelectObject(hdc, oldFont)
	}
}

// drawTextLines 按行绘制一条文字标注（\n 分行，逐行 TextOut）
func drawTextLines(hdc win.HDC, it TextItem) {
	font := fontForSize(it.Size)
	oldFont := win.SelectObject(hdc, win.HGDIOBJ(font))
	win.SetBkMode(hdc, win.TRANSPARENT)
	win.SetTextColor(hdc, win.RGB(it.Color.R, it.Color.G, it.Color.B))
	for i, ln := range strings.Split(it.Text, "\n") {
		if ln == "" {
			continue
		}
		lp, _ := syscall.UTF16PtrFromString(ln)
		win.TextOut(hdc, int32(it.Pos.X), int32(it.Pos.Y+i*lineHeight(it.Size)), lp, int32(len([]rune(ln))))
	}
	win.SelectObject(hdc, oldFont)
}

// fontForSize 按字号取预创建字体
func fontForSize(size int) win.HFONT {
	for i, sz := range textSizes {
		if sz == size {
			return state.textFonts[i]
		}
	}
	return state.textFonts[1] // 默认中号
}

// lineHeight 行高（字号 + 间距）
func lineHeight(size int) int {
	return size + 8
}

// measureText 测量单行文本宽度（需已选好字体）
func measureText(hdc win.HDC, text string) int {
	if text == "" {
		return 0
	}
	lp, _ := syscall.UTF16PtrFromString(text)
	var rc win.RECT
	win.DrawTextEx(hdc, lp, int32(len([]rune(text))), &rc, win.DT_CALCRECT|win.DT_SINGLELINE, nil)
	return int(rc.Right)
}

// drawSwatchGlyph 用色点字体在矩形内居中绘制字形（ClearType 抗锯齿）
func drawSwatchGlyph(hdc win.HDC, rect image.Rectangle, glyph string, c win.COLORREF, font win.HFONT) {
	oldFont := win.SelectObject(hdc, win.HGDIOBJ(font))
	win.SetBkMode(hdc, win.TRANSPARENT)
	win.SetTextColor(hdc, c)
	lp, _ := syscall.UTF16PtrFromString(glyph)
	rc := win.RECT{Left: int32(rect.Min.X), Top: int32(rect.Min.Y), Right: int32(rect.Max.X), Bottom: int32(rect.Max.Y)}
	win.DrawTextEx(hdc, lp, -1, &rc, win.DT_SINGLELINE|win.DT_CENTER|win.DT_VCENTER, nil)
	win.SelectObject(hdc, oldFont)
}

// drawIconGlyph 用图标字体在按钮居中绘制字形
func drawIconGlyph(hdc win.HDC, b image.Rectangle, glyph string, color win.COLORREF) {
	oldFont := win.SelectObject(hdc, win.HGDIOBJ(state.font))
	win.SetBkMode(hdc, win.TRANSPARENT)
	win.SetTextColor(hdc, color)
	lp, _ := syscall.UTF16PtrFromString(glyph)
	rc := win.RECT{Left: int32(b.Min.X), Top: int32(b.Min.Y), Right: int32(b.Max.X), Bottom: int32(b.Max.Y)}
	win.DrawTextEx(hdc, lp, -1, &rc, win.DT_SINGLELINE|win.DT_CENTER|win.DT_VCENTER, nil)
	win.SelectObject(hdc, oldFont)
}

// drawToolbarTip 悬停提示气泡（按钮下方居中）
func drawToolbarTip(hdc win.HDC, btn, bar image.Rectangle, label string) {
	const tipW, tipH = 52, 24
	x := (btn.Min.X+btn.Max.X)/2 - tipW/2
	y := bar.Max.Y + 8

	brush := solidBrush(win.RGB(45, 48, 56))
	defer win.DeleteObject(win.HGDIOBJ(brush))
	oldBrush := win.SelectObject(hdc, win.HGDIOBJ(brush))
	oldPen := win.SelectObject(hdc, win.GetStockObject(win.NULL_PEN))
	win.RoundRect(hdc, int32(x), int32(y), int32(x+tipW), int32(y+tipH), 6, 6)
	win.SelectObject(hdc, oldPen)
	win.SelectObject(hdc, oldBrush)

	win.SetBkMode(hdc, win.TRANSPARENT)
	win.SetTextColor(hdc, win.RGB(255, 255, 255))
	lp, _ := syscall.UTF16PtrFromString(label)
	rc := win.RECT{Left: int32(x), Top: int32(y), Right: int32(x + tipW), Bottom: int32(y + tipH)}
	win.DrawTextEx(hdc, lp, -1, &rc, win.DT_SINGLELINE|win.DT_CENTER|win.DT_VCENTER, nil)
}

// solidBrush 创建纯色画刷（lxn/win 无 CreateSolidBrush，用 CreateBrushIndirect）
func solidBrush(color win.COLORREF) win.HBRUSH {
	return win.CreateBrushIndirect(&win.LOGBRUSH{
		LbStyle: win.BS_SOLID,
		LbColor: color,
	})
}

// pointFromLParam 解析鼠标消息中的屏幕点（客户区坐标）
func pointFromLParam(lParam uintptr) win.POINT {
	return win.POINT{
		X: int32(int16(lParam & 0xFFFF)),
		Y: int32(int16((lParam >> 16) & 0xFFFF)),
	}
}
