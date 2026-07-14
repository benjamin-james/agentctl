package dist

import (
	"strings"
	"testing"
)

// --- Binary ---

func validBinary() Binary {
	return Binary{
		Archive: "https://example.com/codex.tar.gz",
		Cmd:     "/usr/local/bin/codex",
		Args:    []string{"--acp"},
		Env:     map[string]string{"FOO": "bar"},
		SHA256:  strings.Repeat("a", 64),
	}
}

func TestBinaryValidate(t *testing.T) {
	if err := validBinary().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBinaryValidateEmptyArchive(t *testing.T) {
	b := validBinary()
	b.Archive = ""
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for empty archive")
	}
}

func TestBinaryValidateBadCmd(t *testing.T) {
	b := validBinary()
	b.Cmd = "bad;rm -rf /"
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for bad command name")
	}
}

func TestBinaryValidateBadArchiveBasename(t *testing.T) {
	b := validBinary()
	b.Archive = "https://example.com/bad name.tar.gz"
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for archive basename with space")
	}
}

func TestBinaryValidateBadEnvKey(t *testing.T) {
	b := validBinary()
	b.Env = map[string]string{"BAD KEY": "1"}
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for bad env key")
	}
}

func TestBinaryValidateEnvValueWithShellMeta(t *testing.T) {
	// Env values may contain shell metacharacters; they are quoted at emission
	// via Quote, so Validate must ACCEPT them.
	b := validBinary()
	b.Env = map[string]string{"EVIL": "$(rm -rf /)`echo x`"}
	if err := b.Validate(); err != nil {
		t.Fatalf("expected env value with shell metacharacters to be ACCEPTED (quoted at emission): %v", err)
	}
}

func TestBinaryValidateEnvValueWithNewline(t *testing.T) {
	b := validBinary()
	b.Env = map[string]string{"X": "a\nb"}
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for env value with newline")
	}
}

func TestBinaryValidateArgWithNewline(t *testing.T) {
	b := validBinary()
	b.Args = []string{"a\nb"}
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for arg with newline")
	}
}

func TestBinaryValidateBadSHA256(t *testing.T) {
	b := validBinary()
	b.SHA256 = "nothex"
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for non-hex SHA256")
	}
	b.SHA256 = strings.Repeat("a", 63)
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for too-short SHA256")
	}
}

func TestBinaryValidateEmptySHA256Ok(t *testing.T) {
	b := validBinary()
	b.SHA256 = ""
	if err := b.Validate(); err != nil {
		t.Fatalf("empty SHA256 (skip verification) should be allowed: %v", err)
	}
}

func TestBinaryPackages(t *testing.T) {
	if p := (Binary{}).Packages(); p != nil {
		t.Errorf("Binary.Packages() = %v, want nil", p)
	}
}

func TestBinaryInstaller(t *testing.T) {
	b := validBinary()
	script, err := b.Installer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checks := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`wget -O "${tmp}/codex.tar.gz" 'https://example.com/codex.tar.gz'`,
		`echo "` + strings.Repeat("a", 64) + `  ${tmp}/codex.tar.gz" | sha256sum -c -`,
		"*.tar.gz|*.tgz) tar -xzf",
		`bin=$(find "${tmp}" -type f -name 'codex' -print -quit)`,
		`install -m 0755 "${bin}" /usr/local/bin/codex`,
	}
	for _, c := range checks {
		if !strings.Contains(script, c) {
			t.Errorf("installer missing %q\ngot:\n%s", c, script)
		}
	}
}

func TestBinaryInstallerTgz(t *testing.T) {
	b := validBinary()
	b.Archive = "https://example.com/codex.tgz"
	b.SHA256 = ""
	script, err := b.Installer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "*.tar.gz|*.tgz) tar -xzf") {
		t.Errorf(".tgz should be extracted by the tar.gz case:\n%s", script)
	}
}

