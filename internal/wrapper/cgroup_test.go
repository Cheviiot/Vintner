//go:build linux

package wrapper

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCgroupHandleNilSafety(t *testing.T) {
	var h *cgroupHandle
	h.apply(&syscall.SysProcAttr{}) // must not panic
	h.kill()                        // must not panic
	h.close()                       // must not panic
}

// TestSweepOrphanedCgroupsRemovesEmptyOnes is a plain-filesystem test:
// sweepOrphanedCgroups only ever does filepath.Glob + os.Remove, so it
// doesn't need a real cgroupfs to exercise the actual logic (name-prefix
// matching, leaving non-matching siblings alone).
func TestSweepOrphanedCgroupsRemovesEmptyOnes(t *testing.T) {
	base := t.TempDir()
	orphan := filepath.Join(base, "vintner-12345-6789")
	keep := filepath.Join(base, "not-vintner-keep-me")
	if err := os.Mkdir(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(keep, 0o755); err != nil {
		t.Fatal(err)
	}

	sweepOrphanedCgroups(base)

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("expected orphaned %s to be removed", orphan)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("expected non-vintner-prefixed %s to be left alone: %v", keep, err)
	}
}

// TestNewCgroupSweepsRealOrphan reproduces the actual bug found in
// practice: an empty vintner-* cgroup directory left behind by a killed
// invocation (simulated here by just creating one by hand rather than
// actually SIGKILLing a real vintner process) must be gone after the next
// newCgroup() call, in the real cgroupfs.
func TestNewCgroupSweepsRealOrphan(t *testing.T) {
	requireCgroup(t) // skip if cgroups aren't usable here at all

	base, ok := ownCgroupPath()
	if !ok {
		t.Skip("ownCgroupPath() failed after requireCgroup() succeeded - unexpected, but not this test's concern")
	}
	orphan := filepath.Join(base, "vintner-fake-orphan-999999999")
	if err := os.Mkdir(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	// In case the sweep itself doesn't run for some reason, don't leave a
	// real orphan behind from this test.
	t.Cleanup(func() { os.Remove(orphan) })

	h := newCgroup()
	if h == nil {
		t.Fatal("newCgroup() returned nil right after requireCgroup() confirmed cgroups are usable")
	}
	defer h.close()

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("expected the manually-created orphan %s to be swept by newCgroup(), still exists", orphan)
	}
}

// TestCgroupCleanupAvailableMatchesNewCgroup checks CgroupCleanupAvailable
// (what `vintner doctor` reports) agrees with newCgroup's own success/
// failure in this environment, and that it doesn't leak the probe cgroup
// it creates to answer that question.
func TestCgroupCleanupAvailableMatchesNewCgroup(t *testing.T) {
	probe := newCgroup()
	wantAvailable := probe != nil
	if probe != nil {
		probe.close()
	}

	if got := CgroupCleanupAvailable(); got != wantAvailable {
		t.Errorf("CgroupCleanupAvailable() = %v, want %v (newCgroup() != nil)", got, wantAvailable)
	}
}

func requireCgroup(t *testing.T) *cgroupHandle {
	t.Helper()
	h := newCgroup()
	if h == nil {
		t.Skip("cgroup v2 not usable in this environment (no delegated write access, or v1-only) - skipping")
	}
	t.Cleanup(h.close)
	return h
}

func TestCgroupHandleKillsPlainChild(t *testing.T) {
	h := requireCgroup(t)

	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	h.apply(cmd.SysProcAttr)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sleep: %v", err)
	}

	h.kill()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cgroup.kill to kill the child, but it exited successfully")
		}
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		t.Fatal("child still running 3s after cgroup.kill")
	}
}

// TestCgroupHandleReachesSetsidGrandchild is the test that actually proves
// the fix, reproducing the real shape of the bug: an outer process in its
// own process group (standing in for the `wine` process
// setNewProcessGroup/forwardSignals target) that itself spawns a
// grandchild via `setsid` - exactly what Wine's ntdll does before exec'ing
// a CREATE_NO_WINDOW child (confirmed by reading dlls/ntdll/unix/
// process.c; see cgroup.go's doc comment for the full chain from MSBuild's
// NodeLauncher through Wine's kernelbase/ntdll). Killing the outer
// process's group must NOT reach the detached grandchild - reproducing the
// bug - while cg.kill() must.
func TestCgroupHandleReachesSetsidGrandchild(t *testing.T) {
	h := requireCgroup(t)

	// Prints the setsid'd grandchild's pid on its own line as soon as it's
	// backgrounded, then waits on the outer shell itself (which is what
	// SIGKILL-ing its group below actually kills).
	cmd := exec.Command("sh", "-c", "setsid sleep 30 & echo $!; wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	h.apply(cmd.SysProcAttr)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting outer shell: %v", err)
	}
	outerPid := cmd.Process.Pid

	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading grandchild pid: %v", err)
	}
	grandchildPid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parsing grandchild pid %q: %v", line, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Reproduce the bug: killing the outer process's own group (exactly
	// what setNewProcessGroup/forwardSignals/the pre-cgroup cmd.Cancel
	// did) kills the outer shell but must NOT reach the setsid()'d
	// grandchild, since setsid() gave it a brand new session/group of its
	// own the moment it was backgrounded.
	_ = syscall.Kill(-outerPid, syscall.SIGKILL)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("outer shell did not die within 3s of SIGKILL to its own group")
	}
	if err := syscall.Kill(grandchildPid, 0); err != nil {
		t.Fatalf("grandchild (pid %d) already gone after only killing the outer process group - "+
			"test no longer reproduces the bug this fix addresses, investigate before trusting cg.kill()'s coverage: %v",
			grandchildPid, err)
	}

	// Prove the fix: cgroup.kill reaches it anyway, since cgroup
	// membership - unlike process group - isn't something setsid() can
	// escape.
	h.kill()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(grandchildPid, 0) != nil {
			return // gone - fix confirmed
		}
		time.Sleep(20 * time.Millisecond)
	}
	syscall.Kill(grandchildPid, syscall.SIGKILL)
	t.Fatalf("setsid()'d grandchild (pid %d) still running 3s after cg.kill() - the fix did not reach it", grandchildPid)
}
