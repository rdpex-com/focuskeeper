package main

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The registry wrapper on a throwaway key under HKCU\Software, never the real Run key.
func testAutostart(t *testing.T, exe string) autostart {
	key := fmt.Sprintf(`Software\focuskeeper-test-%d`, os.Getpid())
	t.Cleanup(func() { registry.DeleteKey(registry.CURRENT_USER, key) })
	return autostart{key: key, name: runValueName, exe: exe}
}

func TestAutostartOnOff(t *testing.T) {
	a := testAutostart(t, `C:\Program Tools\focuskeeper\focuskeeper.exe`)
	if s, _, err := a.state(); err != nil || s != autostartOff {
		t.Fatalf("no key: state %d, %v; want off", s, err)
	}
	if err := a.disable(); err != nil {
		t.Fatalf("disable with no key: %v", err)
	}
	if err := a.enable(); err != nil {
		t.Fatal(err)
	}
	s, v, err := a.state()
	if err != nil || s != autostartOn || v != `"C:\Program Tools\focuskeeper\focuskeeper.exe"` {
		t.Fatalf("after enable: state %d, %q, %v", s, v, err)
	}
	if err := a.disable(); err != nil {
		t.Fatal(err)
	}
	if s, _, err := a.state(); err != nil || s != autostartOff {
		t.Fatalf("after disable: state %d, %v; want off", s, err)
	}
	if err := a.disable(); err != nil {
		t.Fatalf("disable twice: %v", err)
	}
}

func TestAutostartMovedExe(t *testing.T) {
	old := testAutostart(t, `C:\Old\focuskeeper.exe`)
	if err := old.enable(); err != nil {
		t.Fatal(err)
	}
	moved := old
	moved.exe = `D:\Tools\focuskeeper.exe`
	if s, v, _ := moved.state(); s != autostartElsewhere || v != `"C:\Old\focuskeeper.exe"` {
		t.Fatalf("moved exe: state %d, %q; want elsewhere", s, v)
	}
	if err := moved.enable(); err != nil { // re-checking fixes the path
		t.Fatal(err)
	}
	if s, _, _ := moved.state(); s != autostartOn {
		t.Fatalf("after re-enable: state %d; want on", s)
	}
	same := moved
	same.exe = `d:\tools\.\FOCUSKEEPER.EXE` // the same file, other case
	if s, _, _ := same.state(); s != autostartOn {
		t.Fatalf("case and dots: state %d; want on", s)
	}
}

func TestRunCommandExe(t *testing.T) {
	for cmd, want := range map[string]string{
		`"C:\A B\focuskeeper.exe"`:            `C:\A B\focuskeeper.exe`,
		`"C:\A B\focuskeeper.exe" -mode soft`: `C:\A B\focuskeeper.exe`,
		`C:\Tools\focuskeeper.exe -dry`:       `C:\Tools\focuskeeper.exe`,
		`  C:\Tools\focuskeeper.exe`:          `C:\Tools\focuskeeper.exe`,
		`"C:\unclosed`:                        `C:\unclosed`,
		``:                                    ``,
	} {
		if got := runCommandExe(cmd); got != want {
			t.Errorf("runCommandExe(%q) = %q, want %q", cmd, got, want)
		}
	}
	if samePath("", "") {
		t.Error("empty paths must not match")
	}
}
