// Package diff computes line diffs (Myers) and formats them as unified diffs.
package diff

import (
	"fmt"
	"strings"
)

// Op is the kind of a diff line.
type Op string

const (
	Equal  Op = " "
	Delete Op = "-"
	Insert Op = "+"
)

// Line is one line of a diff. OldN and NewN are 1-based line numbers, 0 when
// the line does not exist on that side.
type Line struct {
	Op   Op     `json:"op"`
	OldN int    `json:"old_n,omitempty"`
	NewN int    `json:"new_n,omitempty"`
	Text string `json:"text"`
}

// Hunk is a group of changed lines with context.
type Hunk struct {
	OldStart int    `json:"old_start"`
	OldLines int    `json:"old_lines"`
	NewStart int    `json:"new_start"`
	NewLines int    `json:"new_lines"`
	Lines    []Line `json:"lines"`
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// Lines returns the full line-by-line diff of a and b.
func Lines(a, b string) []Line {
	al, bl := splitLines(a), splitLines(b)
	ops := myers(al, bl)
	out := make([]Line, 0, len(ops))
	i, j := 0, 0
	for _, op := range ops {
		switch op {
		case Equal:
			out = append(out, Line{Op: Equal, OldN: i + 1, NewN: j + 1, Text: al[i]})
			i++
			j++
		case Delete:
			out = append(out, Line{Op: Delete, OldN: i + 1, Text: al[i]})
			i++
		case Insert:
			out = append(out, Line{Op: Insert, NewN: j + 1, Text: bl[j]})
			j++
		}
	}
	return out
}

// maxEdit bounds the Myers search; beyond it the changed middle is shown as
// removed and re-added, which keeps memory bounded for complete rewrites.
const maxEdit = 1500

// myers returns the edit script turning a into b, one op per line. Common
// leading and trailing lines are stripped first.
func myers(a, b []string) []Op {
	var head, tail int
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	ops := make([]Op, 0, len(a)+len(b))
	for range head {
		ops = append(ops, Equal)
	}
	ops = append(ops, myersCore(a[head:len(a)-tail], b[head:len(b)-tail])...)
	for range tail {
		ops = append(ops, Equal)
	}
	return ops
}

func myersCore(a, b []string) []Op {
	n, m := len(a), len(b)
	maxD := n + m
	if maxD == 0 {
		return nil
	}
	if maxD > maxEdit {
		maxD = maxEdit
	}
	off := maxD + 1
	v := make([]int, 2*maxD+4)
	var trace [][]int // trace[d] holds v[-d..d] as it was before step d
	found := false
	for d := 0; d <= maxD && !found; d++ {
		snap := make([]int, 2*d+1)
		copy(snap, v[off-d:off+d+1])
		trace = append(trace, snap)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
	}
	if !found {
		ops := make([]Op, 0, n+m)
		for range n {
			ops = append(ops, Delete)
		}
		for range m {
			ops = append(ops, Insert)
		}
		return ops
	}
	// backtrack
	var ops []Op
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		vv := trace[d]
		at := func(k int) int { return vv[k+d] }
		k := x - y
		var prevK int
		if k == -d || (k != d && at(k-1) < at(k+1)) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := 0
		if d > 0 {
			prevX = at(prevK)
		}
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			ops = append(ops, Equal)
			x--
			y--
		}
		if d > 0 {
			if x == prevX {
				ops = append(ops, Insert)
				y--
			} else {
				ops = append(ops, Delete)
				x--
			}
		}
	}
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
	return ops
}

// Hunks groups the diff of a and b into hunks with ctx lines of context.
func Hunks(a, b string, ctx int) []Hunk {
	lines := Lines(a, b)
	var hunks []Hunk
	i := 0
	for i < len(lines) {
		if lines[i].Op == Equal {
			i++
			continue
		}
		start := max(0, i-ctx)
		end := i
		// extend while changes are within 2*ctx of each other
		for end < len(lines) {
			if lines[end].Op != Equal {
				end++
				continue
			}
			run := end
			for run < len(lines) && lines[run].Op == Equal {
				run++
			}
			if run < len(lines) && run-end <= 2*ctx {
				end = run
				continue
			}
			end = min(len(lines), end+ctx)
			break
		}
		h := Hunk{Lines: lines[start:end]}
		for _, l := range h.Lines {
			if l.Op != Insert {
				if h.OldStart == 0 {
					h.OldStart = l.OldN
				}
				h.OldLines++
			}
			if l.Op != Delete {
				if h.NewStart == 0 {
					h.NewStart = l.NewN
				}
				h.NewLines++
			}
		}
		if h.OldLines == 0 {
			h.OldStart = prevOld(lines, start)
		}
		if h.NewLines == 0 {
			h.NewStart = prevNew(lines, start)
		}
		hunks = append(hunks, h)
		i = end
	}
	return hunks
}

func prevOld(lines []Line, i int) int {
	for i--; i >= 0; i-- {
		if lines[i].OldN > 0 {
			return lines[i].OldN
		}
	}
	return 0
}

func prevNew(lines []Line, i int) int {
	for i--; i >= 0; i-- {
		if lines[i].NewN > 0 {
			return lines[i].NewN
		}
	}
	return 0
}

// Unified formats the diff of a and b as a unified diff with 3 lines of
// context. It returns "" when the texts are equal.
func Unified(aName, bName, a, b string) string {
	hunks := Hunks(a, b, 3)
	if len(hunks) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", aName, bName)
	for _, h := range hunks {
		fmt.Fprintf(&sb, "@@ -%s +%s @@\n", rng(h.OldStart, h.OldLines), rng(h.NewStart, h.NewLines))
		for _, l := range h.Lines {
			sb.WriteString(string(l.Op))
			sb.WriteString(l.Text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func rng(start, n int) string {
	if n == 1 {
		return fmt.Sprint(start)
	}
	return fmt.Sprintf("%d,%d", start, n)
}
