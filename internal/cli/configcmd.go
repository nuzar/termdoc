package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/nuzar/termdoc/internal/config"
)

const configTemplate = `# termdoc configuration

# Proxy used for downloading docsets. Standard HTTPS_PROXY/HTTP_PROXY
# environment variables are also honored.
# proxy = "http://127.0.0.1:50000"

# Pager for long documents. Empty means auto (less, then more).
# pager = "less -R"

# Document style: auto, dark, light, notty, ascii, dracula, tokyo-night.
# theme = "auto"

# Wrap width in columns. 0 means use the terminal width.
# width = 0

# Disable the pager.
# no_pager = false
`

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show paths and configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			initFile, _ := cmd.Flags().GetBool("init")
			force, _ := cmd.Flags().GetBool("force")
			path := config.Path(app.Home)
			if initFile {
				if _, err := os.Stat(path); err == nil && !force {
					return fmt.Errorf("%s already exists (use --force to overwrite)", path)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(path, []byte(configTemplate), 0o644); err != nil {
					return err
				}
				app.printf("%s %s\n", app.green("Wrote"), path)
				return nil
			}
			app.printf("Home:        %s\n", app.Home)
			app.printf("Config:      %s\n", path)
			app.printf("Docsets:     %s\n", app.Store.DocsetsDir())
			app.printf("Cache:       %s\n", app.Store.CacheDir())
			app.printf("Proxy:       %s\n", orDash(app.Config.ProxyValue()))
			app.printf("Theme:       %s\n", app.Style)
			app.printf("Width:       %d\n", app.Width)
			app.printf("Pager:       %s\n", orDash(app.Config.Pager))
			app.printf("Paging:      %s\n", enabledText(!app.NoPager))
			return nil
		},
	}
	cmd.Flags().Bool("init", false, "write a commented config.toml template")
	cmd.Flags().Bool("force", false, "overwrite an existing config.toml with --init")
	return cmd
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func enabledText(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
