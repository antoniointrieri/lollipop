# lollipop

Semaforo per gli agenti di Orca: finestrella sempre in primo piano, una voce per agente
(giallo = working, verde = done, blu = monitor in background, rosso = attesa input/permesso).
Lampeggia quando un agente ha appena finito; clic sulla voce = Orca su quel terminale.
Tasto destro (sulla finestra o sull'icona nella traybar) = impostazioni (forma, lampeggio, dimensione, compatta,
sempre in primo piano, mostra finestra) ed Esci. L'icona nella traybar ha il colore dell'agente più urgente.
Dettagli in `docs/spec.md`.

## Build

- Windows: `go build -ldflags "-H=windowsgui" -o lollipop.exe .`
- macOS (non ancora provato): `go build -o lollipop .` su un Mac con Xcode Command Line Tools.

Test: `go test ./...`

Icona dell'exe: `lollipop.ico` e `rsrc_windows_amd64.syso` sono nel repo e disegnati dal codice di `ui.go`.
Se cambi il disegno, rigenerali con `go generate` (il test `TestAppIcon` fallisce finché non lo fai).

## Uso

- Debug: `lollipop -once` stampa agenti e pannello attivo ed esce (su Windows: `lollipop.exe -once | more`).
  Sul Mac è il primo comando da provare.
- Avvio automatico: collegamento a `lollipop.exe` in `shell:startup` (Windows) o Elementi di login (macOS).
- Impostazioni e posizione stanno in `<UserConfigDir>/lollipop/settings.json` (`%APPDATA%\lollipop` su Windows).
