package cli

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build information, set via -ldflags at build time.
var (
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

// newVersionCmd reports the build information.
func newVersionCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.jsonOutput() {
				return json.NewEncoder(a.out).Encode(map[string]string{
					"version":   Version,
					"commit":    GitCommit,
					"buildDate": BuildDate,
					"goVersion": runtime.Version(),
					"platform":  runtime.GOOS + "/" + runtime.GOARCH,
				})
			}
			_, err := fmt.Fprintf(a.out, "kctx %s (commit: %s, built: %s, %s %s/%s)\n",
				Version, GitCommit, BuildDate, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return err
		},
	}
}
