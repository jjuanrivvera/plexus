package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// edcStateDir is where the event-driven-claude injector writes its per-process
// state files. Each is named pid-<edcpid>.json and holds {"port":N,"pid":M,...}.
func edcStateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "edc")
}

// edcState is the slice of an edc state file plexus cares about.
type edcState struct {
	Port int `json:"port"`
	PID  int `json:"pid"`
}

// injectPortFromEDC returns the inject port of the edc injector that belongs to
// claudePID, or 0 if none is found. It exists because a session's inject port is
// only discoverable once edc has bound its listener and written its state file —
// which races the SessionStart register, so a detached session that registers
// first is stuck at inject_port=0 until something re-reads the state file. An idle
// detached session never fires PostToolUse (the busy-path re-read), so the
// keepalive heartbeat calls this to reclaim the port and stay injectable.
//
// edc names its state files by the edc process pid, not the session id, so the
// right file is the one whose (live) process descends from claudePID. alive and
// ppidOf are injected so the matching is unit-testable without real processes.
func injectPortFromEDC(dir string, claudePID int, alive func(int) bool, ppidOf func(int) int) int {
	if claudePID <= 1 {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var st edcState
		if json.Unmarshal(b, &st) != nil || st.Port == 0 || st.PID == 0 {
			continue
		}
		if !alive(st.PID) {
			continue
		}
		if descendsFrom(st.PID, claudePID, ppidOf) {
			return st.Port
		}
	}
	return 0
}

// descendsFrom reports whether pid is claudePID or one of its descendants, walking
// up the parent chain. The hop cap guards against a cycle or a re-parented pid.
func descendsFrom(pid, ancestor int, ppidOf func(int) int) bool {
	for hops := 0; pid > 1 && hops < 24; hops++ {
		if pid == ancestor {
			return true
		}
		pid = ppidOf(pid)
	}
	return pid == ancestor
}

// injectPortFromEDCDefault is the production wrapper: the real edc state dir plus
// the real process seams. Returns 0 on any miss so callers can fall through.
func injectPortFromEDCDefault(claudePID int) int {
	return injectPortFromEDC(edcStateDir(), claudePID, procAlive, procPPID)
}

// procAlive reports whether pid is a live process (signal 0 probe; EPERM still
// means it exists, just not ours to signal).
func procAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// procPPID returns pid's parent pid via `ps`, which is portable across macOS and
// Linux (the same tool the shell hooks use); 0 if it can't be determined.
func procPPID(pid int) int {
	if pid <= 1 {
		return 0
	}
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	ppid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return ppid
}
