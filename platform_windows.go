package main

import (
	"net"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

var (
	user32       = syscall.NewLazyDLL("user32.dll")
	procIsIconic = user32.NewProc("IsIconic")
	procGetOwner = user32.NewProc("GetWindow")
)

func dial(kind, endpoint string) (net.Conn, error) {
	timeout := 3 * time.Second
	return winio.DialPipe(endpoint, &timeout)
}

func orcaInFront(pid int) bool {
	_, fg := w32.GetWindowThreadProcessId(w32.GetForegroundWindow())
	return pid != 0 && fg == pid
}

// Stato della ricerca con EnumWindows: la callback si crea una volta sola (syscall.NewCallback non le libera mai).
var (
	enumMu    sync.Mutex
	enumPID   int
	enumFound w32.HWND
	enumCB    = syscall.NewCallback(func(h w32.HWND, _ uintptr) uintptr {
		const gwOwner = 4
		if _, p := w32.GetWindowThreadProcessId(h); p != enumPID || !w32.IsWindowVisible(h) || w32.GetWindowTextLength(h) == 0 {
			return 1
		}
		if owner, _, _ := procGetOwner.Call(uintptr(h), gwOwner); owner != 0 {
			return 1
		}
		enumFound = h
		return 0
	})
)

// Porta in primo piano la finestra principale di Orca (visibile, senza owner, con titolo), ripristinandola se minimizzata.
// SetForegroundWindow e' concesso perche' l'utente ha appena cliccato la nostra finestra.
func activateOrca(pid int) {
	enumMu.Lock()
	enumPID, enumFound = pid, 0
	w32.EnumWindows(enumCB, 0)
	main := enumFound
	enumMu.Unlock()
	if main == 0 || pid == 0 {
		return
	}
	if iconic, _, _ := procIsIconic.Call(uintptr(main)); iconic != 0 {
		w32.ShowWindow(main, w32.SW_RESTORE)
	}
	w32.SetForegroundWindow(main)
}

// Anche la taskbar e' topmost: cliccandola ci finisce sopra. Riafferma topmost senza rubare il focus.
func keepOnTop(win *application.WebviewWindow) {
	application.InvokeAsync(func() {
		if h := w32.HWND(uintptr(unsafe.Pointer(win.NativeWindow()))); h != 0 {
			w32.SetWindowPos(h, w32.HWND_TOPMOST, 0, 0, 0, 0, w32.SWP_NOSIZE|w32.SWP_NOMOVE|w32.SWP_NOACTIVATE)
		}
	})
}
