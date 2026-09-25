<p align="center">
  <img src="docs/img/icon.png" width="96" alt="">
</p>

<h1 align="center">lollipop</h1>

<p align="center">
  Un semaforo per gli agenti AI di <b>Orca</b>: una finestrella sempre in primo piano che ti dice,
  con un'occhiata, quale agente sta lavorando, quale ha finito e quale aspetta te.
</p>

<p align="center">
  <img src="docs/img/pill.png" width="614" alt="La finestra di lollipop con cinque agenti in stati diversi">
</p>

Il nome viene dal *lollipop man* inglese, il vigile con la paletta tonda che dice a ciascuno quando tocca a lui.

## Cosa fa

- **Una voce per agente** con un terminale aperto in Orca, colorata secondo lo stato:

  | Colore | Stato |
  |---|---|
  | 🟡 giallo | sta lavorando (`working`) |
  | 🟢 verde | ha finito (`done`) |
  | 🔵 blu | turno finito, ma con shell o monitor ancora attivi in background |
  | 🔴 rosso | aspetta te: input o un permesso (`waiting`, `blocked`) |

- **Lampeggia** quando un agente ha appena finito e non l'hai ancora guardato. Smette quando clicchi la voce,
  quando apri quel pannello in Orca o quando l'agente riparte. Se stavi già guardando l'agente, non lampeggia.
- **Clic su una voce**: Orca va in primo piano, direttamente sul terminale di quell'agente.
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
- **Orca** in esecuzione sulla stessa macchina (provato con Orca 1.4.211).
- Per compilare: **Go 1.26** o successivo. Non servono né Node né npm.

macOS è supportato solo in teoria: il codice c'è, ma non è ancora mai stato compilato né provato
(vedi [Limitazioni](#limitazioni)).

## Installazione

```sh
git clone <url-di-questo-repo> lollipop
cd lollipop
go build -ldflags "-H=windowsgui" -o lollipop.exe .
```

Avvia `lollipop.exe`: la finestra compare in alto a destra. Per farlo partire con Windows, metti un collegamento
a `lollipop.exe` in `shell:startup` (Win+R → `shell:startup`).

## Uso

| Azione | Effetto |
|---|---|
| Clic su una voce | apre Orca sul terminale dell'agente |
| Trascina la maniglia `⋮` | sposta la finestra |
| Tasto destro (sulla finestra o sull'icona) | menu delle impostazioni ed **Esci** |
| Clic sull'icona nella traybar | mostra la finestra e la porta davanti |

Impostazioni disponibili dal menu (hanno effetto subito e vengono salvate):

- **Forma**: rettangolo o capsula
- **Lampeggio**: lento, normale o veloce
- **Dimensione**: piccola, normale o grande
- **Compatta**: solo i pallini, con il nome nel tooltip
- **Sempre in primo piano** e **Mostra finestra**

Le impostazioni e la posizione stanno in `%APPDATA%\lollipop\settings.json`.
Alt+F4 nasconde la finestra e non chiude l'app: per uscire usa **Esci**.

### Se qualcosa non va

Il pallino rosso con "Errore" nel tooltip vuol dire che lollipop non riesce a parlare con Orca (per esempio
perché Orca è chiuso): riprova da solo ogni mezzo secondo. Per vedere cosa legge da Orca:

```sh
lollipop.exe -once | more
```

Il comando stampa agenti, stati e pannello attivo, poi esce.

## Come funziona

lollipop parla con il runtime locale di Orca attraverso la stessa named pipe che usa la CLI `orca` (socket Unix
su macOS). L'indirizzo e il token li legge da `%APPDATA%\orca\orca-runtime.json`. Ogni 500 ms chiede a Orca la
lista dei worktree con i loro agenti e quella dei terminali; al clic chiede di aprire il terminale. Non modifica
nient'altro in Orca, e il token non viene mai scritto da nessuna parte.

> [!WARNING]
> Il protocollo della pipe **non è documentato**: è stato ricostruito osservando Orca. Un aggiornamento di Orca
> potrebbe romperlo; in quel caso lollipop mostra il pallino rosso con l'errore.

La UI è fatta con [Wails v3](https://github.com/wailsapp/wails): backend Go e una pagina HTML incorporata nel binario.

## Limitazioni

- **macOS non provato**: `platform_darwin.go` è scritto sulla base del codice di Orca, ma non è mai stato
  compilato. Chi ha un Mac può provarlo con `go build -o lollipop .` (servono gli Xcode Command Line Tools) e
  poi `./lollipop -once`.
- Se in Orca dividi le schede in più gruppi affiancati, il rilevamento del pannello attivo guarda solo il
  gruppo principale.
- Mostra solo i terminali locali, non quelli degli host remoti.
- Wails v3 è ancora in beta: la versione è fissata in `go.mod`.

## Sviluppo

```
main.go               avvio, loop di polling, eventi tra Go e frontend
orca.go               client del runtime di Orca
state.go              voci, lampeggio, riepilogo per la traybar (logica pura, testata)
ui.go                 impostazioni, menu, traybar, posizione, disegno dell'icona
platform_*.go         codice specifico di Windows e macOS
frontend/index.html   la finestra (HTML/CSS/JS, senza build)
docs/spec.md          specifica: comportamento e decisioni
```

- Test: `go test ./...`
- L'icona dell'exe (`lollipop.ico`, `rsrc_windows_amd64.syso`) è disegnata dal codice in `ui.go`. Se cambi il
  disegno, rigenerala con `go generate`; `TestAppIcon` fallisce finché non lo fai.
