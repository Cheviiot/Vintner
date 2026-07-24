package wrapper

import "testing"

func TestClStdoutFilterNoteIncluding(t *testing.T) {
	in := `Note: including file: z:\home\user\project\test.h`
	want := `Note: including file: /home/user/project/test.h`
	if got := clStdoutFilter(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestClStdoutFilterLineDirective(t *testing.T) {
	in := `#line 5 "z:\\home\\user\\project\\test.c"`
	want := `#line 5 "/home/user/project/test.c"`
	if got := clStdoutFilter(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestClStdoutFilterErrorLine(t *testing.T) {
	in := `z:\home\user\project\test.c(5): warning C4996: 'foo' was declared deprecated`
	want := `/home/user/project/test.c(5): warning C4996: 'foo' was declared deprecated`
	if got := clStdoutFilter(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestClStdoutFilterNoteLine(t *testing.T) {
	in := `z:\home\user\project\test.h(3): note: see declaration of 'foo'`
	want := `/home/user/project/test.h(3): note: see declaration of 'foo'`
	if got := clStdoutFilter(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestClStdoutFilterPassthrough(t *testing.T) {
	in := "test.c"
	if got := clStdoutFilter(in); got != in {
		t.Errorf("got %q want %q", got, in)
	}
}

func TestClStderrFilter(t *testing.T) {
	in := `Note: including file: z:\a\b.h`
	want := `Note: including file: /a/b.h`
	if got := clStderrFilter(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// stderr does NOT get the error/warning/line-directive rewrite.
	in2 := `z:\a\b.c(1): error C2065: undeclared identifier`
	if got := clStderrFilter(in2); got != in2 {
		t.Errorf("stderr should pass through error lines unchanged, got %q", got)
	}
}

func TestDumpbinStdoutFilter(t *testing.T) {
	in := `  PDB file found at z:\a\b\file.pdb`
	want := `  PDB file found at /a/b/file.pdb`
	if got := dumpbinStdoutFilter(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}

	in2 := `Dump of file z:\a\b\file.exe`
	want2 := `Dump of file /a/b/file.exe`
	if got := dumpbinStdoutFilter(in2); got != want2 {
		t.Errorf("got %q want %q", got, want2)
	}
}

func TestStripCR(t *testing.T) {
	if got := stripCR("hello\r"); got != "hello" {
		t.Errorf("got %q", got)
	}
	if got := stripCR("no cr here"); got != "no cr here" {
		t.Errorf("got %q", got)
	}
}
