package wrapper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

func TestMsbuildPlatform(t *testing.T) {
	for _, tc := range []struct{ arch, want string }{
		{"x86", "Win32"},
		{"x64", "x64"},
		{"arm", "ARM"},
		{"arm64", "ARM64"},
	} {
		if got := msbuildPlatform(tc.arch); got != tc.want {
			t.Errorf("msbuildPlatform(%q) = %q, want %q", tc.arch, got, tc.want)
		}
	}
}

func newTestPaths(t *testing.T, cfg *wineenv.Config) (*wineenv.Paths, string) {
	t.Helper()
	base := t.TempDir()
	return wineenv.NewPaths(cfg, base), base
}

func TestMsbuildEnvBasics(t *testing.T) {
	cfg := &wineenv.Config{Arch: "x64", Host: "x64", DotnetHost: "amd64", MSVCVer: "14.51.36231", SDKVer: "10.0.26100.0"}
	paths, _ := newTestPaths(t, cfg)

	env := msbuildEnv(cfg, paths)

	if env["TZ"] != "UTC" {
		t.Errorf(`env["TZ"] = %q, want "UTC"`, env["TZ"])
	}
	if env["DisableRegistryUse"] != "true" {
		t.Errorf(`env["DisableRegistryUse"] = %q, want "true"`, env["DisableRegistryUse"])
	}
	if env["CheckMSVCComponents"] != "false" {
		t.Errorf(`env["CheckMSVCComponents"] = %q, want "false" (else CheckVCToolsetVersion errors on an aliased PlatformToolset)`, env["CheckMSVCComponents"])
	}
	// VCToolsVersion must be a real version string (see msbuildEnv's doc
	// comment on it: leaving it unset makes Microsoft.Cpp.VCTools.props
	// substitute a placeholder that then breaks unconditional version
	// comparisons elsewhere). CheckMSVCComponents=false is what keeps this
	// safe to combine with an aliased PlatformToolset.
	if env["VCToolsVersion"] != cfg.MSVCVer {
		t.Errorf(`env["VCToolsVersion"] = %q, want %q`, env["VCToolsVersion"], cfg.MSVCVer)
	}
	if env["WindowsTargetPlatformVersion"] != cfg.SDKVer {
		t.Errorf(`env["WindowsTargetPlatformVersion"] = %q, want %q`, env["WindowsTargetPlatformVersion"], cfg.SDKVer)
	}
	if env["Platform"] != "x64" {
		t.Errorf(`env["Platform"] = %q, want "x64"`, env["Platform"])
	}
	if env["SignMode"] != "off" {
		t.Errorf(`env["SignMode"] = %q, want "off" (driver builds must not attempt real signing)`, env["SignMode"])
	}
	// No WDK content on disk in this test - must not claim otherwise.
	if _, ok := env["WDKContentRoot"]; ok {
		t.Error(`env["WDKContentRoot"] set even though no wdk/<arch>/c directory exists`)
	}
}

