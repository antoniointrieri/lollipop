package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type position struct{ Right, Top int }

type settings struct {
	Lang           string    `json:"lang"`          // "auto" | "en" | "it"
	Pos            *position `json:"pos,omitempty"` // right and top edge: the window grows to the left
	Shape          string    `json:"shape"`         // "rect" | "pill"
	Marker         string    `json:"marker"`        // "dot" | "lollipop"
	BlinkMs        int       `json:"blinkMs"`
	Scale          int       `json:"scale"` // percent
	Compact        bool      `json:"compact"`
	Order          string    `json:"order"` // "alpha" | "recent"
	Side           string    `json:"side"`  // where the most important entries go: "right" | "left"
	GroupByState   bool      `json:"groupByState"`
	DoneMax        int       `json:"doneMax"` // idle entries outside the "⋯"; -1: all
	AlwaysOnTop    bool      `json:"alwaysOnTop"`
	ShowWindow     bool      `json:"showWindow"`
	ClaudePrompted bool      `json:"claudePrompted"` // first-run question about the Claude Code hook already asked
}

func settingsFile() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "lollipop", "settings.json")
}

func loadSettings() settings {
	s := settings{Lang: "auto", Shape: "rect", Marker: "dot", BlinkMs: 500, Scale: 100, Order: "alpha", Side: "right", DoneMax: -1, AlwaysOnTop: true, ShowWindow: true}
	if raw, err := os.ReadFile(settingsFile()); err == nil {
		_ = json.Unmarshal(raw, &s)
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
	above    int  // height of the "⋯" list open above the bar, which stays still on screen
	width    int  // as set by place: Bounds reports a few pixels more, and the right edge would creep
	idle     bool // no agents and no error: nothing to show, only the tray icon stays
	trayLast string

	visMu sync.Mutex // serializes Show/Hide; never taken on the UI thread
	shown bool

	checks      []check // checkable items of every menu, re-synced on each change
	menus       []*application.Menu
	settingsWin *application.WebviewWindow // created on first open
}

type check struct {
	item *application.MenuItem
	on   func(settings) bool
}

var uiLang atomic.Value // "en" | "it"

// tr returns the text in the active UI language.
func tr(en, it string) string {
	if uiLang.Load() == "it" {
		return it
	}
	return en
}

func resolveLang(setting string) string {
	if setting == "en" || setting == "it" {
		return setting
	}
	return osLanguage()
}

func (u *ui) get() settings {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.s
}

func toggle(u *ui, m *application.Menu, label string, field func(*settings) *bool) {
	on := func(s settings) bool { return *field(&s) }
	item := m.AddCheckbox(label, on(u.s)).OnClick(func(*application.Context) { u.change(func(s *settings) { *field(s) = !*field(s) }) })
	u.checks = append(u.checks, check{item, on})
}

// buildMenus (re)creates the window and tray menus, e.g. after a language change.
// They are separate objects: no shared native handles.
func (u *ui) buildMenus() {
	u.checks, u.menus = nil, nil
	ctx := u.app.ContextMenu.New()
	u.buildMenu(ctx.Menu)
	u.app.ContextMenu.Add("main", ctx)
	u.tray.SetMenu(u.buildMenu(application.NewMenu()))
}

// buildMenu holds only what is needed often; everything else is in the settings window.
func (u *ui) buildMenu(m *application.Menu) *application.Menu {
	m.Add(tr("Settings…", "Impostazioni…")).OnClick(func(*application.Context) { go u.openSettings() })
	toggle(u, m, tr("Show window", "Mostra finestra"), func(s *settings) *bool { return &s.ShowWindow })
	m.AddSeparator()
	m.Add(tr("Quit", "Esci")).OnClick(func(*application.Context) {
		u.save()
		u.app.Quit()
	})
	u.menus = append(u.menus, m)
	return m
}

// settingKeys are the settings.json fields the settings window may change.
var settingKeys = map[string]bool{"lang": true, "shape": true, "marker": true, "blinkMs": true, "scale": true,
	"compact": true, "order": true, "side": true, "groupByState": true, "doneMax": true, "alwaysOnTop": true, "showWindow": true}

// set changes one setting by its JSON name, as sent by the settings window.
func (u *ui) set(key string, value any) {
	raw, err := json.Marshal(map[string]any{key: value})
	if !settingKeys[key] || err != nil {
		return
	}
	u.change(func(s *settings) {
		_ = json.Unmarshal(raw, s)
		s.Scale, s.BlinkMs = max(50, min(s.Scale, 200)), max(100, min(s.BlinkMs, 3000)) // sliders: 70-160%, 200-1500 ms
		s.DoneMax = max(-1, s.DoneMax)
	})
}

// openSettings shows the settings window, creating it the first time. Closing it only hides it.
func (u *ui) openSettings() {
	u.mu.Lock()
	w := u.settingsWin
	u.mu.Unlock()
	if w == nil {
		w = u.app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name: "settings", Title: settingsTitle(), URL: "/settings.html",
			Width: 760, Height: 540, MinWidth: 620, MinHeight: 420,
			BackgroundType:             application.BackgroundTypeTranslucent, // Mica on Windows 11, vibrancy on macOS
			Windows:                    application.WindowsWindow{BackdropType: application.Mica, Theme: application.SystemDefault},
			Mac:                        application.MacWindow{Backdrop: application.MacBackdropTranslucent},
			DefaultContextMenuDisabled: true,
		})
		w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			e.Cancel()
			go w.Hide()
		})
		u.mu.Lock()
		u.settingsWin = w
		u.mu.Unlock()
	}
	w.Show()
	w.Focus()
}

