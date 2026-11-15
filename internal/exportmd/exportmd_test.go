package exportmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svesh87/agent-tasks/internal/store"
)

func TestExportImport(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	task, _ := st.CreateTask(ctx, store.NewTask{Title: `Задача "с кавычками"`, Summary: "Кратко.", Tags: []string{"topic:x"}})
	if _, err := st.WriteDoc(ctx, store.WriteDocParams{Task: task.ID, Slug: "plan", Body: "# План\n\n- [ ] один\n- [ ] два\n", Author: store.AuthorAgent}); err != nil {
		t.Fatal(err)
	}
	done := true
	st.SetPlanItem(ctx, task.ID, "plan", "i2", &done, "", store.AuthorAgent)
	st.AppendJournal(ctx, task.ID, "log", "first", "laptop")
	st.AppendJournal(ctx, task.ID, "context", "second", "")

	out, err := Export(ctx, st, task.ID, filepath.Join(dir, "export"))
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	taskMD := read("task.md")
	if !strings.Contains(taskMD, `title: "Задача \"с кавычками\""`) || !strings.Contains(taskMD, `tags: ["topic:x"]`) || !strings.Contains(taskMD, "Кратко.") {
		t.Errorf("task.md:\n%s", taskMD)
	}
	planMD := read("plan.md")
	if !strings.Contains(planMD, "- [ ] один\n- [x] два") || !strings.Contains(planMD, "revision: 1") {
		t.Errorf("plan.md:\n%s", planMD)
	}
	j := read("journal.md")
	if strings.Index(j, "first") > strings.Index(j, "second") {
		t.Errorf("journal order:\n%s", j)
	}

	// edit the exported file and import it back as a new revision
	edited := strings.Replace(planMD, "- [x] два\n", "- [x] два\n- [ ] три\n", 1)
	f := filepath.Join(dir, "plan.md")
	os.WriteFile(f, []byte(edited), 0o644)
	res, err := Import(ctx, st, ImportParams{Task: task.ID, File: f})
	if err != nil {
		t.Fatal(err)
	}
	if res.Revision != 2 || res.Document.Slug != "plan" || len(res.Items) != 3 || !res.Items[1].Done {
		t.Fatalf("import %+v", res)
	}
	v, _ := st.GetDoc(ctx, task.ID, "plan", store.DocGetOptions{WithRevisions: true})
	if v.Revisions[1].Author != "import" || strings.HasPrefix(v.Revision.Body, "---") {
		t.Fatalf("revision %+v body %q", v.Revisions[1], v.Revision.Body)
	}
}
