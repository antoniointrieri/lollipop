# lollipop — brief di partenza

> Documento di passaggio consegne scritto dall'agente che ha costruito il proof of concept.
> L'utente parla italiano. Questo brief NON è una specifica approvata: è il punto di partenza per il design.

## Cos'è

**lollipop** è una mini-applicazione desktop (Go, cross-platform: **Windows + macOS**) che fa da "semaforo"
per gli agenti AI attivi in Orca: una finestrella fluttuante sempre in
primo piano con una voce per agente, colorata secondo lo stato, che lampeggia quando un agente ha appena
finito e che, cliccata, porta Orca in primo piano sul terminale di quell'agente.

Il nome viene dal "lollipop man" inglese: il vigile con la paletta tonda che dice a ciascuno quando tocca a lui.

## Proof of concept esistente (riferimento funzionale)

`orca-semaforo.ps1` — PowerShell + WinForms, funzionante su Windows, usato quotidianamente
dall'utente. **Leggilo per intero prima di progettare**: ogni riga codifica una decisione presa con l'utente.
Si lancia con `Semaforo Orca.lnk` (`conhost.exe --headless powershell.exe -File ...`),
debug con `powershell -NoProfile -File orca-semaforo.ps1 -Once`.

Il POC resta in uso finché lollipop non lo sostituisce: non modificarlo.

## Comportamento concordato con l'utente (parità con il POC)

- **Finestra**: senza bordi, sempre in primo piano, non compare nella barra delle applicazioni.
  - Trascinabile da una maniglia `⋮` a sinistra.
  - Cresce verso sinistra mantenendo fermo il bordo destro, clampato allo schermo **su cui si trova**
    (multi-monitor: l'utente la tiene anche sul secondo monitor, sopra la taskbar).
  - Resta sopra la taskbar di Windows anche dopo che la si clicca: la taskbar è anch'essa topmost e vince
    l'ultima attivata, quindi il POC riafferma HWND_TOPMOST ogni 500 ms (SetWindowPos, flag NOACTIVATE).
  - Tasto destro → menu con "Esci".
- **Voci**: una per ogni agente che ha un terminale vivo (gli agenti `done` senza terminale sono sessioni chiuse: ignorati).
  - Pallino ● + nome del repo, entrambi nel colore dello stato.
  - Worktree secondari (`isMainWorktree: false`) mostrati come `repo/displayName`.
  - Tooltip: `[stato] repo` + titolo del terminale (senza il glifo iniziale tipo `✳`/`◑`).
  - Nessun agente: pallino grigio "Nessun agente attivo"; errore di connessione: pallino rosso con l'errore nel tooltip.
- **Colori**:
  - giallo = `working`
  - verde = `done`
  - blu = turno finito ma shell/monitor in background ancora attivi (`working` + `workingMode: monitoring`)
  - rosso = qualsiasi altro stato (`blocked`, `waiting`: attesa input o permesso)
- **Lampeggio** (solo il pallino, non il testo; alterna il colore dello stato con la sua versione al 40% di luminosità):
  - parte sulla transizione `working` → qualsiasi altro stato, osservata dall'app (all'avvio nessuno lampeggia);
  - si ferma se l'agente torna `working`, se l'utente clicca la voce, oppure se Orca è la finestra in primo piano
    del sistema **e** il pannello di quell'agente è quello attivo dentro Orca;
  - se l'utente sta già guardando l'agente quando finisce, non lampeggia.
- **Clic** (sinistro, su pallino o nome):
  - evidenzia subito pallino e nome (sfondo grigio) per 500 ms; nello stesso intervallo i clic ripetuti sono ignorati;
  - porta la finestra di Orca in primo piano (ripristinandola se minimizzata);
  - chiama `terminal.focus` sul terminale dell'agente.
- **Aggiornamento**: ogni 500 ms, lontano dal thread UI (se Orca si blocca la UI resta reattiva).
- **Nessuna finestra di console** all'avvio (su Windows: binario GUI, es. `-ldflags -H=windowsgui`).

## Come parlare con Orca (scoperto per reverse engineering — NON documentato)

Orca 1.4.211 su Windows. Tutto quanto segue è stato verificato su questa macchina; su macOS nulla è verificato.

### Named pipe locale (usata dal POC)

- Endpoint e token in `%APPDATA%\orca\orca-runtime.json`:
  `{ runtimeId, pid, transports: [{kind:"named-pipe", endpoint:"\\\\.\\pipe\\orca-<pid>-<runtimeId prefix>"}, {kind:"websocket", endpoint:"ws://0.0.0.0:6768"}], authToken, startedAt }`.
  Il nome della pipe cambia a ogni riavvio di Orca → rileggere il file quando la connessione cade.
- Protocollo: una riga JSON per richiesta `{"id":"...","authToken":"<token>","method":"...","params":{...}}`,
  una riga JSON per risposta `{"id":"...","ok":true,"result":{...},"_meta":{...}}` oppure `ok:false, error:{code,message}`.
  UTF-8. Una connessione tenuta aperta regge richieste in sequenza (4–15 ms ciascuna).
