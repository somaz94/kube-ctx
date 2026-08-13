// Package probe reports on the health of the contexts in a kubeconfig.
//
// It answers the question a kubeconfig accumulates over a year of work: which
// of these still point at a cluster that exists, and which are held together by
// a certificate that expired in March? Two kinds of check are combined — static
// ones that read the kubeconfig alone, and a live call to /version.
package probe

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"crypto/x509"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// DefaultConcurrency bounds how many clusters are contacted at once. Probing a
// 40-context kubeconfig should be quick without opening 40 TLS handshakes.
const DefaultConcurrency = 8

// DefaultTimeout is the per-context deadline for the live call.
const DefaultTimeout = 3 * time.Second

// AuthKind describes how a context authenticates.
type AuthKind string

const (
	// AuthClientCert is a client certificate, inline or on disk.
	AuthClientCert AuthKind = "client-cert"
	// AuthToken is a bearer token.
	AuthToken AuthKind = "token"
	// AuthBasic is username and password.
	AuthBasic AuthKind = "basic"
	// AuthExec is a credential plugin such as the AWS or GCP helper.
	AuthExec AuthKind = "exec"
	// AuthNone means the context carries no credentials at all.
	AuthNone AuthKind = "none"
)

// Result is what the probe found about one context.
type Result struct {
	Context string `json:"context"`
	Cluster string `json:"cluster"`
	Server  string `json:"server"`

	// Auth describes the credential type, e.g. "exec: aws".
	Auth AuthKind `json:"auth"`
	// AuthDetail names the exec plugin, when there is one.
	AuthDetail string `json:"authDetail,omitempty"`
	// Expiry is when the credential stops working, when that is knowable
	// without contacting the cluster.
	Expiry *time.Time `json:"expiry,omitempty"`

	// Reachable reports whether the API server answered.
	Reachable bool `json:"reachable"`
	// ServerVersion is what /version reported.
	ServerVersion string `json:"serverVersion,omitempty"`
	// Latency is how long the live call took.
	Latency time.Duration `json:"latency,omitempty"`

	// Issues lists problems found without contacting the cluster.
	Issues []string `json:"issues,omitempty"`
	// Err is why the live call failed.
	Err string `json:"error,omitempty"`
}

// Expired reports whether a known credential expiry is in the past.
func (r Result) Expired() bool {
	return r.Expiry != nil && r.Expiry.Before(time.Now())
}

// Healthy reports whether the context is usable right now.
func (r Result) Healthy() bool {
	return r.Reachable && !r.Expired() && len(r.Issues) == 0
}

// VersionFunc fetches the server version for a connection.
type VersionFunc func(ctx context.Context, rc *rest.Config) (string, error)

// Prober runs the checks. RestConfig and Version are fields so tests can drive
// the whole thing without a cluster.
type Prober struct {
	// RestConfig builds a connection for one context.
	RestConfig func(ctxName string) (*rest.Config, error)
	// Version performs the live call. Defaults to LiveVersion.
	Version VersionFunc
	// Concurrency bounds parallel probes.
	Concurrency int
	// Timeout bounds each live call.
	Timeout time.Duration
}

// Run probes the named contexts, or every context when names is empty, and
// returns the results sorted by context name.
func (p *Prober) Run(ctx context.Context, cfg *clientcmdapi.Config, names []string) []Result {
	if len(names) == 0 {
		for name := range cfg.Contexts {
			names = append(names, name)
		}
	}
	concurrency := p.Concurrency
	if concurrency < 1 {
		concurrency = DefaultConcurrency
	}
	version := p.Version
	if version == nil {
		version = LiveVersion
	}
	timeout := p.Timeout
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
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = p.probeOne(ctx, cfg, name, version, timeout)
		}(i, name)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i].Context < results[j].Context })
	return results
}

