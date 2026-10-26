package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"
	"unsafe"
)

// Timings of the triggers.
const (
	shellSettle  = 250 * time.Millisecond // trigger A: the shell stays in front this long (a taskbar click passes through)
	softSettle   = 200 * time.Millisecond // soft mode: no window event for this long before deciding (drags, restores)
	restFor      = 250 * time.Millisecond // trigger B: the mouse rests this long...
	movedWithin  = 800 * time.Millisecond // ...after it moved within this time before the rest
	restPx       = 3                      // still = within this many pixels
	movedPx      = 8                      // moved = farther than this from the resting point
	samplePeriod = 50 * time.Millisecond
)

type sample struct {
	at time.Time
	p  point
}

// env: what the keeper reads from and does to the system; win() is the real one, the tests put in a scripted one.
type env struct {
	now             func() time.Time
	foreground      func() uintptr
	cursor          func() point
	rootAt          func(point) uintptr // the top-level window under a point
	findTarget      func(exe, class string) uintptr
	isShell         func(uintptr) bool
	mouseButtonDown func() bool
	anyKeyDown      func() bool
	fullScreen      func(uintptr) bool
	coveredBy       func(target uintptr) uintptr // the topmost window above the target that overlaps it; 0 - none
	activate        func(target uintptr, method string) string
	describe        func(uintptr) map[string]any
}

func win() env {
	return env{now: time.Now, foreground: foreground, cursor: cursor, rootAt: rootAt, findTarget: findTarget,
		isShell: isShell, mouseButtonDown: mouseButtonDown, anyKeyDown: anyKeyDown, fullScreen: fullScreen,
		coveredBy: coveredBy,
		activate:  activate, describe: describe}
}

// Modes. Hover came first; on a real host trigger B took the focus from a messenger on a casual mouse move, hence soft.
const (
	modeSoft  = "soft"  // default: only when no window lies over the VMware window - never on mouse movement
	modeHover = "hover" // triggers A and B
)

type keeper struct {
	sys        env
	exe, class string
	mode       string // modeSoft or modeHover; changed from the tray (k.mu)
	method     string // auto (input, then attach) or one forced method: plain, input, attach
	trace      bool   // also log the quiet decisions (for the checks)
	self       uintptr

	mu     sync.Mutex
	paused bool
	dry    bool
	last   time.Time // last action (focus given or, in the dry mode, would be)
	menu   bool      // the tray menu is open: leave the foreground alone

	shellSince time.Time // trigger A: since when the shell is in front; zero - an app is
	shellDone  bool      // trigger A: this shell episode is decided
	samples    []sample
	restDone   bool      // trigger B: this rest of the mouse is decided
	lastFg     uintptr   // trigger B: the foreground and the window under the mouse at the last sample; a change of
	lastRoot   uintptr   // either decides the rest afresh
	fgSince    time.Time // trigger B: since when lastFg is in front; only a move after that counts
	softFg     uintptr   // soft mode: the foreground when the last uncovered state was decided
	softDone   bool      // soft mode: this uncovered state (with this foreground) is decided

	log *jsonLog
}

func (k *keeper) settings() (paused, dry bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.paused, k.dry
}

func (k *keeper) currentMode() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.mode
}

// checkUncovered: the soft mode. VMware gets the focus back when it is visible, not minimized, not in front, and no
// window above it in the z-order overlaps it - the window that covered it was minimized, closed or moved away. Called
// softSettle after the last window event (foreground, minimize, show/hide, destroy, move) and by the 1 s safety
// timer. Each uncovered state is acted on once per foreground window. Telegram left open over the VM keeps the focus:
// it overlaps VMware; minimizing or closing it uncovers VMware and the focus comes back at once.
func (k *keeper) checkUncovered() {
	if k.currentMode() != modeSoft || k.menu {
		return
	}
	if paused, _ := k.settings(); paused {
		return
	}
	fg := k.sys.foreground()
	if fg != k.softFg {
		k.softFg, k.softDone = fg, false
	}
	target := k.sys.findTarget(k.exe, k.class)
	if target == 0 || fg == target || (k.self != 0 && fg == k.self) {
		return
	}
	if over := k.sys.coveredBy(target); over != 0 {
		// only worth a line when the desktop or the taskbar is in front: then the user expects the VM back, and the
		// log says what still lies over it (once per foreground)
		if !k.softDone && k.sys.isShell(fg) {
			k.softDone = true
			k.log.write(map[string]any{"ev": "skip", "trigger": "uncovered", "mode": modeSoft, "why": "covered",
				"over": k.sys.describe(over), "prev": k.sys.describe(fg)})
		}
		return
	}
	if k.softDone || k.sys.mouseButtonDown() { // a press on the taskbar is on its way somewhere
		return
	}
	k.softDone = true
	k.give("uncovered", fg, target, nil)
}

