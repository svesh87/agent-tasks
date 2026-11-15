package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/svesh87/agent-tasks/internal/store"
)

func (s *Server) registerPlaces(srv *mcp.Server) {
	st := s.Store

	add(srv, "place_here", "Where am I",
		"Find the place of your working directory and record where it lives on this machine. With a git remote the place is that repository, with the same key on every machine. Without one it is the known place whose folder contains cwd, or else a new directory place <machine>:<cwd>. Returns the place with its locations, its open tasks and its open todos. Call it when you start work in a directory.",
		false, nil,
		func(ctx context.Context, in placeHereIn) (any, error) {
			res, err := st.PlaceHere(ctx, in.Cwd, in.Remote, in.Machine)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"place":     res.Place,
				"created":   res.Created,
				"path":      res.Path,
				"tasks":     s.tasks(res.Tasks),
				"todos":     s.todos(res.Todos),
				"todos_url": s.Links.Todos(res.Place.Key),
			}, nil
		})

	add(srv, "place_get", "Get place",
		"Get a place with its known locations: the machines it lives on and the path on each. If a machine is not listed, the place is not known to exist there.",
		true, nil,
		func(ctx context.Context, in placeGetIn) (any, error) {
			return st.GetPlace(ctx, in.Key)
		})

	add(srv, "todo_add", "Add todo",
		"Record a todo (D-<n>) when the owner asks you to note something for later. A todo is one line that may grow into a task or be dropped. Steps of an existing task belong in its plan document, not here.",
		false, nil,
		func(ctx context.Context, in todoAddIn) (any, error) {
			t, err := st.AddTodo(ctx, store.NewTodo{Text: in.Text, Note: in.Note, Places: in.Places, OriginTask: in.OriginTask, Tags: in.Tags, Source: in.Source})
			if err != nil {
				return nil, err
			}
			return s.todos([]store.Todo{t})[0], nil
		})

	add(srv, "todo_list", "List todos",
		"List todos, newest first, or best match first with a query. By default: open todos of every place. place limits the list to one place key; place none lists todos without a place.",
		true, map[string][]string{"state": store.TodoStates},
		func(ctx context.Context, in todoListIn) (any, error) {
			res, err := st.ListTodos(ctx, store.TodoListParams{Place: in.Place, States: in.State, Query: in.Query, Tags: in.Tags, Limit: in.Limit, Offset: in.Offset})
			if err != nil {
				return nil, err
			}
			return map[string]any{"total": res.Total, "todos": s.todos(res.Todos), "url": s.Links.Todos(in.Place)}, nil
		})

	add(srv, "todo_update", "Update todo",
		"Edit a todo, or close it with state done or dropped; state open reopens it. To turn todos into a task, use task_create from_todos.",
		false, map[string][]string{"state": {"open", "done", "dropped"}},
		func(ctx context.Context, in todoUpdateIn) (any, error) {
			t, err := st.UpdateTodo(ctx, in.ID, store.TodoPatch{Text: in.Text, Note: in.Note, State: in.State,
				PlacesAdd: in.PlacesAdd, PlacesRemove: in.PlacesRemove, TagsAdd: in.TagsAdd, TagsRemove: in.TagsRemove})
			if err != nil {
				return nil, err
			}
			return s.todos([]store.Todo{t})[0], nil
		})
}
