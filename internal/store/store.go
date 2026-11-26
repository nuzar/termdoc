// Package store manages installed docsets on disk.
package store

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nuzar/termdoc/internal/model"
)

const (
	metaFile    = "meta.json"
	entriesFile = "entries.json"
	indexFile   = "search.gob"
	pagesFile   = "pages.zip"
)

// Store is a termdoc data directory.
type Store struct {
	Home string
}

// Open prepares the data directory.
func Open(home string) (*Store, error) {
	s := &Store{Home: home}
	for _, d := range []string{s.DocsetsDir(), s.CacheDir(), s.TempDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", d, err)
		}
	}
	return s, nil
}

func (s *Store) DocsetsDir() string { return filepath.Join(s.Home, "docsets") }
func (s *Store) CacheDir() string   { return filepath.Join(s.Home, "cache") }
func (s *Store) TempDir() string    { return filepath.Join(s.Home, "tmp") }
func (s *Store) Dir(id string) string {
	return filepath.Join(s.DocsetsDir(), id)
}

// SanitizeID makes a string safe to use as a directory name.
func SanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), " .")
	if out == "" {
		return "docset"
	}
	return out
}

// List returns metadata for every installed docset, sorted by title.
func (s *Store) List() ([]model.Meta, error) {
	entries, err := os.ReadDir(s.DocsetsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []model.Meta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.DocsetsDir(), e.Name(), metaFile))
		if err != nil {
			continue
		}
		var m model.Meta
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		if m.ID == "" {
			m.ID = e.Name()
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Title), strings.ToLower(out[j].Title)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Resolve finds installed docsets matching sel (id, title, or substring).
func (s *Store) Resolve(sel string) ([]model.Meta, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return all, nil
	}
	var exact, title, partial []model.Meta
	for _, m := range all {
		switch {
		case strings.EqualFold(m.ID, sel):
			exact = append(exact, m)
		case strings.EqualFold(m.Title, sel):
			title = append(title, m)
		case strings.Contains(strings.ToLower(m.ID), strings.ToLower(sel)) ||
			strings.Contains(strings.ToLower(m.Title), strings.ToLower(sel)):
			partial = append(partial, m)
		}
	}
	for _, set := range [][]model.Meta{exact, title, partial} {
		if len(set) > 0 {
			return set, nil
		}
	}
	return nil, fmt.Errorf("docset %q is not installed (run `termdoc docs list`)", sel)
}

// ResolveOne resolves sel to exactly one docset.
func (s *Store) ResolveOne(sel string) (model.Meta, error) {
	ms, err := s.Resolve(sel)
	if err != nil {
		return model.Meta{}, err
	}
	if len(ms) == 1 {
		return ms[0], nil
	}
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return model.Meta{}, fmt.Errorf("docset %q is ambiguous: %s", sel, strings.Join(ids, ", "))
}

// Remove deletes an installed docset.
func (s *Store) Remove(id string) error {
	dir := s.Dir(id)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("docset %q is not installed", id)
	}
	return os.RemoveAll(dir)
}

// Docset is a loaded, ready-to-query docset.
type Docset struct {
	Meta    model.Meta
	Dir     string
	Entries []model.Entry

	idx      *Index
	pages    *zip.ReadCloser
	pageByID map[string]*zip.File
}

// Load reads a docset's metadata, entries and search index. Page contents are
// opened lazily.
func (s *Store) Load(id string) (*Docset, error) {
	dir := s.Dir(id)
	mb, err := os.ReadFile(filepath.Join(dir, metaFile))
	if err != nil {
		return nil, fmt.Errorf("open docset %q: %w", id, err)
	}
	var m model.Meta
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, metaFile), err)
	}
	eb, err := os.ReadFile(filepath.Join(dir, entriesFile))
	if err != nil {
		return nil, fmt.Errorf("open docset %q: %w", id, err)
	}
	var entries []model.Entry
	if err := json.Unmarshal(eb, &entries); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, entriesFile), err)
	}
	ix, err := LoadIndex(filepath.Join(dir, indexFile))
	if err != nil {
		return nil, fmt.Errorf("load search index for %q: %w", id, err)
	}
	return &Docset{Meta: m, Dir: dir, Entries: entries, idx: ix}, nil
}

