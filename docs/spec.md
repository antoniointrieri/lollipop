# lollipop — specifica v1

> Stato: **approvata** (2026-09-25). Deriva da `docs/brief.md` (comportamento concordato, protocollo Orca) e dal
> brainstorming del 2026-09-25. Dove questa spec tace, vale il comportamento del POC `orca-semaforo.ps1`.

## 1. Decisioni

| Tema | Decisione |
|---|---|
| Piattaforme | Windows (verificato) + macOS (**best effort alla cieca**: scritto, mai compilato né provato finché non c'è un Mac) |
| UI | **Wails v3**, versione fissata a `v3.0.0-beta.25`. Frontend HTML/JS statico incorporato con `embed`: niente npm, niente CLI `wails3`, niente generazione di binding |
| Distribuzione | Solo uso personale: `go build` locale, niente installer, firma o CI |
| Extra | Solo **memoria della posizione**. Avvio automatico e suono: no |
| Split di Orca | Parità col POC: si legge solo il gruppo radice del layout |
| Host remoti | Solo terminali locali (come il POC) |

## 2. Comportamento

Parità con il POC, cioè con la sezione "Comportamento concordato" del brief (finestra, voci, colori, lampeggio,
clic, aggiornamento ogni 500 ms fuori dal thread UI, nessuna console). In più:

- **Memoria della posizione**: all'uscita (menu "Esci") salva il bordo destro e il bordo superiore della finestra
  in `<UserConfigDir>/lollipop/position.json`. All'avvio la finestra riparte lì, se quel punto cade dentro uno
  schermo esistente; altrimenti riparte come il POC (in alto a destra sullo schermo principale, 80 px dal bordo
  destro e 40 px dall'alto).
- **Tolto rispetto al POC**: il ripiego su `orca terminal switch` quando `terminal.focus` fallisce. La CLI parla con
  lo stesso runtime: se la pipe non risponde, fallisce anche lei (dopo circa 1 s).

## 3. Architettura

Un solo package `main` e pochi file. Il codice di piattaforma sta dietro build tag.

```
main.go               app Wails, finestra, loop di polling, eventi, posizione
orca.go               client del runtime: legge orca-runtime.json, richiesta/risposta JSON a righe, riconnessione, poll()
state.go              logica pura: lampeggio, etichette, colori, tooltip
state_test.go         test della logica di state.go
platform_windows.go   dial della named pipe, Orca in primo piano?, attiva Orca, riafferma topmost
platform_darwin.go    dial del socket unix, Orca in primo piano?, attiva Orca (cgo + Cocoa)
frontend/index.html   rendering, lampeggio, clic, misura della dimensione (JS inline)
```

### 3.1 Parlare con Orca (`orca.go`)

- Cartella dati: `filepath.Join(os.UserConfigDir(), "orca")`, cioè `%APPDATA%\orca` su Windows e
  `~/Library/Application Support/orca` su macOS (ipotesi: è la cartella `userData` di Electron, la stessa logica
  che su Windows dà `%APPDATA%\orca`).
- Trasporto, dal campo `transports[]` di `orca-runtime.json`:
  - `kind:"named-pipe"` → `winio.DialPipe` (`github.com/Microsoft/go-winio`, già presente tra le dipendenze di Wails);
  - `kind:"unix"` → `net.Dial("unix", endpoint)`. L'ho trovato nel bundle di Orca: su piattaforme non win32 crea
    `<userData>/o-<pid>-<id>.sock`.
- Il protocollo è quello del brief: una riga JSON per richiesta e una per risposta, su una connessione tenuta
  aperta. Ogni richiesta ha una **deadline di 2 s**; in caso di errore o timeout la connessione viene chiusa e alla
  richiesta successiva si rilegge `orca-runtime.json`.
- Le richieste sono serializzate da un mutex (poll e `terminal.focus` condividono la connessione).
- **Il token non va mai loggato, stampato o incluso nei messaggi d'errore.**
- `poll()` = `worktree.ps` + `terminal.list` + `agent-hooks/last-status.json`, con la stessa logica del POC.
  Restituisce `{agents[], focused, orcaPid}`; `orcaPid` viene da `orca-runtime.json`.

### 3.2 Stato e lampeggio (`state.go`)

Una funzione pura prende lo stato precedente, il nuovo poll e il flag "Orca è in primo piano", e produce le voci
da mostrare (chiave, etichetta, stato, tooltip, lampeggia sì/no). Le regole sono quelle del POC: si comincia a
lampeggiare sulla transizione `working` → altro; si smette se l'agente torna `working`, se l'utente clicca la voce,
o se Orca è in primo piano e ha quel pannello attivo. All'avvio non lampeggia nulla.
Il test `state_test.go` copre queste regole e la costruzione delle etichette (`repo` / `repo/displayName`, glifo
iniziale tolto dal titolo).

### 3.3 Loop e UI (`main.go`, `frontend/index.html`)

- Una goroutine esegue il poll ogni 500 ms, aggiorna lo stato ed emette l'evento `agents` verso il frontend
  **solo quando cambia qualcosa** (lo stesso concetto della "firma" del POC).
- Il rilevamento di "Orca in primo piano" gira solo quando serve, cioè quando il pannello attivo in Orca
  corrisponde a un agente che sta lampeggiando.
- Il frontend (JS puro, `import` da `/wails/runtime.js`):
  - disegna la maniglia `⋮` (`--wails-draggable: drag`) e, per ogni agente, pallino e nome;
  - il tooltip usa l'attributo `title`;
  - il lampeggio è un `setInterval` da 500 ms che alterna il colore dello stato con la sua versione al 40%;
  - al clic evidenzia la voce per 500 ms, ignora i clic ripetuti in quell'intervallo ed emette `focus` con la chiave;
  - dopo ogni rendering misura il contenuto ed emette `size` con larghezza e altezza.
- Su `size` il Go ridimensiona la finestra tenendo fermo il bordo destro, clampato alla `WorkArea` dello schermo
  su cui si trova (`window.GetScreen()`).
- Su `focus` il Go toglie il lampeggio e, in una goroutine, attiva Orca e chiama `terminal.focus`.
- Menu contestuale nativo di Wails v3 (`--custom-contextmenu`) con la sola voce "Esci".
- Opzioni della finestra: `Frameless`, `AlwaysOnTop`, `Windows.HiddenOnTaskbar`; su macOS
  `ActivationPolicyAccessory` (niente icona nel Dock).

### 3.4 Piattaforma

| Funzione | Windows | macOS (non verificato) |
|---|---|---|
| Orca in primo piano | `GetForegroundWindow` → pid, confrontato con `orcaPid` | `NSWorkspace.frontmostApplication.processIdentifier` confrontato con `orcaPid` |
| Attiva Orca | finestra visibile con titolo del processo `orcaPid` (`EnumWindows`); se minimizzata `ShowWindow(SW_RESTORE)`, poi `SetForegroundWindow` | `NSRunningApplication(pid).activateWithOptions` |
| Riafferma topmost | `SetWindowPos(HWND_TOPMOST, NOSIZE\|NOMOVE\|NOACTIVATE)` ogni 500 ms sul thread UI | niente (il livello floating non viene coperto dal Dock) |

Il confronto del pid sostituisce il controllo sul nome del processo fatto dal POC. È da verificare che il pid in
`orca-runtime.json` sia quello del processo che possiede la finestra (il processo main di Electron); se non lo è,
si torna al nome del processo.

## 4. Build ed esecuzione

- Windows: `go build -ldflags "-H=windowsgui" -o lollipop.exe .`, poi un collegamento per l'avvio come per il POC.
- macOS: `go build` su un Mac con Xcode Command Line Tools (cgo). Si ottiene un binario semplice, senza bundle `.app`.
- Debug: `lollipop -once` stampa agenti, stato e pannello attivo, poi esce, come `-Once` nel POC. Su Windows,
  essendo un binario GUI, va rediretto per vedere l'output (`lollipop.exe -once | more`). È anche il primo comando
  da lanciare sul Mac.
- README breve: build, avvio, avvio automatico fatto a mano (collegamento in Esecuzione automatica / Elementi di login).

## 5. Rischi e cose da verificare in implementazione

1. **Tooltip**: ci si aspetta che il tooltip nativo di WebView2 esca dai bordi della finestra, anche se è
   minuscola. Se non succede, la finestra va allargata temporaneamente durante l'hover.
2. **Coordinate e DPI** (schermo al 125%, multi-monitor): con Wails v3 `Position()`/`SetPosition()` sono DIP
   assoluti; va verificato sul secondo monitor.
3. **Topmost dopo un clic sulla taskbar**: si verifica che `SetWindowPos` sull'HWND di Wails (`NativeWindow()`)
   dia lo stesso risultato del POC.
4. **Wails beta**: la versione resta fissata; si aggiorna solo di proposito.
5. **macOS**: cartella dati, `kind:"unix"`, confronto del pid, `ActivationPolicyAccessory` e cgo sono tutte ipotesi.
   Primo collaudo sul Mac: `lollipop -once`.

## 6. Fuori scope v1

Eventi push da Orca (serve il websocket E2EE), gruppi split, host remoti, avvio automatico nel codice, suono,
installer, firma, CI, istanza unica, log su file.

## 7. v1.1 — impostazioni nel menu contestuale

> Stato: **approvata** (2026-09-25).

Il tasto destro apre il menu nativo con sottomenu a scelta singola; ogni scelta ha effetto subito, senza riavvio,
e viene salvata. Niente finestra di impostazioni.

```
Forma       ▸  ◉ Rettangolo   ○ Capsula
Lampeggio   ▸  ○ Lento (1 s)  ◉ Normale (500 ms)  ○ Veloce (250 ms)
Dimensione  ▸  ○ Piccola (85%)  ◉ Normale  ○ Grande (125%)
☐ Compatta
────────────
Esci
```

I valori predefiniti (◉) riproducono l'aspetto attuale.

- **Voci come bottoni**: ogni voce ha sempre uno sfondo grigio tenue (`#3a3a42`, più chiaro della barra `#202024`), con
  pallino e nome nel colore dello stato; al clic lo sfondo diventa `#5a5a6a` per 500 ms, come oggi.
  - Forma: rettangolo con angoli di 3 px, oppure capsula (angoli completamente arrotondati).
- **Lampeggio**: cambia solo il periodo dell'alternanza; le regole su quando si lampeggia restano quelle della §3.2.
- **Dimensione**: scala insieme testo, pallini, maniglia e spaziature (tutto espresso in `em`); la finestra
  si ridimensiona tenendo fermo il bordo destro, come sempre.
- **Compatta**: si vede solo il pallino, senza il nome del repo; il nome resta nel tooltip.
- Il polling resta a 500 ms: non è configurabile.

### Implementazione

- `settings.json` in `<UserConfigDir>/lollipop/` contiene posizione e impostazioni e sostituisce `position.json`
  (che viene ignorato: al massimo la finestra riparte una volta dalla posizione predefinita).
- Si salva a ogni cambio di impostazione e all'uscita (la posizione).
- Il Go costruisce il menu con le voci già spuntate secondo `settings.json` e a ogni scelta invia al frontend
  l'evento `settings`. Il frontend applica classi e variabili CSS e il periodo del lampeggio, ridisegna e invia di
  nuovo `size`. Al caricamento il frontend chiede le impostazioni insieme allo stato (evento `ready`).

## 8. v1.1 — sempre in primo piano e icona nella traybar

> Stato: **approvata** (2026-09-25).

Il menu della §7 diventa così; lo stesso menu si apre anche dal tasto destro sull'icona della traybar:

```
Forma / Lampeggio / Dimensione / ☐ Compatta     (come §7)
────────────
☑ Sempre in primo piano
☑ Mostra finestra
────────────
Esci
```

- **Sempre in primo piano** (predefinito: attivo). Da spento la finestra è normale: niente `AlwaysOnTop` e niente
  riaffermazione ogni 500 ms. Resta comunque fuori dalla taskbar, e la si ritrova dall'icona.
- **Mostra finestra** (predefinito: attivo). Da spento resta solo l'icona nella traybar.
- **Icona nella traybar**, sempre presente:
  - **forma e colore**: un lollipop stilizzato, con la testa nel colore dell'agente più urgente, un vortice bianco
    e un bastoncino in basso a destra. L'ordine è rosso (attesa input o permesso) > verde
    (done) > blu (monitoring) > giallo (working). È grigio senza agenti e rosso in caso di errore di connessione.
  - **tooltip**: il riepilogo, per esempio `lollipop — 2 working, 1 in attesa, 1 done`; con un errore mostra
    `Errore: …`.
  - **click sinistro**: mostra la finestra (se era nascosta riattiva "Mostra finestra") e la porta davanti.
  - **click destro**: apre il menu.
- Il colore e il tooltip dell'icona si aggiornano solo quando cambiano, come la finestra.
- Le icone (5 colori) si generano all'avvio in Go con `image/png` e non ci sono file di asset.
  Su macOS l'icona compare nella barra dei menu, a colori (non come template).
- `settings.json` contiene anche `alwaysOnTop` e `showWindow`.
- **Icona dell'exe** (Esplora risorse, collegamenti): lo stesso lollipop in rosso, in `lollipop.ico` (da 16 a 256 px)
  incorporato come risorsa Windows (`rsrc_windows_amd64.syso`, creato con `go-winres`). Entrambi i file sono nel
  repo e si rigenerano con `go generate`; `TestAppIcon` segnala se non corrispondono più al disegno. Su macOS
  non serve nulla, finché il binario non diventa un bundle `.app`.

### Implementazione

- Il menu della finestra e quello dell'icona si costruiscono con la stessa funzione. Sono due oggetti distinti,
  per non condividere gli handle nativi, e i segni di spunta vengono tenuti allineati tra i due.
- Poiché la finestra ora si può nascondere, si esce solo da "Esci": chiudere la finestra non termina l'app
  (su macOS `ApplicationShouldTerminateAfterLastWindowClosed: false`).
