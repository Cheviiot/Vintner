//go:build !linux

package wrapper

import "syscall"

// cgroupHandle is a no-op stand-in on non-Linux hosts: cgroup v2's
// guaranteed-cleanup semantics (see cgroup.go's doc comment) don't exist
// there, so every wine-hosted tool invocation falls back to the plain
// process-group cleanup setNewProcessGroup already provides.
type cgroupHandle struct{}

// CgroupCleanupAvailable always reports false here - see cgroup.go's doc
// comment on the linux implementation.
func CgroupCleanupAvailable() bool { return false }

func newCgroup() *cgroupHandle                          { return nil }
func (h *cgroupHandle) apply(attr *syscall.SysProcAttr) {}
func (h *cgroupHandle) kill()                           {}
func (h *cgroupHandle) close()                          {}
