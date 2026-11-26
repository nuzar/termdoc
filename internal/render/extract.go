// Package render turns docset HTML into terminal-friendly Markdown and ANSI.
package render

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/nuzar/termdoc/internal/model"
)

// Extract returns a cleaned HTML fragment for one entry.
//
// For docsets with section anchors only that section is returned; otherwise the
// main content of the page is used.
func Extract(source model.Source, pageHTML, anchor string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(pageHTML))
	if err != nil {
		return "", fmt.Errorf("parse page: %w", err)
	}
	stripNoise(doc)

	var frag string
	switch source {
	case model.SourceDevDocs:
		frag = extractDevDocs(doc, anchor)
	case model.SourceDash:
		frag = extractDash(doc, anchor)
	default:
		frag = cleanRoot(doc)
	}
	if strings.TrimSpace(frag) == "" {
		frag = cleanRoot(doc)
	}
	return frag, nil
}

func stripNoise(doc *goquery.Document) {
	doc.Find("script,style,noscript,iframe,svg,canvas,form,input,select,textarea,button,img").Remove()
	// pkg.go.dev wraps the symbol name in a link; keep the text, drop the link.
	doc.Find("a.Documentation-source").Each(func(_ int, s *goquery.Selection) {
		s.ReplaceWithHtml(html.EscapeString(s.Text()))
	})
	doc.Find("a.Documentation-idLink,.Documentation-sinceVersion,.Documentation-exampleButtonsContainer,.Documentation-exampleError").Remove()
	// Remove "¶" / "?" permalink anchors used by DevDocs and pkg.go.dev.
	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		switch strings.TrimSpace(s.Text()) {
		case "¶", "?", "#":
			s.Remove()
		}
	})
	// Repeated page chrome on pkg.go.dev package pages.
	doc.Find(".UnitDoc-title,.UnitReadme-title,.go-Main-nav,.go-Main-banner").Remove()
}

func extractDevDocs(doc *goquery.Document, anchor string) string {
	if anchor == "" {
		doc.Find("#short-nav,#manual-nav,.short-nav,.manual-nav").Remove()
		return cleanRoot(doc)
	}
	want := decodeRef(anchor)
	var target *html.Node
	doc.Find("[id]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if id, _ := s.Attr("id"); id == anchor || decodeRef(id) == want {
			target = s.Get(0)
			return false
		}
		return true
	})
	if target == nil {
		doc.Find("a[name]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			if n, _ := s.Attr("name"); n == anchor || decodeRef(n) == want {
				target = s.Get(0)
				return false
			}
			return true
		})
	}
	if target == nil {
		return cleanRoot(doc)
	}

	start := target
	level := 0
	if isHeading(target) {
		level = headingLevel(target)
	} else if h := closestHeading(target); h != nil {
		start, level = h, headingLevel(h)
	}
	if level == 0 {
		// Anchor without a heading: collect until the next heading.
		level = 6
	}

	var b strings.Builder
	writeNode(&b, start)
	for n := nextSibling(start); n != nil; n = nextSibling(n) {
		if isHeading(n) && headingLevel(n) <= level {
			break
		}
		writeNode(&b, n)
	}
	return b.String()
}

func extractDash(doc *goquery.Document, anchor string) string {
	if anchor == "" {
		return cleanRoot(doc)
	}
	want := decodeRef(anchor)
	var node *html.Node
	doc.Find("a[name]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if n, _ := s.Attr("name"); decodeRef(n) == want {
			node = s.Get(0)
			return false
		}
		return true
	})
	if node == nil {
		doc.Find("[id]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			if id, _ := s.Attr("id"); id == anchor || decodeRef(id) == want {
				node = s.Get(0)
				return false
			}
			return true
		})
	}
	if node == nil {
		return cleanRoot(doc)
	}

	curType, _ := dashRefType(refValue(node))
	isBoundary := func(n *html.Node) bool {
		t, ok := dashRefType(refValue(n))
		if !ok {
			return false
		}
		return primaryRefType(t) || (curType != "" && t == curType)
	}

	var b strings.Builder
	if node.Data == "a" || isHeading(node) {
		// The anchor marks the start of the section; the content follows it.
		writeNode(&b, node)
		collectFollowing(&b, node, isBoundary)
	} else {
		// The element itself is the entry container (e.g. <tr id=...>).
		writeNode(&b, node)
	}
	if strings.TrimSpace(stripTags(b.String())) == "" {
		// Some docsets place the anchor after the content.
		var rev []*html.Node
		for n := node.PrevSibling; n != nil; n = n.PrevSibling {
			if isBoundary(n) || containsBoundary(n, isBoundary) {
				break
			}
			rev = append(rev, n)
		}
		for i := len(rev) - 1; i >= 0; i-- {
			writeNode(&b, rev[i])
		}
	}
	if strings.TrimSpace(stripTags(b.String())) == "" {
		return cleanRoot(doc)
	}
	return b.String()
}