// checkShell: trigger A when the shell has been in front for shellSettle. Called on a WinEvent (foreground changed,
// a window minimizes), by the settle timer the event starts, and by the 1 s safety timer - no polling of its own.
func (k *keeper) checkShell() {
	if k.currentMode() != modeHover {
		return
	}
	fg := k.sys.foreground()
	if !k.sys.isShell(fg) {
		k.shellSince, k.shellDone = time.Time{}, false
		return
	}
	if k.shellSince.IsZero() {
		k.shellSince = k.sys.now()
	}
	if k.shellDone || k.sys.now().Sub(k.shellSince) < shellSettle || k.menu {
		return
	}
	if paused, _ := k.settings(); paused {
		return
	}
	if k.sys.mouseButtonDown() { // a press on the taskbar is on its way somewhere
		return
	}
	k.shellDone = true
	target := k.sys.findTarget(k.exe, k.class)
	if target == 0 {
		if k.trace {
			k.log.write(map[string]any{"ev": "trace", "trigger": "A", "why": "no-target", "prev": k.sys.describe(fg)})
		}
		return
	}
	k.give("A", fg, target, nil)
}

// tick: every samplePeriod - remember the cursor and decide trigger B when the mouse has just come to rest.
func (k *keeper) tick() {
	now := k.sys.now()
	p := k.sys.cursor()
	k.samples = append(k.samples, sample{now, p})
	keep := now.Add(-(movedWithin + restFor + samplePeriod))
	i := 0
	for i < len(k.samples) && k.samples[i].at.Before(keep) {
		i++
	}
	k.samples = k.samples[i:]
	fg, root := k.sys.foreground(), k.sys.rootAt(p)
	if fg != k.lastFg {
		k.lastFg, k.fgSince, k.restDone = fg, now, false
	}
	if root != k.lastRoot {
		k.lastRoot, k.restDone = root, false
	}

	restStart := now.Add(-restFor)
	if len(k.samples) == 0 || k.samples[0].at.After(restStart) {
		return // not enough history yet
	}
	for _, s := range k.samples {
		if !s.at.Before(restStart) && far(s.p, p, restPx) {
			k.restDone = false // still moving: a new rest will be decided afresh
			return
		}
	}
	if k.restDone || k.menu || k.currentMode() != modeHover {
		return
	}
	if paused, _ := k.settings(); paused {
		return
	}
	k.restDone = true
	target := k.sys.findTarget(k.exe, k.class)
	quiet := func(why string) {
		if k.trace {
			k.log.write(map[string]any{"ev": "trace", "trigger": "B", "why": why, "x": p.X, "y": p.Y, "prev": k.sys.describe(fg)})
		}
	}
	switch {
	case target == 0:
		quiet("no-target")
		return
	case fg == target:
		quiet("has-focus")
		return
	case k.self != 0 && fg == k.self:
		quiet("own-window")
		return
	case root != target:
		quiet("not-over-target")
		return
	}
	// the mouse rests on the target's visible part while another app has the focus: decide and say why not. Only a
	// move made while this app was in front counts: Alt+Tab away right after a move over the VM is not undone.
	moved := false
	for _, s := range k.samples {
		if s.at.Before(restStart) && !s.at.Before(k.fgSince) && far(s.p, p, movedPx) {
			moved = true
			break
		}
	}
	why := ""
	switch {
	case !moved:
		why = "mouse-still" // parked over the VM while the user types elsewhere
	case k.sys.mouseButtonDown():
		why = "button-down"
	case k.sys.anyKeyDown():
		why = "key-down"
	case !k.sys.isShell(fg) && k.sys.fullScreen(fg):
		why = "fullscreen"
	}
	if why != "" {
		k.log.write(map[string]any{"ev": "skip", "trigger": "B", "mode": modeHover, "why": why, "x": p.X, "y": p.Y, "prev": k.sys.describe(fg)})
		return
	}
	k.give("B", fg, target, &p)
}

