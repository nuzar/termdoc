// Package model defines the on-disk data model shared by docset importers,
// the local store, and the CLI.
package model

import "time"

// FormatVersion is the version of the termdoc docset layout.
const FormatVersion = 1

// Source identifies where a docset was imported from.
type Source string

const (
	SourceDevDocs Source = "devdocs"
	SourceDash    Source = "dash"
)

// Entry is a single searchable item (a function, type, page, ...).
type Entry struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	// Page is the key of the page inside pages.zip.
	Page string `json:"page"`
	// Anchor optionally points at a section inside Page.
	Anchor string `json:"anchor,omitempty"`
}

// Meta describes an installed docset.
type Meta struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Slug        string    `json:"slug,omitempty"`
	Source      Source    `json:"source"`
	Version     string    `json:"version,omitempty"`
	Release     string    `json:"release,omitempty"`
	Homepage    string    `json:"homepage,omitempty"`
	Attribution string    `json:"attribution,omitempty"`
	Entries     int       `json:"entries"`
	Pages       int       `json:"pages"`
	Bytes       int64     `json:"bytes"`
	Installed   time.Time `json:"installed"`
	Format      int       `json:"format"`
}

// Result is a single search hit across one or more docsets.
type Result struct {
	DocsetID    string
	DocsetTitle string
	Entry       Entry
	Score       int
}
