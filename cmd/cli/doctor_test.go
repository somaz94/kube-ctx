package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/somaz94/kube-ctx/internal/testutil"
	"github.com/somaz94/kube-ctx/pkg/probe"
	"github.com/somaz94/kube-ctx/pkg/render"
)

// writeUserConfig drops a kube-ctx config.yaml into the harness config dir.
func writeUserConfig(t *testing.T, body string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "kube-ctx")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestDoctorOnUnreachableClusters(t *testing.T) {
	h := newHarness(t, defaultSpec())

	// The fixture servers do not exist, so every context fails to connect
	// quickly. The report must still render and the exit must be non-zero.
	err := h.run("doctor", "--timeout", "100ms")
	if err == nil {
		t.Fatal("expected a non-zero exit when contexts are unhealthy")
	}
	if err.Error() != "" {
		t.Errorf("the failure should be silent, got %q", err.Error())
	}

	out := h.stdout()
	for _, want := range []string{"STATUS", "CONTEXT", "VERSION", "dev", "prod", "unreachable"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorSingleContext(t *testing.T) {
	h := newHarness(t, defaultSpec())

	_ = h.run("doctor", "prod", "--timeout", "100ms")
	out := h.stdout()
	if !strings.Contains(out, "prod") {
		t.Errorf("report missing prod:\n%s", out)
	}
	if strings.Contains(out, "staging") {
		t.Errorf("report should cover only the named context:\n%s", out)
	}
}

func TestDoctorJSON(t *testing.T) {
	h := newHarness(t, defaultSpec())

	_ = h.run("doctor", "-o", "json", "--timeout", "100ms")
	if !strings.Contains(h.stdout(), `"context": "dev"`) {
		t.Errorf("stdout = %s", h.stdout())
	}
}

func TestDoctorUnhealthyFilter(t *testing.T) {
	h := newHarness(t, defaultSpec())

	_ = h.run("doctor", "--unhealthy", "--timeout", "100ms")
	// Everything is unreachable in the fixture, so the filter keeps all rows.
	if !strings.Contains(h.stdout(), "dev") {
		t.Errorf("stdout = %s", h.stdout())
	}
}

func TestDoctorEmptyKubeconfig(t *testing.T) {
	h := newHarness(t, testutil.Spec{})

	if err := h.run("doctor"); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(h.stderr(), "No contexts") {
		t.Errorf("stderr = %q", h.stderr())
	}
}

func TestDoctorReportsDanglingReferences(t *testing.T) {
	h := newHarness(t, defaultSpec())

	cfg := h.config()
	delete(cfg.Clusters, "prod-cluster")
	testutil.Write(t, h.kubeconfig, cfg)

	_ = h.run("doctor", "prod", "--timeout", "100ms")
	if !strings.Contains(h.stdout(), "broken") {
		t.Errorf("a dangling cluster reference should read as broken:\n%s", h.stdout())
	}
}

func TestDescribeAuth(t *testing.T) {
	pal := render.NewEnabled(false)
	soon := time.Now().Add(2 * time.Hour)
	later := time.Now().Add(30 * 24 * time.Hour)
	past := time.Now().Add(-time.Hour)

	tests := []struct {
		name   string
		result probe.Result
		want   string
	}{
		{"no expiry", probe.Result{Auth: probe.AuthToken}, "token"},
		{"exec detail", probe.Result{Auth: probe.AuthExec, AuthDetail: "aws"}, "exec:aws"},
		{"expired", probe.Result{Auth: probe.AuthToken, Expiry: &past}, "token (expired)"},
		{"expiring soon", probe.Result{Auth: probe.AuthToken, Expiry: &soon}, "token (2h left)"},
		{"far off", probe.Result{Auth: probe.AuthClientCert, Expiry: &later}, "client-cert (30d left)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeAuth(pal, tt.result); got != tt.want {
				t.Errorf("describeAuth = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetail(t *testing.T) {
	pal := render.NewEnabled(false)

	if got := detail(pal, probe.Result{Issues: []string{"a", "b"}}); got != "a; b" {
		t.Errorf("detail = %q", got)
	}
	if got := detail(pal, probe.Result{Err: "timeout"}); got != "timeout" {
		t.Errorf("detail = %q", got)
	}
	if got := detail(pal, probe.Result{}); got != "" {
		t.Errorf("detail = %q, want empty", got)
	}
}

func TestRoundDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "1m"},
		{90 * time.Second, "2m"},
		{3 * time.Hour, "3h"},
		{119*time.Minute + 59*time.Second, "2h"},
		{72 * time.Hour, "3d"},
	}
	for _, tt := range tests {
		if got := roundDuration(tt.in); got != tt.want {
			t.Errorf("roundDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGuardBadgesInList(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("list"); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := h.stdout()
	if !strings.Contains(out, "GUARD") {
		t.Errorf("list is missing the guard column:\n%s", out)
	}
	// The built-in rules classify "prod" as dangerous and "staging" as a warning.
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "prod") && !strings.Contains(line, "DANGER"):
			t.Errorf("prod row missing the DANGER badge: %q", line)
		case strings.Contains(line, "staging") && !strings.Contains(line, "WARN"):
			t.Errorf("staging row missing the WARN badge: %q", line)
		case strings.Contains(line, "dev") && strings.Contains(line, "DANGER"):
			t.Errorf("dev must not be badged: %q", line)
		}
	}
}

func TestGuardConfirmBlocksSwitch(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, "guards:\n  - match: 'prod'\n    level: danger\n    confirm: true\n")

	h.stdin("no\n")
	err := h.run("ctx", "prod")
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q; a declined guard must not switch", got)
	}
	if !strings.Contains(h.stdout(), "Aborted") {
		t.Errorf("stdout = %q", h.stdout())
	}
	// Declining must not look like success, or "kctx ctx prod && deploy"
	// deploys against whatever context the shell was already on.
	if code := ExitCode(err); code != ExitAborted {
		t.Errorf("ExitCode = %d, want %d", code, ExitAborted)
	}
}

func TestGuardConfirmAcceptsExactName(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, "guards:\n  - match: 'prod'\n    level: danger\n    confirm: true\n")

	h.stdin("prod\n")
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("current = %q, want prod", got)
	}
}

func TestGuardConfirmSkippedWithYes(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, "guards:\n  - match: 'prod'\n    level: danger\n    confirm: true\n")

	if err := h.run("ctx", "prod", "--yes"); err != nil {
		t.Fatalf("ctx prod --yes: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("current = %q, want prod", got)
	}
}

func TestGuardDefaultsDoNotPrompt(t *testing.T) {
	h := newHarness(t, defaultSpec())

	// No config file at all: the built-in rules classify but never block.
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("current = %q, want prod", got)
	}
	if !strings.Contains(h.stdout(), "DANGER") {
		t.Errorf("the switch line should carry the guard badge: %q", h.stdout())
	}
}

func TestInvalidGuardPatternSurfaces(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, "guards:\n  - match: '([unclosed'\n    level: danger\n")

	if err := h.run("ctx", "prod"); err == nil {
		t.Error("expected the invalid pattern to surface as an error")
	}
	if err := h.run("list"); err == nil {
		t.Error("expected the invalid pattern to surface in list too")
	}
}
