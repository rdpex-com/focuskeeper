package main

import (
	"encoding/json"
	"os"
)

// targets: the VM programs focuskeeper knows; the window is the biggest titled one of that exe (and class, if set).
var targets = []struct{ id, name, exe, class string }{
	{"vmware", "VMware Workstation", "vmware.exe", ""},
	{"virtualbox", "VirtualBox", "VirtualBoxVM.exe", ""},
	{"hyperv", "Hyper-V (VMConnect)", "vmconnect.exe", ""},
}

// settings: the tray choices kept between runs in focuskeeper.settings.json next to the exe. Exe/Class, when set
// (by hand in the file), point at any other program and win over Target. Pause and the dry mode are for one run.
type settings struct {
	Mode   string `json:"mode"`            // soft or hover
	Lang   string `json:"lang"`            // en or ru; empty - the Windows UI language
	Target string `json:"target"`          // an id from targets
	Exe    string `json:"exe,omitempty"`   // a custom target program
	Class  string `json:"class,omitempty"` // and, optionally, its window class
}

var settingsPath string

func loadSettings() settings {
	var st settings
	if b, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(b, &st)
	}
	if st.Mode != modeHover {
		st.Mode = modeSoft
	}
	if st.Lang != "en" && st.Lang != "ru" {
		st.Lang = "" // not chosen: follow the Windows UI language (langOf), and keep following it
	}
	if targetByID(st.Target) < 0 {
		st.Target = targets[0].id
	}
	return st
}

func saveSettings(st settings) {
	b, _ := json.MarshalIndent(st, "", "  ")
	os.WriteFile(settingsPath, b, 0o644)
}

// langOf: the language to show - the chosen one, else the Windows UI language.
func langOf(st settings) string {
	if st.Lang != "" {
		return st.Lang
	}
	return systemLang()
}

func targetByID(id string) int {
	for i, t := range targets {
		if t.id == id {
			return i
		}
	}
	return -1
}

// targetOf: the exe and window class to keep focused under these settings.
func targetOf(st settings) (exe, class string) {
	if st.Exe != "" {
		return st.Exe, st.Class
	}
	t := targets[targetByID(st.Target)]
	return t.exe, t.class
}
