// Command sitegen renders the follow_along/ chapters, how-to/ recipes,
// and exercises/ extension prompts into a static website that matches the
// Claude-Design landing-page mockup.
//
// Inputs (relative to the working directory, which is the repo root):
//
//	follow_along/en/*.md   how-to/en/*.md   exercises/en/*.md
//	follow_along/es/*.md   how-to/es/*.md   exercises/es/*.md
//
// Outputs (under -out, default ./site):
//
//	./site/index.html                   -- English landing page
//	./site/chapters/<slug>.html         -- English chapter pages
//	./site/how-to/<slug>.html           -- English how-to guides
//	./site/exercises/<slug>.html        -- English exercise pages
//	./site/es/index.html                -- Spanish landing page
//	./site/es/chapters/<slug>.html      -- Spanish chapter pages
//	./site/es/how-to/<slug>.html        -- Spanish how-to guides
//	./site/es/exercises/<slug>.html     -- Spanish exercise pages
//	./site/styles.css                   -- shared CSS
//
// Missing source directories are tolerated — the section is simply skipped
// for that language. Run from the repo root with `go run ./cmd/sitegen`.
package main

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

//go:embed templates/*.html.tmpl
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// repoURL is used to rewrite relative markdown links that point at source
// files (e.g. `../../internal/agent/agent.go`) into clickable GitHub URLs.
const repoURL = "https://github.com/betta-tech/byo-coding-agent/blob/main"

// chapter is what the templates see for a single rendered chapter.
type chapter struct {
	Num      string        // "00", "01", … or "README"
	Slug     string        // file stem, e.g. "01-the-agent-loop"
	Title    string        // first H1 of the markdown
	Body     template.HTML // rendered HTML for the body (H1 stripped)
	Lang     string        // "en" or "es"
	URL      string        // path to the chapter page, relative to its language root
	OtherURL string        // path to the same chapter in the other language
}

// group is a section in the landing-page curriculum.
type group struct {
	Title    string
	Kicker   string
	Blurb    string
	Chapters []chapter
}

// guide is a how-to recipe: shorter than a chapter, no curriculum number,
// linked from its own section on the landing page.
type guide struct {
	Slug     string
	Title    string
	Blurb    template.HTML // first paragraph after the H1, "Goal:" prefix stripped, inline code rendered
	Body     template.HTML // rendered HTML body (H1 stripped)
	Lang     string
	URL      string // "how-to/<slug>.html", relative to language root
	OtherURL string // same guide in the other language, relative to this page
}

// exercise is an extension prompt: a self-contained task that builds on
// the harness. Has a numeric slug prefix (01-, 02-, …) and a difficulty
// tag pulled out of the markdown for badge rendering on the landing page.
type exercise struct {
	Slug       string
	Num        string        // "01", "02", …
	Title      string        // H1 with "Exercise N — " prefix stripped
	Blurb      template.HTML // from the "**Goal:**" line, inline code rendered
	Difficulty string        // "easy" / "medium" / "hard", lowercase
	Body       template.HTML
	Lang       string
	URL        string // "exercises/<slug>.html", relative to language root
	OtherURL   string
}

// landingData is everything the index template needs.
type landingData struct {
	Lang        string
	OtherLang   string
	OtherURL    string // root of the other-language site, relative to this one
	Total       int
	Groups      []group
	Guides      []guide
	Exercises   []exercise
	GeneratedAt string
}

// chapterData is everything the chapter template needs.
type chapterData struct {
	Lang     string
	Title    string
	Body     template.HTML
	Num      string
	IndexURL string // path back to the index, relative
	OtherURL string // same chapter in the other language, relative
	PrevURL  string // previous chapter; "" if none
	PrevName string
	NextURL  string // next chapter; "" if none
	NextName string
	RepoURL  string
}

// guideData is everything the guide template needs. Guides don't get
// prev/next nav (there are only a handful) — just a back-to-index link.
type guideData struct {
	Lang     string
	Title    string
	Blurb    template.HTML
	Body     template.HTML
	IndexURL string // path back to the landing page, relative
	HowToURL string // path back to the how-to anchor on the landing page
	OtherURL string // same guide in the other language, relative
	RepoURL  string
}

