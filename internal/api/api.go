// Package api is the JSON API used by the web UI, mounted under /api. Every
// write here is made by the owner; agents use MCP.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/svesh87/agent-tasks/internal/anchor"
	"github.com/svesh87/agent-tasks/internal/links"
	"github.com/svesh87/agent-tasks/internal/store"
	"github.com/svesh87/agent-tasks/internal/textproc"
)

// API serves /api/*.
type API struct {
	Store         *store.Store
	Links         links.Links
	WorkspaceRoot string
	Version       string
	UserHeader    string // header naming the signed-in user, e.g. Cf-Access-Authenticated-User-Email
}

// Handler returns the router. Paths are relative to the server root.
func (a *API) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/config", a.config)
	m.HandleFunc("GET /api/tags", a.tags)
	m.HandleFunc("GET /api/tasks", a.searchTasks)
	m.HandleFunc("POST /api/tasks", a.createTask)
	m.HandleFunc("GET /api/tasks/{id}", a.getTask)
	m.HandleFunc("PATCH /api/tasks/{id}", a.updateTask)
	m.HandleFunc("POST /api/tasks/bulk", a.bulkTasks)
	m.HandleFunc("POST /api/todos/bulk", a.bulkTodos)
	m.HandleFunc("GET /api/tasks/{id}/journal", a.getJournal)
	m.HandleFunc("POST /api/tasks/{id}/journal", a.appendJournal)
	m.HandleFunc("GET /api/tasks/{id}/docs/{slug}", a.getDoc)
	m.HandleFunc("PUT /api/tasks/{id}/docs/{slug}", a.writeDoc)
	m.HandleFunc("POST /api/tasks/{id}/docs/{slug}/revert", a.revertDoc)
	m.HandleFunc("GET /api/tasks/{id}/docs/{slug}/revisions", a.revisions)
	m.HandleFunc("GET /api/tasks/{id}/docs/{slug}/diff", a.diff)
	m.HandleFunc("POST /api/tasks/{id}/docs/{slug}/items/{item}", a.setItem)
	m.HandleFunc("GET /api/tasks/{id}/docs/{slug}/rounds", a.rounds)
	m.HandleFunc("POST /api/tasks/{id}/docs/{slug}/rounds/draft", a.draftRound)
	m.HandleFunc("GET /api/rounds/pending", a.pendingRounds)
	m.HandleFunc("GET /api/rounds/{round}", a.getRound)
	m.HandleFunc("PATCH /api/rounds/{round}", a.patchRound)
	m.HandleFunc("DELETE /api/rounds/{round}", a.deleteRound)
	m.HandleFunc("POST /api/rounds/{round}/submit", a.submitRound)
	m.HandleFunc("POST /api/rounds/{round}/comments", a.addComment)
	m.HandleFunc("PATCH /api/comments/{comment}", a.patchComment)
	m.HandleFunc("DELETE /api/comments/{comment}", a.deleteComment)
	m.HandleFunc("POST /api/comments/{comment}/replies", a.reply)
	m.HandleFunc("GET /api/places", a.places)
	m.HandleFunc("GET /api/place", a.getPlace)
	m.HandleFunc("GET /api/todos", a.listTodos)
	m.HandleFunc("POST /api/todos", a.addTodo)
	m.HandleFunc("GET /api/todos/{todo}", a.getTodo)
	m.HandleFunc("PATCH /api/todos/{todo}", a.patchTodo)
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "no such API endpoint: "+r.Method+" "+r.URL.Path)
	})
	return m
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrInvalid):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "internal error: "+err.Error())
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
		return false
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return false
	}
	return true
}

