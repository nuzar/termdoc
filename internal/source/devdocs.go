package source

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nuzar/termdoc/internal/httpx"
	"github.com/nuzar/termdoc/internal/model"
	"github.com/nuzar/termdoc/internal/store"
)

// InstallOptions controls docset installation.
type InstallOptions struct {
	Force   bool
	Refresh bool
	NoCache bool
}

type devdocsIndex struct {
	Entries []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"entries"`
}

// InstallDevDocs downloads and converts a devdocs.io docset.
func InstallDevDocs(ctx context.Context, st *store.Store, client *httpx.Client, doc DevDocsDoc, opt InstallOptions) (*model.Meta, error) {
	if err := alreadyInstalled(st, doc.Slug, opt.Force); err != nil {
		return nil, err
	}
	base := "https://documents.devdocs.io/" + doc.Slug
	indexURL := base + "/index.json"
	dbURL := base + "/db.json"

	client.Logf("Downloading %s index...\n", doc.Name)
	indexBytes, err := client.Get(ctx, indexURL)
	if err != nil {
		return nil, fmt.Errorf("download DevDocs index: %w", err)
	}
	var idx devdocsIndex
	if err := json.Unmarshal(indexBytes, &idx); err != nil {
		return nil, fmt.Errorf("parse DevDocs index: %w", err)
	}

	dbPath, cleanup, err := downloadDB(ctx, st, client, doc, dbURL, opt)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	title := doc.Name
	version := doc.Version
	if version == "" {
		version = doc.Release
	}
	if version != "" && !strings.Contains(title, version) {
		title += " " + version
	}
	b, err := st.NewBuilder(model.Meta{
		ID:          doc.Slug,
		Title:       title,
		Slug:        doc.Slug,
		Source:      model.SourceDevDocs,
		Version:     version,
		Release:     doc.Release,
		Homepage:    doc.Links.Home,
		Attribution: doc.Attribution,
	}, opt.Force)
	if err != nil {
		return nil, err
	}

	pageSet := make(map[string]bool)
	if err := streamJSONObject(dbPath, func(key, val string) error {
		pageSet[key] = true
		return b.AddPage(key, []byte(val))
	}); err != nil {
		b.Abort()
		return nil, fmt.Errorf("read DevDocs database: %w", err)
	}

	seen := make(map[string]bool)
	added := 0
	for _, e := range idx.Entries {
		page, anchor := splitAnchor(e.Path)
		if page == "" || !pageSet[page] {
			continue
		}
		key := e.Name + "\x00" + page + "\x00" + anchor
		if seen[key] {
			continue
		}
		seen[key] = true
		b.AddEntry(model.Entry{Name: e.Name, Type: e.Type, Page: page, Anchor: anchor})
		added++
	}
	if added == 0 {
		b.Abort()
		return nil, fmt.Errorf("DevDocs docset %q contains no usable entries", doc.Slug)
	}

	m, err := b.Commit()
	if err != nil {
		return nil, err
	}
	return m, nil
}

func downloadDB(ctx context.Context, st *store.Store, client *httpx.Client, doc DevDocsDoc, url string, opt InstallOptions) (string, func(), error) {
	cachePath := filepath.Join(st.CacheDir(), "devdocs-"+store.SanitizeID(doc.Slug)+"-db.json")
	if !opt.NoCache {
		if fi, err := os.Stat(cachePath); err == nil && fi.Size() > 0 && (doc.DBSize == 0 || fi.Size() == doc.DBSize) && validJSONObject(cachePath) {
			client.Logf("Using cached database %s\n", cachePath)
			return cachePath, func() {}, nil
		}
	}

	tmp, err := st.TempDirFor("db")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(tmp) }
	dst := filepath.Join(tmp, "db.json")
	f, err := os.Create(dst)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	client.Logf("Downloading %s database...\n", doc.Name)
	_, err = client.Download(ctx, url, f)
	cerr := f.Close()
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("download DevDocs database: %w", err)
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

func streamJSONObject(path string, fn func(key, val string) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("expected a JSON object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := kt.(string)
		if !ok {
			return errors.New("expected string key")
		}
		var val string
		if err := dec.Decode(&val); err != nil {
			return err
		}
		if err := fn(key, val); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}

func splitAnchor(path string) (page, anchor string) {
	if i := strings.IndexByte(path, '#'); i >= 0 {
		return path[:i], path[i+1:]
	}
	return path, ""
}
