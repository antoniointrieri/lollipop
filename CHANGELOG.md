# Changelog

Notable changes to lollipop. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the project uses [Semantic Versioning](https://semver.org/). The section of each version is also used as the
text of its GitHub Release.

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

[0.2.0]: https://github.com/antoniointrieri/lollipop/releases/tag/v0.2.0
[0.1.0]: https://github.com/antoniointrieri/lollipop/releases/tag/v0.1.0
