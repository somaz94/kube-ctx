package cli

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/probe"
	"github.com/somaz94/kube-ctx/pkg/render"
)

// newDoctorCmd checks every context for reachability and credential health.
func newDoctorCmd(a *app) *cobra.Command {
	var (
		timeout     time.Duration
		concurrency int
		unhealthy   bool
	)

	cmd := &cobra.Command{
		Use:   "doctor [context...]",
		Short: "Check every context for reachability and credential health",
		Long: "Contact each cluster in parallel and report what is wrong.\n\n" +
			"Checks that need no network — dangling cluster or user references, an\n" +
			"expired client certificate, an expired token — are reported even when\n" +
			"the cluster cannot be reached at all.\n\n" +
			"Exits non-zero when any probed context is unhealthy, so it can gate a\n" +
			"script.",
		ValidArgsFunction: completeContextList(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(a, args, timeout, concurrency, unhealthy)
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", probe.DefaultTimeout, "per-cluster deadline")
	cmd.Flags().IntVar(&concurrency, "concurrency", probe.DefaultConcurrency, "how many clusters to contact at once")
	cmd.Flags().BoolVar(&unhealthy, "unhealthy", false, "only report contexts with a problem")
	return cmd
}

// runDoctor probes the requested contexts and renders the report.
func runDoctor(a *app, names []string, timeout time.Duration, concurrency int, onlyUnhealthy bool) error {
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

	prober := &probe.Prober{
		RestConfig:  loader.RestConfig,
		Concurrency: concurrency,
		Timeout:     timeout,
	}

	ctx, cancel := contextWithTimeout(0)
	defer cancel()

	results := prober.Run(ctx, cfg, names)
	if onlyUnhealthy {
		filtered := results[:0]
		for _, r := range results {
			if !r.Healthy() {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if a.jsonOutput() {
		if err := writeJSON(a, results); err != nil {
			return err
		}
	} else if err := renderDoctorTable(a, cfg.CurrentContext, results); err != nil {
		return err
	}

	for _, r := range results {
		if !r.Healthy() {
			// A non-zero exit makes the command usable as a check in a script,
			// and a code of its own separates "a cluster is sick" from "kctx
			// could not run" — otherwise "kctx doctor prod || page" fires the
			// same way for an unreachable cluster and a typo in --kubeconfig.
			// The error is silent because the table already said what is wrong.
			return &exitError{code: ExitUnhealthy}
		}
	}
	return nil
}

// renderDoctorTable prints one row per probed context.
func renderDoctorTable(a *app, current string, results []probe.Result) error {
	if len(results) == 0 {
		_, err := fmt.Fprintln(a.errOut, "Nothing to report.")
		return err
	}

	pal := a.palette()
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		name := contextCell(pal, r.Context, current)

		status := pal.Green("ok")
		switch {
		case len(r.Issues) > 0:
			status = pal.Red("broken")
		case !r.Reachable:
			status = pal.Red("unreachable")
		}

		version := r.ServerVersion
		if version == "" {
			version = "-"
		}
		latency := "-"
		if r.Latency > 0 {
			latency = r.Latency.Round(time.Millisecond).String()
		}

		rows = append(rows, []string{
			status, name, version, latency, describeAuth(pal, r), detail(pal, r),
		})
	}
	return renderTable(a, []string{"STATUS", "CONTEXT", "VERSION", "LATENCY", "AUTH", "NOTES"}, rows)
}

// describeAuth renders the credential type and how close it is to expiring.
func describeAuth(pal render.Palette, r probe.Result) string {
	text := string(r.Auth)
	if r.AuthDetail != "" && r.Auth == probe.AuthExec {
		text += ":" + r.AuthDetail
	}
	if r.Expiry == nil {
		return text
	}

	remaining := time.Until(*r.Expiry)
	switch {
	case remaining <= 0:
		return text + " " + pal.Red("(expired)")
	case remaining < 7*24*time.Hour:
		return text + " " + pal.Yellow(fmt.Sprintf("(%s left)", roundDuration(remaining)))
	default:
		return text + pal.Dim(fmt.Sprintf(" (%s left)", roundDuration(remaining)))
	}
}

// detail renders the issue list, falling back to the connection error.
func detail(pal render.Palette, r probe.Result) string {
	if len(r.Issues) > 0 {
		return pal.Red(strings.Join(r.Issues, "; "))
	}
	if r.Err != "" {
		return pal.Dim(r.Err)
	}
	return ""
}

// roundDuration renders a duration at a granularity a human cares about.
//
// It rounds rather than truncates: a credential with 1h59m59s left reads as
// "2h", not the "1h" a truncating conversion would report.
func roundDuration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(math.Round(d.Hours()/24)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(math.Round(d.Hours())))
	default:
		return fmt.Sprintf("%dm", int(math.Round(d.Minutes())))
	}
}
