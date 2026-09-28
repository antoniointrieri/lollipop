# Contributing

## Layout

```
main.go               startup, polling loop, Go <-> frontend events
orca.go               client for Orca's runtime
claude.go             plain Claude Code sessions: hook, session files, settings.json install
state.go              entries, blinking, tray summary (pure logic, tested)
ui.go                 settings, menus, tray, window position, icon drawing
platform_*.go         Windows- and macOS-specific code
frontend/index.html   the window (HTML/CSS/JS, no build step)
docs/spec.md          specification: behavior and decisions (Italian)
```

To build, see [Build from source](README.md#build-from-source).

## Guidelines

- Tests: `go test ./...`
- Notable changes go in [CHANGELOG.md](CHANGELOG.md), in English; the section of a version becomes its Release
  notes.
- CI (`.github/workflows/build.yml`) tests and builds Windows and macOS on every push; pushing a `v*` tag also
  publishes a GitHub Release with both binaries and a build provenance attestation for them.
- The executable icon (`lollipop.ico`, `rsrc_windows_amd64.syso`) is drawn by the code in `ui.go`. If you change
  the drawing, regenerate it with `go generate`. The same `.syso` holds the manifest and the version info, which
  CI regenerates from the git tag on every build.
