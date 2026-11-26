package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nuzar/termdoc/internal/archive"
	"github.com/nuzar/termdoc/internal/model"
	"github.com/nuzar/termdoc/internal/source"
)

func newDocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "docs",
		Aliases: []string{"doc"},
		Short:   "Manage installed docsets",
		Long:    "List, install, import, export and remove documentation sets.",
	}
	cmd.AddCommand(
		newDocsListCmd(),
		newDocsInstallCmd(),
		newDocsImportCmd(),
		newDocsExportCmd(),
		newDocsRemoveCmd(),
		newDocsInfoCmd(),
		newDocsCleanCmd(),
	)
	return cmd
}

func newDocsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed or available docsets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			remote, _ := cmd.Flags().GetBool("remote")
			src, _ := cmd.Flags().GetString("source")
			query, _ := cmd.Flags().GetString("query")
			limit, _ := cmd.Flags().GetInt("limit")
			asJSON, _ := cmd.Flags().GetBool("json")
			refresh, _ := cmd.Flags().GetBool("refresh")
			if remote {
				return listRemote(cmd, src, query, limit, asJSON, refresh)
			}
			return listInstalled(query, asJSON)
		},
	}
	cmd.Flags().Bool("remote", false, "list docsets available for download")
	cmd.Flags().String("source", "all", "remote source: all, devdocs or dash")
	cmd.Flags().StringP("query", "q", "", "filter by name")
	cmd.Flags().IntP("limit", "n", 0, "maximum number of rows (0 = all)")
	cmd.Flags().Bool("json", false, "output JSON")
	cmd.Flags().Bool("refresh", false, "refresh the remote catalog cache")
	return cmd
}

