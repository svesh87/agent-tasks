package textproc

import (
	"strings"
	"testing"
)

func TestStemRussianForms(t *testing.T) {
	forms := []string{"задача", "задачи", "задачу", "задачей", "задачами"}
	want := Stem("задача")
	for _, f := range forms {
		if got := Stem(f); got != want {
			t.Errorf("Stem(%q) = %q, want %q", f, got, want)
		}
	}
	if Stem("ёлка") != Stem("елка") && Stems("Ёлка")[0] != Stem("елка") {
		t.Errorf("ё is not folded")
	}
}

func TestStemEnglish(t *testing.T) {
	if Stem("running") != Stem("run") {
		t.Errorf("running vs run: %q %q", Stem("running"), Stem("run"))
	}
}

func TestMatchQuery(t *testing.T) {
	got := MatchQuery(`Задачи "deploy"`)
	if !strings.Contains(got, `"задач"*`) || !strings.Contains(got, `"deploy"*`) || !strings.Contains(got, " AND ") {
		t.Errorf("MatchQuery = %s", got)
	}
	if MatchQuery("  ,, ") != "" {
		t.Errorf("empty query should give empty match")
	}
}

func TestSnippet(t *testing.T) {
	text := strings.Repeat("слово ", 50) + "Тут описана задача про бэкап. " + strings.Repeat("ещё ", 50)
	sn, ok := MakeSnippet(text, "задачи", 80)
	if !ok {
		t.Fatal("no snippet")
	}
	if !strings.Contains(sn.Marked(), "**задача**") {
		t.Errorf("snippet = %q", sn.Marked())
	}
	if _, ok := MakeSnippet(text, "кошка", 80); ok {
		t.Errorf("unexpected hit")
	}
}
