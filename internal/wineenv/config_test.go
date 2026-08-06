package wineenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := &Config{
		Arch:            "x64",
		Host:            "x64",
		DotnetHost:      "amd64",
		MSVCVer:         "14.51.36231",
		SDKVer:          "10.0.26100.0",
		PlatformToolset: "145",
	}
	if err := want.Save(dir); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *want {
		t.Errorf("Load(Save(cfg)) = %+v, want %+v", got, want)
	}
}

func TestConfigSaveWritesToConfigFileName(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Arch: "x86"}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ConfigFileName)); err != nil {
		t.Errorf("expected %s to exist after Save: %v", ConfigFileName, err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("expected an error loading from a directory with no env.json")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("expected an error loading malformed JSON")
	}
}

// PlatformToolset is "omitempty" - an older install's env.json (written
// before that field existed) must still load cleanly, with it left as "".
func TestLoadOmitsEmptyPlatformToolset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(`{"arch":"x64","host":"x64","dotnet_host":"amd64","msvc_ver":"14.29.30133","sdk_ver":"10.0.19041.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlatformToolset != "" {
		t.Errorf("PlatformToolset = %q, want \"\" for an env.json predating that field", got.PlatformToolset)
	}
	if got.MSVCVer != "14.29.30133" {
		t.Errorf("MSVCVer = %q, want %q", got.MSVCVer, "14.29.30133")
	}
}
