// herdr-auto-rename names herdr tabs and pane borders after the session name of
// the coding agent running in them, exactly as the agent sets it (for Claude
// Code: the session title, including /rename).
//
// Agents put their session name in the terminal title, which herdr exposes as
// terminal_title_stripped. That title goes into:
//   - the pane's display_agent metadata, which herdr shows on split pane
//     borders and in the agent sidebar; display-only, never a pane rename;
//   - the tab label, for tabs holding exactly one agent whose label is still
//     herdr's default number or one this plugin set. Names typed by the user
//     are never touched.
//
// A background watcher (watch.go) reacts to title changes as they happen; the
// startup hook and refresh action sweep everything and start the watcher.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const source = "zee-sh.auto-rename"

func main() {
	var err error
	switch {
	case len(os.Args) == 1:
		err = hook()
	case os.Args[1] == "watch":
		err = watch()
	case os.Args[1] == "restart":
		err = restartWatcher()
	default:
		fmt.Fprintf(os.Stderr, "usage: %s [watch|restart]\n", os.Args[0])
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "herdr-auto-rename:", err)
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
			fmt.Fprintln(os.Stderr, "herdr-auto-rename: start watcher:", err)
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
	prev := map[string]string{}
	for _, a := range agents {
		prev[a.PaneID] = paneLabel(a.PaneID)
		errs = append(errs, labelPane(a, seq))
	}
	for _, t := range tabs {
		errs = append(errs, labelTab(t, agents, prev))
	}
	return errors.Join(errs...)
}

// handlePane labels paneID and its tab.
func handlePane(paneID, seq string) error {
	a, err := getAgent(paneID)
	if err != nil {
		return err
	}
	tabID, prev := "", paneLabel(paneID)
	if a != nil {
		if err := labelPane(a, seq); err != nil {
			return err
		}
		tabID = a.TabID
	} else if tabID, err = paneTab(paneID); err != nil || tabID == "" {
		return err
	}
	return refreshTab(tabID, map[string]string{paneID: prev})
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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
