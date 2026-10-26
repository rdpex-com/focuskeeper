// check: the focuskeeper scenario on a test machine's desktop - never on a computer someone works at:
// it moves the mouse, clicks and types. A stand-in window plays the VMware window, Notepad plays the other app.
//
//	go build -ldflags "-H windowsgui" -o fkcheck.exe ./check
//	fkcheck.exe -fk <focuskeeper.exe> -dir <work folder> [-method auto] [-steps all|b]
//
// Steps (each prints the foreground after it and what was expected; focuskeeper's log follows):
//
//	A-desktop     Notepad in front, a click on the desktop: the shell is in front -> trigger A gives the stand-in the focus
//	A-minimize    Notepad in front, Notepad minimized: whatever Windows does, the stand-in ends up in front
//	B-move-rest   Notepad in front, the mouse moves onto the stand-in and rests -> trigger B
//	B-still-typing Notepad in front and typed into, the mouse parked over the stand-in without moving -> no action
//	B-fullscreen  a full-screen window in front, the mouse moves and rests -> no action
//	A-fullscreen-minimized the full-screen window minimized (a full-screen RDP client minimized) -> trigger A
//
// Results go to <dir>\check.jsonl (the scheduled task that starts this has no console).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procSendInput          = user32.NewProc("SendInput")
	procSetCursorPos       = user32.NewProc("SetCursorPos")
	procGetForegroundWnd   = user32.NewProc("GetForegroundWindow")
	procWindowFromPoint    = user32.NewProc("WindowFromPoint")
	procGetAncestor        = user32.NewProc("GetAncestor")
	procGetClassName       = user32.NewProc("GetClassNameW")
	procGetWindowText      = user32.NewProc("GetWindowTextW")
	procGetWindowThreadPID = user32.NewProc("GetWindowThreadProcessId")
	procEnumWindows        = user32.NewProc("EnumWindows")
	procIsWindowVisible    = user32.NewProc("IsWindowVisible")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
	procShowWindow         = user32.NewProc("ShowWindow")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procSetDpiAwareCtx     = user32.NewProc("SetProcessDpiAwarenessContext")
)

type pt struct{ X, Y int32 }

var out *os.File

func emit(row map[string]any) {
	row["ts"] = time.Now().Format("15:04:05.000")
	b, _ := json.Marshal(row)
	out.Write(append(b, '\n'))
}

func setCursor(p pt) { procSetCursorPos.Call(uintptr(p.X), uintptr(p.Y)) }

type input struct {
	typ uint32
	_   uint32
	a   [4]uint64 // MOUSEINPUT or KEYBDINPUT
}

func mouse(flags uint32) {
	var in input
	*(*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(&in.a)) + 12)) = flags // MOUSEINPUT.dwFlags
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func click(p pt) {
	setCursor(p)
	time.Sleep(60 * time.Millisecond)
	mouse(0x0002) // LEFTDOWN
	time.Sleep(40 * time.Millisecond)
	mouse(0x0004) // LEFTUP
}

func typeText(s string) {
	for _, r := range s {
		for _, up := range []uint32{0, 2} { // KEYEVENTF_UNICODE (4) | KEYUP (2)
			var in input
			in.typ = 1
			*(*uint16)(unsafe.Add(unsafe.Pointer(&in.a), 2)) = uint16(r) // wScan
			*(*uint32)(unsafe.Add(unsafe.Pointer(&in.a), 4)) = 4 | up    // dwFlags
			procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
			time.Sleep(40 * time.Millisecond)
		}
		time.Sleep(120 * time.Millisecond)
	}
}

// glide moves the cursor in small steps, like a hand, ~300 ms.
func glide(from, to pt) {
	for i := 1; i <= 12; i++ {
		setCursor(pt{from.X + (to.X-from.X)*int32(i)/12, from.Y + (to.Y-from.Y)*int32(i)/12})
		time.Sleep(25 * time.Millisecond)
	}
}

func className(h uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassName.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf[:n])
}

