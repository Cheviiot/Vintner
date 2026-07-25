package download

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetryBackoff(t *testing.T) {
	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 10 * time.Second}, // capped
		{10, 10 * time.Second},
	} {
		if got := retryBackoff(tc.attempt); got != tc.want {
			t.Errorf("retryBackoff(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestSHA256File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := sha256File(path)
	if err != nil {
		t.Fatal(err)
	}
	// echo -n hello | sha256sum
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("sha256File(hello) = %q, want %q", got, want)
	}
}

func TestEqualFoldHex(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"ABCDEF", "abcdef", true},
		{"abc123", "ABC123", true},
		{"abc123", "abc124", false},
		{"abc", "abcd", false},
		{"", "", true},
	} {
		if got := equalFoldHex(tc.a, tc.b); got != tc.want {
			t.Errorf("equalFoldHex(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
