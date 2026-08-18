package cli

import (
	"context"
	"strings"
	"testing"
	"time"

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
func stubExpiry(t *testing.T, items map[string][]expiry.Item, skipped []string) {
	t.Helper()
	original := expiryFetch
	// The sweeper hands the fetcher only a rest.Config, so the per-context
	// answer is keyed off the host it was built for.
	expiryFetch = func(_ context.Context, rc *rest.Config) ([]expiry.Item, []string, error) {
		return items[rc.Host], skipped, nil
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

// A report that goes quiet because it could not look is worse than no report.
func TestExpiryNamesWhatItCouldNotRead(t *testing.T) {
	h := newHarness(t, defaultSpec())
	fixedNow(t)
	stubExpiry(t, nil, []string{"secrets"})

	if err := h.run("expiry", "dev"); err != nil {
		t.Fatalf("expiry: %v", err)
	}
	if !strings.Contains(h.stderr(), "not allowed to read secrets") {
		t.Errorf("stderr = %q, want the partial-read warning", h.stderr())
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

// --all is the way to see the whole inventory, not just the urgent end.
func TestExpiryAllIgnoresTheWindow(t *testing.T) {
	h := newHarness(t, defaultSpec())
	now := fixedNow(t)
	stubExpiry(t, map[string][]expiry.Item{
		"https://dev.example.com:6443": {
			{Namespace: "ns", Kind: expiry.KindTLSSecret, Name: "far-off", NotAfter: now.AddDate(2, 0, 0)},
		},
	}, nil)

	err := h.run("expiry", "dev", "--all")
	if code := ExitCode(err); code != ExitUnhealthy {
		t.Fatalf("ExitCode = %d, want %d", code, ExitUnhealthy)
	}
	if !strings.Contains(h.stdout(), "far-off") {
		t.Errorf("stdout = %q, want the far-off certificate", h.stdout())
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
