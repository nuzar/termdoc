// Package source implements remote docset catalogs and importers.
package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nuzar/termdoc/internal/httpx"
	"github.com/nuzar/termdoc/internal/store"
)

const (
	devdocsCatalogURL = "https://devdocs.io/docs.json"
	dashCatalogURL    = "https://api.zealdocs.org/v1/docsets"
	catalogTTL        = 24 * time.Hour
)

// DevDocsDoc is one entry of devdocs.io/docs.json.
type DevDocsDoc struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Type        string `json:"type"`
	Version     string `json:"version"`
	Release     string `json:"release"`
	Mtime       int64  `json:"mtime"`
	DBSize      int64  `json:"db_size"`
	Attribution string `json:"attribution"`
	Links       struct {
		Home string `json:"home"`
		Code string `json:"code"`
	} `json:"links"`
}

// DashDoc is one entry of the Zeal docset catalog.
type DashDoc struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	SourceID string   `json:"sourceId"`
	Versions []string `json:"versions"`
	Archive  string   `json:"archive"`
	Size     int64    `json:"size"`
	Tarix    bool     `json:"tarix"`
}

// URL returns the download URL for a Dash docset archive.
//
// Zeal resolves docsets through go.zealdocs.org, which redirects to the
// current archive (including user-contributed feeds).
func (d DashDoc) URL() string {
	src := d.SourceID
	if src == "" {
		src = "com.kapeli"
	}
	return "https://go.zealdocs.org/d/" + urlEscapePath(src) + "/" + urlEscapePath(d.Name) + "/latest"
}

func urlEscapePath(s string) string {
	// Archive names are plain file names; only spaces need escaping in practice.
	return strings.ReplaceAll(s, " ", "%20")
}

// FetchDevDocsCatalog loads and caches the DevDocs catalog.
func FetchDevDocsCatalog(ctx context.Context, c *httpx.Client, st *store.Store, refresh bool) ([]DevDocsDoc, error) {
	b, err := cachedGet(ctx, c, st, "devdocs-catalog.json", devdocsCatalogURL, refresh)
	if err != nil {
		return nil, err
	}
	var docs []DevDocsDoc
	if err := json.Unmarshal(b, &docs); err != nil {
		return nil, fmt.Errorf("parse DevDocs catalog: %w", err)
	}
	sort.Slice(docs, func(i, j int) bool { return strings.ToLower(docs[i].Name) < strings.ToLower(docs[j].Name) })
	return docs, nil
}

// FetchDashCatalog loads and caches the Zeal docset catalog.
func FetchDashCatalog(ctx context.Context, c *httpx.Client, st *store.Store, refresh bool) ([]DashDoc, error) {
	b, err := cachedGet(ctx, c, st, "zeal-catalog.json", dashCatalogURL, refresh)
	if err != nil {
		return nil, err
	}
	var docs []DashDoc
	if err := json.Unmarshal(b, &docs); err != nil {
		return nil, fmt.Errorf("parse Zeal catalog: %w", err)
	}
	sort.Slice(docs, func(i, j int) bool { return strings.ToLower(docs[i].Title) < strings.ToLower(docs[j].Title) })
	return docs, nil
}

func cachedGet(ctx context.Context, c *httpx.Client, st *store.Store, name, url string, refresh bool) ([]byte, error) {
	path := filepath.Join(st.CacheDir(), name)
	if !refresh {
		if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < catalogTTL {
			if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
				return b, nil
			}
		}
	}
	b, err := c.Get(ctx, url)
	if err != nil {
		if old, rerr := os.ReadFile(path); rerr == nil && len(old) > 0 {
			fmt.Fprintf(os.Stderr, "warning: could not refresh catalog (%v); using cached copy\n", err)
			return old, nil
		}
		return nil, fmt.Errorf("download catalog %s: %w\nHint: set a proxy in %s or with TERMDOC_PROXY", url, err, filepath.Join(st.Home, "config.toml"))
	}
	if err := os.WriteFile(path+".tmp", b, 0o644); err == nil {
		_ = os.Rename(path+".tmp", path)
	}
	return b, nil
}

// ResolveDevDocs picks the best catalog match for name.
func ResolveDevDocs(docs []DevDocsDoc, name string) (DevDocsDoc, error) {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)
	if lower == "" {
		return DevDocsDoc{}, errors.New("empty docset name")
	}
	var exactSlug, exactName, partial []DevDocsDoc
	for _, d := range docs {
		switch {
		case strings.EqualFold(d.Slug, name):
			exactSlug = append(exactSlug, d)
		case strings.EqualFold(d.Name, name):
			exactName = append(exactName, d)
		case strings.Contains(strings.ToLower(d.Slug), lower) || strings.Contains(strings.ToLower(d.Name), lower):
			partial = append(partial, d)
		}
	}
	for _, set := range [][]DevDocsDoc{exactSlug, exactName, partial} {
		if len(set) == 0 {
			continue
		}
		if len(set) == 1 {
			return set[0], nil
		}
		sort.SliceStable(set, func(i, j int) bool {
			if cmp := compareVersions(set[i].Version, set[j].Version); cmp != 0 {
				return cmp > 0
			}
			return set[i].Mtime > set[j].Mtime
		})
		return set[0], nil
	}
	return DevDocsDoc{}, fmt.Errorf("no DevDocs docset matches %q (try `termdoc docs list --remote --source devdocs`)", name)
}

// ResolveDash picks the best catalog match for name.
func ResolveDash(docs []DashDoc, name string) (DashDoc, error) {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)
	if lower == "" {
		return DashDoc{}, errors.New("empty docset name")
	}
	var exactName, exactTitle, partial []DashDoc
	for _, d := range docs {
		switch {
		case strings.EqualFold(d.Name, name):
			exactName = append(exactName, d)
		case strings.EqualFold(d.Title, name):
			exactTitle = append(exactTitle, d)
		case strings.Contains(strings.ToLower(d.Name), lower) || strings.Contains(strings.ToLower(d.Title), lower):
			partial = append(partial, d)
		}
	}
	for _, set := range [][]DashDoc{exactName, exactTitle, partial} {
		if len(set) == 1 {
			return set[0], nil
		}
		if len(set) > 1 {
			names := make([]string, 0, len(set))
			for _, d := range set {
				names = append(names, d.Name)
			}
			sort.Strings(names)
			if len(names) > 8 {
				names = append(names[:8], "...")
			}
			return DashDoc{}, fmt.Errorf("docset %q is ambiguous: %s", name, strings.Join(names, ", "))
		}
	}
	return DashDoc{}, fmt.Errorf("no Zeal/Dash docset matches %q (try `termdoc docs list --remote --source dash`)", name)
}

func compareVersions(a, b string) int {
	as, bs := versionParts(a), versionParts(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) []int {
	var out []int
	cur := 0
	have := false
	for _, r := range v {
		if r >= '0' && r <= '9' {
			cur = cur*10 + int(r-'0')
			have = true
			continue
		}
		if have {
			out = append(out, cur)
			cur, have = 0, false
		}
	}
	if have {
		out = append(out, cur)
	}
	return out
}

// FormatSize renders a byte count for catalog listings.
func FormatSize(n int64) string {
	if n <= 0 {
		return "-"
	}
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
