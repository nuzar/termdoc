package render

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// TermOptions controls ANSI rendering and paging.
type TermOptions struct {
	Style   string
	Width   int
	Color   bool
	Pager   string
	NoPager bool
	// Out receives the rendered document. Defaults to os.Stdout.
	Out io.Writer
}

// RenderMarkdown converts Markdown to ANSI text.
func RenderMarkdown(md string, o TermOptions) (string, error) {
	style := strings.ToLower(strings.TrimSpace(o.Style))
	if !o.Color || style == "notty" || style == "plain" {
		style = "notty"
	}
	var opts []glamour.TermRendererOption
	if style == "" || style == "auto" {
		opts = append(opts, glamour.WithAutoStyle())
	} else {
		opts = append(opts, glamour.WithStandardStyle(style))
	}
	width := o.Width
	if width <= 0 {
		width = 100
	}
	opts = append(opts, glamour.WithWordWrap(width))

	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return "", err
	}
	defer r.Close()
	out, err := r.Render(md)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, " \t\n") + "\n", nil
}

// IsTTY reports whether f is a terminal.
func IsTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// TerminalWidth returns the usable terminal width, or fallback.
func TerminalWidth(fallback int) int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

// Page writes content directly, or through a pager when enabled and needed.
func Page(content string, o TermOptions) {
	out := o.Out
	if out == nil {
		out = os.Stdout
	}
	interactive := o.Out == nil || o.Out == os.Stdout
	if o.NoPager || !interactive || !IsTTY(os.Stdout) {
		fmt.Fprint(out, content)
		return
	}
	_, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || height <= 0 {
		height = 24
	}
	if strings.Count(content, "\n") < height-1 {
		fmt.Fprint(out, content)
		return
	}
	cmdline := strings.TrimSpace(o.Pager)
	if cmdline == "" {
		cmdline = defaultPager()
	}
	if cmdline == "" {
		fmt.Fprint(out, content)
		return
	}
	fields := strings.Fields(cmdline)
	cmd := exec.Command(fields[0], fields[1:]...)
	cmd.Env = append(os.Environ(), "LESS=FRX", "LV=-c", "MORE=FRX")
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprint(out, content)
	}
}

func defaultPager() string {
	if p := os.Getenv("PAGER"); p != "" {
		return p
	}
	if _, err := exec.LookPath("less"); err == nil {
		return "less -R"
	}
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("more"); err == nil {
			return "more"
		}
	}
	return ""
}
