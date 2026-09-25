package main

// Impostazioni (menu della finestra e dell'icona nella traybar), posizione della finestra, icona nella traybar.

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type position struct{ Right, Top int }

type settings struct {
	Pos         *position `json:"pos,omitempty"` // bordo destro e superiore della finestra
	Shape       string    `json:"shape"`         // "rect" | "pill"
	BlinkMs     int       `json:"blinkMs"`       // 1000 | 500 | 250
	Scale       int       `json:"scale"`         // percentuale: 85 | 100 | 125
	Compact     bool      `json:"compact"`       // solo pallini, nome nel tooltip
	AlwaysOnTop bool      `json:"alwaysOnTop"`
	ShowWindow  bool      `json:"showWindow"`
}

func settingsFile() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "lollipop", "settings.json")
}

func loadSettings() settings {
	s := settings{Shape: "rect", BlinkMs: 500, Scale: 100, AlwaysOnTop: true, ShowWindow: true}
	if raw, err := os.ReadFile(settingsFile()); err == nil {
		_ = json.Unmarshal(raw, &s) // i campi assenti tengono il predefinito
	}
	return s
}

type ui struct {
	app  *application.App
	win  *application.WebviewWindow
	tray *application.SystemTray

	mu       sync.Mutex
	s        settings
	placed   bool
	trayLast string

	checks []check // voci spuntabili di tutti i menu, riallineate a ogni cambio
	menus  []*application.Menu
}

type check struct {
	item *application.MenuItem
	on   func(settings) bool
}

func (u *ui) get() settings {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.s
}

func radio[T comparable](u *ui, m *application.Menu, label string, field func(*settings) *T, v T) {
	on := func(s settings) bool { return *field(&s) == v }
	item := m.AddRadio(label, on(u.s)).OnClick(func(*application.Context) { u.change(func(s *settings) { *field(s) = v }) })
	u.checks = append(u.checks, check{item, on})
}

func toggle(u *ui, m *application.Menu, label string, field func(*settings) *bool) {
	on := func(s settings) bool { return *field(&s) }
	item := m.AddCheckbox(label, on(u.s)).OnClick(func(*application.Context) { u.change(func(s *settings) { *field(s) = !*field(s) }) })
	u.checks = append(u.checks, check{item, on})
}

// buildMenu: stesso menu per la finestra e per l'icona (oggetti distinti: niente handle nativi condivisi).
func (u *ui) buildMenu(m *application.Menu) *application.Menu {
	shape := func(s *settings) *string { return &s.Shape }
	sub := m.AddSubmenu("Forma")
	radio(u, sub, "Rettangolo", shape, "rect")
	radio(u, sub, "Capsula", shape, "pill")

	blink := func(s *settings) *int { return &s.BlinkMs }
	sub = m.AddSubmenu("Lampeggio")
	radio(u, sub, "Lento (1 s)", blink, 1000)
	radio(u, sub, "Normale (500 ms)", blink, 500)
	radio(u, sub, "Veloce (250 ms)", blink, 250)

	scale := func(s *settings) *int { return &s.Scale }
	sub = m.AddSubmenu("Dimensione")
	radio(u, sub, "Piccola (85%)", scale, 85)
	radio(u, sub, "Normale", scale, 100)
	radio(u, sub, "Grande (125%)", scale, 125)

	toggle(u, m, "Compatta", func(s *settings) *bool { return &s.Compact })
	m.AddSeparator()
	toggle(u, m, "Sempre in primo piano", func(s *settings) *bool { return &s.AlwaysOnTop })
	toggle(u, m, "Mostra finestra", func(s *settings) *bool { return &s.ShowWindow })
	m.AddSeparator()
	m.Add("Esci").OnClick(func(*application.Context) {
		u.save()
		u.app.Quit()
	})
	u.menus = append(u.menus, m)
	return m
}

func (u *ui) change(set func(*settings)) {
	u.mu.Lock()
	set(&u.s)
	s, placed := u.s, u.placed
	u.mu.Unlock()
	for _, c := range u.checks {
		c.item.SetChecked(c.on(s))
	}
	for _, m := range u.menus {
		m.Update()
	}
	u.apply(s, placed)
	u.save()
}

// apply: impostazioni che riguardano la finestra; quelle di aspetto le applica il frontend.
func (u *ui) apply(s settings, placed bool) {
	u.win.SetAlwaysOnTop(s.AlwaysOnTop)
	if placed {
		if s.ShowWindow {
			u.win.Show()
		} else {
			u.win.Hide()
		}
	}
	u.app.Event.Emit("settings", s)
}

// Mai chiamare metodi della finestra tenendo u.mu: aspettano il thread UI, che puo' essere in attesa di u.mu.
func (u *ui) save() {
	var pos *position
	if u.isPlaced() {
		b := u.win.Bounds()
		pos = &position{Right: b.X + b.Width, Top: b.Y}
	}
	u.mu.Lock()
	if pos != nil {
		u.s.Pos = pos
	}
	raw, _ := json.Marshal(u.s)
	u.mu.Unlock()
	if os.MkdirAll(filepath.Dir(settingsFile()), 0o755) == nil {
		_ = os.WriteFile(settingsFile(), raw, 0o644)
	}
}

