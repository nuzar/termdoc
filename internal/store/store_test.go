package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nuzar/termdoc/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func buildTestDocset(t *testing.T, st *Store, meta model.Meta, force bool) model.Meta {
	t.Helper()
	b, err := st.NewBuilder(meta, force)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddPage("index.html", []byte("<html><body><h1>Hello</h1><p>World</p></body></html>")); err != nil {
		b.Abort()
		t.Fatal(err)
	}
	b.AddEntry(model.Entry{Name: "Hello", Type: "Function", Page: "index.html"})
	b.AddEntry(model.Entry{Name: "World", Type: "Variable", Page: "index.html"})
	m, err := b.Commit()
	if err != nil {
		t.Fatal(err)
	}
	return *m
}

func TestBuilderCommitAndLoad(t *testing.T) {
	st := newTestStore(t)
	m := buildTestDocset(t, st, model.Meta{ID: "test", Title: "Test", Source: model.SourceDevDocs}, false)
	if m.Entries != 2 {
		t.Errorf("entries = %d, want 2", m.Entries)
	}
	if m.Pages != 1 {
		t.Errorf("pages = %d, want 1", m.Pages)
	}

	d, err := st.Load("test")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if len(d.Entries) != 2 {
		t.Fatalf("loaded %d entries, want 2", len(d.Entries))
	}
	hits := d.Search("hello", 0)
	if len(hits) != 1 || d.Entries[hits[0].Index].Name != "Hello" {
		t.Fatalf("unexpected search hits: %v", hits)
	}
	page, err := d.Page("index.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(page) == 0 {
		t.Error("page is empty")
	}
	if found := d.FindEntry("hello"); len(found) != 1 {
		t.Errorf("FindEntry(hello) = %v", found)
	}
}

func TestBuilderRefusesOverwriteWithoutForce(t *testing.T) {
	st := newTestStore(t)
	buildTestDocset(t, st, model.Meta{ID: "test", Title: "Test"}, false)

	b, err := st.NewBuilder(model.Meta{ID: "test", Title: "Test 2"}, false)
	if err != nil {
		t.Fatal(err)
	}
	b.AddEntry(model.Entry{Name: "X", Page: "index.html"})
	if _, err := b.Commit(); err == nil {
		t.Fatal("expected an error when replacing a docset without force")
	}

	m := buildTestDocset(t, st, model.Meta{ID: "test", Title: "Test 2"}, true)
	if m.Title != "Test 2" {
		t.Errorf("title = %q, want Test 2", m.Title)
	}
}

func TestListAndResolve(t *testing.T) {
	st := newTestStore(t)
	buildTestDocset(t, st, model.Meta{ID: "go", Title: "Go"}, false)
	buildTestDocset(t, st, model.Meta{ID: "dash-Go", Title: "Go (Dash)"}, false)
	buildTestDocset(t, st, model.Meta{ID: "ruby~3.13", Title: "Ruby 3.13"}, false)

	all, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("List returned %d docsets, want 3", len(all))
	}

	if m, err := st.ResolveOne("go"); err != nil || m.ID != "go" {
		t.Errorf("ResolveOne(go) = %v, %v", m.ID, err)
	}
	if ms, err := st.Resolve("o"); err != nil || len(ms) != 2 {
		t.Errorf("Resolve(o) = %v, %v", ms, err)
	}
	if _, err := st.ResolveOne("o"); err == nil {
		t.Error("expected ambiguous error for ResolveOne(o)")
	}
	if ms, err := st.Resolve("3.13"); err != nil || len(ms) != 1 || ms[0].ID != "ruby~3.13" {
		t.Errorf("Resolve(3.13) = %v, %v", ms, err)
	}
	if _, err := st.Resolve("nope"); err == nil {
		t.Error("expected an error for an unknown docset")
	}
}

func TestRemove(t *testing.T) {
	st := newTestStore(t)
	buildTestDocset(t, st, model.Meta{ID: "test", Title: "Test"}, false)
	if err := st.Remove("test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st.Dir("test")); !os.IsNotExist(err) {
		t.Error("docset directory still exists after Remove")
	}
	if err := st.Remove("test"); err == nil {
		t.Error("expected an error removing a missing docset")
	}
}

func TestInstallDir(t *testing.T) {
	src := newTestStore(t)
	buildTestDocset(t, src, model.Meta{ID: "test", Title: "Test"}, false)

	dst := newTestStore(t)
	m, err := dst.InstallDir(src.Dir("test"), false)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "test" {
		t.Errorf("ID = %q, want test", m.ID)
	}
	if _, err := dst.Load("test"); err != nil {
		t.Errorf("installed docset cannot be loaded: %v", err)
	}
	if _, err := dst.InstallDir(src.Dir("test"), false); err == nil {
		t.Error("expected an error installing twice without force")
	}
}

func TestInstallDirRejectsBadFormat(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "meta.json"), `{"format":99,"id":"x","title":"X"}`)
	for _, name := range []string{"entries.json", "search.gob", "pages.zip"} {
		writeFile(t, filepath.Join(dir, name), "x")
	}
	st := newTestStore(t)
	if _, err := st.InstallDir(dir, false); err == nil {
		t.Error("expected an unsupported-format error")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
