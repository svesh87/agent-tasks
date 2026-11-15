// Package plan finds markdown task-list items in a document and matches them
// across revisions so that item ids, ticks and notes survive edits.
package plan

import (
	"regexp"
	"sort"
	"strings"
)

// Item is one task-list line found in a markdown body.
type Item struct {
	Text    string // text after the box, trimmed
	Checked bool   // box as written: [x] or [X]
	Line    int    // 1-based line number in the body
}

var itemRe = regexp.MustCompile(`^\s{0,12}(?:[-*+]|\d{1,9}[.)])\s+\[([ xX])\]\s+(.*\S)\s*$`)
var fenceRe = regexp.MustCompile("^\\s{0,3}(```+|~~~+)")

// Parse returns the task-list items of a markdown body in order. Lines inside
// fenced code blocks are ignored.
func Parse(body string) []Item {
	var items []Item
	var fence string
	for i, line := range strings.Split(body, "\n") {
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			marker := m[1][:3]
			switch {
			case fence == "":
				fence = marker
			case fence == marker:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		m := itemRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		items = append(items, Item{Text: m[2], Checked: m[1] != " ", Line: i + 1})
	}
	return items
}

// SetBoxes rewrites the boxes of task-list lines so they show done[i] for the
// i-th item (in Parse order). Used for export and rendering.
func SetBoxes(body string, done []bool) string {
	lines := strings.Split(body, "\n")
	items := Parse(body)
	for i, it := range items {
		if i >= len(done) {
			break
		}
		l := lines[it.Line-1]
		loc := strings.Index(l, "[")
		if loc < 0 {
			continue
		}
		mark := " "
		if done[i] {
			mark = "x"
		}
		lines[it.Line-1] = l[:loc+1] + mark + l[loc+2:]
	}
	return strings.Join(lines, "\n")
}

// MinSimilarity is the lowest similarity at which an edited item is still
// treated as the same item.
const MinSimilarity = 0.6

// Match pairs items of a new revision with items of the previous one. It
// returns, for every new item, the index of the matched old item or -1.
//
// Pass 1 pairs identical texts (after whitespace and case normalisation) in
// order. Pass 2 pairs the remaining items by text similarity, best pairs
// first, but only when their positions in the lists differ by no more than a
// window of max(3, n/4) places.
func Match(old, new []string) []int {
	res := make([]int, len(new))
	for i := range res {
		res[i] = -1
	}
	usedOld := make([]bool, len(old))

	// Pass 1: exact text, in order of appearance.
	byText := map[string][]int{}
	for j, t := range old {
		k := normalize(t)
		byText[k] = append(byText[k], j)
	}
	for i, t := range new {
		k := normalize(t)
		cands := byText[k]
		if len(cands) == 0 {
			continue
		}
		// prefer the candidate closest in position
		best, bestIdx := -1, -1
		for ci, j := range cands {
			if best < 0 || abs(j-i) < abs(best-i) {
				best, bestIdx = j, ci
			}
		}
		res[i] = best
		usedOld[best] = true
		byText[k] = append(cands[:bestIdx:bestIdx], cands[bestIdx+1:]...)
	}

	// Pass 2: similar text within a position window.
	window := max(3, max(len(old), len(new))/4)
	type pair struct {
		i, j int
		sim  float64
	}
	var pairs []pair
	for i, t := range new {
		if res[i] >= 0 {
			continue
		}
		for j, o := range old {
			if usedOld[j] || abs(i-j) > window {
				continue
			}
			if s := Similarity(t, o); s >= MinSimilarity {
				pairs = append(pairs, pair{i, j, s})
			}
		}
	}
	sort.SliceStable(pairs, func(a, b int) bool {
		if pairs[a].sim != pairs[b].sim {
			return pairs[a].sim > pairs[b].sim
		}
		return abs(pairs[a].i-pairs[a].j) < abs(pairs[b].i-pairs[b].j)
	})
	for _, p := range pairs {
		if res[p.i] >= 0 || usedOld[p.j] {
			continue
		}
		res[p.i] = p.j
		usedOld[p.j] = true
	}
	return res
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// Similarity is 1 - levenshtein/maxlen over normalised runes, in [0, 1].
func Similarity(a, b string) float64 {
	ra, rb := []rune(normalize(a)), []rune(normalize(b))
	if len(ra) == 0 && len(rb) == 0 {
		return 1
	}
	d := Levenshtein(ra, rb)
	return 1 - float64(d)/float64(max(len(ra), len(rb)))
}

// Levenshtein returns the edit distance between two rune slices.
func Levenshtein(a, b []rune) int {
	if len(a) < len(b) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
