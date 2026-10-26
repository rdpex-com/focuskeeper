// focuskeeper: gives the keyboard and mouse focus back to a virtual machine's window (VMware Workstation by default;
// VirtualBox and Hyper-V VMConnect by a setting). With the VM shown as a normal window, when another window (a
// messenger, Notepad, a full-screen RDP client) is minimized or closed, Windows often leaves the focus on the desktop
// or the taskbar: the VM looks active, but the first click only activates the VM window and never reaches the guest.
//
//	go build -trimpath -ldflags "-s -w -H windowsgui -buildid=" -o focuskeeper.exe .
//	focuskeeper.exe [-dry] [-mode soft|hover] [-target vmware|virtualbox|hyperv] [-exe <x.exe> [-class <c>]] [-log <file>]
//
// Two modes, chosen in the tray menu (as are the VM program and the language) and remembered in
// focuskeeper.settings.json next to the exe:
//
// Soft (default): never on mouse movement. The VM window gets the focus back when no visible window lies over it in
// the z-order - the window that covered it was minimized, closed or moved away - decided 200 ms after the last window
// event (foreground, minimize, show/hide, destroy, move; SetWinEventHook) and by a 1 s safety check. A window left
// open over the VM keeps the focus.
//
// Hover: triggers A and B.
// Trigger A: the foreground becomes the shell (desktop, taskbar) or nothing and the VM window is visible and not
// minimized - it is brought to the front once the shell has stayed in front for 250 ms, so a click on a taskbar
// button passes through.
// Trigger B: the mouse moved and now rests 250 ms on a visible part of the VM window while another app has the focus
// - the VM window is focused so the click that follows reaches the guest. Never with the mouse still, a mouse button
// or a key held, or a full-screen window in front.
//
// Each action is one JSON line in focuskeeper.jsonl next to the exe. -standin opens a test window instead (the
// checks: a stand-in for the VM window).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	eventSystemForeground    = 0x0003
	eventSystemMinimizeStart = 0x0016
	eventSystemMinimizeEnd   = 0x0017
	eventObjectDestroy       = 0x8001
	eventObjectLocation      = 0x800B // destroy, show, hide, reorder, focus, ... location change
	winEventOutOfContext     = 0x0000
	timerSample              = 1
	timerSettle              = 2
	timerSafety              = 3
	timerSoft                = 4
	wmTray                   = wmApp + 1
	cmdPause                 = 10
	cmdLog                   = 11
	cmdDry                   = 12
	cmdExit                  = 13
	cmdModeSoft              = 14
	cmdModeHover             = 15
	cmdAutostart             = 16
	cmdTarget                = 20 // + index in targets
	cmdLang                  = 40 // + index in langs
)

var (
	k             *keeper
	trayWnd       uintptr
	taskbarMsg    uintptr
	icons         = map[string]uintptr{}
	winEventProcA = windows.NewCallback(func(hook, event, hwnd uintptr, idObject, idChild int32, thread, ms uint32) uintptr {
		if idObject != 0 || idChild != 0 { // only windows themselves (OBJID_WINDOW, CHILDID_SELF)
			return 0
		}
		if root, _, _ := procGetAncestor.Call(hwnd, gaRoot); root != hwnd && hwnd != 0 {
			return 0 // a child window moved inside some app
		}
		if k.currentMode() == modeSoft {
			procSetTimer.Call(trayWnd, timerSoft, uintptr(softSettle/time.Millisecond), 0) // each event restarts it
		} else if event < eventObjectDestroy {
			k.checkShell()
			procSetTimer.Call(trayWnd, timerSettle, uintptr(shellSettle/time.Millisecond+20), 0)
		}
		return 0
	})
)

