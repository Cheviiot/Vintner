package download

import (
	"encoding/json"
	"testing"
)

// fixtureManifest is a trimmed-down stand-in for a real installer manifest,
// covering: a workload -> component -> {required, Optional, Recommended}
// dependency chain, host-arch variants of the same package id, and
// language variants - enough to exercise BuildIndex/collectDependencyClosure
// offline, without ever hitting the network.
const fixtureManifestJSON = `{
  "info": {"productDisplayVersion": "test"},
  "packages": [
    {
      "id": "Microsoft.VisualStudio.Workload.VCTools",
      "type": "Workload",
      "dependencies": {
        "Microsoft.VisualStudio.Component.VC.Tools.x86.x64": "1.0"
      }
    },
    {
      "id": "Microsoft.VisualStudio.Component.VC.Tools.x86.x64",
      "type": "Component",
      "dependencies": {
        "Microsoft.VC.Tools.Core": "1.0",
        "Microsoft.VC.Tools.Optional": {"version": "1.0", "type": "Optional"},
        "Microsoft.VC.Tools.Recommended": {"version": "1.0", "type": "Recommended"}
      }
    },
    {
      "id": "Microsoft.VC.Tools.Core",
      "type": "Msi",
      "machineArch": "x86",
      "payloads": [{"fileName": "core-x86.msi", "url": "https://example.invalid/core-x86.msi", "sha256": "aa", "size": 100}]
    },
    {
      "id": "Microsoft.VC.Tools.Core",
      "type": "Msi",
      "machineArch": "arm64",
      "payloads": [{"fileName": "core-arm64.msi", "url": "https://example.invalid/core-arm64.msi", "sha256": "bb", "size": 200}]
    },
    {
      "id": "Microsoft.VC.Tools.Optional",
      "type": "Msi",
      "payloads": [{"fileName": "optional.msi", "url": "https://example.invalid/optional.msi", "size": 10}]
    },
    {
      "id": "Microsoft.VC.Tools.Recommended",
      "type": "Msi",
      "payloads": [{"fileName": "recommended.msi", "url": "https://example.invalid/recommended.msi", "size": 20}]
    },
    {
      "id": "Microsoft.VisualStudio.Resources",
      "type": "Msi",
      "language": "en-US",
      "payloads": [{"fileName": "res-en.msi", "url": "https://example.invalid/res-en.msi", "size": 1}]
    },
    {
      "id": "Microsoft.VisualStudio.Resources",
      "type": "Msi",
      "language": "de-DE",
      "payloads": [{"fileName": "res-de.msi", "url": "https://example.invalid/res-de.msi", "size": 1}]
    }
  ]
}`

func fixtureIndex(t *testing.T, hostArch string) Index {
	t.Helper()
	var m Manifest
	if err := json.Unmarshal([]byte(fixtureManifestJSON), &m); err != nil {
		t.Fatalf("parsing fixture manifest: %v", err)
	}
	return BuildIndex(&m, hostArch, "en")
}

func TestBuildIndexPrioritizesHostArch(t *testing.T) {
	idx := fixtureIndex(t, "x86")
	p := idx.Find("Microsoft.VC.Tools.Core", nil)
	if p == nil {
		t.Fatal("expected to find Microsoft.VC.Tools.Core")
	}
	if p.MachineArch != "x86" {
		t.Errorf("expected x86 variant to sort first for host x86, got machineArch=%q", p.MachineArch)
	}

	idx64 := fixtureIndex(t, "arm64")
	p64 := idx64.Find("Microsoft.VC.Tools.Core", nil)
	if p64.MachineArch != "arm64" {
		t.Errorf("expected arm64 variant to sort first for host arm64, got machineArch=%q", p64.MachineArch)
	}
}

func TestBuildIndexPrioritizesLanguage(t *testing.T) {
	idx := fixtureIndex(t, "x86")
	p := idx.Find("Microsoft.VisualStudio.Resources", nil)
	if p == nil {
		t.Fatal("expected to find Microsoft.VisualStudio.Resources")
	}
	if p.Language != "en-US" {
		t.Errorf("expected en-US variant to sort first for language=en, got %q", p.Language)
	}
}

