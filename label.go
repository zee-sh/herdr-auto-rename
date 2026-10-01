package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	maxPaneRunes = 40
	maxTabRunes  = 24
)

// Titles that are an agent's or shell's default rather than a session name.
var genericTitles = map[string]bool{
	"claude": true, "claude code": true,
	"codex": true, "opencode": true, "gemini": true, "gemini cli": true,
	"pi": true, "zsh": true, "bash": true, "fish": true, "sh": true,
}

// Shell-prompt titles (user@host:~/dir) and bare paths are what a terminal
// shows when the agent sets no title of its own.
var promptOrPath = regexp.MustCompile(`^[\w.-]+@[\w.-]+[: ]|^[~/]\S*$`)

// sessionTitle returns a's session name, or "" when its terminal title does
// not look like one.
func sessionTitle(a *agent) string {
	t := strings.TrimSpace(a.TerminalTitle)
	switch {
	case t == "",
		strings.EqualFold(t, a.Agent),
		genericTitles[strings.ToLower(t)],
		promptOrPath.MatchString(t):
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

// labelPane sets or clears this plugin's display_agent entry for a's pane.
// The entry is scoped to the agent, so herdr drops it when the agent exits.
func labelPane(a *agent, seq string) error {
	state := stateFile("pane-" + a.PaneID)
	title := truncate(sessionTitle(a), maxPaneRunes)

	if title == "" {
		// Only clear a label this plugin set, so a clear (which itself fires
		// an event) can't loop against a display_agent owned by another source.
		if !exists(state) {
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
	return os.WriteFile(state, []byte(title), 0o644)
}

// refreshTab relabels one tab against the current agent list.
func refreshTab(tabID string) error {
	t, err := getTab(tabID)
	if err != nil || t == nil {
		return err
	}
	tabs, err := listTabs("--workspace", t.WorkspaceID)
	if err != nil {
		return err
	}
	agents, err := listAgents()
	if err != nil {
		return err
	}
	for _, o := range tabs {
		if o.TabID == tabID {
			return labelTab(o, agents)
		}
	}
	return nil
}

// labelTab names t after its agent's session when it holds exactly one agent,
// and otherwise shows its position number.
//
// A tab is only touched while it carries herdr's own default label (its
// position) or the label this plugin last set, so a name the user typed always
// wins. herdr can't hand a renamed tab back to automatic numbering, so once
// the session label goes the plugin keeps owning the number it writes and
// keeps it in step with the tab's position.
func labelTab(t *tab, agents []*agent) error {
	var in []*agent
	for _, a := range agents {
		if a.TabID == t.TabID {
			in = append(in, a)
		}
	}
	want := ""
	if len(in) == 1 {
		want = truncate(sessionTitle(in[0]), maxTabRunes)
	}

	pos := strconv.Itoa(t.Pos)
	state := stateFile("tab-" + t.TabID)
	set := ""
	if b, err := os.ReadFile(state); err == nil {
		set = string(b)
	}
	ours := set != "" && t.Label == set
	if !ours && t.Label != pos {
		return nil
	}
	if want == "" {
		if !ours {
			return nil // herdr's own number; already right
		}
		want = pos
	}
	if want != set {
		if err := os.WriteFile(state, []byte(want), 0o644); err != nil {
			return err
		}
	}
	if t.Label == want {
		return nil
	}
	_, err := herdr("tab", "rename", t.TabID, want)
	return err
}