func main() {
	dry := flag.Bool("dry", false, "only log what would be done")
	modeArg := flag.String("mode", "", "soft or hover for this run (default: the one chosen in the tray, else soft)")
	target := flag.String("target", "", "vmware, virtualbox or hyperv for this run (default: the one chosen in the tray)")
	exe := flag.String("exe", "", "any other program whose main window gets the focus back (wins over -target)")
	class := flag.String("class", "", "window class of that window (any if empty; with -standin: the stand-in's class)")
	logPath := flag.String("log", "", "log file (default: focuskeeper.jsonl next to the exe)")
	method := flag.String("method", "auto", "how to take the foreground: auto, input, attach, plain (checks only)")
	trace := flag.Bool("trace", false, "also log the quiet decisions (for the checks)")
	standin := flag.Bool("standin", false, "open a test window instead (stand-in for the VMware window)")
	title := flag.String("title", "focuskeeper stand-in", "-standin: window title")
	rectArg := flag.String("rect", "100,100,900,700", "-standin: x,y,width,height")
	full := flag.Bool("full", false, "-standin: a caption-less window over the whole monitor (like a full-screen RDP client)")
	flag.Parse()
	procSetDpiAwareCtx.Call(^uintptr(3)) // per-monitor v2: physical pixels, the same as the cursor
	runtime.LockOSThread()
	if *standin {
		runStandin(*class, *title, *rectArg, *full)
		return
	}
	// one keeper per user session: start.cmd twice does not make two
	procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(utf16(`Local\focuskeeper`))))
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		return
	}
	self, _ := os.Executable()
	auto = newAutostart(self)
	if *logPath == "" {
		*logPath = filepath.Join(filepath.Dir(self), "focuskeeper.jsonl")
	}
	settingsPath = filepath.Join(filepath.Dir(*logPath), "focuskeeper.settings.json")
	st = loadSettings()
	once := st // this run's view: flags win over the file, but are not saved into it
	if *modeArg == modeSoft || *modeArg == modeHover {
		once.Mode = *modeArg
	}
	if targetByID(*target) >= 0 {
		once.Target, once.Exe, once.Class = *target, "", ""
	}
	if *exe != "" {
		once.Exe, once.Class = *exe, *class
	}
	lang = langOf(once)
	tExe, tClass := targetOf(once)
	k = &keeper{sys: win(), mode: once.Mode, exe: tExe, class: tClass, method: *method, trace: *trace, dry: *dry,
		log: &jsonLog{path: *logPath}}
	v := windows.RtlGetVersion()
	k.log.write(map[string]any{"ev": "start", "pid": os.Getpid(), "exe": tExe, "class": tClass, "dry": *dry,
		"version": version, "mode": once.Mode, "lang": lang, "method": *method,
		"windows": fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)})

	taskbarMsg, _, _ = procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	trayWnd = newWindow("FocusKeeperTray", "focuskeeper", windows.NewCallback(trayProc), 0, 0, 0, rect{})
	k.self = trayWnd
	icons["on"], icons["paused"], icons["dry"] = makeIcon(colorOn, true), makeIcon(colorPaused, true), makeIcon(colorDry, false)
	notify(0x00000000) // NIM_ADD
	procSetWinEventHook.Call(eventSystemForeground, eventSystemMinimizeEnd, 0, winEventProcA, 0, 0, winEventOutOfContext)
	procSetWinEventHook.Call(eventObjectDestroy, eventObjectLocation, 0, winEventProcA, 0, 0, winEventOutOfContext)
	procSetTimer.Call(trayWnd, timerSample, uintptr(samplePeriod/time.Millisecond), 0)
	procSetTimer.Call(trayWnd, timerSafety, 1000, 0)
	messageLoop()
	notify(0x00000002) // NIM_DELETE
	k.log.write(map[string]any{"ev": "stop"})
}

func trayProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch {
	case msg == wmTimer:
		switch wparam {
		case timerSample:
			k.tick()
		case timerSettle:
			procKillTimer.Call(hwnd, timerSettle)
			k.checkShell()
		case timerSoft:
			procKillTimer.Call(hwnd, timerSoft)
			k.checkUncovered()
		case timerSafety:
			k.checkShell()
			k.checkUncovered()
			notify(0x00000001) // NIM_MODIFY: the tooltip shows the last action's time
		}
		return 0
	case msg == wmTray && (lparam == wmRButtonUp || lparam == wmLButtonUp):
		showMenu(hwnd)
		return 0
	case msg == wmCommand:
		command(wparam & 0xffff)
		return 0
	case taskbarMsg != 0 && msg == taskbarMsg: // Explorer restarted: the icon is gone, put it back
		notify(0x00000000)
		return 0
	case msg == wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wparam, lparam)
	return r
}

// version: set by build.ps1 (-X main.version=<tag>); "dev" for a plain go build.
var version = "dev"

// st: the remembered settings; the tray changes and saves them.
var st settings

