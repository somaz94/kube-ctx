package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// newSessionsCmd lists and cleans up the per-terminal kubeconfig copies.
func newSessionsCmd(a *app) *cobra.Command {
	var (
		clean bool
		all   bool
	)

	cmd := &cobra.Command{
		Use:     "sessions",
		Aliases: []string{"session"},
		Short:   "List the per-terminal kubeconfig copies",
		Long: "List the private kubeconfig copies kube-ctx keeps for managed shells.\n\n" +
			"One is created the first time a terminal with the shell hook switches\n" +
			"context, or by \"kctx shell\", which removes its own on exit. A hooked\n" +
			"terminal's copy outlives the terminal, since a tmux server or editor\n" +
			"started there may still be reading it, and each one holds every cluster\n" +
			"and credential in your kubeconfig — so knowing what is there matters.\n\n" +
			"  kctx sessions            list them\n" +
			"  kctx sessions --clean    remove the ones nothing has used in a week\n" +
			"  kctx sessions --clean --all\n" +
			"                           remove every one but this shell's\n\n" +
			"Age is time since last use, not since creation: every kube-ctx command\n" +
			"run in a session refreshes it, so a terminal open for a month is not\n" +
			"mistaken for an abandoned one. Copies idle for a week are also swept\n" +
			"automatically whenever a terminal starts a session.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessions(a, clean, all)
		},
	}
	cmd.Flags().BoolVar(&clean, "clean", false, "remove sessions instead of listing them")
	cmd.Flags().BoolVar(&all, "all", false, "with --clean, remove every session but this shell's")
	return cmd
}

// runSessions lists the sessions, or removes them when asked.
func runSessions(a *app, clean, all bool) error {
	sessions, err := shellenv.List()
	if err != nil {
		return err
	}
	if clean {
		return cleanSessions(a, sessions, all)
	}

	if len(sessions) == 0 && !a.jsonOutput() {
		_, err := fmt.Fprintln(a.errOut, "No shell sessions.")
		return err
	}

	pal := a.palette()
	rows := make([][]string, 0, len(sessions))
	for _, s := range sessions {
		marker := " "
		if s.Current {
			marker = pal.Green("*")
		}
		rows = append(rows, []string{marker, s.ID, s.Context, humanAge(time.Since(s.LastUsed))})
	}
	return renderOutput(a, []string{"", "ID", "CONTEXT", "LAST USED"}, rows, sessions)
}

// cleanSessions removes idle session copies, or with all every one but this shell's.
func cleanSessions(a *app, sessions []shellenv.Info, all bool) error {
	removed := 0
	for _, s := range sessions {
		// Never this shell's own: $KUBECONFIG points at it, and removing it
		// would break every kubectl in the terminal asking for the cleanup.
		if s.Current {
			continue
		}
		if !all && time.Since(s.LastUsed) < shellenv.DefaultMaxAge {
			continue
		}
		session := shellenv.Session{ID: s.ID, Path: s.Path}
		if err := session.Remove(); err != nil {
			return err
		}
		removed++
	}

	if a.jsonOutput() {
		return writeJSON(a, map[string]int{"removed": removed})
	}
	_, err := fmt.Fprintf(a.out, "Removed %d session(s).\n", removed)
	return err
}

// humanAge renders a duration the way a table wants it: one unit, no decimals.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
