package wrapper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// waitDelay bounds how long cmd.Wait() itself may block after the process
// group has already been told to die (by a timeout or a forwarded signal) -
// usually resolved immediately, but a wedged Wine transport is exactly the
// case that isn't guaranteed to notice a plain SIGKILL right away.
const waitDelay = 5 * time.Second

// commandTimeout returns how long a single tool invocation may run before
// vintner kills it and reports a timeout, from VINTNER_TIMEOUT (a
// time.ParseDuration string, e.g. "30m", "2h"). Unset, empty, or invalid
// all mean "no timeout" (0) - the default stays a plain, unbounded build,
// matching every real build observed so far (Ogre3D's from-scratch build
// alone ran several minutes). This exists for exactly one failure mode: a
// wedged Wine-hosted process (a corrupted MSBuild node-reuse worker in the
// one confirmed case so far, but nothing about the mechanism is
// MSBuild-specific) that will otherwise never exit on its own, hanging
// vintner - and whatever's waiting on vintner - forever with no feedback.
// Automated/scripted callers that would rather fail loudly after N minutes
// than risk hanging indefinitely can set this; interactive use is
// unaffected unless it's set.
func commandTimeout() time.Duration {
	v := os.Getenv("VINTNER_TIMEOUT")
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

// toolCommand wraps the exec.Cmd every wine-hosted tool invocation is built
// from, plus enough state to tell a VINTNER_TIMEOUT kill apart from every
// other failure once Wait() returns.
type toolCommand struct {
	*exec.Cmd
	ctx     context.Context
	cancel  context.CancelFunc
	timeout time.Duration // 0 if VINTNER_TIMEOUT wasn't set
}

// newToolCommand builds a toolCommand: its own process group
// (setNewProcessGroup) and, when VINTNER_TIMEOUT is set, a deadline that
// kills the *whole group* - not just the immediate `wine` process, since a
// wedged Wine-hosted child surviving past its parent is exactly the
// scenario this needs to reach - if the tool hasn't finished in time.
//
// Callers must defer the returned cleanup func, and should call
// timedOut() after Wait() returns to tell a timeout-triggered kill apart
// from every other failure.
func newToolCommand(name string, args ...string) (tc *toolCommand, cleanup func()) {
	timeout := commandTimeout()
	if timeout <= 0 {
		cmd := exec.Command(name, args...)
		setNewProcessGroup(cmd)
		return &toolCommand{Cmd: cmd, ctx: context.Background()}, func() {}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	cmd := exec.CommandContext(ctx, name, args...)
	setNewProcessGroup(cmd)
	// cmd.Cancel's default (Go 1.20+) only signals the immediate child;
	// override it to reach the whole process group, same as
	// forwardSignals - the wedged process a timeout exists to clean up is
	// typically under wine, not wine itself.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = waitDelay
	tc = &toolCommand{Cmd: cmd, ctx: ctx, cancel: cancel, timeout: timeout}
	return tc, cancel
}

// timedOut reports whether this command was killed by its own
// VINTNER_TIMEOUT deadline rather than exiting (however it exited) on its
// own - call after Wait() returns a non-nil error.
func (tc *toolCommand) timedOut() bool {
	return tc.ctx.Err() == context.DeadlineExceeded
}

func (tc *toolCommand) timeoutMessage() string {
	return fmt.Sprintf("vintner: %s: timed out after %s (VINTNER_TIMEOUT), killed", tc.Path, tc.timeout)
}
