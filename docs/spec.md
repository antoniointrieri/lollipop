# lollipop — specifica v1

> Stato: **approvata** (2026-09-25). Deriva da `docs/brief.md` (comportamento concordato, protocollo Orca) e dal
> brainstorming del 2026-09-25. Dove questa spec tace, vale il comportamento del POC `orca-semaforo.ps1`.

## 1. Decisioni

| Tema | Decisione |
|---|---|
| Piattaforme | Windows (verificato) + macOS (**best effort alla cieca**: scritto, mai compilato né provato finché non c'è un Mac) |
| UI | **Wails v3**, versione fissata a `v3.0.0-beta.25`. Frontend HTML/JS statico incorporato con `embed`: niente npm, niente CLI `wails3`, niente generazione di binding |
| Distribuzione | Solo uso personale: `go build` locale, niente installer, firma o CI |
| Extra | Solo **memoria della posizione**. Suono: no. Avvio automatico: aggiunto nella v0.3 (§11) |
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
  repo e si rigenerano con `go generate`. Il `.syso` contiene anche il manifest (DPI awareness per-monitor v2, la
  stessa che Wails imposterebbe a runtime; Common Controls v6) e le informazioni di versione (nome, descrizione,
  versione dal tag git), che la CI rigenera a ogni build: un exe senza metadati alza il punteggio degli
  antivirus euristici. Su macOS
  non serve nulla, finché il binario non diventa un bundle `.app`.

### Implementazione

- Il menu della finestra e quello dell'icona si costruiscono con la stessa funzione. Sono due oggetti distinti,
  per non condividere gli handle nativi, e i segni di spunta vengono tenuti allineati tra i due.
- Poiché la finestra ora si può nascondere, si esce solo da "Esci": chiudere la finestra non termina l'app
  (su macOS `ApplicationShouldTerminateAfterLastWindowClosed: false`).

## 9. v0.2 — sessioni Claude Code fuori da Orca

> Stato: **approvata** (2026-09-25).

Oltre agli agenti di Orca, lollipop mostra le sessioni di Claude Code avviate altrove: Windows Terminal, console,
terminale di VS Code o IntelliJ, e così via. Non serve nessuna configurazione per programma. Riferimenti:
[claudepulse-win](https://github.com/stantheman0128/claudepulse-win), [soundpad](https://github.com/davidef393s/soundpad)
e lo script hook di Orca stessa (`~/.orca/agent-hooks/claude-hook.cmd`).

### Comportamento

- **Voci**: sono mescolate a quelle di Orca nella stessa lista, ordinata per nome, con lo stesso stile. Il nome è la
  cartella della sessione (base di `cwd`). Il titolo della conversazione e la provenienza compaiono solo nel
  tooltip: `[stato] cartella` + `Security check con Aikido` + `Claude Code in Windows Terminal`. Il titolo è
  l'ultimo record `ai-title` negli ultimi 256 KB del transcript (formato interno di Claude Code: se cambia, si
  perde solo il titolo). Il nome dato a un pannello dentro il terminale, per esempio in Warp, non è leggibile
  dall'esterno.
- **Stati** ricavati dagli hook:

  | Evento hook | Stato |
  |---|---|
  | `SessionStart` | done (sessione aperta, in attesa del primo prompt; non lampeggia) |
  | `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure` | working |
  | `PermissionRequest`, `Notification` (`permission_prompt`, `elicitation_dialog`) | waiting |
  | `Stop` | done |
  | `StopFailure` | blocked (rosso) |
  | `SessionEnd` | la voce sparisce |

  Il blu (monitoring) non esiste per queste sessioni: gli hook non dicono se restano processi in background.
- **Sessioni morte**: se il processo di Claude Code non esiste più (Ctrl+C, finestra chiusa, crash: `SessionEnd`
  non è garantito), la voce sparisce al poll successivo.
- **Lampeggio**: le regole sono quelle della §3.2. "Stai guardando l'agente" qui significa che la finestra che
  ospita la sessione è in primo piano. Per un IDE con più finestre conta quella che ha la cartella nel titolo.
  Limite accettato: con più schede Claude nella stessa finestra di Windows Terminal, una sessione può risultare
  "vista" anche se stai guardando un'altra scheda.
- **Clic**: porta davanti la finestra che ospita la sessione, ripristinandola se è minimizzata.

  | Dove gira | Cosa viene portato davanti |
  |---|---|
  | console classica (cmd, PowerShell) | la finestra esatta |
  | Windows Terminal | la finestra giusta, non la scheda |
  | VS Code / Cursor / IntelliJ | la finestra dell'IDE con la cartella nel titolo, non il pannello del terminale |
  | macOS | l'app ospite (non verificato) |
  | WSL, SSH, container | niente: lo stato si vede, il clic non fa nulla |

- **Sessioni dentro Orca**: l'hook le ignora (variabile `ORCA_PANE_KEY` presente) perché le mostra già Orca.

### Installazione dell'hook

- **Primo avvio**: se esiste `~/.claude`, un dialogo chiede "Mostrare anche le sessioni di Claude Code fuori da
  Orca?". La risposta viene salvata in `settings.json` e la domanda non si ripete.
- **Menu**, sottomenu **Claude Code**: una riga di stato non cliccabile (`Integrazione: attiva` / `non installata` /
  `da riparare`), poi **Installa** (che dopo l'installazione diventa **Ripara**) e **Rimuovi integrazione**.
- **Cosa scrive**: in `~/.claude/settings.json` (impostazioni utente, quindi valide ovunque giri Claude Code), per
  ogni evento della tabella aggiunge un hook di tipo command:
  `"<percorso>/lollipop.exe" hook || echo {}`, con `"async": true`. Se l'exe non esiste più, il comando restituisce
  `{}`, cioè nessun effetto: nessun blocco e nessun errore. È lo stesso schema usato da Orca. Con `async`, Claude
  non aspetta l'hook: su Windows il solo avvio di un processo costa circa 200 ms (misurato, anche con un exe Go
  vuoto), e senza `async` ogni uso di uno strumento lo pagherebbe.
- **Senza far danni**:
  - prima di ogni modifica viene fatto un backup in `settings.json.lollipop-bak`;
  - vengono toccate solo le voci di lollipop, riconosciute da `lollipop… hook` nel comando;
  - gli hook degli altri (Orca compresa) restano intatti;
  - le altre chiavi del file restano nello stesso ordine. Il file viene riscritto con indentazione di 2 spazi,
    lo stesso formato che usa Claude Code.
- **Auto-riparazione**: se l'integrazione è attiva ma l'exe è stato spostato, all'avvio lollipop aggiorna il
  percorso negli hook, senza chiedere.
- **Rimuovi integrazione** toglie solo le voci di lollipop. Chi cancella l'exe senza passare dal menu lascia voci
  inerti, che non fanno danni.

### Implementazione

- **`lollipop hook`** (stesso binario, sottocomando) viene eseguito da Claude Code a ogni evento. Deve durare
  pochi millisecondi. Nell'ordine:
  1. esce subito se c'è `ORCA_PANE_KEY`;
  2. legge il JSON da stdin;
  3. trova il processo di Claude Code (`CLAUDE_PID` se presente, altrimenti risalendo i processi antenati) e il
     processo ospite, cioè il primo antenato con una finestra visibile;
  4. scrive `<UserConfigDir>/lollipop/claude/<session_id>.json` in modo atomico (file temporaneo + rename). Il file
     contiene stato, `cwd`, pid e ora di creazione del processo di Claude, pid e nome dell'ospite, e l'ora
     dell'evento.

  Un evento più vecchio di quello già scritto viene scartato, perché gli hook possono sovrapporsi. `SessionEnd`
  cancella il file.
- **App**: a ogni poll legge la cartella. Cancella i file la cui sessione è morta, cioè quando il pid non esiste
  più o ha un'ora di creazione diversa (pid riciclato). Unisce le voci a quelle di Orca con chiave
  `claude:<session_id>`. Il clic e il controllo "sta guardando" passano dalla sorgente della voce: Orca oppure
  finestra ospite.
- **Codice di piattaforma**:
  - Windows: Toolhelp32 per i processi antenati e `EnumWindows` per le finestre dell'ospite;
  - macOS: `NSRunningApplication` dell'antenato, scritto alla cieca.
- **Verificato** (Claude Code 2.1.282, Windows): gli hook girano in Git Bash; `CLAUDE_PID` arriva all'hook ed è
  il pid di `claude.exe`. Nella console classica viene trovata la finestra esatta; dentro Orca, risalendo i
  processi, viene trovato `Orca.exe`. Se si uccide Claude, la voce sparisce al poll successivo.
- **Ancora da verificare** su una macchina che li usa: Windows Terminal, VS Code e IntelliJ; se le sessioni già
  aperte leggono subito i nuovi hook o solo al riavvio.
- **Orca non avviato non è più un errore**, perché ora lollipop serve anche a chi usa solo Claude Code. Se non si
  riesce a leggere `orca-runtime.json` o a connettersi alla pipe, semplicemente non ci sono voci di Orca. Il
  pallino rosso resta per gli errori di protocollo, cioè quando la pipe risponde ma in modo inatteso.

### Fuori scope

Rispondere ai permessi dal semaforo (come soundpad), selezionare la scheda o il pannello esatto, WSL/SSH/container,
installer, altri strumenti come Codex o Gemini.

## 10. v0.2 — lingua dell'interfaccia

> Stato: **approvata** (2026-09-25).

- **Lingue**: inglese (predefinita) e italiano.
- **Scelta automatica**: italiano se la lingua dell'interfaccia del sistema operativo è l'italiano, altrimenti
  inglese.
  - Windows: `GetUserDefaultUILanguage`.
  - macOS: la prima lingua preferita dell'utente (`NSLocale.preferredLanguages`), non verificato.
- **Menu**: nuovo sottomenu **Language / Lingua ▸ ◉ Automatica · English · Italiano**. L'etichetta è bilingue, così
  la si trova anche se l'interfaccia è nella lingua che non si capisce. La scelta ha effetto subito: i menu della
  finestra e della traybar vengono ricostruiti, e la finestra e il tooltip dell'icona si aggiornano al poll
  successivo. Si salva in `settings.json` (`lang`: `auto` | `en` | `it`).
- **Cosa viene tradotto**: menu, dialoghi, tooltip (finestra e traybar), messaggi d'errore e l'output di `-once`.
- **Cosa non viene tradotto**: i nomi degli stati tra parentesi quadre nel tooltip (`[working]`, `[done]`, …),
  perché sono quelli di Orca e di Claude Code.
- **Implementazione**: niente librerie e niente file di traduzione. Ogni testo si scrive dove serve come
  `tr("English", "Italiano")`, che restituisce la versione della lingua attiva. Il frontend riceve la lingua con
  l'evento `settings` e ha i suoi due testi ("Nessun agente attivo", "Errore").
- **README**: la nota "The UI is in Italian" diventa "English and Italian, following the OS language".

## 11. v0.3 — avvio automatico e finestra solo con agenti

> Stato: **approvata** (2026-09-26).

### Finestra solo con agenti

- Senza agenti la finestra sparisce e resta solo l'icona nella traybar (grigia, tooltip `nessun agente attivo`).
  Ricompare quando arriva il primo agente.
- Con un errore di protocollo di Orca la finestra resta visibile col pallino rosso, anche senza agenti.
- La finestra è visibile quando valgono tutte e tre: **Mostra finestra** attivo, finestra già posizionata, almeno un
  agente (o un errore). **Mostra finestra** resta la preferenza dell'utente e non viene toccata.
- All'avvio la finestra resta nascosta finché il primo poll non trova un agente, così non lampeggia vuota.
- Nascondere è immediato (al poll). Mostrare aspetta che il frontend comunichi la nuova dimensione (evento `size`),
  così la finestra non compare con il contenuto vecchio.
- Il clic sull'icona riattiva **Mostra finestra** come prima; senza agenti la finestra resta nascosta e non prende
  il focus.

### Avvio automatico

- Nuova voce di menu **Avvia all'accesso** (casella), sotto **Mostra finestra**. Lo stato si legge dal sistema
  operativo a ogni costruzione del menu e dopo ogni clic: non sta in `settings.json`, così resta giusto anche se
  l'utente toglie la voce da fuori.
- **Windows**: valore `lollipop` in `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, con il percorso dell'exe
  tra virgolette. Gestione attività può disattivarlo senza toglierlo: lo segna in
  `...\Explorer\StartupApproved\Run` con un primo byte dispari (`02` attivo, `03` disattivato, verificato sul
  registro). In quel caso la casella risulta spenta, e attivandola lollipop cancella quel segno, che altrimenti
  prevarrebbe sulla nuova voce.
- **macOS** (non verificato): LaunchAgent `~/Library/LaunchAgents/io.github.antoniointrieri.lollipop.plist` con
  `RunAtLoad` e senza `KeepAlive`, così **Esci** resta definitivo. launchd lo carica al login successivo: niente
  `launchctl bootstrap`, che con `RunAtLoad` avvierebbe subito una seconda istanza. Un agente disattivato da
  Impostazioni di Sistema > Elementi di login risulta comunque attivo.
- **Exe spostato**: all'avvio, se la voce esiste ma punta a un altro percorso, viene riscritta in silenzio, come
  l'hook di Claude Code.
- **Perché non l'API `Autostart` di Wails**: su Windows ignora la disattivazione da Gestione attività e riconosce
  la voce solo se punta all'exe corrente, quindi non si accorge di un exe spostato; su macOS fa
  `launchctl bootstrap` appena attivata.

## 12. v0.3 — indicatore a lollipop

> Stato: **approvata** (2026-09-26).

- Nuovo sottomenu **Indicatore ▸ ◉ Pallino · Lollipop**, sotto **Forma**. Si salva in `settings.json`
  (`marker`: `dot` | `lollipop`, predefinito `dot`).
- **Lollipop**: al posto del pallino, la testa del lollipop dell'icona senza bastoncino. Cerchio nel colore dello
  stato con una spirale di Archimede bianca di 2 giri, tratto 1,7 su un diametro di 20. La spirale dell'icona
  (3 giri circa) a 8-15 px diventerebbe una macchia; con 1,5 giri e tratto più spesso il bianco copre troppo il
  colore.
- Disegnato in SVG nel frontend (generato in JS all'avvio), con la testa in `currentColor`: si adatta alla
  dimensione e ai colori senza file di asset.
- **Giallo più scuro** per la testa del lollipop e per l'icona nella traybar (`rgb(230,176,0)` invece di
  `rgb(255,215,0)`): sul giallo pieno la spirale bianca quasi non si vede. Il testo della voce e il pallino restano
  del giallo normale.
- **Misure**: testa di 1rem in vista normale e compatta, 1,15rem per il segno di "nessun agente" / errore.
- **Lampeggio**: la fase spenta applica `filter: brightness(0.4)` all'indicatore, così si abbassano insieme testa
  e spirale. Anche il pallino ora lampeggia così, con lo stesso risultato di prima (colore al 40%).

## 13. v0.4 — ordine delle voci e agenti done dietro l'ellissi

> Stato: **approvata** (2026-09-28).

Con molti agenti la barra diventa più lunga dello schermo. Tutto è opzionale: con i valori predefiniti la barra
resta com'era (ordine alfabetico, nessun raggruppamento, nessuna ellissi).

### Impostazioni

Nella pagina **Voci** della finestra delle impostazioni (§14): **Ordine** (Alfabetico / Ultima attività),
**Raggruppa per stato**, **Raccogli gli agenti idle** con il numero di quelli che restano visibili.

In `settings.json`: `order` (`alpha` | `recent`), `side` (`right` | `left`), `groupByState`, `doneMax` (`-1` = tutti).

### Ordine

- **Voci più importanti**: a destra (predefinito), il bordo che resta fermo mentre la finestra cresce verso
  sinistra, oppure a sinistra. Qui sotto "a destra" vale per il predefinito; con "a sinistra" tutto si specchia,
  ellissi compresa, tranne l'ordine alfabetico.
- **Alfabetico**: come prima, sempre da sinistra a destra.
- **Ultima attività**: l'agente con il cambio di stato più recente sta a destra. Il momento del cambio è il poll in
  cui lollipop lo osserva: gli agenti trovati all'avvio sono alla pari e restano in ordine alfabetico. La fine
  del lampeggio (agente visto) non conta come attività.
- **Raggruppa per stato**: da destra, in attesa (rosso) · finiti da vedere (verde lampeggiante) · al lavoro (giallo
  e blu) · **idle**, cioè finiti già visti (verde fisso). Dentro ogni gruppo vale l'ordine scelto.

### Ellissi

- Con **Raccogli gli agenti idle** attivo e N visibili restano in barra solo gli N agenti idle con il cambio di stato più recente (a
  parità, quelli più a destra). Gli altri vanno dietro una voce grigia `⋯ K` (K = quanti sono), all'estremo
  sinistro della barra accanto alla maniglia. Solo gli idle possono finire lì: un agente che richiede attenzione
  resta sempre in barra.
- Al passaggio del mouse sulla voce `⋯` (o al clic) si apre una lista verticale con quelle voci, allineata a
  destra, cliccabile come la barra. Si apre verso l'alto se la barra sta nella metà inferiore dello schermo, verso il
  basso altrimenti, ed è alta al massimo lo spazio disponibile da quel lato (poi scorre). Si chiude 300 ms dopo che
  il mouse è uscito da `⋯` e dalla lista.
- La finestra si allarga per contenere la lista, tenendo fermi il bordo destro e la riga della barra: aprendo verso
  l'alto si sposta in su di quanto è alta la lista. La posizione salvata è quella della barra.

### Voci ferme sotto il mouse

Finché il mouse è sulla finestra le voci non cambiano posto e non entrano nell'ellissi: i colori si aggiornano,
l'ordine resta quello sullo schermo. Così un clic non colpisce una voce appena spostata, e la voce cliccata (che
smette di lampeggiare e diventa idle) non scappa via. Due eccezioni: una voce nell'ellissi che richiede attenzione
torna subito in barra, e un agente nuovo compare a sinistra. Quando il mouse esce si applica l'ordine attuale.

### Implementazione

- Ordine e appartenenza all'ellissi si calcolano in Go (`arrange` in `state.go`, testata), dopo il tracker, che
  registra il poll dell'ultimo cambio di stato di ogni agente. Ogni voce inviata al frontend ha il campo `more`.
- Il frontend decide la direzione della lista da `window.screenY` e `screen.availTop/availHeight` (da verificare
  con più monitor e su macOS) e invia `size` con l'altezza della barra e la direzione; `place` ricava la cima della barra dai limiti
  della finestra e tiene fissa quella.

## 14. v0.4 — finestra delle impostazioni

> Stato: **approvata** (2026-09-28). Supera la scelta della §7 ("niente finestra di impostazioni"): con le
> opzioni della §13 il menu era diventato illeggibile.

- Il menu contestuale (finestra e icona) si riduce a **Impostazioni…**, **Mostra finestra**, **Esci**.
- **Impostazioni…** apre una finestra normale (con cornice, nella taskbar, non sempre in primo piano), una sola:
  riaprirla la porta davanti, chiuderla la nasconde. Titolo "Impostazioni di lollipop".
- **Stile del sistema**: controlli standard (casella di spunta, menu a tendina, cursore, campo numerico, pulsante)
  nel tema chiaro/scuro e nel colore d'accento del sistema; la pagina li dispone soltanto, senza ridisegnarli:
  navigazione a sinistra, una riga per impostazione (etichetta a sinistra, controllo a destra), niente sottotitoli.
  Tasto destro disattivato. Sfondo Mica su Windows 11 22H2+, vibrancy su macOS, sfondo pieno su Windows 10.
  Accento: su Windows dalla `AccentPalette` del registro (tonalità scura in tema chiaro, chiara in tema scuro,
  come le Impostazioni), su macOS `-apple-system-control-accent`.
- **Spiegazioni**: nessun "?" né tooltip sulle impostazioni. Le informazioni importanti (cosa fa un'integrazione)
  sono scritte per esteso nella pagina.
- **Cursori** per Dimensione (70-160%, passo 5) e Velocità del lampeggio (periodo 200-1500 ms, passo 50; verso
  destra più veloce). Il valore si applica al rilascio; il Go lo riporta comunque nei limiti.
- **Icona della finestra**: il lollipop rosso, passato a Wails come `Options.Icon` (Wails cerca l'icona dell'exe
  nella risorsa 3, go-winres la mette altrove).
- **Pagine**:

| Pagina | Contenuto |
|---|---|
| Aspetto | Forma, Indicatore, Dimensione, Velocità del lampeggio, Vista compatta |
| Voci | Ordine, Voci più importanti (a destra / a sinistra), Raggruppa per stato, Raccogli gli agenti idle + quanti lasciarne visibili (0-99) |
| Generale | Sempre in primo piano, Mostra finestra, Avvia lollipop all'accesso, Lingua |
| Integrazioni | una sezione per agente, per ora solo **Claude Code**: "lollipop aggiunge un hook a `settings.json`" con il percorso cliccabile (lo apre con l'app predefinita, o apre la cartella se il file non c'è), stato, Installa/Ripara, Rimuovi |
| Informazioni | versione (dal tag stampato da `go build`), licenza, link al repo |

- Effetto immediato come prima, niente pulsante Salva.
- **Implementazione**: seconda finestra Wails su `frontend/settings.html`, creata alla prima apertura. La pagina
  chiede lo stato con `settings-ready` e riceve `settings` (come la barra) e `status` (integrazione Claude Code,
  avvio all'accesso, versione, accento, sfondo). Invia `setting` {key, value} per i campi di `settings.json`
  (solo quelli ammessi), `autostart`, `claude` (`install` | `remove`), `open-claude-settings` e `open-repo`.
