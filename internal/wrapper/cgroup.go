//go:build linux

package wrapper

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// cgroupHandle wraps a per-invocation cgroup v2 directory used to guarantee
// cleanup reaches every descendant of a wine-hosted tool - including one
// that has detached itself from the Unix process group setNewProcessGroup
// relies on.
//
// A Wine-hosted process created with CREATE_NO_WINDOW (the ordinary case
// for a background worker, e.g. an MSBuild node-reuse node) makes Wine's
// ntdll call setsid() on it before exec (dlls/kernelbase/process.c maps
// CREATE_NO_WINDOW to CONSOLE_HANDLE_ALLOC_NO_WINDOW; dlls/ntdll/unix/
// process.c calls setsid() whenever that's the console handle - confirmed
// by reading Wine's own source). setsid() moves the process into a brand
// new Unix session and process group, permanently out of reach of
// syscall.Kill(-pid, ...) - exactly why a wedged MSBuild node worker
// survives past vintner's own process-group cleanup (see the doc comment
// on msbuildNodeReuseArgs in msbuildenv.go for the symptom this was first
// observed as).
//
// cgroup v2 membership doesn't have this hole: a process cannot leave its
// cgroup on its own (setsid() doesn't touch it), so cgroup.kill reliably
// reaches it. A nil *cgroupHandle means cgroups aren't usable here (v1-only
// system, no delegated write access, read-only /sys/fs/cgroup - all common
// in containers) - every method is a no-op on nil, so callers always fall
// back to today's process-group-only behavior rather than failing a build
// over this.
type cgroupHandle struct {
	dir string
	f   *os.File
}

// CgroupCleanupAvailable reports whether this process can actually create
// and use a cgroup v2 child - i.e. whether the guaranteed cleanup
// cgroupHandle provides (see its doc comment) is really in effect here,
// rather than every wine-hosted tool invocation silently falling back to
// process-group-only cleanup. Used by `vintner doctor` to surface that
// degradation instead of leaving it invisible. Does a real create-then-
// close probe rather than just checking for a v2 mount, since delegation
// (write access) is what actually varies between environments - common in
// containers without cgroup delegation, for example.
func CgroupCleanupAvailable() bool {
	h := newCgroup()
	if h == nil {
		return false
	}
	h.close()
	return true
}

// newCgroup creates a fresh child cgroup under vintner's own (delegated)
// cgroup and returns a handle ready for apply(), or nil if that isn't
// possible here.
func newCgroup() *cgroupHandle {
	base, ok := ownCgroupPath()
	if !ok {
		return nil
	}
	sweepOrphanedCgroups(base)
	dir := filepath.Join(base, fmt.Sprintf("vintner-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		os.Remove(dir)
		return nil
	}
	return &cgroupHandle{dir: dir, f: f}
}

// sweepOrphanedCgroups best-effort removes any leftover vintner-* cgroup
// directories under base from a previous invocation whose own process got
// SIGKILLed (an explicit kill, an OOM-killer, a crash) before its deferred
// cleanup() (see newToolCommand, timeout.go) could run - Go's defer cannot
// survive that, so an empty orphaned cgroup directory is the one thing
// left behind. Harmless on its own (see close()'s doc comment), but
// accumulates indefinitely over a machine's lifetime with nothing else to
// reclaim it - confirmed in practice: found 3 real orphaned, empty
// vintner-* cgroups still sitting under this session's own delegated
// cgroup after nothing more than normal interrupted testing.
//
// Safe to call unconditionally on every newCgroup(): os.Remove on a
// directory that's still a live cgroup (an actually-concurrent vintner
// invocation, or another one's close() mid-retry) fails at the kernel
// level exactly like rmdir on a non-empty directory would - left alone,
// no different from before this existed. Only genuinely empty (orphaned)
// ones are ever removed.
func sweepOrphanedCgroups(base string) {
	matches, err := filepath.Glob(filepath.Join(base, "vintner-*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		os.Remove(m)
	}
}

// ownCgroupPath resolves the cgroupfs directory vintner's own process
// currently lives in, from the unified (v2) entry in /proc/self/cgroup
// ("0::<path>"). A cgroup v1-only system has no such line - cgroup.kill is
// a v2-only interface, so that's reported as unusable rather than falling
// back to a v1-specific mechanism.
func ownCgroupPath() (string, bool) {
	f, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return "", false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "0::") {
			continue
		}
		return filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(line, "0::")), true
	}
	return "", false
}

// apply places attr's eventual process into h's cgroup atomically at
// clone/exec time (CLONE_INTO_CGROUP via Go's UseCgroupFD/CgroupFD, Go
// >=1.20) - a plain directory fd is exactly what the kernel expects here,
// no O_PATH or extra flags needed. Call after setNewProcessGroup has
// already populated attr, since apply only adds to it.
func (h *cgroupHandle) apply(attr *syscall.SysProcAttr) {
	if h == nil {
		return
	}
	attr.UseCgroupFD = true
	attr.CgroupFD = int(h.f.Fd())
}

// kill hard-kills every process h's cgroup has ever held, including ones
// setsid() moved out of the Unix process group syscall.Kill(-pid, ...)
// targets - see cgroupHandle's doc comment. Best-effort: errors are
// ignored, since this is a reliability addition on top of the existing
// process-group kill, not a replacement callers depend on succeeding.
func (h *cgroupHandle) kill() {
	if h == nil {
		return
	}
	_ = os.WriteFile(filepath.Join(h.dir, "cgroup.kill"), []byte("1"), 0)
}

// close releases h: closes the cgroup fd and removes the now-hopefully-
// empty directory, retrying briefly since kill()'s effect isn't
// synchronous. Failing to remove it isn't fatal - a single leftover empty
// cgroup directory holds no process and costs nothing at rest - but see
// sweepOrphanedCgroups for why leaving it around forever still isn't the
// end of the story (this only runs at all if the process survives long
// enough for its own defer to fire in the first place).
func (h *cgroupHandle) close() {
	if h == nil {
		return
	}
	h.f.Close()
	for i := 0; i < 20; i++ {
		if err := os.Remove(h.dir); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
