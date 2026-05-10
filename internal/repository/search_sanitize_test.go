package repository

import "testing"

func TestSanitizeSearchQuery(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello world", "hello world"},                           // normal
		{"O'Brien", "O Brien"},                                   // apostrophe stripped
		{"\"exact phrase\"", "\"exact phrase\""},                 // quotes NOT stripped (acceptable)
		{"C:\\Users\\rat", "C: Users rat"},                      // backslashes stripped
		{"SELECT * FROM nodes;", "SELECT * FROM nodes"},        // semicolon stripped, no trailing space
		{"-- comment", "comment"},                              // SQL comment stripped, no leading space
		{"/* block */", "block"},                                // block comment stripped, no extra spaces
		{"node.js", "node.js"},                                   // dot preserved
		{"C++", "C++"},                                          // plus preserved
		{"go-patterns", "go-patterns"},                           // hyphen preserved
		{"foo--bar", "foo bar"},                                 // double dash stripped, single space
		{"a/*b*/c", "a b c"},                                    // block comment stripped
	}
	for _, c := range cases {
		got := sanitizeSearchQuery(c.input)
		if got != c.want {
			t.Errorf("sanitizeSearchQuery(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
