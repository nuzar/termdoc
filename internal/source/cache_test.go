package source

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nuzar/termdoc/internal/model"
	"github.com/nuzar/termdoc/internal/store"
)

func TestValidArchive(t *testing.T) {
	dir := t.TempDir()
	gzipFile := filepath.Join(dir, "a.tgz")
	if err := os.WriteFile(gzipFile, []byte{0x1f, 0x8b, 0x08, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if !validArchive(gzipFile) {
		t.Error("gzip file not recognized")
	}

	zipFile := filepath.Join(dir, "b.zip")
	if err := os.WriteFile(zipFile, []byte{'P', 'K', 0x03, 0x04}, 0o644); err != nil {
		t.Fatal(err)
	}
	if !validArchive(zipFile) {
		t.Error("zip file not recognized")
	}

	htmlFile := filepath.Join(dir, "c.tgz")
	if err := os.WriteFile(htmlFile, []byte("\n\n<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if validArchive(htmlFile) {
		t.Error("HTML file recognized as an archive")
	}
}

func TestValidJSONObject(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.json")
	if err := os.WriteFile(ok, []byte("  \n{\"a\":1}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !validJSONObject(ok) {
		t.Error("JSON object not recognized")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("\n<html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if validJSONObject(bad) {
		t.Error("HTML file recognized as JSON")
	}
}

func TestReplaceCacheOverwrites(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "cache.tgz")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "new.tgz")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceCache(src, dst); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "new" {
		t.Errorf("cache content = %q, want new", b)
	}
}

func TestAlreadyInstalled(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.NewBuilder(model.Meta{ID: "go", Title: "Go"}, false)
	if err != nil {
		t.Fatal(err)
	}
	b.AddPage("index.html", []byte("<html></html>"))
	b.AddEntry(model.Entry{Name: "Foo", Page: "index.html"})
	if _, err := b.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := alreadyInstalled(st, "go", false); err == nil {
		t.Error("expected an already-installed error")
	}
	if err := alreadyInstalled(st, "go", true); err != nil {
		t.Errorf("force should bypass the check: %v", err)
	}
	if err := alreadyInstalled(st, "other", false); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