// auto: the Start with Windows value for this exe (autostart.go).
var auto autostart

func command(id uintptr) {
	switch {
	case id == cmdPause || id == cmdDry:
		k.mu.Lock()
		if id == cmdPause {
			k.paused = !k.paused
		} else {
			k.dry = !k.dry
		}
		k.mu.Unlock()
	case id == cmdModeSoft || id == cmdModeHover:
		st.Mode = modeHover
		if id == cmdModeSoft {
			st.Mode = modeSoft
		}
		k.mu.Lock()
		k.mode = st.Mode
		k.mu.Unlock()
		saveSettings(st)
	case id >= cmdTarget && int(id-cmdTarget) < len(targets):
		st.Target, st.Exe, st.Class = targets[id-cmdTarget].id, "", ""
		k.exe, k.class = targetOf(st)
		saveSettings(st)
	case id >= cmdLang && int(id-cmdLang) < len(langs):
		st.Lang = langs[id-cmdLang]
		lang = st.Lang
		saveSettings(st)
	case id == cmdAutostart:
		// on - off; off or another copy's path - this exe
		s, was, err := auto.state()
		if err == nil {
			if s == autostartOn {
				err = auto.disable()
			} else {
				err = auto.enable()
			}
		}
		now, value, _ := auto.state()
		ev := map[string]any{"ev": "autostart", "on": now == autostartOn, "value": value}
		if s == autostartElsewhere {
			ev["was"] = was
		}
		if err != nil {
			ev["err"] = err.Error()
		}
		k.log.write(ev)
		return
	case id == cmdLog:
		procShellExecute.Call(0, uintptr(unsafe.Pointer(utf16("open"))), uintptr(unsafe.Pointer(utf16("notepad.exe"))),
			uintptr(unsafe.Pointer(utf16(`"`+k.log.path+`"`))), 0, 1)
		return
	case id == cmdExit:
		procPostQuitMessage.Call(0)
		return
	default:
		return
	}
	paused, dry := k.settings()
	k.log.write(map[string]any{"ev": "settings", "paused": paused, "dry": dry, "mode": k.currentMode(),
		"exe": k.exe, "class": k.class, "lang": lang})
	notify(0x00000001)
}