func settingsTitle() string { return tr("lollipop settings", "Impostazioni di lollipop") }

func (u *ui) change(set func(*settings)) {
	u.mu.Lock()
	set(&u.s)
	s := u.s
	u.mu.Unlock()
	if l := resolveLang(s.Lang); l != uiLang.Load() {
		uiLang.Store(l)
		u.buildMenus()
		u.mu.Lock()
		w := u.settingsWin
		u.mu.Unlock()
		if w != nil {
			w.SetTitle(settingsTitle())
		}
	} else {
		for _, c := range u.checks {
			c.item.SetChecked(c.on(s))
		}
		for _, m := range u.menus {
			m.Update()
		}
	}
	u.apply(s)
	u.save()
}

// apply handles window-level settings; the frontend applies the visual ones.
func (u *ui) apply(s settings) {
	u.win.SetAlwaysOnTop(s.AlwaysOnTop)
	u.syncVisible()
	u.emitSettings(s)
}

// syncVisible shows the window when "Show window" is on, it has been placed and there is something to show.
func (u *ui) syncVisible() {
	u.visMu.Lock()
	defer u.visMu.Unlock()
	u.mu.Lock()
	want := u.s.ShowWindow && u.placed && !u.idle
	u.mu.Unlock()
	if want == u.shown {
		return
	}
	u.shown = want
	if want {
		u.win.Show()
	} else {
		u.win.Hide()
	}
}

// setIdle hides the window right away when the last agent goes; showing it waits for the frontend to report
// the new size (place), so the window never appears with the old content.
func (u *ui) setIdle(idle bool) {
	u.mu.Lock()
	u.idle = idle
	u.mu.Unlock()
	if idle {
		u.syncVisible()
	}
}

// emitSettings sends the settings to the frontend together with the resolved UI language.
func (u *ui) emitSettings(s settings) {
	u.app.Event.Emit("settings", struct {
		settings
		UILang string `json:"uiLang"`
	}{s, uiLang.Load().(string)})
}

// Never call window methods while holding u.mu: they wait for the UI thread, which may be waiting for u.mu.
func (u *ui) save() {
	var pos *position
	if u.isPlaced() {
		b := u.win.Bounds()
		pos = &position{Right: b.X, Top: b.Y}
	}
	u.mu.Lock()
	if pos != nil {
		pos.Right += u.width
		pos.Top += u.above
		u.s.Pos = pos
	}
	raw, _ := json.Marshal(u.s)
	u.mu.Unlock()
	if os.MkdirAll(filepath.Dir(settingsFile()), 0o755) == nil {
		_ = os.WriteFile(settingsFile(), raw, 0o644)
	}
}

