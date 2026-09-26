package main

import (
	"errors"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procIsIconic      = user32.NewProc("IsIconic")
	procGetWindow     = user32.NewProc("GetWindow")
	procGetAncestor   = user32.NewProc("GetAncestor")
	procGetClassName  = user32.NewProc("GetClassNameW")
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procFreeConsole   = kernel32.NewProc("FreeConsole")
	procGetConsoleWnd = kernel32.NewProc("GetConsoleWindow")
	procGetUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")
)

func dial(kind, endpoint string) (net.Conn, error) {
	timeout := 3 * time.Second
	return winio.DialPipe(endpoint, &timeout)
}

func orcaInFront(pid int) bool {
	_, fg := w32.GetWindowThreadProcessId(w32.GetForegroundWindow())
	return pid != 0 && fg == pid
}

// The EnumWindows callback is created once: syscall.NewCallback never frees callbacks.
var (
	enumMu    sync.Mutex
	enumPID   int
	enumFound []w32.HWND
	enumCB    = syscall.NewCallback(func(h w32.HWND, _ uintptr) uintptr {
		const gwOwner = 4
		if _, p := w32.GetWindowThreadProcessId(h); p != enumPID || !w32.IsWindowVisible(h) || w32.GetWindowTextLength(h) == 0 {
			return 1
		}
		if owner, _, _ := procGetWindow.Call(uintptr(h), gwOwner); owner != 0 {
			return 1
		}
		enumFound = append(enumFound, h)
		return 1
	})
)

// mainWindows returns the process's visible, unowned, titled top-level windows.
func mainWindows(pid int) []w32.HWND {
	if pid == 0 {
		return nil
	}
	enumMu.Lock()
	defer enumMu.Unlock()
	enumPID, enumFound = pid, nil
	w32.EnumWindows(enumCB, 0)
	return enumFound
}

// pickWindow prefers, for an IDE with several windows, the one with the session folder in its title.
func pickWindow(h hostRef) w32.HWND {
	if hw := w32.HWND(h.HWND); hw != 0 && w32.IsWindow(hw) && w32.IsWindowVisible(hw) {
		return hw
	}
	wins := mainWindows(h.PID)
	for _, w := range wins {
		if h.Folder != "" && strings.Contains(strings.ToLower(w32.GetWindowText(w)), strings.ToLower(h.Folder)) {
			return w
		}
	}
	if len(wins) > 0 {
		return wins[0]
	}
	return 0
}

// SetForegroundWindow is allowed because the user just clicked our window.
func activateWindow(w w32.HWND) {
	if w == 0 {
		return
	}
	if iconic, _, _ := procIsIconic.Call(uintptr(w)); iconic != 0 {
		w32.ShowWindow(w, w32.SW_RESTORE)
	}
	w32.SetForegroundWindow(w)
}

func activateOrca(pid int) { activateWindow(pickWindow(hostRef{PID: pid})) }

func activateHost(h hostRef) { activateWindow(pickWindow(h)) }

func hostInFront(h hostRef) bool {
	fg := w32.GetForegroundWindow()
	if h.HWND != 0 {
		return uintptr(fg) == h.HWND
	}
	if _, p := w32.GetWindowThreadProcessId(fg); p != h.PID || h.PID == 0 {
		return false
	}
	return len(mainWindows(h.PID)) == 1 || pickWindow(h) == fg
}

// processStart returns the creation time of a live process, 0 otherwise.
func processStart(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(p)
	var code uint32
	if windows.GetExitCodeProcess(p, &code) != nil || code != 259 { // STILL_ACTIVE
		return 0
	}
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(p, &created, &exited, &kernel, &user) != nil {
		return 0
	}
	return created.Nanoseconds()
}

