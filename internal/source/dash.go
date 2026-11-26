package source

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/nuzar/termdoc/internal/httpx"
	"github.com/nuzar/termdoc/internal/model"
	"github.com/nuzar/termdoc/internal/store"
)

// InstallDash downloads and converts a Zeal/Dash docset.
func InstallDash(ctx context.Context, st *store.Store, client *httpx.Client, doc DashDoc, opt InstallOptions) (*model.Meta, error) {
	id := "dash-" + store.SanitizeID(doc.Name)
	if err := alreadyInstalled(st, id, opt.Force); err != nil {
		return nil, err
	}
	archive, cleanup, err := downloadDash(ctx, st, client, doc, opt)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	version := ""
	if len(doc.Versions) > 0 {
		version = doc.Versions[0]
	}
	title := doc.Title
	if title == "" {
		title = doc.Name
	}
	return installDashPath(st, archive, id, title, version, opt.Force)
}

func downloadDash(ctx context.Context, st *store.Store, client *httpx.Client, doc DashDoc, opt InstallOptions) (string, func(), error) {
	name := doc.Archive
	if name == "" {
		name = doc.Name + ".tgz"
	}
	cachePath := filepath.Join(st.CacheDir(), "dash-"+store.SanitizeID(strings.TrimSuffix(name, filepath.Ext(name)))+filepath.Ext(name))
	if !opt.NoCache {
		if fi, err := os.Stat(cachePath); err == nil && fi.Size() > 0 && (doc.Size == 0 || fi.Size() == doc.Size) && validArchive(cachePath) {
			client.Logf("Using cached archive %s\n", cachePath)
			return cachePath, func() {}, nil
		}
	}
	tmp, err := st.TempDirFor("dash-dl")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(tmp) }
	dst := filepath.Join(tmp, filepath.Base(cachePath))
	f, err := os.Create(dst)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	client.Logf("Downloading %s...\n", doc.Name)
	_, err = client.Download(ctx, doc.URL(), f)
	cerr := f.Close()
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("download Dash docset: %w", err)
	}
	if cerr != nil {
		cleanup()
		return "", func() {}, cerr
	}
	if !opt.NoCache {
		if err := replaceCache(dst, cachePath); err == nil {
			cleanup()
			return cachePath, func() {}, nil
		}
	}
	return dst, cleanup, nil
}

// InstallDashPath imports a local Dash docset archive (.tgz/.tar.gz/.zip) or a
// directory containing one or more .docset bundles.
func InstallDashPath(st *store.Store, path string, force bool) ([]model.Meta, error) {
	docsetDirs, cleanup, err := prepareDash(path)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	var out []model.Meta
	for _, dir := range docsetDirs {
		name := strings.TrimSuffix(filepath.Base(dir), ".docset")
		id := "dash-" + store.SanitizeID(name)
		m, err := installDashDocset(st, dir, id, name, "", force)
		if err != nil {
			return out, err
		}
		out = append(out, *m)
	}
	if len(out) == 0 {
		return nil, errors.New("no Dash docset (.docset/Contents/Resources/docSet.dsidx) found")
	}
	return out, nil
}

func installDashPath(st *store.Store, archive, id, title, version string, force bool) (*model.Meta, error) {
	docsetDirs, cleanup, err := prepareDash(archive)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if len(docsetDirs) == 0 {
		return nil, errors.New("archive does not contain a Dash docset")
	}
	return installDashDocset(st, docsetDirs[0], id, title, version, force)
}

// prepareDash extracts the parts of a Dash archive that termdoc needs and
// returns the .docset directories found.
func prepareDash(path string) ([]string, func(), error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, func() {}, err
	}
	if fi.IsDir() {
		return findDashRoots(path), func() {}, nil
	}

	tmp, err := os.MkdirTemp("", "termdoc-dash-")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { os.RemoveAll(tmp) }

	if err := extractDashArchive(path, tmp); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	roots := findDashRoots(tmp)
	if len(roots) == 0 {
		cleanup()
		return nil, func() {}, errors.New("archive does not contain a Dash docset")
	}
	return roots, cleanup, nil
}

func extractDashArchive(path, dst string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	magic := make([]byte, 4)
	n, _ := io.ReadFull(f, magic)
	f.Seek(0, io.SeekStart)

	switch {
	case n >= 4 && magic[0] == 'P' && magic[1] == 'K':
		zr, err := zip.OpenReader(path)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, zf := range zr.File {
			if !keepDashFile(zf.Name) {
				continue
			}
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			err = writeExtracted(dst, zf.Name, rc)
			rc.Close()
			if err != nil {
				return err
			}
		}
		return nil
	case n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if h.Typeflag != tar.TypeReg || !keepDashFile(h.Name) {
				continue
			}
			if err := writeExtracted(dst, h.Name, tr); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s is not a .tgz or .zip Dash archive", filepath.Base(path))
	}
}

