package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A scripted desktop: the "VM" window on the left (0..999), the other app on the right (1000..1999), the screen
// 2000 wide. A full-screen window, when in front, covers everything.
const (
	vmWin    = uintptr(100)
	otherWin = uintptr(200)
	shellWin = uintptr(300)
	fullWin  = uintptr(400)
)

type world struct {
	t         *testing.T
	clock     time.Time
	fg        uintptr
	cur       point
	button    bool
	key       bool
	fullUp    bool    // the full-screen window is shown (covers everything)
	over      uintptr // soft mode: the window lying over the VM (0 - none)
	activated int
	k         *keeper
	logPath   string
}

func newWorld(t *testing.T) *world {
	w := &world{t: t, clock: time.Date(2026, 10, 26, 12, 0, 0, 0, time.Local), fg: otherWin, cur: point{1500, 500}}
	w.logPath = filepath.Join(t.TempDir(), "fk.jsonl")
	w.k = &keeper{mode: modeHover, exe: "vmware.exe", method: "auto", trace: true, log: &jsonLog{path: w.logPath}, sys: env{
		now:        func() time.Time { return w.clock },
		foreground: func() uintptr { return w.fg },
		cursor:     func() point { return w.cur },
		rootAt: func(p point) uintptr {
			switch {
			case w.fullUp:
				return fullWin
			case p.X < 1000:
				return vmWin
			}
			return otherWin
		},
		findTarget:      func(string, string) uintptr { return vmWin },
		isShell:         func(h uintptr) bool { return h == 0 || h == shellWin },
		mouseButtonDown: func() bool { return w.button },
		anyKeyDown:      func() bool { return w.key },
		fullScreen:      func(h uintptr) bool { return h == fullWin },
		coveredBy:       func(uintptr) uintptr { return w.over },
		activate: func(target uintptr, _ string) string {
			w.activated++
			w.fg = target
			return "input"
		},
		describe: func(h uintptr) map[string]any { return map[string]any{"h": h} },
	}}
	return w
}

// run advances the clock in sample steps, calling the sampler (trigger B) and the safety check (trigger A).
func (w *world) run(d time.Duration) {
	for end := w.clock.Add(d); w.clock.Before(end); {
		w.clock = w.clock.Add(samplePeriod)
		w.k.tick()
		w.k.checkShell()
		w.k.checkUncovered()
	}
}

// glide moves the cursor to p over ~300 ms.
func (w *world) glide(p point) {
	from := w.cur
	for i := int32(1); i <= 6; i++ {
		w.cur = point{from.X + (p.X-from.X)*i/6, from.Y + (p.Y-from.Y)*i/6}
		w.run(samplePeriod)
	}
}