func showMenu(hwnd uintptr) {
	paused, dry := k.settings()
	const mfGrayed, mfChecked, mfPopup, mfSeparator = 0x1, 0x8, 0x10, 0x800
	add := func(m uintptr, flags uint32, id uintptr, text string) {
		procAppendMenu.Call(m, uintptr(flags), id, uintptr(unsafe.Pointer(utf16(text))))
	}
	radio := func(m uintptr, first, last, on uintptr) {
		procCheckMenuRadioItem.Call(m, first, last, on, 0) // MF_BYCOMMAND
	}
	m, _, _ := procCreatePopupMenu.Call()
	add(m, mfGrayed, 0, statusText(paused, dry))
	add(m, mfSeparator, 0, "")
	add(m, 0, cmdModeSoft, tr("modeSoft"))
	add(m, 0, cmdModeHover, tr("modeHover"))
	on := uintptr(cmdModeSoft)
	if k.currentMode() == modeHover {
		on = cmdModeHover
	}
	radio(m, cmdModeSoft, cmdModeHover, on)
	add(m, mfSeparator, 0, "")

	sub, _, _ := procCreatePopupMenu.Call()
	for i, t := range targets {
		add(sub, 0, cmdTarget+uintptr(i), t.name)
	}
	if st.Exe != "" {
		add(sub, mfSeparator, 0, "")
		add(sub, mfGrayed|mfChecked, 0, tr("custom")+st.Exe)
	} else {
		radio(sub, cmdTarget, cmdTarget+uintptr(len(targets)-1), cmdTarget+uintptr(targetByID(st.Target)))
	}
	add(m, mfPopup, sub, tr("target"))
	langMenu, _, _ := procCreatePopupMenu.Call()
	for i, l := range langs {
		add(langMenu, 0, cmdLang+uintptr(i), tr("lang."+l))
		if l == lang {
			radio(langMenu, cmdLang, cmdLang+uintptr(len(langs)-1), cmdLang+uintptr(i))
		}
	}
	add(m, mfPopup, langMenu, tr("language"))
	switch s, _, _ := auto.state(); s {
	case autostartOn:
		add(m, mfChecked, cmdAutostart, tr("autostart"))
	case autostartElsewhere: // unchecked: a click points it at this exe
		add(m, 0, cmdAutostart, tr("autostart")+tr("autostartElsewhere"))
	default:
		add(m, 0, cmdAutostart, tr("autostart"))
	}
	add(m, mfSeparator, 0, "")
	pause := tr("pause")
	if paused {
		pause = tr("resume")
	}
	add(m, 0, cmdPause, pause)
	add(m, 0, cmdLog, tr("openLog"))
	dryFlag := uint32(0)
	if dry {
		dryFlag = mfChecked
	}
	add(m, dryFlag, cmdDry, tr("dry"))
	add(m, mfSeparator, 0, "")
	add(m, 0, cmdExit, tr("exit"))
	p := cursor()
	k.menu = true
	procSetForegroundWindow.Call(hwnd) // or the menu does not close on a click elsewhere
	const tpmRightButton, tpmReturnCmd, tpmNoNotify = 0x2, 0x100, 0x80
	id, _, _ := procTrackPopupMenu.Call(m, tpmRightButton|tpmReturnCmd|tpmNoNotify, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
	procPostMessage.Call(hwnd, wmNull, 0, 0)
	procDestroyMenu.Call(m) // destroys the submenus too
	k.menu = false
	if id != 0 {
		command(id)
	}
}

func statusText(paused, dry bool) string {
	s := tr("on")
	if paused {
		s = tr("paused")
	}
	if k.currentMode() == modeHover {
		s += " · " + tr("modeHover")
	} else {
		s += " · " + tr("modeSoft")
	}
	if dry {
		s += tr("drySuffix")
	}
	return s
}

// notify adds, changes or removes the tray icon (Shell_NotifyIconW with NOTIFYICONDATAW).
func notify(action uintptr) {
	var nid struct {
		Size             uint32
		Wnd              uintptr
		ID, Flags, CbMsg uint32
		Icon             uintptr
		Tip              [128]uint16
		State, StateMask uint32
		Info             [256]uint16
		Version          uint32
		InfoTitle        [64]uint16
		InfoFlags        uint32
		Guid             [16]byte
		BalloonIcon      uintptr
	}
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Wnd, nid.ID = trayWnd, 1
	nid.Flags = 0x1 | 0x2 | 0x4 // NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.CbMsg = wmTray
	paused, dry := k.settings()
	switch {
	case paused:
		nid.Icon = icons["paused"]
	case dry:
		nid.Icon = icons["dry"]
	default:
		nid.Icon = icons["on"]
	}
	k.mu.Lock()
	last := k.last
	k.mu.Unlock()
	tip := statusText(paused, dry) + "\n" + tr("lastNone")
	if !last.IsZero() {
		tip = statusText(paused, dry) + "\n" + tr("lastAt") + last.Format("15:04:05")
	}
	t, _ := windows.UTF16FromString(tip)
	copy(nid.Tip[:len(nid.Tip)-1], t)
	procShellNotifyIcon.Call(action, uintptr(unsafe.Pointer(&nid)))
}

// runStandin: a plain window to stand in for the VMware window (or, with -full, for a full-screen RDP client).
func runStandin(class, title, rectArg string, full bool) {
	if class == "" {
		class = "FocusKeeperStandIn"
	}
	var x, y, w, h int32
	fmt.Sscanf(strings.ReplaceAll(rectArg, ",", " "), "%d %d %d %d", &x, &y, &w, &h)
	r := rect{x, y, x + w, y + h}
	const wsOverlappedWindow, wsPopup, wsVisible = 0x00CF0000, 0x80000000, 0x10000000
	st := uint32(wsOverlappedWindow | wsVisible)
	brush, _, _ := procCreateSolidBrush.Call(0x5a3a1a) // BGR: a dark blue, easy to tell apart on a screenshot
	if full {
		sw, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
		sh, _, _ := procGetSystemMetrics.Call(1)
		r, st = rect{0, 0, int32(sw), int32(sh)}, wsPopup|wsVisible
		brush, _, _ = procCreateSolidBrush.Call(0x303030)
	}
	newWindow(class, title, windows.NewCallback(standinProc), brush, 0, st, r)
	messageLoop()
}

func standinProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	if msg == wmDestroy {
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wparam, lparam)
	return r
}