// findHost finds the window hosting a Claude Code process:
//  1. its console window if visible (classic console), or the window owning it (Windows Terminal, ConPTY);
//  2. otherwise the first ancestor process with a main window (IDEs, desktop apps, ...).
func findHost(claudePID int) hostRef {
	if claudePID <= 0 {
		return hostRef{}
	}
	type proc struct {
		ppid int
		exe  string
	}
	procs := map[int]proc{}
	if snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0); err == nil {
		e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
		for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
			procs[int(e.ProcessID)] = proc{int(e.ParentProcessID), strings.TrimSuffix(windows.UTF16ToString(e.ExeFile[:]), ".exe")}
		}
		windows.CloseHandle(snap)
	}
	if w := consoleWindow(claudePID); w != 0 {
		_, p := w32.GetWindowThreadProcessId(w)
		return hostRef{PID: p, HWND: uintptr(w), Name: procs[p].exe}
	}
	for p, i := procs[claudePID].ppid, 0; p != 0 && i < 20; p, i = procs[p].ppid, i+1 {
		if len(mainWindows(p)) > 0 {
			return hostRef{PID: p, Name: procs[p].exe}
		}
	}
	return hostRef{}
}

// consoleWindow attaches to the process's console for a moment: the hook process is a GUI app and has none.
func consoleWindow(pid int) w32.HWND {
	const gaRootOwner = 3
	procFreeConsole.Call()
	if ok, _, _ := procAttachConsole.Call(uintptr(pid)); ok == 0 {
		return 0
	}
	h, _, _ := procGetConsoleWnd.Call()
	procFreeConsole.Call()
	w := w32.HWND(h)
	if w == 0 {
		return 0
	}
	// A ConPTY console (Windows Terminal, Warp, IDEs) is a 0x0 PseudoConsoleWindow that still reports as
	// visible: only its owner, when the terminal sets one, is a real window.
	if w32.IsWindowVisible(w) && className(w) != "PseudoConsoleWindow" {
		return w
	}
	if owner, _, _ := procGetAncestor.Call(h, gaRootOwner); owner != 0 && owner != h && w32.IsWindowVisible(w32.HWND(owner)) {
		return w32.HWND(owner)
	}
	return 0
}

func className(w w32.HWND) string {
	var buf [256]uint16
	n, _, _ := procGetClassName.Call(uintptr(w), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

// The taskbar is topmost too and wins when clicked: re-assert topmost without stealing focus.
func keepOnTop(win *application.WebviewWindow) {
	application.InvokeAsync(func() {
		if h := w32.HWND(uintptr(unsafe.Pointer(win.NativeWindow()))); h != 0 {
			w32.SetWindowPos(h, w32.HWND_TOPMOST, 0, 0, 0, 0, w32.SWP_NOSIZE|w32.SWP_NOMOVE|w32.SWP_NOACTIVATE)
		}
	})
}

// osLanguage returns "it" when the Windows display language is Italian (primary LANGID 0x10).
func osLanguage() string {
	if id, _, _ := procGetUILanguage.Call(); id&0x3ff == 0x10 {
		return "it"
	}
	return "en"
}

const (
	runKey        = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvedKey   = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	autostartName = "lollipop"
)

func autostartCommand() string { return `"` + exePath() + `"` }

// autostartState reads the per-user Run entry. Task Manager can disable it without removing it: then its
// StartupApproved value starts with an odd byte (02 enabled, 03 disabled).
func autostartState() (on, current bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer k.Close()
	cmd, _, err := k.GetStringValue(autostartName)
	if err != nil {
		return false, false
	}
	if a, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.QUERY_VALUE); err == nil {
		v, _, err := a.GetBinaryValue(autostartName)
		a.Close()
		if err == nil && len(v) > 0 && v[0]&1 == 1 {
			return false, false
		}
	}
	return true, cmd == autostartCommand()
}

func setAutostart(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	// A "disabled" mark left by Task Manager would win over the new entry.
	if a, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.SET_VALUE); err == nil {
		_ = a.DeleteValue(autostartName)
		a.Close()
	}
	if on {
		return k.SetStringValue(autostartName, autostartCommand())
	}
	if err := k.DeleteValue(autostartName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