func far(a, b point, px int32) bool {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx > px || dx < -px || dy > px || dy < -px
}

// give brings the target to the foreground (or, in the dry mode, only says it would) and logs the action.
func (k *keeper) give(trigger string, prev, target uintptr, p *point) {
	row := map[string]any{"ev": "focus", "trigger": trigger, "mode": k.currentMode(), "prev": k.sys.describe(prev),
		"target": k.sys.describe(target)}
	if p != nil {
		row["x"], row["y"] = p.X, p.Y
	}
	_, dry := k.settings()
	if dry {
		row["result"] = "dry"
	} else if how := k.sys.activate(target, k.method); how != "" {
		row["result"], row["how"] = "ok", how
	} else {
		row["result"] = "refused"
		row["now"] = k.sys.describe(k.sys.foreground())
	}
	k.mu.Lock()
	k.last = k.sys.now()
	k.mu.Unlock()
	k.log.write(row)
}

func describe(h uintptr) map[string]any {
	if h == 0 {
		return map[string]any{"cls": ""}
	}
	return map[string]any{"cls": className(h), "exe": exeOf(h)}
}

// activate: Windows lets a background program take the foreground only on conditions (SetForegroundWindow docs);
// one of them is "the process received the last input event". Ways, in the order of auto:
//   - input: send one empty mouse input (no move, no button) from this process, then SetForegroundWindow - the
//     system counts this process as the last one with input; nothing reaches any window, no key is pressed;
//   - attach: share the input state with the foreground window's thread (AttachThreadInput), SetForegroundWindow,
//     detach.
//
// plain (SetForegroundWindow alone) is only for the comparison in the checks: when refused, Windows flashes the
// target's taskbar button instead. An Alt key tap is not used: VMware would pass the Alt to the guest.
// Returns the way that worked, "" if none did.
func activate(target uintptr, method string) string {
	ways := []string{"input", "attach"}
	if method != "auto" {
		ways = []string{method}
	}
	for _, way := range ways {
		switch way {
		case "plain":
			procSetForegroundWindow.Call(target)
		case "input":
			emptyMouseInput()
			procSetForegroundWindow.Call(target)
		case "attach":
			fgThread, _ := threadOf(foreground())
			me, _, _ := procGetCurrentThreadId.Call()
			attached := false
			if fgThread != 0 && uintptr(fgThread) != me {
				r, _, _ := procAttachThreadInput.Call(me, uintptr(fgThread), 1)
				attached = r != 0
			}
			procBringWindowToTop.Call(target)
			procSetForegroundWindow.Call(target)
			if attached {
				procAttachThreadInput.Call(me, uintptr(fgThread), 0)
			}
		}
		for i := 0; i < 5; i++ {
			if foreground() == target {
				return way
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return ""
}

func emptyMouseInput() {
	var in struct { // INPUT with a MOUSEINPUT: type 0, all fields zero
		typ uint32
		_   uint32
		mi  [4]uint64
	}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

// jsonLog: one JSON line per action next to the exe; past 5 MB the file moves to .1 (one old file kept).
type jsonLog struct {
	mu   sync.Mutex
	path string
}

func (l *jsonLog) write(row map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	row["ts"] = time.Now().Format("2006-01-02T15:04:05.000")
	if st, err := os.Stat(l.path); err == nil && st.Size() > 5<<20 {
		os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(row)
	f.Write(append(b, '\n'))
}
