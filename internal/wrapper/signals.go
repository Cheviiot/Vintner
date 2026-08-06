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

// forwardSignals relays SIGINT/SIGTERM received by vintner itself to tc's
// entire process group (tc.Process must have been started via
// setNewProcessGroup, making tc.Process.Pid also the group id), escalating
// after killGrace to a SIGKILL of that group plus - if tc.cg is non-nil - a
// cgroup kill, which alone is guaranteed to reach a Wine-hosted worker that
// setsid()'d itself out of the process group (see cgroup.go's doc comment).
// Callers must call the returned stop func once the process has actually
// exited (e.g. right after cmd.Wait() returns), both to stop listening for
// signals and to cancel a pending escalation.
func forwardSignals(tc *toolCommand) (stop func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})

	go func() {
		pgid := -tc.Process.Pid
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
					_ = syscall.Kill(pgid, syscall.SIGKILL)
					tc.cg.kill()
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
