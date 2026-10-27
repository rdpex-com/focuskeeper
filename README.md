# focuskeeper

A small Windows tray tool that gives the keyboard and mouse focus back to your virtual machine's window, so the first
click into the VM is not lost.

[README in Russian](README.ru.md)

## The problem

You run a VM in VMware Workstation (or VirtualBox, or Hyper-V VMConnect) as a normal window on your desktop. You
switch to another program - a messenger, Notepad, a full-screen RDP client - and then minimize or close it. The VM
window is in front again and looks active, but Windows has left the focus on the desktop or the taskbar. Your next
click only activates the VM window; it never reaches the guest. Clicks seem to get "eaten", typing goes nowhere.

focuskeeper watches for that moment and gives the focus back to the VM window.

## Download and run

Take `focuskeeper.exe` from the [Releases](../../releases) page, put it in any folder (for example
`C:\Tools\focuskeeper`) and run it. A round blue icon appears in the tray. Starting it twice does nothing.

The exe is **not code-signed**, so Windows SmartScreen may say "Windows protected your PC" on the first start: choose
**More info** - **Run anyway**, after you have checked the hash (below) or built it yourself.

### Verify the download

Each release is built from the tagged source by GitHub Actions ([release.yml](.github/workflows/release.yml)); the
release page lists the SHA-256 of the exe and the workflow run that built it.

- Check the hash in PowerShell: `Get-FileHash focuskeeper.exe` (or `certutil -hashfile focuskeeper.exe SHA256`) and
  compare it with `SHA256SUMS.txt` from the release.
- Check that GitHub built it from this repository (GitHub CLI):
  `gh attestation verify focuskeeper.exe -R <owner>/<repo>`.
- Or build it yourself (below): with the same Go version the build is reproducible - you get the very same bytes and
  the same hash as the release.

## Start with Windows

Tick **Start with Windows** in the tray menu. There is no installer: focuskeeper writes one value for your user only
(no admin rights) -

    HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run
        focuskeeper = "<the full path of focuskeeper.exe>"

Untick it to delete the value. The tick is read from the registry each time the menu opens, so it is right even when
you turned autostart off elsewhere (Task Manager - Startup).

**Moved the exe?** The value still points at the old folder; the item then reads *Start with Windows (set for another
copy - click to use this one)*. Click it once and the value points at the exe you are running.

To remove autostart by hand: Task Manager - **Startup apps** - focuskeeper - **Disable**, or delete the value
`focuskeeper` in the key above (`regedit`, or in PowerShell:
`Remove-ItemProperty HKCU:\Software\Microsoft\Windows\CurrentVersion\Run -Name focuskeeper`).

## The tray menu

Right or left click on the icon:

- the status line: on / paused, the mode, dry mode;
- **Soft** / **On hover** - the mode (see below);
- **VM window** - which program's window to keep: VMware Workstation (default), VirtualBox, Hyper-V (VMConnect);
- **Language** - English or Russian (by default, the Windows display language);
- **Start with Windows** - see above;
- **Pause** / **Resume**;
- **Open log**;
- **Dry mode** - only write to the log what it would do (the icon turns amber);
- **Exit**.

The icon is grey while paused; its tooltip shows the time of the last action. The white ring and dot mean the VM
window has the focus; a faint ring without the dot - another window has it. The mode, the VM program and the
language are remembered in `focuskeeper.settings.json` next to the exe.

## How it works: the two modes

**Soft** (the default) never reacts to the mouse. The VM window gets the focus back when no other window lies over
it any more: the window that covered it was minimized, closed or moved away. It is decided 200 ms after the last
window change (Windows events: foreground, minimize, show/hide, close, move - no polling) and checked again every
second. The taskbar, hidden and tool windows and menus do not count as covering. While your messenger stays open over
the VM nothing happens; minimize or close it and the focus is back in the VM at once.

**On hover** adds a mouse rule. It suits a VM window that does not fill the screen:

