package shellenv

import (
	"os/exec"
	"strings"
	"testing"
)

func TestParseShell(t *testing.T) {
	tests := []struct {
		name     string
		arg      string
		shellEnv string
		want     Shell
		wantErr  bool
	}{
		{"explicit bash", "bash", "", Bash, false},
		{"explicit zsh", "zsh", "", Zsh, false},
		{"explicit fish", "fish", "", Fish, false},
		{"case insensitive", "ZSH", "", Zsh, false},
		{"from $SHELL", "", "/bin/zsh", Zsh, false},
		{"from $SHELL with a path", "", "/opt/homebrew/bin/fish", Fish, false},
		{"unsupported", "csh", "", "", true},
		{"nothing at all", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseShell(tt.arg, tt.shellEnv)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseShell: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseShell = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseShellErrorNamesSupportedShells(t *testing.T) {
	_, err := ParseShell("csh", "")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, sh := range Shells {
		if !strings.Contains(err.Error(), string(sh)) {
			t.Errorf("error %q does not mention %q", err, sh)
		}
	}
}

func TestHookDefinesAWrapperFunction(t *testing.T) {
	tests := []struct {
		sh       Shell
		binary   string
		wantFunc string
	}{
		{Bash, "kctx", "kctx() {"},
		{Zsh, "/usr/local/bin/kctx", "kctx() {"},
		{Fish, "kctx", "function kctx"},
		{Bash, "", "kctx() {"},
	}
	for _, tt := range tests {
		t.Run(string(tt.sh)+"/"+tt.binary, func(t *testing.T) {
			got := Hook(tt.sh, tt.binary)

			if !strings.Contains(got, tt.wantFunc) {
				t.Errorf("hook missing %q:\n%s", tt.wantFunc, got)
			}
			// The wrapper must hand the binary a file to write exports to, and
			// source it afterwards.
			if !strings.Contains(got, EnvFile+"=") {
				t.Errorf("hook does not pass %s:\n%s", EnvFile, got)
			}
			if !strings.Contains(got, "mktemp") {
				t.Errorf("hook does not create a temp file:\n%s", got)
			}
			if !strings.Contains(got, "rm -f") {
				t.Errorf("hook does not clean up its temp file:\n%s", got)
			}
			// The wrapper must bypass itself or recurse forever: bash and zsh use
			// "command" (env cannot exec a builtin), fish uses env.
			switch tt.sh {
			case Fish:
				if !strings.Contains(got, "env "+EnvFile+"=") {
					t.Errorf("fish hook should invoke the binary through env:\n%s", got)
				}
			default:
				if !strings.Contains(got, "command "+tt.wantFunc[:strings.Index(tt.wantFunc, "(")]) {
					t.Errorf("hook could recurse into itself:\n%s", got)
				}
				if strings.Contains(got, "env "+EnvFile+"=") {
					t.Errorf("env cannot exec the \"command\" builtin:\n%s", got)
				}
			}
		})
	}
}

// Unnamed, the env file's syntax would follow $SHELL, which is only the login
// shell: a switch that reports success and changes nothing.
func TestHookNamesItsShell(t *testing.T) {
	for _, sh := range Shells {
		got := Hook(sh, "kctx")
		want := EnvShell + "=" + string(sh)
		if !strings.Contains(got, want) {
			t.Errorf("%s hook does not pass %q:\n%s", sh, want, got)
		}
	}
}

func TestHookPreservesExitStatus(t *testing.T) {
	for _, sh := range Shells {
		got := Hook(sh, "kctx")
		if !strings.Contains(got, "return $__kctx_status") {
			t.Errorf("%s hook drops the exit status:\n%s", sh, got)
		}
	}
}

func TestHookUsesTheInstalledBinaryName(t *testing.T) {
	got := Hook(Zsh, "/opt/bin/kube-ctx")
	if !strings.Contains(got, "kube-ctx() {") {
		t.Errorf("hook should wrap the installed name:\n%s", got)
	}
	if strings.Contains(got, "/opt/bin/kube-ctx() {") {
		t.Errorf("the function name must not be a path:\n%s", got)
	}
}

func TestExportLine(t *testing.T) {
	tests := []struct {
		sh   Shell
		want string
	}{
		{Bash, "export FOO='bar'"},
		{Zsh, "export FOO='bar'"},
		{Fish, "set -gx FOO 'bar'"},
	}
	for _, tt := range tests {
		if got := exportLine(tt.sh, "FOO", "bar"); got != tt.want {
			t.Errorf("exportLine(%s) = %q, want %q", tt.sh, got, tt.want)
		}
	}
}

func TestQuoteEscapesSingleQuotes(t *testing.T) {
	got := quote(Bash, `a'b`)
	if got != `'a'\''b'` {
		t.Errorf("quote = %q", got)
	}
	// A value that would otherwise expand must stay literal.
	if q := quote(Bash, "$(rm -rf /)"); q != `'$(rm -rf /)'` {
		t.Errorf("quote = %q", q)
	}
}

// fish reads \\ and \' inside single quotes, so a value ending in a backslash
// left the quote open and the whole env file failed to parse.
func TestQuoteFishEscapesBackslashesAndQuotes(t *testing.T) {
	tests := []struct{ in, want string }{
		{`plain`, `'plain'`},
		{`a\b`, `'a\\b'`},
		{`trail\`, `'trail\\'`},
		{`a'b`, `'a\'b'`},
		{`a\'b`, `'a\\\'b'`},
	}
	for _, tt := range tests {
		if got := quote(Fish, tt.in); got != tt.want {
			t.Errorf("quote(fish, %q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Each shell has its own quoting rules, so the proof is the shell reading its
// own export back. Shells that are not installed are skipped.
func TestExportLineRoundTripsThroughRealShells(t *testing.T) {
	shells := []struct {
		sh   Shell
		argv []string
	}{
		{Bash, []string{"bash", "--norc", "--noprofile", "-c"}},
		{Zsh, []string{"zsh", "-f", "-c"}},
		{Fish, []string{"fish", "--no-config", "-c"}},
	}
	values := []string{`plain`, `a\b`, `a\\b`, `trail\`, `a'b`, `a\'b`, `$(echo pwned)`, "`echo pwned`"}

	for _, s := range shells {
		t.Run(string(s.sh), func(t *testing.T) {
			if _, err := exec.LookPath(s.argv[0]); err != nil {
				t.Skipf("%s is not installed", s.argv[0])
			}
			for _, v := range values {
				script := exportLine(s.sh, "KCTX_QUOTE_TEST", v) + `; printf '%s' "$KCTX_QUOTE_TEST"`
				out, err := exec.Command(s.argv[0], append(s.argv[1:], script)...).CombinedOutput()
				if err != nil {
					t.Errorf("%q: %v\n%s", v, err, out)
					continue
				}
				if string(out) != v {
					t.Errorf("%q came back as %q", v, out)
				}
			}
		})
	}
}

func TestPromptHint(t *testing.T) {
	for _, sh := range Shells {
		got := PromptHint(sh)
		if !strings.Contains(got, EnvActive) {
			t.Errorf("%s prompt hint does not mention %s: %q", sh, EnvActive, got)
		}
		if !strings.HasPrefix(got, "#") {
			t.Errorf("%s prompt hint should be a comment: %q", sh, got)
		}
	}
}

// The hook has to run the binding check on a directory change, and each shell
// spells that differently — zsh has chpwd_functions, fish watches $PWD, and
// bash has neither, so it compares $PWD from PROMPT_COMMAND.
func TestHookWiresUpDirectoryChanges(t *testing.T) {
	tests := []struct {
		sh    Shell
		wants []string
	}{
		{Bash, []string{"PROMPT_COMMAND", "__kctx_last_pwd", "bind --apply"}},
		{Zsh, []string{"chpwd_functions", "bind --apply"}},
		{Fish, []string{"--on-variable PWD", "bind --apply"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.sh), func(t *testing.T) {
			hook := Hook(tt.sh, "kctx")
			for _, want := range tt.wants {
				if !strings.Contains(hook, want) {
					t.Errorf("hook missing %q:\n%s", want, hook)
				}
			}
			// A terminal opened inside a bound directory never fires a change
			// event, so the hook resolves the starting directory itself.
			if !strings.HasSuffix(strings.TrimSpace(hook), "__kctx_chpwd") {
				t.Errorf("hook does not run once on install:\n%s", hook)
			}
		})
	}
}

func TestExportLineIsShellSpecific(t *testing.T) {
	if got := ExportLine(Bash, "K", "v"); got != "export K='v'" {
		t.Errorf("ExportLine(bash) = %q", got)
	}
	if got := ExportLine(Fish, "K", "v"); got != "set -gx K 'v'" {
		t.Errorf("ExportLine(fish) = %q", got)
	}
}
