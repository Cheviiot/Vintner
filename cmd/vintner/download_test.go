package main

import "testing"

// TestRunDownloadRejectsInvalidArchFlags exercises the validation added
// after the flags are parsed, which must reject typos before runDownload
// gets anywhere near the network (FetchChannelManifest) - these tests would
// hang/fail on network access if that ordering ever regressed.
func TestRunDownloadRejectsInvalidArchFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"bad architecture", []string{"--architecture", "x866"}},
		{"bad architecture, valid mixed with invalid", []string{"--architecture", "x64", "--architecture", "sparc"}},
		{"bad host-arch", []string{"--host-arch", "sparc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := runDownload(tc.args); code != 2 {
				t.Errorf("runDownload(%v) = %d, want 2", tc.args, code)
			}
		})
	}
}

func TestValidArchitectureSets(t *testing.T) {
	for _, a := range []string{"x86", "x64", "arm", "arm64", "host"} {
		if !validArchitectures[a] {
			t.Errorf("validArchitectures[%q] = false, want true", a)
		}
	}
	for _, a := range []string{"x86", "x64", "arm64"} {
		if !validHostArchs[a] {
			t.Errorf("validHostArchs[%q] = false, want true", a)
		}
	}
	if validHostArchs["arm"] {
		t.Error(`validHostArchs["arm"] = true, want false (no 32-bit ARM host toolchain exists)`)
	}
}
