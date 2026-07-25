package main

import "testing"

func TestRunInstallRejectsExtraArgs(t *testing.T) {
	if code := runInstall([]string{"one", "two"}); code != 1 {
		t.Errorf("runInstall with two args = %d, want 1", code)
	}
}

func TestRunInstallHelp(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		if code := runInstall([]string{flag}); code != 1 {
			t.Errorf("runInstall([%q]) = %d, want 1", flag, code)
		}
	}
}
