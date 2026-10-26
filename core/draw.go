package core

import (
	"image"
	"image/color"
	"math"
)

// 画笔笔画（蒙层上绘制的标注，最终合成到截图）

// Stroke 一笔：连续点列 + 颜色 + 粗细（点坐标为背景图坐标）
type Stroke struct {
	Points []image.Point
	Color  color.RGBA
	Width  int // 线宽（像素）
}

// RectShape 方框标注：矩形 + 颜色 + 线宽
type RectShape struct {
	Rect  image.Rectangle
	Color color.RGBA
	Width int
}

// DrawRects 将方框按各自颜色/粗细画到图像上（四条边）
func DrawRects(img *image.RGBA, rects []RectShape) {
	for _, rs := range rects {
		w := rs.Width
		if w < 1 {
			w = 1
		}
		r := rs.Rect
		drawThickLine(img, image.Pt(r.Min.X, r.Min.Y), image.Pt(r.Max.X-1, r.Min.Y), rs.Color, w)
		drawThickLine(img, image.Pt(r.Min.X, r.Max.Y-1), image.Pt(r.Max.X-1, r.Max.Y-1), rs.Color, w)
		drawThickLine(img, image.Pt(r.Min.X, r.Min.Y), image.Pt(r.Min.X, r.Max.Y-1), rs.Color, w)
		drawThickLine(img, image.Pt(r.Max.X-1, r.Min.Y), image.Pt(r.Max.X-1, r.Max.Y-1), rs.Color, w)
	}
}

// DrawStrokes 将笔画按各自粗细画到图像上
func DrawStrokes(img *image.RGBA, strokes []Stroke) {
	for _, st := range strokes {
		if len(st.Points) < 2 {
			continue
		}
		for i := 1; i < len(st.Points); i++ {
			drawThickLine(img, st.Points[i-1], st.Points[i], st.Color, st.Width)
		}
	}
}

// drawThickLine 沿线段以圆形笔刷落点（步长 1px，线条连续）
func drawThickLine(img *image.RGBA, a, b image.Point, c color.RGBA, width int) {
	dx, dy := float64(b.X-a.X), float64(b.Y-a.Y)
	steps := int(math.Hypot(dx, dy)) + 1
	r := width / 2 // 半径（1px 线 → 0，单像素点）
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		cx := a.X + int(dx*t)
		cy := a.Y + int(dy*t)
		dot(img, cx, cy, r, c)
	}
}

// dot 在 (cx,cy) 画半径 r 的笔刷点（含边界检查）
func dot(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	b := img.Bounds()
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if r > 0 {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy > r*r+1 {
					continue
				}
			}
			if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
				continue
			}
			img.Set(x, y, c)
		}
	}
}
