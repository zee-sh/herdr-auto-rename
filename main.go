// herdr-pane-title labels a herdr pane with the session name of the coding
// agent running in it.
//
// Agents such as Claude Code put their session name in the terminal title,
// which herdr exposes as terminal_title_stripped. This binary copies that title
// into:
//   - the pane's display_agent metadata, which herdr draws on split pane
//     borders (ui.show_agent_labels_on_pane_borders = true) when no manual
//     pane name is set;
//   - the tab label, for tabs holding exactly one agent whose label is still
//     herdr's default number (or one this plugin set). Custom tab names are
//     never touched.
//
// A background watcher (see watch.go) reacts to title changes as they happen;
// the startup hook and refresh action sweep everything and start the watcher.
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
	source      = "zee-sh.pane-title"
	maxPaneRune = 40
	maxTabRune  = 24
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
	TabID         string `json:"tab_id"`
	Agent         string `json:"agent"`
	DisplayAgent  string `json:"display_agent"`
	TerminalTitle string `json:"terminal_title_stripped"`
}

func main() {
	var err error
	if len(os.Args) > 1 {
		if os.Args[1] != "watch" {
			fmt.Fprintf(os.Stderr, "usage: %s [watch]\n", os.Args[0])
			os.Exit(2)
		}
		err = watch()
	} else {
		err = hook()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "herdr-pane-title:", err)
		os.Exit(1)
	}
}

// hook runs as a herdr startup hook, event hook or action. The watcher does
// the real work; hooks make sure it is running, and cover for it if it can't
// start.
func hook() error {
	event := os.Getenv("HERDR_PLUGIN_EVENT_JSON")
	debugLog(event)

	running := watcherRunning()
	if !running {
		if err := startWatcher(); err != nil {
			fmt.Fprintln(os.Stderr, "herdr-pane-title: start watcher:", err)
		} else if event == "" {
			return nil // a new watcher sweeps as soon as it subscribes
		}
	}
	if event == "" {
		// Startup hook or refresh action.
		return sweep(newSeq())
	}
	if running {
		return nil
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
	return handlePane(ev.Data.PaneID, newSeq())
}

// newSeq returns a report sequence number. Take it before reading state so a
// slower concurrent run can't overwrite a newer label: herdr drops reports
// with an older seq from the same source.
func newSeq() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// sweep labels every live agent pane and every tab.
func sweep(seq string) error {
	agents, err := listAgents()
	if err != nil {
		return err
	}
	tabs, err := listTabs()
	if err != nil {
		return err
	}
	var errs []error
	for _, a := range agents {
		errs = append(errs, label(a, seq))
	}
	for _, t := range tabs {
		errs = append(errs, labelTab(t, agents))
	}
	return errors.Join(errs...)
}

// handlePane labels paneID and its tab.
func handlePane(paneID, seq string) error {
	a, err := getAgent(paneID)
	if err != nil {
		return err
	}
	tabID := ""
	if a != nil {
		if err := label(a, seq); err != nil {
			return err
		}
		tabID = a.TabID
	} else if tabID, err = paneTab(paneID); err != nil || tabID == "" {
		return err
	}
	return refreshTab(tabID)
}

// label sets or clears this plugin's display_agent entry for a's pane.
func label(a *agent, seq string) error {
	state := stateFile(a.PaneID)
	title := truncate(sessionTitle(a), maxPaneRune)

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
	return t
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

type tab struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

// refreshTab relabels one tab against the current agent list.
func refreshTab(tabID string) error {
	t, err := getTab(tabID)
	if err != nil || t == nil {
		return err
	}
	agents, err := listAgents()
	if err != nil {
		return err
	}
	return labelTab(t, agents)
}

// labelTab names t after its agent's session when it holds exactly one agent,
// and gives the tab back its position number once that no longer holds. A tab
// is only touched while its label is a default number or the name this plugin
// last set, so a name the user typed always wins.
func labelTab(t *tab, agents []*agent) error {
	var in []*agent
	for _, a := range agents {
		if a.TabID == t.TabID {
			in = append(in, a)
		}
	}
	want := ""
	if len(in) == 1 {
		want = truncate(sessionTitle(in[0]), maxTabRune)
	}

	state := stateFile("tab-" + t.TabID)
	set := ""
	if b, err := os.ReadFile(state); err == nil {
		set = string(b)
	}
	ours := set != "" && t.Label == set
	if !ours && !isDefaultLabel(t.Label) {
		return nil
	}

	if want == "" {
		if !ours {
			return nil
		}
		n, err := tabPosition(t)
		if err != nil {
			return err
		}
		want = strconv.Itoa(n)
		_ = os.Remove(state)
	} else if state != "" {
		_ = os.WriteFile(state, []byte(want), 0o644)
	}
	if t.Label == want {
		return nil
	}
	_, err := herdr("tab", "rename", t.TabID, want)
	return err
}

// isDefaultLabel reports whether label looks like herdr's generated tab
// label, a per-workspace position number.
func isDefaultLabel(label string) bool {
	n, err := strconv.Atoi(label)
	return err == nil && n > 0
}

// tabPosition returns t's 1-based position in its workspace, which is the
// label herdr generates for it.
func tabPosition(t *tab) (int, error) {
	tabs, err := listTabs("--workspace", t.WorkspaceID)
	if err != nil {
		return 0, err
	}
	for i, o := range tabs {
		if o.TabID == t.TabID {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("tab %s not in workspace %s", t.TabID, t.WorkspaceID)
}

func listTabs(args ...string) ([]*tab, error) {
	out, err := herdr(append([]string{"tab", "list"}, args...)...)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Tabs []*tab `json:"tabs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode tab list: %w", err)
	}
	return resp.Result.Tabs, nil
}

func getTab(tabID string) (*tab, error) {
	out, err := herdr("tab", "get", tabID)
	if err != nil {
		if strings.Contains(err.Error(), "not_found") {
			return nil, nil
		}
		return nil, err
	}
	var resp struct {
		Result struct {
			Tab *tab `json:"tab"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode tab get: %w", err)
	}
	return resp.Result.Tab, nil
}

// paneTab returns the tab holding paneID, or "" when the pane is gone.
func paneTab(paneID string) (string, error) {
	out, err := herdr("pane", "get", paneID)
	if err != nil {
		if strings.Contains(err.Error(), "pane_not_found") {
			return "", nil
		}
		return "", err
	}
	var resp struct {
		Result struct {
			Pane struct {
				TabID string `json:"tab_id"`
			} `json:"pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("decode pane get: %w", err)
	}
	return resp.Result.Pane.TabID, nil
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

// stateFile is where the label last set for id (a pane, or "tab-<tab id>")
// is remembered, or "" when herdr provided no state dir.
func stateFile(id string) string {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		return ""
	}
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "label-"+strings.ReplaceAll(id, ":", "_"))
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
