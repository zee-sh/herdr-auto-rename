// herdr-pane-title labels a herdr pane with the session name of the coding
// agent running in it.
//
// Agents such as Claude Code put their session name in the terminal title,
// which herdr exposes as terminal_title_stripped. On every agent event this
// binary copies that title into the pane's display_agent metadata, which herdr
// draws on the pane border (ui.show_agent_labels_on_pane_borders = true) when
// no manual pane name is set.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	event := os.Getenv("HERDR_PLUGIN_EVENT_JSON")
	debugLog(event)

	// Events carry the pane in their payload; actions only carry the focused pane.
	paneID := os.Getenv("HERDR_PANE_ID")
	if id := paneFrom(event, "pane_id"); id != "" {
		paneID = id
	} else if id := paneFrom(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"), "focused_pane_id"); id != "" {
		paneID = id
	}
	if paneID == "" {
		return nil
	}

	a, err := getAgent(paneID)
	if err != nil || a == nil {
		// No agent in the pane (or it just exited): nothing to label.
		return nil
	}

	title := sessionTitle(a)
	if title == "" || title == a.DisplayAgent {
		return nil
	}
	_, err = herdr("pane", "report-metadata", paneID,
		"--source", source,
		"--agent", a.Agent,
		"--display-agent", title)
	return err
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
		return nil, fmt.Errorf("herdr %s: %w: %s", args[0]+" "+args[1], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// paneFrom decodes raw JSON and returns the first string stored under key.
func paneFrom(raw, key string) string {
	var v any
	if raw == "" || json.Unmarshal([]byte(raw), &v) != nil {
		return ""
	}
	s, _ := findString(v, key)
	return s
}

// findString returns the first string value stored under key anywhere in v.
func findString(v any, key string) (string, bool) {
	switch v := v.(type) {
	case map[string]any:
		if s, ok := v[key].(string); ok && s != "" {
			return s, true
		}
		for _, child := range v {
			if s, ok := findString(child, key); ok {
				return s, true
			}
		}
	case []any:
		for _, child := range v {
			if s, ok := findString(child, key); ok {
				return s, true
			}
		}
	}
	return "", false
}

// debugLog appends the raw event to $HERDR_PLUGIN_CONFIG_DIR/events.log when
// a file named "debug" exists there, for inspecting payload shapes.
func debugLog(event string) {
	dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	if dir == "" {
		return
	}
	if _, err := os.Stat(dir + "/debug"); err != nil {
		return
	}
	f, err := os.OpenFile(dir+"/events.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), event)
}
