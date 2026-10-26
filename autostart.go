package main

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Start with Windows: a value in the user's Run key, no installer and no admin rights. The registry is the truth -
// the tray reads it each time the menu opens, so a value removed elsewhere (Task Manager - Startup, regedit) shows
// as off, and a value left by a copy of the exe in another folder shows as pointing elsewhere.

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "focuskeeper"
)

const (
	autostartOff       = iota // no value
	autostartOn               // the value starts this exe
	autostartElsewhere        // the value starts some other path (the exe was moved or copied)
)

// autostart: the Run value for one exe; key is the path under HKCU (the tests use their own).
type autostart struct {
	key, name, exe string
}

func newAutostart(exe string) autostart {
	return autostart{key: runKeyPath, name: runValueName, exe: exe}
}

// state: off, on or elsewhere, and the command line found in the value.
func (a autostart) state() (int, string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, a.key, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return autostartOff, "", nil
	}
	if err != nil {
		return autostartOff, "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(a.name)
	if errors.Is(err, registry.ErrNotExist) {
		return autostartOff, "", nil
	}
	if err != nil {
		return autostartOff, "", err
	}
	if samePath(runCommandExe(v), a.exe) {
		return autostartOn, v, nil
	}
	return autostartElsewhere, v, nil
}

// enable writes "<exe>" (quoted: the folder may have spaces); flags are not added - the tray choices live in
// focuskeeper.settings.json.
func (a autostart) enable() error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, a.key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(a.name, `"`+a.exe+`"`)
}

func (a autostart) disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, a.key, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(a.name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// runCommandExe: the program part of a Run command line - quoted, or up to the first space.
func runCommandExe(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if end := strings.Index(cmd[1:], `"`); end >= 0 {
			return cmd[1 : 1+end]
		}
		return cmd[1:]
	}
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

func samePath(a, b string) bool {
	return a != "" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
