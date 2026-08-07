package wrapper

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestForwardSignalsKillsChild verifies the actual mechanism that keeps a
// wine subprocess from being orphaned: a SIGTERM delivered to the current
// process (mimicking `kill <vintner-pid>`, not an interactive Ctrl-C) must
// reach a child started with setNewProcessGroup, even though it's no longer
// in the same process group.
func TestForwardSignalsKillsChild(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	setNewProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sleep: %v", err)
	}
	stop := forwardSignals(cmd.Process.Pid, nil)
	defer stop()

	// signal.Notify (inside forwardSignals) intercepts this rather than
	// letting it terminate the test binary itself.
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signaling self: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the child to be killed by the forwarded signal, but it exited successfully")
		}
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		t.Fatal("child was still running 3s after the signal should have been forwarded")
	}
}
