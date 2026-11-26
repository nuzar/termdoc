package render

import (
	"strings"
	"testing"

	"github.com/nuzar/termdoc/internal/model"
)

const devdocsPage = `<!DOCTYPE html>
<html><body>
<nav id="short-nav">navigation</nav>
<main>
<h1 id="pkg-overview">Overview</h1>
<p>Package overview.</p>
<h2 id="fmt.Println">func Println</h2>
<pre><code>func Println(a ...any) (n int, err error)</code></pre>
<p>Println formats its operands.</p>
<h2 id="fmt.Printf">func Printf</h2>
<p>Printf formats according to a format specifier.</p>
</main>
</body></html>`

func TestExtractDevDocsSection(t *testing.T) {
	frag, err := Extract(model.SourceDevDocs, devdocsPage, "fmt.Println")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(frag, "Println formats") {
		t.Errorf("fragment does not contain the section body:\n%s", frag)
	}
	if !strings.Contains(frag, "func Println") {
		t.Errorf("fragment does not contain the section heading:\n%s", frag)
	}
	if strings.Contains(frag, "Printf formats") {
		t.Errorf("fragment leaks the next section:\n%s", frag)
	}
	if strings.Contains(frag, "navigation") {
		t.Errorf("fragment contains page chrome:\n%s", frag)
	}
}

func TestExtractDevDocsUnknownAnchorFallsBackToPage(t *testing.T) {
	frag, err := Extract(model.SourceDevDocs, devdocsPage, "does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(frag, "Package overview") {
		t.Errorf("expected the whole page, got:\n%s", frag)
	}
}

const dashPage = `<!DOCTYPE html>
<html><body>
<section class='category'>
<h2 id='//dash_ref/Category/File/1'>File Presentation</h2>
<table>
<tr id='//dash_ref_File/Entry/Pipe/0'>
<td class='command'><p><code>--pager=COMMAND</code></p></td>
<td class='description'>
<div class='name'><p>Pipe all output through COMMAND</p></div>
<div class='notes'><p>For example, --pager="less -R".</p></div>
</td>
</tr>
<tr id='//dash_ref_File/Entry/NoPager/0'>
<td class='command'><p><code>--nopager</code></p></td>
<td class='description'><div class='name'><p>Do not page output</p></div></td>
</tr>
</table>
</section>
</body></html>`

func TestExtractDashTableRow(t *testing.T) {
	frag, err := Extract(model.SourceDash, dashPage, "//dash_ref_File/Entry/Pipe/0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(frag, "--pager=COMMAND") || !strings.Contains(frag, "Pipe all output") {
		t.Errorf("fragment is missing the entry content:\n%s", frag)
	}
	if strings.Contains(frag, "--nopager") {
		t.Errorf("fragment leaks the next entry:\n%s", frag)
	}
}

func TestExtractDashAppleRefAnchor(t *testing.T) {
	page := `<html><body>
<a class="dashAnchor" name="//apple_ref/cpp/Function/Foo"></a>
<h3>Foo</h3>
<p>Foo does things.</p>
<a class="dashAnchor" name="//apple_ref/cpp/Function/Bar"></a>
<h3>Bar</h3>
<p>Bar does other things.</p>
</body></html>`
	frag, err := Extract(model.SourceDash, page, "//apple_ref/cpp/Function/Foo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(frag, "Foo does things") {
		t.Errorf("fragment is missing the section body:\n%s", frag)
	}
	if strings.Contains(frag, "Bar does other things") {
		t.Errorf("fragment leaks the next section:\n%s", frag)
	}
}

func TestExtractDashPercentEncodedAnchor(t *testing.T) {
	page := `<html><body>
<section class='category'>
<h2 id='//dash_ref/Category/File%20Presentation/1'>File Presentation</h2>
<table>
<tr id='//dash_ref_File%20Presentation/Entry/Pipe%20all%20output/0'>
<td><p>entry body</p></td>
</tr>
</table>
</section>
</body></html>`
	frag, err := Extract(model.SourceDash, page, "//dash_ref_File%20Presentation/Entry/Pipe%20all%20output/0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(frag, "entry body") {
		t.Errorf("fragment is missing the entry body:\n%s", frag)
	}
}

func TestToMarkdownTidy(t *testing.T) {
	md, err := ToMarkdown("<h2>Title</h2>\n\n\n<p>Some <code>code</code>.</p>")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "\n\n\n") {
		t.Errorf("markdown was not tidied:\n%q", md)
	}
	if !strings.Contains(md, "## Title") || !strings.Contains(md, "`code`") {
		t.Errorf("unexpected markdown:\n%s", md)
	}
}

func TestText(t *testing.T) {
	if got := Text("<p>Hello <b>world</b></p>"); got != "Hello world" {
		t.Errorf("Text = %q", got)
	}
}
