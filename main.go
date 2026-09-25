// lollipop: semaforo per gli agenti di Orca. Finestrella sempre in primo piano, un pallino per agente.
//
//	giallo = working, verde = done, blu = turno finito ma comandi in background ancora attivi,
//	rosso = qualsiasi altro stato (blocked/waiting: attesa input o permesso)
//	lampeggia = ha appena smesso di lavorare e non l'hai ancora guardato
//
// Clic sulla voce -> apre Orca su quell'agente. Tasto destro (anche sull'icona nella traybar) -> impostazioni ed Esci.
// Trascina dalla maniglia a sinistra.
// Debug: lollipop -once (su Windows, binario GUI: lollipop.exe -once | more)
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed frontend
var assets embed.FS

const pollInterval = 500 * time.Millisecond

func main() {
	once := flag.Bool("once", false, "stampa agenti e pannello attivo, poi esce")
	flag.Parse()
	orca := &orcaClient{}
	if *once {
		s, err := orca.poll()
		if err != nil {
			fmt.Println("Errore:", err)
			os.Exit(1)
		}
		for _, a := range s.Agents {
			fmt.Printf("%-10s %-30s %-40s %s\n", a.State, a.Label, a.Title, a.Handle)
		}
		fmt.Println("Focused:", s.Focused, " Orca in primo piano:", orcaInFront(orca.orcaPID()))
		return
	}

	frontend, _ := fs.Sub(assets, "frontend")
	app := application.New(application.Options{
		Name:   "lollipop",
		Assets: application.AssetOptions{Handler: application.BundledAssetFileServer(frontend)},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory, // niente icona nel Dock
			ApplicationShouldTerminateAfterLastWindowClosed: false, // si esce solo da "Esci"
		},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
	})
	u := &ui{app: app, s: loadSettings()}
	u.win = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "lollipop",
		Width: 60, Height: 36, // provvisori: il frontend comunica la dimensione vera con l'evento "size"
		Frameless: true, AlwaysOnTop: u.s.AlwaysOnTop, DisableResize: true, Hidden: true,
		BackgroundColour:           application.NewRGB(32, 32, 36),
		DefaultContextMenuDisabled: true,
		Windows:                    application.WindowsWindow{HiddenOnTaskbar: true},
	})
	// Chiudere la finestra (es. Alt+F4) la nasconde soltanto: si ritrova dall'icona nella traybar.
	u.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		go u.change(func(s *settings) { s.ShowWindow = false })
	})
	menu := app.ContextMenu.New()
	u.buildMenu(menu.Menu)
	app.ContextMenu.Add("main", menu)
	u.setupTray()

	tr := newTracker()
	var mu sync.Mutex
	lastSig := "" // ultimo stato inviato al frontend: si reinvia solo se cambia

	app.Event.On("ready", func(*application.CustomEvent) { // frontend (ri)caricato: reinvia impostazioni e stato
		app.Event.Emit("settings", u.get())
		mu.Lock()
		lastSig = ""
		mu.Unlock()
	})
	app.Event.On("size", func(e *application.CustomEvent) {
		m, _ := e.Data.(map[string]any)
		w, _ := m["w"].(float64)
		h, _ := m["h"].(float64)
		mu.Lock()
		defer mu.Unlock()
		u.place(int(w), int(h))
	})
	app.Event.On("focus", func(e *application.CustomEvent) {
		m, _ := e.Data.(map[string]any)
		key, _ := m["key"].(string)
		handle, _ := m["handle"].(string)
		tr.seen(key)
		activateOrca(orca.orcaPID())
		if err := orca.focusTerminal(handle); err != nil {
			log.Println("terminal.focus:", err)
		}
	})

	go func() {
		for range time.Tick(pollInterval) {
			if s := u.get(); s.AlwaysOnTop && s.ShowWindow {
				keepOnTop(u.win)
			}
		}
	}()
	// Polling fuori dal thread UI: se Orca e' lento o bloccato la finestra resta reattiva.
	go func() {
		for range time.Tick(pollInterval) {
			s, err := orca.poll()
			items, errMsg := tr.update(s, func() bool { return orcaInFront(orca.orcaPID()) }), ""
			if err != nil {
				errMsg = err.Error()
			}
			u.updateTray(items, errMsg)
			msg := map[string]any{"items": items, "error": errMsg}
			sig, _ := json.Marshal(msg)
			mu.Lock()
			changed := string(sig) != lastSig
			lastSig = string(sig)
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
