package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// preambleSessionCount caps how many recent sessions get summarised into
// the system prompt. Bigger = more context auto-loaded per session, more
// tokens spent on every turn (or every cached read, with prompt caching).
const preambleSessionCount = 5

// SessionFiles is the default Store: one markdown file per session under
// <root>/sessions/, with <root>/index.json as a fast-lookup layer over
// them. Both files are human-readable and git-friendly; the index lets
// Recall filter without opening every session file.
type SessionFiles struct {
	root         string
	mu           sync.Mutex
	index        []SessionRecord
	draft        []Entry // in-memory entries gathered this session, flushed at Close
	sessionStart time.Time
}

// SessionRecord is the metadata stored in index.json — one entry per
// session file. Path is relative to the store's root directory.
type SessionRecord struct {
	Path    string    `json:"path"`
	Date    time.Time `json:"date"`
	Summary string    `json:"summary"`
	Tags    []string  `json:"tags"`
}

// NewSessionFiles opens (or creates) the memory directory at root and
// loads the index. Missing sessions referenced in the index get pruned
// silently — drift between disk and index is fixed in place instead of
// surfacing as errors at recall time.
func NewSessionFiles(root string) (*SessionFiles, error) {
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0o755); err != nil {
		return nil, fmt.Errorf("memory: create %s: %w", root, err)
	}
	s := &SessionFiles{root: root, sessionStart: time.Now()}
	if err := s.loadIndex(); err != nil {
		return nil, err
	}
	s.pruneMissing()
	return s, nil
}

