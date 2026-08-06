package main

import (
	"runtime"
	"testing"
)

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

func TestContains(t *testing.T) {
	list := []string{"x86", "x64", "arm64"}
	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"x64", true},
		{"arm64", true},
		{"arm", false},
		{"", false},
	} {
		if got := contains(list, tc.v); got != tc.want {
			t.Errorf("contains(%v, %q) = %v, want %v", list, tc.v, got, tc.want)
		}
	}
	if contains(nil, "x64") {
		t.Error("contains(nil, ...) = true, want false")
	}
}

// TestDetectHostArch can't exercise both branches (runtime.GOARCH is fixed
// for the whole build, not something a test can swap), but it does pin
// down that the function's result actually agrees with the real GOARCH
// this test binary was built for, rather than e.g. always returning "x64"
// unconditionally.
func TestDetectHostArch(t *testing.T) {
	want := "x64"
	if runtime.GOARCH == "arm64" {
		want = "arm64"
	}
	if got := detectHostArch(); got != want {
		t.Errorf("detectHostArch() = %q, want %q (runtime.GOARCH = %q)", got, want, runtime.GOARCH)
	}
}
