package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionFilesSaveAndCloseWritesSessionAndIndex(t *testing.T) {
	root := t.TempDir()
	s, err := NewSessionFiles(root)
	if err != nil {
		t.Fatalf("NewSessionFiles: %v", err)
	}

	ctx := context.Background()
	if err := s.Save(ctx, Entry{Kind: KindFact, Content: "fixtures live in testdata/golden"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, Entry{
		Kind:    KindSessionSummary,
		Content: "Worked on memory implementation",
		Tags:    []string{"memory", "tests"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	idx, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	if !strings.Contains(string(idx), "Worked on memory implementation") {
		t.Errorf("index.json missing the session summary:\n%s", idx)
	}

	entries, err := os.ReadDir(filepath.Join(root, "sessions"))
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 session file, got %d", len(entries))
	}

	body, _ := os.ReadFile(filepath.Join(root, "sessions", entries[0].Name()))
	for _, want := range []string{
		"Worked on memory implementation",
		"## Facts",
		"fixtures live in testdata/golden",
		"tags: memory, tests",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("session file missing %q\nfull:\n%s", want, body)
		}
	}
}

// Empty sessions shouldn't leak into the index — opening, doing nothing,
// and closing leaves the store untouched.
func TestSessionFilesEmptyCloseIsNoop(t *testing.T) {
	root := t.TempDir()
	s, _ := NewSessionFiles(root)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "index.json")); err == nil {
		t.Error("empty session created an index.json")
	}
	if entries, err := os.ReadDir(filepath.Join(root, "sessions")); err == nil && len(entries) > 0 {
		t.Errorf("empty session created %d session file(s)", len(entries))
	}
}

// After a session is closed, reopening the store should re-load the index
// and surface the previous session via Preamble and Recall.
func TestSessionFilesReopenLoadsHistory(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	s, _ := NewSessionFiles(root)
	_ = s.Save(ctx, Entry{
		Kind:    KindSessionSummary,
		Content: "set up compaction strategies",
		Tags:    []string{"compaction", "strategy"},
	})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := NewSessionFiles(root)
	if err != nil {
		t.Fatal(err)
	}

	pre, _ := s2.Preamble(ctx)
	if !strings.Contains(pre, "set up compaction strategies") {
		t.Errorf("preamble after reopen missing prior summary:\n%s", pre)
	}

	matches, _ := s2.Recall(ctx, "compaction", 5)
	if len(matches) != 1 {
		t.Fatalf("Recall returned %d entries, want 1", len(matches))
	}
	if !strings.Contains(matches[0].Content, "set up compaction strategies") {
		t.Errorf("Recall returned unexpected content: %s", matches[0].Content)
	}
}

// Recall is matched by tag OR summary substring.
func TestSessionFilesRecallMatchesTagsAndSummary(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// Session 1: tagged "mcp", summary about deepwiki.
	s, _ := NewSessionFiles(root)
	_ = s.Save(ctx, Entry{
		Kind:    KindSessionSummary,
		Content: "added the deepwiki server",
		Tags:    []string{"mcp", "remote-tools"},
	})
	_ = s.Close()

	// Session 2: tagged "ui", summary mentions the word "compaction" in body.
	s, _ = NewSessionFiles(root)
	_ = s.Save(ctx, Entry{
		Kind:    KindSessionSummary,
		Content: "refactored compaction tests",
		Tags:    []string{"tests"},
	})
	_ = s.Close()

	s2, _ := NewSessionFiles(root)

	// Tag match on the first session.
	got, _ := s2.Recall(ctx, "mcp", 10)
	if len(got) != 1 || !strings.Contains(got[0].Content, "deepwiki") {
		t.Errorf("tag match failed, got: %+v", got)
	}

	// Summary substring match on the second.
	got, _ = s2.Recall(ctx, "compaction", 10)
	if len(got) != 1 || !strings.Contains(got[0].Content, "refactored") {
		t.Errorf("summary match failed, got: %+v", got)
	}

	// No matches returns empty.
	got, _ = s2.Recall(ctx, "nonexistent-topic", 10)
	if len(got) != 0 {
		t.Errorf("expected 0 matches, got %d", len(got))
	}
}

// If the session file referenced in index.json was deleted manually, the
// loader should prune the dead reference on the next open.
func TestSessionFilesPrunesMissingFiles(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	s, _ := NewSessionFiles(root)
	_ = s.Save(ctx, Entry{Kind: KindSessionSummary, Content: "to be deleted", Tags: []string{"x"}})
	_ = s.Close()

	// Sanity: index has one entry now.
	s2, _ := NewSessionFiles(root)
	if matches, _ := s2.Recall(ctx, "", 5); len(matches) != 1 {
		t.Fatalf("expected 1 recall match before pruning, got %d", len(matches))
	}

	// Remove the session file behind the index's back.
	entries, _ := os.ReadDir(filepath.Join(root, "sessions"))
	for _, f := range entries {
		_ = os.Remove(filepath.Join(root, "sessions", f.Name()))
	}

	// Reopening should prune.
	s3, _ := NewSessionFiles(root)
	if matches, _ := s3.Recall(ctx, "", 5); len(matches) != 0 {
		t.Errorf("expected 0 matches after pruning, got %d", len(matches))
	}
}
