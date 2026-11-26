package source

import (
	"testing"
)

func TestResolveDevDocsPrefersNewestVersion(t *testing.T) {
	docs := []DevDocsDoc{
		{Name: "Go", Slug: "go", Version: "1.26"},
		{Name: "Go", Slug: "go", Version: "1.27"},
		{Name: "Django", Slug: "django~5.0", Version: "5.0"},
	}
	got, err := ResolveDevDocs(docs, "go")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.27" {
		t.Errorf("picked version %q, want 1.27", got.Version)
	}
	if got, err := ResolveDevDocs(docs, "django"); err != nil || got.Slug != "django~5.0" {
		t.Errorf("ResolveDevDocs(django) = %+v, %v", got, err)
	}
	if _, err := ResolveDevDocs(docs, "nope"); err == nil {
		t.Error("expected an error for an unknown docset")
	}
}

func TestResolveDash(t *testing.T) {
	docs := []DashDoc{
		{Name: "Go", Title: "Go"},
		{Name: "Go_Contrib", Title: "Go (user contributed)"},
	}
	if got, err := ResolveDash(docs, "go"); err != nil || got.Name != "Go" {
		t.Errorf("ResolveDash(go) = %+v, %v", got, err)
	}
	if got, err := ResolveDash(docs, "contributed"); err != nil || got.Name != "Go_Contrib" {
		t.Errorf("ResolveDash(contributed) = %+v, %v", got, err)
	}
	if _, err := ResolveDash(docs, "G"); err == nil {
		t.Error("expected an ambiguity error")
	}
}

func TestDashURL(t *testing.T) {
	d := DashDoc{Name: "Python_3", SourceID: "com.kapeli"}
	want := "https://go.zealdocs.org/d/com.kapeli/Python_3/latest"
	if got := d.URL(); got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
	d = DashDoc{Name: "Om"}
	want = "https://go.zealdocs.org/d/com.kapeli/Om/latest"
	if got := d.URL(); got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.27", "1.26", 1},
		{"1.9", "1.10", -1},
		{"v2.0.0", "2.0", 0},
		{"", "1.0", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	cases := map[int64]string{
		0:          "-",
		512:        "512 B",
		1024:       "1.0 KiB",
		1536:       "1.5 KiB",
		1048576:    "1.0 MiB",
		1073741824: "1.0 GiB",
	}
	for in, want := range cases {
		if got := FormatSize(in); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitAnchor(t *testing.T) {
	page, anchor := splitAnchor("pkg/foo.html#Bar")
	if page != "pkg/foo.html" || anchor != "Bar" {
		t.Errorf("splitAnchor = %q, %q", page, anchor)
	}
	page, anchor = splitAnchor("index.html")
	if page != "index.html" || anchor != "" {
		t.Errorf("splitAnchor = %q, %q", page, anchor)
	}
}
