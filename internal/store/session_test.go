package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeRepo(t *testing.T) {
	cases := map[string]string{
		"git@github.com:acme/infra.git":                      "github.com/acme/infra",
		"git@github.com:/acme/infra.git":                     "github.com/acme/infra",
		"https://github.com/acme/infra.git":                  "github.com/acme/infra",
		"https://user:secret@GitHub.com/acme/infra/":         "github.com/acme/infra",
		"https://oauth2:tok@gitlab.example.com:8443/g/p.git": "gitlab.example.com/g/p",
		"ssh://git@repo.example.com:2222/team/proj.git":      "repo.example.com/team/proj",
		"ssh://repo.example.com/team/proj":                   "repo.example.com/team/proj",
		"user@host.example:team/proj.git":                    "host.example/team/proj",
		"github.com/acme/infra":                              "github.com/acme/infra",
		"repo:github.com/acme/infra":                         "github.com/acme/infra",
		"https://gitlab.example.com/group/sub/project.git":   "gitlab.example.com/group/sub/project",
	}
	for in, want := range cases {
		if got := NormalizeRepo(in); got != want {
			t.Errorf("NormalizeRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePlaceKey(t *testing.T) {
	cases := []struct{ in, kind, key string }{
		{"laptop:/home/u/experiments/", "path", "laptop:/home/u/experiments"},
		{"git@github.com:a/b.git", "git", "github.com/a/b"},
		{"git@host:/srv/repo.git", "git", "host/srv/repo"},
		{"https://github.com/a/b", "git", "github.com/a/b"},
	}
	for _, c := range cases {
		kind, key, err := ParsePlaceKey(c.in)
		if err != nil || kind != c.kind || key != c.key {
			t.Errorf("ParsePlaceKey(%q) = %q %q %v", c.in, kind, key, err)
		}
	}
	if _, _, err := ParsePlaceKey("justaword"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bare word accepted: %v", err)
	}
}

func TestPlaceLocationLearning(t *testing.T) {
	s := newStore(t)
	// git place: same key from any machine, a location per machine
	h1 := must[HereResult](t)(s.PlaceHere(ctx, "/home/dev/src/infra", "git@github.com:acme/infra.git", "laptop"))
	if !h1.Created || h1.Place.Key != "github.com/acme/infra" || h1.Place.Kind != "git" || h1.Place.Label != "infra" || h1.Path != "/home/dev/src/infra" {
		t.Fatalf("here 1 %+v", h1)
	}
	h2 := must[HereResult](t)(s.PlaceHere(ctx, "/Users/dev/src/infra", "https://github.com/acme/infra", "macbook"))
	if h2.Created || len(h2.Place.Locations) != 2 {
		t.Fatalf("here 2 %+v", h2)
	}
	// a subfolder of a known location on the same machine, without a remote,
	// resolves to the same place and does not add a location
	h3 := must[HereResult](t)(s.PlaceHere(ctx, "/home/dev/src/infra/roles/x", "", "laptop"))
	if h3.Place.Key != "github.com/acme/infra" || h3.Path != "/home/dev/src/infra" || len(h3.Place.Locations) != 2 {
		t.Fatalf("here 3 %+v", h3)
	}
	// a git subfolder with the remote also keeps the repo root location
	h4 := must[HereResult](t)(s.PlaceHere(ctx, "/home/dev/src/infra/docs", "git@github.com:acme/infra.git", "laptop"))
	if h4.Path != "/home/dev/src/infra" || len(h4.Place.Locations) != 2 {
		t.Fatalf("here 4 %+v", h4)
	}
	// a directory without a remote is a path place, here and only here
	h5 := must[HereResult](t)(s.PlaceHere(ctx, "/home/dev/experiments/", "", "laptop"))
	if !h5.Created || h5.Place.Kind != "path" || h5.Place.Key != "laptop:/home/dev/experiments" || h5.Place.Label != "experiments" {
		t.Fatalf("here 5 %+v", h5)
	}
	h6 := must[HereResult](t)(s.PlaceHere(ctx, "/home/dev/experiments", "", "macbook"))
	if h6.Place.Key != "macbook:/home/dev/experiments" {
		t.Fatalf("here 6 %+v", h6)
	}
	// a prefix that is not a path boundary does not match
	h7 := must[HereResult](t)(s.PlaceHere(ctx, "/home/dev/experiments2", "", "laptop"))
	if h7.Place.Key != "laptop:/home/dev/experiments2" {
		t.Fatalf("here 7 %+v", h7)
	}
	if _, err := s.PlaceHere(ctx, "/x", "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no machine: %v", err)
	}
	p := must[Place](t)(s.GetPlace(ctx, "https://github.com/acme/infra.git"))
	if len(p.Locations) != 2 {
		t.Fatalf("place_get %+v", p)
	}
	ps := must[[]PlaceSummary](t)(s.Places(ctx, ""))
	if len(ps) != 4 {
		t.Fatalf("places %+v", ps)
	}
	// search: label match first, then key or path matches; case-insensitive
	ps = must[[]PlaceSummary](t)(s.Places(ctx, "EXPERIMENTS"))
	if len(ps) != 3 || ps[0].Label != "experiments" {
		t.Fatalf("places q=experiments %+v", ps)
	}
	ps = must[[]PlaceSummary](t)(s.Places(ctx, "dev/src"))
	if len(ps) != 1 || ps[0].Key != "github.com/acme/infra" {
		t.Fatalf("places q by path %+v", ps)
	}
	if ms := must[[]string](t)(s.Machines(ctx)); len(ms) != 2 || ms[0] != "laptop" {
		t.Fatalf("machines %v", ms)
	}
}

func TestTaskPlacesAndSearch(t *testing.T) {
	s := newStore(t)
	a := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "a", Places: []PlaceRef{{Place: "git@github.com:acme/infra.git", Role: "primary"}, {Place: "github.com/acme/tools", Note: "MCP"}}}))
	if len(a.Places) != 2 || a.Places[0].Role != "primary" || a.Places[0].Key != "github.com/acme/infra" || a.Places[1].Note != "MCP" {
		t.Fatalf("places %+v", a.Places)
	}
	must[Task](t)(s.CreateTask(ctx, NewTask{Title: "b"}))
	up := must[Task](t)(s.UpdateTask(ctx, a.ID, TaskPatch{PlacesRemove: []string{"https://github.com/acme/tools"}, PlacesAdd: []PlaceRef{{Place: "laptop:/home/dev/exp", Role: "touched"}}}))
	if len(up.Places) != 2 || up.Places[1].Key != "laptop:/home/dev/exp" {
		t.Fatalf("update places %+v", up.Places)
	}
	if _, err := s.UpdateTask(ctx, a.ID, TaskPatch{PlacesAdd: []PlaceRef{{Place: "github.com/a/b", Role: "boss"}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad role: %v", err)
	}
	res := must[SearchResult](t)(s.Search(ctx, SearchParams{Place: "https://github.com/acme/infra"}))
	if res.Total != 1 || res.Tasks[0].ID != a.ID {
		t.Fatalf("place filter %+v", res)
	}
	res = must[SearchResult](t)(s.Search(ctx, SearchParams{Place: "none"}))
	if res.Total != 1 || res.Tasks[0].Title != "b" {
		t.Fatalf("none filter %+v", res)
	}
	// every task response has locations on all machines; machine adds "here"
	must[HereResult](t)(s.PlaceHere(ctx, "/src/infra", "github.com/acme/infra", "laptop"))
	must[HereResult](t)(s.PlaceHere(ctx, "/Users/x/infra", "github.com/acme/infra", "mac"))
	ov := must[Overview](t)(s.TaskOverview(ctx, a.ID, 5, false, "laptop"))
	if p := ov.Places[0]; len(p.Locations) != 2 || p.Here != "/src/infra" {
		t.Fatalf("overview locations %+v", p)
	}
	if p := must[Task](t)(s.GetTask(ctx, a.ID)).Places[0]; len(p.Locations) != 2 || p.Here != "" {
		t.Fatalf("task locations %+v", p)
	}
}

func TestTodoLifecycle(t *testing.T) {
	s := newStore(t)
	origin := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "origin"}))
	d1 := must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "Проверить бэкапы базы", Note: "после переезда", Places: []string{"git@github.com:acme/infra.git"}, OriginTask: origin.ID, Tags: []string{"topic:backup"}, Source: "laptop"}))
	if d1.ID != "D-1" || d1.State != "open" || len(d1.Places) != 1 || d1.OriginTask != origin.ID || d1.Tags[0] != "topic:backup" {
		t.Fatalf("todo %+v", d1)
	}
	must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "Без места"}))
	if _, err := s.AddTodo(ctx, NewTodo{Text: "two\nlines"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("multi-line text: %v", err)
	}
	l := must[TodoList](t)(s.ListTodos(ctx, TodoListParams{}))
	if l.Total != 2 {
		t.Fatalf("list %+v", l)
	}
	l = must[TodoList](t)(s.ListTodos(ctx, TodoListParams{Place: "none"}))
	if l.Total != 1 || l.Todos[0].Text != "Без места" {
		t.Fatalf("none %+v", l)
	}
	l = must[TodoList](t)(s.ListTodos(ctx, TodoListParams{Place: "https://github.com/acme/infra"}))
	if l.Total != 1 || l.Todos[0].ID != "D-1" {
		t.Fatalf("place %+v", l)
	}
	l = must[TodoList](t)(s.ListTodos(ctx, TodoListParams{Query: "бэкап"}))
	if l.Total != 1 || l.Todos[0].ID != "D-1" {
		t.Fatalf("query %+v", l)
	}
	l = must[TodoList](t)(s.ListTodos(ctx, TodoListParams{Tags: []string{"topic:backup"}}))
	if l.Total != 1 {
		t.Fatalf("tags %+v", l)
	}
	up := must[Todo](t)(s.UpdateTodo(ctx, "d-2", TodoPatch{State: ptr("dropped"), PlacesAdd: []string{"laptop:/tmp/x"}, TagsAdd: []string{"x"}}))
	if up.State != "dropped" || up.ClosedAt == "" || len(up.Places) != 1 {
		t.Fatalf("dropped %+v", up)
	}
	if l = must[TodoList](t)(s.ListTodos(ctx, TodoListParams{})); l.Total != 1 {
		t.Fatalf("dropped still listed %+v", l)
	}
	if l = must[TodoList](t)(s.ListTodos(ctx, TodoListParams{States: []string{"dropped", "done"}})); l.Total != 1 {
		t.Fatalf("closed list %+v", l)
	}
	re := must[Todo](t)(s.UpdateTodo(ctx, "D-2", TodoPatch{State: ptr("open"), Text: ptr("Снова открыт")}))
	if re.State != "open" || re.ClosedAt != "" || re.Text != "Снова открыт" {
		t.Fatalf("reopen %+v", re)
	}
	if _, err := s.UpdateTodo(ctx, "D-2", TodoPatch{State: ptr("in_task")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("in_task directly: %v", err)
	}

	// archiving and deleting the origin task never touches the todo
	must[Task](t)(s.UpdateTask(ctx, origin.ID, TaskPatch{Status: ptr("archived"), ClosedReason: ptr("done")}))
	if got := must[Todo](t)(s.GetTodo(ctx, "D-1")); got.State != "open" || got.OriginTask != origin.ID {
		t.Fatalf("after archive %+v", got)
	}
	s.Now = func() time.Time { return time.Now().UTC().AddDate(0, 0, 400) }
	if ids := must[[]string](t)(s.PurgeArchived(ctx, 30)); len(ids) != 1 {
		t.Fatalf("purge %v", ids)
	}
	got := must[Todo](t)(s.GetTodo(ctx, "D-1"))
	if got.State != "open" || got.OriginTask != "" || got.Text != "Проверить бэкапы базы" {
		t.Fatalf("after retention %+v", got)
	}
}

func TestTaskFromTodos(t *testing.T) {
	s := newStore(t)
	d1 := must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "one", Places: []string{"github.com/a/b"}}))
	d2 := must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "two"}))
	dropped := must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "three"}))
	must[Todo](t)(s.UpdateTodo(ctx, dropped.ID, TodoPatch{State: ptr("dropped")}))

	if _, err := s.CreateTask(ctx, NewTask{Title: "x", FromTodos: []string{dropped.ID}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("closed todo accepted: %v", err)
	}
	nt := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "new", FromTodos: []string{d1.ID, d2.ID}, Source: "test"}))
	if len(nt.Places) != 1 || nt.Places[0].Key != "github.com/a/b" || nt.Places[0].Role != "touched" {
		t.Fatalf("places from todos %+v", nt.Places)
	}
	for _, id := range []string{d1.ID, d2.ID} {
		if td := must[Todo](t)(s.GetTodo(ctx, id)); td.State != "in_task" || td.Task != nt.ID {
			t.Fatalf("todo %s %+v", id, td)
		}
	}
	j := must[[]JournalEntry](t)(s.Journal(ctx, nt.ID, "", 0))
	all := ""
	for _, e := range j {
		all += e.Body + "\n"
	}
	if !strings.Contains(all, "D-1 one") || !strings.Contains(all, "D-2 two") {
		t.Fatalf("journal of new task:\n%s", all)
	}
	ov := must[Overview](t)(s.TaskOverview(ctx, nt.ID, 10, false, ""))
	if len(ov.Todos) != 2 {
		t.Fatalf("overview todos %+v", ov.Todos)
	}
	// merging is gone from the model
	if _, err := s.UpdateTask(ctx, nt.ID, TaskPatch{Status: ptr("archived"), ClosedReason: ptr("merged")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("closed_reason merged accepted: %v", err)
	}
	// reopening a todo that went into a task clears the link
	re := must[Todo](t)(s.UpdateTodo(ctx, d2.ID, TodoPatch{State: ptr("open")}))
	if re.Task != "" {
		t.Fatalf("reopen %+v", re)
	}
}

