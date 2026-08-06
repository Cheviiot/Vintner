package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	envKeys := []string{"VINTNER_LANG", "LC_ALL", "LC_MESSAGES", "LANG"}
	clear := func() {
		for _, k := range envKeys {
			t.Setenv(k, "")
			// t.Setenv("", "") leaves the var set-but-empty, which detect()
			// already treats as "unset" (its loop skips v == "") - matches
			// how a genuinely-unset env var behaves for this function.
		}
	}

	for _, tc := range []struct {
		name string
		env  map[string]string
		want Lang
	}{
		{"nothing set defaults to English", nil, EN},
		{"VINTNER_LANG=ru", map[string]string{"VINTNER_LANG": "ru"}, RU},
		{"VINTNER_LANG=en", map[string]string{"VINTNER_LANG": "en"}, EN},
		{"VINTNER_LANG wins over a Russian LANG", map[string]string{"VINTNER_LANG": "en", "LANG": "ru_RU.UTF-8"}, EN},
		{"LC_ALL wins over LANG", map[string]string{"LC_ALL": "ru_RU.UTF-8", "LANG": "en_US.UTF-8"}, RU},
		{"LANG=ru_RU.UTF-8 alone", map[string]string{"LANG": "ru_RU.UTF-8"}, RU},
		{"LANG=en_US.UTF-8 alone", map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{"unrelated locale defaults to English", map[string]string{"LANG": "de_DE.UTF-8"}, EN},
		{"case-insensitive RU prefix", map[string]string{"VINTNER_LANG": "RU"}, RU},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clear()
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := detect(); got != tc.want {
				t.Errorf("detect() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCatalogCompleteness guards against adding an EN string without its RU
// counterpart (or vice versa) - a silent gap here degrades to showing the
// wrong language's text via T()'s EN-fallback rather than failing loudly.
func TestCatalogCompleteness(t *testing.T) {
	for key, entry := range catalog {
		en, hasEN := entry[EN]
		if !hasEN || strings.TrimSpace(en) == "" {
			t.Errorf("catalog[%q] has no (non-empty) English translation", key)
		}
		ru, hasRU := entry[RU]
		if !hasRU || strings.TrimSpace(ru) == "" {
			t.Errorf("catalog[%q] has no (non-empty) Russian translation", key)
		}
	}
}

var reTCallSite = regexp.MustCompile(`i18n\.T\("([a-zA-Z0-9_.]+)"`)

// TestEveryCallSiteKeyExistsInCatalog scans every .go file in the module
// (from the repo root, two levels up from this package) for a call to T
// with a literal string key and checks each key is actually present in
// catalog. T()'s own fallback for a missing key (TestTMissingKeyReturns
// KeyItself) is to quietly return the raw key string instead of a real
// translation - a typo'd key at a call site compiles fine and only shows
// up as garbled untranslated text in the running CLI, nothing else catches
// it (TestCatalogCompleteness only checks EN/RU are both present *within*
// whatever's already in catalog, not that every real call site has an
// entry there at all).
func TestEveryCallSiteKeyExistsInCatalog(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range reTCallSite.FindAllStringSubmatch(string(data), -1) {
			key := m[1]
			if seen[key] {
				continue
			}
			seen[key] = true
			if _, ok := catalog[key]; !ok {
				t.Errorf("%s: i18n.T(%q) - key not found in catalog", path, key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) < 15 { // sanity check: the walk/regex didn't silently find nothing
		t.Fatalf("only found %d distinct i18n.T call sites across the module - the scan likely needs updating (wrong root, or T's call pattern changed): %v", len(seen), seen)
	}
}

func TestTMissingKeyReturnsKeyItself(t *testing.T) {
	got := T("no.such.key")
	if got != "no.such.key" {
		t.Errorf("T(unknown key) = %q, want the key itself", got)
	}
}

func TestTFallsBackToEnglish(t *testing.T) {
	const testKey = "test.fallback.only.en"
	catalog[testKey] = map[Lang]string{EN: "hello %s"}
	defer delete(catalog, testKey)

	saved := current
	current = RU
	defer func() { current = saved }()

	if got := T(testKey, "world"); got != "hello world" {
		t.Errorf("T(%q) with no RU entry = %q, want %q", testKey, got, "hello world")
	}
}

func TestTFormatsArgs(t *testing.T) {
	saved := current
	current = EN
	defer func() { current = saved }()

	got := T("download.wdk_installed", "x64", "10.0.26100.1", "/dest")
	want := "Installed WDK (x64) 10.0.26100.1 at /dest\n"
	if got != want {
		t.Errorf("T(download.wdk_installed, ...) = %q, want %q", got, want)
	}
}
