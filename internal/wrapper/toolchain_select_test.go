package wrapper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

func TestFindProjectArg(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"Foo.sln", "/p:Configuration=Release"}, "Foo.sln"},
		{[]string{"/nologo", "source\\Bar.vcxproj"}, "source\\Bar.vcxproj"},
		{[]string{"/p:Configuration=Release", "/t:Build"}, ""},
		{[]string{"-verbosity:minimal"}, ""},
		{[]string{"/home/user/project/source/LCClient.sln", "/p:Configuration=LC_RUS"}, "/home/user/project/source/LCClient.sln"},
		{nil, ""},
	} {
		if got := findProjectArg(tc.args); got != tc.want {
			t.Errorf("findProjectArg(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const vcxprojV142 = `<?xml version="1.0" encoding="utf-8"?>
<Project>
  <PropertyGroup Condition="'$(Configuration)|$(Platform)'=='Release|x64'">
    <PlatformToolset>v142</PlatformToolset>
  </PropertyGroup>
  <PropertyGroup Condition="'$(Configuration)|$(Platform)'=='Debug|x64'">
    <PlatformToolset>v142</PlatformToolset>
  </PropertyGroup>
</Project>
`

const vcxprojV145 = `<?xml version="1.0" encoding="utf-8"?>
<Project>
  <PropertyGroup>
    <PlatformToolset>v145</PlatformToolset>
  </PropertyGroup>
</Project>
`

const vcxprojNoToolset = `<?xml version="1.0" encoding="utf-8"?>
<Project>
  <PropertyGroup>
    <OutDir>bin\</OutDir>
  </PropertyGroup>
</Project>
`

func TestVcxprojPlatformToolsetsDedupsWithinFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Engine.vcxproj")
	writeFile(t, path, vcxprojV142)

	got, err := vcxprojPlatformToolsets(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "142" {
		t.Errorf("vcxprojPlatformToolsets(...) = %v, want [\"142\"]", got)
	}
}

func TestVcxprojPlatformToolsetsNoneFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "NoToolset.vcxproj")
	writeFile(t, path, vcxprojNoToolset)

	got, err := vcxprojPlatformToolsets(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("vcxprojPlatformToolsets(...) = %v, want none", got)
	}
}

func TestRequestedPlatformToolsetFromVcxproj(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Engine.vcxproj")
	writeFile(t, path, vcxprojV142)

	if got := requestedPlatformToolset(path); got != "142" {
		t.Errorf("requestedPlatformToolset(%q) = %q, want \"142\"", path, got)
	}
}

func TestRequestedPlatformToolsetFromSlnUnanimous(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "source", "Engine", "Engine.vcxproj"), vcxprojV142)
	writeFile(t, filepath.Join(dir, "source", "GameMP", "GameMP.vcxproj"), vcxprojV142)

	slnPath := filepath.Join(dir, "LCClient.sln")
	writeFile(t, slnPath, `Microsoft Visual Studio Solution File, Format Version 12.00
Project("{8BC9CEB8-8B4A-11D0-8D11-00A0C91BC942}") = "Engine", "source\Engine\Engine.vcxproj", "{11111111-1111-1111-1111-111111111111}"
EndProject
Project("{8BC9CEB8-8B4A-11D0-8D11-00A0C91BC942}") = "GameMP", "source\GameMP\GameMP.vcxproj", "{22222222-2222-2222-2222-222222222222}"
EndProject
`)

	if got := requestedPlatformToolset(slnPath); got != "142" {
		t.Errorf("requestedPlatformToolset(%q) = %q, want \"142\"", slnPath, got)
	}
}

// TestRequestedPlatformToolsetFromSlnBackslashArg reproduces a real
// `vintner msbuild source\LCClient.sln ...` invocation from a Unix shell:
// the top-level project argument itself, not just the paths inside the
// .sln, commonly arrives Windows-style (backslash-separated) even though
// this parsing always runs on a Unix host.
func TestRequestedPlatformToolsetFromSlnBackslashArg(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "source", "Engine", "Engine.vcxproj"), vcxprojV142)

	slnPath := filepath.Join(dir, "source", "LCClient.sln")
	writeFile(t, slnPath, `Microsoft Visual Studio Solution File, Format Version 12.00
Project("{8BC9CEB8-8B4A-11D0-8D11-00A0C91BC942}") = "Engine", "Engine\Engine.vcxproj", "{11111111-1111-1111-1111-111111111111}"
EndProject
`)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	if got := requestedPlatformToolset(`source\LCClient.sln`); got != "142" {
		t.Errorf(`requestedPlatformToolset("source\LCClient.sln") = %q, want "142"`, got)
	}
}

func TestRequestedPlatformToolsetFromSlnConflicting(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "source", "Engine", "Engine.vcxproj"), vcxprojV142)
	writeFile(t, filepath.Join(dir, "source", "Tool", "Tool.vcxproj"), vcxprojV145)

	slnPath := filepath.Join(dir, "Mixed.sln")
	writeFile(t, slnPath, `Microsoft Visual Studio Solution File, Format Version 12.00
Project("{8BC9CEB8-8B4A-11D0-8D11-00A0C91BC942}") = "Engine", "source\Engine\Engine.vcxproj", "{11111111-1111-1111-1111-111111111111}"
EndProject
Project("{8BC9CEB8-8B4A-11D0-8D11-00A0C91BC942}") = "Tool", "source\Tool\Tool.vcxproj", "{22222222-2222-2222-2222-222222222222}"
EndProject
`)

	if got := requestedPlatformToolset(slnPath); got != "" {
		t.Errorf("requestedPlatformToolset(%q) = %q, want \"\" (conflicting toolsets across the solution)", slnPath, got)
	}
}

func TestRequestedPlatformToolsetNoneFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "NoToolset.vcxproj")
	writeFile(t, path, vcxprojNoToolset)

	if got := requestedPlatformToolset(path); got != "" {
		t.Errorf("requestedPlatformToolset(%q) = %q, want \"\"", path, got)
	}
}

func saveConfig(t *testing.T, dir string, cfg *wineenv.Config) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// setupEngineSln writes an Engine.vcxproj (pinned to vcxprojV142) plus an
// LCClient.sln referencing it, under a fresh temp root - the fixture every
// selectToolchainDir test below needs, shared instead of copy-pasted.
func setupEngineSln(t *testing.T) (root, slnPath string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "source", "Engine", "Engine.vcxproj"), vcxprojV142)
	slnPath = filepath.Join(root, "LCClient.sln")
	writeFile(t, slnPath, `Microsoft Visual Studio Solution File, Format Version 12.00
Project("{8BC9CEB8-8B4A-11D0-8D11-00A0C91BC942}") = "Engine", "source\Engine\Engine.vcxproj", "{11111111-1111-1111-1111-111111111111}"
EndProject
`)
	return root, slnPath
}

func TestSelectToolchainDirNoEnvVarLeavesPrimaryAlone(t *testing.T) {
	t.Setenv(toolchainsEnvVar, "")
	primary := saveConfig(t, t.TempDir(), &wineenv.Config{PlatformToolset: "145"})

	got, cfg, note := selectToolchainDir(primary, []string{"Foo.sln"})
	if got != primary || cfg != nil || note != "" {
		t.Errorf("selectToolchainDir(...) = (%q, %v, %q), want (%q, nil, \"\")", got, cfg, note, primary)
	}
}

func TestSelectToolchainDirSwitchesOnExactMatch(t *testing.T) {
	root, slnPath := setupEngineSln(t)

	primary := saveConfig(t, filepath.Join(root, "toolchain-default", "bin", "x64"), &wineenv.Config{PlatformToolset: "145"})
	alt := saveConfig(t, filepath.Join(root, "toolchain-v142", "bin", "x64"), &wineenv.Config{PlatformToolset: "142"})

	t.Setenv(toolchainsEnvVar, alt)

	got, cfg, note := selectToolchainDir(primary, []string{slnPath, "/p:Configuration=LC_RUS"})
	if got != alt {
		t.Errorf("selectToolchainDir(...) binDir = %q, want %q (the v142 alt)", got, alt)
	}
	if cfg == nil || cfg.PlatformToolset != "142" {
		t.Errorf("selectToolchainDir(...) cfg = %v, want the already-loaded v142 config (so the caller doesn't reload it)", cfg)
	}
	if note == "" {
		t.Error("selectToolchainDir(...) note = \"\", want an explanatory message when switching")
	}
}

func TestSelectToolchainDirNoMatchFallsBackToPrimary(t *testing.T) {
	root, slnPath := setupEngineSln(t)

	primary := saveConfig(t, filepath.Join(root, "toolchain-default", "bin", "x64"), &wineenv.Config{PlatformToolset: "145"})
	// The only registered extra toolchain is also v145 - no v142 anywhere.
	alt := saveConfig(t, filepath.Join(root, "toolchain-other", "bin", "x64"), &wineenv.Config{PlatformToolset: "145"})

	t.Setenv(toolchainsEnvVar, alt)

	got, cfg, note := selectToolchainDir(primary, []string{slnPath})
	if got != primary || note != "" {
		t.Errorf("selectToolchainDir(...) = (%q, %q), want (%q, \"\") (no v142 install available)", got, note, primary)
	}
	if cfg == nil || cfg.PlatformToolset != "145" {
		t.Errorf("selectToolchainDir(...) cfg = %v, want primary's already-loaded v145 config", cfg)
	}
}

func TestSelectToolchainDirPrimaryAlreadyMatches(t *testing.T) {
	root, slnPath := setupEngineSln(t)

	primary := saveConfig(t, filepath.Join(root, "toolchain-default", "bin", "x64"), &wineenv.Config{PlatformToolset: "142"})
	alt := saveConfig(t, filepath.Join(root, "toolchain-other", "bin", "x64"), &wineenv.Config{PlatformToolset: "142"})

	t.Setenv(toolchainsEnvVar, alt)

	got, cfg, note := selectToolchainDir(primary, []string{slnPath})
	if got != primary || note != "" {
		t.Errorf("selectToolchainDir(...) = (%q, %q), want (%q, \"\") (primary already the right generation)", got, note, primary)
	}
	if cfg == nil || cfg.PlatformToolset != "142" {
		t.Errorf("selectToolchainDir(...) cfg = %v, want primary's already-loaded v142 config", cfg)
	}
}
