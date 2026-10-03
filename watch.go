package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// herdr only passes a fixed set of events to plugin hooks, and pane.updated
// (which fires when a pane's terminal title changes, e.g. on Claude Code's
// /rename) is not one of them. The watcher subscribes to it over the socket
// instead. One watcher runs per herdr server, guarded by a lock file; it exits
// when the server closes the socket and the next startup hook starts a new one.

var subscriptions = []string{
	"pane.updated",        // title changes, agent reports and releases
	"pane.agent_detected", // a new agent can change its tab's agent count
	"pane.closed",
	"pane.moved",
	"pane.exited",
	"tab.created", // tab events shift positions, and so the numbers we own
	"tab.closed",
	"tab.moved",
}

var (
	errEventsLost = errors.New("events lost")
	errInactive   = errors.New("plugin disabled, unlinked or moved")
)

// activeCheckEvery bounds how often the watcher asks herdr whether the plugin
// is still enabled. herdr has no event for that, so the check runs lazily
// before handling an event: an idle watcher does no work at all.
const activeCheckEvery = 10 * time.Second

func watch() error {
	lock, err := lockWatcher()
	if err != nil || lock == nil {
		return err // nil lock: another watcher holds it
	}
	defer lock.Close()

	logf, err := os.OpenFile(filepath.Join(sessionDir(), "watch.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		log.SetOutput(logf)
		defer logf.Close()
	}
	log.Printf("watcher %d started for %s", os.Getpid(), os.Getenv("HERDR_SOCKET_PATH"))

	for {
		err := subscribe()
		if errors.Is(err, errEventsLost) {
			log.Print("events lost; resubscribing")
			continue
		}
		log.Printf("watcher %d exiting: %v", os.Getpid(), err)
		return err
	}
}

// subscribe streams events until the server closes the connection. Returns
// nil on a clean close, errEventsLost when the server dropped events.
func subscribe() error {
	conn, err := net.Dial("unix", os.Getenv("HERDR_SOCKET_PATH"))
	if err != nil {
		return err
	}
	defer conn.Close()

	type sub struct {
		Type string `json:"type"`
	}
	req := struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params struct {
			Subscriptions []sub `json:"subscriptions"`
		} `json:"params"`
	}{ID: source + ":watch", Method: "events.subscribe"}
	for _, s := range subscriptions {
		req.Params.Subscriptions = append(req.Params.Subscriptions, sub{s})
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}

	var checked time.Time
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	started := false
	for sc.Scan() {
		var msg struct {
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Event string `json:"event"`
			Data  struct {
				PaneID string `json:"pane_id"`
				Pane   struct {
					PaneID string `json:"pane_id"`
				} `json:"pane"`
			} `json:"data"`
		}
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			log.Printf("decode event: %v", err)
			continue
		}
		if msg.Error != nil {
			if msg.Error.Code == "events_lost" {
				return errEventsLost
			}
			return fmt.Errorf("subscribe: %s: %s", msg.Error.Code, msg.Error.Message)
		}
		if time.Since(checked) > activeCheckEvery {
			if !pluginActive() {
				return errInactive
			}
			checked = time.Now()
		}
		if !started {
			// The ack. Labels may have drifted while no one was listening.
			started = true
			logErr(sweep(newSeq()))
			continue
		}

		switch msg.Event {
		case "pane_updated":
			logErr(handlePane(msg.Data.Pane.PaneID, newSeq()))
		case "pane_agent_detected":
			logErr(handlePane(msg.Data.PaneID, newSeq()))
		case "pane_closed", "pane_moved", "pane_exited", "tab_created", "tab_closed", "tab_moved":
			// A pane id or tab position changed, so recheck everything.
			logErr(sweep(newSeq()))
		}
	}
	return sc.Err()
}

// pluginActive reports whether herdr still runs this plugin from this
// checkout. On a failed check it assumes yes, so a busy server can't stop it.
func pluginActive() bool {
	out, err := herdr("plugin", "list", "--plugin", source, "--json")
	if notFound(err) {
		return false
	} else if err != nil {
		return true
	}
	r, err := decode[struct {
		Plugins []struct {
			Enabled    bool   `json:"enabled"`
			PluginRoot string `json:"plugin_root"`
		}
	}](out, "plugin list")
	if err != nil {
		return true
	}
	root := os.Getenv("HERDR_PLUGIN_ROOT")
	for _, p := range r.Plugins {
		if p.Enabled && (root == "" || p.PluginRoot == root) {
			return true
		}
	}
	return false
}

func logErr(err error) {
	if err != nil {
		log.Print(err)
	}
}

// startWatcher launches a detached watcher unless one is already running.
func startWatcher() error {
	if watcherRunning() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "watch")
	// Own session so herdr doesn't wait on or signal it with the hook; stdio
	// goes to /dev/null so herdr isn't left waiting for the pipes to close.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// watcherRunning reports whether a watcher holds the lock for this server.
func watcherRunning() bool {
	f, err := os.OpenFile(lockPath(), os.O_RDONLY|os.O_CREATE, 0o644)
	if err != nil {
		return false
	}
	defer f.Close()
	return syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) != nil
}

// lockWatcher takes the watcher lock for this server, or returns nil if
// another watcher holds it. The lock lasts until the returned file is closed.
func lockWatcher() (*os.File, error) {
	f, err := os.OpenFile(lockPath(), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, nil
	}
	_ = f.Truncate(0)
	fmt.Fprintln(f, os.Getpid())
	return f, nil
}

// restartWatcher replaces the running watcher, e.g. after an upgrade, so the
// new binary takes over without restarting the herdr server.
func restartWatcher() error {
	if b, err := os.ReadFile(lockPath()); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && watcherRunning() {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				return fmt.Errorf("stop watcher %d: %w", pid, err)
			}
			for deadline := time.Now().Add(5 * time.Second); watcherRunning(); {
				if time.Now().After(deadline) {
					return fmt.Errorf("watcher %d still running", pid)
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
	return startWatcher()
}

func lockPath() string {
	return filepath.Join(sessionDir(), "watch.lock")
}

// sessionDir holds the state of one herdr server. herdr gives a plugin the
// same state dir in every named session, and tab/pane ids repeat between
// sessions, so state is keyed by the server's socket.
func sessionDir() string {
	sum := sha256.Sum256([]byte(os.Getenv("HERDR_SOCKET_PATH")))
	dir := filepath.Join(stateDir(), "session-"+hex.EncodeToString(sum[:6]))
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// pruneState deletes remembered labels for panes and tabs that no longer
// exist, given every live agent pane and every tab.
func pruneState(agents []*agent, tabs []*tab) {
	keep := map[string]bool{}
	for _, a := range agents {
		keep[stateFile("pane-"+a.PaneID)] = true
	}
	for _, t := range tabs {
		keep[stateFile("tab-"+t.TabID)] = true
	}
	files, _ := filepath.Glob(filepath.Join(sessionDir(), "label-*"))
	for _, f := range files {
		if !keep[f] {
			_ = os.Remove(f)
		}
	}
}

// stateFile is where the label last set for id ("pane-<id>" or "tab-<id>")
// is remembered.
func stateFile(id string) string {
	return filepath.Join(sessionDir(), "label-"+strings.ReplaceAll(id, ":", "_"))
}

func stateDir() string {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), source)
	}
	_ = os.MkdirAll(dir, 0o755)
	return dir
}
