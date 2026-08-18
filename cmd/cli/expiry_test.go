package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"k8s.io/client-go/rest"

	"github.com/somaz94/kube-ctx/pkg/expiry"
)

// fixedNow pins the clock so a fixture certificate stays where the test put it.
func fixedNow(t *testing.T) time.Time {
	t.Helper()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	original := timeNow
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = original })
	return now
}

// stubExpiry replaces the cluster read, the way newHarness stubs the picker.
func stubExpiry(t *testing.T, items map[string][]expiry.Item, skipped []expiry.Skip) {
	t.Helper()
	original := expiryFetch
	// The sweeper hands the fetcher only a rest.Config, so the per-context
	// answer is keyed off the host it was built for.
	expiryFetch = func(_ context.Context, rc *rest.Config) ([]expiry.Item, []expiry.Skip, error) {
		// A copy: sweepOne stamps the context onto each item, and two
		// contexts pointing at one host would otherwise race on the map's
		// own slice.
		return append([]expiry.Item(nil), items[rc.Host]...), skipped, nil
	}
	t.Cleanup(func() { expiryFetch = original })
}

func TestExpiryReportsWhatIsInTheWindow(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "istio", Kind: expiry.KindCertificate, Name: "gw", NotAfter: now.AddDate(0, 0, 8), Issuer: "letsencrypt"},
			{Namespace: "default", Kind: expiry.KindTLSSecret, Name: "far-off", NotAfter: now.AddDate(1, 0, 0)},
		},
	}, nil)

	err := h.run("expiry", "dev")
	if code := ExitCode(err); code != ExitUnhealthy {
		t.Fatalf("ExitCode = %d, want %d; something expires inside the window", code, ExitUnhealthy)
	}

	out := h.stdout()
	if !strings.Contains(out, "gw") || !strings.Contains(out, "8d") {
		t.Errorf("stdout = %q, want the expiring certificate", out)
	}
	// Beyond the window is not this report's business.
	if strings.Contains(out, "far-off") {
		t.Errorf("stdout included a certificate outside the window: %q", out)
	}
}

// Nothing expiring is a success, or the command cannot gate a cron.
func TestExpiryExitsZeroWhenNothingIsDue(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "default", Kind: expiry.KindTLSSecret, Name: "healthy", NotAfter: now.AddDate(1, 0, 0)},
		},
	}, nil)

	if err := h.run("expiry", "dev"); err != nil {
		t.Fatalf("expiry: %v", err)
	}
	if !strings.Contains(h.stderr(), "Nothing expires within 30 days") {
		t.Errorf("stderr = %q", h.stderr())
	}
}

// An already-expired certificate is the most urgent row on the page.
func TestExpiryReportsTheAlreadyExpired(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "ns", Kind: expiry.KindTLSSecret, Name: "dead", NotAfter: now.AddDate(0, 0, -3)},
		},
	}, nil)

	err := h.run("expiry", "dev")
	if code := ExitCode(err); code != ExitUnhealthy {
		t.Errorf("ExitCode = %d, want %d", code, ExitUnhealthy)
	}
	if !strings.Contains(h.stdout(), "expired") {
		t.Errorf("stdout = %q, want it to say expired", h.stdout())
	}
}

// The secrets list is cluster-wide and issued once, so a refusal reads zero
// certificates. Reporting that as a clean run is the same silence as exiting 0
// on an unreachable cluster.
func TestExpiryTreatsARefusedSecretsListAsUnknown(t *testing.T) {
	h := newHarness(t, defaultSpec())
	fixedNow(t)
	stubExpiry(t, nil, []expiry.Skip{{Resource: "secrets", Blind: true}})

	err := h.run("expiry", "dev")
	if code := ExitCode(err); code != ExitUnhealthy {
		t.Fatalf("ExitCode = %d, want %d; nothing at all was read", code, ExitUnhealthy)
	}
	if !strings.Contains(h.stderr(), "could not read secrets") {
		t.Errorf("stderr = %q, want it to name what could not be read", h.stderr())
	}
}

