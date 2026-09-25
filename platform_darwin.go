package main

// ponytail: MAI COMPILATO NE' PROVATO (scritto senza un Mac, vedi docs/spec.md §3.4). Primo collaudo: lollipop -once.

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

static int frontPID(void) {
	NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
	return app ? app.processIdentifier : 0;
}

// ponytail: non de-minimizza le finestre; se serve, aprire app.bundleURL (evento "reopen" di Electron)
static void activatePID(int pid) {
	NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
	[app unhide];
	[app activateWithOptions:NSApplicationActivateAllWindows | NSApplicationActivateIgnoringOtherApps];
}
*/
import "C"

import (
	"net"
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

// Il livello "floating" di AlwaysOnTop non viene coperto dal Dock: niente da riaffermare.
func keepOnTop(*application.WebviewWindow) {}
