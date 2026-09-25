<p align="center">
  <img src="docs/img/icon.png" width="96" alt="">
</p>

<h1 align="center">lollipop</h1>

<p align="center">
  Un semaforo per i tuoi agenti AI, in <b>Orca</b> e nelle sessioni di <b>Claude Code</b>: una finestrella
  sempre in primo piano che ti dice, con un'occhiata, quale agente sta lavorando, quale ha finito e quale
  aspetta te.
</p>

<p align="center">
  <img src="docs/img/pill.png" width="614" alt="La finestra di lollipop con cinque agenti in stati diversi">
</p>

<p align="center"><a href="README.md">Read in English</a></p>

Il nome viene dal *lollipop man* inglese, il vigile con la paletta tonda che dice a ciascuno quando tocca a lui.

## Cosa fa

- **Una voce per agente**: ogni agente di Orca con un terminale aperto e ogni sessione di Claude Code in
  esecuzione sulla macchina (console, Windows Terminal, Warp, terminale di VS Code o IntelliJ, ...), colorata
  secondo lo stato:

  | Colore | Stato |
  |---|---|
  | 🟡 giallo | sta lavorando (`working`) |
  | 🟢 verde | ha finito (`done`) |
  | 🔵 blu | turno finito, ma con shell o monitor ancora attivi in background (solo Orca) |
  | 🔴 rosso | aspetta te: input o un permesso (`waiting`, `blocked`) |

- **Lampeggia** quando un agente ha appena finito e non l'hai ancora guardato. Smette quando clicchi la voce,
  quando porti davanti l'agente o quando l'agente riparte. Se stavi già guardando l'agente, non lampeggia.
