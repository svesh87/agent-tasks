// Package anchor locates a quoted text range of one revision in another
// revision (comment re-anchoring). All offsets are in Unicode code points
// (runes) of the markdown body.
package anchor

import (
	"strings"
	"unicode/utf8"
)

// ContextLen is how many runes of prefix and suffix an anchor keeps.
const ContextLen = 32

// Anchor is a text range in a markdown body.
type Anchor struct {
	Quote  string `json:"quote"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
}

// MinFuzzyLen is the shortest quote that may be matched approximately;
// shorter quotes must reappear exactly.
const MinFuzzyLen = 8

// MaxFuzzyRatio is the largest edit distance, relative to the quote length,
// accepted by the fuzzy pass.
const MaxFuzzyRatio = 0.25

// Resolve finds a in body as given (for a new comment): it prefers the exact
// range [Start, End) when it holds the quote, else the occurrence of the
// quote closest to Start. Prefix and suffix are recomputed from body.
func Resolve(body string, a Anchor) (Anchor, bool) {
	r := []rune(body)
	q := []rune(a.Quote)
	if len(q) == 0 {
		return Anchor{}, false
	}
	if a.Start >= 0 && a.End == a.Start+len(q) && a.End <= len(r) && string(r[a.Start:a.End]) == a.Quote {
		return withContext(r, a.Start, a.End), true
	}
	occ := occurrences(r, q)
	if len(occ) == 0 {
		return Anchor{}, false
	}
	best := occ[0]
	for _, o := range occ[1:] {
		if abs(o-a.Start) < abs(best-a.Start) {
			best = o
		}
	}
	return withContext(r, best, best+len(q)), true
}

// Relocate finds the anchor a, taken from oldBody, in newBody.
//
//  1. Exact quote: every occurrence is scored by how much of the prefix and
//     suffix agree and by its distance from the expected position; the best
//     wins.
//  2. Fuzzy: for quotes of at least MinFuzzyLen runes, the substring of
//     newBody near the expected position with the smallest edit distance to
//     the quote, if that distance is at most MaxFuzzyRatio of the quote.
//  3. Otherwise ok is false: the comment is outdated.
//
// The expected position is the old start scaled by the length change.
func Relocate(oldBody, newBody string, a Anchor) (Anchor, bool) {
	nr := []rune(newBody)
	q := []rune(a.Quote)
	if len(q) == 0 {
		return Anchor{}, false
	}
	oldLen := len([]rune(oldBody))
	expected := a.Start
	if oldLen > 0 {
		expected = int(float64(a.Start) * float64(len(nr)) / float64(oldLen))
	}

	if occ := occurrences(nr, q); len(occ) > 0 {
		best, bestScore := -1, -1.0
		for _, o := range occ {
			s := contextScore(nr, o, o+len(q), a) - float64(abs(o-expected))/float64(len(nr)+1)
			if s > bestScore {
				best, bestScore = o, s
			}
		}
		return withContext(nr, best, best+len(q)), true
	}

	if len(q) < MinFuzzyLen {
		return Anchor{}, false
	}
	radius := max(3000, 3*len(q))
	from := max(0, expected-radius)
	to := min(len(nr), expected+len(q)+radius)
	start, end, dist := approxSearch(nr[from:to], q)
	if start < 0 || float64(dist) > MaxFuzzyRatio*float64(len(q)) {
		return Anchor{}, false
	}
	return withContext(nr, from+start, from+end), true
}

func withContext(r []rune, start, end int) Anchor {
	ps := max(0, start-ContextLen)
	se := min(len(r), end+ContextLen)
	return Anchor{
		Quote:  string(r[start:end]),
		Prefix: string(r[ps:start]),
		Suffix: string(r[end:se]),
		Start:  start,
		End:    end,
	}
}

// contextScore is in [0, 2]: the share of the prefix (matched from its end)
// and of the suffix (matched from its start) found around [start, end).
func contextScore(r []rune, start, end int, a Anchor) float64 {
	score := 0.0
	if p := []rune(a.Prefix); len(p) > 0 {
		n := 0
		for n < len(p) && start-1-n >= 0 && r[start-1-n] == p[len(p)-1-n] {
			n++
		}
		score += float64(n) / float64(len(p))
	}
	if s := []rune(a.Suffix); len(s) > 0 {
		n := 0
		for n < len(s) && end+n < len(r) && r[end+n] == s[n] {
			n++
		}
		score += float64(n) / float64(len(s))
	}
	return score
}

func occurrences(r, q []rune) []int {
	hay, needle := string(r), string(q)
	var out []int
	runeOff, byteOff := 0, 0
	for {
		i := strings.Index(hay[byteOff:], needle)
		if i < 0 {
			return out
		}
		runeOff += len([]rune(hay[byteOff : byteOff+i]))
		out = append(out, runeOff)
		// advance by one rune past the match start
		_, size := utf8.DecodeRuneInString(hay[byteOff+i:])
		byteOff += i + size
		runeOff++
	}
}

// approxSearch finds the substring of text with the smallest Levenshtein
// distance to pattern (Sellers' algorithm) and returns its rune range. For
// equal distances the substring closest in length to the pattern wins. Long patterns are compared by their
// first 400 runes to bound the cost.
func approxSearch(text, pattern []rune) (start, end, dist int) {
	m := len(pattern)
	if m == 0 || len(text) == 0 {
		return -1, -1, 0
	}
	if m > 400 {
		pattern = pattern[:400]
		m = 400
	}
	// D[i] = distance of pattern[:i] against the best substring ending at
	// the current text position; S[i] = where that substring starts.
	D := make([]int, m+1)
	S := make([]int, m+1)
	nD := make([]int, m+1)
	nS := make([]int, m+1)
	for i := range D {
		D[i] = i
		S[i] = 0
	}
	bestDist, bestStart, bestEnd := m+1, -1, -1
	for j := 1; j <= len(text); j++ {
		nD[0], nS[0] = 0, j
		for i := 1; i <= m; i++ {
			cost := 1
			if pattern[i-1] == text[j-1] {
				cost = 0
			}
			// substitution / match
			d, s := D[i-1]+cost, S[i-1]
			// deletion from pattern (skip pattern char)
			if v := nD[i-1] + 1; v < d {
				d, s = v, nS[i-1]
			}
			// insertion (extra text char)
			if v := D[i] + 1; v < d {
				d, s = v, S[i]
			}
			nD[i], nS[i] = d, s
		}
		// Smaller distance wins; on a tie, the length closer to the pattern.
		if nD[m] < bestDist || (nD[m] == bestDist && abs(j-nS[m]-m) < abs(bestEnd-bestStart-m)) {
			bestDist, bestStart, bestEnd = nD[m], nS[m], j
		}
		D, nD = nD, D
		S, nS = nS, S
	}
	return bestStart, bestEnd, bestDist
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Line returns the 1-based line number of rune offset off in body.
func Line(body string, off int) int {
	n := 1
	i := 0
	for _, r := range body {
		if i >= off {
			break
		}
		if r == '\n' {
			n++
		}
		i++
	}
	return n
}