// probeOne runs the static checks and, when they leave a usable connection, the
// live one.
func (p *Prober) probeOne(ctx context.Context, cfg *clientcmdapi.Config, name string, version VersionFunc, timeout time.Duration) Result {
	result := Result{Context: name}

	kubeCtx, ok := cfg.Contexts[name]
	if !ok {
		result.Issues = append(result.Issues, "context not found")
		return result
	}
	result.Cluster = kubeCtx.Cluster

	cluster, ok := cfg.Clusters[kubeCtx.Cluster]
	if !ok {
		result.Issues = append(result.Issues, fmt.Sprintf("cluster %q is not defined", kubeCtx.Cluster))
	} else {
		result.Server = cluster.Server
	}

	authInfo, ok := cfg.AuthInfos[kubeCtx.AuthInfo]
	if !ok && kubeCtx.AuthInfo != "" {
		result.Issues = append(result.Issues, fmt.Sprintf("user %q is not defined", kubeCtx.AuthInfo))
	}
	if authInfo != nil {
		kind, detail, expiry, err := inspectAuth(authInfo)
		result.Auth, result.AuthDetail, result.Expiry = kind, detail, expiry
		if err != nil {
			result.Issues = append(result.Issues, err.Error())
		}
		if result.Expired() {
			result.Issues = append(result.Issues,
				fmt.Sprintf("credential expired %s", result.Expiry.Format(time.RFC3339)))
		}
	} else {
		result.Auth = AuthNone
	}

	if result.Server == "" {
		return result // nothing to contact
	}

	rc, err := p.RestConfig(name)
	if err != nil {
		result.Err = err.Error()
		return result
	}
	rc.Timeout = timeout

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	serverVersion, err := version(callCtx, rc)
	result.Latency = time.Since(start)
	if err != nil {
		result.Err = trimError(err)
		return result
	}
	result.Reachable = true
	result.ServerVersion = serverVersion
	return result
}

// LiveVersion asks the API server for its version.
func LiveVersion(ctx context.Context, rc *rest.Config) (string, error) {
	client, err := discovery.NewDiscoveryClientForConfig(rc)
	if err != nil {
		return "", err
	}
	info, err := client.ServerVersion()
	if err != nil {
		return "", err
	}
	return info.GitVersion, nil
}

// inspectAuth determines the credential type and, where it is discoverable
// offline, when it expires.
func inspectAuth(authInfo *clientcmdapi.AuthInfo) (AuthKind, string, *time.Time, error) {
	switch {
	case authInfo.Exec != nil:
		return AuthExec, authInfo.Exec.Command, nil, nil

	case len(authInfo.ClientCertificateData) > 0:
		expiry, err := certExpiry(authInfo.ClientCertificateData)
		return AuthClientCert, "inline", expiry, err

	case authInfo.ClientCertificate != "":
		data, err := os.ReadFile(authInfo.ClientCertificate)
		if err != nil {
			return AuthClientCert, authInfo.ClientCertificate, nil,
				fmt.Errorf("client certificate is unreadable: %w", err)
		}
		expiry, err := certExpiry(data)
		return AuthClientCert, authInfo.ClientCertificate, expiry, err

	case authInfo.Token != "":
		return AuthToken, "", tokenExpiry(authInfo.Token), nil

	case authInfo.TokenFile != "":
		data, err := os.ReadFile(authInfo.TokenFile)
		if err != nil {
			return AuthToken, authInfo.TokenFile, nil,
				fmt.Errorf("token file is unreadable: %w", err)
		}
		return AuthToken, authInfo.TokenFile, tokenExpiry(strings.TrimSpace(string(data))), nil

	case authInfo.Username != "":
		return AuthBasic, authInfo.Username, nil, nil

	case authInfo.AuthProvider != nil:
		return AuthExec, authInfo.AuthProvider.Name, nil, nil
	}
	return AuthNone, "", nil, nil
}

// certExpiry reads notAfter out of a PEM-encoded client certificate.
func certExpiry(pemData []byte) (*time.Time, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("client certificate is not valid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("client certificate is unparseable: %w", err)
	}
	return &cert.NotAfter, nil
}

// tokenExpiry reads the exp claim of a JWT.
//
// The token is decoded, not verified: kube-ctx has no business validating a
// signature it cannot check, and an unverifiable exp is still the right thing
// to warn about. A non-JWT token (a service account secret, say) simply has no
// discoverable expiry.
func tokenExpiry(token string) *time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return nil
	}
	t := time.Unix(claims.Exp, 0)
	return &t
}

// trimError shortens the multi-line, deeply wrapped errors client-go produces
// into something that fits in a table cell.
func trimError(err error) string {
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	const maxLen = 120
	if len(msg) > maxLen {
		msg = msg[:maxLen-1] + "…"
	}
	return msg
}
