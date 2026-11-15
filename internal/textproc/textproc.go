// Package textproc splits text into words and stems them for the full-text
// index. SQLite FTS5 only sees stems produced here, so the same function must
// be used for documents and for queries.
package textproc

import (
	"strings"
	"unicode"

	"github.com/kljensen/snowball/english"
	"github.com/kljensen/snowball/russian"
)

// Token is a word found in a text, with rune offsets [Start, End).
type Token struct {
	Word  string // lower-cased, ё folded to е
	Start int
	End   int
}

// Tokenize returns the words of s. A word is a run of letters and digits;
// apostrophes and hyphens inside a word split it.
func Tokenize(s string) []Token {
	var out []Token
	runes := []rune(s)
	start := -1
	for i := 0; i <= len(runes); i++ {
		inWord := i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]))
		if inWord && start < 0 {
			start = i
		}
		if !inWord && start >= 0 {
			out = append(out, Token{Word: fold(string(runes[start:i])), Start: start, End: i})
			start = -1
		}
	}
	return out
}

func fold(w string) string {
	w = strings.ToLower(w)
	return strings.ReplaceAll(w, "ё", "е")
}

// Stem returns the stem of a single lower-cased word: Russian Snowball for
// words with Cyrillic letters, English Snowball for Latin ones, the word as is
// otherwise (numbers, other scripts). Stop words are stemmed too, so that a
// query never silently drops a word.
func Stem(word string) string {
	switch script(word) {
	case cyrillic:
		return russian.Stem(word, true)
	case latin:
		return english.Stem(word, true)
	}
	return word
}

type scriptKind int

const (
	other scriptKind = iota
	latin
	cyrillic
)

func script(w string) scriptKind {
	for _, r := range w {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			return cyrillic
		case r < unicode.MaxASCII && unicode.IsLetter(r):
			return latin
		}
	}
	return other
}

// Stems returns the stems of all words in s, in order.
func Stems(s string) []string {
	toks := Tokenize(s)
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = Stem(t.Word)
	}
	return out
}

// IndexText is the text stored in the FTS5 column: stems joined by spaces.
func IndexText(s string) string {
	return strings.Join(Stems(s), " ")
}

// MatchQuery turns a user query into an FTS5 MATCH expression: every stem
// must be present, each matched as a prefix so that partial words still find
// something. It returns "" when the query has no words.
func MatchQuery(q string) string {
	stems := Stems(q)
	seen := map[string]bool{}
	var parts []string
	for _, s := range stems {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		parts = append(parts, `"`+strings.ReplaceAll(s, `"`, `""`)+`"*`)
	}
	return strings.Join(parts, " AND ")
}

// Snippet is a fragment of a text around the first query hit.
type Snippet struct {
	Text       string   `json:"text"`
	Highlights [][2]int `json:"highlights"` // rune offsets within Text
	// At and AtEnd are the rune range of the first hit in the whole source text.
	At    int `json:"-"`
	AtEnd int `json:"-"`
}

// Marked returns the snippet text with hits wrapped in ** for plain-text use.
func (s Snippet) Marked() string {
	if len(s.Highlights) == 0 {
		return s.Text
	}
	r := []rune(s.Text)
	var b strings.Builder
	prev := 0
	for _, h := range s.Highlights {
		b.WriteString(string(r[prev:h[0]]))
		b.WriteString("**")
		b.WriteString(string(r[h[0]:h[1]]))
		b.WriteString("**")
		prev = h[1]
	}
	b.WriteString(string(r[prev:]))
	return b.String()
}

// MakeSnippet finds words of text whose stems start with one of the query
// stems and returns about width runes around the first hit. ok is false when
// nothing matches.
func MakeSnippet(text, query string, width int) (Snippet, bool) {
	qs := Stems(query)
	if len(qs) == 0 {
		return Snippet{}, false
	}
	toks := Tokenize(text)
	var hits []Token
	for _, t := range toks {
		st := Stem(t.Word)
		for _, q := range qs {
			if strings.HasPrefix(st, q) || strings.HasPrefix(t.Word, q) {
				hits = append(hits, t)
				break
			}
		}
	}
	if len(hits) == 0 {
		return Snippet{}, false
	}
	runes := []rune(text)
	from := max(hits[0].Start-width/3, 0)
	to := min(from+width, len(runes))
	// snap to word boundaries
	for from > 0 && !unicode.IsSpace(runes[from-1]) && hits[0].Start-from < width/2 {
		from--
	}
	for to < len(runes) && !unicode.IsSpace(runes[to]) && to-from < width+20 {
		to++
	}
	prefix, suffix := "", ""
	if from > 0 {
		prefix = "…"
	}
	if to < len(runes) {
		suffix = "…"
	}
	body := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, string(runes[from:to]))
	sn := Snippet{Text: prefix + body + suffix, At: hits[0].Start, AtEnd: hits[0].End}
	off := len([]rune(prefix))
	for _, h := range hits {
		if h.Start >= from && h.End <= to {
			sn.Highlights = append(sn.Highlights, [2]int{h.Start - from + off, h.End - from + off})
		}
	}
	return sn, true
}
