// Package wineenv computes the environment (INCLUDE/LIB/WINEPATH/...) needed
// to run a Wine-hosted MSVC tool.
package wineenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigFileName is the per-architecture config dropped next to the tool
// symlinks by `msvc-go-wine install`.
const ConfigFileName = "env.json"

// Config is the per-architecture info generated at install time.
type Config struct {
	Arch       string `json:"arch"`        // x86, x64, arm, arm64 (target arch)
	Host       string `json:"host"`        // x64 or arm64 (Host<Host> bin dir suffix)
	DotnetHost string `json:"dotnet_host"` // amd64 or arm64 (.NET tool host dir suffix)
	MSVCVer    string `json:"msvc_ver"`
	SDKVer     string `json:"sdk_ver"`
}

// Load reads env.json from dir (the directory the running binary was
// invoked from, i.e. <dest>/bin/<arch>).
func Load(dir string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(dir, ConfigFileName))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", ConfigFileName, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ConfigFileName, err)
	}
	return &c, nil
}

// Save writes env.json into dir.
func (c *Config) Save(dir string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ConfigFileName), data, 0o644)
}
