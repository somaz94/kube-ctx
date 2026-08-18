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
			"Exits 2 when anything falls inside the window — or when a cluster\n" +
			"could not be read at all, since a sweep that reached nothing has not\n" +
			"established that nothing is wrong there. So it can gate a cron:\n" +
			"  kctx expiry --days 30 || notify-oncall\n\n" +
			"--all widens what is shown, never what counts as due: the exit status\n" +
			"stays keyed to --days.",
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
	cmd.Flags().BoolVar(&all, "all", false, "show every certificate expiring within 200 years")
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

	if opts.days < 0 {
		return fmt.Errorf("--days must not be negative; %d would report only what has already expired", opts.days)
	}

	names, err = resolveContexts(a, cfg, names)
	if err != nil {
		return err
	}
	// Two spellings of one context sweep it twice and print it twice; aliases
	// resolving here make that easy to do by accident.
	names = dedupe(names)

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

	// The window that counts as due is always --days. --all only widens what
	// is shown; letting it widen the threshold too would exit 2 on any cluster
	// holding a single TLS secret, which is not what "something is due" means.
	due := expiry.Within(results, now, opts.days)

	shown := due
	if opts.all {
		shown = expiry.Within(results, now, everything)
	}

	if a.jsonOutput() {
		if err := writeJSON(a, shown); err != nil {
			return err
		}
	} else if err := renderExpiryTable(a, cfg.CurrentContext, shown, now, opts); err != nil {
		return err
	}

	// Over the unfiltered results, and outside the output branch: the warning
	// is about what could be read, not about the window or the format, and a
	// later display-side filter must not be able to take it away.
	if err := reportSkipped(a, results); err != nil {
		return err
	}

	// Two ways to be non-zero, and both have to be: something is due, or a
	// cluster could not be read at all. A sweep that reached nothing has not
	// established that nothing is wrong there, and "kctx expiry || notify"
	// going quiet when every cluster is unreachable is the failure mode this
	// command exists to prevent.
	// Unknown reads the unfiltered results on purpose: it is a statement about
	// what could be read, not about the window. Keyed to the display slice, a
	// later display-side filter would silently take the cron gate with it.
	if expiry.Expiring(due) || expiry.Unknown(results) {
		// The same separation doctor makes: 2 is "the clusters answered and
		// something needs doing", distinct from 1, which is kube-ctx failing
		// to run at all. Silent, because the table already said what.
		return &exitError{code: ExitUnhealthy}
	}
	return nil
}

// renewalGrace is how long past its own renewalTime a Certificate is given
// before the renewal is called failed.
//
// cert-manager sets renewalTime to when it will start, not when it will
// finish, so a DNS-01 order legitimately leaves it in the past for a while.
// Without the grace, every healthy renewal in flight renders as the most
// urgent row on the page, and a marker that cries wolf stops being read.
const renewalGrace = 6 * time.Hour

// everything is the window --all uses. Deliberately absurd rather than
// unbounded: Within takes a day count, and a second code path for "no filter"
// would be one more thing to keep in step with the first.
//
// It is a real bound, not a synonym for infinity. A certificate carrying RFC
// 5280's "no well-defined expiry" (9999-12-31) falls outside it, which is the
// better failure: time.Duration saturates past ~292 years, so such a row would
// render a nonsense day count if it were let through.
const everything = 200 * 365

// renderExpiryTable prints one row per expiring certificate.
func renderExpiryTable(a *app, current string, results []expiry.Result, now time.Time, opts expiryOptions) error {
	if len(results) == 0 {
		if opts.all {
			_, err := fmt.Fprintln(a.errOut, "No certificates found.")
			return err
		}
		_, err := fmt.Fprintf(a.errOut, "Nothing expires within %d days.\n", opts.days)
		return err
	}

	pal := a.palette()
	rows := make([][]string, 0)
	for _, r := range results {
		// A context that established nothing gets a row saying so. Without
		// this a refused secrets list rendered as a header and no rows, while
		// exiting 2 — the exit status called it unreadable and the table
		// showed a clean sweep.
		if reason := unreadable(r); reason != "" {
			rows = append(rows, []string{
				boldIfCurrent(pal, r.Context, current), "-", "-",
				pal.Red("unreadable"), pal.Dim(trimError(reason, 40)),
			})
			continue
		}
		for _, item := range r.Items {
			rows = append(rows, []string{
				boldIfCurrent(pal, r.Context, current),
				item.Namespace,
				string(item.Kind),
				item.Name,
				expiryCell(pal, item, now),
			})
		}
	}

	return renderOutput(a, []string{"CONTEXT", "NAMESPACE", "KIND", "NAME", "IN"}, rows, results)
}

// expiryCell renders the time left, colored by how much of it there is.
//
// A managed certificate is dimmed rather than colored: cert-manager renewing
// next Tuesday is not something anyone has to do, and a report where every row
// is red is one nobody reads twice.
func expiryCell(pal render.Palette, item expiry.Item, now time.Time) string {
	if item.Expired(now) {
		if item.Managed() {
			// The same distinction (auto, overdue) draws, at the end it runs
			// to: cert-manager owns this one and let it die, which is a
			// different fix from a secret nobody ever automated.
			return pal.Red("expired (auto)")
		}
		return pal.Red("expired")
	}

	left := item.In(now)
	text := fmt.Sprintf("%dd", int(left.Hours()/24))
	switch {
	// Dimmed only while cert-manager's own schedule is still ahead of it.
	// Renewals do fail — DNS-01 broken, an ACME rate limit, an issuer deleted
	// — and a renewal date that has passed with the certificate still here is
	// the most urgent row on the page, not the quietest.
	case item.Managed() && (item.RenewalTime == nil || now.Sub(*item.RenewalTime) < renewalGrace):
		return pal.Dim(text + " (auto)")
	case item.Managed():
		// cert-manager meant to renew this and the date went by. Saying so is
		// the point: rendered as a plain countdown it is indistinguishable
		// from a certificate nobody ever automated, and the fix is different.
		return pal.Red(text + " (auto, overdue)")
	case left < 7*24*time.Hour:
		return pal.Red(text)
	default:
		return pal.Yellow(text)
	}
}

// unreadable reports why a context established nothing, or "" when it did.
func unreadable(r expiry.Result) string {
	if r.Err != "" {
		return r.Err
	}
	for _, skip := range r.Skipped {
		if skip.Blind {
			return skip.String()
		}
	}
	return ""
}

// reportSkipped names what could not be read, so a quiet report is never
// mistaken for a clean one.
func reportSkipped(a *app, results []expiry.Result) error {
	for _, r := range results {
		for _, skip := range r.Skipped {
			_, err := fmt.Fprintf(a.errOut,
				"warning: %s: could not read %s, so this context was not fully checked\n",
				r.Context, trimError(skip.String(), 120))
			if err != nil {
				return err
			}
		}
	}
	return nil
}
