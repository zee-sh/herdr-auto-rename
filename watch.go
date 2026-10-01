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
	"syscall"
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
}

var errEventsLost = errors.New("events lost")

func watch() error {
	lock, err := lockWatcher()
	if err != nil || lock == nil {
		return err // nil lock: another watcher holds it
	}
	defer lock.Close()

	logf, err := os.OpenFile(filepath.Join(stateDir(), "watch.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
		case "pane_closed", "pane_moved", "pane_exited":
			// The pane (or its id) is gone, so recheck every tab.
			logErr(sweep(newSeq()))
		}
	}
	return sc.Err()
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

// lockPath is per socket, so each named herdr session gets its own watcher.
func lockPath() string {
	sum := sha256.Sum256([]byte(os.Getenv("HERDR_SOCKET_PATH")))
	return filepath.Join(stateDir(), "watch-"+hex.EncodeToString(sum[:6])+".lock")
}

func stateDir() string {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), source)
	}
	_ = os.MkdirAll(dir, 0o755)
	return dir
}
