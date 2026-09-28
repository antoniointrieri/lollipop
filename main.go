// lollipop: an always-on-top traffic light for AI agents running in Orca and in plain Claude Code sessions.
// See README.md and docs/spec.md.
package main

//go:generate go test -run TestAppIcon -update
// Exe icon, manifest (same DPI awareness Wails sets at runtime) and version info. CI regenerates it on every build, so a release gets its tag.
//go:generate go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64 --icon lollipop.ico --manifest gui --product-name lollipop --file-description "lollipop: traffic light for AI coding agents" --original-filename lollipop.exe --copyright "Copyright (c) 2026 lollipop contributors" --product-version=git-tag --file-version=git-tag

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed frontend
var assets embed.FS

const pollInterval = 500 * time.Millisecond

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hook" { // run by Claude Code on every hook event: keep it fast
		runHook()
	}
	s := loadSettings()
	uiLang.Store(resolveLang(s.Lang))
	once := flag.Bool("once", false, tr("print agents and the active pane, then exit", "stampa agenti e pannello attivo, poi esce"))
	flag.Parse()
	orca := &orcaClient{}
	if *once {
		printOnce(orca)
		return
	}

	frontend, _ := fs.Sub(assets, "frontend")
	app := application.New(application.Options{
		Name: "lollipop",
		// icon of the settings window: Wails looks for resource 3, go-winres stores the exe icon under another id
		Icon:   lollipopPNGSize(color.RGBA{255, 0, 0, 255}, 64),
		Assets: application.AssetOptions{Handler: application.BundledAssetFileServer(frontend)},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory, // no Dock icon
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
	})
	u := &ui{app: app, s: s, idle: true} // hidden until the first poll finds an agent
	u.win = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "lollipop",
		Width: 60, Height: 36, // replaced by the "size" event from the frontend
		Frameless: true, AlwaysOnTop: u.s.AlwaysOnTop, DisableResize: true, Hidden: true,
		BackgroundColour:           application.NewRGB(32, 32, 36),
		DefaultContextMenuDisabled: true,
		Windows:                    application.WindowsWindow{HiddenOnTaskbar: true},
	})
	u.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel() // Alt+F4 only hides the window; quitting is "Esci"
		go u.change(func(s *settings) { s.ShowWindow = false })
	})
	u.setupTray()
	u.buildMenus()
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() { // one after the other: both update the menus, which Wails doesn't guard against concurrent use
			u.autostartStartup()
			u.claudeStartup()
		}()
		go u.checkUpdates()
	})

	tr := newTracker()
	var mu sync.Mutex
	lastSig := ""
	agents := map[string]agent{}

	app.Event.On("ready", func(*application.CustomEvent) {
		u.emitSettings(u.get())
		mu.Lock()
		lastSig = "" // resend the state to a (re)loaded frontend
		mu.Unlock()
	})
	app.Event.On("size", func(e *application.CustomEvent) {
		m, _ := e.Data.(map[string]any)
		w, _ := m["w"].(float64)
		h, _ := m["h"].(float64)
		bar, _ := m["bar"].(float64)
		up, _ := m["up"].(bool)
		mu.Lock()
		defer mu.Unlock()
		u.place(int(w), int(h), int(bar), up)
	})
	app.Event.On("focus", func(e *application.CustomEvent) {
		m, _ := e.Data.(map[string]any)
		key, _ := m["key"].(string)
		tr.seen(key)
		mu.Lock()
		a := agents[key]
		mu.Unlock()
		if a.Host != nil {
			activateHost(*a.Host)
			if link := sessionLink(*a.Host); link != "" {
				time.Sleep(150 * time.Millisecond) // lets VS Code register which of its windows is focused
				_ = app.Browser.OpenURL(link)
			}
			return
		}
		activateOrca(orca.orcaPID())
		if err := orca.focusTerminal(a.Handle); err != nil {
			log.Println("terminal.focus:", err)
		}
	})

	// settings window
	app.Event.On("settings-ready", func(*application.CustomEvent) {
		u.emitSettings(u.get())
		u.emitStatus()
	})
	app.Event.On("setting", func(e *application.CustomEvent) {
		m, _ := e.Data.(map[string]any)
		key, _ := m["key"].(string)
		u.set(key, m["value"])
	})
	app.Event.On("autostart", func(e *application.CustomEvent) {
		on, _ := e.Data.(bool)
		u.setAutostart(on)
	})
	app.Event.On("claude", func(e *application.CustomEvent) {
		action, _ := e.Data.(string)
		u.claudeRun(action)
	})
	app.Event.On("open-claude-settings", func(*application.CustomEvent) {
		p := claudeSettingsPath()
		if _, err := os.Stat(p); err != nil {
			p = claudeConfigDir() // no settings.json yet: its folder
		}
		_ = app.Browser.OpenFile(p)
	})
	app.Event.On("open-release", func(e *application.CustomEvent) {
		if url, _ := e.Data.(string); strings.HasPrefix(url, "https://github.com/antoniointrieri/lollipop/") {
			_ = app.Browser.OpenURL(url)
		}
	})
	app.Event.On("open-repo", func(*application.CustomEvent) {
		_ = app.Browser.OpenURL("https://github.com/antoniointrieri/lollipop")
	})

	go func() {
		for range time.Tick(pollInterval) {
			if s := u.get(); s.AlwaysOnTop && s.ShowWindow {
				keepOnTop(u.win)
			}
		}
	}()
	go func() { // off the UI thread: a slow or stuck Orca must not freeze the window
		for range time.Tick(pollInterval) {
			s, err := pollAll(orca)
			seen := func(a agent) bool {
				if a.Host != nil {
					return hostInFront(*a.Host)
				}
				return a.Key == s.Focused && orcaInFront(orca.orcaPID())
			}
			set := u.get()
			items, errMsg := arrange(tr.update(s, seen), set.Order, set.Side, set.GroupByState, set.DoneMax), ""
			if err != nil {
				errMsg = err.Error()
			}
			u.updateTray(items, errMsg)
			msg := map[string]any{"items": items, "error": errMsg}
			sig, _ := json.Marshal(msg)
			mu.Lock()
			changed := string(sig) != lastSig
			lastSig = string(sig)
			clear(agents)
			for _, a := range s.Agents {
				agents[a.Key] = a
			}
			mu.Unlock()
			if changed {
				app.Event.Emit("agents", msg)
			}
		}
	}()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// pollAll merges Orca agents and plain Claude Code sessions. Orca not running is not an error:
