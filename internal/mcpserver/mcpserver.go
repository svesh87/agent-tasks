// Package mcpserver exposes the store as MCP tools (see CONTRACT.md).
package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/svesh87/agent-tasks/internal/links"
	"github.com/svesh87/agent-tasks/internal/store"
)

// Instructions are sent to clients on initialize.
const Instructions = `agent-tasks keeps tasks that outlive one session or one repository. A task has documents (plan, research, notes) with immutable revisions, plan items ("- [ ]" lines) with stable ids, the owner's review rounds with comments, and a journal.

Usual flow:
1. Find the task with task_search or task_get.
2. Publish a plan with doc_write and send the owner the url it returns.
3. Wait for the review with review_wait, or check once with review_pending.
4. Answer every comment with comment_reply (resolve: true when handled), write the next revision with doc_write, then call round_addressed.
5. Tick plan items with plan_item_set; do not rewrite the document for that.
6. Before the session ends, add a journal entry of kind "context" that says where the work stands.

Places: work is tied to a git repository (key: the origin remote as host/path, the same on every machine) or to a directory without a remote (key: <machine>:<absolute path>). Call place_here with your directory, its git remote and the machine name. It returns the tasks and open todos of that place and records where the place lives on this machine. Link a task to places with task_create places or task_update places_add.

Todos (D-<n>) are one-line notes the owner asks you to keep for later: todo_add, todo_list, todo_update. task_create from_todos turns open todos into a task.

Task ids look like T-12. Results carry web urls; give them to the owner unchanged. Keep scratch files in the task's workspace folder (field "workspace"), not on this server.`

// Server builds MCP servers bound to a store.
type Server struct {
	Store         *store.Store
	Links         links.Links
	WorkspaceRoot string // e.g. ~/agent-work, returned unexpanded
	Version       string
	MaxWait       time.Duration // review_wait cap; 0 = DefaultMaxWait
}

func (s *Server) workspace(id string) string {
	return strings.TrimRight(s.WorkspaceRoot, "/") + "/" + id
}

// New returns an MCP server with all tools registered.
func (s *Server) New() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "agent-tasks", Version: s.Version}, &mcp.ServerOptions{Instructions: Instructions})
	s.register(srv)
	return srv
}

// schema infers the input schema of T and sets enums on named properties.
func schema[T any](enums map[string][]string) *jsonschema.Schema {
	sc, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	for name, vals := range enums {
		p := sc.Properties[name]
		if p == nil {
			panic("no property " + name)
		}
		target := p
		if p.Items != nil {
			target = p.Items
		}
		for _, v := range vals {
			target.Enum = append(target.Enum, v)
		}
	}
	return sc
}

func add[In any](srv *mcp.Server, name, title, desc string, readOnly bool, enums map[string][]string, h func(context.Context, In) (any, error)) {
	tool := &mcp.Tool{
		Name:        name,
		Title:       title,
		Description: desc,
		InputSchema: schema[In](enums),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: readOnly},
	}
	if !readOnly {
		f := false
		tool.Annotations.DestructiveHint = &f
	}
	mcp.AddTool(srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		out, err := h(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	})
}

// ---- outputs ----

type taskOut struct {
	store.Task
	URL       string `json:"url"`
	Workspace string `json:"workspace"`
}

func (s *Server) task(t store.Task) taskOut {
	return taskOut{Task: t, URL: s.Links.Task(t.ID), Workspace: s.workspace(t.ID)}
}

type docSummaryOut struct {
	store.DocSummary
	URL string `json:"url"`
}

type roundBriefOut struct {
	store.RoundBrief
	URL string `json:"url"`
}

type overviewOut struct {
	taskOut
	Documents     []docSummaryOut      `json:"documents"`
	Progress      store.Progress       `json:"plan_progress"`
	Journal       []store.JournalEntry `json:"journal"`
	PendingRounds []roundBriefOut      `json:"pending_rounds"`
	Todos         []todoOut            `json:"todos"`
}

type commentOut struct {
	store.Comment
	URL string `json:"url"`
}

