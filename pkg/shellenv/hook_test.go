package shellenv

import (
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
			// The wrapper must bypass itself when calling the binary, or it
			// would recurse forever. bash and zsh use the "command" builtin;
			// fish uses env, which execs the binary from PATH.
			//
			// "env VAR=x command kctx" is specifically wrong: command is a
			// shell builtin and env can only exec a real binary, so that form
			// fails with "env: command: No such file or directory".
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