func TestAggregateDependsDefaultExcludesOptionalOnly(t *testing.T) {
	// Optional deps are skipped unless --include-optional is passed, but
	// Recommended deps are pulled in unless --skip-recommended is passed -
	// it's "recommended", not "extra".
	idx := fixtureIndex(t, "x86")
	opts := &Options{
		Package:  []string{"Microsoft.VisualStudio.Workload.VCTools"},
		HostArch: "x86",
		OnlyHost: true,
	}
	selected, err := ExpandSelection(idx, opts)
	if err != nil {
		t.Fatal(err)
	}
	ids := idsOf(selected)

	mustContain(t, ids, "Microsoft.VisualStudio.Workload.VCTools")
	mustContain(t, ids, "Microsoft.VisualStudio.Component.VC.Tools.x86.x64")
	mustContain(t, ids, "Microsoft.VC.Tools.Core")
	mustContain(t, ids, "Microsoft.VC.Tools.Recommended")
	mustNotContain(t, ids, "Microsoft.VC.Tools.Optional")
}

func TestAggregateDependsIncludeOptional(t *testing.T) {
	idx := fixtureIndex(t, "x86")
	opts := &Options{
		Package:         []string{"Microsoft.VisualStudio.Workload.VCTools"},
		HostArch:        "x86",
		OnlyHost:        true,
		IncludeOptional: true,
	}
	selected, err := ExpandSelection(idx, opts)
	if err != nil {
		t.Fatal(err)
	}
	ids := idsOf(selected)
	mustContain(t, ids, "Microsoft.VC.Tools.Optional")
	// Recommended is included by default (only --skip-recommended excludes it).
	mustContain(t, ids, "Microsoft.VC.Tools.Recommended")
}

func TestAggregateDependsSkipRecommended(t *testing.T) {
	idx := fixtureIndex(t, "x86")
	opts := &Options{
		Package:         []string{"Microsoft.VisualStudio.Workload.VCTools"},
		HostArch:        "x86",
		OnlyHost:        true,
		SkipRecommended: true,
	}
	selected, err := ExpandSelection(idx, opts)
	if err != nil {
		t.Fatal(err)
	}
	ids := idsOf(selected)
	mustNotContain(t, ids, "Microsoft.VC.Tools.Recommended")
}

func TestAggregateDependsRespectsIgnore(t *testing.T) {
	idx := fixtureIndex(t, "x86")
	opts := &Options{
		Package:  []string{"Microsoft.VisualStudio.Workload.VCTools"},
		Ignore:   []string{"microsoft.vc.tools.core"},
		HostArch: "x86",
		OnlyHost: true,
	}
	selected, err := ExpandSelection(idx, opts)
	if err != nil {
		t.Fatal(err)
	}
	ids := idsOf(selected)
	mustNotContain(t, ids, "Microsoft.VC.Tools.Core")
	mustContain(t, ids, "Microsoft.VisualStudio.Component.VC.Tools.x86.x64")
}

func TestAggregateDependsHostArchMismatchExcludes(t *testing.T) {
	idx := fixtureIndex(t, "x86")
	opts := &Options{
		Package:  []string{"Microsoft.VC.Tools.Core"},
		HostArch: "arm", // no arm variant exists, only x86/arm64
		OnlyHost: true,
	}
	selected, err := ExpandSelection(idx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 0 {
		t.Errorf("expected no packages selected for mismatched host arch, got %v", idsOf(selected))
	}
}

func idsOf(pkgs []*Package) []string {
	var ids []string
	for _, p := range pkgs {
		ids = append(ids, p.ID)
	}
	return ids
}

func mustContain(t *testing.T, ids []string, want string) {
	t.Helper()
	for _, id := range ids {
		if id == want {
			return
		}
	}
	t.Errorf("expected %v to contain %q", ids, want)
}

func mustNotContain(t *testing.T, ids []string, unwanted string) {
	t.Helper()
	for _, id := range ids {
		if id == unwanted {
			t.Errorf("expected %v to NOT contain %q", ids, unwanted)
		}
	}
}
