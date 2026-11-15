// Package exportmd writes a task as a folder of markdown files and imports a
// markdown file as a new document revision.
//
// Layout of an exported task:
//
//	<dir>/<T-n>/task.md      front matter (id, title, status, tags, ...) + summary
//	<dir>/<T-n>/<slug>.md    latest revision of each document, boxes set from item state
//	<dir>/<T-n>/journal.md   journal, oldest first
package exportmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/svesh87/agent-tasks/internal/plan"
	"github.com/svesh87/agent-tasks/internal/store"
)

func q(s string) string {
	b, _ := json.Marshal(s) // a JSON string is a valid YAML scalar
	return string(b)
}

func frontMatter(pairs [][2]string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, p := range pairs {
		if p[1] == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", p[0], p[1])
	}
	b.WriteString("---\n")
	return b.String()
}

// Export writes task id into dir/<id>/ and returns that folder.
func Export(ctx context.Context, st *store.Store, id, dir string) (string, error) {
	o, err := st.TaskOverview(ctx, id, 1_000_000, false, "")
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, o.ID)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	tags := make([]string, len(o.Tags))
	for i, t := range o.Tags {
		tags[i] = q(t)
	}
	places := make([]string, len(o.Places))
	for i, p := range o.Places {
		places[i] = q(p.Key + " (" + p.Role + ")")
	}
	task := frontMatter([][2]string{
		{"id", o.ID}, {"title", q(o.Title)}, {"status", o.Status}, {"waiting_on", qIf(o.WaitingOn)},
		{"closed_reason", o.ClosedReason},
		{"tags", "[" + strings.Join(tags, ", ") + "]"}, {"places", "[" + strings.Join(places, ", ") + "]"},
		{"created_at", o.CreatedAt}, {"updated_at", o.UpdatedAt}, {"archived_at", o.ArchivedAt},
	})
	task += "\n# " + o.Title + "\n"
	if strings.TrimSpace(o.Summary) != "" {
		task += "\n" + strings.TrimRight(o.Summary, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(out, "task.md"), []byte(task), 0o644); err != nil {
		return "", err
	}
	for _, d := range o.Documents {
		v, err := st.GetDoc(ctx, o.ID, d.Slug, store.DocGetOptions{})
		if err != nil {
			return "", err
		}
		done := make([]bool, len(v.Items))
		for i, it := range v.Items {
			done[i] = it.Done
		}
		body := plan.SetBoxes(v.Revision.Body, done)
		fm := frontMatter([][2]string{
			{"slug", d.Slug}, {"kind", d.Kind}, {"title", q(d.Title)},
			{"revision", strconv.Itoa(v.Revision.N)}, {"author", v.Revision.Author}, {"created_at", v.Revision.CreatedAt},
		})
		if err := os.WriteFile(filepath.Join(out, d.Slug+".md"), []byte(fm+"\n"+body), 0o644); err != nil {
			return "", err
		}
	}
	var j strings.Builder
	fmt.Fprintf(&j, "# Journal of %s\n", o.ID)
	for i := len(o.Journal) - 1; i >= 0; i-- {
		e := o.Journal[i]
		fmt.Fprintf(&j, "\n## %s · %s", e.CreatedAt, e.Kind)
		if e.Source != "" {
			fmt.Fprintf(&j, " · %s", e.Source)
		}
		fmt.Fprintf(&j, "\n\n%s\n", strings.TrimRight(e.Body, "\n"))
	}
	if err := os.WriteFile(filepath.Join(out, "journal.md"), []byte(j.String()), 0o644); err != nil {
		return "", err
	}
	return out, nil
}

func qIf(s string) string {
	if s == "" {
		return ""
	}
	return q(s)
}

// SplitFrontMatter separates a leading "---" block from the body and parses
// its simple "key: value" lines (values may be JSON-quoted).
func SplitFrontMatter(text string) (map[string]string, string) {
	meta := map[string]string{}
	if !strings.HasPrefix(text, "---\n") {
		return meta, text
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return meta, text
	}
	block := text[4 : 4+end]
	body := strings.TrimPrefix(text[4+end+5:], "\n")
	sc := bufio.NewScanner(strings.NewReader(block))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, `"`) {
			var s string
			if json.Unmarshal([]byte(v), &s) == nil {
				v = s
			}
		}
		meta[strings.TrimSpace(k)] = v
	}
	return meta, body
}

// ImportParams are the inputs of Import. Empty slug, kind and title are taken
// from the file's front matter (as written by Export) or the file name.
type ImportParams struct {
	Task    string
	File    string
	Slug    string
	Kind    string
	Title   string
	Message string
}

// Import adds the markdown file as a new revision (author import).
func Import(ctx context.Context, st *store.Store, p ImportParams) (store.WriteResult, error) {
	raw, err := os.ReadFile(p.File)
	if err != nil {
		return store.WriteResult{}, err
	}
	meta, body := SplitFrontMatter(string(raw))
	if p.Slug == "" {
		p.Slug = meta["slug"]
	}
	if p.Slug == "" {
		p.Slug = strings.ToLower(strings.TrimSuffix(filepath.Base(p.File), filepath.Ext(p.File)))
	}
	if p.Kind == "" {
		p.Kind = meta["kind"]
	}
	if p.Title == "" {
		p.Title = meta["title"]
	}
	if p.Message == "" {
		p.Message = "imported from " + filepath.Base(p.File)
	}
	return st.WriteDoc(ctx, store.WriteDocParams{Task: p.Task, Slug: p.Slug, Body: body, Kind: p.Kind, Title: p.Title, Message: p.Message, Author: store.AuthorImport})
}