func describe(h uintptr) map[string]any {
	buf := make([]uint16, 256)
	n, _, _ := procGetWindowText.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return map[string]any{"cls": className(h), "title": syscall.UTF16ToString(buf[:n])}
}

func fg() uintptr { h, _, _ := procGetForegroundWnd.Call(); return h }

func rootAt(p pt) (under, root uintptr) {
	under, _, _ = procWindowFromPoint.Call(uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32)
	root, _, _ = procGetAncestor.Call(under, 2)
	return
}

var found struct {
	pid   uint32
	class string
	h     uintptr
}

var enumCB = windows.NewCallback(func(h, _ uintptr) uintptr {
	var pid uint32
	procGetWindowThreadPID.Call(h, uintptr(unsafe.Pointer(&pid)))
	if v, _, _ := procIsWindowVisible.Call(h); v != 0 && pid == found.pid && className(h) == found.class {
		found.h = h
		return 0
	}
	return 1
})

func windowOf(pid uint32, class string) uintptr {
	for i := 0; i < 50; i++ {
		found.pid, found.class, found.h = pid, class, 0
		procEnumWindows.Call(enumCB, 0)
		if found.h != 0 {
			return found.h
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0
}

func place(h uintptr, x, y, w, ht int32) {
	procSetWindowPos.Call(h, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(ht), 0x0004|0x0010) // NOZORDER|NOACTIVATE
}

// desktopPoint: a point where the desktop shows (nothing covers it), for the click that puts the shell in front.
func desktopPoint(w, h int32) (pt, bool) {
	for y := h - 120; y > 40; y -= 40 {
		for x := w - 40; x > 40; x -= 40 {
			if _, root := rootAt(pt{x, y}); className(root) == "Progman" || className(root) == "WorkerW" {
				return pt{x, y}, true
			}
		}
	}
	return pt{}, false
}

func check(step string, want uintptr, wantText string) {
	f := fg()
	emit(map[string]any{"step": step, "fg": describe(f), "expect": wantText, "ok": f == want})
}

func main() {
	fk := flag.String("fk", "", "focuskeeper.exe")
	dir := flag.String("dir", "", "work folder")
	method := flag.String("method", "auto", "focuskeeper -method")
	steps := flag.String("steps", "all", "all or b (trigger B only: the foreground-lock comparison)")
	flag.Parse()
	runtime.LockOSThread()
	procSetDpiAwareCtx.Call(^uintptr(3))
	os.MkdirAll(*dir, 0o755)
	var err error
	if out, err = os.Create(filepath.Join(*dir, "check-"+*method+".jsonl")); err != nil {
		return
	}
	defer out.Close()
	sw, _, _ := procGetSystemMetrics.Call(0)
	sh, _, _ := procGetSystemMetrics.Call(1)
	W, H := int32(sw), int32(sh)
	emit(map[string]any{"step": "setup", "screen": []int32{W, H}, "method": *method})

	var procs []*exec.Cmd
	defer func() {
		for _, p := range procs {
			p.Process.Kill()
		}
	}()
	start := func(name string, args ...string) *exec.Cmd {
		c := exec.Command(name, args...)
		if c.Start() != nil {
			emit(map[string]any{"step": "start", "error": name})
			return nil
		}
		procs = append(procs, c)
		return c
	}
	// the stand-in "VM" on the left, Notepad ("the other app") on the right, apart
	vm := pt{40, 40}
	vmW, vmH := W*55/100, H*70/100
	vmCenter := pt{vm.X + vmW/2, vm.Y + vmH/2}
	sc := start(*fk, "-standin", "-title", "stand-in VM", "-rect",
		itoa(vm.X)+","+itoa(vm.Y)+","+itoa(vmW)+","+itoa(vmH))
	stand := windowOf(uint32(sc.Process.Pid), "FocusKeeperStandIn")
	nc := start("notepad.exe")
	note := windowOf(uint32(nc.Process.Pid), "Notepad")
	noteX, noteY, noteW, noteH := W*62/100, int32(80), W*30/100, H*40/100
	place(note, noteX, noteY, noteW, noteH)
	noteCenter := pt{noteX + noteW/2, noteY + noteH/2}
	logPath := filepath.Join(*dir, "focuskeeper-"+*method+".jsonl")
	os.Remove(logPath)
	start(*fk, "-exe", "focuskeeper.exe", "-class", "FocusKeeperStandIn", "-log", logPath, "-method", *method, "-trace")
	time.Sleep(1500 * time.Millisecond)
	emit(map[string]any{"step": "windows", "standin": stand != 0, "notepad": note != 0})

	if *steps == "all" {
		// A-desktop
		click(noteCenter)
		time.Sleep(700 * time.Millisecond)
		check("A-desktop/notepad-in-front", note, "Notepad")
		if p, ok := desktopPoint(W, H); ok {
			click(p)
			time.Sleep(1200 * time.Millisecond)
			check("A-desktop", stand, "stand-in (trigger A)")
		} else {
			emit(map[string]any{"step": "A-desktop", "error": "no visible desktop point"})
		}
		// A-minimize
		click(noteCenter)
		time.Sleep(700 * time.Millisecond)
		check("A-minimize/notepad-in-front", note, "Notepad")
		setCursor(pt{noteCenter.X, H - 200})
		time.Sleep(1200 * time.Millisecond) // the mouse rests (not over the stand-in): trigger B stays out of it
		procShowWindow.Call(note, 6)        // SW_MINIMIZE
		time.Sleep(1200 * time.Millisecond)
		check("A-minimize", stand, "stand-in")
		procShowWindow.Call(note, 4) // SW_SHOWNOACTIVATE
		time.Sleep(500 * time.Millisecond)
	}

	// B-move-rest
	click(noteCenter)
	time.Sleep(1200 * time.Millisecond)
	check("B-move-rest/notepad-in-front", note, "Notepad")
	glide(noteCenter, vmCenter)
	time.Sleep(900 * time.Millisecond)
	check("B-move-rest", stand, "stand-in (trigger B)")
	if *steps != "all" {
		return
	}

	// B-still-typing: Notepad moved over the stand-in's middle, clicked there, the mouse left alone; Notepad moves
	// back - the mouse now rests over the stand-in without having moved - and is typed into
	place(note, vmCenter.X-noteW/2, vmCenter.Y-noteH/2, noteW, noteH)
	time.Sleep(300 * time.Millisecond)
	click(vmCenter)
	time.Sleep(1500 * time.Millisecond)
	place(note, noteX, noteY, noteW, noteH)
	time.Sleep(300 * time.Millisecond)
	typeText("typing while the mouse is parked over the VM")
	time.Sleep(800 * time.Millisecond)
	_, root := rootAt(vmCenter)
	emit(map[string]any{"step": "B-still-typing/under-mouse", "root": describe(root)})
	check("B-still-typing", note, "Notepad (no action)")

	// B-fullscreen
	fc := start(*fk, "-standin", "-class", "FocusKeeperFull", "-title", "full-screen stand-in", "-full")
	full := windowOf(uint32(fc.Process.Pid), "FocusKeeperFull")
	time.Sleep(500 * time.Millisecond)
	click(pt{W / 2, H / 2})
	time.Sleep(1200 * time.Millisecond)
	check("B-fullscreen/full-in-front", full, "full-screen window")
	glide(pt{W / 2, H / 2}, vmCenter)
	time.Sleep(900 * time.Millisecond)
	check("B-fullscreen", full, "full-screen window (no action)")

	// A-fullscreen-minimized
	procShowWindow.Call(full, 6)
	time.Sleep(1200 * time.Millisecond)
	check("A-fullscreen-minimized", stand, "stand-in")

	time.Sleep(300 * time.Millisecond)
	if f, err := os.Open(logPath); err == nil {
		s := bufio.NewScanner(f)
		for s.Scan() {
			out.Write(append([]byte("FK "), append(s.Bytes(), '\n')...))
		}
		f.Close()
	}
}

func itoa(v int32) string {
	b, _ := json.Marshal(v)
	return string(b)
}