// exerciseData is everything the exercise template needs.
type exerciseData struct {
	Lang        string
	Title       string
	Num         string
	Difficulty  string
	Blurb       template.HTML
	Body        template.HTML
	IndexURL    string
	ListURL     string // back to the exercises anchor on the landing page
	OtherURL    string
	RepoURL     string
}

type config struct {
	src       string // follow_along/
	howto     string // how-to/
	exercises string // exercises/
	out       string // ./site
}

func main() {
	c := config{}
	flag.StringVar(&c.src, "src", "follow_along", "source directory containing en/ and es/ subdirs")
	flag.StringVar(&c.howto, "howto", "how-to", "how-to recipes directory containing en/ and es/ subdirs")
	flag.StringVar(&c.exercises, "exercises", "exercises", "exercises directory containing en/ and es/ subdirs")
	flag.StringVar(&c.out, "out", "site", "output directory")
	flag.Parse()

	if err := run(c); err != nil {
		log.Fatalf("sitegen: %v", err)
	}
}

func run(c config) error {
	md := newMarkdown()

	enChapters, err := loadChapters(filepath.Join(c.src, "en"), "en", md)
	if err != nil {
		return fmt.Errorf("load english chapters: %w", err)
	}
	esChapters, err := loadChapters(filepath.Join(c.src, "es"), "es", md)
	if err != nil {
		return fmt.Errorf("load spanish chapters: %w", err)
	}
	enGuides, err := loadGuides(filepath.Join(c.howto, "en"), "en", md)
	if err != nil {
		return fmt.Errorf("load english how-tos: %w", err)
	}
	esGuides, err := loadGuides(filepath.Join(c.howto, "es"), "es", md)
	if err != nil {
		return fmt.Errorf("load spanish how-tos: %w", err)
	}
	enExercises, err := loadExercises(filepath.Join(c.exercises, "en"), "en", md)
	if err != nil {
		return fmt.Errorf("load english exercises: %w", err)
	}
	esExercises, err := loadExercises(filepath.Join(c.exercises, "es"), "es", md)
	if err != nil {
		return fmt.Errorf("load spanish exercises: %w", err)
	}

	if err := os.RemoveAll(c.out); err != nil {
		return err
	}
	if err := os.MkdirAll(c.out, 0o755); err != nil {
		return err
	}

	tmpl, err := template.ParseFS(templatesFS, "templates/*.html.tmpl")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	if err := copyStatic(c.out); err != nil {
		return fmt.Errorf("copy static: %w", err)
	}

	if err := renderLanguage(tmpl, c.out, "", enChapters, enGuides, enExercises, "en", "es", len(esExercises) > 0); err != nil {
		return fmt.Errorf("render en: %w", err)
	}
	if err := renderLanguage(tmpl, c.out, "es", esChapters, esGuides, esExercises, "es", "en", len(enExercises) > 0); err != nil {
		return fmt.Errorf("render es: %w", err)
	}

	log.Printf("sitegen: wrote %d en + %d es chapters, %d en + %d es how-tos, %d en + %d es exercises to %s",
		len(enChapters), len(esChapters), len(enGuides), len(esGuides), len(enExercises), len(esExercises), c.out)
	return nil
}

