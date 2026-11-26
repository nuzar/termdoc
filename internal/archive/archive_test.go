package archive

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuzar/termdoc/internal/model"
	"github.com/nuzar/termdoc/internal/store"
)

func buildDocset(t *testing.T, st *store.Store, id, title string) {
	t.Helper()
	b, err := st.NewBuilder(model.Meta{ID: id, Title: title, Source: model.SourceDevDocs}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddPage("index.html", []byte("<html><body><h2 id='foo'>Foo</h2><p>Hello foo.</p></body></html>")); err != nil {
		b.Abort()
		t.Fatal(err)
	}
	b.AddEntry(model.Entry{Name: "Foo", Type: "Function", Page: "index.html", Anchor: "foo"})
	if _, err := b.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	src, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	buildDocset(t, src, "go", "Go")
	buildDocset(t, src, "python~3.13", "Python 3.13")

	var buf bytes.Buffer
	names, err := Export(src, []string{"go", "python~3.13"}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("exported %v", names)
	}

	archivePath := filepath.Join(t.TempDir(), "export.tar.gz")
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	dst, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ids, err := Import(context.Background(), dst, nil, archivePath, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("imported %v", ids)
	}
	for _, id := range []string{"go", "python~3.13"} {
		d, err := dst.Load(id)
		if err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
		if len(d.Entries) != 1 || d.Entries[0].Name != "Foo" {
			t.Errorf("%s entries = %v", id, d.Entries)
		}
		md, err := d.EntryMarkdown(d.Entries[0])
		if err != nil {
			t.Fatal(err)
		}
		if !contains(md, "Hello foo") {
			t.Errorf("%s markdown = %q", id, md)
		}
		d.Close()
	}
}

func TestImportStdin(t *testing.T) {
	src, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	buildDocset(t, src, "go", "Go")

	var buf bytes.Buffer
	if _, err := Export(src, []string{"go"}, &buf); err != nil {
		t.Fatal(err)
	}

	dst, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ids, err := ImportStdin(context.Background(), dst, nil, bytes.NewReader(buf.Bytes()), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "go" {
		t.Fatalf("imported %v", ids)
	}
}

func TestImportSingleDocsetDir(t *testing.T) {
	src, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	buildDocset(t, src, "go", "Go")

	dst, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ids, err := Import(context.Background(), dst, nil, src.Dir("go"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "go" {
		t.Fatalf("imported %v", ids)
	}
}

func TestImportUnsupportedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Import(context.Background(), dst, nil, path, false); err == nil {
		t.Error("expected an unsupported archive error")
	}
}

func TestClassifyNames(t *testing.T) {
	cases := []struct {
		names []string
		want  archiveKind
	}{
		{[]string{markerName, "go/meta.json"}, kindTermdocExport},
		{[]string{"Go.docset/Contents/Resources/docSet.dsidx"}, kindDash},
		{[]string{"meta.json", "entries.json"}, kindTermdocSingle},
		{[]string{"readme.txt"}, kindUnknown},
	}
	for _, c := range cases {
		if got := classifyNames(c.names); got != c.want {
			t.Errorf("classifyNames(%v) = %v, want %v", c.names, got, c.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && bytes.Contains([]byte(s), []byte(sub))
}
