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
func Run(tool string, args []string) int {
	if nativeTools[tool] {
		return runNative(tool, args)
	}

	s, ok := Tools[tool]
	if !ok {
		fmt.Fprintf(os.Stderr, "vintner: unknown tool %q\n", tool)
		return 127
	}

	// os.Executable() (backed by /proc/self/exe on Linux) fully resolves
	// symlinks, unlike os.Args[0]: not every shell passes a PATH-resolved
	// absolute path as argv[0] (some just pass the bare command name), which
	// would make an argv[0]-based lookup resolve against the caller's cwd
	// instead of the actual install dir. `install` sets each arch dir up
	// with its own local copy of the binary precisely so this resolves to
	// <dest>/bin/<arch>, not <dest>/bin.
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	scriptDir := filepath.Dir(exePath)

	cfg, err := wineenv.Load(scriptDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner: loading install config:", err)
		return 1
	}
	baseUnix, err := wineenv.FindBaseUnix(scriptDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner: locating installation root:", err)
		return 1
	}
	paths := wineenv.NewPaths(cfg, baseUnix)

	toolExePath := filepath.Join(s.exeDir(paths), s.exeName)

	wineBin, err := wineenv.FindWine()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}

	rewritten := RewriteArgs(args)

	var exitCode int
	switch {
	case s.rawStdout:
		// MSBuild: skip all filtering/toolrelay (its output is meant to be
		// read as-is), and add the extra environment MSBuild's own
		// toolset/SDK-detection props need on top of the generic
		// INCLUDE/LIB/WINEPATH.
		cmd := exec.Command(wineBin, append([]string{toolExePath}, rewritten...)...)
		env := buildEnv(paths)
		for k, v := range msbuildEnv(cfg, paths) {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
		cmd.Stdin = os.Stdin
		exitCode = runRawStdout(cmd)
	default:
		relay := filepath.Join(paths.BaseUnix, "bin", toolRelayName)
		if fi, err := os.Stat(relay); err == nil && !fi.IsDir() {
			exitCode = runViaToolRelay(wineBin, relay, toolExePath, rewritten, paths, s.stdoutFilter, s.stderrFilter)
		} else {
			cmd := exec.Command(wineBin, append([]string{toolExePath}, rewritten...)...)
			cmd.Env = buildEnv(paths)
			cmd.Stdin = os.Stdin
			exitCode = runFiltered(cmd, s.stdoutFilter, s.stderrFilter)
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
func runViaToolRelay(wineBin, relayExe, exePath string, args []string, paths *wineenv.Paths, stdoutF, stderrF lineFilter) int {
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
	cmd := exec.Command(wineBin, cmdArgs...)
	cmd.Env = append(buildEnv(paths), "MSVCGOWINE_STDOUT="+stdoutFifo, "MSVCGOWINE_STDERR="+stderrFifo)
	if devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		defer devNull.Close()
		cmd.Stdout = devNull
		cmd.Stderr = devNull
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}

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

	err := cmd.Wait()
	wg.Wait()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	return 0
}

// runRawStdout runs cmd, copying its stdout/stderr through byte-for-byte
// (MSBuild's own console formatting is meant to reach the user as-is). It
// pipes rather than inheriting os.Stdout/os.Stderr directly so that only our
// own copy goroutines - not the caller's terminal or pipe - are exposed to
// Wine's background processes holding those descriptors open; see
// pipeDrainGrace.
func runRawStdout(cmd *exec.Cmd) int {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}

	doneOut := make(chan struct{})
	doneErr := make(chan struct{})
	go func() { io.Copy(os.Stdout, stdout); close(doneOut) }()
	go func() { io.Copy(os.Stderr, stderr); close(doneErr) }()

	err = cmd.Wait()
	drain(doneOut)
	drain(doneErr)

	if err != nil {
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

func buildEnv(p *wineenv.Paths) []string {
	overrides := map[string]string{
		"INCLUDE":          p.Include,
		"LIB":              p.Lib,
		"LIBPATH":          p.LibPath,
		"WINEPATH":         p.WinePath,
		"WINEDLLOVERRIDES": p.WineDLLOverrides,
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
func runFiltered(cmd *exec.Cmd, stdoutF, stderrF lineFilter) int {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "vintner:", err)
		return 1
	}

	doneOut := make(chan struct{})
	doneErr := make(chan struct{})
	go func() { pumpLines(stdout, os.Stdout, stdoutF); close(doneOut) }()
	go func() { pumpLines(stderr, os.Stderr, stderrF); close(doneErr) }()

	err = cmd.Wait()
	drain(doneOut)
	drain(doneErr)

	if err != nil {
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
