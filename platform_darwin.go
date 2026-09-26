package main

// ponytail: NEVER RUN on a real Mac (written without one, see docs/spec.md §3.4 and §9); it only compiles in CI.
// First check on a Mac: lollipop -once.

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <libproc.h>
#include <sys/proc_info.h>

static int frontPID(void) {
	NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
	return app ? app.processIdentifier : 0;
}

// ponytail: doesn't unminimize windows; if needed, open app.bundleURL (Electron "reopen" event)
static void activatePID(int pid) {
	NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
	[app unhide];
	[app activateWithOptions:NSApplicationActivateAllWindows | NSApplicationActivateIgnoringOtherApps];
}

// Parent pid and start time (microseconds); returns 0 if the process doesn't exist.
static int procInfo(int pid, int *ppid, long long *start) {
	struct proc_bsdinfo info;
	if (proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info)) != sizeof(info)) return 0;
	*ppid = info.pbi_ppid;
	*start = (long long)info.pbi_start_tvsec * 1000000 + info.pbi_start_tvusec;
	return 1;
}

static int preferredItalian(void) {
	NSString *l = [NSLocale preferredLanguages].firstObject;
	return l && [l hasPrefix:@"it"];
}

// A regular app (windows, Dock icon) is the one hosting the session.
static int regularApp(int pid, char *name, int size) {
	NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
	if (!app || app.activationPolicy != NSApplicationActivationPolicyRegular) return 0;
	const char *n = app.localizedName ? app.localizedName.UTF8String : "";
	strlcpy(name, n, size);
	return 1;
}
*/
import "C"

import (
	"encoding/xml"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func dial(kind, endpoint string) (net.Conn, error) {
	return net.DialTimeout("unix", endpoint, 3*time.Second)
}

func orcaInFront(pid int) bool { return pid != 0 && int(C.frontPID()) == pid }

func activateOrca(pid int) {
	if pid != 0 {
		C.activatePID(C.int(pid))
	}
}

func activateHost(h hostRef) { activateOrca(h.PID) }

// ponytail: with several windows of one app (two VS Code projects) the app counts, not the window
func hostInFront(h hostRef) bool { return orcaInFront(h.PID) }

func processStart(pid int) int64 {
	var ppid C.int
	var start C.longlong
	if pid <= 0 || C.procInfo(C.int(pid), &ppid, &start) == 0 {
		return 0
	}
	return int64(start) * 1000
}

// findHost returns Claude's first ancestor that is a regular app (Terminal, iTerm2, VS Code, ...).
func findHost(claudePID int) hostRef {
	var name [256]C.char
	for p, i := claudePID, 0; p > 1 && i < 20; i++ {
		var ppid C.int
		var start C.longlong
		if C.procInfo(C.int(p), &ppid, &start) == 0 {
			break
		}
		p = int(ppid)
		if p > 1 && C.regularApp(C.int(p), &name[0], C.int(len(name))) != 0 {
			return hostRef{PID: p, Name: C.GoString(&name[0])}
		}
	}
	return hostRef{}
}

// AlwaysOnTop's floating level isn't covered by the Dock: nothing to re-assert.
func keepOnTop(*application.WebviewWindow) {}

func osLanguage() string {
	if C.preferredItalian() != 0 {
		return "it"
	}
	return "en"
}

const launchAgentLabel = "io.github.antoniointrieri.lollipop"

// launchd loads the user's LaunchAgents at login: no launchctl needed. Without KeepAlive, Quit stays quit.
func launchAgentPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

func launchAgent() string {
	var exe strings.Builder
	_ = xml.EscapeText(&exe, []byte(exePath()))
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + launchAgentLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + exe.String() + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`
}

// ponytail: an agent disabled in System Settings > Login Items still counts as on
func autostartState() (on, current bool) {
	raw, err := os.ReadFile(launchAgentPath())
	return err == nil, string(raw) == launchAgent()
}

func setAutostart(on bool) error {
	if !on {
		if err := os.Remove(launchAgentPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeAtomic(launchAgentPath(), []byte(launchAgent()))
}
