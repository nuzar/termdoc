package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/nuzar/termdoc/internal/config"
	"github.com/nuzar/termdoc/internal/httpx"
	"github.com/nuzar/termdoc/internal/render"
	"github.com/nuzar/termdoc/internal/store"
)

// App holds initialized dependencies for all commands.
type App struct {
	Home    string
	Config  *config.Config
	Store   *store.Store
	Client  *httpx.Client
	Out     io.Writer
	Err     io.Writer
	Color   bool
	Width   int
	Style   string
	NoPager bool
	Quiet   bool
}

func (a *App) termOptions() render.TermOptions {
	return render.TermOptions{
		Style:   a.Style,
		Width:   a.Width,
		Color:   a.Color,
		Pager:   a.Config.Pager,
		NoPager: a.NoPager,
		Out:     a.Out,
	}
}

func (a *App) paint(code, s string) string {
	if !a.Color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (a *App) bold(s string) string { return a.paint("1", s) }
func (a *App) dim(s string) string  { return a.paint("2", s) }
func (a *App) cyan(s string) string { return a.paint("36", s) }
func (a *App) green(s string) string {
	return a.paint("32", s)
}
func (a *App) yellow(s string) string { return a.paint("33", s) }

func pad(s string, w int) string {
	n := runewidth.StringWidth(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

func truncate(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "…")
}

// errOut returns the configured error writer.
func (a *App) errOut() io.Writer {
	if a.Err != nil {
		return a.Err
	}
	return os.Stderr
}

func (a *App) printf(format string, args ...any) {
	fmt.Fprintf(a.Out, format, args...)
}

func (a *App) eprintf(format string, args ...any) {
	fmt.Fprintf(a.errOut(), format, args...)
}

// infof prints an informational line to stderr unless quiet mode is enabled.
func (a *App) infof(format string, args ...any) {
	if a.Quiet {
		return
	}
	fmt.Fprintf(a.errOut(), format, args...)
}