func (s *Server) comments(cs []store.Comment) []commentOut {
	out := make([]commentOut, len(cs))
	for i, c := range cs {
		out[i] = commentOut{Comment: c, URL: s.Links.Comment(c.Task, c.Slug, c.Revision, c.ID)}
	}
	return out
}

type roundOut struct {
	Round       int64        `json:"round"`
	Task        string       `json:"task"`
	Slug        string       `json:"slug"`
	N           int          `json:"n"`
	Revision    int          `json:"revision"`
	State       string       `json:"state"`
	Summary     string       `json:"summary,omitempty"`
	Message     string       `json:"message,omitempty"`
	SubmittedAt string       `json:"submitted_at,omitempty"`
	AddressedAt string       `json:"addressed_at,omitempty"`
	AddressedIn int          `json:"addressed_revision,omitempty"`
	URL         string       `json:"url"`
	Comments    []commentOut `json:"comments"`
}

func (s *Server) round(r store.Round) roundOut {
	return roundOut{
		Round: r.ID, Task: r.Task, Slug: r.Slug, N: r.N, Revision: r.Revision, State: r.State,
		Summary: r.Summary, Message: r.Message, SubmittedAt: r.SubmittedAt, AddressedAt: r.AddressedAt, AddressedIn: r.AddressedRevision,
		URL:      s.Links.Revision(r.Task, r.Slug, r.Revision),
		Comments: s.comments(r.Comments),
	}
}

// ---- inputs ----

type placeRefIn struct {
	Place string `json:"place" jsonschema:"Place key: a git remote or host/path (github.com/owner/repo), or <machine>:<absolute path> for a directory without a remote. Unknown places are created."`
	Role  string `json:"role,omitempty" jsonschema:"primary (where the work mainly happens) or touched (default)."`
	Note  string `json:"note,omitempty" jsonschema:"What the task does there, short."`
}

func placeRefs(in []placeRefIn) []store.PlaceRef {
	out := make([]store.PlaceRef, len(in))
	for i, r := range in {
		out[i] = store.PlaceRef{Place: r.Place, Role: r.Role, Note: r.Note}
	}
	return out
}

type taskCreateIn struct {
	Title     string       `json:"title" jsonschema:"Short task title, in the owner's language."`
	Summary   string       `json:"summary,omitempty" jsonschema:"Optional short markdown: what the task is about and what done looks like."`
	Tags      []string     `json:"tags,omitempty" jsonschema:"Free tags, e.g. topic:<name>, area:work or area:personal. Repositories and directories go to places, not tags."`
	Status    string       `json:"status,omitempty" jsonschema:"Initial status, default active."`
	WaitingOn string       `json:"waiting_on,omitempty" jsonschema:"With status waiting: who or what the task waits for."`
	Places    []placeRefIn `json:"places,omitempty" jsonschema:"Places the task works in (repositories, directories). Can be added later."`
	FromTodos []string     `json:"from_todos,omitempty" jsonschema:"Open todo ids (D-<n>) this task is formed from; they become in_task and their places join the task."`
	Source    string       `json:"source,omitempty" jsonschema:"Who writes (machine, session) for the journal entry listing the todos."`
}

type taskGetIn struct {
	ID      string `json:"id" jsonschema:"Task id, e.g. T-12."`
	Machine string `json:"machine,omitempty" jsonschema:"Your machine name. Each place then gets here: its path on this machine, absent if unknown. Locations on all machines are listed either way."`
}

type taskUpdateIn struct {
	ID           string       `json:"id" jsonschema:"Task id, e.g. T-12."`
	Title        *string      `json:"title,omitempty" jsonschema:"New title."`
	Summary      *string      `json:"summary,omitempty" jsonschema:"New summary (markdown); empty string clears it."`
	Status       *string      `json:"status,omitempty" jsonschema:"New status. archived requires closed_reason; leaving archived clears it."`
	WaitingOn    *string      `json:"waiting_on,omitempty" jsonschema:"What the task waits for (kept only while status is waiting)."`
	ClosedReason *string      `json:"closed_reason,omitempty" jsonschema:"Why the task is archived: done or cancelled."`
	TagsAdd      []string     `json:"tags_add,omitempty" jsonschema:"Tags to add."`
	TagsRemove   []string     `json:"tags_remove,omitempty" jsonschema:"Tags to remove."`
	PlacesAdd    []placeRefIn `json:"places_add,omitempty" jsonschema:"Places to link (or to change role/note of)."`
	PlacesRemove []string     `json:"places_remove,omitempty" jsonschema:"Place keys to unlink."`
}