func TestBinaryInstallerBz2(t *testing.T) {
	b := validBinary()
	b.Archive = "https://example.com/codex.tar.bz2"
	b.SHA256 = ""
	script, err := b.Installer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "*.tar.bz2) tar -xjf") {
		t.Errorf(".tar.bz2 should be extracted by tar -xjf:\n%s", script)
	}
}

func TestBinaryInstallerNoSHA256OmitsCheck(t *testing.T) {
	b := validBinary()
	b.SHA256 = ""
	script, err := b.Installer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(script, "sha256sum") {
		t.Errorf("installer should omit sha256sum when no digest:\n%s", script)
	}
}

func TestBinaryRunner(t *testing.T) {
	b := validBinary()
	script, err := b.Runner()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checks := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"umask 077",
		`: "${ACP_WORKDIR:=/data}"`,
		`mkdir -p "${ACP_WORKDIR}"`,
		`cd "${ACP_WORKDIR}"`,
		`export FOO='bar'`,
		`exec codex '--acp'`,
	}
	for _, c := range checks {
		if !strings.Contains(script, c) {
			t.Errorf("runner missing %q\ngot:\n%s", c, script)
		}
	}
}

func TestBinaryRunnerNoArgsNoEnv(t *testing.T) {
	b := Binary{Archive: "https://example.com/c.tar.gz", Cmd: "c"}
	script, err := b.Runner()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "exec c\n") {
		t.Errorf("runner should exec 'c' with no args:\n%s", script)
	}
	if strings.Contains(script, "export ") {
		t.Errorf("runner should not export env when none provided:\n%s", script)
	}
}

func TestBinaryRunnerEnvShellSafe(t *testing.T) {
	// An env value with shell metacharacters must be single-quoted so it is
	// not evaluated by bash.
	b := validBinary()
	b.Env = map[string]string{"X": "$(evil)`tick`"}
	script, err := b.Runner()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "export X='$(evil)`tick`'") {
		t.Errorf("env value must be single-quoted verbatim:\n%s", script)
	}
}

// --- URL-encoded archive basenames ---

func TestBinaryValidateUrlEncodedBasename(t *testing.T) {
	// cortex-code's archive URL contains %2B (encoded "+") in the path.
	b := Binary{
		Archive: "https://sfc-repo.snowflakecomputing.com/cortex-code-cli/a4643c4278/1.0.73%2B180523.e6179a031de9/coco-1.0.73%2B180523.e6179a031de9-linux-amd64.tar.gz",
		Cmd:     "./cortex",
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("URL-encoded basename should pass validation: %v", err)
	}
}

func TestBinaryInstallerUrlEncodedBasename(t *testing.T) {
	b := Binary{
		Archive: "https://sfc-repo.snowflakecomputing.com/cortex-code-cli/a4643c4278/1.0.73%2B180523.e6179a031de9/coco-1.0.73%2B180523.e6179a031de9-linux-amd64.tar.gz",
		Cmd:     "./cortex",
	}
	script, err := b.Installer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The wget URL must be the original encoded URL.
	if !strings.Contains(script, "'https://sfc-repo.snowflakecomputing.com/cortex-code-cli/a4643c4278/1.0.73%2B180523.e6179a031de9/coco-1.0.73%2B180523.e6179a031de9-linux-amd64.tar.gz'") {
		t.Errorf("installer should use original encoded URL for wget:\n%s", script)
	}
	// The local filename must be the decoded form (+, not %2B).
	decodedBase := "coco-1.0.73+180523.e6179a031de9-linux-amd64.tar.gz"
	if !strings.Contains(script, "${tmp}/"+decodedBase) {
		t.Errorf("installer should use decoded basename %q:\n%s", decodedBase, script)
	}
	// The case pattern must also use the decoded form (for glob matching to work).
	if !strings.Contains(script, "case '"+decodedBase+"' in") {
		t.Errorf("installer case pattern should use decoded basename:\n%s", script)
	}
}

