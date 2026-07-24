// Package wrapper implements the per-tool command dispatch (cl, link, lib,
// ml, ml64, mc, midl, mt, rc, dumpbin, msbuild, nmake, armasm, armasm64, cmd,
// findstr): loading each tool's environment, invoking it under Wine, and
// filtering its output.
package wrapper

import "github.com/Cheviiot/msvc-go-wine/internal/wineenv"

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
	"msbuild":  {exeName: "MSBuild.exe", dir: dirMSBuild, rawStdout: true},
}

// nativeTools are handled entirely without Wine.
var nativeTools = map[string]bool{"cmd": true, "findstr": true}

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