func (w *world) rows() []map[string]any {
	f, err := os.Open(w.logPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []map[string]any
	s := bufio.NewScanner(f)
	for s.Scan() {
		var r map[string]any
		json.Unmarshal(s.Bytes(), &r)
		out = append(out, r)
	}
	return out
}

func (w *world) find(ev, trigger string) map[string]any {
	for _, r := range w.rows() {
		if r["ev"] == ev && r["trigger"] == trigger {
			return r
		}
	}
	return nil
}

func TestTriggerAShellInFront(t *testing.T) {
	w := newWorld(t)
	w.cur = point{1500, 1590} // resting over the other app, not the VM
	w.run(time.Second)
	w.fg = shellWin // the other app minimized: the desktop is left in front
	w.k.checkShell()
	w.run(200 * time.Millisecond)
	if w.activated != 0 {
		t.Fatalf("acted before the shell settled")
	}
	w.run(100 * time.Millisecond)
	r := w.find("focus", "A")
	if w.activated != 1 || r == nil || r["result"] != "ok" || r["how"] != "input" {
		t.Fatalf("trigger A: activated %d, log %v", w.activated, w.rows())
	}
}

func TestTriggerANoForegroundAtAll(t *testing.T) {
	w := newWorld(t)
	w.fg = 0
	w.run(400 * time.Millisecond)
	if w.activated != 1 {
		t.Fatalf("no foreground window: activated %d", w.activated)
	}
}

func TestTriggerATaskbarClickPassesThrough(t *testing.T) {
	w := newWorld(t)
	w.fg, w.button = shellWin, true // pressed on a taskbar button
	w.run(600 * time.Millisecond)
	w.button = false
	w.fg = otherWin // the button's app comes up
	w.run(time.Second)
	if w.activated != 0 {
		t.Fatalf("a taskbar click was undone: %v", w.rows())
	}
}

func TestTriggerBMoveAndRest(t *testing.T) {
	w := newWorld(t)
	w.run(time.Second) // the other app in front, the mouse on it
	w.glide(point{500, 500})
	if w.activated != 0 {
		t.Fatalf("acted while the mouse moved")
	}
	w.run(300 * time.Millisecond)
	r := w.find("focus", "B")
	if w.activated != 1 || r == nil || r["result"] != "ok" {
		t.Fatalf("trigger B: activated %d, log %v", w.activated, w.rows())
	}
	w.run(2 * time.Second) // the same rest is not acted on again
	if w.activated != 1 {
		t.Fatalf("acted twice on one rest")
	}
}

func TestTriggerBMouseParkedWhileTyping(t *testing.T) {
	w := newWorld(t)
	w.fg = vmWin
	w.cur = point{500, 500} // the mouse parked over the VM, the VM in front
	w.run(2 * time.Second)
	w.fg = otherWin // Alt+Tab to the other app, then typing there
	for i := 0; i < 20; i++ {
		w.key = i%2 == 0
		w.run(100 * time.Millisecond)
	}
	if w.activated != 0 {
		t.Fatalf("took the focus from the app being typed into: %v", w.rows())
	}
	if r := w.find("skip", "B"); r == nil || r["why"] != "mouse-still" {
		t.Fatalf("want a skip mouse-still, log %v", w.rows())
	}
}

func TestTriggerBAltTabRightAfterAMove(t *testing.T) {
	w := newWorld(t)
	w.fg = vmWin
	w.glide(point{500, 500}) // moved over the VM while the VM was in front
	w.fg = otherWin          // and Alt+Tab away at once
	w.run(2 * time.Second)
	if w.activated != 0 {
		t.Fatalf("an Alt+Tab was undone: %v", w.rows())
	}
}

func TestTriggerBKeyOrButtonHeld(t *testing.T) {
	for _, held := range []string{"key-down", "button-down"} {
		w := newWorld(t)
		w.run(time.Second)
		w.key, w.button = held == "key-down", held == "button-down"
		w.glide(point{500, 500})
		w.run(400 * time.Millisecond)
		if w.activated != 0 {
			t.Fatalf("%s: acted", held)
		}
		if r := w.find("skip", "B"); r == nil || r["why"] != held {
			t.Fatalf("%s: log %v", held, w.rows())
		}
	}
}

func TestTriggerBFullScreenInFront(t *testing.T) {
	w := newWorld(t)
	w.fg, w.fullUp = fullWin, true // a full-screen RDP client: the VM is not visible under the mouse
	w.run(time.Second)
	w.glide(point{500, 500})
	w.run(time.Second)
	if w.activated != 0 {
		t.Fatalf("acted over a full-screen window")
	}
	// a full-screen window in front that somehow leaves the VM visible under the mouse: still no action
	w = newWorld(t)
	w.fg = fullWin
	w.run(time.Second)
	w.glide(point{500, 500})
	w.run(400 * time.Millisecond)
	if r := w.find("skip", "B"); w.activated != 0 || r == nil || r["why"] != "fullscreen" {
		t.Fatalf("full-screen in front: activated %d, log %v", w.activated, w.rows())
	}
}

func TestFullScreenMinimizedThenA(t *testing.T) {
	w := newWorld(t)
	w.fg, w.fullUp = fullWin, true
	w.run(time.Second)
	w.fg, w.fullUp = shellWin, false // the RDP client minimized: Windows leaves the taskbar in front
	w.k.checkShell()
	w.run(400 * time.Millisecond)
	if r := w.find("focus", "A"); w.activated != 1 || r == nil {
		t.Fatalf("after the full-screen window minimized: activated %d, log %v", w.activated, w.rows())
	}
}

func TestDryAndPause(t *testing.T) {
	w := newWorld(t)
	w.k.dry = true
	w.fg = shellWin
	w.run(400 * time.Millisecond)
	if r := w.find("focus", "A"); w.activated != 0 || r == nil || r["result"] != "dry" {
		t.Fatalf("dry: activated %d, log %v", w.activated, w.rows())
	}
	w = newWorld(t)
	w.k.paused = true
	w.fg = shellWin
	w.run(time.Second)
	w.fg = otherWin
	w.run(time.Second)
	w.glide(point{500, 500})
	w.run(time.Second)
	if w.activated != 0 || w.find("focus", "A") != nil || w.find("focus", "B") != nil {
		t.Fatalf("paused but acted: %v", w.rows())
	}
}

// --- the soft mode -------------------------------------------------------------------------------------------------

func softWorld(t *testing.T) *world {
	w := newWorld(t)
	w.k.mode = modeSoft
	w.over = otherWin // Telegram open over the VM, in front
	return w
}

func TestSoftWindowOverTheVMKeepsTheFocus(t *testing.T) {
	w := softWorld(t)
	w.run(time.Second)
	w.glide(point{500, 500}) // a casual move over the VM area: never a reason in this mode
	w.run(2 * time.Second)
	if w.activated != 0 {
		t.Fatalf("took the focus while a window lies over the VM: %v", w.rows())
	}
}

func TestSoftMinimizedOrClosedGivesTheFocusBack(t *testing.T) {
	for _, after := range []uintptr{shellWin, 0, otherWin} { // Windows leaves the taskbar, nothing, or the minimized app in front
		w := softWorld(t)
		w.run(time.Second)
		w.over, w.fg = 0, after
		w.run(samplePeriod)
		r := w.find("focus", "uncovered")
		if w.activated != 1 || r == nil || r["mode"] != modeSoft || r["result"] != "ok" {
			t.Fatalf("fg %d after minimize: activated %d, log %v", after, w.activated, w.rows())
		}
		w.run(2 * time.Second)
		if w.activated != 1 {
			t.Fatalf("acted twice")
		}
	}
}

func TestSoftWaitsForTheButton(t *testing.T) {
	w := softWorld(t)
	w.run(time.Second)
	w.over, w.fg, w.button = 0, shellWin, true // pressing a taskbar button
	w.run(time.Second)
	if w.activated != 0 {
		t.Fatalf("acted with the button down")
	}
	w.button = false
	w.over, w.fg = otherWin, otherWin // the button's app comes up over the VM
	w.run(time.Second)
	if w.activated != 0 {
		t.Fatalf("a taskbar click was undone")
	}
}

func TestSoftShellInFrontButCoveredIsLoggedOnce(t *testing.T) {
	w := softWorld(t)
	w.fg = shellWin // the taskbar in front, yet a window still lies over the VM
	w.run(3 * time.Second)
	n := 0
	for _, r := range w.rows() {
		if r["ev"] == "skip" && r["why"] == "covered" && r["mode"] == modeSoft {
			n++
		}
	}
	if w.activated != 0 || n != 1 {
		t.Fatalf("activated %d, covered lines %d: %v", w.activated, n, w.rows())
	}
}

func TestHoverActionsSayTheMode(t *testing.T) {
	w := newWorld(t)
	w.fg = shellWin
	w.run(400 * time.Millisecond)
	if r := w.find("focus", "A"); r == nil || r["mode"] != modeHover {
		t.Fatalf("log %v", w.rows())
	}
}

func TestSettingsRemembered(t *testing.T) {
	settingsPath = filepath.Join(t.TempDir(), "focuskeeper.settings.json")
	st := loadSettings()
	if st.Mode != modeSoft || st.Target != "vmware" || st.Lang != "" {
		t.Fatalf("defaults: %+v", st)
	}
	if exe, class := targetOf(st); exe != "vmware.exe" || class != "" {
		t.Fatalf("default target %s %s", exe, class)
	}
	saveSettings(settings{Mode: modeHover, Lang: "ru", Target: "virtualbox"})
	st = loadSettings()
	if exe, _ := targetOf(st); st.Mode != modeHover || langOf(st) != "ru" || exe != "VirtualBoxVM.exe" {
		t.Fatalf("not remembered: %+v", st)
	}
	saveSettings(settings{Target: "hyperv", Exe: "qemu-system-x86_64.exe", Class: "SDL_app"})
	if exe, class := targetOf(loadSettings()); exe != "qemu-system-x86_64.exe" || class != "SDL_app" {
		t.Fatalf("a custom exe should win: %s %s", exe, class)
	}
	os.WriteFile(settingsPath, []byte(`{"mode":"weird","target":"nope","lang":"xx"}`), 0o644)
	if st := loadSettings(); st.Mode != modeSoft || st.Target != "vmware" || st.Lang != "" {
		t.Fatalf("bad values not reset: %+v", st)
	}
}

func TestTextsComplete(t *testing.T) {
	for key, row := range texts {
		for _, l := range langs {
			if row[l] == "" {
				t.Errorf("%s: no %s text", key, l)
			}
		}
	}
	defer func(was string) { lang = was }(lang)
	lang = "ru"
	if tr("exit") != texts["exit"]["ru"] || tr("no-such-key") != "" {
		t.Fatalf("tr")
	}
	lang = "de" // an unknown language falls back to English
	if tr("exit") != "Exit" {
		t.Fatalf("fallback")
	}
}

func TestIconDraws(t *testing.T) {
	px := drawIcon(16, colorOn, true, false)
	if len(px) != 16*16*4 || px[3] != 0 || px[(8*16+8)*4+3] != 255 || px[(8*16+8)*4] != 255 {
		t.Fatalf("corner should be clear and the centre dot white and opaque")
	}
	// away: no dot (the centre is the plain blue), the ring fainter but still lighter than the blue
	away := drawIcon(16, colorOn, true, true)
	if c := (8*16 + 8) * 4; away[c] != byte(colorOn&0xff) || away[c+2] != byte(colorOn>>16) {
		t.Fatalf("away: the centre should be the plain colour, got %v", away[c:c+4])
	}
	ringAt := (8*16 + 8 + 4) * 4 // 0.27*16 = 4.3 px right of the centre
	if !(px[ringAt+2] > away[ringAt+2] && away[ringAt+2] > byte(colorOn>>16)) {
		t.Fatalf("away ring should be fainter than the focused one, yet visible: %d %d", px[ringAt+2], away[ringAt+2])
	}
}

func TestVMFocused(t *testing.T) {
	w := newWorld(t)
	w.fg = vmWin
	if !w.k.vmFocused() {
		t.Fatalf("the VM window in front: focused")
	}
	w.fg = otherWin
	if w.k.vmFocused() {
		t.Fatalf("another window in front: away")
	}
}
