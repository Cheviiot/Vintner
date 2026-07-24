package wineenv

import "testing"

// Values lifted from the original wrappers/cl template
// (MSVCVER=14.13.26128, SDKVER=10.0.16299.0, ARCH=x86) to cross-check the Go
// port produces byte-identical INCLUDE/LIB strings.
func TestNewPathsMatchesOriginalTemplate(t *testing.T) {
	cfg := &Config{
		Arch:       "x86",
		Host:       "x64",
		DotnetHost: "amd64",
		MSVCVer:    "14.13.26128",
		SDKVer:     "10.0.16299.0",
	}
	p := NewPaths(cfg, "/opt/msvc")

	wantInclude := `z:\opt\msvc\vc\tools\msvc\14.13.26128\atlmfc\include;` +
		`z:\opt\msvc\vc\tools\msvc\14.13.26128\include;` +
		`z:\opt\msvc\kits\10\include\10.0.16299.0\shared;` +
		`z:\opt\msvc\kits\10\include\10.0.16299.0\ucrt;` +
		`z:\opt\msvc\kits\10\include\10.0.16299.0\um;` +
		`z:\opt\msvc\kits\10\include\10.0.16299.0\winrt;` +
		`z:\opt\msvc\kits\10\include\10.0.16299.0\km`
	if p.Include != wantInclude {
		t.Errorf("INCLUDE mismatch:\n got: %s\nwant: %s", p.Include, wantInclude)
	}

	wantLib := `z:\opt\msvc\vc\tools\msvc\14.13.26128\atlmfc\lib\x86;` +
		`z:\opt\msvc\vc\tools\msvc\14.13.26128\lib\x86;` +
		`z:\opt\msvc\kits\10\lib\10.0.16299.0\ucrt\x86;` +
		`z:\opt\msvc\kits\10\lib\10.0.16299.0\um\x86;` +
		`z:\opt\msvc\kits\10\lib\10.0.16299.0\km\x86`
	if p.Lib != wantLib {
		t.Errorf("LIB mismatch:\n got: %s\nwant: %s", p.Lib, wantLib)
	}

	if p.WineDLLOverrides != "vcruntime140=n;vcruntime140_1=n" {
		t.Errorf("WINEDLLOVERRIDES mismatch: %s", p.WineDLLOverrides)
	}

	wantBinDir := "/opt/msvc/vc/tools/msvc/14.13.26128/bin/Hostx64/x86"
	if p.BinDir != wantBinDir {
		t.Errorf("BinDir mismatch:\n got: %s\nwant: %s", p.BinDir, wantBinDir)
	}

	wantSDKBinDir := "/opt/msvc/kits/10/bin/10.0.16299.0/x64"
	if p.SDKBinDir != wantSDKBinDir {
		t.Errorf("SDKBinDir mismatch:\n got: %s\nwant: %s", p.SDKBinDir, wantSDKBinDir)
	}

	wantMSBuildBinDir := "/opt/msvc/MSBuild/Current/Bin/amd64"
	if p.MSBuildBinDir != wantMSBuildBinDir {
		t.Errorf("MSBuildBinDir mismatch:\n got: %s\nwant: %s", p.MSBuildBinDir, wantMSBuildBinDir)
	}
}
