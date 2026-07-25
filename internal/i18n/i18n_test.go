package i18n

import (
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