// renderLanguage writes one full set of pages (landing + chapters + how-tos
// + exercises) for a given language under out/<subdir>. Pass subdir="" for
// the root language. otherHasExercises indicates whether the *other* language
// has any exercise pages — used to decide where the lang-toggle on an
// exercise page should point when no parallel translation exists.
func renderLanguage(tmpl *template.Template, out, subdir string, chapters []chapter, guides []guide, exercises []exercise, lang, otherLang string, otherHasExercises bool) error {
	root := out
	if subdir != "" {
		root = filepath.Join(out, subdir)
	}
	if err := os.MkdirAll(filepath.Join(root, "chapters"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "how-to"), 0o755); err != nil {
		return err
	}
	if len(exercises) > 0 {
		if err := os.MkdirAll(filepath.Join(root, "exercises"), 0o755); err != nil {
			return err
		}
	}

	// landing page
	groups := buildGroups(chapters)
	otherURL := "./" // root → root is itself if same language; renderLanguage caller passes the right value
	switch {
	case lang == "en":
		otherURL = "es/"
	case lang == "es":
		otherURL = "../"
	}
	landing := landingData{
		Lang:      lang,
		OtherLang: otherLang,
		OtherURL:  otherURL,
		Total:     len(chapters),
		Groups:    groups,
		Guides:    guides,
		Exercises: exercises,
	}
	if err := writeTemplate(tmpl, "index.html.tmpl", filepath.Join(root, "index.html"), landing); err != nil {
		return err
	}

	// chapter pages
	for i, ch := range chapters {
		var prev, next chapter
		var prevName, nextName, prevURL, nextURL string
		if i > 0 {
			prev = chapters[i-1]
			prevName = prev.Title
			prevURL = prev.Slug + ".html"
		}
		if i+1 < len(chapters) {
			next = chapters[i+1]
			nextName = next.Title
			nextURL = next.Slug + ".html"
		}
		other := ""
		if lang == "en" {
			other = "../es/chapters/" + ch.Slug + ".html"
		} else {
			other = "../../chapters/" + ch.Slug + ".html"
		}
		cd := chapterData{
			Lang:     lang,
			Title:    ch.Title,
			Body:     ch.Body,
			Num:      ch.Num,
			IndexURL: "../index.html",
			OtherURL: other,
			PrevURL:  prevURL,
			PrevName: prevName,
			NextURL:  nextURL,
			NextName: nextName,
			RepoURL:  repoURL,
		}
		out := filepath.Join(root, "chapters", ch.Slug+".html")
		if err := writeTemplate(tmpl, "chapter.html.tmpl", out, cd); err != nil {
			return err
		}
	}

	// how-to pages
	for _, g := range guides {
		other := ""
		if lang == "en" {
			other = "../es/how-to/" + g.Slug + ".html"
		} else {
			other = "../../how-to/" + g.Slug + ".html"
		}
		gd := guideData{
			Lang:     lang,
			Title:    g.Title,
			Blurb:    g.Blurb,
			Body:     g.Body,
			IndexURL: "../index.html",
			HowToURL: "../index.html#how-to",
			OtherURL: other,
			RepoURL:  repoURL,
		}
		out := filepath.Join(root, "how-to", g.Slug+".html")
		if err := writeTemplate(tmpl, "guide.html.tmpl", out, gd); err != nil {
			return err
		}
	}

	// exercise pages. The lang toggle falls back to the other language's
	// landing page when no parallel translation exists.
	for _, ex := range exercises {
		other := ""
		switch {
		case lang == "en" && otherHasExercises:
			other = "../es/exercises/" + ex.Slug + ".html"
		case lang == "en":
			other = "../es/"
		case lang == "es" && otherHasExercises:
			other = "../../exercises/" + ex.Slug + ".html"
		default:
			other = "../../"
		}
		ed := exerciseData{
			Lang:       lang,
			Title:      ex.Title,
			Num:        ex.Num,
			Difficulty: ex.Difficulty,
			Blurb:      ex.Blurb,
			Body:       ex.Body,
			IndexURL:   "../index.html",
			ListURL:    "../index.html#exercises",
			OtherURL:   other,
			RepoURL:    repoURL,
		}
		out := filepath.Join(root, "exercises", ex.Slug+".html")
		if err := writeTemplate(tmpl, "exercise.html.tmpl", out, ed); err != nil {
			return err
		}
	}
	return nil
}

func writeTemplate(t *template.Template, name, out string, data any) error {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("execute %s: %w", name, err)
	}
	return os.WriteFile(out, buf.Bytes(), 0o644)
}

func copyStatic(out string) error {
	return fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel("static", path)
		data, err := staticFS.ReadFile(path)
		if err != nil {
			return err
		}
		dest := filepath.Join(out, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
}

// loadChapters reads every *.md in dir (skipping README.md, which is a
// table of contents the generator builds itself) and returns rendered
// chapters in filename-sorted order.
func loadChapters(dir, lang string, md goldmark.Markdown) ([]chapter, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var chapters []chapter
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if strings.EqualFold(name, "README.md") {
			continue
		}
		ch, err := renderChapter(filepath.Join(dir, name), name, lang, md)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", name, err)
		}
		chapters = append(chapters, ch)
	}
	sort.Slice(chapters, func(i, j int) bool {
		return chapters[i].Slug < chapters[j].Slug
	})
	return chapters, nil
}

