package wrapper

import (
	"testing"

	"github.com/Cheviiot/vintner/internal/wineenv"
)

func TestSpecExeDir(t *testing.T) {
	paths := &wineenv.Paths{
		BinDir:        "/bin-dir",
		SDKBinDir:     "/sdk-bin-dir",
		MSBuildBinDir: "/msbuild-bin-dir",
	}
	for _, tc := range []struct {
		name string
		dir  dirKind
		want string
	}{
		{"dirBin", dirBin, "/bin-dir"},
		{"dirSDK", dirSDK, "/sdk-bin-dir"},
		{"dirMSBuild", dirMSBuild, "/msbuild-bin-dir"},
	} {
		s := spec{dir: tc.dir}
		if got := s.exeDir(paths); got != tc.want {
			t.Errorf("%s: exeDir() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestToolsAndNativeToolsAreDisjoint(t *testing.T) {
	for name := range Tools {
		if nativeTools[name] {
			t.Errorf("%q is in both Tools and nativeTools", name)
		}
	}
}

func TestEveryToolHasAnExeName(t *testing.T) {
	for name, s := range Tools {
		if s.exeName == "" {
			t.Errorf("Tools[%q] has no exeName", name)
		}
	}
}
