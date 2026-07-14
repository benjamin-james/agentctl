package dist

import (
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
)

type IntegrityFetcher interface {
	// Interface that checks SHA string if GH, but can have a null checker as well
	FetchSHA256(archiveURL string) (string, error)
}

// Closed sum type: only Binary and Npx because isDistribution is unexported
// Thus public facing code uses type switch over concrete types
type Distribution interface {
	isDistribution()

	Validate() error

	// e.g. nodejs/npm
	Packages() []string

	// the body of /usr/local/sbin/install-acp.
	Installer() (string, error)

	// the body of /usr/local/bin/acp-run.
	Runner() (string, error)
}

// Tarball based distr, backed by checksum
// SHA256 is `-` because it is resolved at runtime
type Binary struct {
	Archive string            `json:"archive"`
	Cmd     string            `json:"cmd"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	SHA256  string            `json:"-"`
}

// Uses NPX based install
type Npx struct {
	Package string            `json:"package"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Closed sum type Distribution, explicit null implementations
func (Binary) isDistribution() {}
func (Npx) isDistribution()    {}

func (b Binary) Validate() error {
	if b.Archive == "" {
		return fmt.Errorf("binary distribution: empty archive URL")
	}
	if err := ValidateURL(b.Archive); err != nil {
		return fmt.Errorf("binary distribution: %w", err)
	}
	cmd := filepath.Base(b.Cmd)
	if err := CheckToken(cmd, "command name"); err != nil {
		return fmt.Errorf("binary distribution: %w", err)
	}
	// basename is interpolated into shell double quotes and a case
	// pattern glob so it must be safe. URL-decode first so encoded
	// characters like %2B transform into + *before* validation
	base, err := archiveBasename(b.Archive)
	if err != nil {
		return fmt.Errorf("binary distribution: %w", err)
	}
	if err := CheckToken(base, "archive basename"); err != nil {
		return fmt.Errorf("binary distribution: %w", err)
	}
	if err := validateEnv(b.Env); err != nil {
		return fmt.Errorf("binary distribution: %w", err)
	}
	if err := validateArgs(b.Args); err != nil {
		return fmt.Errorf("binary distribution: %w", err)
	}
	if b.SHA256 != "" {
		if len(b.SHA256) != 64 || !isHex(b.SHA256) {
			return fmt.Errorf("binary distribution: invalid SHA256 %q: must be 64 hex chars", b.SHA256)
		}
	}
	return nil
}

func (Binary) Packages() []string { return nil }

func (b Binary) Installer() (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	cmd := filepath.Base(b.Cmd)
	base, err := archiveBasename(b.Archive)
	if err != nil {
		return "", fmt.Errorf("binary distribution: %w", err)
	}
	out := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"tmp=$(mktemp -d)",
		"cleanup() { rm -rf \"${tmp}\"; }",
		"trap cleanup EXIT",
		fmt.Sprintf("wget -O \"${tmp}/%s\" %s", base, Quote(b.Archive)),
	}
	if b.SHA256 != "" {
		// don't need to re-validate SHA256 hex and base so interpolation is safe here
		out = append(out, fmt.Sprintf("echo \"%s  ${tmp}/%s\" | sha256sum -c -", b.SHA256, base))
	}
	// delegate tar flags based on extension in explicit switch statement
	out = append(out, "case "+Quote(base)+" in")
	out = append(out, "  *.tar.gz|*.tgz) tar -xzf \"${tmp}/"+base+"\" -C \"${tmp}\" ;;")
	out = append(out, "  *.tar.bz2) tar -xjf \"${tmp}/"+base+"\" -C \"${tmp}\" ;;")
	out = append(out, "  *) echo \"unsupported archive format for "+base+"\" >&2; exit 1 ;;")
	out = append(out, "esac")
	// find binary using find(1) instead of assuming at root
	out = append(out, fmt.Sprintf("bin=$(find \"${tmp}\" -type f -name %s -print -quit)", Quote(cmd)))
	out = append(out, "if [ -z \"${bin}\" ]; then echo \"could not find "+cmd+" in archive\" >&2; exit 1; fi")
	out = append(out, fmt.Sprintf("install -m 0755 \"${bin}\" /usr/local/bin/%s", cmd))
	return strings.Join(out, "\n"), nil
}

func (b Binary) Runner() (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	var s strings.Builder
	s.WriteString(acpRunPreamble())
	writeEnv(&s, b.Env)
	cmd := filepath.Base(b.Cmd)
	if len(b.Args) > 0 {
		fmt.Fprintf(&s, "exec %s %s\n", cmd, QuoteAll(b.Args))
	} else {
		fmt.Fprintf(&s, "exec %s\n", cmd)
	}
	return s.String(), nil
}

func (n Npx) Validate() error {
	if n.Package == "" {
		return fmt.Errorf("npx distribution: empty package")
	}
	if !ValidToken(n.Package) {
		return fmt.Errorf("npx distribution: invalid package %q", n.Package)
	}
	if err := validateEnv(n.Env); err != nil {
		return fmt.Errorf("npx distribution: %w", err)
	}
	if err := validateArgs(n.Args); err != nil {
		return fmt.Errorf("npx distribution: %w", err)
	}
	return nil
}

func (Npx) Packages() []string { return []string{"nodejs", "npm"} }

func (n Npx) Installer() (string, error) {
	if err := n.Validate(); err != nil {
		return "", err
	}
	out := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		fmt.Sprintf("npm install -g %s", n.Package),
	}
	return strings.Join(out, "\n"), nil
}

func (n Npx) Runner() (string, error) {
	if err := n.Validate(); err != nil {
		return "", err
	}
	var s strings.Builder
	s.WriteString(acpRunPreamble())
	writeEnv(&s, n.Env)
	// use npx to run to avoid guessing the executable name
	// we should have it eagerly installed in cloud-init using `npm install -g`
	if len(n.Args) > 0 {
		fmt.Fprintf(&s, "exec npx %s %s\n", Quote(n.Package), QuoteAll(n.Args))
	} else {
		fmt.Fprintf(&s, "exec npx %s\n", Quote(n.Package))
	}
	return s.String(), nil
}

// Factorize out the start of the script
func acpRunPreamble() string {
	return "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"umask 077\n" +
		": \"${ACP_WORKDIR:=/data}\"\n" +
		"mkdir -p \"${ACP_WORKDIR}\"\n" +
		"cd \"${ACP_WORKDIR}\"\n"
}

// Write env files explicitly
func writeEnv(s *strings.Builder, env map[string]string) {
	if len(env) == 0 {
		return
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(s, "export %s=%s\n", k, Quote(env[k]))
	}
}

// ensure every key is ValidToken and no newlines, otherwise Quote ensures safety
func validateEnv(env map[string]string) error {
	for k, v := range env {
		if !ValidToken(k) {
			return fmt.Errorf("invalid env var name %q", k)
		}
		if strings.ContainsAny(v, "\n\r") {
			return fmt.Errorf("env var %q value contains a newline", k)
		}
	}
	return nil
}

// Ensure no newline
func validateArgs(args []string) error {
	for i, a := range args {
		if strings.ContainsAny(a, "\n\r") {
			return fmt.Errorf("argument %d contains a newline", i)
		}
	}
	return nil
}

// Ensure URL encoding is preserved (e.g. %2B -> + ). Afterwards use basename
func archiveBasename(archiveURL string) (string, error) {
	raw := filepath.Base(archiveURL)
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL encoding in archive basename %q: %w", raw, err)
	}
	return filepath.Base(decoded), nil
}

func isHex(s string) bool {
	for _, r := range s {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !ok {
			return false
		}
	}
	return true
}
