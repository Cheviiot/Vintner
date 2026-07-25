package main

import "testing"

func TestRunEnvRequiresBin(t *testing.T) {
	if code := runEnv(nil); code != 1 {
		t.Errorf("runEnv(nil) = %d, want 1", code)
	}
}

func TestRunEnvRejectsMissingBinDir(t *testing.T) {
	if code := runEnv([]string{"--bin", "/nonexistent/path/for/vintner/tests"}); code != 1 {
		t.Errorf("runEnv with a nonexistent --bin = %d, want 1", code)
	}
}

func TestToUnixPathList(t *testing.T) {
	got := toUnixPathList(`z:\vc\include;z:\kits\10\include`)
	want := "/vc/include;/kits/10/include"
	if got != want {
		t.Errorf("toUnixPathList(...) = %q, want %q", got, want)
	}
}
