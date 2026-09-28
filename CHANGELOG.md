# Changelog

Notable changes to lollipop. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the project uses [Semantic Versioning](https://semver.org/). The section of each version is also used as the
text of its GitHub Release.

## [Unreleased]

## [0.5.0] - 2026-09-28

### Added

- Clicking a Claude Code session of the **VS Code extension** or of the **Claude app** (Code tab) opens its own
  tab, besides bringing the window to the front. VS Code extension sessions started with an older lollipop get
  it from their next event. Tested on Windows; on macOS it should work the same way but is untested.

## [0.4.0] - 2026-09-28

### Added

- **Settings window** in the style of the OS (Mica on Windows 11, vibrancy on macOS), with pages: Appearance,
  Entries, General, Integrations (Claude Code), About. The right-click menu keeps only **Settings…**, **Show window** and
  **Quit**.
- **Order**: entries in alphabetical order or by last activity (most recent state change first); **Most
  important entries** on the right (default) or on the left; **Group by state**: from the most important, agents
  waiting for you, just finished, working, then the finished ones already seen.
- **Collapse idle agents** (finished and already seen): past the chosen number they go behind a `⋯ N` entry, which lists
  them vertically on mouseover (upward in the lower half of the screen). Off by default.
- While the mouse is over the window, entries keep their place; the new order applies when it leaves.
- **Update check**: once a day lollipop asks GitHub for the latest release and, when a newer one is out, says so in
  the menu, the tray tooltip and the About page, with a link to the release. It downloads nothing. It can be turned
  off in General.

### Fixed

- A crash at startup when the Claude Code and login checks updated the menus together: `concurrent map writes` on
  Windows, a segmentation fault on macOS, often on every start after the first. Menu changes are now serialized.
- The right edge of the window crept a few pixels to the right at every resize.

## [0.3.0] - 2026-09-28

### Added

- **Start at login** menu item: a per-user `Run` registry entry on Windows, a LaunchAgent on macOS. An entry
  disabled from Task Manager shows as off, and ticking the item enables it again. If the exe is moved, the
  entry is fixed on the next start.
- **Indicator** menu: entries can show a small lollipop head with a white spiral instead of the dot.

### Changed

- With no agents the window disappears and only the tray icon stays; it comes back with the first agent. A
  protocol error from Orca still shows the red dot.
- The yellow of the tray icon (and of the new lollipop indicator) is slightly darker, so the white spiral stays
  visible.
- The Windows executable carries version information (name, description, version) and an application manifest,
  and is no longer stripped of debug symbols. Both changes aim at fewer false positives from heuristic antivirus
  detections such as `Trojan:Win32/Wacatac.B!ml`.
- Release binaries come with a build provenance attestation: `gh attestation verify lollipop.exe --repo
  antoniointrieri/lollipop` checks that they were built by this repository's CI.

## [0.2.1] - 2026-09-25

### Added

- The tooltip of a plain Claude Code session shows the conversation title.

### Fixed

- Clicking a Claude Code session running in Warp (or any terminal using ConPTY without owning its console
  window) now brings the terminal to the front instead of doing nothing. Existing sessions pick up the fix on
  their next event.

## [0.2.0] - 2026-09-25

### Added

- **Plain Claude Code sessions**: sessions started outside Orca (console, Windows Terminal, VS Code or IntelliJ
  terminal, ...) show up next to the Orca agents. lollipop finds the hosting window by itself, and clicking the
  entry brings it to the front. Sessions that are closed or killed disappear on their own.
- **Claude Code integration menu**: on first start lollipop offers to install a background hook in the Claude
  Code user settings, with a backup and without touching other hooks. The **Claude Code** submenu shows the
  status and lets you install, repair or remove it. If the exe is moved, the path is fixed on the next start.
- **English and Italian UI**, following the OS language, with a **Language / Lingua** menu to switch it.

### Changed

- A closed Orca is no longer shown as an error, since lollipop is now useful with Claude Code alone.

## [0.1.0] - 2026-09-25

First release: a Go + Wails v3 port of the PowerShell proof of concept.

### Added

- Always-on-top, frameless window with one entry per Orca agent, colored by state (working, done,
  background monitoring, waiting for you).
- Blinking when an agent has just finished and you haven't looked at it; click an entry to open Orca right on
  that agent's terminal.
- Tray icon in the color of the most urgent agent, with a summary tooltip.
- Settings from the right-click menu: shape (rectangle or pill), blinking speed, size, compact view, always on
  top, show window. Window position and settings are remembered.
- Windows build and experimental macOS build (universal binary) from GitHub Actions.

[Unreleased]: https://github.com/antoniointrieri/lollipop/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/antoniointrieri/lollipop/releases/tag/v0.5.0
[0.4.0]: https://github.com/antoniointrieri/lollipop/releases/tag/v0.4.0
[0.3.0]: https://github.com/antoniointrieri/lollipop/releases/tag/v0.3.0
[0.2.1]: https://github.com/antoniointrieri/lollipop/releases/tag/v0.2.1
[0.2.0]: https://github.com/antoniointrieri/lollipop/releases/tag/v0.2.0
[0.1.0]: https://github.com/antoniointrieri/lollipop/releases/tag/v0.1.0