- **Clic su una voce**: porta davanti l'agente. Per Orca, direttamente sul terminale di quell'agente; per
  Claude Code, la finestra che ospita la sessione, che lollipop trova da solo. Vedi
  [Sessioni Claude Code](#sessioni-claude-code).
- **Icona nella traybar** nel colore dell'agente più urgente, con un riepilogo nel tooltip
  (`lollipop — 1 in attesa, 1 done, 2 working`). Puoi anche nascondere la finestra e tenere solo l'icona.
- **Si adatta**: cresce verso sinistra tenendo fermo il bordo destro, resta sopra la taskbar, funziona su più
  monitor e al riavvio si ricorda dove l'avevi lasciata.

<p align="center">
  <img src="docs/img/compact.png" width="188" alt="Vista compatta: solo i pallini">
  &nbsp;&nbsp;&nbsp;&nbsp;
  <img src="docs/img/tray.png" width="200" alt="Icone della traybar nei cinque colori">
</p>
<p align="center"><sub>Vista compatta e icone della traybar</sub></p>

## Requisiti

- **Windows 10/11** con WebView2 (già presente su Windows 11 e su Windows 10 aggiornato).
- Almeno uno tra:
  - **Orca** in esecuzione sulla stessa macchina (provato con Orca 1.4.211);
  - la CLI di **Claude Code** (provata con la 2.1.282), con l'hook di lollipop installato dal menu.
- **macOS 11+**: sperimentale, vedi [Limitazioni](#limitazioni).

## Installazione

### Download

Scarica l'ultima versione da [Releases](https://github.com/antoniointrieri/lollipop/releases), oppure la build di
un qualsiasi commit dalla pagina [Actions](https://github.com/antoniointrieri/lollipop/actions).

- **Windows**: `lollipop.exe`. L'eseguibile non è firmato: al primo avvio SmartScreen può dire "PC protetto da
  Windows"; clicca *Ulteriori informazioni* → *Esegui comunque*.
- **macOS**: `lollipop-macos.tar.gz` (binario universale, Apple Silicon e Intel). Anche questo non è firmato,
  quindi prima di avviarlo togli la quarantena:

  ```sh
  tar -xzf lollipop-macos.tar.gz
  xattr -d com.apple.quarantine lollipop
  ./lollipop
  ```

### Compilare dai sorgenti

Serve **Go 1.26** o successivo; non servono né Node né npm.

```sh
git clone https://github.com/antoniointrieri/lollipop.git
cd lollipop
go build -ldflags "-H=windowsgui" -o lollipop.exe .   # Windows
go build -o lollipop .                               # macOS (servono gli Xcode Command Line Tools)
```

Avvia `lollipop.exe`: la finestra compare in alto a destra. Per farlo partire con Windows, metti un collegamento
a `lollipop.exe` in `shell:startup` (Win+R → `shell:startup`); su macOS aggiungilo agli *Elementi di login*.

## Uso

| Azione | Effetto |
|---|---|
| Clic su una voce | porta davanti l'agente (Orca sul suo terminale, oppure la finestra di Claude Code) |
| Trascina la maniglia `⋮` | sposta la finestra |
| Tasto destro (sulla finestra o sull'icona) | menu delle impostazioni ed **Esci** |
| Clic sull'icona nella traybar | mostra la finestra e la porta davanti |

Impostazioni disponibili dal menu (hanno effetto subito e vengono salvate):

- **Forma**: rettangolo o capsula
- **Lampeggio**: lento, normale o veloce
- **Dimensione**: piccola, normale o grande
- **Compatta**: solo i pallini, con il nome nel tooltip
- **Sempre in primo piano** e **Mostra finestra**
- **Claude Code**: stato dell'integrazione, **Installa** (poi **Ripara**), **Rimuovi integrazione**
- **Language / Lingua**: automatica (lingua del sistema operativo), English o Italiano

Impostazioni e posizione stanno in `settings.json`, dentro `%APPDATA%\lollipop` (Windows) o
`~/Library/Application Support/lollipop` (macOS).
Alt+F4 nasconde la finestra e non chiude l'app: per uscire usa **Esci**.

### Sessioni Claude Code

Al primo avvio lollipop propone di aggiungere un hook alle impostazioni utente di Claude Code
(`~/.claude/settings.json`, con backup). L'hook esegue `lollipop.exe hook` in background a ogni evento di Claude
Code, e Claude non lo aspetta mai. Le sessioni dentro Orca vengono ignorate, perché le mostra già Orca.

Il sottomenu **Claude Code** mostra se l'integrazione è attiva e permette di ripararla o rimuoverla. La
rimozione tocca solo le voci di lollipop; se cancelli l'exe senza rimuoverla, le voci rimaste non fanno nulla.
Se sposti l'exe, lollipop corregge il percorso al successivo avvio.

Il clic porta davanti la finestra giusta, non la scheda esatta: Windows Terminal e gli IDE non permettono ad
altre app di selezionare una scheda o un pannello del terminale. Per le sessioni in WSL, SSH o container si
vede lo stato, ma il clic non fa nulla.

### Se qualcosa non va

Il pallino rosso con "Errore" nel tooltip vuol dire che Orca ha risposto in modo inatteso (per esempio dopo
un aggiornamento di Orca); lollipop riprova da solo ogni mezzo secondo. Orca chiuso non è un errore. Per vedere cosa legge da Orca e da Claude Code:

```sh
lollipop.exe -once | more     # Windows
./lollipop -once              # macOS
```

Il comando stampa agenti, stati e pannello attivo, poi esce.

## Come funziona

**Orca**: lollipop parla con il runtime locale di Orca attraverso la stessa named pipe che usa la CLI `orca` (socket Unix
su macOS). L'indirizzo e il token li legge da `%APPDATA%\orca\orca-runtime.json`. Ogni 500 ms chiede a Orca la
lista dei worktree con i loro agenti e quella dei terminali; al clic chiede di aprire il terminale. Non modifica
nient'altro in Orca, e il token non viene mai scritto da nessuna parte.

> [!WARNING]
> Il protocollo della pipe **non è documentato**: è stato ricostruito osservando Orca. Un aggiornamento di Orca
> potrebbe romperlo; in quel caso lollipop mostra il pallino rosso con l'errore.

**Claude Code**: l'hook scrive un piccolo file di stato per ogni sessione nella cartella delle impostazioni di
lollipop, che lollipop legge ogni 500 ms. L'hook registra anche quale finestra ospita la sessione, risalendo dal
processo di Claude Code; una sessione il cui processo non esiste più viene tolta.

La UI è fatta con [Wails v3](https://github.com/wailsapp/wails): backend Go e una pagina HTML incorporata nel binario.

## Limitazioni

- **macOS non provato**: `platform_darwin.go` è scritto sulla base del codice di Orca e compila in CI, ma non è
  mai stato eseguito. La cartella dati di Orca (`~/Library/Application Support/orca`) e il trasporto via socket
  sono ipotesi ragionate. Chi ha un Mac può lanciare `./lollipop -once` e raccontare cosa succede.
- Sessioni Claude Code: il clic porta davanti la finestra giusta, non la scheda o il pannello esatto dell'IDE.
  Con più finestre dello stesso terminale vince quella con la cartella della sessione nel titolo, altrimenti la
  prima. Per le sessioni in WSL, SSH o container si vede lo stato, ma il clic non fa nulla.
- Se in Orca dividi le schede in più gruppi affiancati, il rilevamento del pannello attivo guarda solo il
  gruppo principale.
- Mostra solo i terminali locali, non quelli degli host remoti.
- Wails v3 è ancora in beta: la versione è fissata in `go.mod`.

## Sviluppo

```
main.go               avvio, loop di polling, eventi tra Go e frontend
orca.go               client del runtime di Orca
claude.go             sessioni Claude Code pure: hook, file di sessione, installazione in settings.json
state.go              voci, lampeggio, riepilogo per la traybar (logica pura, testata)
ui.go                 impostazioni, menu, traybar, posizione, disegno dell'icona
platform_*.go         codice specifico di Windows e macOS
frontend/index.html   la finestra (HTML/CSS/JS, senza build)
docs/spec.md          specifica: comportamento e decisioni
```

- Test: `go test ./...`
- Le novità significative vanno in [CHANGELOG.md](CHANGELOG.md), in inglese: la sezione di una versione diventa il testo
  della sua Release.
- La CI (`.github/workflows/build.yml`) testa e compila Windows e macOS a ogni push; un tag `v*` pubblica anche
  una GitHub Release con i due binari.
- L'icona dell'exe (`lollipop.ico`, `rsrc_windows_amd64.syso`) è disegnata dal codice in `ui.go`. Se cambi il
  disegno, rigenerala con `go generate`.

## Licenza

[MIT](LICENSE)
