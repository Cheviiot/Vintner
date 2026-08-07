package wrapper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

// findEnv returns the value of key in env ("KEY=value" entries), and
// whether it was present at all.
func findEnv(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix), true
		}
	}
	return "", false
}

func TestBuildEnvLeavesWINEPREFIXAloneWithoutOverride(t *testing.T) {
	t.Setenv("WINEPREFIX", "/some/inherited/prefix")

	env := buildEnv(&wineenv.Paths{}, nil)

	got, ok := findEnv(env, "WINEPREFIX")
	if !ok || got != "/some/inherited/prefix" {
		t.Errorf("WINEPREFIX = (%q, %v), want the inherited value untouched", got, ok)
	}
}

func TestBuildEnvAppliesWinePrefixEnvOverride(t *testing.T) {
	t.Setenv("WINEPREFIX", "/some/inherited/prefix")

	env := buildEnv(&wineenv.Paths{}, []string{"WINEPREFIX=/isolated/bundled/prefix"})

	got, ok := findEnv(env, "WINEPREFIX")
	if !ok || got != "/isolated/bundled/prefix" {
		t.Errorf("WINEPREFIX = (%q, %v), want the override to replace the inherited value", got, ok)
	}
	// Only one WINEPREFIX entry should survive, not both.
	count := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "WINEPREFIX=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("found %d WINEPREFIX entries in the built env, want exactly 1", count)
	}
}

// TestRunViaToolRelayDoesNotHangWhenRelayNeverOpensFifos reproduces the
// scenario where wine (or toolrelay.exe itself) exits without ever opening
// the stdout/stderr FIFOs it's supposed to relay tool output through - wine
// failing to launch the relay, or the relay crashing before it reaches its
// own FIFO-open code. Before unblockFifo existed, the reader goroutines'
// blocking os.Open() calls had no writer left to rendezvous with and would
// hang forever, even though the underlying process had already exited -
// nothing, not even VINTNER_TIMEOUT, bounds that wait. A fake "wine" that
// just exits 0 without touching either FIFO reproduces exactly that.
func TestRunViaToolRelayDoesNotHangWhenRelayNeverOpensFifos(t *testing.T) {
	fakeWine := filepath.Join(t.TempDir(), "fake-wine")
	if err := os.WriteFile(fakeWine, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		done <- runViaToolRelay(fakeWine, "relay.exe", "tool.exe", nil, &wineenv.Paths{}, nil, nil, nil)
	}()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("runViaToolRelay returned %d, want 0 (fake wine always exits 0)", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runViaToolRelay hung: the FIFO reader goroutines were never unblocked after the underlying process exited without opening them")
	}
}
