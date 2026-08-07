package wrapper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	cg      *cgroupHandle // nil if cgroups aren't usable here
}

// newToolCommand builds a toolCommand: its own process group
// (setNewProcessGroup) plus, where available, a cgroup (see cgroup.go) -
// and, when VINTNER_TIMEOUT is set, a deadline that kills through both -
// not just the immediate `wine` process, since a wedged Wine-hosted child
// surviving past its parent is exactly the scenario this needs to reach -
// if the tool hasn't finished in time.
//
// Callers must defer the returned cleanup func, and should call
// timedOut() after Wait() returns to tell a timeout-triggered kill apart
// from every other failure.
func newToolCommand(name string, args ...string) (tc *toolCommand, cleanup func()) {
	cg := newCgroup()
	timeout := commandTimeout()
	if timeout <= 0 {
		cmd := exec.Command(name, args...)
		setNewProcessGroup(cmd)
		cg.apply(cmd.SysProcAttr)
		return &toolCommand{Cmd: cmd, ctx: context.Background(), cg: cg}, func() { cg.close() }
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	cmd := exec.CommandContext(ctx, name, args...)
	setNewProcessGroup(cmd)
	cg.apply(cmd.SysProcAttr)
	// cmd.Cancel's default (Go 1.20+) only signals the immediate child;
	// override it to killAll (signals.go) instead, same as forwardSignals'
	// own escalation - the wedged process a timeout exists to clean up is
	// typically under wine, not wine itself, and can have setsid()'d itself
	// out of reach of a plain process-group kill (see cgroup.go's doc
	// comment for the full chain killAll's cgroup half exists for).
	cmd.Cancel = func() error {
		pid := 0
		if cmd.Process != nil {
			pid = cmd.Process.Pid
		}
		killAll(pid, cg)
		return nil
	}
	cmd.WaitDelay = waitDelay
	tc = &toolCommand{Cmd: cmd, ctx: ctx, cancel: cancel, timeout: timeout, cg: cg}
	return tc, func() { cancel(); cg.close() }
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
