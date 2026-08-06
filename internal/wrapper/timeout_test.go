package wrapper

import (
	"syscall"
	"testing"
	"time"
)

func TestCommandTimeout(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		want time.Duration
	}{
		{"unset", "", 0},
		{"valid", "30m", 30 * time.Minute},
		{"invalid unit-less number", "30", 0},
		{"zero", "0s", 0},
		{"negative", "-5m", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VINTNER_TIMEOUT", tc.env)
			if got := commandTimeout(); got != tc.want {
				t.Errorf("commandTimeout() with VINTNER_TIMEOUT=%q = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

// TestNewToolCommandKillsOnTimeout is the real end-to-end check: start a
// process that would otherwise run far longer than the timeout (mimicking
// a wedged Wine-hosted tool), and confirm newToolCommand's deadline
// actually kills it - not just that timedOut() would report true in
// principle, but that Wait() actually returns, promptly, with the process
// gone.
func TestNewToolCommandKillsOnTimeout(t *testing.T) {
	t.Setenv("VINTNER_TIMEOUT", "300ms")

	tc, cleanup := newToolCommand("sleep", "30")
	defer cleanup()

	if err := tc.Start(); err != nil {
		t.Fatalf("starting sleep: %v", err)
	}
	pid := tc.Process.Pid

	done := make(chan error, 1)
	go func() { done <- tc.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected sleep 30 to be killed by the timeout, but it exited successfully")
		}
		if !tc.timedOut() {
			t.Errorf("Wait() returned an error (%v) but timedOut() = false", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("newToolCommand's timeout did not kill the process within 5s of a 300ms deadline")
	}

	// Belt-and-suspenders: the process should genuinely be gone, not just
	// reported as such. Signal 0 sends nothing but still fails with ESRCH
	// once the pid is gone - the standard Unix way to probe existence.
	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("pid %d still exists after the timeout killed it", pid)
	}
}

func TestNewToolCommandNoTimeoutByDefault(t *testing.T) {
	t.Setenv("VINTNER_TIMEOUT", "")

	tc, cleanup := newToolCommand("true")
	defer cleanup()

	if err := tc.Run(); err != nil {
		t.Fatalf("running `true` with no VINTNER_TIMEOUT set: %v", err)
	}
	if tc.timedOut() {
		t.Error("timedOut() = true for a command that finished well within any reasonable time, with no timeout configured")
	}
}

func TestTimeoutMessage(t *testing.T) {
	t.Setenv("VINTNER_TIMEOUT", "30m")
	tc, cleanup := newToolCommand("some-tool", "arg1")
	defer cleanup()

	got := tc.timeoutMessage()
	want := "vintner: some-tool: timed out after 30m0s (VINTNER_TIMEOUT), killed"
	if got != want {
		t.Errorf("timeoutMessage() = %q, want %q", got, want)
	}
}
