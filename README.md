<p align="center">
  <img src="docs/img/icon.png" width="96" alt="">
</p>

<h1 align="center">lollipop</h1>

<p align="center">
  A traffic light for the AI agents running in <b>Orca</b>: a small always-on-top window that tells you,
  at a glance, which agent is working, which one is done and which one is waiting for you.
</p>

<p align="center">
  <img src="docs/img/pill.png" width="614" alt="The lollipop window showing five agents in different states">
</p>

<p align="center"><a href="README.it.md">Leggi in italiano</a></p>

The name comes from the British *lollipop man*: the crossing guard with the round sign who tells each person
when it's their turn.

## Features

- **One entry per agent** with an open terminal in Orca, colored by state:

  | Color | State |
  |---|---|
  | 🟡 yellow | working (`working`) |
  | 🟢 green | done (`done`) |
  | 🔵 blue | turn finished, but background shells or monitors are still running |
  | 🔴 red | waiting for you: input or a permission (`waiting`, `blocked`) |

- **Blinks** when an agent has just finished and you haven't looked at it yet. It stops when you click the entry,
  open that pane in Orca, or the agent starts working again. If you were already looking at the agent, it doesn't
  blink.
- **Click an entry** to bring Orca to the front, right on that agent's terminal.
- **Tray icon** in the color of the most urgent agent, with a summary in its tooltip
  (`lollipop — 1 in attesa, 1 done, 2 working`). You can hide the window and keep only the icon.
- **Stays out of the way**: grows to the left keeping its right edge fixed, stays above the taskbar, works across
  multiple monitors and remembers where you left it.

<p align="center">
  <img src="docs/img/compact.png" width="188" alt="Compact view: dots only">
  &nbsp;&nbsp;&nbsp;&nbsp;
  <img src="docs/img/tray.png" width="200" alt="Tray icons in the five colors">
</p>
<p align="center"><sub>Compact view and tray icons</sub></p>

> The UI (menu, tooltips) is in Italian.

## Requirements

- **Windows 10/11** with WebView2 (preinstalled on Windows 11 and on up-to-date Windows 10).
- **Orca** running on the same machine (tested with Orca 1.4.211).
- **macOS 11+**: experimental, see [Limitations](#limitations).

## Install

### Download

Get the latest build from [Releases](https://github.com/antoniointrieri/lollipop/releases), or the build of any
commit from the [Actions](https://github.com/antoniointrieri/lollipop/actions) page.

- **Windows**: `lollipop.exe`. The executable isn't signed, so the first time SmartScreen may say "Windows
  protected your PC": click *More info* → *Run anyway*.
- **macOS**: `lollipop-macos.tar.gz` (universal binary, Apple Silicon and Intel). It isn't signed either, so
  remove the quarantine flag before running it:

  ```sh
  tar -xzf lollipop-macos.tar.gz
  xattr -d com.apple.quarantine lollipop
  ./lollipop
  ```

### Build from source

Requires **Go 1.26** or later. No Node or npm needed.

```sh
git clone https://github.com/antoniointrieri/lollipop.git
cd lollipop
go build -ldflags "-H=windowsgui" -o lollipop.exe .   # Windows
go build -o lollipop .                               # macOS (needs the Xcode Command Line Tools)
```

To start it with Windows, put a shortcut to `lollipop.exe` in `shell:startup` (Win+R → `shell:startup`); on macOS
add it to *Login Items*.

## Usage

| Action | Effect |
|---|---|
| Click an entry | opens Orca on the agent's terminal |
| Drag the `⋮` handle | moves the window |
| Right-click (window or tray icon) | settings menu and **Esci** (quit) |
| Click the tray icon | shows the window and brings it to the front |

Settings, available from the menu, apply immediately and are saved:

- **Forma** (shape): rectangle or pill
- **Lampeggio** (blink): slow, normal or fast
- **Dimensione** (size): small, normal or large
- **Compatta** (compact): dots only, with the name in the tooltip
- **Sempre in primo piano** (always on top) and **Mostra finestra** (show window)

Settings and window position are stored in `settings.json` inside `%APPDATA%\lollipop` (Windows) or
`~/Library/Application Support/lollipop` (macOS). Alt+F4 hides the window instead of closing the app: use
**Esci** to quit.

### Troubleshooting

A red dot with "Errore" in the tooltip means lollipop can't talk to Orca (for example because Orca is closed);
it keeps retrying every half second. To see what it reads from Orca:

```sh
lollipop.exe -once | more     # Windows
./lollipop -once              # macOS
```

It prints agents, states and the active pane, then exits.

## How it works

lollipop talks to Orca's local runtime through the same named pipe the `orca` CLI uses (a Unix socket on macOS),
reading the address and token from `orca-runtime.json` in Orca's data folder. Every 500 ms it asks Orca for the
worktrees with their agents and for the list of terminals; on click it asks Orca to focus the terminal. It
changes nothing else in Orca, and the token is never written anywhere.

> [!WARNING]
> The pipe protocol is **undocumented**: it was worked out by observing Orca. An Orca update could break it; if
> that happens, lollipop shows the red dot with the error.

The UI is built with [Wails v3](https://github.com/wailsapp/wails): a Go backend and an HTML page embedded in the
binary.

## Limitations

- **macOS is untested**: `platform_darwin.go` is written from Orca's source and compiles in CI, but it has never
  been run. The Orca data folder (`~/Library/Application Support/orca`) and the socket transport are educated
  guesses. If you have a Mac, run `./lollipop -once` and report what happens.
- If you split Orca tabs into several side-by-side groups, active-pane detection only looks at the main group.
- Only local terminals are shown, not those of remote hosts.
- Wails v3 is still in beta: the version is pinned in `go.mod`.

## Development

```
main.go               startup, polling loop, Go <-> frontend events
orca.go               client for Orca's runtime
state.go              entries, blinking, tray summary (pure logic, tested)
ui.go                 settings, menus, tray, window position, icon drawing
platform_*.go         Windows- and macOS-specific code
frontend/index.html   the window (HTML/CSS/JS, no build step)
docs/spec.md          specification: behavior and decisions (Italian)
```

- Tests: `go test ./...`
- CI (`.github/workflows/build.yml`) tests and builds Windows and macOS on every push; pushing a `v*` tag also
  publishes a GitHub Release with both binaries.
- The executable icon (`lollipop.ico`, `rsrc_windows_amd64.syso`) is drawn by the code in `ui.go`. If you change
  the drawing, regenerate it with `go generate`; `TestAppIcon` fails until you do.

## License

[MIT](LICENSE)
