package main

import "testing"

// TestRunCLIDispatch covers the subset of runCLI's switch that has no side
// effects (no filesystem/network touched) - the actual subcommand bodies
// (download/install/env) get their own focused tests.
func TestRunCLIDispatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"no args prints usage", nil, 1},
		{"unknown subcommand", []string{"frobnicate"}, 1},
		{"help long", []string{"--help"}, 0},
		{"help short flag", []string{"-h"}, 0},
		{"help word", []string{"help"}, 0},
		{"help alias", []string{"h"}, 0},
		{"version word", []string{"version"}, 0},
		{"version alias", []string{"v"}, 0},
		{"version flag", []string{"--version"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runCLI(tc.args); got != tc.want {
				t.Errorf("runCLI(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