// place resizes the window to its content keeping the right edge fixed, clamped to its screen, and the bar row
// (height bar) still: with up the "⋯" list opens above it. The first time it restores the saved position (or top
// right of the primary screen). Callers serialize it.
func (u *ui) place(w, h, bar int, up bool) {
	s, first := u.get(), !u.isPlaced()
	b := u.win.Bounds()
	u.mu.Lock()
	right, top := b.X+u.width, b.Y+u.above
	u.mu.Unlock()
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
	above, width := 0, b.Width
	if up && h > bar {
		above = h - bar
	}
	if scr != nil {
		right = min(right, scr.WorkArea.X+scr.WorkArea.Width)
		u.win.SetBounds(application.Rect{X: max(scr.WorkArea.X, right-w), Y: top - above, Width: w, Height: h})
		width = w
	} else {
		above = 0
	}
	u.mu.Lock()
	u.above, u.width = above, width
	u.placed = true
	u.mu.Unlock()
	u.syncVisible()
}

func (u *ui) isPlaced() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.placed
}

func (u *ui) setupTray() {
	u.tray = u.app.SystemTray.New()
	trayIcons = map[string][]byte{}
	for name, c := range map[string]color.RGBA{"red": {255, 0, 0, 255}, "green": {50, 205, 50, 255},
		"blue": {0, 191, 255, 255}, "yellow": {230, 176, 0, 255}, "gray": {105, 105, 105, 255}} {
		trayIcons[name] = lollipopPNG(c)
	}
	u.tray.SetIcon(trayIcons["gray"])
	u.tray.SetTooltip("lollipop")
	u.tray.OnClick(func() {
		go func() { // the handler runs on the UI thread
			u.change(func(s *settings) { s.ShowWindow = true })
			u.visMu.Lock()
			shown := u.shown // with no agents the window stays hidden
			u.visMu.Unlock()
			if shown {
				u.win.Focus()
			}
		}()
	})
}

func (u *ui) updateTray(items []item, errMsg string) {
	u.setIdle(len(items) == 0 && errMsg == "")
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

var trayIcons map[string][]byte // built in setupTray, not at init: "lollipop hook" must start fast

func lollipopPNG(base color.RGBA) []byte { return lollipopPNGSize(base, 32) }

func lollipopPNGSize(base color.RGBA, n int) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, lollipopImage(base, n))
	return buf.Bytes()
}

// lollipopImage draws on a 32-unit grid scaled to n: head with a white Archimedean spiral, stick at the
// bottom right. 4x4 supersampling for antialiasing.
func lollipopImage(base color.RGBA, n int) *image.RGBA {
	const ss = 4
	u := float64(n) / 32
	cx, cy, radius := 13*u, 13*u, 11.5*u
	spacing, width := 3.8*u, 1.5*u // spiral: distance between turns, stroke width
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
						k := math.Round((r/b - phi) / (2 * math.Pi)) // nearest spiral turn
						if math.Abs(r-b*(phi+2*math.Pi*k)) < width/2 && r < radius-1.2*u {
							c = &swirl
						}
					} else if t := math.Max(0, math.Min(1, (px+py-38*u)/(20*u))); math.Hypot(px-(19+10*t)*u, py-(19+10*t)*u) < 1.75*u {
						c = &stick // segment (19,19)-(29,29)
					}
					if c != nil {
						sr, sg, sb, sa = sr+float64(c.R), sg+float64(c.G), sb+float64(c.B), sa+1
					}
				}
			}
			const k = ss * ss // premultiplied colors: plain average of the samples
			img.SetRGBA(x, y, color.RGBA{uint8(sr / k), uint8(sg / k), uint8(sb / k), uint8(255 * sa / k)})
		}
	}
	return img
}

func (st hooksStatus) label() string {
	switch st {
	case hooksOK:
		return tr("active", "attiva")
	case hooksBroken:
		return tr("needs repair", "da riparare")
	case hooksInvalid:
		return tr("invalid settings.json", "settings.json non valido")
	}
	return tr("not installed", "non installata")
}