// Close releases open page archives.
func (d *Docset) Close() error {
	if d.pages != nil {
		err := d.pages.Close()
		d.pages = nil
		d.pageByID = nil
		return err
	}
	return nil
}

// Search ranks entries for query.
func (d *Docset) Search(query string, limit int) []Hit {
	return d.idx.Search(d.Entries, query, limit)
}

// Page returns the raw HTML for a page key.
func (d *Docset) Page(key string) ([]byte, error) {
	if d.pages == nil {
		zr, err := zip.OpenReader(filepath.Join(d.Dir, pagesFile))
		if err != nil {
			return nil, fmt.Errorf("open pages of %q: %w", d.Meta.ID, err)
		}
		d.pages = zr
		d.pageByID = make(map[string]*zip.File, len(zr.File))
		for _, f := range zr.File {
			d.pageByID[f.Name] = f
		}
	}
	f, ok := d.pageByID[key]
	if !ok {
		return nil, fmt.Errorf("page %q not found in docset %q", key, d.Meta.ID)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// Builder assembles a docset in a temporary directory and installs it
// atomically on Commit.
type Builder struct {
	home    string
	meta    model.Meta
	dir     string
	zipFile *os.File
	zw      *zip.Writer
	seen    map[string]bool
	entries []model.Entry
	force   bool
	pages   int
	closed  bool
}

// NewBuilder starts a new docset build. The caller must call Commit or Abort.
func (s *Store) NewBuilder(meta model.Meta, force bool) (*Builder, error) {
	meta.ID = SanitizeID(meta.ID)
	if meta.ID == "" {
		return nil, errors.New("docset id is empty")
	}
	tmp, err := os.MkdirTemp(s.TempDir(), "build-"+meta.ID+"-")
	if err != nil {
		return nil, err
	}
	f, err := os.Create(filepath.Join(tmp, pagesFile))
	if err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	return &Builder{
		home:    s.Home,
		meta:    meta,
		dir:     tmp,
		zipFile: f,
		zw:      zip.NewWriter(f),
		seen:    make(map[string]bool),
		force:   force,
	}, nil
}

// AddPage adds a page to pages.zip. Duplicate keys are ignored.
func (b *Builder) AddPage(key string, content []byte) error {
	key = strings.ReplaceAll(key, "\\", "/")
	key = strings.TrimPrefix(key, "/")
	if key == "" || b.seen[key] {
		return nil
	}
	b.seen[key] = true
	w, err := b.zw.CreateHeader(&zip.FileHeader{
		Name:     key,
		Method:   zip.Deflate,
		Modified: time.Now(),
	})
	if err != nil {
		return err
	}
	if _, err := w.Write(content); err != nil {
		return err
	}
	b.pages++
	return nil
}

// AddEntry appends a searchable entry.
func (b *Builder) AddEntry(e model.Entry) {
	if e.Name == "" || e.Page == "" {
		return
	}
	e.Page = strings.ReplaceAll(e.Page, "\\", "/")
	e.Page = strings.TrimPrefix(e.Page, "/")
	b.entries = append(b.entries, e)
}

// Abort discards the temporary build directory.
func (b *Builder) Abort() {
	if b.closed {
		return
	}
	b.closed = true
	b.zw.Close()
	b.zipFile.Close()
	os.RemoveAll(b.dir)
}

// Commit finalizes the docset and moves it into place.
func (b *Builder) Commit() (*model.Meta, error) {
	if b.closed {
		return nil, errors.New("builder already closed")
	}
	b.closed = true
	if err := b.zw.Close(); err != nil {
		b.zipFile.Close()
		os.RemoveAll(b.dir)
		return nil, err
	}
	if err := b.zipFile.Close(); err != nil {
		os.RemoveAll(b.dir)
		return nil, err
	}

	eb, err := json.Marshal(b.entries)
	if err != nil {
		os.RemoveAll(b.dir)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(b.dir, entriesFile), eb, 0o644); err != nil {
		os.RemoveAll(b.dir)
		return nil, err
	}

	ix := BuildIndex(b.entries)
	if err := ix.Save(filepath.Join(b.dir, indexFile)); err != nil {
		os.RemoveAll(b.dir)
		return nil, err
	}

	b.meta.Format = model.FormatVersion
	b.meta.Entries = len(b.entries)
	b.meta.Pages = b.pages
	b.meta.Installed = time.Now().UTC()
	b.meta.Bytes = dirSize(b.dir)
	mb, err := json.MarshalIndent(b.meta, "", "  ")
	if err != nil {
		os.RemoveAll(b.dir)
		return nil, err
	}
	mb = append(mb, '\n')
	if err := os.WriteFile(filepath.Join(b.dir, metaFile), mb, 0o644); err != nil {
		os.RemoveAll(b.dir)
		return nil, err
	}

	dst := filepath.Join(b.home, "docsets", b.meta.ID)
	if _, err := os.Stat(dst); err == nil {
		if !b.force {
			os.RemoveAll(b.dir)
			return nil, fmt.Errorf("docset %q is already installed (use --force to replace it)", b.meta.ID)
		}
		if err := os.RemoveAll(dst); err != nil {
			os.RemoveAll(b.dir)
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		os.RemoveAll(b.dir)
		return nil, err
	}
	if err := os.Rename(b.dir, dst); err != nil {
		os.RemoveAll(b.dir)
		return nil, err
	}
	m := b.meta
	return &m, nil
}

// InstallDir installs an already prepared docset directory (containing
// meta.json, entries.json, search.gob and pages.zip) under its meta ID.
func (s *Store) InstallDir(src string, force bool) (model.Meta, error) {
	var m model.Meta
	b, err := os.ReadFile(filepath.Join(src, metaFile))
	if err != nil {
		return m, fmt.Errorf("not a termdoc docset (missing %s)", metaFile)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("parse meta.json: %w", err)
	}
	if m.Format != 0 && m.Format != model.FormatVersion {
		return m, fmt.Errorf("unsupported docset format %d (this build supports %d)", m.Format, model.FormatVersion)
	}
	if m.ID == "" {
		m.ID = SanitizeID(m.Title)
	}
	m.ID = SanitizeID(m.ID)
	for _, f := range []string{entriesFile, indexFile, pagesFile} {
		if _, err := os.Stat(filepath.Join(src, f)); err != nil {
			return m, fmt.Errorf("not a termdoc docset (missing %s)", f)
		}
	}
	dst := s.Dir(m.ID)
	if _, err := os.Stat(dst); err == nil {
		if !force {
			return m, fmt.Errorf("docset %q is already installed (use --force to replace it)", m.ID)
		}
		if err := os.RemoveAll(dst); err != nil {
			return m, err
		}
	}
	if err := copyDir(src, dst); err != nil {
		return m, err
	}
	// Re-read the metadata from the destination so Bytes reflects reality.
	if b, err := os.ReadFile(filepath.Join(dst, metaFile)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m, nil
}

func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, e := d.Info(); e == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			in.Close()
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			in.Close()
			return err
		}
		_, err = io.Copy(out, in)
		in.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		return err
	})
}

// TempDirFor creates a unique temporary directory under the store.
func (s *Store) TempDirFor(prefix string) (string, error) {
	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	return os.MkdirTemp(s.TempDir(), prefix+"-"+hex.EncodeToString(rnd[:])+"-")
}