// the user may only use Claude Code.
func pollAll(orca *orcaClient) (snapshot, error) {
	s, err := orca.poll()
	if errors.Is(err, errOrcaAbsent) {
		err = nil
	}
	s.Agents = append(s.Agents, loadClaudeSessions()...)
	sortAgents(s.Agents)
	return s, err
}

func printOnce(orca *orcaClient) {
	s, err := pollAll(orca)
	if err != nil {
		fmt.Println(tr("Error:", "Errore:"), err)
	}
	for _, a := range s.Agents {
		where := strings.TrimSpace(a.Handle)
		if a.Host != nil {
			where = sessionLink(*a.Host)
		}
		fmt.Printf("%-10s %-30s %-40s %s\n", a.State, a.Label, a.Title, where)
	}
	fmt.Println(tr("Focused:", "Pannello attivo:"), s.Focused, tr(" Orca in front:", " Orca in primo piano:"), orcaInFront(orca.orcaPID()))
	fmt.Println(tr("Claude Code integration:", "Integrazione Claude Code:"), claudeHooksStatus().label())
	fmt.Print(tr("Version: ", "Versione: "), appVersion())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if tag, _, err := latestRelease(ctx); err == nil {
		fmt.Print(tr(", latest release: ", ", ultima release: "), tag)
		if newer(tag, appVersion()) {
			fmt.Print(tr(" (update available)", " (aggiornamento disponibile)"))
		}
	}
	fmt.Println()
}

// exePath is the running executable with symlinks resolved: what the Claude Code hook and the login entry launch.
func exePath() string {
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}
