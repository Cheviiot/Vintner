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

	"github.com/Cheviiot/msvc-go-wine/internal/wineenv"
)

// toolRelayName is where `msvc-go-wine install` places the compiled
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
		fmt.Fprintf(os.Stderr, "msvc-go-wine: unknown tool %q\n", tool)
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
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
		return 1
	}
	scriptDir := filepath.Dir(exePath)

	cfg, err := wineenv.Load(scriptDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine: loading install config:", err)
		return 1
	}
	baseUnix, err := wineenv.FindBaseUnix(scriptDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine: locating installation root:", err)
		return 1
	}
	paths := wineenv.NewPaths(cfg, baseUnix)

	toolExePath := filepath.Join(s.exeDir(paths), s.exeName)

	wineBin, err := wineenv.FindWine()
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
		return 1
	}

	rewritten := RewriteArgs(args)

	var exitCode int
	switch {
	case s.rawStdout:
		// MSBuild: skip all filtering/toolrelay, inherit stdio directly.
		cmd := exec.Command(wineBin, append([]string{toolExePath}, rewritten...)...)
		cmd.Env = buildEnv(paths)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		exitCode = runAndWait(cmd)
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
	stdoutFifo := filepath.Join(os.TempDir(), fmt.Sprintf("msvc-go-wine.stdout.%d", os.Getpid()))
	stderrFifo := filepath.Join(os.TempDir(), fmt.Sprintf("msvc-go-wine.stderr.%d", os.Getpid()))
	os.Remove(stdoutFifo)
	os.Remove(stderrFifo)
	if err := syscall.Mkfifo(stdoutFifo, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
		return 1
	}
	defer os.Remove(stdoutFifo)
	if err := syscall.Mkfifo(stderrFifo, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
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
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
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
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
		return 1
	}
	return 0
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
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
		return 1
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
		return 1
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
		return 1
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pumpLines(stdout, os.Stdout, stdoutF) }()
	go func() { defer wg.Done(); pumpLines(stderr, os.Stderr, stderrF) }()
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
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
}

func runAndWait(cmd *exec.Cmd) int {
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "msvc-go-wine:", err)
		return 1
	}
	return 0
}
