// lollipop: semaforo per gli agenti di Orca. Finestrella sempre in primo piano, un pallino per agente.
//
//	giallo = working, verde = done, blu = turno finito ma comandi in background ancora attivi,
//	rosso = qualsiasi altro stato (blocked/waiting: attesa input o permesso)
//	lampeggia = ha appena smesso di lavorare e non l'hai ancora guardato
//
// Clic sulla voce -> apre Orca su quell'agente. Tasto destro -> Esci. Trascina dalla maniglia a sinistra.
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
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
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
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "lollipop",
		Width: 60, Height: 36, // provvisori: il frontend comunica la dimensione vera con l'evento "size"
		Frameless: true, AlwaysOnTop: true, DisableResize: true, Hidden: true,
		BackgroundColour:           application.NewRGB(32, 32, 36),
		DefaultContextMenuDisabled: true,
		Windows:                    application.WindowsWindow{HiddenOnTaskbar: true},
	})

	menu := app.ContextMenu.New()
	menu.Add("Esci").OnClick(func(*application.Context) {
		savePosition(win.Bounds())
		app.Quit()
	})
	app.ContextMenu.Add("main", menu)

	tr := newTracker()
	var mu sync.Mutex
	lastSig := "" // ultimo stato inviato al frontend: si reinvia solo se cambia
	placed := false

	app.Event.On("ready", func(*application.CustomEvent) { // frontend (ri)caricato: reinvia lo stato
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
		place(app, win, int(w), int(h), !placed)
		placed = true
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
			keepOnTop(win)
		}
	}()
	// Polling fuori dal thread UI: se Orca e' lento o bloccato la finestra resta reattiva.
	go func() {
		for range time.Tick(pollInterval) {
			s, err := orca.poll()
			msg := map[string]any{"items": tr.update(s, func() bool { return orcaInFront(orca.orcaPID()) }), "error": ""}
			if err != nil {
				msg["error"] = err.Error()
			}
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

// place ridimensiona la finestra al contenuto tenendo fermo il bordo destro, clampato allo schermo su cui si trova.
// Al primo posizionamento parte dalla posizione salvata (o in alto a destra sullo schermo principale) e mostra la finestra.
func place(app *application.App, win *application.WebviewWindow, w, h int, first bool) {
	b := win.Bounds()
	right, top := b.X+b.Width, b.Y
	scr, _ := win.GetScreen()
	if first {
		scr = nil
		if p, ok := loadPosition(); ok {
			for _, s := range app.Screen.GetAll() {
				if wa := s.WorkArea; p.Right > wa.X && p.Right <= wa.X+wa.Width && p.Top >= wa.Y && p.Top < wa.Y+wa.Height {
					scr, right, top = s, p.Right, p.Top
				}
			}
		}
		if scr == nil {
			scr = app.Screen.GetPrimary()
			right, top = scr.WorkArea.X+scr.WorkArea.Width-80, scr.WorkArea.Y+40
		}
	}
	if scr != nil {
		right = min(right, scr.WorkArea.X+scr.WorkArea.Width)
		win.SetBounds(application.Rect{X: max(scr.WorkArea.X, right-w), Y: top, Width: w, Height: h})
	}
	if first {
		win.Show()
	}
}

type position struct{ Right, Top int }

func positionFile() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "lollipop", "position.json")
}

func loadPosition() (p position, ok bool) {
	raw, err := os.ReadFile(positionFile())
	return p, err == nil && json.Unmarshal(raw, &p) == nil
}

func savePosition(b application.Rect) {
	raw, _ := json.Marshal(position{Right: b.X + b.Width, Top: b.Y})
	if os.MkdirAll(filepath.Dir(positionFile()), 0o755) == nil {
		_ = os.WriteFile(positionFile(), raw, 0o644)
	}
}
