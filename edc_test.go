package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeEDC drops a pid-<pid>.json state file like edc does.
func writeEDC(t *testing.T, dir string, pid, port int) {
	t.Helper()
	body := fmt.Sprintf(`{"port":%d,"pid":%d,"bind":"0.0.0.0"}`, port, pid)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("pid-%d.json", pid)), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInjectPortFromEDC(t *testing.T) {
	dir := t.TempDir()
	writeEDC(t, dir, 200, 8793) // the session's edc: pid 200, child of claude 100
	writeEDC(t, dir, 400, 9999) // another session's edc: pid 400, child of claude 300
	writeEDC(t, dir, 500, 7777) // dead edc

	// process table: 200 -> 150 -> 100 (claude); 400 -> 300; 500 -> 100 but dead
	parent := map[int]int{200: 150, 150: 100, 400: 300, 500: 100}
	alive := func(pid int) bool { return pid != 500 }
	ppid := func(pid int) int { return parent[pid] }

	if got := injectPortFromEDC(dir, 100, alive, ppid); got != 8793 {
		t.Fatalf("want 8793 for claude 100, got %d", got)
	}
	if got := injectPortFromEDC(dir, 300, alive, ppid); got != 9999 {
		t.Fatalf("want 9999 for claude 300, got %d", got)
	}
	// claude 999 owns no live edc
	if got := injectPortFromEDC(dir, 999, alive, ppid); got != 0 {
		t.Fatalf("want 0 for unrelated claude, got %d", got)
	}
}

func TestInjectPortFromEDC_DeadInjectorIgnored(t *testing.T) {
	dir := t.TempDir()
	writeEDC(t, dir, 500, 7777) // the only match is dead
	alive := func(int) bool { return false }
	ppid := func(pid int) int { return map[int]int{500: 100}[pid] }
	if got := injectPortFromEDC(dir, 100, alive, ppid); got != 0 {
		t.Fatalf("a dead injector must not be claimed, got %d", got)
	}
}

func TestInjectPortFromEDC_Guards(t *testing.T) {
	alive := func(int) bool { return true }
	ppid := func(int) int { return 0 }
	// claudePID <= 1 never matches (avoids claiming via the init/reparent ancestor)
	if got := injectPortFromEDC(t.TempDir(), 1, alive, ppid); got != 0 {
		t.Fatalf("claudePID 1 must return 0, got %d", got)
	}
	// missing dir is a clean 0, not a panic
	if got := injectPortFromEDC(filepath.Join(t.TempDir(), "nope"), 100, alive, ppid); got != 0 {
		t.Fatalf("missing dir must return 0, got %d", got)
	}
}

func TestInjectPortFromEDC_MalformedSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pid-200.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeEDC(t, dir, 210, 0) // port 0 → skipped
	writeEDC(t, dir, 220, 8080)
	parent := map[int]int{200: 100, 210: 100, 220: 100}
	alive := func(int) bool { return true }
	ppid := func(pid int) int { return parent[pid] }
	if got := injectPortFromEDC(dir, 100, alive, ppid); got != 8080 {
		t.Fatalf("want the one valid file's port 8080, got %d", got)
	}
}

func TestDescendsFrom_CycleCapped(t *testing.T) {
	// a parent cycle must not loop forever
	ppid := func(pid int) int { return map[int]int{2: 3, 3: 2}[pid] }
	if descendsFrom(2, 100, ppid) {
		t.Fatal("cycle that never reaches the ancestor must return false")
	}
}