func pathInt(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || n <= 0 {
		writeErr(w, http.StatusBadRequest, "bad "+name+" id")
		return 0, false
	}
	return n, true
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

// multi reads a repeated or comma-separated query parameter.
func multi(r *http.Request, name string) []string {
	var out []string
	for _, v := range r.URL.Query()[name] {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

type taskOut struct {
	store.Task
	URL       string `json:"url"`
	Workspace string `json:"workspace"`
}

func (a *API) task(t store.Task) taskOut {
	return taskOut{Task: t, URL: a.Links.Task(t.ID), Workspace: strings.TrimRight(a.WorkspaceRoot, "/") + "/" + t.ID}
}

// ---- handlers ----

func (a *API) config(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"base_url":       a.Links.Base,
		"workspace_root": a.WorkspaceRoot,
		"version":        a.Version,
		"statuses":       store.Statuses,
		"doc_kinds":      store.DocKinds,
		"journal_kinds":  store.JournalKinds,
		"close_reasons":  store.CloseReasons,
		"todo_states":    store.TodoStates,
	}
	machines, err := a.Store.Machines(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	out["machines"] = machines
	if a.UserHeader != "" {
		out["user"] = r.Header.Get(a.UserHeader)
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/tags?q=&limit= — tags of tasks and todos for the tag picker.
func (a *API) tags(w http.ResponseWriter, r *http.Request) {
	tags, err := a.Store.Tags(r.Context(), r.URL.Query().Get("q"), queryInt(r, "limit"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

// GET /api/tasks?q=&status=active,waiting&tag=a&tag=b&place=<key>|none&sort=updated&limit=&offset=
func (a *API) searchTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := a.Store.Search(r.Context(), store.SearchParams{
		Query: q.Get("q"), Statuses: multi(r, "status"), Tags: multi(r, "tag"), Place: q.Get("place"), Sort: q.Get("sort"),
		Limit: queryInt(r, "limit"), Offset: queryInt(r, "offset"), Facets: true, RowStats: true, Todos: true,
	})
	if err != nil {
		fail(w, err)
		return
	}
	type item struct {
		taskOut
		*store.TaskRow
		Hits []hitOut `json:"hits"`
	}
	type todoHit struct {
		todoOut
		Kind    string           `json:"kind"`
		Snippet textproc.Snippet `json:"snippet"`
	}
	out := struct {
		Total        int              `json:"total"`
		Tasks        []item           `json:"tasks"`
		Todos        []todoHit        `json:"todos"`
		StatusCounts map[string]int   `json:"status_counts"`
		TagCounts    []store.TagCount `json:"tag_counts"`
	}{Total: res.Total, Tasks: []item{}, Todos: []todoHit{}, StatusCounts: res.StatusCounts, TagCounts: res.TagCounts}
	for _, th := range res.Tasks {
		out.Tasks = append(out.Tasks, item{a.task(th.Task), th.TaskRow, hitsOut(a.Links, th.ID, th.Hits)})
	}
	for _, td := range res.Todos {
		out.Todos = append(out.Todos, todoHit{a.todos([]store.Todo{td.Todo})[0], "todo", td.Snippet})
	}
	writeJSON(w, http.StatusOK, out)
}

// hitOut is a search hit with the deep link of the matched object.
type hitOut struct {
	store.Hit
	URL string `json:"url"`
}

func hitsOut(l links.Links, task string, hs []store.Hit) []hitOut {
	out := make([]hitOut, len(hs))
	for i, h := range hs {
		out[i] = hitOut{h, l.Hit(task, h.Kind, h.Slug, h.Revision, h.Start, h.End, h.Comment, h.Round, h.Journal)}
	}
	return out
}

// POST /api/tasks/bulk {ids: [T-1, ...], status, closed_reason?, waiting_on?}
// — one transaction: all tasks change or none.
func (a *API) bulkTasks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs          []string `json:"ids"`
		Status       *string  `json:"status"`
		ClosedReason *string  `json:"closed_reason"`
		WaitingOn    *string  `json:"waiting_on"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Status == nil {
		writeErr(w, http.StatusBadRequest, "status is required")
		return
	}
	ts, err := a.Store.UpdateTasks(r.Context(), in.IDs, store.TaskPatch{Status: in.Status, ClosedReason: in.ClosedReason, WaitingOn: in.WaitingOn})
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]taskOut, len(ts))
	for i, t := range ts {
		out[i] = a.task(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

func (a *API) createTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title     string           `json:"title"`
		Summary   string           `json:"summary"`
		Tags      []string         `json:"tags"`
		Status    string           `json:"status"`
		WaitingOn string           `json:"waiting_on"`
		Places    []store.PlaceRef `json:"places"`
		FromTodos []string         `json:"from_todos"`
	}
	if !decode(w, r, &in) {
		return
	}
	t, err := a.Store.CreateTask(r.Context(), store.NewTask{Title: in.Title, Summary: in.Summary, Tags: in.Tags, Status: in.Status, WaitingOn: in.WaitingOn,
		Places: in.Places, FromTodos: in.FromTodos, Source: "owner"})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a.task(t))
}

// GET /api/tasks/{id}?machine= — places carry locations on all machines, or
// on one machine when given.
func (a *API) getTask(w http.ResponseWriter, r *http.Request) {
	o, err := a.Store.TaskOverview(r.Context(), r.PathValue("id"), 50, true, r.URL.Query().Get("machine"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		taskOut
		Documents     []store.DocSummary   `json:"documents"`
		Progress      store.Progress       `json:"plan_progress"`
		Journal       []store.JournalEntry `json:"journal"`
		PendingRounds []store.RoundBrief   `json:"pending_rounds"`
		DraftRounds   []store.RoundBrief   `json:"draft_rounds"`
		Todos         []todoOut            `json:"todos"`
	}{a.task(o.Task), o.Documents, o.Progress, o.Journal, o.PendingRounds, o.DraftRounds, a.todos(o.Todos)})
}

func (a *API) updateTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title        *string          `json:"title"`
		Summary      *string          `json:"summary"`
		Status       *string          `json:"status"`
		WaitingOn    *string          `json:"waiting_on"`
		ClosedReason *string          `json:"closed_reason"`
		TagsAdd      []string         `json:"tags_add"`
		TagsRemove   []string         `json:"tags_remove"`
		PlacesAdd    []store.PlaceRef `json:"places_add"`
		PlacesRemove []string         `json:"places_remove"`
	}
	if !decode(w, r, &in) {
		return
	}
	t, err := a.Store.UpdateTask(r.Context(), r.PathValue("id"), store.TaskPatch{Title: in.Title, Summary: in.Summary, Status: in.Status,
		WaitingOn: in.WaitingOn, ClosedReason: in.ClosedReason, TagsAdd: in.TagsAdd, TagsRemove: in.TagsRemove,
		PlacesAdd: in.PlacesAdd, PlacesRemove: in.PlacesRemove})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.task(t))
}

func (a *API) getJournal(w http.ResponseWriter, r *http.Request) {
	es, err := a.Store.Journal(r.Context(), r.PathValue("id"), r.URL.Query().Get("kind"), queryInt(r, "limit"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": es})
}

func (a *API) appendJournal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind   string `json:"kind"`
		Body   string `json:"body"`
		Source string `json:"source"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Source == "" {
		in.Source = "owner"
	}
	e, err := a.Store.AppendJournal(r.Context(), r.PathValue("id"), in.Kind, in.Body, in.Source)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

// GET /api/tasks/{id}/docs/{slug}?rev=n — the owner view: revision body,
// plan items, every comment of every round (drafts included), revisions and
// rounds. The UI shows a comment on revision R when comment.revision == R or
// comment.current.revision == R.
func (a *API) getDoc(w http.ResponseWriter, r *http.Request) {
	v, err := a.Store.GetDoc(r.Context(), r.PathValue("id"), r.PathValue("slug"), store.DocGetOptions{Revision: queryInt(r, "rev"), OwnerView: true})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		store.DocView
		URL string `json:"url"`
	}{v, a.Links.Revision(v.Document.Task, v.Document.Slug, v.Revision.N)})
}

// POST .../revert {to, base_revision?}
func (a *API) revertDoc(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To           int `json:"to"`
		BaseRevision int `json:"base_revision"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := a.Store.RevertDoc(r.Context(), r.PathValue("id"), r.PathValue("slug"), in.To, in.BaseRevision)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *API) writeDoc(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body    string `json:"body"`
		Kind    string `json:"kind"`
		Title   string `json:"title"`
		Message string `json:"message"`
		// revision the owner's edit started from; 409 if the document moved on
		BaseRevision int `json:"base_revision"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := a.Store.WriteDoc(r.Context(), store.WriteDocParams{Task: r.PathValue("id"), Slug: r.PathValue("slug"), Body: in.Body,
		Kind: in.Kind, Title: in.Title, Message: in.Message, Author: store.AuthorOwner, BaseRevision: in.BaseRevision})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *API) revisions(w http.ResponseWriter, r *http.Request) {
	rs, err := a.Store.Revisions(r.Context(), r.PathValue("id"), r.PathValue("slug"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": rs})
}

// GET .../diff?from=a&to=b
func (a *API) diff(w http.ResponseWriter, r *http.Request) {
	d, err := a.Store.Diff(r.Context(), r.PathValue("id"), r.PathValue("slug"), queryInt(r, "from"), queryInt(r, "to"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *API) setItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Done *bool  `json:"done"`
		Note string `json:"note"`
	}
	if !decode(w, r, &in) {
		return
	}
	it, err := a.Store.SetPlanItem(r.Context(), r.PathValue("id"), r.PathValue("slug"), r.PathValue("item"), in.Done, in.Note, store.AuthorOwner)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (a *API) rounds(w http.ResponseWriter, r *http.Request) {
	rs, err := a.Store.Rounds(r.Context(), r.PathValue("id"), r.PathValue("slug"), true)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rounds": rs})
}

// POST .../rounds/draft {"revision": n} — get or create the document's draft round.
func (a *API) draftRound(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision int `json:"revision"`
	}
	if !decode(w, r, &in) {
		return
	}
	rd, err := a.Store.DraftRound(r.Context(), r.PathValue("id"), r.PathValue("slug"), in.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rd)
}

func (a *API) pendingRounds(w http.ResponseWriter, r *http.Request) {
	rs, err := a.Store.PendingRounds(r.Context(), r.URL.Query().Get("task"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rounds": rs})
}

func (a *API) getRound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(w, r, "round")
	if !ok {
		return
	}
	rd, err := a.Store.GetRound(r.Context(), id, true)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rd)
}

func (a *API) patchRound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(w, r, "round")
	if !ok {
		return
	}
	var in struct {
		Summary string `json:"summary"`
	}
	if !decode(w, r, &in) {
		return
	}
	rd, err := a.Store.SetRoundSummary(r.Context(), id, in.Summary)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rd)
}

func (a *API) deleteRound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(w, r, "round")
	if !ok {
		return
	}
	if err := a.Store.DeleteDraftRound(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) submitRound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(w, r, "round")
	if !ok {
		return
	}
	rd, err := a.Store.SubmitRound(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rd)
}

// POST /api/rounds/{round}/comments {"body": "...", "anchor": {"quote", "start", "end"}}
// The anchor is optional; prefix and suffix are computed by the server.
func (a *API) addComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(w, r, "round")
	if !ok {
		return
	}
	var in struct {
		Body   string         `json:"body"`
		Anchor *anchor.Anchor `json:"anchor"`
	}
	if !decode(w, r, &in) {
		return
	}
	c, err := a.Store.AddComment(r.Context(), id, store.NewComment{Anchor: in.Anchor, Body: in.Body})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// PATCH /api/comments/{id} {"body"} edits a draft comment; {"state": "resolved"|"open"}
// resolves or reopens a submitted one.
func (a *API) patchComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(w, r, "comment")
	if !ok {
		return
	}
	var in struct {
		Body  *string `json:"body"`
		State string  `json:"state"`
	}
	if !decode(w, r, &in) {
		return
	}
	var c store.Comment
	var err error
	switch {
	case in.Body != nil && in.State == "":
		c, err = a.Store.EditComment(r.Context(), id, *in.Body)
	case in.State == "resolved" || in.State == "open":
		c, err = a.Store.ReplyComment(r.Context(), store.ReplyParams{Comment: id, Author: store.AuthorOwner,
			Resolve: in.State == "resolved", Reopen: in.State == "open"})
	default:
		writeErr(w, http.StatusBadRequest, `send {"body": ...} for a draft comment or {"state": "resolved"|"open"}`)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *API) deleteComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(w, r, "comment")
	if !ok {
		return
	}
	if err := a.Store.DeleteComment(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/comments/{id}/replies {"body", "resolve"?, "reopen"?}
func (a *API) reply(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(w, r, "comment")
	if !ok {
		return
	}
	var in struct {
		Body    string `json:"body"`
		Resolve bool   `json:"resolve"`
		Reopen  bool   `json:"reopen"`
	}
	if !decode(w, r, &in) {
		return
	}
	c, err := a.Store.ReplyComment(r.Context(), store.ReplyParams{Comment: id, Author: store.AuthorOwner, Body: in.Body, Resolve: in.Resolve, Reopen: in.Reopen})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}
