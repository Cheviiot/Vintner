package wrapper

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

// pipeDrainGrace bounds how long we wait for a tool's stdout/stderr copy
// goroutines to see EOF after the tool's own process has already exited.
// Wine keeps wineserver and its service processes (services.exe,
// winedevice.exe, explorer.exe, ...) running in the background for reuse
// across invocations, and they inherit our pipes' write ends - so EOF can
// otherwise never arrive, hanging any caller piping our output (`| tee`,
// `| tail`, CI log capture) long after the actual build finished.
const pipeDrainGrace = 500 * time.Millisecond

// toolRelayName is where `vintner install` places the compiled
// toolrelay.exe helper, shared across all arch bin dirs.
const toolRelayName = "toolrelay.exe"

// Run executes the named multi-call tool with args, exactly as the original
// bash wrappers would, and returns the process exit code.
//
// binDir is the <dest>/bin/<arch> directory holding env.json for this
// invocation. Pass "" for the ordinary multi-call case (invoked as `cl`,
// `link`, etc. via a same-directory symlink) to have it resolved from the
// running binary's own location; a non-empty value is for `vintner <tool>
// ...` direct dispatch (see cmd/vintner's runTool), which isn't running
// from inside any particular <dest>/bin/<arch> and so has nothing to
// resolve on its own.
func Run(tool string, args []string, binDir string) int {
	if nativeTools[tool] {
		return runNative(tool, args)
	}

	s, ok := Tools[tool]
	if !ok {
		fmt.Fprintf(os.Stderr, "vintner: unknown tool %q\n", tool)
		return 127
	}

	scriptDir := binDir
	if scriptDir == "" {
		// os.Executable() (backed by /proc/self/exe on Linux) fully resolves
		// symlinks, unlike os.Args[0]: not every shell passes a
		// PATH-resolved absolute path as argv[0] (some just pass the bare
		// command name), which would make an argv[0]-based lookup resolve
		// against the caller's cwd instead of the actual install dir.
		// `install` sets each arch dir up with its own local copy of the
		// binary precisely so this resolves to <dest>/bin/<arch>, not
		// <dest>/bin.
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "vintner:", err)
			return 1
		}
		scriptDir = filepath.Dir(exePath)
	}

	var cfg *wineenv.Config
	if s.resolveScriptDir != nil {
		var note string
		scriptDir, cfg, note = s.resolveScriptDir(scriptDir, args)
		if note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
	}
	if cfg == nil {
		var err error
		cfg, err = wineenv.Load(scriptDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "vintner: loading install config:", err)
			return 1
		}
	}
	baseUnix, err := wineenv.FindBaseUnix(scriptDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner: locating installation root:", err)
		return 1
	}
	paths := wineenv.NewPaths(cfg, baseUnix)

	toolExePath := filepath.Join(s.exeDir(paths), s.exeName)

	// wineHome is vintner's own root (~/.vintner) - independent of
	// scriptDir/baseUnix above, which name a *particular* toolchain
	// install directory (a user can have several side by side - see
	// toolchain_select.go); the bundled wine and its isolated prefix are
	// shared across all of them, not tied to one. A resolution failure
	// here (e.g. os.UserHomeDir() erroring) just means the bundled-wine
	// lookup below never finds anything, falling through to
	// VINTNER_WINE/system wine exactly as if wineHome were never set.
	wineHome, _ := wineenv.DefaultHome()
	wineBin, wineSource, err := wineenv.ResolveWine(wineHome)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	winePrefixEnv := wineenv.WinePrefixEnv(wineHome, wineSource)

	rewritten := RewriteArgs(args)
	rewritten = rewriteResponseFileArgs(rewritten)

	var exitCode int
	switch {
	case s.rawStdout:
		// MSBuild: skip all filtering/toolrelay (its output is meant to be
		// read as-is), and add the extra environment MSBuild's own
		// toolset/SDK-detection props need on top of the generic
		// INCLUDE/LIB/WINEPATH, plus any global properties a project file
		// itself could otherwise override (see msbuildGlobalArgs) and a
		// forced /nodeReuse:false (see msbuildNodeReuseArgs).
		msArgs := append(msbuildGlobalArgs(cfg, rewritten), rewritten...)
		msArgs = append(msbuildNodeReuseArgs(rewritten), msArgs...)
		tc, cleanup := newToolCommand(wineBin, append([]string{toolExePath}, msArgs...)...)
		defer cleanup()
		env := buildEnv(paths, winePrefixEnv)
		for k, v := range msbuildEnv(cfg, paths) {
			env = append(env, k+"="+v)
		}
		tc.Env = env
		tc.Stdin = os.Stdin
		exitCode = runRawStdout(tc)
	default:
		relay := filepath.Join(paths.BaseUnix, "bin", toolRelayName)
		if fi, err := os.Stat(relay); err == nil && !fi.IsDir() {
			exitCode = runViaToolRelay(wineBin, relay, toolExePath, rewritten, paths, winePrefixEnv, s.stdoutFilter, s.stderrFilter)
		} else {
			tc, cleanup := newToolCommand(wineBin, append([]string{toolExePath}, rewritten...)...)
			defer cleanup()
			tc.Env = buildEnv(paths, winePrefixEnv)
			tc.Stdin = os.Stdin
			exitCode = runFiltered(tc, s.stdoutFilter, s.stderrFilter)
		}
	}

	if s.postProcess != nil {
		s.postProcess(args)
	}

	return exitCode
}