func TestMsbuildEnvDiscoversEveryToolsetVersion(t *testing.T) {
	cfg := &wineenv.Config{Arch: "x64", Host: "x64", DotnetHost: "amd64", MSVCVer: "14.51.36231", SDKVer: "10.0.26100.0"}
	paths, base := newTestPaths(t, cfg)

	// Real layout has two independent sources feeding VCInstallDir_<N>/
	// VCToolsInstallDir_<N> (see toolsetSuffixes' doc comment for why both
	// are needed): the PlatformToolset short names a downloaded compiler
	// ships default-props for, and MSBuild's own fixed schema-version dirs.
	buildDir := filepath.Join(base, "vc", "Auxiliary", "Build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"Microsoft.VCToolsVersion.v145.default.props",
		"Microsoft.VCToolsVersion.v143.default.props",
		"Microsoft.VCToolsVersion.default.props", // no version suffix - must not match
	} {
		if err := os.WriteFile(filepath.Join(buildDir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"v180", "not-a-version"} {
		if err := os.MkdirAll(filepath.Join(base, "MSBuild", "Microsoft", "VC", v), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	env := msbuildEnv(cfg, paths)

	for _, n := range []string{"145", "143", "180"} {
		if _, ok := env["VCInstallDir_"+n]; !ok {
			t.Errorf("expected VCInstallDir_%s to be set", n)
		}
		if _, ok := env["VCToolsInstallDir_"+n]; !ok {
			t.Errorf("expected VCToolsInstallDir_%s to be set", n)
		}
	}
	if _, ok := env["VCInstallDir_not-a-version"]; ok {
		t.Error("a directory not matching v<digits> should not have produced a VCInstallDir_ entry")
	}
}

func TestToolsetSuffixesDedupsOverlap(t *testing.T) {
	base := t.TempDir()
	buildDir := filepath.Join(base, "vc", "Auxiliary", "Build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "Microsoft.VCToolsVersion.v180.default.props"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "MSBuild", "Microsoft", "VC", "v180"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := toolsetSuffixes(base)
	count := 0
	for _, n := range got {
		if n == "180" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("toolsetSuffixes() returned %q with %d entries for \"180\" (from both sources), want exactly 1", got, count)
	}
}

func TestMsbuildGlobalArgsForcesWindowsTargetPlatformVersion(t *testing.T) {
	cfg := &wineenv.Config{SDKVer: "10.0.26100.0"}

	got := msbuildGlobalArgs(cfg, []string{"Foo.sln", "/p:Configuration=Release"})
	want := "/p:WindowsTargetPlatformVersion=10.0.26100.0"
	found := false
	for _, a := range got {
		if a == want {
			found = true
		}
	}
	if !found {
		t.Errorf("msbuildGlobalArgs(...) = %v, want it to contain %q", got, want)
	}
}

func TestMsbuildGlobalArgsRespectsExplicitOverride(t *testing.T) {
	cfg := &wineenv.Config{SDKVer: "10.0.26100.0"}

	for _, explicit := range []string{
		"/p:WindowsTargetPlatformVersion=10.0.19041.0",
		"-p:WindowsTargetPlatformVersion=10.0.19041.0",
		"/property:WindowsTargetPlatformVersion=10.0.19041.0",
	} {
		got := msbuildGlobalArgs(cfg, []string{"Foo.sln", explicit})
		for _, a := range got {
			if reGlobalProp("WindowsTargetPlatformVersion").MatchString(a) {
				t.Errorf("msbuildGlobalArgs with explicit %q also injected %q - should have left the caller's value alone", explicit, a)
			}
		}
	}
}

func TestMsbuildEnvDetectsWDKContentRoot(t *testing.T) {
	cfg := &wineenv.Config{Arch: "x64", Host: "x64", DotnetHost: "amd64", MSVCVer: "14.51.36231", SDKVer: "10.0.26100.0"}
	paths, base := newTestPaths(t, cfg)

	if err := os.MkdirAll(filepath.Join(base, "wdk", "x64", "c"), 0o755); err != nil {
		t.Fatal(err)
	}

	env := msbuildEnv(cfg, paths)

	if env["WDKContentRoot"] == "" {
		t.Error("expected WDKContentRoot to be set once wdk/x64/c exists on disk")
	}
	if env["WDKBuildFolder"] != cfg.SDKVer {
		t.Errorf(`env["WDKBuildFolder"] = %q, want %q`, env["WDKBuildFolder"], cfg.SDKVer)
	}
}

func TestMsbuildEnvPreferredToolArchitecture(t *testing.T) {
	// PreferredToolArchitecture should only be set when the host toolset
	// bin dir is the 64-bit ("amd64") .NET host - not for arm64.
	cfg64 := &wineenv.Config{Arch: "x64", Host: "x64", DotnetHost: "amd64", MSVCVer: "1", SDKVer: "1"}
	paths64, _ := newTestPaths(t, cfg64)
	if env := msbuildEnv(cfg64, paths64); env["PreferredToolArchitecture"] != "x64" {
		t.Errorf(`with DotnetHost=amd64, PreferredToolArchitecture = %q, want "x64"`, env["PreferredToolArchitecture"])
	}

	cfgARM := &wineenv.Config{Arch: "arm64", Host: "arm64", DotnetHost: "arm64", MSVCVer: "1", SDKVer: "1"}
	pathsARM, _ := newTestPaths(t, cfgARM)
	if env := msbuildEnv(cfgARM, pathsARM); env["PreferredToolArchitecture"] != "" {
		t.Errorf(`with DotnetHost=arm64, PreferredToolArchitecture = %q, want unset`, env["PreferredToolArchitecture"])
	}
}

func TestMsbuildNodeReuseArgsForcesOffByDefault(t *testing.T) {
	got := msbuildNodeReuseArgs([]string{"Foo.sln", "/p:Configuration=Release"})
	want := []string{"/nodeReuse:false"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("msbuildNodeReuseArgs(...) = %v, want %v", got, want)
	}
}

func TestMsbuildNodeReuseArgsRespectsExplicitOverride(t *testing.T) {
	for _, explicit := range []string{
		"/nodeReuse:true",
		"-nodeReuse:true",
		"/nr:true",
		"/NODEREUSE:FALSE", // caller explicitly wanting it off too - still shouldn't double up
	} {
		got := msbuildNodeReuseArgs([]string{"Foo.sln", explicit})
		if got != nil {
			t.Errorf("msbuildNodeReuseArgs with explicit %q = %v, want nil (left alone)", explicit, got)
		}
	}
}