// loadGuides reads every *.md in dir (skipping README.md, which is just an
// index in the markdown source) and returns rendered guides sorted by
// filename. Returns nil and no error if the directory is missing — the
// landing page handles an empty list gracefully.
func loadGuides(dir, lang string, md goldmark.Markdown) ([]guide, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var guides []guide
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if strings.EqualFold(name, "README.md") {
			continue
		}
		g, err := renderGuide(filepath.Join(dir, name), name, lang, md)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", name, err)
		}
		guides = append(guides, g)
	}
	sort.Slice(guides, func(i, j int) bool {
		return guides[i].Slug < guides[j].Slug
	})
	return guides, nil
}

func renderGuide(path, name, lang string, md goldmark.Markdown) (guide, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return guide{}, err
	}
	_, title, body := splitChapter(raw)
	blurb := firstParagraph(body)
	// Guides live under how-to/, so a reference to a sibling chapter
	// markdown needs to climb one level and dive into chapters/.
	rewritten := rewriteLinksFrom(body, "../chapters/")

	var buf bytes.Buffer
	if err := md.Convert([]byte(rewritten), &buf); err != nil {
		return guide{}, err
	}

	slug := strings.TrimSuffix(name, ".md")
	return guide{
		Slug:     slug,
		Title:    title,
		Blurb:    template.HTML(renderInline(blurb)),
		Body:     template.HTML(buf.String()),
		Lang:     lang,
		URL:      "how-to/" + slug + ".html",
		OtherURL: slug + ".html",
	}, nil
}

// renderInline applies a minimal inline-markdown pass: HTML-escapes the
// text, then turns `...` backtick runs into <code>...</code>. Good enough
// for one-line blurbs; chapter bodies still go through full goldmark.
func renderInline(s string) string {
	escaped := template.HTMLEscapeString(s)
	return inlineCodeRe.ReplaceAllString(escaped, "<code>$1</code>")
}

var inlineCodeRe = regexp.MustCompile("`([^`]+)`")

// loadExercises reads every *.md in dir (skipping README.md) and returns
// rendered exercises sorted by slug. Returns nil with no error if the
// directory is missing.
func loadExercises(dir, lang string, md goldmark.Markdown) ([]exercise, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var exs []exercise
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if strings.EqualFold(name, "README.md") {
			continue
		}
		ex, err := renderExerciseFile(filepath.Join(dir, name), name, lang, md)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", name, err)
		}
		exs = append(exs, ex)
	}
	sort.Slice(exs, func(i, j int) bool {
		return exs[i].Slug < exs[j].Slug
	})
	return exs, nil
}

func renderExerciseFile(path, name, lang string, md goldmark.Markdown) (exercise, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return exercise{}, err
	}
	_, h1, body := splitChapter(raw)
	title := cleanExerciseTitle(h1)
	blurb := exerciseBlurb(body)
	difficulty := extractDifficulty(body)
	// Exercise pages sit under exercises/, parallel to how-to/, so a
	// reference to a follow_along chapter resolves the same way.
	rewritten := rewriteLinksFrom(body, "../chapters/")

	var buf bytes.Buffer
	if err := md.Convert([]byte(rewritten), &buf); err != nil {
		return exercise{}, err
	}

	slug := strings.TrimSuffix(name, ".md")
	num := ""
	if m := exerciseNumRe.FindStringSubmatch(slug); len(m) == 2 {
		num = m[1]
	}
	return exercise{
		Slug:       slug,
		Num:        num,
		Title:      title,
		Blurb:      template.HTML(renderInline(blurb)),
		Difficulty: difficulty,
		Body:       template.HTML(buf.String()),
		Lang:       lang,
		URL:        "exercises/" + slug + ".html",
		OtherURL:   slug + ".html",
	}, nil
}

