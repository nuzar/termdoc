// Package archive exports installed docsets into a portable tar.gz and imports
// termdoc/Dash archives.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nuzar/termdoc/internal/httpx"
	"github.com/nuzar/termdoc/internal/model"
	"github.com/nuzar/termdoc/internal/source"
	"github.com/nuzar/termdoc/internal/store"
)

const markerName = "termdoc-export.json"

type marker struct {
	Format  int       `json:"format"`
	Tool    string    `json:"tool"`
	Docsets []string  `json:"docsets"`
	Created time.Time `json:"created"`
}

// Export writes the given docsets into a portable tar.gz archive.
func Export(st *store.Store, ids []string, w io.Writer) ([]string, error) {
	if len(ids) == 0 {
		return nil, errors.New("no docsets selected")
	}
	var metas []model.Meta
	for _, id := range ids {
		m, err := st.ResolveOne(id)
		if err != nil {
			return nil, err
		}
		metas = append(metas, m)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].ID < metas[j].ID })

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	names := make([]string, 0, len(metas))
	for _, m := range metas {
		names = append(names, m.ID)
	}
	mb, _ := json.MarshalIndent(marker{Format: model.FormatVersion, Tool: "termdoc", Docsets: names, Created: time.Now().UTC()}, "", "  ")
	if err := writeTarFile(tw, markerName, append(mb, '\n'), time.Now()); err != nil {
		return nil, err
	}

	for _, m := range metas {
		root := st.Dir(m.ID)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			hdr := &tar.Header{
				Name:     m.ID + "/" + filepath.ToSlash(rel),
				Mode:     0o644,
				Size:     info.Size(),
				ModTime:  info.ModTime(),
				Typeflag: tar.TypeReg,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				f.Close()
				return err
			}
			_, err = io.Copy(tw, f)
			cerr := f.Close()
			if err == nil {
				err = cerr
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return names, nil
}

func writeTarFile(tw *tar.Writer, name string, data []byte, mod time.Time) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), ModTime: mod, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

type archiveKind int

const (
	kindUnknown archiveKind = iota
	kindTermdocExport
	kindTermdocSingle
	kindDash
)

// Import installs a docset from a local path. Supported inputs:
//   - a termdoc export (.tar.gz containing termdoc-export.json)
//   - a single termdoc docset directory or archive
//   - a Dash/Zeal docset (.docset directory, .tgz, .tar.gz, .zip)
//
// The special path "-" reads the archive from r.
func Import(ctx context.Context, st *store.Store, client *httpx.Client, path string, force bool) ([]string, error) {
	if path == "-" {
		return ImportStdin(ctx, st, client, os.Stdin, force)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return importDir(st, path, force)
	}
	return importFile(ctx, st, client, path, force)
}

// ImportStdin installs an archive read from r.
func ImportStdin(ctx context.Context, st *store.Store, client *httpx.Client, r io.Reader, force bool) ([]string, error) {
	f, err := os.CreateTemp(st.TempDir(), "import-stdin-*.tar.gz")
	if err != nil {
		return nil, err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return importFile(ctx, st, client, name, force)
}

func importDir(st *store.Store, dir string, force bool) ([]string, error) {
	if fileExists(filepath.Join(dir, "meta.json")) {
		m, err := st.InstallDir(dir, force)
		if err != nil {
			return nil, err
		}
		return []string{m.ID}, nil
	}
	if hasDashDocset(dir) {
		metas, err := source.InstallDashPath(st, dir, force)
		if err != nil {
			return nil, err
		}
		return metaIDs(metas), nil
	}
	// A directory holding several termdoc docsets.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		if !fileExists(filepath.Join(sub, "meta.json")) {
			continue
		}
		m, err := st.InstallDir(sub, force)
		if err != nil {
			return ids, err
		}
		ids = append(ids, m.ID)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%s is not a termdoc or Dash docset", dir)
	}
	return ids, nil
}

func importFile(ctx context.Context, st *store.Store, client *httpx.Client, path string, force bool) ([]string, error) {
	kind, err := detectFile(path)
	if err != nil {
		return nil, err
	}
	switch kind {
	case kindDash:
		metas, err := source.InstallDashPath(st, path, force)
		if err != nil {
			return nil, err
		}
		return metaIDs(metas), nil
	case kindTermdocExport, kindTermdocSingle:
		tmp, err := os.MkdirTemp(st.TempDir(), "import-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		if err := extractTermdoc(path, tmp); err != nil {
			return nil, err
		}
		return importDir(st, tmp, force)
	default:
		return nil, fmt.Errorf("%s: unsupported archive (expected a termdoc export, .docset, .tgz or .zip)", filepath.Base(path))
	}
}

func detectFile(path string) (archiveKind, error) {
	f, err := os.Open(path)
	if err != nil {
		return kindUnknown, err
	}
	magic := make([]byte, 4)
	n, _ := io.ReadFull(f, magic)
	f.Close()

	switch {
	case n >= 4 && magic[0] == 'P' && magic[1] == 'K':
		zr, err := zip.OpenReader(path)
		if err != nil {
			return kindUnknown, err
		}
		defer zr.Close()
		names := make([]string, 0, len(zr.File))
		for _, zf := range zr.File {
			names = append(names, zf.Name)
		}
		return classifyNames(names), nil
	case n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		f, err := os.Open(path)
		if err != nil {
			return kindUnknown, err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return kindUnknown, err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		var names []string
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return kindUnknown, err
			}
			names = append(names, h.Name)
		}
		return classifyNames(names), nil
	}
	return kindUnknown, nil
}

func classifyNames(names []string) archiveKind {
	hasMarker := false
	hasDash := false
	hasRootMeta := false
	for _, n := range names {
		n = strings.TrimPrefix(strings.ReplaceAll(n, "\\", "/"), "./")
		if n == markerName {
			hasMarker = true
		}
		if strings.EqualFold(filepath.Base(n), "docSet.dsidx") {
			hasDash = true
		}
		if n == "meta.json" || n == "entries.json" {
			hasRootMeta = true
		}
	}
	switch {
	case hasMarker:
		return kindTermdocExport
	case hasDash:
		return kindDash
	case hasRootMeta:
		return kindTermdocSingle
	default:
		return kindUnknown
	}
}

func extractTermdoc(path, dst string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
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
		if h.Typeflag != tar.TypeReg {
			continue
		}
		rel := filepath.FromSlash(strings.ReplaceAll(h.Name, "\\", "/"))
		rel = filepath.Clean(rel)
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func hasDashDocset(dir string) bool {
	found := false
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(d.Name(), "docSet.dsidx") {
			found = true
			return io.EOF
		}
		return nil
	})
	return found
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func metaIDs(ms []model.Meta) []string {
	ids := make([]string, 0, len(ms))
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return ids
}
