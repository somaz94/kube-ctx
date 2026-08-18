// Package expiry finds the certificates in a cluster that are about to run
// out, across every context at once.
//
// It is the workload-level counterpart to pkg/probe. probe answers "can I
// reach this cluster right now" and exits non-zero when the answer is no;
// this answers "what breaks in the next N days", where nothing is wrong yet.
// The two are deliberately separate commands: folding expiry into doctor would
// make a certificate with three weeks left report a sick cluster, and would
// make doctor — one GET /version per context — into a namespace-wide list that
// fails for any credential allowed to reach the API but not read secrets.
//
// The unit of truth is the certificate itself, not the resource that manages
// it. Every TLS secret carries the PEM, so notAfter is readable on any cluster
// with no CRD installed and no assumption about what issued it; cert-manager
// Certificates are then folded in on top, because they say what will renew the
// thing and when it means to.
package expiry

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sort"
	"sync"
	"time"

	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// DefaultConcurrency bounds how many contexts are swept at once, for the same
// reason pkg/probe bounds its own: answering a question about a 40-context
// kubeconfig should not open 40 connections to do it.
const DefaultConcurrency = 8

// DefaultTimeout is how long one context gets to answer.
//
// Longer than probe's, because this lists across namespaces rather than
// fetching a single version endpoint, and a slow answer is still an answer.
const DefaultTimeout = 15 * time.Second

// DefaultDays is the window that counts as "about to expire".
//
// Thirty days is the shortest notice that is still actionable: it clears a
// month of change freezes, and it is longer than the 15 days before notAfter
// at which cert-manager renews by default, so a healthy managed certificate
// shows up here having already scheduled its own fix rather than as a surprise.
const DefaultDays = 30

// Kind is what sort of thing is expiring.
type Kind string

const (
	// KindCertificate is a cert-manager Certificate, which knows its own
	// renewal schedule.
	KindCertificate Kind = "Certificate"
	// KindTLSSecret is a kubernetes.io/tls secret read straight from its PEM.
	KindTLSSecret Kind = "Secret/tls"
)

// Item is one certificate that is going to expire.
type Item struct {
	Context   string    `json:"context"`
	Namespace string    `json:"namespace"`
	Kind      Kind      `json:"kind"`
	Name      string    `json:"name"`
	NotAfter  time.Time `json:"notAfter"`
	// Issuer names the cert-manager issuer, empty for an unmanaged secret. It
	// is the difference between "this renews itself" and "someone has to".
	Issuer string `json:"issuer,omitempty"`
	// RenewalTime is when cert-manager intends to renew, if it said so.
	RenewalTime *time.Time `json:"renewalTime,omitempty"`
}

// In reports how long is left, rounded down to whole days.
func (i Item) In(now time.Time) time.Duration { return i.NotAfter.Sub(now) }

// Expired reports whether the deadline has already passed.
func (i Item) Expired(now time.Time) bool { return !i.NotAfter.After(now) }

// Managed reports whether something is going to renew this without help.
func (i Item) Managed() bool { return i.Issuer != "" }

// Result is one context's answer.
type Result struct {
	Context string `json:"context"`
	Items   []Item `json:"items"`
	// Err is the context failing outright — unreachable, or no credential.
	Err string `json:"error,omitempty"`
	// Skipped names the resource kinds this credential was not allowed to
	// read. Reported rather than fatal: a token scoped to one namespace still
	// gives a true answer about that namespace, and refusing to say anything
	// because it cannot say everything is how a safety report gets ignored.
	Skipped []string `json:"skipped,omitempty"`
}

// FetchFunc reads every certificate one cluster knows about.
//
// Injectable for the same reason probe's VersionFunc is: the sweep, the
// windowing and the reporting are all testable without an API server, and only
// this one function needs a live cluster.
type FetchFunc func(ctx context.Context, rc *rest.Config) ([]Item, []string, error)

// Sweeper collects expiring certificates across contexts.
type Sweeper struct {
	// Fetch reads one cluster. Defaults to Live.
	Fetch FetchFunc
	// Concurrency bounds contexts in flight. Zero means DefaultConcurrency.
	Concurrency int
	// Timeout bounds one context. Zero means DefaultTimeout.
	Timeout time.Duration
	// RestConfig turns a context name into a connection.
	RestConfig func(name string) (*rest.Config, error)
}

// Run sweeps every named context, or all of them when names is empty.
func (s *Sweeper) Run(ctx context.Context, cfg *clientcmdapi.Config, names []string) []Result {
	if len(names) == 0 {
		for name := range cfg.Contexts {
			names = append(names, name)
		}
	}
	concurrency := s.Concurrency
	if concurrency < 1 {
		concurrency = DefaultConcurrency
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	results := make([]Result, len(names))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			// Cancellable acquire, as in pkg/probe: on a large kubeconfig most
			// contexts are queued here rather than in flight, and an
			// uncancellable wait keeps working long after the caller gave up.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = Result{Context: name, Err: ctx.Err().Error()}
				return
			}
			defer func() { <-sem }()
			results[i] = s.sweepOne(ctx, name, timeout)
		}(i, name)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i].Context < results[j].Context })
	return results
}

// sweepOne reads one context.
func (s *Sweeper) sweepOne(ctx context.Context, name string, timeout time.Duration) Result {
	result := Result{Context: name}

	rc, err := s.RestConfig(name)
	if err != nil {
		result.Err = err.Error()
		return result
	}

	fetch := s.Fetch
	if fetch == nil {
		fetch = Live
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	items, skipped, err := fetch(ctx, rc)
	if err != nil {
		result.Err = err.Error()
		return result
	}
	for i := range items {
		items[i].Context = name
	}
	result.Items = items
	result.Skipped = skipped
	return result
}

// Within narrows results to the certificates expiring inside the window,
// soonest first, and drops the contexts left with nothing to say.
//
// An already-expired certificate is kept rather than filtered out as "past":
// it is the most urgent row on the page, and hiding it would mean the report
// goes quiet exactly when the outage starts.
func Within(results []Result, now time.Time, days int) []Result {
	deadline := now.AddDate(0, 0, days)

	out := make([]Result, 0, len(results))
	for _, r := range results {
		kept := make([]Item, 0, len(r.Items))
		for _, item := range r.Items {
			if item.NotAfter.Before(deadline) {
				kept = append(kept, item)
			}
		}
		sort.SliceStable(kept, func(i, j int) bool { return kept[i].NotAfter.Before(kept[j].NotAfter) })

		// A context with nothing expiring is still reported when it failed or
		// was partly unreadable — that is the difference between "clean" and
		// "could not look".
		if len(kept) == 0 && r.Err == "" && len(r.Skipped) == 0 {
			continue
		}
		r.Items = kept
		out = append(out, r)
	}
	return out
}

// Expiring reports whether anything at all is inside the window, which is what
// decides the command's exit status.
func Expiring(results []Result) bool {
	for _, r := range results {
		if len(r.Items) > 0 {
			return true
		}
	}
	return false
}

// certNotAfter reads notAfter out of a PEM certificate chain.
//
// The leaf is the first block: a tls.crt commonly carries intermediates after
// it, and those outlive the leaf, so taking the last or the maximum would
// report a certificate as healthy for years after it stopped working.
func certNotAfter(pemData []byte) (time.Time, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return time.Time{}, fmt.Errorf("not valid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("unparseable certificate: %w", err)
	}
	return cert.NotAfter, nil
}
