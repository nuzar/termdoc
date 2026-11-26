package store

import (
	"path/filepath"
	"testing"

	"github.com/nuzar/termdoc/internal/model"
)

func TestIndexTokensCamelCase(t *testing.T) {
	got := IndexTokens("getElementById")
	want := map[string]bool{"getelementbyid": true, "get": true, "element": true, "by": true, "id": true}
	seen := make(map[string]bool)
	for _, tok := range got {
		seen[tok] = true
	}
	for tok := range want {
		if !seen[tok] {
			t.Errorf("IndexTokens(%q) = %v, missing %q", "getElementById", got, tok)
		}
	}
}

func TestIndexTokensSplitsPunctuation(t *testing.T) {
	got := IndexTokens("fmt.Println()")
	want := map[string]bool{"fmt": true, "println": true}
	seen := make(map[string]bool)
	for _, tok := range got {
		seen[tok] = true
	}
	for tok := range want {
		if !seen[tok] {
			t.Errorf("IndexTokens(%q) = %v, missing %q", "fmt.Println()", got, tok)
		}
	}
}

func TestQueryTokens(t *testing.T) {
	got := QueryTokens("  fmt   Println fmt ")
	want := []string{"fmt", "println"}
	if len(got) != len(want) {
		t.Fatalf("QueryTokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("QueryTokens = %v, want %v", got, want)
		}
	}
}

func TestSearchRanking(t *testing.T) {
	entries := []model.Entry{
		{Name: "fmt.Println", Type: "Function"},
		{Name: "fmt.Printf", Type: "Function"},
		{Name: "Println", Type: "Sample"},
		{Name: "getElementById", Type: "Method"},
	}
	ix := BuildIndex(entries)

	hits := ix.Search(entries, "println", 0)
	if len(hits) < 2 {
		t.Fatalf("expected at least 2 hits, got %v", hits)
	}
	if entries[hits[0].Index].Name != "Println" {
		t.Errorf("first hit = %q, want Println", entries[hits[0].Index].Name)
	}

	hits = ix.Search(entries, "get element", 0)
	if len(hits) == 0 || entries[hits[0].Index].Name != "getElementById" {
		t.Fatalf("camelCase sub-word search failed: %v", hits)
	}

	hits = ix.Search(entries, "printf", 0)
	if len(hits) == 0 || entries[hits[0].Index].Name != "fmt.Printf" {
		t.Fatalf("prefix search failed: %v", hits)
	}
}

func TestIndexSaveLoad(t *testing.T) {
	entries := []model.Entry{
		{Name: "Client", Type: "Class"},
		{Name: "NewClient", Type: "Function"},
	}
	path := filepath.Join(t.TempDir(), "search.gob")
	if err := BuildIndex(entries).Save(path); err != nil {
		t.Fatal(err)
	}
	ix, err := LoadIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	hits := ix.Search(entries, "client", 0)
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits after round-trip, got %v", hits)
	}
}

func TestSanitizeID(t *testing.T) {
	cases := map[string]string{
		"go":           "go",
		"Python 3.13":  "Python 3.13",
		"a/b:c*d":      "a_b_c_d",
		"  trimmed.  ": "trimmed",
		"":             "docset",
		"..":           "docset",
	}
	for in, want := range cases {
		if got := SanitizeID(in); got != want {
			t.Errorf("SanitizeID(%q) = %q, want %q", in, got, want)
		}
	}
}
