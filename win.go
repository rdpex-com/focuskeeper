package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")

	procSetWinEventHook       = user32.NewProc("SetWinEventHook")
	procGetMessage            = user32.NewProc("GetMessageW")
	procDispatchMessage       = user32.NewProc("DispatchMessageW")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procPostMessage           = user32.NewProc("PostMessageW")
	procRegisterClassEx       = user32.NewProc("RegisterClassExW")
	procCreateWindowEx        = user32.NewProc("CreateWindowExW")
	procDefWindowProc         = user32.NewProc("DefWindowProcW")
	procSetTimer              = user32.NewProc("SetTimer")
	procKillTimer             = user32.NewProc("KillTimer")
	procGetForegroundWindow   = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop      = user32.NewProc("BringWindowToTop")
	procAttachThreadInput     = user32.NewProc("AttachThreadInput")
	procSendInput             = user32.NewProc("SendInput")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procWindowFromPoint       = user32.NewProc("WindowFromPoint")
	procGetAncestor           = user32.NewProc("GetAncestor")
	procGetClassName          = user32.NewProc("GetClassNameW")
	procGetWindowText         = user32.NewProc("GetWindowTextW")
	procGetWindowThreadPID    = user32.NewProc("GetWindowThreadProcessId")
	procEnumWindows           = user32.NewProc("EnumWindows")
	procIsWindowVisible       = user32.NewProc("IsWindowVisible")
	procIsIconic              = user32.NewProc("IsIconic")
	procGetWindow             = user32.NewProc("GetWindow")
	procGetWindowLong         = user32.NewProc("GetWindowLongW")
	procGetWindowRect         = user32.NewProc("GetWindowRect")
	procMonitorFromWindow     = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfo        = user32.NewProc("GetMonitorInfoW")
	procGetAsyncKeyState      = user32.NewProc("GetAsyncKeyState")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procSetDpiAwareCtx        = user32.NewProc("SetProcessDpiAwarenessContext")
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	procAppendMenu            = user32.NewProc("AppendMenuW")
	procTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	procDestroyMenu           = user32.NewProc("DestroyMenu")
	procCheckMenuRadioItem    = user32.NewProc("CheckMenuRadioItem")
	procCreateIconIndirect    = user32.NewProc("CreateIconIndirect")
	procDestroyIcon           = user32.NewProc("DestroyIcon")
	procShowWindow            = user32.NewProc("ShowWindow")
	procGetModuleHandle       = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadId    = kernel32.NewProc("GetCurrentThreadId")
	procCreateMutex           = kernel32.NewProc("CreateMutexW")
	procShellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	procShellExecute          = shell32.NewProc("ShellExecuteW")
	procCreateDIBSection      = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap          = gdi32.NewProc("CreateBitmap")
	procDeleteObject          = gdi32.NewProc("DeleteObject")
	procCreateSolidBrush      = gdi32.NewProc("CreateSolidBrush")
	procDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")
)

const (
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmTimer        = 0x0113
	wmCommand      = 0x0111
	wmNull         = 0x0000
	wmApp          = 0x8000
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	gaRoot         = 2
	gwOwner        = 4
	gwlStyle       = -16
	gwlExStyle     = -20
	wsCaption      = 0x00C00000
	wsExToolWindow = 0x00000080
	processQueryLI = 0x1000
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// newWindow registers a class and creates a window of it; style 0 and no size make an invisible helper window.
func newWindow(class, title string, proc uintptr, brush uintptr, exStyle, style uint32, r rect) uintptr {
	inst, _, _ := procGetModuleHandle.Call(0)
	wc := wndClassEx{WndProc: proc, Instance: inst, ClassName: utf16(class), Background: brush}
	wc.Size = uint32(unsafe.Sizeof(wc))
	wc.Cursor = loadArrow()
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	h, _, _ := procCreateWindowEx.Call(uintptr(exStyle), uintptr(unsafe.Pointer(utf16(class))), uintptr(unsafe.Pointer(utf16(title))),
		uintptr(style), uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0, 0, inst, 0)
	return h
}

func loadArrow() uintptr {
	h, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512) // IDC_ARROW
	return h
}

func messageLoop() {
	var msg [48]byte
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
	}
}

func foreground() uintptr {
	h, _, _ := procGetForegroundWindow.Call()
	return h
}

func cursor() point {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func rootAt(p point) uintptr {
	under, _, _ := procWindowFromPoint.Call(uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32)
	root, _, _ := procGetAncestor.Call(under, gaRoot)
	return root
}

func className(h uintptr) string {
	if h == 0 {
		return ""
	}
	buf := make([]uint16, 256)
	n, _, _ := procGetClassName.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf[:n])
}

func windowText(h uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetWindowText.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf[:n])
}

func threadOf(h uintptr) (tid, pid uint32) {
	r, _, _ := procGetWindowThreadPID.Call(h, uintptr(unsafe.Pointer(&pid)))
	return uint32(r), pid
}

func exeOf(h uintptr) string {
	_, pid := threadOf(h)
	if pid == 0 {
		return ""
	}
	p, err := windows.OpenProcess(processQueryLI, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(p)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if windows.QueryFullProcessImageName(p, 0, &buf[0], &size) != nil {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buf[:size]))
}

