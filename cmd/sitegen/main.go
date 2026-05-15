// Command sitegen renders the follow_along/ markdown chapters into a
// static website that matches the Claude-Design landing-page mockup.
//
// Inputs (relative to the working directory, which is the repo root):
//
//	follow_along/en/*.md
//	follow_along/es/*.md
//
// Outputs (under -out, default ./site):
//
//	./site/index.html                  -- English landing page
//	./site/chapters/<slug>.html        -- English chapter pages
//	./site/es/index.html                -- Spanish landing page
//	./site/es/chapters/<slug>.html     -- Spanish chapter pages
//	./site/styles.css                  -- shared CSS
//
// Run it from the repo root with `go run ./cmd/sitegen`. CI does the same
// and uploads ./site as a GitHub Pages artifact.
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

// landingData is everything the index template needs.
type landingData struct {
	Lang        string
	OtherLang   string
	OtherURL    string // root of the other-language site, relative to this one
	Total       int
	Groups      []group
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

type config struct {
	src string // follow_along/
	out string // ./site
}

func main() {
	c := config{}
	flag.StringVar(&c.src, "src", "follow_along", "source directory containing en/ and es/ subdirs")
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
		return fmt.Errorf("load english: %w", err)
	}
	esChapters, err := loadChapters(filepath.Join(c.src, "es"), "es", md)
	if err != nil {
		return fmt.Errorf("load spanish: %w", err)
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

	if err := renderLanguage(tmpl, c.out, "", enChapters, "en", "es"); err != nil {
		return fmt.Errorf("render en: %w", err)
	}
	if err := renderLanguage(tmpl, c.out, "es", esChapters, "es", "en"); err != nil {
		return fmt.Errorf("render es: %w", err)
	}

	log.Printf("sitegen: wrote %d en + %d es chapters to %s", len(enChapters), len(esChapters), c.out)
	return nil
}

// renderLanguage writes one full set of pages (landing + chapters) for a
// given language under out/<subdir>. Pass subdir="" for the root language.
func renderLanguage(tmpl *template.Template, out, subdir string, chapters []chapter, lang, otherLang string) error {
	root := out
	if subdir != "" {
		root = filepath.Join(out, subdir)
	}
	if err := os.MkdirAll(filepath.Join(root, "chapters"), 0o755); err != nil {
		return err
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

func renderChapter(path, name, lang string, md goldmark.Markdown) (chapter, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return chapter{}, err
	}
	num, title, body := splitChapter(raw)
	rewritten := rewriteLinks(body)

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

// rewriteLinks rewrites two flavors of relative markdown link before
// goldmark sees them:
//
//   - `(NN-name.md)` becomes `(NN-name.html)` so links between chapters
//     point at the generated pages instead of the raw markdown.
//   - `(../internal/...)` and similar repo-relative paths become
//     absolute GitHub blob URLs so source-file pointers are clickable.
func rewriteLinks(body string) string {
	body = mdLinkRe.ReplaceAllStringFunc(body, func(match string) string {
		// match is `](something)` — pull out the something
		sub := mdLinkRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		target := sub[1]

		// Anchor or external link: leave alone.
		if strings.HasPrefix(target, "http") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
			return match
		}

		// Chapter-to-chapter link: rewrite .md → .html
		if strings.HasSuffix(target, ".md") && !strings.Contains(target, "/") {
			return "](" + strings.TrimSuffix(target, ".md") + ".html)"
		}

		// Repo-relative path (e.g. ../../internal/foo.go or ../README.md):
		// resolve to GitHub blob URL.
		if strings.HasPrefix(target, "../") {
			cleaned := strings.TrimLeft(target, "./")
			// Drop trailing #anchor for the URL build, but reapply.
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
				highlighting.WithStyle("monokai"),
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
