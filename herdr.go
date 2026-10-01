package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type agent struct {
	PaneID        string `json:"pane_id"`
	TabID         string `json:"tab_id"`
	Agent         string `json:"agent"`
	DisplayAgent  string `json:"display_agent"`
	TerminalTitle string `json:"terminal_title_stripped"`
}

type tab struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Pos         int    `json:"-"` // 1-based position in its workspace: herdr's default label
}

// herdr runs the herdr CLI and returns its stdout. Errors carry herdr's stderr,
// which holds a JSON error with a code such as "agent_not_found".
func herdr(args ...string) ([]byte, error) {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	var stderr bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("herdr %s: %w: %s", strings.Join(args[:2], " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func notFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "_not_found")
}

func decode[T any](out []byte, what string) (T, error) {
	var resp struct {
		Result T `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return resp.Result, fmt.Errorf("decode %s: %w", what, err)
	}
	return resp.Result, nil
}

// getAgent returns the agent in paneID, or nil when there is none.
func getAgent(paneID string) (*agent, error) {
	out, err := herdr("agent", "get", paneID)
	if notFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	r, err := decode[struct{ Agent *agent }](out, "agent get")
	return r.Agent, err
}

func listAgents() ([]*agent, error) {
	out, err := herdr("agent", "list")
	if err != nil {
		return nil, err
	}
	r, err := decode[struct{ Agents []*agent }](out, "agent list")
	return r.Agents, err
}

// listTabs lists tabs in workspace order and fills in their positions.
func listTabs(args ...string) ([]*tab, error) {
	out, err := herdr(append([]string{"tab", "list"}, args...)...)
	if err != nil {
		return nil, err
	}
	r, err := decode[struct{ Tabs []*tab }](out, "tab list")
	pos := map[string]int{}
	for _, t := range r.Tabs {
		pos[t.WorkspaceID]++
		t.Pos = pos[t.WorkspaceID]
	}
	return r.Tabs, err
}

// getTab returns tabID (without its position), or nil when it is gone.
func getTab(tabID string) (*tab, error) {
	out, err := herdr("tab", "get", tabID)
	if notFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	r, err := decode[struct{ Tab *tab }](out, "tab get")
	return r.Tab, err
}

// paneTab returns the tab holding paneID, or "" when the pane is gone.
func paneTab(paneID string) (string, error) {
	out, err := herdr("pane", "get", paneID)
	if notFound(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	r, err := decode[struct {
		Pane struct {
			TabID string `json:"tab_id"`
		}
	}](out, "pane get")
	return r.Pane.TabID, err
}