var (
	// Pulls the leading "01-" out of "01-tool-retry-on-error".
	exerciseNumRe = regexp.MustCompile(`^(\d+)-`)
	// Strips the leading "Exercise N — " (em dash or hyphen) from the H1.
	exerciseTitlePrefixRe = regexp.MustCompile(`^(?:Exercise|Ejercicio)\s+\d+\s*[—–-]\s*`)
	// "**Difficulty:** easy." / "**Dificultad:** media."
	difficultyRe = regexp.MustCompile(`(?i)\*\*(?:Difficulty|Dificultad):\*\*\s*([A-Za-zÁÉÍÓÚáéíóú]+)`)
	// "**Goal:** ..." up to the next double newline or "**" boundary.
	goalLineRe = regexp.MustCompile(`(?s)\*\*(?:Goal|Objetivo):\*\*\s*(.+?)(?:\n\n|$)`)
)

func cleanExerciseTitle(h1 string) string {
	return strings.TrimSpace(exerciseTitlePrefixRe.ReplaceAllString(h1, ""))
}

func extractDifficulty(body string) string {
	if m := difficultyRe.FindStringSubmatch(body); len(m) == 2 {
		return strings.ToLower(strings.TrimSpace(m[1]))
	}
	return ""
}

// exerciseBlurb looks for the "**Goal:** ..." line. Falls back to the first
// paragraph if no goal marker is present.
func exerciseBlurb(body string) string {
	if m := goalLineRe.FindStringSubmatch(body); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return firstParagraph(body)
}

// firstParagraph returns the first non-empty paragraph of body as plain
// text, dropping a leading "Goal:" / "Objetivo:" label if present. Used
// as the guide blurb on the landing page.
func firstParagraph(body string) string {
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			if b.Len() > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trim, "#") {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(trim)
	}
	out := b.String()
	for _, prefix := range []string{"Goal:", "Objetivo:"} {
		if strings.HasPrefix(out, prefix) {
			out = strings.TrimSpace(out[len(prefix):])
			break
		}
	}
	return out
}

func renderChapter(path, name, lang string, md goldmark.Markdown) (chapter, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return chapter{}, err
	}
	num, title, body := splitChapter(raw)
	// Chapter pages live under chapters/, so sibling chapter links resolve
	// directly to the same directory.
	rewritten := rewriteLinksFrom(body, "")

	var buf bytes.Buffer
	if err := md.Convert([]byte(rewritten), &buf); err != nil {
		return chapter{}, err
	}

	slug := strings.TrimSuffix(name, ".md")
	return chapter{
		Num:      num,
		Slug:     slug,
		Title:    title,
		Body:     template.HTML(buf.String()),
		Lang:     lang,
		URL:      "chapters/" + slug + ".html",
		OtherURL: slug + ".html",
	}, nil
}

var (
	h1Re = regexp.MustCompile(`(?m)^# (.+?)\s*$`)
	// Chapter heads look like `# 00 · Introduction` or `# 14 · MCP support`.
	chapterNumRe = regexp.MustCompile(`^([0-9]{2})\s*·`)
)

// splitChapter pulls the first H1 out of the raw markdown, returns the
// chapter number (best-effort), the human title, and the body without the
// H1 (because the chapter template renders the title in its own header).
func splitChapter(raw []byte) (num, title, body string) {
	m := h1Re.FindSubmatchIndex(raw)
	if m == nil {
		return "", "untitled", string(raw)
	}
	full := string(raw[m[2]:m[3]])
	title = strings.TrimSpace(full)
	if nm := chapterNumRe.FindStringSubmatch(title); len(nm) == 2 {
		num = nm[1]
		// "00 · Introduction" → "Introduction"
		title = strings.TrimSpace(title[len(nm[0]):])
	}
	body = strings.TrimSpace(string(raw[m[1]:]))
	return num, title, body
}