func keepDashFile(name string) bool {
	n := strings.ToLower(strings.ReplaceAll(name, "\\", "/"))
	if strings.Contains(n, "contents/resources/") && strings.HasSuffix(n, ".dsidx") {
		return true
	}
	if strings.Contains(n, "/documents/") && (strings.HasSuffix(n, ".html") || strings.HasSuffix(n, ".htm")) {
		return true
	}
	return false
}

func writeExtracted(root, name string, r io.Reader) error {
	rel := filepath.FromSlash(strings.ReplaceAll(name, "\\", "/"))
	rel = filepath.Clean(rel)
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") || strings.Contains(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	target := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func findDashRoots(root string) []string {
	var out []string
	seen := make(map[string]bool)
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "docSet.dsidx") {
			dir := filepath.Dir(filepath.Dir(filepath.Dir(path))) // Contents/Resources/docSet.dsidx
			if !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

type dashRow struct {
	Name string
	Type string
	Path string
}

func installDashDocset(st *store.Store, docsetDir, id, title, version string, force bool) (*model.Meta, error) {
	dsidx := ""
	filepath.WalkDir(docsetDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(d.Name(), "docSet.dsidx") {
			dsidx = path
			return io.EOF
		}
		return nil
	})
	if dsidx == "" {
		return nil, fmt.Errorf("%s: docSet.dsidx not found", docsetDir)
	}
	pagesRoot := filepath.Join(docsetDir, "Contents", "Resources", "Documents")
	if _, err := os.Stat(pagesRoot); err != nil {
		pagesRoot = filepath.Dir(dsidx)
	}

	db, err := openSQLiteRO(dsidx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT name, type, path FROM searchIndex`)
	if err != nil {
		return nil, fmt.Errorf("read Dash search index: %w", err)
	}
	var entries []dashRow
	for rows.Next() {
		var name, typ, path sql.NullString
		if err := rows.Scan(&name, &typ, &path); err != nil {
			rows.Close()
			return nil, err
		}
		if !name.Valid || !path.Valid {
			continue
		}
		entries = append(entries, dashRow{Name: name.String, Type: typ.String, Path: path.String})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s: search index is empty", filepath.Base(docsetDir))
	}

	b, err := st.NewBuilder(model.Meta{
		ID:      id,
		Title:   title,
		Slug:    strings.TrimSuffix(filepath.Base(docsetDir), ".docset"),
		Source:  model.SourceDash,
		Version: version,
	}, force)
	if err != nil {
		return nil, err
	}

	pageExists := make(map[string]bool)
	fileOK := func(rel string) bool {
		if v, ok := pageExists[rel]; ok {
			return v
		}
		p := filepath.Join(pagesRoot, filepath.FromSlash(rel))
		_, err := os.Stat(p)
		pageExists[rel] = err == nil
		return err == nil
	}
	loaded := make(map[string]bool)
	seen := make(map[string]bool)
	added := 0
	for _, row := range entries {
		rawPath := row.Path
		// Dash stores extra metadata as <dash_entry_name=...>... prefixes in
		// the path column. The real path starts after the last '>'.
		if strings.HasPrefix(strings.TrimSpace(rawPath), "<") {
			if i := strings.LastIndex(rawPath, ">"); i >= 0 {
				rawPath = rawPath[i+1:]
			}
		}
		page, anchor := splitAnchor(rawPath)
		page = strings.TrimPrefix(strings.ReplaceAll(page, "\\", "/"), "./")
		if page == "" || strings.Contains(page, "://") {
			continue
		}
		if !fileOK(page) {
			if un, err := url.PathUnescape(page); err == nil && un != page && fileOK(un) {
				page = un
			} else {
				continue
			}
		}
		if !loaded[page] {
			data, err := os.ReadFile(filepath.Join(pagesRoot, filepath.FromSlash(page)))
			if err != nil {
				continue
			}
			if err := b.AddPage(page, data); err != nil {
				b.Abort()
				return nil, err
			}
			loaded[page] = true
		}
		key := row.Name + "\x00" + row.Type + "\x00" + page + "\x00" + anchor
		if seen[key] {
			continue
		}
		seen[key] = true
		b.AddEntry(model.Entry{Name: row.Name, Type: row.Type, Page: page, Anchor: anchor})
		added++
	}
	if added == 0 {
		b.Abort()
		return nil, fmt.Errorf("%s: no entries with local pages", filepath.Base(docsetDir))
	}
	return b.Commit()
}

func openSQLiteRO(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	q := url.Values{}
	q.Set("mode", "ro")
	q.Set("immutable", "1")
	dsn := u.String() + "?" + q.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}