- **Il token è un segreto: non loggarlo, non stamparlo.**
- Metodi usati:
  - `worktree.ps` `{"limit":500}` → `result.worktrees[]` con `repo`, `displayName`, `isMainWorktree`, `isActive`
    (worktree visualizzato in Orca), `worktreeId`, `agents[]` con `paneKey` (`<tabId>:<leafId>`), `state`,
    `prompt`, `toolName`, `stateStartedAt`, `updatedAt`.
  - `terminal.list` `{"includeVisualLayouts":true}` → `result.terminals[]` con `handle`, `tabId`, `leafId`, `title`,
    `worktreeId`; `result.visualLayouts[]` con `worktreeId` e `root` (`type:"group"`, `activeTabId`,
    `tabs[]` con `tabId` e `activeLeafId`). Pannello in primo piano in Orca = worktree `isActive` →
    `root.activeTabId` → `activeLeafId` di quella scheda. (Il POC legge solo il gruppo radice; con schede
    divise in più gruppi è da approfondire.)
  - `terminal.focus` `{"terminal":"<handle>"}` → apre scheda e worktree giusti (`navigated: true`).
- Le sottoscrizioni (`notifications.subscribe`, ecc.) sulla pipe rispondono
  `method_not_supported: requires a streaming transport`.

### Stati agente

Enum in Orca: `working`, `blocked`, `waiting`, `done`. `workingMode: "monitoring"` quando il turno è finito ma
restano shell/monitor in background (sub-agenti vivi invece danno `working` puro). `worktree.ps` **non** espone
`workingMode`: il POC lo legge da `%APPDATA%\orca\agent-hooks\last-status.json`
(`entries[<paneKey>].payload.workingMode`). File interno: se cambia formato si perde solo il blu.

### Feed di eventi (valutato e scartato per ora)

- `notifications.subscribe` emette `agent-task-complete` con `paneKey` (anche `includeDesktopSuppressed`), ma
  solo su trasporto streaming: il websocket richiede un handshake E2EE di pairing
  (chiude con `4001 Invalid e2ee_hello`). Non reimplementare quel protocollo.
- `agentSession.subscribeStatus` riguarda solo le sessioni "structured" di Orca, non gli agenti nei terminali.
- `events.subscribe` esiste nell'API dei plugin di Orca (`scope: host-events`), non esplorata.
- Decisione: polling sulla pipe; passare agli eventi se Orca li esporrà in locale.

## Decisioni già prese con l'utente

- Linguaggio **Go** (installato: go 1.26.4, node 22, npm 10, git; WebView2 presente; niente gcc; CLI Wails da installare).
- Piattaforme della prima versione: **Windows e macOS**.
- UI: orientamento verso **Wails v2** (finestra frameless + AlwaysOnTop, backend Go). Alternativa discussa e
  non scelta: icona nella system tray (`fyne.io/systray`), più semplice ma senza colpo d'occhio su tutti gli agenti.
  Fyne scartato (niente always-on-top). **Da riconfermare con l'utente in fase di design.**
- Codice specifico per piattaforma (trasporto verso Orca, finestra in primo piano, attivazione di Orca)
  isolato in file con build tag.

## Aperto / da verificare

- **macOS**: trasporto verso il runtime (probabilmente socket Unix — leggere `orca-runtime.json` su macOS),
  percorso della cartella dati di Orca, rilevamento dell'app in primo piano (NSWorkspace) e attivazione di Orca.
  L'utente non ha ancora detto se ha un Mac per i test: chiederlo.
- Distribuzione: solo uso personale o anche per il team? (installer, firma, avvio automatico)
- Extra proposti e mai richiesti: memoria della posizione, avvio automatico, suono alla fine.
- `orca terminal list` include anche terminali di host remoti? Per ora solo host `local`.

## Lezioni dal POC (per non ripeterle)

- Output della CLI `orca` = UTF-8: se decodificato con la codepage OEM i glifi diventano `Ô£│`.
- Schermo principale al 125% di scaling: attenzione a coordinate logiche vs fisiche.
- Un agente con un comando in background ancora vivo resta `working` per Orca: quando fai prove, non lasciare
  processi in background appesi (falsano proprio lo stato che questa app mostra).
- Cercare stringhe in `%LOCALAPPDATA%\Programs\orca\resources\app.asar` (≈120 MB): usare ricerche a stringa fissa
  (`grep -aobF`), le regex con `.{0,N}` durano minuti.

## Come procedere

Il design è a metà: è stata fatta solo la prima domanda (piattaforme → Windows + macOS).
Prosegui con il processo di brainstorming → spec scritta → approvazione dell'utente → piano → implementazione.
Non scrivere codice di prodotto prima che l'utente abbia approvato la spec.