func TestSessionContext(t *testing.T) {
	s := newStore(t)
	a := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "a", Places: []PlaceRef{{Place: "github.com/acme/infra", Role: "primary"}, {Place: "github.com/acme/tools"}}}))
	b := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "b", Status: "waiting", WaitingOn: "owner", Places: []PlaceRef{{Place: "github.com/acme/infra"}}}))
	c := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "c", Places: []PlaceRef{{Place: "github.com/acme/infra"}}}))
	must[Task](t)(s.UpdateTask(ctx, c.ID, TaskPatch{Status: ptr("archived"), ClosedReason: ptr("done")}))
	other := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "other"}))
	must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "todo here", Places: []string{"github.com/acme/infra"}}))
	must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "todo elsewhere"}))
	// agent-tasks is known on this machine
	must[HereResult](t)(s.PlaceHere(ctx, "/home/x/src/tools", "git@github.com:acme/tools.git", "laptop"))

	info := must[SessionInfo](t)(s.SessionContext(ctx, "git@github.com:acme/infra.git", "/home/x/src/infra", "laptop"))
	if info.Place == nil || info.Place.Key != "github.com/acme/infra" || info.Path != "/home/x/src/infra" {
		t.Fatalf("place %+v %q", info.Place, info.Path)
	}
	if len(info.Tasks) != 2 || info.Tasks[0].ID != a.ID || info.Tasks[1].ID != b.ID {
		t.Fatalf("tasks %+v", info.Tasks)
	}
	op := info.Tasks[0].OtherPlaces
	if len(op) != 1 || op[0].Key != "github.com/acme/tools" || op[0].Path != "/home/x/src/tools" {
		t.Fatalf("other places %+v", op)
	}
	if len(info.Todos) != 1 || info.Todos[0].Text != "todo here" {
		t.Fatalf("todos %+v", info.Todos)
	}
	// the location was learned
	if p := must[Place](t)(s.GetPlace(ctx, "github.com/acme/infra")); len(p.Locations) != 1 || p.Locations[0].Machine != "laptop" {
		t.Fatalf("learned %+v", p)
	}
	// the workspace folder of a task
	info = must[SessionInfo](t)(s.SessionContext(ctx, "", "/home/x/agent-work/"+other.ID+"/scripts", "laptop"))
	found := false
	for _, st := range info.Tasks {
		found = found || st.ID == other.ID
	}
	if !found {
		t.Fatalf("cwd task %+v", info.Tasks)
	}
	info = must[SessionInfo](t)(s.SessionContext(ctx, "github.com/nobody/none", "/tmp/none", "laptop"))
	if len(info.Tasks) != 0 || len(info.Todos) != 0 {
		t.Fatalf("unexpected %+v", info)
	}
}
