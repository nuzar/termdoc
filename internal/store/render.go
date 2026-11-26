package store

import (
	"fmt"
	"strings"

	"github.com/nuzar/termdoc/internal/model"
	"github.com/nuzar/termdoc/internal/render"
)

// EntryMarkdown returns the Markdown document for one entry, including a title
// header and source footer.
func (d *Docset) EntryMarkdown(e model.Entry) (string, error) {
	page, err := d.Page(e.Page)
	if err != nil {
		return "", err
	}
	frag, err := render.Extract(d.Meta.Source, string(page), e.Anchor)
	if err != nil {
		return "", err
	}
	body, err := render.ToMarkdown(frag)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(body) == "" {
		body = "_No content available for this entry._"
	}

	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(e.Name)
	b.WriteString("\n\n")

	var sub []string
	if e.Type != "" {
		sub = append(sub, e.Type)
	}
	title := d.Meta.Title
	if d.Meta.Version != "" && !strings.Contains(title, d.Meta.Version) {
		title += " " + d.Meta.Version
	}
	sub = append(sub, title)
	sub = append(sub, string(d.Meta.Source))
	b.WriteString("*")
	b.WriteString(strings.Join(sub, " · "))
	b.WriteString("*\n\n")
	b.WriteString(body)
	b.WriteString("\n")

	if d.Meta.Attribution != "" {
		attr := render.Text(d.Meta.Attribution)
		if attr != "" {
			b.WriteString("\n---\n\n*")
			b.WriteString(attr)
			b.WriteString("*\n")
		}
	}
	return b.String(), nil
}

// EntryHTML returns the extracted HTML fragment for one entry.
func (d *Docset) EntryHTML(e model.Entry) (string, error) {
	page, err := d.Page(e.Page)
	if err != nil {
		return "", err
	}
	return render.Extract(d.Meta.Source, string(page), e.Anchor)
}

// FindEntry looks up an entry by exact name (case-insensitive). When several
// entries share the name, all are returned.
func (d *Docset) FindEntry(name string) []model.Entry {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	want := strings.ToLower(name)
	wantNoParen := strings.TrimSuffix(want, "()")
	var out []model.Entry
	for _, e := range d.Entries {
		n := strings.ToLower(e.Name)
		if n == want || strings.TrimSuffix(n, "()") == wantNoParen {
			out = append(out, e)
		}
	}
	return out
}

// ShortID renders a docset ID for humans.
func ShortID(id string) string {
	return strings.TrimPrefix(id, "dash-")
}

// Describe renders a one-line description of a docset.
func Describe(m model.Meta) string {
	parts := []string{m.Title}
	if m.Version != "" && !strings.Contains(m.Title, m.Version) {
		parts = append(parts, m.Version)
	}
	parts = append(parts, fmt.Sprintf("%d entries", m.Entries), string(m.Source))
	return strings.Join(parts, " · ")
}