- the desktop, the taskbar or nothing is in front for 250 ms (no mouse button held, so a click on a taskbar button
  still opens that program) and the VM window is visible - it gets the focus;
- the mouse moved while another program was in front and now rests 250 ms on a visible part of the VM window - the
  VM window gets the focus, so your next click reaches the guest. Never when the mouse has not moved (it may stay
  parked over the VM while you type elsewhere), never while a key or a mouse button is held, never when a full-screen
  window (an RDP client, a game) is in front.

How the focus is taken: Windows lets a background program take the foreground only if it "received the last input
event". focuskeeper sends one empty mouse input (no movement, no button, no key) and then calls
`SetForegroundWindow`; if Windows still refuses, it attaches to the foreground window's input thread
(`AttachThreadInput`). No key presses are faked - an Alt tap would reach the guest.

## Other programs

To keep any other program's window, write its exe (and, if it has several big windows, the window class) into
`focuskeeper.settings.json`:

```json
{ "mode": "soft", "target": "vmware", "exe": "qemu-system-x86_64.exe", "class": "" }
```

`exe` wins over `target`. Or for one run: `focuskeeper.exe -exe qemu-system-x86_64.exe`.

## Log

`focuskeeper.jsonl` next to the exe, one JSON line per event (moved to `focuskeeper.jsonl.1` past 5 MB):

- `{"ev":"start", "version", "windows":"10.0.22631", "exe", "mode", "lang", ...}` - started;
- `{"ev":"focus", "trigger":"uncovered"|"A"|"B", "mode":"soft"|"hover", "prev":{"cls","exe"}, "target":{"cls","exe"}, "result":"ok"|"refused"|"dry", "how":"input"|"attach"}`
  - an action: what was in front before, and whether Windows let the focus change;
- `{"ev":"skip", "trigger":"uncovered", "mode":"soft", "why":"covered", "over":{"cls","exe"}}` - the desktop or the
  taskbar is in front but that window still lies over the VM;
- `{"ev":"skip", "trigger":"B", "mode":"hover", "why":"mouse-still"|"button-down"|"key-down"|"fullscreen"}` - the mouse
  rested on the VM while another program was in front, and focuskeeper left it alone;
- `{"ev":"autostart", "on", "value", "was", "err"}` - **Start with Windows** clicked: the Run value now (and before,
  if it pointed at another copy);
- `{"ev":"settings", ...}`, `{"ev":"stop"}`.

## Command line

    focuskeeper.exe [-dry] [-mode soft|hover] [-target vmware|virtualbox|hyperv] [-exe <x.exe> [-class <class>]] [-log <file>]

Flags change only this run; the tray choices stay in the settings file. For testing: `-trace` logs the quiet
decisions too, `-method input|attach|plain` forces one way of taking the focus, `-standin [-rect x,y,w,h] [-full]`
opens a test window instead.

## Build it yourself

Install Go (the version pinned in `build.ps1`, now **1.27.2**, gives the release's exact bytes), then in this folder:

    build.cmd v1.0.0

It runs `go build -trimpath -buildvcs=false -ldflags "-s -w -H windowsgui -buildid= -X main.version=v1.0.0"` and
writes `release\focuskeeper.exe` and `release\SHA256SUMS.txt`. Tests: `go test ./...` (both modes on a scripted
desktop). `check/` is a scenario driver that moves the mouse, clicks and types - run it only on a test machine.

## License

Copyright (c) 2026 rdpex.com (https://rdpex.com).

focuskeeper is free for personal and other noncommercial use - at home, for study, in charities, schools and public
bodies - under the [PolyForm Noncommercial License 1.0.0](LICENSE.md). Any commercial use needs a separate permission
from the author. Third-party code in the exe (Go, golang.org/x/sys - BSD 3-Clause): [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md).

VMware, VirtualBox and Hyper-V are trademarks of their owners; focuskeeper is not affiliated with them.