// place ridimensiona la finestra al contenuto tenendo fermo il bordo destro, clampato allo schermo su cui si trova.
// Al primo posizionamento parte dalla posizione salvata (o in alto a destra sullo schermo principale).
// Chiamata solo dall'evento "size" (serializzato dal chiamante).
func (u *ui) place(w, h int) {
	s, first := u.get(), !u.isPlaced()
	b := u.win.Bounds()
	right, top := b.X+b.Width, b.Y
	scr, _ := u.win.GetScreen()
	if first {
		scr = nil
		if p := s.Pos; p != nil {
			for _, s := range u.app.Screen.GetAll() {
				if wa := s.WorkArea; p.Right > wa.X && p.Right <= wa.X+wa.Width && p.Top >= wa.Y && p.Top < wa.Y+wa.Height {
					scr, right, top = s, p.Right, p.Top
				}
			}
		}
		if scr == nil {
			scr = u.app.Screen.GetPrimary()
			right, top = scr.WorkArea.X+scr.WorkArea.Width-80, scr.WorkArea.Y+40
		}
	}
	if scr != nil {
		right = min(right, scr.WorkArea.X+scr.WorkArea.Width)
		u.win.SetBounds(application.Rect{X: max(scr.WorkArea.X, right-w), Y: top, Width: w, Height: h})
	}
	u.mu.Lock()
	u.placed = true
	u.mu.Unlock()
	if first && s.ShowWindow {
		u.win.Show()
	}
}

func (u *ui) isPlaced() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.placed
}

func (u *ui) setupTray() {
	u.tray = u.app.SystemTray.New()
	u.tray.SetIcon(trayIcons["gray"]).SetMenu(u.buildMenu(application.NewMenu()))
	u.tray.SetTooltip("lollipop")
	u.tray.OnClick(func() { // gira sul thread UI: il lavoro va in una goroutine
		go func() { // mostra la finestra e la porta davanti
			u.change(func(s *settings) { s.ShowWindow = true })
			u.win.Focus()
		}()
	})
}

// updateTray: colore dell'agente piu' urgente e riepilogo nel tooltip, solo se cambiano.
func (u *ui) updateTray(items []item, errMsg string) {
	c, tip := summary(items, errMsg)
	u.mu.Lock()
	changed := c+tip != u.trayLast
	u.trayLast = c + tip
	u.mu.Unlock()
	if changed {
		u.tray.SetIcon(trayIcons[c])
		u.tray.SetTooltip(tip)
	}
}

var trayIcons = map[string][]byte{
	"red":    lollipopPNG(color.RGBA{255, 0, 0, 255}),
	"green":  lollipopPNG(color.RGBA{50, 205, 50, 255}),
	"blue":   lollipopPNG(color.RGBA{0, 191, 255, 255}),
	"yellow": lollipopPNG(color.RGBA{255, 215, 0, 255}),
	"gray":   lollipopPNG(color.RGBA{105, 105, 105, 255}),
}

// lollipopPNG: icona della traybar, 32x32.
func lollipopPNG(base color.RGBA) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, lollipopImage(base, 32))
	return buf.Bytes()
}

// lollipopImage: icona n x n (disegno su griglia 32): testa nel colore dato con vortice bianco
// (spirale di Archimede) e bastoncino in basso a destra. Supersampling 4x4 per l'antialias.
func lollipopImage(base color.RGBA, n int) *image.RGBA {
	const ss = 4
	u := float64(n) / 32                 // unita' della griglia 32
	cx, cy, radius := 13*u, 13*u, 11.5*u // testa
	spacing, width := 3.8*u, 1.5*u       // vortice: distanza tra i giri e spessore
	swirl, stick := color.RGBA{255, 255, 255, 255}, color.RGBA{232, 224, 208, 255}
	b := spacing / (2 * math.Pi)
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			var sr, sg, sb, sa float64
			for sy := range ss {
				for sx := range ss {
					px, py := float64(x)+(float64(sx)+0.5)/ss, float64(y)+(float64(sy)+0.5)/ss
					dx, dy := px-cx, py-cy
					r := math.Hypot(dx, dy)
					var c *color.RGBA
					if r <= radius {
						c = &base
						phi := math.Atan2(dy, dx)
						if phi < 0 {
							phi += 2 * math.Pi
						}
						k := math.Round((r/b - phi) / (2 * math.Pi)) // giro della spirale piu' vicino
						if math.Abs(r-b*(phi+2*math.Pi*k)) < width/2 && r < radius-1.2*u {
							c = &swirl
						}
					} else if t := math.Max(0, math.Min(1, (px+py-38*u)/(20*u))); math.Hypot(px-(19+10*t)*u, py-(19+10*t)*u) < 1.75*u {
						c = &stick // segmento da (19,19) a (29,29)
					}
					if c != nil {
						sr, sg, sb, sa = sr+float64(c.R), sg+float64(c.G), sb+float64(c.B), sa+1
					}
				}
			}
			const k = ss * ss // colori premoltiplicati: media semplice dei campioni
			img.SetRGBA(x, y, color.RGBA{uint8(sr / k), uint8(sg / k), uint8(sb / k), uint8(255 * sa / k)})
		}
	}
	return img
}