func isVisible(h uintptr) bool { r, _, _ := procIsWindowVisible.Call(h); return r != 0 }
func isIconic(h uintptr) bool  { r, _, _ := procIsIconic.Call(h); return r != 0 }

func windowRect(h uintptr) rect {
	var r rect
	procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}

func style(h uintptr, which int32) uint32 {
	r, _, _ := procGetWindowLong.Call(h, uintptr(which))
	return uint32(r)
}

// fullScreen: the window covers its whole monitor and has no caption - a full-screen RDP client or game, not a
// maximized window (that one keeps its title bar).
func fullScreen(h uintptr) bool {
	if h == 0 || style(h, gwlStyle)&wsCaption == wsCaption {
		return false
	}
	mon, _, _ := procMonitorFromWindow.Call(h, 2) // MONITOR_DEFAULTTONEAREST
	var mi struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}
	mi.Size = uint32(unsafe.Sizeof(mi))
	if r, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return false
	}
	w := windowRect(h)
	return w.Left <= mi.Monitor.Left && w.Top <= mi.Monitor.Top && w.Right >= mi.Monitor.Right && w.Bottom >= mi.Monitor.Bottom
}

func keyDown(vk int) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

func mouseButtonDown() bool {
	return keyDown(0x01) || keyDown(0x02) || keyDown(0x04) || keyDown(0x05) || keyDown(0x06)
}

// anyKeyDown: a keyboard key is held right now (typing in progress); mouse buttons are not counted.
func anyKeyDown() bool {
	for vk := 0x08; vk <= 0xFE; vk++ {
		if keyDown(vk) {
			return true
		}
	}
	return false
}

// shellClasses: the foreground is "nothing an app": the desktop or the taskbar.
var shellClasses = map[string]bool{"Progman": true, "WorkerW": true, "Shell_TrayWnd": true, "Shell_SecondaryTrayWnd": true}

func isShell(h uintptr) bool { return h == 0 || shellClasses[className(h)] }

// findTarget: the biggest visible, not minimized, unowned top-level window with a title of the target program
// (exe name, case-insensitive) and, if given, of that window class. VMware Workstation has helper windows too; its
// main window is the big titled one.
func findTarget(exe, class string) uintptr {
	enum = enumState{exe: exe, class: class}
	procEnumWindows.Call(enumCallback, 0)
	return enum.best
}

type enumState struct {
	exe, class string
	best       uintptr
	area       int64
}

var (
	enum enumState
	// one callback for the program's life: windows.NewCallback slots are never freed
	enumCallback = windows.NewCallback(func(h, _ uintptr) uintptr {
		if !isVisible(h) || isIconic(h) {
			return 1
		}
		if o, _, _ := procGetWindow.Call(h, gwOwner); o != 0 {
			return 1
		}
		if style(h, gwlExStyle)&wsExToolWindow != 0 || windowText(h) == "" {
			return 1
		}
		if enum.class != "" && className(h) != enum.class {
			return 1
		}
		if !strings.EqualFold(exeOf(h), enum.exe) {
			return 1
		}
		r := windowRect(h)
		if a := int64(r.Right-r.Left) * int64(r.Bottom-r.Top); a > enum.area {
			enum.best, enum.area = h, a
		}
		return 1
	})
)

// coveredBy walks the top-level windows from the top of the z-order down to the target and returns the first one
// that overlaps it: visible, not minimized, not cloaked (hidden UWP windows, other virtual desktops), not a tool
// window, not click-through, not the taskbar or desktop, not a menu, not of this program. 0 - nothing lies over it.
func coveredBy(target uintptr) uintptr {
	cover = coverState{target: target, rect: windowRect(target), pid: uint32(os.Getpid())}
	procEnumWindows.Call(coverCallback, 0)
	return cover.over
}

type coverState struct {
	target, over uintptr
	rect         rect
	pid          uint32
}

var (
	cover         coverState
	coverCallback = windows.NewCallback(func(h, _ uintptr) uintptr {
		if h == cover.target {
			return 0 // reached the target: everything below does not matter
		}
		if !isVisible(h) || isIconic(h) || cloaked(h) {
			return 1
		}
		const wsExTransparent = 0x00000020
		if style(h, gwlExStyle)&(wsExToolWindow|wsExTransparent) != 0 {
			return 1
		}
		if c := className(h); shellClasses[c] || c == "#32768" {
			return 1
		}
		if _, pid := threadOf(h); pid == cover.pid {
			return 1
		}
		r := windowRect(h)
		if r.Left < cover.rect.Right && r.Right > cover.rect.Left && r.Top < cover.rect.Bottom && r.Bottom > cover.rect.Top {
			cover.over = h
			return 0
		}
		return 1
	})
)

func cloaked(h uintptr) bool {
	var v uint32
	r, _, _ := procDwmGetWindowAttribute.Call(h, 14, uintptr(unsafe.Pointer(&v)), 4) // DWMWA_CLOAKED
	return r == 0 && v != 0
}

// systemLang: ru when the Windows UI language is Russian, else en.
func systemLang() string {
	id, _, _ := kernel32.NewProc("GetUserDefaultUILanguage").Call()
	if id&0x3ff == 0x19 { // LANG_RUSSIAN
		return "ru"
	}
	return "en"
}