type taskSearchIn struct {
	Query  string   `json:"query,omitempty" jsonschema:"Words to find in titles, summaries, latest document revisions, journal and review comments. Russian and English word forms match (задачи finds задача). Omit to list by filters only."`
	Status []string `json:"status,omitempty" jsonschema:"Statuses to include. Default: all except archived."`
	Place  string   `json:"place,omitempty" jsonschema:"Only tasks linked to this place (git remote, host/path or <machine>:<path>); none = tasks without places."`
	Tags   []string `json:"tags,omitempty" jsonschema:"Tasks must carry all of these tags."`
	Sort   string   `json:"sort,omitempty" jsonschema:"Order, newest first. Default: relevance with a query, else updated."`
	Limit  int      `json:"limit,omitempty" jsonschema:"Page size, default 20, max 100."`
	Offset int      `json:"offset,omitempty" jsonschema:"Skip this many results."`
}

type docWriteIn struct {
	Task    string `json:"task" jsonschema:"Task id, e.g. T-12."`
	Slug    string `json:"slug" jsonschema:"Document name within the task: lowercase letters, digits, . _ -; e.g. plan, research, review-2026-11-20."`
	Body    string `json:"body" jsonschema:"Full markdown of the new revision (not a patch). Task-list lines '- [ ] text' become plan items."`
	Kind    string `json:"kind,omitempty" jsonschema:"Document kind; default from the slug (plan, research, review, notes), else other."`
	Title   string `json:"title,omitempty" jsonschema:"Document title; default: the first '# ' heading, else the slug."`
	Message string `json:"message,omitempty" jsonschema:"One line about this revision, e.g. 'addresses review round 2'."`
}

type docGetIn struct {
	Task     string `json:"task" jsonschema:"Task id, e.g. T-12."`
	Slug     string `json:"slug" jsonschema:"Document slug, e.g. plan."`
	Revision int    `json:"revision,omitempty" jsonschema:"Revision number; default the latest."`
}

type docDiffIn struct {
	Task string `json:"task" jsonschema:"Task id, e.g. T-12."`
	Slug string `json:"slug" jsonschema:"Document slug."`
	From int    `json:"from" jsonschema:"Older revision number."`
	To   int    `json:"to,omitempty" jsonschema:"Newer revision number; default the latest."`
}

type planItemSetIn struct {
	Task string `json:"task" jsonschema:"Task id, e.g. T-12."`
	Slug string `json:"slug" jsonschema:"Document slug, e.g. plan."`
	Item string `json:"item" jsonschema:"Plan item id from doc_write or doc_get, e.g. i3."`
	Done *bool  `json:"done,omitempty" jsonschema:"true ticks the item, false unticks it; omit to leave as is."`
	Note string `json:"note,omitempty" jsonschema:"Short note to append, e.g. what was done or why it is blocked."`
	By   string `json:"by,omitempty" jsonschema:"Who did it: agent (the default), or owner when the owner told you to tick for them."`
}

type reviewPendingIn struct {
	Task string `json:"task,omitempty" jsonschema:"Task id to limit to; omit for all tasks."`
}

type commentReplyIn struct {
	Comment int64  `json:"comment" jsonschema:"Comment id from review_pending or doc_get."`
	Body    string `json:"body" jsonschema:"Reply in markdown: what was changed, or why not."`
	Resolve bool   `json:"resolve,omitempty" jsonschema:"Mark the comment resolved (handled). The owner can reopen it."`
}

type roundAddressedIn struct {
	Round   int64  `json:"round" jsonschema:"Round id from review_pending."`
	Message string `json:"message,omitempty" jsonschema:"One line for the owner, e.g. 'revision 3 addresses all comments'."`
}

