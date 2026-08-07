package wrapper

import (
	"reflect"
	"testing"
)

func TestParseCmdLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "double-slash c form",
			args: []string{"//c", "copy", "a", "b"},
			want: []string{"copy", "a", "b"},
		},
		{
			name: "bare command with no switches or chaining",
			args: []string{"cl", "/c", "foo.cpp"},
			want: []string{"cl", "/c", "foo.cpp"},
		},
		{
			name: "S C with a single setup call chained to the real command",
			args: []string{"/S", "/C", "call", "vcvarsall.bat", "x86_amd64", ">nul", "&&", "cl", "/c", "foo.cpp"},
			want: []string{"cl", "/c", "foo.cpp"},
		},
		{
			name: "S C lowercase switches",
			args: []string{"/s", "/c", "call", "setup.bat", "&&", "cl", "foo.cpp"},
			want: []string{"cl", "foo.cpp"},
		},
		{
			name: "multiple chained segments - only the last one runs",
			args: []string{"/S", "/C", "call", "a.bat", "&&", "call", "b.bat", "&&", "link", "foo.obj"},
			want: []string{"link", "foo.obj"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCmdLine(tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseCmdLine(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