// runViaToolRelay runs exePath through the compiled toolrelay.exe helper:
// toolrelay.exe spawns the real tool natively under Windows, redirecting
// its stdio to two named FIFOs we create and read from here. This is what
// lets `mt.exe`'s CMake-compatibility exit code translation (0x41020001 ->
// 0xbb) survive Wine's own exit-code truncation, since toolrelay.exe
// observes the real 32-bit exit code via Win32 before translating and
// re-exiting with a value that fits in a byte.
func runViaToolRelay(wineBin, relayExe, exePath string, args []string, paths *wineenv.Paths, winePrefixEnv []string, stdoutF, stderrF lineFilter) int {
	stdoutFifo := filepath.Join(os.TempDir(), fmt.Sprintf("vintner.stdout.%d", os.Getpid()))
	stderrFifo := filepath.Join(os.TempDir(), fmt.Sprintf("vintner.stderr.%d", os.Getpid()))
	os.Remove(stdoutFifo)
	os.Remove(stderrFifo)
	if err := syscall.Mkfifo(stdoutFifo, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	defer os.Remove(stdoutFifo)
	if err := syscall.Mkfifo(stderrFifo, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	defer os.Remove(stderrFifo)

	cmdArgs := append([]string{relayExe, exePath}, args...)
	tc, cleanup := newToolCommand(wineBin, cmdArgs...)
	defer cleanup()
	tc.Env = append(buildEnv(paths, winePrefixEnv), "MSVCGOWINE_STDOUT="+stdoutFifo, "MSVCGOWINE_STDERR="+stderrFifo)
	if devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		defer devNull.Close()
		tc.Stdout = devNull
		tc.Stderr = devNull
	}

	if err := tc.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	stopSignals := forwardSignals(tc.Process.Pid, tc.cg)
	defer stopSignals()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		f, err := os.Open(stdoutFifo) // blocks until toolrelay.exe opens its end
		if err != nil {
			return
		}
		defer f.Close()
		pumpLines(f, os.Stdout, stdoutF)
	}()
	go func() {
		defer wg.Done()
		f, err := os.Open(stderrFifo)
		if err != nil {
			return
		}
		defer f.Close()
		pumpLines(f, os.Stderr, stderrF)
	}()

	err := tc.Wait()
	// If wine (or toolrelay.exe itself) exited without ever opening one of
	// the FIFOs for writing - wine failing to launch it, or a crash before
	// it reaches its own FIFO-open code - the matching reader goroutine
	// above is stuck forever in a blocking os.Open() waiting for a writer
	// that's never coming now; nothing else unblocks it, not even
	// VINTNER_TIMEOUT (which only bounds tc.Wait(), already returned by
	// this point). Opening each FIFO for writing ourselves and immediately
	// closing it is the standard fix: the kernel rendezvouses us with the
	// blocked reader-open (letting it return), and closing right away with
	// nothing written gives its next Read() a clean EOF.
	//
	// Retried on a short tick rather than fired once: right here, a reader
	// goroutine above might simply not have reached its blocking os.Open()
	// yet (goroutine-scheduling latency, not a real hang) - a single
	// unblock attempt could lose that race and find no reader waiting yet,
	// getting ENXIO right when it was actually needed. Retrying until the
	// readers finish on their own costs nothing in the normal case (a
	// toolrelay.exe that ran has already written and closed by the time
	// tc.Wait() returns, so readersDone fires within a tick or two) and
	// reliably still reaches a reader that was just slow to start blocking,
	// in the actual hang case.
	readersDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(readersDone)
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
waitForReaders:
	for {
		select {
		case <-readersDone:
			break waitForReaders
		case <-ticker.C:
			unblockFifo(stdoutFifo)
			unblockFifo(stderrFifo)
		}
	}

	if err != nil {
		if tc.timedOut() {
			fmt.Fprintln(os.Stderr, tc.timeoutMessage())
			return 124
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	return 0
}

// unblockFifo opens path for writing (non-blocking - there's no reason to
// risk blocking here too) and immediately closes it, releasing any reader
// stuck in a blocking open() on the same FIFO waiting for a writer. A FIFO
// open() only succeeds once a reader and a writer rendezvous; if one is
// already blocked waiting, opening from the other side always completes
// that rendezvous immediately, non-blocking or not. See runViaToolRelay.
func unblockFifo(path string) {
	// os.OpenFile has no portable O_NONBLOCK - it's not one of Go's os.O_*
	// flags - so this goes through syscall directly.
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return
	}
	syscall.Close(fd)
}

// runRawStdout runs cmd, copying its stdout/stderr through byte-for-byte
// (MSBuild's own console formatting is meant to reach the user as-is). It
// pipes rather than inheriting os.Stdout/os.Stderr directly so that only our
// own copy goroutines - not the caller's terminal or pipe - are exposed to
// Wine's background processes holding those descriptors open; see
// pipeDrainGrace.
func runRawStdout(tc *toolCommand) int {
	stdout, err := tc.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	stderr, err := tc.StderrPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}

	if err := tc.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	stopSignals := forwardSignals(tc.Process.Pid, tc.cg)
	defer stopSignals()

	doneOut := make(chan struct{})
	doneErr := make(chan struct{})
	go func() { io.Copy(os.Stdout, stdout); close(doneOut) }()
	go func() { io.Copy(os.Stderr, stderr); close(doneErr) }()

	err = tc.Wait()
	drain(doneOut)
	drain(doneErr)

	if err != nil {
		if tc.timedOut() {
			fmt.Fprintln(os.Stderr, tc.timeoutMessage())
			return 124
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	return 0
}

// drain waits for a pipe-copy goroutine to see EOF, but not past
// pipeDrainGrace - see its doc comment for why EOF can otherwise never come.
func drain(done <-chan struct{}) {
	select {
	case <-done:
	case <-time.After(pipeDrainGrace):
	}
}

// buildEnv assembles the environment for a wine-hosted tool invocation.
// winePrefixEnv is wineenv.WinePrefixEnv's result ("WINEPREFIX=..." or nil)
// - isolating WINEPREFIX only makes sense for vintner's own bundled wine
// (see WinePrefixEnv's doc comment), so callers using system/VINTNER_WINE
// pass nil and today's inherited-from-environment behavior is unchanged.
func buildEnv(p *wineenv.Paths, winePrefixEnv []string) []string {
	overrides := map[string]string{
		"INCLUDE":          p.Include,
		"LIB":              p.Lib,
		"LIBPATH":          p.LibPath,
		"WINEPATH":         p.WinePath,
		"WINEDLLOVERRIDES": p.WineDLLOverrides,
	}
	for _, kv := range winePrefixEnv {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			overrides[kv[:i]] = kv[i+1:]
		}
	}
	base := os.Environ()
	if _, set := os.LookupEnv("WINEDEBUG"); !set {
		overrides["WINEDEBUG"] = "-all"
	}

	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		key := kv[:strings.IndexByte(kv, '=')]
		if _, skip := overrides[key]; skip {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

// runFiltered streams stdout/stderr line by line through the tool's
// filters (CR-stripping always applied first), then waits for completion.
func runFiltered(tc *toolCommand, stdoutF, stderrF lineFilter) int {
	stdout, err := tc.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	stderr, err := tc.StderrPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}

	if err := tc.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	stopSignals := forwardSignals(tc.Process.Pid, tc.cg)
	defer stopSignals()

	doneOut := make(chan struct{})
	doneErr := make(chan struct{})
	go func() { pumpLines(stdout, os.Stdout, stdoutF); close(doneOut) }()
	go func() { pumpLines(stderr, os.Stderr, stderrF); close(doneErr) }()

	err = tc.Wait()
	drain(doneOut)
	drain(doneErr)

	if err != nil {
		if tc.timedOut() {
			fmt.Fprintln(os.Stderr, tc.timeoutMessage())
			return 124
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	return 0
}

// pumpLines reads r line by line, CR-stripping and applying filter (if
// non-nil) before writing each line to w.
func pumpLines(r io.Reader, w *os.File, filter lineFilter) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := stripCR(scanner.Text())
		if filter != nil {
			line = filter(line)
		}
		fmt.Fprintln(w, line)
	}
	// bufio.Scanner silently stops (dropping the rest of the stream) once a
	// single line exceeds its 16MB buffer - surface that rather than letting
	// build output vanish without explanation (heavily templated C++ error
	// messages are the realistic way to hit this).
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(w, "vintner: output truncated: %v\n", err)
	}
}
