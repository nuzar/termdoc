package store

import (
	"encoding/gob"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/nuzar/termdoc/internal/model"
)

// Index is a compact inverted index over entry names and types. It is built
// once at import time and loaded when searching.
type Index struct {
	// Tokens is sorted; Name/Type postings are parallel slices of entry IDs.
	Tokens     []string
	Name       [][]uint32
	typeTokens []string
	Type       [][]uint32
}

// BuildIndex indexes the searchable parts of entries.
func BuildIndex(entries []model.Entry) *Index {
	nameMap := make(map[string][]uint32)
	typeMap := make(map[string][]uint32)

	for i, e := range entries {
		id := uint32(i)
		seen := make(map[string]bool)
		for _, tok := range IndexTokens(e.Name) {
			if seen[tok] {
				continue
			}
			seen[tok] = true
			nameMap[tok] = append(nameMap[tok], id)
		}
		seen = make(map[string]bool)
		for _, tok := range IndexTokens(e.Type) {
			if seen[tok] {
				continue
			}
			seen[tok] = true
			typeMap[tok] = append(typeMap[tok], id)
		}
	}

	ix := &Index{}
	keys := make([]string, 0, len(nameMap))
	for k := range nameMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ix.Tokens = keys
	ix.Name = make([][]uint32, len(keys))
	for i, k := range keys {
		ix.Name[i] = nameMap[k]
	}

	tkeys := make([]string, 0, len(typeMap))
	for k := range typeMap {
		tkeys = append(tkeys, k)
	}
	sort.Strings(tkeys)
	ix.Type = make([][]uint32, len(tkeys))
	for i, k := range tkeys {
		ix.Type[i] = typeMap[k]
	}
	// Type tokens are looked up in a separate sorted list stored in the same
	// Tokens slice? Keep them separate for clarity: encode as a parallel
	// structure by appending with a marker is messy, so store TypeTokens too.
	ix.typeTokens = tkeys
	return ix
}

// typeTokens is not serialized directly; see gobEncode/gobDecode below.
type indexGob struct {
	Tokens     []string
	Name       [][]uint32
	TypeTokens []string
	Type       [][]uint32
}

func (ix *Index) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := gob.NewEncoder(f)
	err = enc.Encode(indexGob{
		Tokens:     ix.Tokens,
		Name:       ix.Name,
		TypeTokens: ix.typeTokens,
		Type:       ix.Type,
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func LoadIndex(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var g indexGob
	if err := gob.NewDecoder(f).Decode(&g); err != nil {
		return nil, fmt.Errorf("decode search index: %w", err)
	}
	ix := &Index{Tokens: g.Tokens, Name: g.Name, Type: g.Type, typeTokens: g.TypeTokens}
	return ix, nil
}

// Hit is a scored entry.
type Hit struct {
	Index int
	Score int
}

// Search returns ranked entry indexes. Multi-word queries are AND by default,
// falling back to OR when nothing matches.
func (ix *Index) Search(entries []model.Entry, query string, limit int) []Hit {
	qt := QueryTokens(query)
	if len(qt) == 0 {
		return nil
	}
	scores := make(map[int]int)
	matched := make(map[int]int)

	for _, tok := range qt {
		seen := make(map[int]bool)
		add := func(ids []uint32, weight int) {
			for _, id := range ids {
				scores[int(id)] += weight
				seen[int(id)] = true
			}
		}
		if ids := lookup(ix.Tokens, ix.Name, tok); len(ids) > 0 {
			add(ids, 20)
		}
		if len([]rune(tok)) >= 2 {
			ix.prefix(ix.Tokens, ix.Name, tok, func(ids []uint32) { add(ids, 8) })
		}
		if ids := lookup(ix.typeTokens, ix.Type, tok); len(ids) > 0 {
			add(ids, 2)
		}
		if len([]rune(tok)) >= 2 {
			ix.prefix(ix.typeTokens, ix.Type, tok, func(ids []uint32) { add(ids, 1) })
		}
		for id := range seen {
			matched[id]++
		}
	}

	qraw := strings.ToLower(strings.TrimSpace(query))
	qcompact := strings.Join(qt, "")
	for i, e := range entries {
		name := strings.ToLower(e.Name)
		name = strings.TrimSuffix(name, "()")
		switch {
		case name == qraw || name == qcompact:
			scores[i] += 1000
			matched[i] = len(qt)
		case qraw != "" && strings.Contains(name, qraw):
			scores[i] += 120
		}
	}

	hits := make([]Hit, 0, len(scores))
	for i, sc := range scores {
		if len(qt) > 1 && matched[i] < len(qt) {
			continue
		}
		hits = append(hits, Hit{Index: i, Score: sc})
	}
	if len(hits) == 0 && len(qt) > 1 {
		for i, sc := range scores {
			hits = append(hits, Hit{Index: i, Score: sc})
		}
	}

	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Score != hits[b].Score {
			return hits[a].Score > hits[b].Score
		}
		na, nb := entries[hits[a].Index].Name, entries[hits[b].Index].Name
		if len(na) != len(nb) {
			return len(na) < len(nb)
		}
		return na < nb
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func lookup(tokens []string, postings [][]uint32, tok string) []uint32 {
	i := sort.SearchStrings(tokens, tok)
	if i < len(tokens) && tokens[i] == tok {
		return postings[i]
	}
	return nil
}

func (ix *Index) prefix(tokens []string, postings [][]uint32, tok string, fn func([]uint32)) {
	i := sort.SearchStrings(tokens, tok)
	if i < len(tokens) && tokens[i] == tok {
		i++ // exact match already scored
	}
	n := 0
	for ; i < len(tokens) && strings.HasPrefix(tokens[i], tok); i++ {
		fn(postings[i])
		n++
		if n >= 300 {
			return
		}
	}
}

// IndexTokens returns the lowercase tokens stored in the index for s.
// Identifiers are also split at camelCase boundaries.
func IndexTokens(s string) []string {
	var out []string
	for _, w := range splitWords(s) {
		if w == "" {
			continue
		}
		lower := strings.ToLower(w)
		out = append(out, lower)
		// Split camelCase on the original spelling; the lowercase form has no
		// case boundaries left to detect.
		for _, sub := range splitCamel(w) {
			sub = strings.ToLower(sub)
			if len([]rune(sub)) >= 2 && sub != lower {
				out = append(out, sub)
			}
		}
	}
	return out
}

// QueryTokens splits a user query into lowercase terms.
func QueryTokens(s string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, w := range splitWords(strings.ToLower(s)) {
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '+'
	})
}

func splitCamel(s string) []string {
	r := []rune(s)
	if len(r) < 3 {
		return nil
	}
	var out []string
	start := 0
	for i := 1; i < len(r); i++ {
		prev, cur := r[i-1], r[i]
		boundary := false
		if unicode.IsLower(prev) && unicode.IsUpper(cur) {
			boundary = true
		}
		if unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(r) && unicode.IsLower(r[i+1]) {
			boundary = true
		}
		if boundary {
			out = append(out, string(r[start:i]))
			start = i
		}
	}
	if start > 0 {
		out = append(out, string(r[start:]))
	}
	return out
}
