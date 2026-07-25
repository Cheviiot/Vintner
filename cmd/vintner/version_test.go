package main

import "testing"

func TestFormatVersion(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		ver, revision, buildTime string
		dirty                    bool
		want                     string
	}{
		{
			name: "no VCS info at all",
			ver:  "dev",
			want: "vintner dev",
		},
		{
			name:      "clean build with full info",
			ver:       "0.3.0",
			revision:  "6186837fd23616335ba8aff830801692a756799c",
			buildTime: "2026-07-25T01:33:41Z",
			want:      "vintner 0.3.0 (6186837fd236, 2026-07-25T01:33:41Z)",
		},
		{
			name:     "dirty tree",
			ver:      "0.3.0",
			revision: "6186837fd23616335ba8aff830801692a756799c",
			dirty:    true,
			want:     "vintner 0.3.0 (6186837fd236-dirty)",
		},
		{
			name:     "short revision left untouched",
			ver:      "dev",
			revision: "abc123",
			want:     "vintner dev (abc123)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := formatVersion(tc.ver, tc.revision, tc.buildTime, tc.dirty)
			if got != tc.want {
				t.Errorf("formatVersion(%q, %q, %q, %v) = %q, want %q",
					tc.ver, tc.revision, tc.buildTime, tc.dirty, got, tc.want)
			}
		})
	}
}
