package dist

import (
	"fmt"
	"regexp"
	"strings"
)

var tokenRe = regexp.MustCompile(`^[a-zA-Z0-9@][a-zA-Z0-9._/:@~^+\-]*$`)

var usernameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.\-]{0,31}$`)

func ValidToken(s string) bool {
	return tokenRe.MatchString(s)
}

func ValidUsername(s string) bool {
	return usernameRe.MatchString(s)
}

func CheckUsername(s string) error {
	if !ValidUsername(s) {
		return fmt.Errorf("invalid username %q: must match [a-zA-Z][a-zA-Z0-9_.-]{0,31}", s)
	}
	return nil
}

func CheckToken(s, context string) error {
	if !ValidToken(s) {
		return fmt.Errorf("invalid %s %q: must match a safe token (letters, digits, and ._/-@~^+)", context, s)
	}
	return nil
}

func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func QuoteAll(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = Quote(a)
	}
	return strings.Join(parts, " ")
}
