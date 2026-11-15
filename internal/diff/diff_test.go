package diff

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func apply(lines []Line) (string, string) {
	var a, b []string
	for _, l := range lines {
		if l.Op != Insert {
			a = append(a, l.Text)
		}
		if l.Op != Delete {
			b = append(b, l.Text)
		}
	}
	return strings.Join(a, "\n"), strings.Join(b, "\n")
}

func TestLinesRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	words := []string{"a", "b", "c", "d"}
	gen := func() string {
		n := r.Intn(30)
		var ls []string
		for range n {
			ls = append(ls, words[r.Intn(len(words))])
		}
		return strings.Join(ls, "\n")
	}
	for i := range 500 {
		a, b := gen(), gen()
		ga, gb := apply(Lines(a, b))
		if ga != a || gb != b {
			t.Fatalf("case %d: round trip failed\na=%q\nb=%q", i, a, b)
		}
	}
}

func TestUnified(t *testing.T) {
	a := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	b := "one\ntwo\nTHREE\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\n"
	got := Unified("plan r1", "plan r2", a, b)
	want := "--- plan r1\n+++ plan r2\n" +
		"@@ -1,6 +1,6 @@\n one\n two\n-three\n+THREE\n four\n five\n six\n" +
		"@@ -8,3 +8,4 @@\n eight\n nine\n ten\n+eleven\n"
	if got != want {
		t.Errorf("Unified:\n%s\nwant:\n%s", got, want)
	}
	if Unified("a", "b", a, a) != "" {
		t.Error("equal texts should give empty diff")
	}
}

func TestLargeRewriteBounded(t *testing.T) {
	var a, b []string
	for i := range 3000 {
		a = append(a, fmt.Sprintf("old %d", i))
		b = append(b, fmt.Sprintf("new %d", i))
	}
	ga, gb := apply(Lines(strings.Join(a, "\n"), strings.Join(b, "\n")))
	if ga != strings.Join(a, "\n") || gb != strings.Join(b, "\n") {
		t.Fatal("fallback diff is wrong")
	}
}