func collectFollowing(b *strings.Builder, node *html.Node, isBoundary func(*html.Node) bool) {
	for n := node.NextSibling; n != nil; n = n.NextSibling {
		if isBoundary(n) || containsBoundary(n, isBoundary) {
			return
		}
		writeNode(b, n)
	}
}

func cleanRoot(doc *goquery.Document) string {
	doc.Find("a.dashAnchor").Remove()
	doc.Find("nav,header,footer,aside").Remove()
	doc.Find("[class*=sidebar],[class*=side-nav],[class*=breadcrumb],[class*=navbar],[class*=site-nav]").Remove()
	doc.Find("[id*=sidebar],[id*=breadcrumb]").Remove()

	root := doc.Find("body")
	for _, sel := range []string{"main", "article", "[role=main]", "#main-content", "#content", ".markdown-body", ".document", ".docs-content"} {
		if s := doc.Find(sel).First(); s.Length() > 0 {
			root = s
			break
		}
	}
	var b strings.Builder
	if root.Length() == 0 {
		return ""
	}
	for _, n := range root.Nodes {
		writeNode(&b, n)
	}
	return b.String()
}

func writeNode(b *strings.Builder, n *html.Node) {
	if n == nil {
		return
	}
	if n.Type == html.TextNode {
		if strings.TrimSpace(n.Data) != "" {
			b.WriteString(html.EscapeString(n.Data))
		}
		return
	}
	if n.Type != html.ElementNode {
		return
	}
	if n.Data == "a" && strings.Contains(attr(n, "class"), "dashAnchor") {
		return
	}
	if n.Data == "pre" {
		normalizePre(n)
	}
	if err := html.Render(b, n); err != nil {
		return
	}
}

func normalizePre(n *html.Node) {
	lang := attr(n, "data-language")
	if lang == "" {
		return
	}
	for i := range n.Attr {
		if n.Attr[i].Key == "class" {
			if !strings.Contains(n.Attr[i].Val, "language-") {
				n.Attr[i].Val = strings.TrimSpace(n.Attr[i].Val + " language-" + lang)
			}
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: "class", Val: "language-" + lang})
}

func nextSibling(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	return n.NextSibling
}

func isHeading(n *html.Node) bool {
	return n != nil && n.Type == html.ElementNode && len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6'
}

func headingLevel(n *html.Node) int {
	if !isHeading(n) {
		return 0
	}
	return int(n.Data[1] - '0')
}

func closestHeading(n *html.Node) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if isHeading(p) {
			return p
		}
	}
	return nil
}

func containsBoundary(n *html.Node, isBoundary func(*html.Node) bool) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	if isBoundary(n) {
		return true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if containsBoundary(c, isBoundary) {
			return true
		}
	}
	return false
}

func refValue(n *html.Node) string {
	if n == nil {
		return ""
	}
	if v := attr(n, "name"); v != "" {
		return v
	}
	return attr(n, "id")
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func decodeRef(s string) string {
	if d, err := url.PathUnescape(s); err == nil {
		return d
	}
	return s
}

func dashRefType(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	s := strings.TrimPrefix(name, "//")
	parts := strings.Split(s, "/")
	if strings.HasPrefix(parts[0], "dash_ref_") {
		if len(parts) >= 2 {
			return parts[1], true
		}
		return "", true
	}
	if parts[0] == "apple_ref" && len(parts) >= 3 {
		return parts[2], true
	}
	return "", false
}

func primaryRefType(t string) bool {
	switch strings.ToLower(t) {
	case "sample", "example", "guide", "topic":
		return false
	}
	return true
}

// stripTags is a cheap text extraction used only to detect empty sections.
func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Text extracts readable text from an HTML fragment.
func Text(fragment string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return stripTags(fragment)
	}
	doc.Find("script,style,noscript").Remove()
	doc.Find("br").Each(func(_ int, s *goquery.Selection) { s.ReplaceWithHtml(" ") })
	t := strings.Join(strings.Fields(doc.Text()), " ")
	return strings.TrimSpace(t)
}
