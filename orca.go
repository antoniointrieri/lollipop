package main

// Client for Orca's local runtime. The protocol is undocumented (see docs/brief.md): one JSON line per request
// {id, authToken, method, params}, one JSON line per response. The methods mirror the documented CLI commands
// "orca worktree ps --json" and "orca terminal list --json".
// ponytail: undocumented transport; if it breaks, fall back to running the orca CLI (~1 s per call)

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

var errOrcaAbsent = errors.New("Orca is not running")

type orcaClient struct {
	mu    sync.Mutex
	conn  net.Conn
	r     *bufio.Reader
	token string // from orca-runtime.json: never log it or put it in an error
	pid   int
}

func orcaDir() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "orca")
}

func (c *orcaClient) connect() error {
	raw, err := os.ReadFile(filepath.Join(orcaDir(), "orca-runtime.json"))
	if err != nil {
		return errOrcaAbsent
	}
	var rt struct {
		PID        int
		AuthToken  string
		Transports []struct{ Kind, Endpoint string }
	}
	if err := json.Unmarshal(raw, &rt); err != nil {
		return errors.New(tr("unreadable orca-runtime.json", "orca-runtime.json illeggibile"))
	}
	for _, t := range rt.Transports {
		if t.Kind != "named-pipe" && t.Kind != "unix" {
			continue
		}
		conn, err := dial(t.Kind, t.Endpoint)
		if err != nil {
			return errOrcaAbsent // stale runtime file: Orca was closed
		}
		c.conn, c.r, c.token, c.pid = conn, bufio.NewReader(conn), rt.AuthToken, rt.PID
		return nil
	}
	return errors.New(tr("orca-runtime.json: no local transport", "orca-runtime.json: nessun trasporto locale"))
}

// call reconnects lazily: the endpoint changes every time Orca restarts.
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
	for err == nil && resp.ID != "lollipop" { // skip lines that aren't a reply to us
		var line []byte
		if line, err = c.r.ReadBytes('\n'); err == nil {
			resp.ID = ""
			err = json.Unmarshal(line, &resp)
		}
	}
	if err != nil {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf(tr("lost connection to Orca: %w", "connessione a Orca persa: %w"), err)
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

func (c *orcaClient) poll() (snapshot, error) {
	var wt worktreePS
	var tl terminalList
	if err := c.call("worktree.ps", map[string]int{"limit": 500}, &wt); err != nil {
		return snapshot{}, err
	}
	if err := c.call("terminal.list", map[string]bool{"includeVisualLayouts": true}, &tl); err != nil {
		return snapshot{}, err
	}
	// worktree.ps doesn't expose workingMode; Orca writes it here ("monitoring" = turn done, background shells alive).
	// ponytail: internal Orca file; if its format changes only the blue state is lost
	var hooks lastStatus
	if raw, err := os.ReadFile(filepath.Join(orcaDir(), "agent-hooks", "last-status.json")); err == nil {
		_ = json.Unmarshal(raw, &hooks)
	}
	return buildSnapshot(wt, tl, hooks), nil
}