// A cert-manager problem costs only the "who renews this" column, so it stays
// a warning rather than taking the exit status with it.
func TestExpiryOverlayFailureIsNotUnknown(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "ns", Kind: expiry.KindTLSSecret, Name: "tls", NotAfter: now.AddDate(1, 0, 0)},
		},
	}, []expiry.Skip{{Resource: "certificates.cert-manager.io", Reason: "context deadline exceeded"}})

	if err := h.run("expiry", "dev"); err != nil {
		t.Fatalf("ExitCode = %d, want 0: every notAfter was read", ExitCode(err))
	}
	// The reason travels, or a timeout reads as a permission problem.
	if !strings.Contains(h.stderr(), "deadline exceeded") {
		t.Errorf("stderr = %q, want the real reason", h.stderr())
	}
}

// cert-manager meant to renew and the date went by. Rendered as a plain
// countdown it is indistinguishable from a certificate nobody automated.
func TestExpiryMarksAnOverdueRenewal(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	overdue := now.AddDate(0, 0, -2)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "ns", Kind: expiry.KindCertificate, Name: "stuck", NotAfter: now.AddDate(0, 0, 3),
				Issuer: "letsencrypt", RenewalTime: &overdue},
		},
	}, nil)

	_ = h.run("expiry", "dev")
	out := h.stdout()
	if !strings.Contains(out, "(auto, overdue)") {
		t.Errorf("stdout = %q, want the failed renewal called out", out)
	}
	// And not dimmed as an ordinary managed row. Asserted on the whole cell so
	// it does not pass merely because "(auto)" is a prefix of "(auto, overdue)".
	if strings.Contains(out, "3d (auto)") {
		t.Errorf("a failed renewal was dimmed as automatic: %q", out)
	}
}

// cert-manager sets renewalTime to when it will start, so a renewal in flight
// legitimately leaves it in the past. Firing on those makes the marker noise.
func TestExpiryGivesAnInFlightRenewalGrace(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	justStarted := now.Add(-30 * time.Minute)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "ns", Kind: expiry.KindCertificate, Name: "renewing", NotAfter: now.AddDate(0, 0, 10),
				Issuer: "letsencrypt", RenewalTime: &justStarted},
		},
	}, nil)

	_ = h.run("expiry", "dev")
	if strings.Contains(h.stdout(), "overdue") {
		t.Errorf("a renewal half an hour old was called overdue: %q", h.stdout())
	}
}

// An expired managed certificate is cert-manager having owned it and let it
// die — a different fix from a secret nobody ever automated.
func TestExpiryMarksAnExpiredManagedCertificate(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "ns", Kind: expiry.KindCertificate, Name: "dead", NotAfter: now.AddDate(0, 0, -1),
				Issuer: "letsencrypt"},
		},
	}, nil)

	_ = h.run("expiry", "dev")
	if !strings.Contains(h.stdout(), "expired (auto)") {
		t.Errorf("stdout = %q, want the dead managed certificate marked", h.stdout())
	}
}

// A blind context must produce a row. Rendering a header and nothing else
// while exiting 2 says "unreadable" in the status and "clean" on screen.
func TestExpiryRendersARowForABlindContext(t *testing.T) {
	h := newHarness(t, defaultSpec())
	fixedNow(t)
	stubExpiry(t, nil, []expiry.Skip{{Resource: "secrets", Reason: "forbidden", Blind: true}})

	_ = h.run("expiry", "dev")
	if !strings.Contains(h.stdout(), "unreadable") {
		t.Errorf("stdout = %q, want a row saying the context could not be read", h.stdout())
	}
}

func TestExpiryJSON(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "ns", Kind: expiry.KindTLSSecret, Name: "tls", NotAfter: now.AddDate(0, 0, 5)},
		},
	}, nil)

	err := h.run("expiry", "dev", "-o", "json")
	if code := ExitCode(err); code != ExitUnhealthy {
		t.Fatalf("ExitCode = %d, want %d", code, ExitUnhealthy)
	}
	out := h.stdout()
	// lowerCamel everywhere, as the rest of the JSON surface is.
	for _, key := range []string{`"context"`, `"namespace"`, `"notAfter"`, `"kind"`} {
		if !strings.Contains(out, key) {
			t.Errorf("json is missing %s: %q", key, out)
		}
	}
}

