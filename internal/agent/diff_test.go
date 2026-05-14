package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWriteDiffNewFileShowsAdditions(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "fresh.txt")
	rawInput := `{"path":"` + target + `","content":"line one\nline two\n"}`

	diff := buildWriteDiff(rawInput)
	if diff == "" {
		t.Fatal("buildWriteDiff returned empty for a new-file write")
	}
	// New-file diffs use /dev/null as the "from" so chroma highlights all
	// the body as additions.
	if !strings.Contains(diff, "/dev/null") {
		t.Errorf("expected /dev/null prelude for new file, got:\n%s", diff)
	}
	for _, want := range []string{"+line one", "+line two"} {
		if !strings.Contains(diff, want) {
			t.Errorf("missing %q in diff:\n%s", want, diff)
		}
	}
}

func TestBuildWriteDiffShowsChangedLines(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(target, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rawInput := `{"path":"` + target + `","content":"a\nB\nc\n"}`

	diff := buildWriteDiff(rawInput)
	if !strings.Contains(diff, "-b") {
		t.Errorf("expected a removal line `-b`, got:\n%s", diff)
	}
	if !strings.Contains(diff, "+B") {
		t.Errorf("expected an addition line `+B`, got:\n%s", diff)
	}
	// Context line should still be there
	if !strings.Contains(diff, " a") {
		t.Errorf("missing context line ` a`:\n%s", diff)
	}
}

func TestBuildWriteDiffIdenticalContentReturnsMarker(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "same.txt")
	if err := os.WriteFile(target, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rawInput := `{"path":"` + target + `","content":"hello\n"}`

	diff := buildWriteDiff(rawInput)
	if !strings.Contains(diff, "no changes") {
		t.Errorf("expected a 'no changes' marker, got:\n%q", diff)
	}
}

func TestBuildWriteDiffMalformedInputReturnsEmpty(t *testing.T) {
	if diff := buildWriteDiff(`not json`); diff != "" {
		t.Errorf("expected empty diff for malformed input, got: %q", diff)
	}
}