// rewriteLinksFrom rewrites the relative markdown links in body so they
// work on the generated site:
//
//   - `(NN-name.md)` becomes `(NN-name.html)` (sibling page in the same
//     directory).
//   - `(../../follow_along/<lang>/NN-name.md)` becomes a chapter URL,
//     prefixed with chaptersBase so it resolves from the source page's
//     own directory (e.g. how-to/X.html prefixes with "../chapters/").
//   - Any other `(../...)` repo-relative path becomes an absolute GitHub
//     blob URL so source-file pointers stay clickable.
func rewriteLinksFrom(body, chaptersBase string) string {
	body = mdLinkRe.ReplaceAllStringFunc(body, func(match string) string {
		sub := mdLinkRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		target := sub[1]

		if strings.HasPrefix(target, "http") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
			return match
		}

		// Cross-section link to a follow_along chapter, possibly with #anchor.
		// We strip the language directory and treat the rest as a sibling chapter file.
		if i := followAlongRe.FindStringSubmatchIndex(target); i != nil {
			file := target[i[2]:i[3]] // capture: <slug>.md or <slug>.md#anchor
			anchor := ""
			if j := strings.Index(file, "#"); j >= 0 {
				anchor = file[j:]
				file = file[:j]
			}
			slug := strings.TrimSuffix(file, ".md")
			return "](" + chaptersBase + slug + ".html" + anchor + ")"
		}

		// Sibling .md (e.g. another chapter or another how-to in the same folder).
		if strings.HasSuffix(target, ".md") && !strings.Contains(target, "/") {
			return "](" + strings.TrimSuffix(target, ".md") + ".html)"
		}

		// Repo-relative path (e.g. ../../internal/foo.go): GitHub blob URL.
		if strings.HasPrefix(target, "../") {
			cleaned := strings.TrimLeft(target, "./")
			anchor := ""
			if i := strings.Index(cleaned, "#"); i >= 0 {
				anchor = cleaned[i:]
				cleaned = cleaned[:i]
			}
			return "](" + repoURL + "/" + cleaned + anchor + ")"
		}

		return match
	})
	return body
}

// followAlongRe matches relative paths like
//   ../../follow_along/en/14-mcp-support.md
//   ../../follow_along/es/02-the-permission-gate.md#anchor
// Captures the trailing chapter file (with optional anchor).
var followAlongRe = regexp.MustCompile(`^\.\./\.\./follow_along/(?:en|es)/(.+)$`)

// mdLinkRe matches the URL part of a markdown link: `](target)`.
// Restricting to the closing-paren form keeps it simple — image syntax
// and reference-style links aren't used in this repo's chapters.
var mdLinkRe = regexp.MustCompile(`\]\(([^)]+)\)`)

// buildGroups partitions chapters into the four curriculum chapters of
// the design. Extra chapters past 12 fall into "Going further" so adding
// a new chapter to follow_along/en/ shows up automatically without
// touching this code.
func buildGroups(chapters []chapter) []group {
	groups := []*group{
		{Title: "Foundations", Kicker: "§00–04", Blurb: "The smallest thing that can call a model, take a turn, and run a tool."},
		{Title: "Conversation", Kicker: "§05–08", Blurb: "Messages, budgets, and the bookkeeping that keeps a long session coherent."},
		{Title: "Tools & structure", Kicker: "§09–12", Blurb: "A real tool surface, an opinionated layout, subagents, and a proper TUI."},
		{Title: "Going further", Kicker: "§13+", Blurb: "Optional chapters — MCP, caching, diff approval, memory, and where to take it next."},
	}
	for _, ch := range chapters {
		n, err := strconv.Atoi(ch.Num)
		if err != nil {
			groups[3].Chapters = append(groups[3].Chapters, ch)
			continue
		}
		switch {
		case n <= 4:
			groups[0].Chapters = append(groups[0].Chapters, ch)
		case n <= 8:
			groups[1].Chapters = append(groups[1].Chapters, ch)
		case n <= 12:
			groups[2].Chapters = append(groups[2].Chapters, ch)
		default:
			groups[3].Chapters = append(groups[3].Chapters, ch)
		}
	}
	out := make([]group, 0, len(groups))
	for _, g := range groups {
		if len(g.Chapters) > 0 {
			out = append(out, *g)
		}
	}
	return out
}

func newMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			highlighting.NewHighlighting(
				highlighting.WithStyle("gruvbox"),
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(false), // inline styles, no extra CSS needed
				),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithUnsafe(),
		),
	)
}
