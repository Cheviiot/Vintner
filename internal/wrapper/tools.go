// Package wrapper implements the per-tool command dispatch (cl, link, lib,
// ml, ml64, mc, midl, mt, rc, dumpbin, msbuild, nmake, armasm, armasm64, cmd,
// findstr): loading each tool's environment, invoking it under Wine, and
// filtering its output.
package wrapper

import (
	"sort"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

// dirKind selects which install directory a tool's real .exe lives in.
type dirKind int

const (
	dirBin dirKind = iota // <bindir> - vc/tools/msvc/<ver>/bin/Host<host>/<arch>
	dirSDK                // <sdkbindir> - kits/10/bin/<sdkver>/<host>
	dirMSBuild
)

type spec struct {
	exeName      string
	dir          dirKind
	rawStdout    bool // MSBuild: skip line filtering, inherit stdio directly
	stdoutFilter lineFilter
	stderrFilter lineFilter
	postProcess  func(origArgs []string) // cl: fix up /P /Fi<file> output

	// resolveScriptDir lets a tool override which <dest>/bin/<arch>
	// directory Run() actually loads env.json from, given the one
	// resolution otherwise settles on (VINTNER_BIN or argv0-derived) and
	// the tool's own args - msbuild uses this to auto-select a toolchain
	// matching the project's PlatformToolset (see toolchain_select.go).
	// Returning a non-nil cfg alongside dir means Run() can skip its own
	// wineenv.Load(dir), since resolveScriptDir already had to load it to
	// decide; nil cfg falls through to the normal load. Every other tool
	// leaves this nil and gets the default resolution untouched.
	resolveScriptDir func(dir string, args []string) (resolvedDir string, cfg *wineenv.Config, note string)
}

// Tools lists every multi-call name this binary answers to, besides its own
// management-CLI name.
var Tools = map[string]spec{
	"cl":       {exeName: "cl.exe", dir: dirBin, stdoutFilter: clStdoutFilter, stderrFilter: clStderrFilter, postProcess: clPostProcess},
	"link":     {exeName: "link.exe", dir: dirBin},
	"lib":      {exeName: "lib.exe", dir: dirBin},
	"ml":       {exeName: "ml.exe", dir: dirBin},
	"ml64":     {exeName: "ml64.exe", dir: dirBin},
	"nmake":    {exeName: "nmake.exe", dir: dirBin},
	"armasm":   {exeName: "armasm.exe", dir: dirBin},
	"armasm64": {exeName: "armasm64.exe", dir: dirBin},
	"dumpbin":  {exeName: "dumpbin.exe", dir: dirBin, stdoutFilter: dumpbinStdoutFilter},
	"mc":       {exeName: "mc.exe", dir: dirSDK},
	"midl":     {exeName: "midl.exe", dir: dirSDK},
	"mt":       {exeName: "mt.exe", dir: dirSDK},
	"rc":       {exeName: "rc.exe", dir: dirSDK},
	"msbuild":  {exeName: "MSBuild.exe", dir: dirMSBuild, rawStdout: true, resolveScriptDir: selectToolchainDir},
}

// nativeTools are handled entirely without Wine.
var nativeTools = map[string]bool{"cmd": true, "findstr": true}

// IsTool reports whether name is a recognized wrapped tool - either a
// Wine-hosted one in Tools or a native shim in nativeTools. Shared between
// the ordinary multi-call dispatch (invoked *as* one of these names via a
// same-directory symlink) and `vintner <tool> ...` direct dispatch, so both
// recognize exactly the same set of names.
func IsTool(name string) bool {
	_, ok := Tools[name]
	return ok || nativeTools[name]
}

// ToolNames returns every recognized tool name, sorted - Tools and
// nativeTools combined. Used to generate shell completion without a
// separate hand-maintained list that could drift from the real set (as
// happened once already with a stale --with-dxsdk completion entry).
func ToolNames() []string {
	names := make([]string, 0, len(Tools)+len(nativeTools))
	for name := range Tools {
		names = append(names, name)
	}
	for name := range nativeTools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s spec) exeDir(p *wineenv.Paths) string {
	switch s.dir {
	case dirSDK:
		return p.SDKBinDir
	case dirMSBuild:
		return p.MSBuildBinDir
	default:
		return p.BinDir
	}
}