func listInstalled(query string, asJSON bool) error {
	metas, err := app.Store.List()
	if err != nil {
		return err
	}
	q := strings.ToLower(query)
	var filtered []model.Meta
	for _, m := range metas {
		if q != "" && !strings.Contains(strings.ToLower(m.ID+" "+m.Title), q) {
			continue
		}
		filtered = append(filtered, m)
	}
	if asJSON {
		enc := json.NewEncoder(app.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(filtered)
	}
	if len(filtered) == 0 {
		if query != "" {
			return fmt.Errorf("no installed docset matches %q", query)
		}
		app.printf("No docsets installed.\n\nTry:\n  termdoc docs install go\n  termdoc docs import ./docs.tar.gz\n")
		return nil
	}

	idW, titleW, verW, srcW := 4, 5, 7, 6
	for _, m := range filtered {
		idW = maxInt(idW, len(m.ID))
		titleW = maxInt(titleW, len(m.Title))
		verW = maxInt(verW, len(m.Version))
		srcW = maxInt(srcW, len(string(m.Source)))
	}
	titleW = minInt(titleW, 40)
	verW = minInt(verW, 14)
	app.printf("%s  %s  %s  %8s  %9s  %s\n",
		app.dim(pad("ID", idW)), app.dim(pad("TITLE", titleW)), app.dim(pad("VERSION", verW)),
		app.dim("ENTRIES"), app.dim("SIZE"), app.dim("SOURCE"))
	for _, m := range filtered {
		app.printf("%s  %s  %s  %8d  %9s  %s\n",
			pad(m.ID, idW), pad(truncate(m.Title, titleW), titleW), pad(m.Version, verW),
			m.Entries, source.FormatSize(m.Bytes), m.Source)
	}
	return nil
}

func listRemote(cmd *cobra.Command, src, query string, limit int, asJSON, refresh bool) error {
	src = strings.ToLower(strings.TrimSpace(src))
	if src == "" {
		src = "all"
	}
	q := strings.ToLower(strings.TrimSpace(query))
	type row struct {
		Source  string `json:"source"`
		Name    string `json:"name"`
		Title   string `json:"title"`
		Version string `json:"version,omitempty"`
		Size    int64  `json:"size,omitempty"`
		rank    int
	}
	var rows []row

	if src == "all" || src == "devdocs" {
		docs, err := source.FetchDevDocsCatalog(cmd.Context(), app.Client, app.Store, refresh)
		if err != nil {
			if src != "all" {
				return err
			}
			app.eprintf("warning: %v\n", err)
		}
		for _, d := range docs {
			title := d.Name
			if d.Version != "" && !strings.Contains(title, d.Version) {
				title += " " + d.Version
			}
			rank := matchRank(d.Slug, title, q)
			if rank < 0 {
				continue
			}
			rows = append(rows, row{Source: "devdocs", Name: d.Slug, Title: title, Version: d.Version, Size: d.DBSize, rank: rank})
		}
	}
	if src == "all" || src == "dash" {
		docs, err := source.FetchDashCatalog(cmd.Context(), app.Client, app.Store, refresh)
		if err != nil {
			if src != "all" {
				return err
			}
			app.eprintf("warning: %v\n", err)
		}
		for _, d := range docs {
			version := ""
			if len(d.Versions) > 0 {
				version = d.Versions[0]
			}
			title := d.Title
			if title == "" {
				title = d.Name
			}
			rank := matchRank(d.Name, title, q)
			if rank < 0 {
				continue
			}
			rows = append(rows, row{Source: "dash", Name: d.Name, Title: title, Version: version, Size: d.Size, rank: rank})
		}
	}
	if src != "all" && src != "devdocs" && src != "dash" {
		return fmt.Errorf("unknown source %q (use all, devdocs or dash)", src)
	}
	if len(rows) == 0 {
		return fmt.Errorf("no remote docsets match %q", query)
	}
	// Exact matches first, then prefix matches, then substring matches.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].rank < rows[j].rank })
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	if asJSON {
		enc := json.NewEncoder(app.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	srcW, nameW, titleW := 6, 6, 5
	for _, r := range rows {
		srcW = maxInt(srcW, len(r.Source))
		nameW = maxInt(nameW, len(r.Name))
		titleW = maxInt(titleW, len(r.Title))
	}
	nameW = minInt(nameW, 30)
	titleW = minInt(titleW, 42)
	app.printf("%s  %s  %s  %s  %9s\n", app.dim(pad("SOURCE", srcW)), app.dim(pad("NAME", nameW)), app.dim(pad("TITLE", titleW)), app.dim(pad("VERSION", 10)), app.dim("SIZE"))
	for _, r := range rows {
		app.printf("%s  %s  %s  %s  %9s\n",
			pad(r.Source, srcW), pad(truncate(r.Name, nameW), nameW),
			pad(truncate(r.Title, titleW), titleW), pad(r.Version, 10), source.FormatSize(r.Size))
	}
	app.printf("\n%s\n", app.dim("Install with: termdoc docs install <name>  (add --source dash for the Dash/Zeal catalog)"))
	return nil
}

func newDocsInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "install <name>...",
		Aliases: []string{"add"},
		Short:   "Download and install docsets",
		Long: `Download docsets from the devdocs or Zeal/Dash catalog.

A local file or directory may also be given; it is treated like
"termdoc docs import <path>".`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, _ := cmd.Flags().GetString("source")
			force, _ := cmd.Flags().GetBool("force")
			refresh, _ := cmd.Flags().GetBool("refresh")
			noCache, _ := cmd.Flags().GetBool("no-cache")
			return installDocs(cmd, args, src, force, refresh, noCache)
		},
	}
	cmd.Flags().String("source", "auto", "catalog: auto, devdocs or dash")
	cmd.Flags().Bool("force", false, "replace an existing docset")
	cmd.Flags().Bool("refresh", false, "refresh the remote catalog cache")
	cmd.Flags().Bool("no-cache", false, "ignore cached downloads")
	return cmd
}

func installDocs(cmd *cobra.Command, names []string, src string, force, refresh, noCache bool) error {
	src = strings.ToLower(strings.TrimSpace(src))
	if src == "" {
		src = "auto"
	}
	opt := source.InstallOptions{Force: force, Refresh: refresh, NoCache: noCache}
	for _, name := range names {
		// Allow `docs install ./Go.tgz` as a shortcut for `docs import`.
		if fi, err := os.Stat(name); err == nil && (fi.IsDir() || fi.Mode().IsRegular()) {
			ids, err := archive.Import(cmd.Context(), app.Store, app.Client, name, force)
			if err != nil {
				return err
			}
			printImported(ids)
			continue
		}
		m, err := installOne(cmd, name, src, opt)
		if err != nil {
			return err
		}
		app.printf("%s %s %s\n", app.green("Installed"), app.bold(m.Title), app.dim("("+m.ID+")"))
		app.printf("  %d entries · %d pages · %s\n", m.Entries, m.Pages, source.FormatSize(m.Bytes))
	}
	return nil
}

