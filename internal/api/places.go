package api

import (
	"net/http"

	"github.com/svesh87/agent-tasks/internal/store"
)

type todoOut struct {
	store.Todo
	URL string `json:"url"`
}

func (a *API) todos(ts []store.Todo) []todoOut {
	out := make([]todoOut, len(ts))
	for i, t := range ts {
		out[i] = todoOut{t, a.Links.Todo(t.ID)}
	}
	return out
}

// GET /api/places?q= — places with locations, task and open todo counts;
// q filters by label, key or location path (label matches first).
func (a *API) places(w http.ResponseWriter, r *http.Request) {
	ps, err := a.Store.Places(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"places": ps})
}

// GET /api/place?key=<key> — one place with its locations.
func (a *API) getPlace(w http.ResponseWriter, r *http.Request) {
	p, err := a.Store.GetPlace(r.Context(), r.URL.Query().Get("key"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// GET /api/todos?place=<key>|none&state=open,done&tag=x&q=&limit=&offset=
func (a *API) listTodos(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := a.Store.ListTodos(r.Context(), store.TodoListParams{
		Place: q.Get("place"), States: multi(r, "state"), Query: q.Get("q"), Tags: multi(r, "tag"),
		Limit: queryInt(r, "limit"), Offset: queryInt(r, "offset"), Facets: true,
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": res.Total, "todos": a.todos(res.Todos),
		"state_counts": res.StateCounts, "tag_counts": res.TagCounts})
}

func (a *API) addTodo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text       string   `json:"text"`
		Note       string   `json:"note"`
		Places     []string `json:"places"`
		OriginTask string   `json:"origin_task"`
		Tags       []string `json:"tags"`
	}
	if !decode(w, r, &in) {
		return
	}
	t, err := a.Store.AddTodo(r.Context(), store.NewTodo{Text: in.Text, Note: in.Note, Places: in.Places, OriginTask: in.OriginTask, Tags: in.Tags, Source: "owner"})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a.todos([]store.Todo{t})[0])
}

func (a *API) getTodo(w http.ResponseWriter, r *http.Request) {
	t, err := a.Store.GetTodo(r.Context(), r.PathValue("todo"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.todos([]store.Todo{t})[0])
}

func (a *API) patchTodo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text         *string  `json:"text"`
		Note         *string  `json:"note"`
		State        *string  `json:"state"`
		PlacesAdd    []string `json:"places_add"`
		PlacesRemove []string `json:"places_remove"`
		TagsAdd      []string `json:"tags_add"`
		TagsRemove   []string `json:"tags_remove"`
	}
	if !decode(w, r, &in) {
		return
	}
	t, err := a.Store.UpdateTodo(r.Context(), r.PathValue("todo"), store.TodoPatch{Text: in.Text, Note: in.Note, State: in.State,
		PlacesAdd: in.PlacesAdd, PlacesRemove: in.PlacesRemove, TagsAdd: in.TagsAdd, TagsRemove: in.TagsRemove})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.todos([]store.Todo{t})[0])
}

// POST /api/todos/bulk {ids: [D-1, ...], state: open|done|dropped} — one
// transaction: all todos change or none.
func (a *API) bulkTodos(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs   []string `json:"ids"`
		State *string  `json:"state"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.State == nil {
		writeErr(w, http.StatusBadRequest, "state is required")
		return
	}
	ts, err := a.Store.UpdateTodos(r.Context(), in.IDs, store.TodoPatch{State: in.State})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"todos": a.todos(ts)})
}
