package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/expiry"
	"github.com/somaz94/kube-ctx/pkg/render"
)

// expiryFetch reads one cluster. A variable so tests can drive the whole
// command without an API server, the way newPicker and runCommand are.
var expiryFetch = expiry.Live

// timeNow is the clock the report is rendered against, replaced in tests so a
// fixture certificate does not have to be regenerated to stay in the window.
var timeNow = time.Now

// newExpiryCmd reports the certificates about to run out, across contexts.
func newExpiryCmd(a *app) *cobra.Command {
	var (
		days        int
		timeout     time.Duration
		concurrency int
		all         bool
	)

	cmd := &cobra.Command{
		Use:     "expiry [context...]",
		Aliases: []string{"expire", "certs"},
		Short:   "Report the certificates expiring soon, across every context",
		Long: "Sweep every cluster for TLS certificates that are about to run out.\n\n" +
			"Every kubernetes.io/tls secret is read straight from its PEM, so this\n" +
			"works on a cluster with no cert-manager installed and makes no guess\n" +
			"about what issued anything. Where cert-manager is present its\n" +
			"Certificates are folded in, which is what says who will renew a\n" +
			"certificate and when — the difference between a row you can ignore\n" +
			"and one somebody has to act on.\n\n" +
			"This is not doctor. doctor asks whether a cluster works right now and\n" +
			"calls a sick one a failure; nothing here is broken yet, which is the\n" +
			"whole point of being told about it.\n\n" +
			"Exits 2 when anything falls inside the window, so it can gate a cron:\n" +
			"  kctx expiry --days 30 || notify-oncall",
		ValidArgsFunction: completeContextList(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExpiry(a, args, expiryOptions{
				days:        days,
				timeout:     timeout,
				concurrency: concurrency,
				all:         all,
			})
		},
	}
	cmd.Flags().IntVarP(&days, "days", "d", expiry.DefaultDays, "report certificates expiring within this many days")
	cmd.Flags().BoolVar(&all, "all", false, "report every certificate, however far off")
	cmd.Flags().DurationVar(&timeout, "timeout", expiry.DefaultTimeout, "per-cluster deadline")
	cmd.Flags().IntVar(&concurrency, "concurrency", expiry.DefaultConcurrency, "how many clusters to read at once")
	return cmd
}

// expiryOptions holds the flags of the expiry command.
type expiryOptions struct {
	days        int
	timeout     time.Duration
	concurrency int
	all         bool
}

// runExpiry sweeps the requested contexts and renders the report.
func runExpiry(a *app, names []string, opts expiryOptions) error {
	loader := a.loader()
	cfg, err := loader.Load()
	if err != nil {
		return err
	}
	if len(cfg.Contexts) == 0 {
		_, err := fmt.Fprintln(a.errOut, "No contexts found in the kubeconfig.")
		return err
	}

	names, err = resolveContexts(a, cfg, names)
	if err != nil {
		return err
	}

	sweeper := &expiry.Sweeper{
		Fetch:       expiryFetch,
		RestConfig:  loader.RestConfig,
		Concurrency: opts.concurrency,
		Timeout:     opts.timeout,
	}

	ctx, cancel := contextWithTimeout(0)
	defer cancel()

	results := sweeper.Run(ctx, cfg, names)

	now := timeNow()
	days := opts.days
	if opts.all {
		// Far enough out that "--all" means all without a second code path.
		days = 100 * 365
	}
	results = expiry.Within(results, now, days)

	if a.jsonOutput() {
		if err := writeJSON(a, results); err != nil {
			return err
		}
	} else if err := renderExpiryTable(a, cfg.CurrentContext, results, now, opts); err != nil {
		return err
	}

	if expiry.Expiring(results) {
		// The same separation doctor makes: 2 is "the clusters answered and
		// something needs doing", distinct from 1, which is kube-ctx failing
		// to run at all. Silent, because the table already said what.
		return &exitError{code: ExitUnhealthy}
	}
	return nil
}

// renderExpiryTable prints one row per expiring certificate.
func renderExpiryTable(a *app, current string, results []expiry.Result, now time.Time, opts expiryOptions) error {
	if len(results) == 0 {
		_, err := fmt.Fprintf(a.errOut, "Nothing expires within %d days.\n", opts.days)
		return err
	}

	pal := a.palette()
	rows := make([][]string, 0)
	for _, r := range results {
		if r.Err != "" {
			rows = append(rows, []string{
				contextCell(pal, r.Context, current), "-", "-",
				pal.Red("unreadable"), pal.Dim(trimTo(r.Err, 40)),
			})
			continue
		}
		for _, item := range r.Items {
			rows = append(rows, []string{
				contextCell(pal, r.Context, current),
				item.Namespace,
				string(item.Kind),
				item.Name,
				expiryCell(pal, item, now),
			})
		}
	}

	if err := renderOutput(a, []string{"CONTEXT", "NAMESPACE", "KIND", "NAME", "IN"}, rows, results); err != nil {
		return err
	}
	return reportSkipped(a, results)
}

// contextCell bolds the context the user is currently on.
func contextCell(pal render.Palette, name, current string) string {
	if name == current {
		return pal.Bold(name)
	}
	return name
}

// expiryCell renders the time left, colored by how much of it there is.
//
// A managed certificate is dimmed rather than colored: cert-manager renewing
// next Tuesday is not something anyone has to do, and a report where every row
// is red is one nobody reads twice.
func expiryCell(pal render.Palette, item expiry.Item, now time.Time) string {
	if item.Expired(now) {
		return pal.Red("expired")
	}

	left := item.In(now)
	text := fmt.Sprintf("%dd", int(left.Hours()/24))
	switch {
	case item.Managed():
		return pal.Dim(text + " (auto)")
	case left < 7*24*time.Hour:
		return pal.Red(text)
	default:
		return pal.Yellow(text)
	}
}

// reportSkipped names what the credential was not allowed to read, so a quiet
// report is never mistaken for a clean one.
func reportSkipped(a *app, results []expiry.Result) error {
	for _, r := range results {
		for _, kind := range r.Skipped {
			_, err := fmt.Fprintf(a.errOut,
				"warning: %s: not allowed to read %s, so this context is only partly checked\n",
				r.Context, kind)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// trimTo shortens a message to fit a table cell.
func trimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