// --all is the way to see the whole inventory. It widens what is shown and
// deliberately not what counts as due — otherwise any cluster holding a single
// TLS secret would exit 2 and the gate would mean nothing.
func TestExpiryAllShowsEverythingWithoutRaisingTheAlarm(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "ns", Kind: expiry.KindTLSSecret, Name: "far-off", NotAfter: now.AddDate(2, 0, 0)},
		},
	}, nil)

	if err := h.run("expiry", "dev", "--all"); err != nil {
		t.Fatalf("ExitCode = %d, want 0: nothing is due inside --days", ExitCode(err))
	}
	if !strings.Contains(h.stdout(), "far-off") {
		t.Errorf("stdout = %q, want the far-off certificate listed", h.stdout())
	}
}

// ... but something genuinely due still exits 2 with --all in play.
func TestExpiryAllStillFlagsWhatIsDue(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "ns", Kind: expiry.KindTLSSecret, Name: "far-off", NotAfter: now.AddDate(2, 0, 0)},
			{Namespace: "ns", Kind: expiry.KindTLSSecret, Name: "due", NotAfter: now.AddDate(0, 0, 4)},
		},
	}, nil)

	err := h.run("expiry", "dev", "--all")
	if code := ExitCode(err); code != ExitUnhealthy {
		t.Errorf("ExitCode = %d, want %d", code, ExitUnhealthy)
	}
	for _, name := range []string{"far-off", "due"} {
		if !strings.Contains(h.stdout(), name) {
			t.Errorf("stdout = %q, want %s listed", h.stdout(), name)
		}
	}
}

// A managed certificate renews itself; a report where every row is red is one
// nobody reads twice.
func TestExpiryMarksManagedCertificates(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "ns", Kind: expiry.KindCertificate, Name: "auto", NotAfter: now.AddDate(0, 0, 10), Issuer: "letsencrypt"},
			{Namespace: "ns", Kind: expiry.KindTLSSecret, Name: "manual", NotAfter: now.AddDate(0, 0, 10)},
		},
	}, nil)

	_ = h.run("expiry", "dev")
	out := h.stdout()
	if !strings.Contains(out, "10d (auto)") {
		t.Errorf("stdout = %q, want the managed row marked auto", out)
	}
	if strings.Contains(out, "manual") && strings.Count(out, "(auto)") != 1 {
		t.Errorf("the unmanaged row was also marked auto: %q", out)
	}
}

// The command's whole advertised use is "kctx expiry --days 30 || notify".
// Exiting 0 because every cluster was unreachable is the failure mode that
// gate exists to prevent.
func TestExpiryExitsNonZeroWhenAContextCannotBeRead(t *testing.T) {
	h := newHarness(t, defaultSpec())
	fixedNow(t)

	original := expiryFetch
	expiryFetch = func(context.Context, *rest.Config) ([]expiry.Item, []expiry.Skip, error) {
		return nil, nil, errors.New("connection refused")
	}
	t.Cleanup(func() { expiryFetch = original })

	err := h.run("expiry", "dev")
	if code := ExitCode(err); code != ExitUnhealthy {
		t.Fatalf("ExitCode = %d, want %d; nothing was read, so nothing can be said", code, ExitUnhealthy)
	}
	if !strings.Contains(h.stdout(), "unreadable") {
		t.Errorf("stdout = %q, want the context marked unreadable", h.stdout())
	}
}

// render measures a cell's width over the whole string, so an embedded newline
// would set its column to the length of the entire client-go error.
func TestTrimErrorCutsAtTheFirstNewline(t *testing.T) {
	got := trimError("connection refused\nDid you mean something else?\nstack trace", 40)
	if strings.Contains(got, "\n") {
		t.Errorf("trimError kept a newline: %q", got)
	}
	if got != "connection refused" {
		t.Errorf("trimError = %q", got)
	}
	// Runes, not bytes: cutting a multi-byte character in half emits invalid UTF-8.
	long := strings.Repeat("한", 60)
	if !utf8.ValidString(trimError(long, 10)) {
		t.Error("trimError produced invalid UTF-8")
	}
}