type journalAppendIn struct {
	Task   string `json:"task" jsonschema:"Task id, e.g. T-12."`
	Kind   string `json:"kind" jsonschema:"context: snapshot of where the work stands, for a later session; log: what was done; decision: what was decided and why, including the owner's choices."`
	Body   string `json:"body" jsonschema:"Markdown."`
	Source string `json:"source,omitempty" jsonschema:"Who writes: machine, session or agent name."`
}

type journalGetIn struct {
	Task  string `json:"task" jsonschema:"Task id, e.g. T-12."`
	Kind  string `json:"kind,omitempty" jsonschema:"Only this kind; default all."`
	Limit int    `json:"limit,omitempty" jsonschema:"How many latest entries, default 20."`
}

type placeHereIn struct {
	Cwd     string `json:"cwd" jsonschema:"Absolute path of the working directory; for a git repository preferably its top level (git rev-parse --show-toplevel)."`
	Remote  string `json:"remote,omitempty" jsonschema:"Output of 'git remote get-url origin' in cwd, if any. Credentials are stripped by the server."`
	Machine string `json:"machine" jsonschema:"Name of this machine (e.g. hostname), the same in every session on it."`
}

type placeGetIn struct {
	Key string `json:"key" jsonschema:"Place key: git remote or host/path, or <machine>:<absolute path>."`
}

type todoAddIn struct {
	Text       string   `json:"text" jsonschema:"One line: what to do."`
	Note       string   `json:"note,omitempty" jsonschema:"Details in markdown."`
	Places     []string `json:"places,omitempty" jsonschema:"Place keys the todo belongs to (git remote, host/path, <machine>:<path>); may be empty."`
	OriginTask string   `json:"origin_task,omitempty" jsonschema:"Task where it came up, for provenance only; the todo stays independent of it."`
	Tags       []string `json:"tags,omitempty" jsonschema:"Free tags."`
	Source     string   `json:"source,omitempty" jsonschema:"Who records it (machine, session)."`
}

type todoListIn struct {
	Place  string   `json:"place,omitempty" jsonschema:"Only todos of this place; none = todos without places."`
	State  []string `json:"state,omitempty" jsonschema:"States to include; default open."`
	Query  string   `json:"query,omitempty" jsonschema:"Words to find in text and note (Russian and English word forms match)."`
	Tags   []string `json:"tags,omitempty" jsonschema:"Todos must carry all of these tags."`
	Limit  int      `json:"limit,omitempty" jsonschema:"Page size, default 50, max 200."`
	Offset int      `json:"offset,omitempty" jsonschema:"Skip this many results."`
}

type todoUpdateIn struct {
	ID           string   `json:"id" jsonschema:"Todo id, e.g. D-7."`
	Text         *string  `json:"text,omitempty" jsonschema:"New one-line text."`
	Note         *string  `json:"note,omitempty" jsonschema:"New note; empty string clears it."`
	State        *string  `json:"state,omitempty" jsonschema:"open, done or dropped. A todo goes into a task through task_create from_todos."`
	PlacesAdd    []string `json:"places_add,omitempty" jsonschema:"Place keys to add."`
	PlacesRemove []string `json:"places_remove,omitempty" jsonschema:"Place keys to remove."`
	TagsAdd      []string `json:"tags_add,omitempty" jsonschema:"Tags to add."`
	TagsRemove   []string `json:"tags_remove,omitempty" jsonschema:"Tags to remove."`
}

type todoOut struct {
	store.Todo
	URL string `json:"url"`
}

func (s *Server) todos(ts []store.Todo) []todoOut {
	out := make([]todoOut, len(ts))
	for i, t := range ts {
		out[i] = todoOut{t, s.Links.Todo(t.ID)}
	}
	return out
}

func (s *Server) tasks(ts []store.Task) []taskOut {
	out := make([]taskOut, len(ts))
	for i, t := range ts {
		out[i] = s.task(t)
	}
	return out
}