func TestArchiveBasename(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://example.com/codex.tar.gz", "codex.tar.gz", false},
		{"https://example.com/coco-1.0.73%2B180523.tar.gz", "coco-1.0.73+180523.tar.gz", false},
		{"https://example.com/%20file.tar.gz", " file.tar.gz", false},
		{"https://example.com/bad%ZZ.tar.gz", "", true},
	}
	for _, c := range cases {
		got, err := archiveBasename(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("archiveBasename(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("archiveBasename(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("archiveBasename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- Npx ---

func validNpx() Npx {
	return Npx{
		Package: "@agentclientprotocol/codex-acp@1.1.0",
		Args:    []string{"--acp"},
		Env:     map[string]string{"FOO": "bar"},
	}
}

func TestNpxValidate(t *testing.T) {
	if err := validNpx().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNpxValidateEmptyPackage(t *testing.T) {
	n := validNpx()
	n.Package = ""
	if err := n.Validate(); err == nil {
		t.Fatal("expected error for empty package")
	}
}

func TestNpxValidateBadPackage(t *testing.T) {
	n := validNpx()
	n.Package = "bad;rm -rf /"
	if err := n.Validate(); err == nil {
		t.Fatal("expected error for bad package")
	}
}

func TestNpxPackages(t *testing.T) {
	p := (Npx{}).Packages()
	if len(p) != 2 || p[0] != "nodejs" || p[1] != "npm" {
		t.Errorf("Npx.Packages() = %v, want [nodejs npm]", p)
	}
}

func TestNpxInstaller(t *testing.T) {
	n := validNpx()
	script, err := n.Installer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checks := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"npm install -g @agentclientprotocol/codex-acp@1.1.0",
	}
	for _, c := range checks {
		if !strings.Contains(script, c) {
			t.Errorf("installer missing %q\ngot:\n%s", c, script)
		}
	}
	for _, bad := range []string{"wget", "tar -x", "sha256sum"} {
		if strings.Contains(script, bad) {
			t.Errorf("npx installer must not contain %q:\n%s", bad, script)
		}
	}
}

func TestNpxRunner(t *testing.T) {
	n := validNpx()
	script, err := n.Runner()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checks := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"umask 077",
		`: "${ACP_WORKDIR:=/data}"`,
		`export FOO='bar'`,
		`exec npx '@agentclientprotocol/codex-acp@1.1.0' '--acp'`,
	}
	for _, c := range checks {
		if !strings.Contains(script, c) {
			t.Errorf("runner missing %q\ngot:\n%s", c, script)
		}
	}
}

func TestNpxRunnerNoArgsNoEnv(t *testing.T) {
	n := Npx{Package: "simple-agent@0.1"}
	script, err := n.Runner()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "exec npx 'simple-agent@0.1'\n") {
		t.Errorf("runner should exec npx with package spec:\n%s", script)
	}
	if strings.Contains(script, "export ") {
		t.Errorf("runner should not export env when none provided:\n%s", script)
	}
}

func TestNpxRunnerEnvSortedAndShellSafe(t *testing.T) {
	n := Npx{
		Package: "agent",
		Env:     map[string]string{"ZED": "1", "ABC": "$(x)", "MID": "2"},
	}
	script, err := n.Runner()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Keys must appear in sorted order.
	iABC := strings.Index(script, "export ABC=")
	iMID := strings.Index(script, "export MID=")
	iZED := strings.Index(script, "export ZED=")
	if iABC < 0 || iMID < 0 || iZED < 0 {
		t.Fatalf("missing env exports:\n%s", script)
	}
	if iABC >= iMID || iMID >= iZED {
		t.Errorf("env exports must be sorted ABC<MID<ZED:\n%s", script)
	}
	if !strings.Contains(script, "export ABC='$(x)'") {
		t.Errorf("env value must be single-quoted verbatim:\n%s", script)
	}
}

// --- closed-sum enforcement ---

func TestDistributionInterfaceSatisfied(t *testing.T) {
	var _ Distribution = Binary{}
	var _ Distribution = Npx{}
	// A type from outside this package cannot implement isDistribution, so the
	// sum is closed. This is a compile-time guarantee; this test just documents
	// the two concrete members.
}
