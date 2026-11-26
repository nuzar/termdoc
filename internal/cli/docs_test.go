package cli

import "testing"

func TestMatchRank(t *testing.T) {
	cases := []struct {
		name, title, query string
		want               int
	}{
		{"go", "Go", "go", 0},
		{"django~5.0", "Django 5.0", "django", 1},
		{"django~5.0", "Django 5.0", "jango", 2},
		{"django~5.0", "Django 5.0", "rust", -1},
		{"anything", "Anything", "", 0},
	}
	for _, c := range cases {
		if got := matchRank(c.name, c.title, c.query); got != c.want {
			t.Errorf("matchRank(%q, %q, %q) = %d, want %d", c.name, c.title, c.query, got, c.want)
		}
	}
}
