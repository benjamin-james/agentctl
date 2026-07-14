// Package dist defines distribution kinds (binary, npx), the ACP registry
// fetch/parse layer, and platform-aware resolution of an agent's distribution.
//
// Distribution kinds are modeled as a closed sum type: the unexported
// isDistribution marker method on the Distribution interface ensures only
// types declared in this package can satisfy it, and consumers must use a type
// switch the compiler can check for exhaustiveness.
package dist

import (
	"fmt"
	"regexp"
	"strings"
)

// tokenRe matches strings safe to interpolate unquoted into shell scripts in
// "name"-like contexts: command basenames, env var keys, npx package
// specifiers. It unifies the previously overlapping safeNameRegex and
// npxPackageRe into one definition.
//
// Allowed: a leading letter or digit, then letters, digits, and the
// punctuation . _ / : - @ ~ ^ +. A leading @ is also permitted (for scoped
// npm packages like @scope/name). At least one character is required.
//
// This is intentionally permissive about punctuation (it allows / and . in env
// keys, matching the prior safeNameRegex) but excludes all shell
// metacharacters, whitespace, and quotes. Username validation has its own
// stricter rule (see ValidUsername).
var tokenRe = regexp.MustCompile(`^[a-zA-Z0-9@][a-zA-Z0-9._/:@~^+\-]*$`)

// usernameRe matches a valid cloud-init/Linux username: a leading ASCII letter
// followed by letters, digits, underscores, hyphens, or dots, max 32 chars.
// This matches the prior main.validName rule exactly and is stricter than
// ValidToken because usernames propagate into many contexts (home directory
// paths, systemd unit names, chown arguments).
var usernameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.\-]{0,31}$`)

// ValidToken reports whether s is safe to use unquoted in a shell "name"
// context. It is the single gate for command names, env var keys, and npx
// package specifiers.
func ValidToken(s string) bool {
	return tokenRe.MatchString(s)
}

// ValidUsername reports whether s is a valid Linux/cloud-init username (leading
// letter, then [a-zA-Z0-9_.-], max 32 chars). It is stricter than ValidToken.
func ValidUsername(s string) bool {
	return usernameRe.MatchString(s)
}

// CheckUsername returns an error if s is not a ValidUsername.
func CheckUsername(s string) error {
	if !ValidUsername(s) {
		return fmt.Errorf("invalid username %q: must match [a-zA-Z][a-zA-Z0-9_.-]{0,31}", s)
	}
	return nil
}

// CheckToken returns an error if s is not a ValidToken, naming context in the
// message so callers don't have to format it themselves.
func CheckToken(s, context string) error {
	if !ValidToken(s) {
		return fmt.Errorf("invalid %s %q: must match a safe token (letters, digits, and ._/-@~^+)", context, s)
	}
	return nil
}

// Quote wraps s in single quotes for safe interpolation into a POSIX shell,
// escaping any embedded single quote as '\”. Single-quoting is the only shell
// quoting mode in which the body is treated fully literally (no $, backtick,
// or backslash expansion), so it is the correct choice for arbitrary string
// values such as env var values.
//
// This replaces the previous use of Go's %q verb, which produces Go-string
// literals (double-quoted, with backslash escapes) that bash still evaluates
// for $/`/\\ inside the double quotes — a subtle injection surface.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// QuoteAll quotes each element of args with Quote and joins them with single
// spaces. It does not validate args: values may legitimately contain arbitrary
// characters, and Quote renders them safe. Callers wanting to restrict args to
// a fixed character set should validate separately.
func QuoteAll(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = Quote(a)
	}
	return strings.Join(parts, " ")
}
