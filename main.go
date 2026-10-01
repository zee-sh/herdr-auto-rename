// herdr-pane-title labels a herdr pane with the session name of the coding
// agent running in it.
//
// Agents such as Claude Code put their session name in the terminal title,
// which herdr exposes as terminal_title_stripped. This binary copies that title
// into the pane's display_agent metadata, which herdr draws on the pane border
// (ui.show_agent_labels_on_pane_borders = true) when no manual pane name is set.
//
// Run from an event hook it updates the event's pane; run from the startup hook
// or the refresh action it sweeps every live agent.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	source   = "zee-sh.pane-title"
	maxRunes = 40
)

// Titles that are an agent's default rather than a session name.
var genericTitles = map[string]bool{
	"claude":      true,
	"claude code": true,
	"codex":       true,
	"opencode":    true,
}

type agent struct {
	PaneID        string `json:"pane_id"`
	Agent         string `json:"agent"`
	DisplayAgent  string `json:"display_agent"`
	TerminalTitle string `json:"terminal_title_stripped"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-pane-title:", err)
		os.Exit(1)
	}
}

func run() error {
	// Captured before any read so a slower concurrent run can't overwrite a
	// newer label: herdr drops reports with an older seq from the same source.
	seq := strconv.FormatInt(time.Now().UnixNano(), 10)

	event := os.Getenv("HERDR_PLUGIN_EVENT_JSON")
	debugLog(event)

	if event == "" {
		// Startup hook or refresh action.
		agents, err := listAgents()
		if err != nil {
			return err
		}
		var errs []error
		for _, a := range agents {
			errs = append(errs, label(a, seq))
		}
		return errors.Join(errs...)
	}

	var ev struct {
		Data struct {
			PaneID string `json:"pane_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(event), &ev); err != nil {
		return fmt.Errorf("decode event: %w", err)
	}
	if ev.Data.PaneID == "" {
		return nil
	}
	a, err := getAgent(ev.Data.PaneID)
	if err != nil || a == nil {
		return err
	}
	return label(a, seq)
}

// label sets or clears this plugin's display_agent entry for a's pane.
func label(a *agent, seq string) error {
	state := stateFile(a.PaneID)
	title := sessionTitle(a)

	if title == "" {
		// Only clear a label this plugin set, so a clear (which itself fires
		// an event) can't loop against a display_agent owned by another source.
		if state == "" || !exists(state) {
			return nil
		}
		_ = os.Remove(state)
		_, err := herdr("pane", "report-metadata", a.PaneID,
			"--source", source, "--agent", a.Agent, "--seq", seq, "--clear-display-agent")
		return err
	}

	if title == a.DisplayAgent {
		return nil
	}
	if _, err := herdr("pane", "report-metadata", a.PaneID,
		"--source", source, "--agent", a.Agent, "--seq", seq, "--display-agent", title); err != nil {
		return err
	}
	if state != "" {
		_ = os.WriteFile(state, []byte(title), 0o644)
	}
	return nil
}

// sessionTitle returns the label to show for a, or "" when the terminal title
// does not look like a session name.
func sessionTitle(a *agent) string {
	t := strings.TrimSpace(a.TerminalTitle)
	if t == "" || strings.EqualFold(t, a.Agent) || genericTitles[strings.ToLower(t)] {
		return ""
	}
	if r := []rune(t); len(r) > maxRunes {
		t = string(r[:maxRunes-1]) + "…"
	}
	return t
}

func getAgent(paneID string) (*agent, error) {
	out, err := herdr("agent", "get", paneID)
	if err != nil {
		// The pane has no agent (or is gone): nothing to label.
		if strings.Contains(err.Error(), "agent_not_found") || strings.Contains(err.Error(), "pane_not_found") {
			return nil, nil
		}
		return nil, err
	}
	var resp struct {
		Result struct {
			Agent *agent `json:"agent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode agent get: %w", err)
	}
	return resp.Result.Agent, nil
}

func listAgents() ([]*agent, error) {
	out, err := herdr("agent", "list")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Agents []*agent `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode agent list: %w", err)
	}
	return resp.Result.Agents, nil
}

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
		return nil, fmt.Errorf("herdr %s %s: %w: %s", args[0], args[1], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// stateFile is where the label last reported for paneID is remembered, or ""
// when herdr provided no state dir.
func stateFile(paneID string) string {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		return ""
	}
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "label-"+strings.ReplaceAll(paneID, ":", "_"))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// debugLog appends the raw event to $HERDR_PLUGIN_CONFIG_DIR/events.log when
// a file named "debug" exists there, for inspecting payload shapes.
func debugLog(event string) {
	dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	if dir == "" || !exists(filepath.Join(dir, "debug")) {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), event)
}
