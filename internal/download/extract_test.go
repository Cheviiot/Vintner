package download

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCombineDirTreesNonexistentSrcIsNoop(t *testing.T) {
	dest := t.TempDir()
	if err := combineDirTrees(filepath.Join(t.TempDir(), "does-not-exist"), dest); err != nil {
		t.Fatalf("combineDirTrees with a nonexistent src returned an error: %v", err)
	}
}

func TestCombineDirTreesRenamesWholesaleWhenDestMissing(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "nested", "dest")
	writeFile(t, filepath.Join(src, "file.txt"), "hello")

	if err := combineDirTrees(src, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(filepath.Join(dest, "file.txt")); err != nil {
		t.Errorf("expected %s/file.txt to exist after combine: %v", dest, err)
	}
	if isDir(src) {
		t.Error("src should have been moved (renamed), not copied")
	}
}

func TestCombineDirTreesMergesNewSubdir(t *testing.T) {
	root := t.TempDir()
	src, dest := filepath.Join(root, "src"), filepath.Join(root, "dest")
	writeFile(t, filepath.Join(src, "NewDir", "a.txt"), "a")
	writeFile(t, filepath.Join(dest, "Existing.txt"), "keep me")

	if err := combineDirTrees(src, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(filepath.Join(dest, "NewDir", "a.txt")); err != nil {
		t.Errorf("expected merged NewDir/a.txt: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(dest, "Existing.txt")); err != nil {
		t.Errorf("pre-existing dest file was lost: %v", err)
	}
}

func TestCombineDirTreesMergesCaseInsensitiveCollision(t *testing.T) {
	root := t.TempDir()
	src, dest := filepath.Join(root, "src"), filepath.Join(root, "dest")
	// src has "Include" (capital I), dest already has "include" (lowercase) -
	// this is exactly the MSVC/WinSDK casing-inconsistency scenario the
	// function's doc comment describes.
	writeFile(t, filepath.Join(src, "Include", "new.h"), "new")
	writeFile(t, filepath.Join(dest, "include", "old.h"), "old")

	if err := combineDirTrees(src, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(filepath.Join(dest, "include", "new.h")); err != nil {
		t.Errorf("new.h should have merged into the existing lowercase 'include' dir: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(dest, "include", "old.h")); err != nil {
		t.Errorf("old.h should still be there: %v", err)
	}
	if isDir(filepath.Join(dest, "Include")) {
		t.Error("a separate capital-I 'Include' dir should not have been created")
	}
}

func TestCombineDirTreesRecursesIntoExactNameMatch(t *testing.T) {
	root := t.TempDir()
	src, dest := filepath.Join(root, "src"), filepath.Join(root, "dest")
	writeFile(t, filepath.Join(src, "lib", "x64", "new.lib"), "new")
	writeFile(t, filepath.Join(dest, "lib", "x64", "old.lib"), "old")

	if err := combineDirTrees(src, dest); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"new.lib", "old.lib"} {
		if _, err := os.ReadFile(filepath.Join(dest, "lib", "x64", f)); err != nil {
			t.Errorf("expected lib/x64/%s to survive the merge: %v", f, err)
		}
	}
}

func TestCopyRedirectedAssembliesNoConfigIsNoop(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "MSBuild.exe")
	if err := CopyRedirectedAssemblies(app); err != nil {
		t.Fatalf("with no .config file present, expected no error, got: %v", err)
	}
}

func TestCopyRedirectedAssembliesCopiesReferencedDLL(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "MSBuild.exe")
	writeFile(t, app+".config", `<?xml version="1.0"?>
<configuration>
  <runtime>
    <assemblyBinding xmlns="urn:schemas-microsoft-com:asm.v1">
      <dependentAssembly>
        <codeBase version="1.0.0.0" href="amd64\Some.Assembly.dll"/>
      </dependentAssembly>
    </assemblyBinding>
  </runtime>
</configuration>`)
	writeFile(t, filepath.Join(dir, "amd64", "Some.Assembly.dll"), "binary-content")

	if err := CopyRedirectedAssemblies(app); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "Some.Assembly.dll"))
	if err != nil {
		t.Fatalf("expected Some.Assembly.dll copied next to MSBuild.exe: %v", err)
	}
	if string(got) != "binary-content" {
		t.Errorf("copied file content = %q, want %q", got, "binary-content")
	}
}

func TestCopyRedirectedAssembliesSkipsMissingTarget(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "MSBuild.exe")
	writeFile(t, app+".config", `<?xml version="1.0"?>
<configuration>
  <runtime>
    <assemblyBinding xmlns="urn:schemas-microsoft-com:asm.v1">
      <dependentAssembly>
        <codeBase href="nowhere\Missing.dll"/>
      </dependentAssembly>
    </assemblyBinding>
  </runtime>
</configuration>`)

	if err := CopyRedirectedAssemblies(app); err != nil {
		t.Fatalf("a redirect pointing at a nonexistent file should be silently skipped, got: %v", err)
	}
}
