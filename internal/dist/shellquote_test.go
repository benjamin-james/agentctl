package dist

import (
	"strings"
	"testing"
)

func TestValidToken(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"codex", true},
		{"codex-acp", true},
		{"codex_acp", true},
		{"codex.acp", true},
		{"codex2", true},
		// Note: full paths like /usr/local/bin/codex are NOT valid tokens;
		// consumers apply filepath.Base first. Leading / is rejected.
		{"/usr/local/bin/codex", false},
		{"@agentclientprotocol/codex-acp", true},
		{"@agentclientprotocol/codex-acp@1.1.0", true},
		{"codex-acp@1.1.0", true},
		{"codex-acp@^1.1.0", true},
		{"FOO_BAR", true},
		{"a", true},
		{"", false},
		{"bad;rm -rf /", false},
		{"bad arg", false},
		{"bad`tick", false},
		{"bad$var", false},
		{"bad\"quote", false},
		{"bad|pipe", false},
		{"bad\nnewline", false},
		{"@bad/space name", false},
		{".leadingdot", false},
		{"-leadinghyphen", false},
	}
	for _, c := range cases {
		if got := ValidToken(c.in); got != c.want {
			t.Errorf("ValidToken(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValidUsername(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"agent", true},
		{"a", true},
		{"agent-user", true},
		{"agent_user", true},
		{"agent.user", true},
		{"a123", true},
		{strings.Repeat("a", 32), true},
		{"", false},
		{"1leading", false},
		{"-leading", false},
		{".leading", false},
		{"has/slash", false},
		{"has@at", false},
		{"has space", false},
		{strings.Repeat("a", 33), false},
	}
	for _, c := range cases {
		if got := ValidUsername(c.in); got != c.want {
			t.Errorf("ValidUsername(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCheckToken(t *testing.T) {
	if err := CheckToken("codex", "command"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := CheckToken("bad;rm", "command"); err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestCheckUsername(t *testing.T) {
	if err := CheckUsername("agent"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := CheckUsername("1bad"); err == nil {
		t.Fatal("expected error for invalid username")
	}
}

func TestQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "''"},
		{"simple", "'simple'"},
		{"a b c", "'a b c'"},
		{"a'b", "'a'\\''b'"},
		{"a$(evil)", "'a$(evil)'"},
		{"a`tick`", "'a`tick`'"},
		{"a\\b", "'a\\b'"},
		{"a\"b", "'a\"b'"},
		{"a;b", "'a;b'"},
	}
	for _, c := range cases {
		if got := Quote(c.in); got != c.want {
			t.Errorf("Quote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuoteAll(t *testing.T) {
	got := QuoteAll([]string{"--port", "8080", "a b"})
	want := "'--port' '8080' 'a b'"
	if got != want {
		t.Errorf("QuoteAll = %q, want %q", got, want)
	}
	if got := QuoteAll(nil); got != "" {
		t.Errorf("QuoteAll(nil) = %q, want empty", got)
	}
}

// TestQuoteShellSafe is a defense-in-depth check: the output of Quote, when
// placed in a double-quoted shell context, must not allow command substitution
// or variable expansion to leak. We verify the literal contains no unquoted
// $ or ` by checking that every such character sits inside a single-quoted
// region. A simpler proxy: Quote(x) must contain the raw x between quotes with
// only the '\” escape applied, so no $ or ` from x can be interpreted.
func TestQuoteShellSafe(t *testing.T) {
	evil := "$(rm -rf /)`echo pwned`$HOME"
	q := Quote(evil)
	// The quoted form must wrap the entire string in single quotes with no
	// unescaped single quote breaking out, so the dangerous payload is inert.
	if !strings.HasPrefix(q, "'") || !strings.HasSuffix(q, "'") {
		t.Fatalf("Quote must wrap in single quotes, got %q", q)
	}
	// Reconstruct: strip outer quotes, undo '\'' -> ', should equal input.
	inner := q[1 : len(q)-1]
	reconstructed := strings.ReplaceAll(inner, `'\''`, `'`)
	if reconstructed != evil {
		t.Errorf("Quote round-trip mismatch: got %q want %q", reconstructed, evil)
	}
}
