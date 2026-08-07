package wrapper

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// killGrace bounds how long a forwarded SIGINT/SIGTERM gets to make a
// subprocess tree exit on its own before escalating to SIGKILL - long
// enough for wineserver to tear down a Windows process tree cleanly, short
// enough that an unresponsive one doesn't hang vintner's own shutdown.
const killGrace = 5 * time.Second

// setNewProcessGroup puts cmd's eventual child in its own process group
// (pgid = its own pid) instead of inheriting vintner's. Without this, a
// caller that signals vintner by PID alone (a CI runner enforcing a
// timeout, a supervisor's `kill <pid>`) never reaches the wine/wineserver
// tree underneath it, which is then reparented to init and keeps running -
// wasting CPU, holding file locks, leaving stray FIFOs/temp files behind.
// (Interactive Ctrl-C already reaches every process in the terminal's
// foreground group regardless of this, but forwardSignals below handles
// that case too now that the child has moved to its own group.)
func setNewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killAll delivers SIGKILL to pid's own process group (setNewProcessGroup
// makes pid its own group id too) and, if cg is non-nil, to its cgroup as
// well - the second, guaranteed-reach mechanism cgroupHandle exists for
// (see cgroup.go's doc comment: a Wine-hosted CREATE_NO_WINDOW child gets
// setsid()'d out of the process group before a plain group kill can ever
// reach it). pid == 0 skips the process-group kill (nothing to signal yet).
//
// Shared by both hard-kill paths needing this exact sequence: a
// VINTNER_TIMEOUT deadline (timeout.go's cmd.Cancel) and forwardSignals'
// own escalation below - previously duplicated between the two.
func killAll(pid int, cg *cgroupHandle) {
	if pid != 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	cg.kill()
}

// forwardSignals relays SIGINT/SIGTERM received by vintner itself to pid's
// entire process group (pid must be a process setNewProcessGroup started,
// making it also the group id), escalating after killGrace to killAll(pid,
// cg). Callers must call the returned stop func once the process has
// actually exited (e.g. right after cmd.Wait() returns), both to stop
// listening for signals and to cancel a pending escalation.
//
// Takes just the pid and cg it actually needs rather than the whole
// *toolCommand, so the background goroutine below - which runs for the
// entire lifetime of the wine-hosted invocation - doesn't keep the rest of
// toolCommand's *exec.Cmd (Env, Stdin/Stdout/Stderr buffers callers assign,
// SysProcAttr, ...) reachable for no reason.
func forwardSignals(pid int, cg *cgroupHandle) (stop func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})

	go func() {
		pgid := -pid
		for {
			select {
			case sig := <-sigCh:
				s, ok := sig.(syscall.Signal)
				if !ok {
					continue
				}
				_ = syscall.Kill(pgid, s)
				select {
				case <-time.After(killGrace):
					killAll(pid, cg)
				case <-done:
					return
				}
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(sigCh)
		close(done)
	}
}