func (s *Server) register(srv *mcp.Server) {
	st := s.Store
	s.registerPlaces(srv)
	s.registerWait(srv)

	add(srv, "task_create", "Create task",
		"Create a task. You can link places and build it from open todos (from_todos): those todos move to in_task and their places join the task. Returns the task with its id (T-<n>), web url and workspace folder for scratch files.",
		false, map[string][]string{"status": {"active", "waiting", "later"}},
		func(ctx context.Context, in taskCreateIn) (any, error) {
			t, err := st.CreateTask(ctx, store.NewTask{Title: in.Title, Summary: in.Summary, Tags: in.Tags, Status: in.Status, WaitingOn: in.WaitingOn,
				Places: placeRefs(in.Places), FromTodos: in.FromTodos, Source: in.Source})
			if err != nil {
				return nil, err
			}
			return s.task(t), nil
		})

	add(srv, "task_get", "Get task",
		"Get a task with what you need to resume it: fields, url, workspace folder, places with their locations on every machine (pass machine to get here, the path on your machine), documents with latest revision, open comments and plan progress, overall plan progress, the last 10 journal entries, review rounds waiting for you, and the todos it was built from.",
		true, nil,
		func(ctx context.Context, in taskGetIn) (any, error) {
			o, err := st.TaskOverview(ctx, in.ID, 10, false, strings.TrimSpace(in.Machine))
			if err != nil {
				return nil, err
			}
			out := overviewOut{taskOut: s.task(o.Task), Progress: o.Progress, Journal: o.Journal, Todos: s.todos(o.Todos)}
			out.Documents = make([]docSummaryOut, len(o.Documents))
			for i, d := range o.Documents {
				out.Documents[i] = docSummaryOut{d, s.Links.Doc(o.ID, d.Slug)}
			}
			out.PendingRounds = make([]roundBriefOut, len(o.PendingRounds))
			for i, r := range o.PendingRounds {
				out.PendingRounds[i] = roundBriefOut{r, s.Links.Revision(o.ID, r.Slug, r.Revision)}
			}
			return out, nil
		})

	add(srv, "task_update", "Update task",
		"Change task fields, tags and places. Fields you leave out stay as they are. To archive, set status archived and closed_reason done or cancelled. Archived tasks are deleted after the retention period; their todos are kept.",
		false, map[string][]string{"status": store.Statuses, "closed_reason": store.CloseReasons},
		func(ctx context.Context, in taskUpdateIn) (any, error) {
			t, err := st.UpdateTask(ctx, in.ID, store.TaskPatch{Title: in.Title, Summary: in.Summary, Status: in.Status,
				WaitingOn: in.WaitingOn, ClosedReason: in.ClosedReason, TagsAdd: in.TagsAdd, TagsRemove: in.TagsRemove,
				PlacesAdd: placeRefs(in.PlacesAdd), PlacesRemove: in.PlacesRemove})
			if err != nil {
				return nil, err
			}
			return s.task(t), nil
		})

	add(srv, "task_search", "Search tasks",
		"Find tasks by words and filters. Archived tasks are left out unless status asks for them. Each hit names what matched (kind: task, document, comment, round or journal), shows a snippet with matches in **bold**, and gives the url of that exact spot. Give the owner that url. With a query, matching todos (kind todo) are listed as well.",
		true, map[string][]string{"status": store.Statuses, "sort": store.SortOrders},
		func(ctx context.Context, in taskSearchIn) (any, error) {
			res, err := st.Search(ctx, store.SearchParams{Query: in.Query, Statuses: in.Status, Tags: in.Tags, Place: in.Place, Sort: in.Sort, Limit: in.Limit, Offset: in.Offset, Todos: true})
			if err != nil {
				return nil, err
			}
			type hitOut struct {
				Kind     string `json:"kind"`
				Source   string `json:"source"`
				Slug     string `json:"slug,omitempty"`
				Revision int    `json:"revision,omitempty"`
				Line     int    `json:"line,omitempty"`
				Snippet  string `json:"snippet"`
				URL      string `json:"url"`
			}
			type item struct {
				taskOut
				Hits []hitOut `json:"hits,omitempty"`
			}
			type todoHit struct {
				Kind    string `json:"kind"`
				ID      string `json:"id"`
				Text    string `json:"text"`
				State   string `json:"state"`
				Snippet string `json:"snippet"`
				URL     string `json:"url"`
			}
			out := struct {
				Total int       `json:"total"`
				Tasks []item    `json:"tasks"`
				Todos []todoHit `json:"todos,omitempty"`
			}{Total: res.Total, Tasks: []item{}}
			for _, th := range res.Tasks {
				it := item{taskOut: s.task(th.Task)}
				for _, h := range th.Hits {
					it.Hits = append(it.Hits, hitOut{h.Kind, h.Source, h.Slug, h.Revision, h.Line, h.Marked(),
						s.Links.Hit(th.ID, h.Kind, h.Slug, h.Revision, h.Start, h.End, h.Comment, h.Round, h.Journal)})
				}
				out.Tasks = append(out.Tasks, it)
			}
			for _, td := range res.Todos {
				out.Todos = append(out.Todos, todoHit{"todo", td.ID, td.Text, td.State, td.Snippet.Marked(), s.Links.Todo(td.ID)})
			}
			return out, nil
		})

	add(srv, "doc_write", "Write document",
		"Create a document in a task, or add a revision with the full new body. Plan items ('- [ ] text' lines) keep their ids, ticks and notes when their text stays the same or changes a little. New items get new ids; a new '- [x]' line starts ticked. Open review comments move to the new revision; those whose text is gone come back in outdated. An identical body writes nothing (unchanged: true). Send the owner the url it returns.",
		false, map[string][]string{"kind": store.DocKinds},
		func(ctx context.Context, in docWriteIn) (any, error) {
			res, err := st.WriteDoc(ctx, store.WriteDocParams{Task: in.Task, Slug: in.Slug, Body: in.Body, Kind: in.Kind, Title: in.Title, Message: in.Message, Author: store.AuthorAgent})
			if err != nil {
				return nil, err
			}
			d := res.Document
			out := map[string]any{
				"task": d.Task, "slug": d.Slug, "kind": d.Kind, "title": d.Title,
				"revision": res.Revision, "created": res.Created, "unchanged": res.Unchanged,
				"url":        s.Links.Doc(d.Task, d.Slug),
				"items":      res.Items,
				"reanchored": res.Reanchored,
				"outdated":   s.comments(res.Outdated),
			}
			if res.Revision > 1 && !res.Unchanged {
				out["diff_url"] = s.Links.Diff(d.Task, d.Slug, res.Revision-1, res.Revision)
			}
			return out, nil
		})

	add(srv, "doc_get", "Get document",
		"Get a document revision (the latest by default): body, plan items, open comments of submitted rounds, and the revision list. An item's done state comes from the server, not from the box written in the body. Each comment has its quote, its line in the reviewed revision and its current position in the latest revision.",
		true, nil,
		func(ctx context.Context, in docGetIn) (any, error) {
			v, err := st.GetDoc(ctx, in.Task, in.Slug, store.DocGetOptions{Revision: in.Revision, OnlyOpen: true, WithRevisions: true})
			if err != nil {
				return nil, err
			}
			d := v.Document
			return map[string]any{
				"task": d.Task, "slug": d.Slug, "kind": d.Kind, "title": d.Title,
				"latest_revision": d.LatestRevision,
				"revision":        v.Revision.N, "author": v.Revision.Author, "message": v.Revision.Message, "created_at": v.Revision.CreatedAt,
				"url":           s.Links.Revision(d.Task, d.Slug, v.Revision.N),
				"body":          v.Revision.Body,
				"items":         v.Items,
				"open_comments": s.comments(v.Comments),
				"revisions":     v.Revisions,
			}, nil
		})

	add(srv, "doc_diff", "Diff revisions",
		"Unified diff between two revisions of a document (to defaults to the latest). Empty diff means identical.",
		true, nil,
		func(ctx context.Context, in docDiffIn) (any, error) {
			d, err := st.Diff(ctx, in.Task, in.Slug, in.From, in.To)
			if err != nil {
				return nil, err
			}
			return map[string]any{"task": d.Document.Task, "slug": d.Document.Slug, "from": d.From, "to": d.To,
				"url": s.Links.Diff(d.Document.Task, d.Document.Slug, d.From, d.To), "diff": d.Unified}, nil
		})

	add(srv, "plan_item_set", "Set plan item",
		"Tick or untick a plan item, add a short note, or both. Each tick and note records who did it: by agent (the default) or by owner, when the owner told you to tick for them. Items show this as done_by and notes[].by. No revision is created; use this instead of rewriting the document.",
		false, map[string][]string{"by": {"agent", "owner"}},
		func(ctx context.Context, in planItemSetIn) (any, error) {
			it, err := st.SetPlanItem(ctx, in.Task, in.Slug, in.Item, in.Done, in.Note, in.By)
			if err != nil {
				return nil, err
			}
			id, _ := store.ParseTaskID(in.Task)
			return map[string]any{"item": it, "url": s.Links.Doc(store.TaskID(id), in.Slug)}, nil
		})

	add(srv, "review_pending", "Pending reviews",
		"Review rounds the owner has submitted and nobody has addressed yet, for one task or all. Each comment comes with id, state, body, replies, its anchor (quote and line in the reviewed revision; none for a general comment) and its current position in the latest revision. Answer each comment with comment_reply, write a new revision with doc_write if needed, then call round_addressed.",
		true, nil,
		func(ctx context.Context, in reviewPendingIn) (any, error) {
			rs, err := st.PendingRounds(ctx, in.Task)
			if err != nil {
				return nil, err
			}
			out := make([]roundOut, len(rs))
			for i, r := range rs {
				out[i] = s.round(r)
			}
			return map[string]any{"rounds": out}, nil
		})

	add(srv, "comment_reply", "Reply to comment",
		"Reply to a review comment. resolve: true marks it handled. Reply to every comment in a round, even if only to say why nothing changed.",
		false, nil,
		func(ctx context.Context, in commentReplyIn) (any, error) {
			c, err := st.ReplyComment(ctx, store.ReplyParams{Comment: in.Comment, Author: store.AuthorAgent, Body: in.Body, Resolve: in.Resolve})
			if err != nil {
				return nil, err
			}
			return s.comments([]store.Comment{c})[0], nil
		})

	add(srv, "round_addressed", "Mark round addressed",
		"Mark a submitted review round as handled. Call it after you have replied to its comments and written the new revision, then give the owner the document url.",
		false, nil,
		func(ctx context.Context, in roundAddressedIn) (any, error) {
			r, err := st.RoundAddressed(ctx, in.Round, in.Message)
			if err != nil {
				return nil, err
			}
			out := s.round(r)
			out.URL = s.Links.Doc(r.Task, r.Slug)
			return out, nil
		})

	add(srv, "journal_append", "Append journal",
		"Add a journal entry to a task. Kinds: context (where the work stands, so a later session can pick it up), log (what was done), decision (what was decided and why).",
		false, map[string][]string{"kind": store.JournalKinds},
		func(ctx context.Context, in journalAppendIn) (any, error) {
			e, err := st.AppendJournal(ctx, in.Task, in.Kind, in.Body, in.Source)
			if err != nil {
				return nil, err
			}
			id, _ := store.ParseTaskID(in.Task)
			return map[string]any{"entry": e, "url": s.Links.Task(store.TaskID(id))}, nil
		})

	add(srv, "journal_get", "Get journal",
		"Latest journal entries of a task, newest first. To resume work, read the last entry of kind context.",
		true, map[string][]string{"kind": store.JournalKinds},
		func(ctx context.Context, in journalGetIn) (any, error) {
			es, err := st.Journal(ctx, in.Task, in.Kind, in.Limit)
			if err != nil {
				return nil, err
			}
			id, _ := store.ParseTaskID(in.Task)
			return map[string]any{"task": store.TaskID(id), "url": s.Links.Task(store.TaskID(id)), "entries": es}, nil
		})
}
