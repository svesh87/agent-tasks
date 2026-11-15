// Package links builds the stable web URLs that agents put in chat.
package links

import (
	"fmt"
	"net/url"
	"strings"
)

// Todos is /todos, or /todos?place=<key> for one place ("none" = without places).
func (l Links) Todos(place string) string {
	if place == "" {
		return l.Base + "/todos"
	}
	return l.Base + "/todos?place=" + url.QueryEscape(place)
}

// Todo is /todos/<id>.
func (l Links) Todo(id string) string { return l.Base + "/todos/" + id }

// Links holds the public base URL of the web UI.
type Links struct{ Base string }

// New trims a trailing slash from base.
func New(base string) Links { return Links{Base: strings.TrimRight(base, "/")} }

// Task is /t/<id>.
func (l Links) Task(id string) string { return l.Base + "/t/" + id }

// Doc is /t/<id>/d/<slug> (latest revision, review mode).
func (l Links) Doc(id, slug string) string { return l.Task(id) + "/d/" + slug }

// Revision is /t/<id>/d/<slug>/r/<n>.
func (l Links) Revision(id, slug string, n int) string {
	return fmt.Sprintf("%s/r/%d", l.Doc(id, slug), n)
}

// Comment is /t/<id>/d/<slug>/r/<n>?c=<comment id>: the reviewed revision,
// scrolled to the comment.
func (l Links) Comment(id, slug string, n int, comment int64) string {
	return fmt.Sprintf("%s?c=%d", l.Revision(id, slug, n), comment)
}

// Hit is the deep link of a search hit:
//   - task:     /t/<id>
//   - document: /t/<id>/d/<slug>/r/<n>?at=<start>-<end>  (code points in the body; no ?at when the match is in the title)
//   - comment:  /t/<id>/d/<slug>/r/<n>?c=<comment id>
//   - round:    /t/<id>/d/<slug>/r/<n>?round=<round id>
//   - journal:  /t/<id>?j=<entry id>
func (l Links) Hit(task, kind, slug string, rev, start, end int, comment, round, journal int64) string {
	switch kind {
	case "document":
		if slug == "" || rev == 0 {
			return l.Task(task)
		}
		u := l.Revision(task, slug, rev)
		if end > start {
			u += fmt.Sprintf("?at=%d-%d", start, end)
		}
		return u
	case "comment":
		if slug != "" && rev > 0 {
			return l.Comment(task, slug, rev, comment)
		}
	case "round":
		if slug != "" && rev > 0 {
			return fmt.Sprintf("%s?round=%d", l.Revision(task, slug, rev), round)
		}
	case "journal":
		return fmt.Sprintf("%s?j=%d", l.Task(task), journal)
	}
	return l.Task(task)
}

// Diff is /t/<id>/d/<slug>/diff/<a>..<b>.
func (l Links) Diff(id, slug string, a, b int) string {
	return fmt.Sprintf("%s/diff/%d..%d", l.Doc(id, slug), a, b)
}