// printImported reports docsets installed by an import operation.
func printImported(ids []string) {
	for _, id := range ids {
		m, err := app.Store.ResolveOne(id)
		if err != nil {
			app.printf("%s %s\n", app.green("Imported"), id)
			continue
		}
		app.printf("%s %s %s\n", app.green("Imported"), app.bold(m.Title), app.dim("("+m.ID+")"))
		app.printf("  %d entries · %d pages · %s\n", m.Entries, m.Pages, source.FormatSize(m.Bytes))
	}
}

func installOne(cmd *cobra.Command, name, src string, opt source.InstallOptions) (*model.Meta, error) {
	switch src {
	case "devdocs":
		docs, err := source.FetchDevDocsCatalog(cmd.Context(), app.Client, app.Store, opt.Refresh)
		if err != nil {
			return nil, err
		}
		doc, err := source.ResolveDevDocs(docs, name)
		if err != nil {
			return nil, err
		}
		app.infof("Resolved %q to %s (devdocs)\n", name, doc.Slug)
		return source.InstallDevDocs(cmd.Context(), app.Store, app.Client, doc, opt)
	case "dash":
		docs, err := source.FetchDashCatalog(cmd.Context(), app.Client, app.Store, opt.Refresh)
		if err != nil {
			return nil, err
		}
		doc, err := source.ResolveDash(docs, name)
		if err != nil {
			return nil, err
		}
		app.infof("Resolved %q to %s (dash)\n", name, doc.Name)
		return source.InstallDash(cmd.Context(), app.Store, app.Client, doc, opt)
	case "auto":
		docs, err := source.FetchDevDocsCatalog(cmd.Context(), app.Client, app.Store, opt.Refresh)
		if err == nil {
			if doc, rerr := source.ResolveDevDocs(docs, name); rerr == nil {
				app.infof("Resolved %q to %s (devdocs)\n", name, doc.Slug)
				return source.InstallDevDocs(cmd.Context(), app.Store, app.Client, doc, opt)
			}
		}
		dashDocs, derr := source.FetchDashCatalog(cmd.Context(), app.Client, app.Store, opt.Refresh)
		if derr != nil {
			if err != nil {
				return nil, err
			}
			return nil, derr
		}
		doc, rerr := source.ResolveDash(dashDocs, name)
		if rerr != nil {
			if err != nil {
				return nil, fmt.Errorf("%v; %v", err, rerr)
			}
			return nil, rerr
		}
		app.infof("Resolved %q to %s (dash)\n", name, doc.Name)
		return source.InstallDash(cmd.Context(), app.Store, app.Client, doc, opt)
	default:
		return nil, fmt.Errorf("unknown source %q (use auto, devdocs or dash)", src)
	}
}

func newDocsImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "import <path>...",
		Aliases: []string{"load"},
		Short:   "Import docsets from local files or directories",
		Long: `Import documentation that you copied into this environment.

Supported inputs:
  - a termdoc export (.tar.gz)
  - a Dash/Zeal docset (.docset directory, .tgz, .tar.gz or .zip)
  - a termdoc docset directory
  - "-" to read an archive from standard input`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			force, _ := cmd.Flags().GetBool("force")
			for _, path := range args {
				ids, err := archive.Import(cmd.Context(), app.Store, app.Client, path, force)
				if err != nil {
					return err
				}
				printImported(ids)
			}
			return nil
		},
	}
	cmd.Flags().Bool("force", false, "replace existing docsets")
	return cmd
}

func newDocsExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export [name...]",
		Short: "Export docsets into a portable archive",
		Long: `Export installed docsets into a single .tar.gz that can be copied to an
offline machine and installed with "termdoc docs import".

Use -o - to write the archive to stdout.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			all, _ := cmd.Flags().GetBool("all")
			out, _ := cmd.Flags().GetString("output")
			var ids []string
			if all {
				metas, err := app.Store.List()
				if err != nil {
					return err
				}
				for _, m := range metas {
					ids = append(ids, m.ID)
				}
			} else {
				if len(args) == 0 {
					return fmt.Errorf("specify docsets to export, or use --all")
				}
				ids = args
			}
			if len(ids) == 0 {
				return fmt.Errorf("no docsets to export")
			}
			if out == "" {
				out = "termdoc-docs-" + time.Now().Format("20060102") + ".tar.gz"
			}
			if out == "-" {
				_, err := archive.Export(app.Store, ids, app.Out)
				return err
			}
			f, err := os.Create(out)
			if err != nil {
				return err
			}
			names, err := archive.Export(app.Store, ids, f)
			cerr := f.Close()
			if err != nil {
				os.Remove(out)
				return err
			}
			if cerr != nil {
				return cerr
			}
			fi, _ := os.Stat(out)
			size := int64(0)
			if fi != nil {
				size = fi.Size()
			}
			app.printf("%s %s\n", app.green("Exported"), strings.Join(names, ", "))
			app.printf("  %s · %s\n", out, source.FormatSize(size))
			return nil
		},
	}
	cmd.Flags().Bool("all", false, "export every installed docset")
	cmd.Flags().StringP("output", "o", "", "output file (- for stdout)")
	return cmd
}

func newDocsRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <name>...",
		Aliases: []string{"rm", "uninstall", "delete"},
		Short:   "Remove installed docsets",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, name := range args {
				ms, err := app.Store.Resolve(name)
				if err != nil {
					return err
				}
				if len(ms) > 1 {
					var ids []string
					for _, m := range ms {
						ids = append(ids, m.ID)
					}
					return fmt.Errorf("%q is ambiguous: %s", name, strings.Join(ids, ", "))
				}
				m := ms[0]
				if err := app.Store.Remove(m.ID); err != nil {
					return err
				}
				app.printf("%s %s %s\n", app.green("Removed"), app.bold(m.Title), app.dim("("+m.ID+")"))
			}
			return nil
		},
	}
	return cmd
}

func newDocsInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info [name]",
		Short: "Show details about a docset",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			if len(args) == 0 {
				return showHome(asJSON)
			}
			m, err := app.Store.ResolveOne(args[0])
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(app.Out)
				enc.SetIndent("", "  ")
				return enc.Encode(m)
			}
			app.printf("ID:          %s\n", m.ID)
			app.printf("Title:       %s\n", m.Title)
			app.printf("Source:      %s\n", m.Source)
			if m.Version != "" {
				app.printf("Version:     %s\n", m.Version)
			}
			if m.Release != "" {
				app.printf("Release:     %s\n", m.Release)
			}
			if m.Homepage != "" {
				app.printf("Homepage:    %s\n", m.Homepage)
			}
			app.printf("Entries:     %d\n", m.Entries)
			app.printf("Pages:       %d\n", m.Pages)
			app.printf("Size:        %s\n", source.FormatSize(m.Bytes))
			app.printf("Installed:   %s\n", m.Installed.Local().Format("2006-01-02 15:04"))
			app.printf("Path:        %s\n", app.Store.Dir(m.ID))
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "output JSON")
	return cmd
}

func showHome(asJSON bool) error {
	metas, err := app.Store.List()
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(app.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"home":    app.Home,
			"config":  filepath.Join(app.Home, "config.toml"),
			"docsets": metas,
		})
	}
	app.printf("Home:    %s\n", app.Home)
	app.printf("Config:  %s\n", filepath.Join(app.Home, "config.toml"))
	app.printf("Docsets: %s\n", app.Store.DocsetsDir())
	app.printf("Cache:   %s\n", app.Store.CacheDir())
	app.printf("Installed: %d docset(s), %d entries\n", len(metas), totalEntries(metas))
	return nil
}

func newDocsCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Delete downloaded archives and temporary files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var freed int64
			for _, dir := range []string{app.Store.CacheDir(), app.Store.TempDir()} {
				entries, err := os.ReadDir(dir)
				if err != nil {
					continue
				}
				for _, e := range entries {
					path := filepath.Join(dir, e.Name())
					if info, err := os.Stat(path); err == nil && !info.IsDir() {
						freed += info.Size()
					}
					os.RemoveAll(path)
				}
				os.MkdirAll(dir, 0o755)
			}
			app.printf("%s %s\n", app.green("Cleaned"), source.FormatSize(freed))
			return nil
		},
	}
	return cmd
}

func totalEntries(ms []model.Meta) int {
	n := 0
	for _, m := range ms {
		n += m.Entries
	}
	return n
}

// matchRank scores a catalog entry against a lowercase query. Lower is better;
// -1 means no match.
func matchRank(name, title, q string) int {
	if q == "" {
		return 0
	}
	n, t := strings.ToLower(name), strings.ToLower(title)
	switch {
	case n == q || t == q:
		return 0
	case strings.HasPrefix(n, q) || strings.HasPrefix(t, q):
		return 1
	case strings.Contains(n, q) || strings.Contains(t, q):
		return 2
	}
	return -1
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
