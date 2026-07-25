package download

import (
	"encoding/json"
	"testing"
)

func TestPayloadName(t *testing.T) {
	for _, tc := range []struct {
		fileName string
		want     string
	}{
		{"payload.msi", "payload.msi"},
		{"folder/payload.msi", "payload.msi"},
		{`folder\payload.msi`, "payload.msi"},
		{`a\b/c\payload.msi`, "payload.msi"},
		{"", ""},
	} {
		p := Payload{FileName: tc.fileName}
		if got := p.Name(); got != tc.want {
			t.Errorf("Payload{FileName: %q}.Name() = %q, want %q", tc.fileName, got, tc.want)
		}
	}
}

func TestPackageKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Package
		want string
	}{
		{"id only", Package{ID: "Foo"}, "Foo"},
		{"id+version", Package{ID: "Foo", Version: "1.0"}, "Foo-1.0"},
		{
			"id+version+all arches",
			Package{ID: "Foo", Version: "1.0", Chip: "x64", MachineArch: "x86", ProductArch: "neutral"},
			"Foo-1.0-chip.x64-machineArch.x86-productArch.neutral",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.Key(); got != tc.want {
				t.Errorf("Key() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPackageLocalized(t *testing.T) {
	noResources := Package{}
	if got := noResources.Localized("en"); got != nil {
		t.Errorf("Localized() on a package with no LocalizedResources = %v, want nil", got)
	}

	p := Package{LocalizedResources: []LocalizedResource{
		{Language: "de-DE", Title: "Deutsch"},
		{Language: "en-US", Title: "English"},
		{Language: "ru-RU", Title: "Русский"},
	}}

	for _, tc := range []struct {
		lang      string
		wantTitle string
	}{
		{"ru", "Русский"},
		{"ru-RU", "Русский"},
		{"en", "English"},
		{"", "English"},   // "" defaults to "en"
		{"fr", "English"}, // no fr variant, falls back to the en-* one
	} {
		t.Run("lang="+tc.lang, func(t *testing.T) {
			got := p.Localized(tc.lang)
			if got == nil {
				t.Fatalf("Localized(%q) = nil", tc.lang)
			}
			if got.Title != tc.wantTitle {
				t.Errorf("Localized(%q).Title = %q, want %q", tc.lang, got.Title, tc.wantTitle)
			}
		})
	}
}

func TestPackageSizes(t *testing.T) {
	p := Package{
		InstallSizes: map[string]int64{"x86": 100, "x64": 200},
		Payloads:     []Payload{{Size: 10}, {Size: 20}, {Size: 30}},
	}
	if got := p.InstalledSize(); got != 300 {
		t.Errorf("InstalledSize() = %d, want 300", got)
	}
	if got := p.DownloadSize(); got != 60 {
		t.Errorf("DownloadSize() = %d, want 60", got)
	}
}

func TestPackageDependenciesNormalizesBothShapes(t *testing.T) {
	p := Package{DependenciesRaw: map[string]json.RawMessage{
		"Bare.Version":    json.RawMessage(`"1.0"`),
		"Full.Object":     json.RawMessage(`{"version":"2.0","type":"Optional","id":"Real.Target"}`),
		"Recommended.Dep": json.RawMessage(`{"version":"3.0","type":"Recommended"}`),
	}}
	deps := p.Dependencies()

	if d := deps["Bare.Version"]; d.Version != "1.0" || d.TargetID != "" || d.Type != "" {
		t.Errorf("Bare.Version = %+v, want Version=1.0 TargetID='' Type=''", d)
	}
	if d := deps["Full.Object"]; d.Version != "2.0" || d.TargetID != "Real.Target" || d.Type != "Optional" {
		t.Errorf("Full.Object = %+v, want Version=2.0 TargetID=Real.Target Type=Optional", d)
	}
	if d := deps["Recommended.Dep"]; d.Version != "3.0" || d.Type != "Recommended" {
		t.Errorf("Recommended.Dep = %+v, want Version=3.0 Type=Recommended", d)
	}

	// Calling Dependencies() again must return the same cached map, not
	// re-parse (and must not panic on the second call).
	if d2 := p.Dependencies(); len(d2) != len(deps) {
		t.Errorf("second Dependencies() call returned a different map: %v vs %v", d2, deps)
	}
}

func TestHumanizeBytes(t *testing.T) {
	for _, tc := range []struct {
		size int64
		want string
	}{
		{500, "500 bytes"},
		{2048, "2.0 KB"},
		{5 * 1024 * 1024, "5.0 MB"},
		{2 * 1024 * 1024 * 1024, "2.0 GB"},
	} {
		if got := HumanizeBytes(tc.size); got != tc.want {
			t.Errorf("HumanizeBytes(%d) = %q, want %q", tc.size, got, tc.want)
		}
	}
}
