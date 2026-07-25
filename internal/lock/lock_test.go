package lock

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireAndRelease(t *testing.T) {
	dest := t.TempDir()

	unlock, err := Acquire(dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, FileName)); err != nil {
		t.Errorf("expected the lock file to exist while held: %v", err)
	}
	unlock()

	// Released - a second Acquire against the same dest must now succeed.
	unlock2, err := Acquire(dest)
	if err != nil {
		t.Fatalf("Acquire after release failed: %v", err)
	}
	unlock2()
}

func TestAcquireCreatesDestIfMissing(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "does", "not", "exist", "yet")
	unlock, err := Acquire(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if fi, err := os.Stat(dest); err != nil || !fi.IsDir() {
		t.Errorf("expected Acquire to create %s, stat err: %v", dest, err)
	}
}

func TestAcquireFailsWhileAlreadyHeld(t *testing.T) {
	dest := t.TempDir()

	unlock, err := Acquire(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	if _, err := Acquire(dest); err == nil {
		t.Fatal("expected a second Acquire against the same dest, while the first is still held, to fail")
	}
}

func TestAcquireSucceedsAfterHolderReleases(t *testing.T) {
	dest := t.TempDir()

	unlock1, err := Acquire(dest)
	if err != nil {
		t.Fatal(err)
	}
	unlock1()

	unlock2, err := Acquire(dest)
	if err != nil {
		t.Fatalf("Acquire should succeed once the first holder released: %v", err)
	}
	unlock2()
}

// TestAcquireFailsAcrossRealProcesses is the real end-to-end check: flock
// is per-open-file-description, not per-process or per-thread, so a lock
// held by *this* test process via one fd could in principle still be
// re-acquirable by another fd in the same process depending on the
// platform's exact semantics. Spawning this test binary as a genuinely
// separate child process (via the standard TestMain re-exec trick) and
// having it hold the lock while the parent tries to acquire it is what
// actually proves two independent `vintner download`/`install` processes
// contend correctly, not just two Go-level calls in one process.
func TestAcquireFailsAcrossRealProcesses(t *testing.T) {
	if os.Getenv("VINTNER_LOCK_TEST_HOLD") != "" {
		unlock, err := Acquire(os.Getenv("VINTNER_LOCK_TEST_HOLD"))
		if err != nil {
			os.Exit(2)
		}
		defer unlock()
		// Signal readiness, then wait to be killed by the parent. A plain
		// `select {}` here would have zero other goroutines able to ever
		// wake it, which Go's runtime provably detects as a deadlock and
		// crashes on ("fatal error: all goroutines are asleep") - a real
		// timer avoids that.
		os.Stdout.WriteString("locked\n")
		time.Sleep(time.Minute)
	}

	dest := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(exe, "-test.run=TestAcquireFailsAcrossRealProcesses")
	cmd.Env = append(os.Environ(), "VINTNER_LOCK_TEST_HOLD="+dest)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	buf := make([]byte, len("locked\n"))
	if _, err := io.ReadFull(stdout, buf); err != nil || string(buf) != "locked\n" {
		t.Fatalf("child process didn't report holding the lock: %v (%q)", err, buf)
	}

	if _, err := Acquire(dest); err == nil {
		t.Fatal("expected Acquire to fail while a separate process holds the lock")
	}
}
