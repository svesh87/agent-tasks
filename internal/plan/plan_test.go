package plan

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	body := "# Plan\n\n- [ ] first\n- [x] second\n  * [X] nested\n1. [ ] numbered\n```\n- [ ] in code\n```\n- [] not an item\n- [ ]\n"
	items := Parse(body)
	var got []string
	for _, it := range items {
		got = append(got, it.Text)
	}
	want := []string{"first", "second", "nested", "numbered"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse = %v, want %v", got, want)
	}
	if items[0].Checked || !items[1].Checked || !items[2].Checked || items[0].Line != 3 {
		t.Errorf("unexpected items %+v", items)
	}
}

func TestSetBoxes(t *testing.T) {
	body := "- [ ] a\ntext [ ] stays\n- [x] b\n"
	got := SetBoxes(body, []bool{true, false})
	want := "- [x] a\ntext [ ] stays\n- [ ] b\n"
	if got != want {
		t.Errorf("SetBoxes = %q", got)
	}
}

func TestMatch(t *testing.T) {
	old := []string{"Write the plan", "Set up DNS record for tasks", "Deploy", "Write docs"}
	new := []string{
		"Write the plan",                       // exact -> 0
		"Add backups",                          // new
		"Set up the DNS record for tasks host", // similar -> 1
		"Deploy",                               // exact -> 2
	}
	got := Match(old, new)
	want := []int{0, -1, 1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Match = %v, want %v", got, want)
	}
}

func TestMatchDuplicatesAndWindow(t *testing.T) {
	old := []string{"check", "a", "b", "c", "d", "e", "f", "g", "h", "check"}
	new := []string{"check", "check"}
	got := Match(old, new)
	if got[0] != 0 || got[1] != 9 {
		t.Errorf("duplicates matched as %v", got)
	}
	// Similar text far away is not matched.
	old2 := []string{"x1", "x2", "x3", "x4", "x5", "x6", "x7", "x8", "Configure the firewall rules"}
	new2 := []string{"Configure the firewall rule"}
	if m := Match(old2, new2); m[0] != -1 {
		t.Errorf("far item matched: %v", m)
	}
}

func TestSimilarity(t *testing.T) {
	if s := Similarity("Настроить бэкап", "настроить  бэкапы"); s < MinSimilarity {
		t.Errorf("similarity too low: %v", s)
	}
	if s := Similarity("Настроить бэкап", "Удалить старые диски"); s >= MinSimilarity {
		t.Errorf("similarity too high: %v", s)
	}
}
