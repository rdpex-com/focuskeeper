package main

import (
	"math"
	"unsafe"
)

// Tray icon, drawn at run time (no resource files): a blue rounded square with a white focus ring and a dot. Grey
// when paused, amber in the dry mode.
const (
	colorOn     = 0x2f6fed
	colorPaused = 0x8a8f98
	colorDry    = 0xd08a1e
)

func makeIcon(rgb uint32, dot bool) uintptr {
	s, _, _ := procGetSystemMetrics.Call(49) // SM_CXSMICON, at the process DPI
	size := int(s)
	if size < 16 {
		size = 16
	}
	px := drawIcon(size, rgb, dot)
	var bi struct {
		Size                         uint32
		Width, Height                int32
		Planes, BitCount             uint16
		Compression, SizeImage       uint32
		XPels, YPels, Used, Required int32
	}
	bi.Size = uint32(unsafe.Sizeof(bi))
	bi.Width, bi.Height, bi.Planes, bi.BitCount = int32(size), -int32(size), 1, 32 // top-down
	var bits unsafe.Pointer
	color, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 {
		return 0
	}
	copy(unsafe.Slice((*byte)(bits), len(px)), px)
	mask := make([]byte, ((size+15)/16)*2*size)
	maskBmp, _, _ := procCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, uintptr(unsafe.Pointer(&mask[0])))
	ii := struct {
		Icon        int32
		HotX, HotY  uint32
		Mask, Color uintptr
	}{Icon: 1, Mask: maskBmp, Color: color}
	h, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	procDeleteObject.Call(color)
	procDeleteObject.Call(maskBmp)
	return h
}

// drawIcon returns BGRA pixels (straight alpha), 4x4 supersampled.
func drawIcon(size int, rgb uint32, dot bool) []byte {
	const ss = 4
	n := float64(size)
	radius, ring, ringW, dotR := 0.22*n, 0.27*n, 0.09*n, 0.10*n
	c := n / 2
	out := make([]byte, size*size*4)
	br, bg, bb := float64(rgb>>16&0xff), float64(rgb>>8&0xff), float64(rgb&0xff)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var cover, white float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					fx, fy := float64(x)+(float64(sx)+0.5)/ss, float64(y)+(float64(sy)+0.5)/ss
					if !inRounded(fx, fy, n, radius) {
						continue
					}
					cover++
					d := math.Hypot(fx-c, fy-c)
					if math.Abs(d-ring) <= ringW/2 || (dot && d <= dotR) {
						white++
					}
				}
			}
			if cover == 0 {
				continue
			}
			w := white / cover
			i := (y*size + x) * 4
			out[i] = byte(bb + (255-bb)*w)
			out[i+1] = byte(bg + (255-bg)*w)
			out[i+2] = byte(br + (255-br)*w)
			out[i+3] = byte(255 * cover / (ss * ss))
		}
	}
	return out
}

func inRounded(x, y, n, r float64) bool {
	cx := math.Max(r, math.Min(n-r, x))
	cy := math.Max(r, math.Min(n-r, y))
	return math.Hypot(x-cx, y-cy) <= r
}
