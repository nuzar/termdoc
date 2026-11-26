package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nuzar/termdoc/internal/config"
	"github.com/nuzar/termdoc/internal/httpx"
	"github.com/nuzar/termdoc/internal/render"
	"github.com/nuzar/termdoc/internal/store"
)

// Version is the build version, overridable with -ldflags.
var Version = "0.1.0"

var global struct {
	home    string
	proxy   string
	style   string
	width   int
	noColor bool
	noPager bool
	quiet   bool
}

var app *App

// Execute runs the CLI.
func Execute() error {
	root := newRootCmd()
	root.SilenceUsage = true
	root.SilenceErrors = true
	err := root.Execute()
	if err != nil && err.Error() != "" {
		fmt.Fprintln(os.Stderr, "termdoc:", err)
	}
	return err
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "termdoc [query]",
		Short: "Offline programming documentation in your terminal",
		Long: `termdoc is a small, offline documentation browser for the terminal.

Install docsets from devdocs.io or the Zeal/Dash catalog, or import a docset
archive that you copied into your environment. Then search and read docs
without any network access.

Examples:
  termdoc docs install go python~3.13
  termdoc docs install --source dash Go
  termdoc docs import ./Go.tgz
  termdoc search fmt.Println
  termdoc show fmt.Println
  termdoc docs export --all -o docs.tar.gz`,
		Version: Version,
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return runSearch(cmd, strings.Join(args, " "))
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initApp(cmd)
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&global.home, "home", "", "data directory (default $TERMDOC_HOME or platform data dir)")
	pf.StringVar(&global.proxy, "proxy", "", "HTTP/SOCKS5 proxy for downloads (e.g. http://127.0.0.1:50000)")
	pf.StringVar(&global.style, "style", "", "document style: auto, dark, light, notty")
	pf.IntVar(&global.width, "width", 0, "wrap width (0 = terminal width)")
	pf.BoolVar(&global.noColor, "no-color", false, "disable colors")
	pf.BoolVar(&global.noPager, "no-pager", false, "do not use a pager")
	pf.BoolVar(&global.quiet, "quiet", false, "suppress download progress")

	root.AddCommand(
		newSearchCmd(),
		newShowCmd(),
		newDocsCmd(),
		newConfigCmd(),
		newVersionCmd(),
	)
	return root
}

func initApp(cmd *cobra.Command) error {
	home := global.home
	if home == "" {
		h, err := config.Home()
		if err != nil {
			return err
		}
		home = h
	}
	cfg, err := config.Load(home)
	if err != nil {
		return err
	}
	if f := cmd.Flags().Lookup("style"); f != nil && f.Changed {
		cfg.Theme = global.style
	}
	if f := cmd.Flags().Lookup("width"); f != nil && f.Changed {
		cfg.Width = global.width
	}
	if cfg.Theme == "" {
		cfg.Theme = "auto"
	}

	st, err := store.Open(home)
	if err != nil {
		return err
	}

	proxy := cfg.ProxyValue()
	if f := cmd.Flags().Lookup("proxy"); f != nil && f.Changed {
		proxy = global.proxy
	}
	client, err := httpx.New(proxy)
	if err != nil {
		return err
	}
	client.Quiet = global.quiet || !render.IsTTY(os.Stderr)

	color := !global.noColor && os.Getenv("NO_COLOR") == "" && render.IsTTY(os.Stdout)
	width := cfg.Width
	if width <= 0 {
		width = render.TerminalWidth(100) - 2
		if width > 120 {
			width = 120
		}
		if width < 40 {
			width = 40
		}
	}
	app = &App{
		Home:    home,
		Config:  cfg,
		Store:   st,
		Client:  client,
		Out:     os.Stdout,
		Err:     os.Stderr,
		Color:   color,
		Width:   width,
		Style:   cfg.Theme,
		NoPager: global.noPager || cfg.NoPager,
		Quiet:   global.quiet || !render.IsTTY(os.Stderr),
	}
	return nil
}