func (s *SessionFiles) loadIndex() error {
	data, err := os.ReadFile(filepath.Join(s.root, "index.json"))
	if os.IsNotExist(err) {
		s.index = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("memory: read index.json: %w", err)
	}
	var doc struct {
		Sessions []SessionRecord `json:"sessions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("memory: parse index.json: %w", err)
	}
	s.index = doc.Sessions
	return nil
}

func (s *SessionFiles) pruneMissing() {
	kept := s.index[:0]
	for _, r := range s.index {
		if _, err := os.Stat(filepath.Join(s.root, r.Path)); err == nil {
			kept = append(kept, r)
		}
	}
	s.index = kept
}

func (s *SessionFiles) flushIndex() error {
	out := struct {
		Sessions []SessionRecord `json:"sessions"`
	}{Sessions: s.index}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.root, "index.json"), data, 0o644)
}

// Save appends an entry to the in-progress session draft. Nothing reaches
// disk until Close — drafts are cheap, and per-Save file writes during a
// busy session would be needless churn.
func (s *SessionFiles) Save(_ context.Context, e Entry) error {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	s.mu.Lock()
	s.draft = append(s.draft, e)
	s.mu.Unlock()
	return nil
}

// Recall walks the index and returns up to `limit` session records whose
// summary or tags match the query. Match is a case-insensitive substring
// scan, ordered most-recent first — enough for sub-thousand-session
// histories. Swap in a Bleve index or vector store when you outgrow it.
func (s *SessionFiles) Recall(_ context.Context, query string, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 5
	}
	q := strings.ToLower(strings.TrimSpace(query))
	s.mu.Lock()
	candidates := append([]SessionRecord(nil), s.index...)
	s.mu.Unlock()
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Date.After(candidates[j].Date)
	})
	var out []Entry
	for _, r := range candidates {
		if q != "" && !matches(r, q) {
			continue
		}
		out = append(out, recordToEntry(r))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func matches(r SessionRecord, q string) bool {
	if strings.Contains(strings.ToLower(r.Summary), q) {
		return true
	}
	for _, t := range r.Tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

func recordToEntry(r SessionRecord) Entry {
	return Entry{
		Time:    r.Date,
		Kind:    KindSessionSummary,
		Content: fmt.Sprintf("[%s] %s", r.Path, r.Summary),
		Tags:    r.Tags,
	}
}

// Preamble returns the last preambleSessionCount session summaries as a
// compact block ready to concatenate to the system prompt. Bounded size
// so a year of daily sessions doesn't bloat the per-turn token cost.
func (s *SessionFiles) Preamble(_ context.Context) (string, error) {
	s.mu.Lock()
	records := append([]SessionRecord(nil), s.index...)
	s.mu.Unlock()
	if len(records) == 0 {
		return "", nil
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Date.After(records[j].Date)
	})
	if len(records) > preambleSessionCount {
		records = records[:preambleSessionCount]
	}
	var b strings.Builder
	b.WriteString("\n\n# Recent sessions (most recent first)\n\n")
	for _, r := range records {
		b.WriteString("- ")
		b.WriteString(r.Date.Format("2006-01-02 15:04"))
		if len(r.Tags) > 0 {
			b.WriteString(" (")
			b.WriteString(strings.Join(r.Tags, ", "))
			b.WriteString(")")
		}
		b.WriteString(": ")
		b.WriteString(r.Summary)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// Close flushes the current session draft to a markdown file and adds a
// record to index.json. An empty draft (no Save calls) is a no-op — empty
// sessions shouldn't pollute the index. Safe to call multiple times; only
// the first call has any effect.
func (s *SessionFiles) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.draft) == 0 {
		return nil
	}
	summary, tags, body := s.assembleSession()
	filename := s.sessionStart.Format("2006-01-02-15h04") + ".md"
	rel := filepath.Join("sessions", filename)
	fullPath := filepath.Join(s.root, rel)
	if err := os.WriteFile(fullPath, []byte(body), 0o644); err != nil {
		return fmt.Errorf("memory: write session: %w", err)
	}
	s.index = append(s.index, SessionRecord{
		Path:    rel,
		Date:    s.sessionStart,
		Summary: summary,
		Tags:    tags,
	})
	if err := s.flushIndex(); err != nil {
		return err
	}
	s.draft = nil
	return nil
}

// assembleSession turns the in-memory draft into (summary, tags, body).
// The session-summary entry (if any) becomes the file header + the index
// record; everything else is grouped by Kind into sections of the body.
func (s *SessionFiles) assembleSession() (summary string, tags []string, body string) {
	summary = "(no summary)"
	var rest []Entry
	for _, e := range s.draft {
		if e.Kind == KindSessionSummary {
			summary = e.Content
			tags = e.Tags
			continue
		}
		rest = append(rest, e)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", s.sessionStart.Format("2006-01-02 15:04"))
	if len(tags) > 0 {
		fmt.Fprintf(&b, "tags: %s\n", strings.Join(tags, ", "))
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, summary)

	if len(rest) == 0 {
		return summary, tags, b.String()
	}

	grouped := groupByKind(rest)
	for _, kind := range []string{KindFact, KindDecision, KindPreference} {
		entries := grouped[kind]
		if len(entries) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n", titleCase(kind+"s"))
		for _, e := range entries {
			fmt.Fprintf(&b, "- %s\n", e.Content)
		}
		delete(grouped, kind)
	}
	// Anything with a custom or empty Kind falls under Notes so it's still
	// preserved in the file even if it doesn't match the canonical kinds.
	var extras []Entry
	for _, es := range grouped {
		extras = append(extras, es...)
	}
	if len(extras) > 0 {
		fmt.Fprintln(&b, "\n## Notes")
		fmt.Fprintln(&b)
		for _, e := range extras {
			if e.Kind != "" {
				fmt.Fprintf(&b, "- (%s) %s\n", e.Kind, e.Content)
			} else {
				fmt.Fprintf(&b, "- %s\n", e.Content)
			}
		}
	}
	return summary, tags, b.String()
}

func groupByKind(entries []Entry) map[string][]Entry {
	out := make(map[string][]Entry)
	for _, e := range entries {
		out[e.Kind] = append(out[e.Kind], e)
	}
	return out
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
