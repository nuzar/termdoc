package cli

import (
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app.printf("termdoc  %s\n", Version)
			app.printf("go       %s\n", runtime.Version())
			app.printf("platform %s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
