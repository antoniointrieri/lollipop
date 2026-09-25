package main

// Client del runtime locale di Orca (protocollo non documentato, vedi docs/brief.md):
// una riga JSON {id, authToken, method, params} per richiesta, una riga di risposta.
// Connessione tenuta aperta; se cade (es. Orca riavviato: cambia endpoint) si riconnette alla richiesta dopo.
// ponytail: protocollo non documentato; se cambia, tornare a "orca worktree ps --json" / "orca terminal list --json"

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const requestTimeout = 2 * time.Second

type orcaClient struct {
	mu    sync.Mutex
	conn  net.Conn
	r     *bufio.Reader
	token string // segreto: mai loggato, stampato o messo in un errore
	pid   int
}

func orcaDir() string {
	d, _ := os.UserConfigDir() // %APPDATA% su Windows, ~/Library/Application Support su macOS
	return filepath.Join(d, "orca")
}

func (c *orcaClient) connect() error {
	raw, err := os.ReadFile(filepath.Join(orcaDir(), "orca-runtime.json"))
	if err != nil {
		return fmt.Errorf("Orca non avviato? %w", err)
	}
	var rt struct {
		PID        int
		AuthToken  string
		Transports []struct{ Kind, Endpoint string }
	}
	if err := json.Unmarshal(raw, &rt); err != nil {
		return errors.New("orca-runtime.json illeggibile")
	}
	for _, t := range rt.Transports {
		if t.Kind != "named-pipe" && t.Kind != "unix" {
			continue
		}
		conn, err := dial(t.Kind, t.Endpoint)
		if err != nil {
			return fmt.Errorf("connessione a Orca: %w", err)
		}
		c.conn, c.r, c.token, c.pid = conn, bufio.NewReader(conn), rt.AuthToken, rt.PID
		return nil
	}
	return errors.New("orca-runtime.json: nessun trasporto locale")
}

func (c *orcaClient) call(method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		if err := c.connect(); err != nil {
			return err
		}
	}
	req, _ := json.Marshal(map[string]any{"id": "lollipop", "authToken": c.token, "method": method, "params": params})
	var resp struct {
		ID     string
		OK     bool
		Result json.RawMessage
		Error  struct{ Code, Message string }
	}
	err := c.conn.SetDeadline(time.Now().Add(requestTimeout))
	if err == nil {
		_, err = c.conn.Write(append(req, '\n'))
	}
	for err == nil && resp.ID != "lollipop" { // salta eventuali righe non nostre
		var line []byte
		if line, err = c.r.ReadBytes('\n'); err == nil {
			resp.ID = ""
			err = json.Unmarshal(line, &resp)
		}
	}
	if err != nil {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("connessione a Orca persa: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("%s: %s", method, resp.Error.Message)
	}
	return json.Unmarshal(resp.Result, result)
}

func (c *orcaClient) orcaPID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pid
}

func (c *orcaClient) focusTerminal(handle string) error {
	var ignored any
	return c.call("terminal.focus", map[string]string{"terminal": handle}, &ignored)
}

// poll: agenti con un terminale vivo + pannello in primo piano dentro Orca.
func (c *orcaClient) poll() (snapshot, error) {
	var wt worktreePS
	var tl terminalList
	if err := c.call("worktree.ps", map[string]int{"limit": 500}, &wt); err != nil {
		return snapshot{}, err
	}
	if err := c.call("terminal.list", map[string]bool{"includeVisualLayouts": true}, &tl); err != nil {
		return snapshot{}, err
	}
	// worktree.ps non espone workingMode: Orca lo scrive qui ("monitoring" = turno finito ma shell/monitor in background).
	// ponytail: file interno di Orca, se cambia formato si perde solo il blu (resta giallo)
	var hooks lastStatus
	if raw, err := os.ReadFile(filepath.Join(orcaDir(), "agent-hooks", "last-status.json")); err == nil {
		_ = json.Unmarshal(raw, &hooks)
	}
	return buildSnapshot(wt, tl, hooks), nil
}
