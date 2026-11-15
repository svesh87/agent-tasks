package main

import "testing"

func TestParseTokens(t *testing.T) {
	got := parseTokens("laptop=abc, def\n# comment\n\nrunner = ghi\n")
	if len(got) != 3 || got[0].Name != "laptop" || got[0].Secret != "abc" || got[1].Secret != "def" || got[2].Name != "runner" || got[2].Secret != "ghi" {
		t.Fatalf("parseTokens = %+v", got)
	}
}
