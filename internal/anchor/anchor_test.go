package anchor

import (
	"strings"
	"testing"
)

func mk(body, quote string, nth int) Anchor {
	r := []rune(body)
	occ := occurrences(r, []rune(quote))
	return withContext(r, occ[nth], occ[nth]+len([]rune(quote)))
}

func TestResolve(t *testing.T) {
	body := "Шаг: бэкап базы. Потом снова бэкап базы."
	a, ok := Resolve(body, Anchor{Quote: "бэкап базы", Start: 30})
	if !ok || a.Start != 29 {
		t.Fatalf("Resolve = %+v %v", a, ok)
	}
	if !strings.HasSuffix(a.Prefix, "снова ") || a.Suffix != "." {
		t.Errorf("context: %+v", a)
	}
	if _, ok := Resolve(body, Anchor{Quote: "нет такого"}); ok {
		t.Error("missing quote resolved")
	}
}

func TestRelocateExactMoved(t *testing.T) {
	old := "Intro.\n\nUse Traefik for routing.\n"
	a := mk(old, "Traefik", 0)
	nb := "New first paragraph that pushes text down.\n\nIntro.\n\nUse Traefik for routing.\n"
	got, ok := Relocate(old, nb, a)
	if !ok || got.Quote != "Traefik" || string([]rune(nb)[got.Start:got.End]) != "Traefik" {
		t.Fatalf("Relocate = %+v %v", got, ok)
	}
}

func TestRelocatePicksByContext(t *testing.T) {
	old := "alpha: run tests. beta: run tests. gamma: run tests."
	a := mk(old, "run tests", 1) // the beta one
	nb := "gamma: run tests. alpha: run tests. beta: run tests."
	got, ok := Relocate(old, nb, a)
	if !ok {
		t.Fatal("not found")
	}
	if !strings.HasSuffix(got.Prefix, "beta: ") {
		t.Errorf("picked wrong occurrence: %+v", got)
	}
}

func TestRelocateFuzzy(t *testing.T) {
	old := "Plan.\n\nМы настроим ежедневный бэкап базы данных в R2.\n\nКонец."
	a := mk(old, "настроим ежедневный бэкап базы данных", 0)
	nb := "Plan.\n\nМы настроим еженедельный бэкап базы данных в R2.\n\nКонец."
	got, ok := Relocate(old, nb, a)
	if !ok {
		t.Fatal("fuzzy relocation failed")
	}
	if !strings.Contains(got.Quote, "еженедельный бэкап базы данных") {
		t.Errorf("fuzzy quote = %q", got.Quote)
	}
}

func TestRelocateOutdated(t *testing.T) {
	old := "Use Traefik for routing and Caddy for nothing."
	a := mk(old, "Traefik for routing", 0)
	nb := "This paragraph was rewritten completely, nothing remains."
	if got, ok := Relocate(old, nb, a); ok {
		t.Errorf("expected outdated, got %+v", got)
	}
	// short quotes need an exact match
	a2 := mk(old, "Caddy", 0)
	if _, ok := Relocate(old, "Use Cady here", a2); ok {
		t.Error("short quote matched fuzzily")
	}
}

func TestLine(t *testing.T) {
	if Line("a\nb\nc", 4) != 3 || Line("a\nb", 0) != 1 {
		t.Error("Line wrong")
	}
}