func (st hooksStatus) code() string {
	switch st {
	case hooksOK:
		return "ok"
	case hooksBroken:
		return "broken"
	case hooksInvalid:
		return "invalid"
	}
	return "absent"
}

// emitStatus sends the settings window what lives outside settings.json: Claude Code hook, login entry, version.
func (u *ui) emitStatus() {
	on, _ := autostartState()
	light, dark := accentColors()
	u.app.Event.Emit("status", map[string]any{"claude": claudeHooksStatus().code(), "claudeSettings": claudeSettingsPath(),
		"autostart": on, "version": appVersion(), "translucent": translucentBackdrop(), "accent": []string{light, dark}})
}

// appVersion is the tag stamped by go build (v0.4.0), or a pseudo-version for untagged commits.
func appVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// claudeRun runs an action of the settings window's Claude Code page: "install" (or repair) or "remove".
func (u *ui) claudeRun(action string) {
	switch {
	case action == "remove":
		u.claudeAction(removeClaudeHooks, tr("Claude Code integration removed.", "Integrazione con Claude Code rimossa."))
	case action == "install" && claudeHooksStatus() == hooksAbsent:
		u.claudeAction(installClaudeHooks, tr(
			"Claude Code integration installed. Sessions that were already open may need a restart to show up.",
			"Integrazione con Claude Code installata. Le sessioni già aperte potrebbero dover essere riavviate per comparire."))
	case action == "install":
		u.claudeAction(installClaudeHooks, tr("Claude Code integration repaired.", "Integrazione con Claude Code riparata."))
	}
}

func (u *ui) claudeAction(fn func() error, done string) {
	if err := fn(); err != nil {
		u.app.Dialog.Error().SetTitle("lollipop").SetMessage(err.Error()).Show()
	} else {
		u.app.Dialog.Info().SetTitle("lollipop").SetMessage(done).Show()
	}
	u.emitStatus()
}

// claudeStartup silently repairs an installed hook (e.g. the exe was moved) and asks once whether to install it.
func (u *ui) claudeStartup() {
	defer u.emitStatus()
	switch claudeHooksStatus() {
	case hooksBroken:
		_ = installClaudeHooks()
		return
	case hooksAbsent:
	default:
		return
	}
	if _, err := os.Stat(claudeConfigDir()); err != nil || u.get().ClaudePrompted {
		return
	}
	d := u.app.Dialog.Question().SetTitle("lollipop").SetMessage(tr(
		"Show Claude Code sessions running outside Orca too?\n\nlollipop adds a hook to your Claude Code user settings ("+
			claudeSettingsPath()+"), with a backup. You can remove it any time from the Claude Code menu.",
		"Mostrare anche le sessioni di Claude Code fuori da Orca?\n\nlollipop aggiunge un hook alle impostazioni utente di Claude Code ("+
			claudeSettingsPath()+"), con un backup. Puoi rimuoverlo quando vuoi dal menu Claude Code."))
	// Windows shows a system Yes/No box (localized by Windows) and matches the pressed button by these English labels.
	yesLabel, noLabel := "Yes", "No"
	if runtime.GOOS != "windows" {
		yesLabel, noLabel = tr("Yes", "Sì"), tr("No", "No")
	}
	yes, no := d.AddButton(yesLabel), d.AddButton(noLabel)
	yes.OnClick(func() {
		u.change(func(s *settings) { s.ClaudePrompted = true })
		u.claudeAction(installClaudeHooks, tr("Claude Code integration installed.", "Integrazione con Claude Code installata."))
	})
	no.OnClick(func() { u.change(func(s *settings) { s.ClaudePrompted = true }) })
	d.SetDefaultButton(yes).SetCancelButton(no).Show()
}

func (u *ui) setAutostart(on bool) {
	if err := setAutostart(on); err != nil {
		u.app.Dialog.Error().SetTitle("lollipop").SetMessage(err.Error()).Show()
	}
	u.emitStatus()
}

// autostartStartup points an existing entry to this exe, e.g. after it was moved.
func (u *ui) autostartStartup() {
	if on, current := autostartState(); on && !current {
		_ = setAutostart(true)
	}
}
