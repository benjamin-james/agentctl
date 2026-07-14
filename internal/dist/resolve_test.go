package dist

import (
	"errors"
	"strings"
	"testing"
)

// fakeFetcher is an IntegrityFetcher stub for tests.
type fakeFetcher struct {
	hash  string
	err   error
	calls int
}

func (f *fakeFetcher) FetchSHA256(archiveURL string) (string, error) {
	f.calls++
	return f.hash, f.err
}

func testRegistry(binaryKey string, npx bool) *RegistryData {
	dist := RegistryDist{}
	if binaryKey != "" {
		dist.Binary = map[string]Binary{
			binaryKey: {Archive: "https://example.com/c.tar.gz", Cmd: "codex"},
		}
	}
	if npx {
		dist.Npx = &Npx{Package: "@scope/codex@1.0"}
	}
	return &RegistryData{
		Version: "1.0",
		Agents: []RegistryAgent{
			{ID: "codex-acp", Name: "Codex", Version: "1.0", Dist: dist},
		},
	}
}

func plat(t *testing.T, key string) Platform {
	t.Helper()
	p, err := ParsePlatform(key)
	if err != nil {
		t.Fatalf("ParsePlatform: %v", err)
	}
	return p
}

func TestResolveBinaryFound(t *testing.T) {
	p := plat(t, "linux-x86_64")
	reg := testRegistry("linux-x86_64", false)
	hash := strings.Repeat("a", 64)
	f := &fakeFetcher{hash: hash}
	d, err := Resolve(reg, "codex-acp", p, f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.calls != 1 {
		t.Errorf("fetcher called %d times, want 1", f.calls)
	}
	b, ok := AsBinary(d)
	if !ok {
		t.Fatalf("expected *Binary, got %T", d)
	}
	if b.SHA256 != hash {
		t.Errorf("SHA256 = %q, want %q", b.SHA256, hash)
	}
}

func TestResolveBinaryPreferredOverNpx(t *testing.T) {
	p := plat(t, "linux-x86_64")
	// Both binary (for this arch) and npx present: binary must win.
	reg := testRegistry("linux-x86_64", true)
	d, err := Resolve(reg, "codex-acp", p, &fakeFetcher{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := AsBinary(d); !ok {
		t.Errorf("expected *Binary (preferred), got %T", d)
	}
}

func TestResolveArchUnsupportedWithNpxFallback(t *testing.T) {
	p := plat(t, "linux-x86_64")
	// Binary only for an unrelated arch; npx present: npx must be used.
	reg := testRegistry("linux-aarch64", true)
	d, err := Resolve(reg, "codex-acp", p, &fakeFetcher{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	n, ok := AsNpx(d)
	if !ok {
		t.Errorf("expected *Npx (fallback), got %T", d)
	}
	if n.Package != "@scope/codex@1.0" {
		t.Errorf("Package = %q", n.Package)
	}
}

func TestResolveNpxOnly(t *testing.T) {
	p := plat(t, "linux-x86_64")
	reg := testRegistry("", true)
	d, err := Resolve(reg, "codex-acp", p, &fakeFetcher{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := AsNpx(d); !ok {
		t.Errorf("expected *Npx, got %T", d)
	}
}

func TestResolveAgentUnknown(t *testing.T) {
	p := plat(t, "linux-x86_64")
	reg := testRegistry("linux-x86_64", false)
	_, err := Resolve(reg, "nonexistent", p, &fakeFetcher{})
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if !IsAgentUnknown(err) {
		t.Errorf("expected IsAgentUnknown, got: %v", err)
	}
	if IsArchUnsupported(err) {
		t.Errorf("IsArchUnsupported should be false for unknown agent")
	}
}

func TestResolveArchUnsupportedNoFallback(t *testing.T) {
	p := plat(t, "linux-x86_64")
	// Binary only for another arch, no npx: arch unsupported.
	reg := testRegistry("linux-aarch64", false)
	_, err := Resolve(reg, "codex-acp", p, &fakeFetcher{})
	if err == nil {
		t.Fatal("expected error for unsupported arch with no fallback")
	}
	if !IsArchUnsupported(err) {
		t.Errorf("expected IsArchUnsupported, got: %v", err)
	}
}

func TestResolveSHA256FetchError(t *testing.T) {
	p := plat(t, "linux-x86_64")
	reg := testRegistry("linux-x86_64", false)
	f := &fakeFetcher{err: errors.New("network down")}
	_, err := Resolve(reg, "codex-acp", p, f)
	if err == nil {
		t.Fatal("expected error when SHA256 fetch fails")
	}
	if !strings.Contains(err.Error(), "fetching SHA256") {
		t.Errorf("error should mention SHA256 fetch, got: %v", err)
	}
}

func TestResolveNilFetcherDefaultsToGitHub(t *testing.T) {
	// With a non-GitHub archive URL, the default GitHubAPIFetcher returns ""
	// (no integrity check) without error, so Resolve should succeed with an
	// empty SHA256.
	p := plat(t, "linux-x86_64")
	reg := testRegistry("linux-x86_64", false)
	d, err := Resolve(reg, "codex-acp", p, nil)
	if err != nil {
		t.Fatalf("unexpected error with nil fetcher: %v", err)
	}
	b, ok := AsBinary(d)
	if !ok {
		t.Fatalf("expected *Binary, got %T", d)
	}
	if b.SHA256 != "" {
		t.Errorf("SHA256 should be empty for non-GitHub URL, got %q", b.SHA256)
	}
}

func TestResolveErrorMessages(t *testing.T) {
	p := plat(t, "linux-x86_64")
	reg := testRegistry("linux-aarch64", false)
	_, err := Resolve(reg, "codex-acp", p, &fakeFetcher{})
	if !strings.Contains(err.Error(), "codex-acp") || !strings.Contains(err.Error(), "x86_64") {
		t.Errorf("arch-unsupported error should name agent and arch, got: %v", err)
	}

	reg2 := testRegistry("linux-x86_64", false)
	_, err2 := Resolve(reg2, "ghost", p, &fakeFetcher{})
	if !strings.Contains(err2.Error(), "ghost") {
		t.Errorf("unknown-agent error should name agent, got: %v", err2)
	}
}

func TestResolveRejectsNpxEmptyPackage(t *testing.T) {
	p := plat(t, "linux-x86_64")
	reg := &RegistryData{
		Agents: []RegistryAgent{
			{ID: "x", Name: "X", Version: "0.1", Dist: RegistryDist{Npx: &Npx{Package: ""}}},
		},
	}
	if _, err := Resolve(reg, "x", p, &fakeFetcher{}); err == nil {
		t.Fatal("expected error for empty npx package at resolve time")
	}
}

func TestResolveRejectsNpxInvalidPackage(t *testing.T) {
	p := plat(t, "linux-x86_64")
	reg := &RegistryData{
		Agents: []RegistryAgent{
			{ID: "x", Name: "X", Version: "0.1", Dist: RegistryDist{Npx: &Npx{Package: "bad;rm -rf /"}}},
		},
	}
	if _, err := Resolve(reg, "x", p, &fakeFetcher{}); err == nil {
		t.Fatal("expected error for invalid npx package at resolve time")
	}
}

func TestResolveRejectsBinaryBadURL(t *testing.T) {
	p := plat(t, "linux-x86_64")
	reg := &RegistryData{
		Agents: []RegistryAgent{
			{ID: "x", Name: "X", Version: "0.1", Dist: RegistryDist{Binary: map[string]Binary{
				"linux-x86_64": {Archive: "http://insecure/x.tar.gz", Cmd: "x"},
			}}},
		},
	}
	if _, err := Resolve(reg, "x", p, &fakeFetcher{}); err == nil {
		t.Fatal("expected error for non-https archive URL at resolve time")
	}
}

func TestResolveRejectsBinaryBadCmd(t *testing.T) {
	p := plat(t, "linux-x86_64")
	reg := &RegistryData{
		Agents: []RegistryAgent{
			{ID: "x", Name: "X", Version: "0.1", Dist: RegistryDist{Binary: map[string]Binary{
				"linux-x86_64": {Archive: "https://example.com/x.tar.gz", Cmd: "bad;rm"},
			}}},
		},
	}
	if _, err := Resolve(reg, "x", p, &fakeFetcher{}); err == nil {
		t.Fatal("expected error for bad binary cmd at resolve time")
	}
}

func TestResolveRejectsBinaryUnsafeEnvKey(t *testing.T) {
	p := plat(t, "linux-x86_64")
	reg := &RegistryData{
		Agents: []RegistryAgent{
			{ID: "x", Name: "X", Version: "0.1", Dist: RegistryDist{Binary: map[string]Binary{
				"linux-x86_64": {Archive: "https://example.com/x.tar.gz", Cmd: "x", Env: map[string]string{"BAD KEY": "1"}},
			}}},
		},
	}
	if _, err := Resolve(reg, "x", p, &fakeFetcher{}); err == nil {
		t.Fatal("expected error for unsafe env key at resolve time")
	}
}

func TestAsBinaryAsNpx(t *testing.T) {
	b := &Binary{Archive: "https://e.com/x.tar.gz", Cmd: "x"}
	n := &Npx{Package: "x@1"}
	if got, ok := AsBinary(b); !ok || got != b {
		t.Error("AsBinary should return the *Binary")
	}
	if _, ok := AsBinary(n); ok {
		t.Error("AsBinary should return false for *Npx")
	}
	if got, ok := AsNpx(n); !ok || got != n {
		t.Error("AsNpx should return the *Npx")
	}
	if _, ok := AsNpx(b); ok {
		t.Error("AsNpx should return false for *Binary")
	}
}
