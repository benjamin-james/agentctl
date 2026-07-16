package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/benjamin-james/agentctl/internal/agent"
	"github.com/benjamin-james/agentctl/internal/cloudinit"
	"github.com/benjamin-james/agentctl/internal/dist"
)

// TestEndToEndBinaryDistribution exercises the full pipeline against an
// httptest registry serving a binary distribution (non-GitHub archive URL, so
// Resolve's default GitHubAPIFetcher skips integrity fetching without network).
func TestEndToEndBinaryDistribution(t *testing.T) {
	plat := dist.CurrentPlatform()
	registryJSON := `{
		"version": "1.0",
		"agents": [
			{
				"id": "codex-acp",
				"name": "Codex",
				"version": "1.1.0",
				"distribution": {
					"binary": {
						"` + plat.Key() + `": {
							"archive": "https://cdn.example.com/codex.tar.gz",
							"cmd": "codex",
							"args": ["--acp"],
							"env": {"CODEX_HOME": "/data/.codex"}
						}
					}
				}
			}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, registryJSON)
	}))
	defer srv.Close()

	reg, err := dist.GetRegistry(srv.URL)
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}
	a, ok := agent.GetAgent("codex")
	if !ok {
		t.Fatal("codex agent not found")
	}
	d, err := dist.Resolve(reg, a.AcpID, plat, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok := dist.AsBinary(d); !ok {
		t.Fatalf("expected *Binary, got %T", d)
	}
	cc, err := cloudinit.Build(cloudinit.Options{
		User:           "agent",
		Agent:          a,
		Dist:           d,
		AuthorizedKeys: []string{"ssh-rsa AAAA integration@host"},
		SecretsData:    `{"token":"secret"}`,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	out, err := cloudinit.Marshal(cc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	assertContains(t, out, "#cloud-config")
	assertContains(t, out, "package_update: true")
	assertContains(t, out, "rsync")
	assertContains(t, out, "wget")       // binary installer
	assertContains(t, out, "find")       // find-based extraction
	assertContains(t, out, "exec codex") // runner
	assertContains(t, out, "CODEX_HOME") // binary env (previously dropped!)
	assertContains(t, out, "/dev/shm/acp-secrets")
	assertContains(t, out, `chown -R "agent":"agent" /data`)
	assertNotContains(t, out, "nodejs") // binary dist, no nodejs
}

// Npx fallback. Swap arch to ensure lack of binary dist.
func TestEndToEndNpxFallback(t *testing.T) {
	plat := dist.CurrentPlatform()
	// Pick an arch key different from the current platform so binary miss.
	otherArch := "linux-aarch64"
	if plat.Arch == "aarch64" {
		otherArch = "linux-x86_64"
	}
	registryJSON := `{
		"version": "1.0",
		"agents": [
			{
				"id": "codex-acp",
				"name": "Codex",
				"version": "1.1.0",
				"distribution": {
					"binary": {
						"` + otherArch + `": {"archive": "https://cdn.example.com/codex.tar.gz", "cmd": "codex"}
					},
					"npx": {
						"package": "@agentclientprotocol/codex-acp@1.1.0",
						"args": ["--acp"],
						"env": {"AUGMENT_DISABLE_AUTO_UPDATE": "1"}
					}
				}
			}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, registryJSON)
	}))
	defer srv.Close()

	reg, err := dist.GetRegistry(srv.URL)
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}
	a, ok := agent.GetAgent("codex")
	if !ok {
		t.Fatal("codex agent not found")
	}
	d, err := dist.Resolve(reg, a.AcpID, plat, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok := dist.AsNpx(d); !ok {
		t.Fatalf("expected *Npx (fallback), got %T", d)
	}
	cc, err := cloudinit.Build(cloudinit.Options{
		User:           "agent",
		Agent:          a,
		Dist:           d,
		AuthorizedKeys: []string{"ssh-rsa AAAA integration@host"},
		ConfigData:     "[config]",
		ShareData:      true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	out, err := cloudinit.Marshal(cc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	assertContains(t, out, "nodejs")
	assertContains(t, out, "npm")
	assertContains(t, out, "npm install -g @agentclientprotocol/codex-acp@1.1.0")
	assertContains(t, out, "exec npx '@agentclientprotocol/codex-acp@1.1.0' '--acp'")
	assertContains(t, out, "AUGMENT_DISABLE_AUTO_UPDATE")
	assertContains(t, out, "9p") // share
	assertContains(t, out, "/dev/shm/acp-config")
}

// Guard for Env silently dropped in previous version
func TestEndToEndBinaryEnvNotDropped(t *testing.T) {
	plat := dist.CurrentPlatform()
	registryJSON := `{
		"version":"1.0",
		"agents":[{"id":"codex-acp","name":"Codex","version":"1.0",
		"distribution":{"binary":{"` + plat.Key() + `":
		{"archive":"https://cdn.example.com/c.tar.gz","cmd":"codex","env":{"MY_VAR":"my-value"}}}}}]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, registryJSON)
	}))
	defer srv.Close()

	reg, err := dist.GetRegistry(srv.URL)
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}
	d, err := dist.Resolve(reg, "codex-acp", plat, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	if !strings.Contains(runner, "export MY_VAR='my-value'") {
		t.Errorf("binary runner must export Env:\n%s", runner)
	}
}

// Empty sha256, avoid github api calls during tests
type noIntegrityFetcher struct{}

func (noIntegrityFetcher) FetchSHA256(string) (string, error) { return "", nil }

// Live ACP registry test.
func TestRealRegistrySmokeTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	skipIfNoBash(t)

	reg, err := dist.GetRegistry("https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json")
	if err != nil {
		t.Skipf("registry unreachable, skipping: %v", err)
	}

	plat := dist.CurrentPlatform()
	var resolved, skipped int
	for _, a := range reg.Agents {
		d, err := dist.Resolve(reg, a.ID, plat, noIntegrityFetcher{})
		if err != nil {
			if dist.IsArchUnsupported(err) {
				skipped++
				continue
			}
			t.Errorf("agent %q: resolve failed: %v", a.ID, err)
			continue
		}
		resolved++

		installer, err := d.Installer()
		if err != nil {
			t.Errorf("agent %q: installer generation failed: %v", a.ID, err)
			continue
		}
		checkBashSyntax(t, a.ID+" installer", installer)

		runner, err := d.Runner()
		if err != nil {
			t.Errorf("agent %q: runner generation failed: %v", a.ID, err)
			continue
		}
		checkBashSyntax(t, a.ID+" runner", runner)
	}

	t.Logf("resolved %d agents, skipped %d (unsupported arch), total %d", resolved, skipped, len(reg.Agents))
	if resolved == 0 {
		t.Error("expected at least one resolved agent")
	}
}

// Runs `bash -n` on installer
func TestGeneratedScriptsAreValidBash(t *testing.T) {
	skipIfNoBash(t)

	cases := []struct {
		name string
		d    dist.Distribution
	}{
		{"binary with args+env", &dist.Binary{
			Archive: "https://example.com/codex.tar.gz",
			Cmd:     "/usr/local/bin/codex",
			Args:    []string{"--acp", "--port", "8080"},
			Env:     map[string]string{"FOO": "bar", "BAZ": "qux"},
		}},
		{"binary url-encoded basename", &dist.Binary{
			Archive: "https://sfc-repo.example.com/cortex/1.0.73%2B180523/coco-1.0.73%2B180523-linux-amd64.tar.gz",
			Cmd:     "./cortex",
		}},
		{"binary with sha256", &dist.Binary{
			Archive: "https://example.com/codex.tar.gz",
			Cmd:     "codex",
			SHA256:  strings.Repeat("a", 64),
		}},
		{"npx with args+env", &dist.Npx{
			Package: "@agentclientprotocol/codex-acp@1.1.0",
			Args:    []string{"--acp"},
			Env:     map[string]string{"DISABLE_UPDATE": "1"},
		}},
		{"npx no args", &dist.Npx{
			Package: "simple-agent@0.1",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installer, err := tc.d.Installer()
			if err != nil {
				t.Fatalf("installer: %v", err)
			}
			checkBashSyntax(t, "installer", installer)

			runner, err := tc.d.Runner()
			if err != nil {
				t.Fatalf("runner: %v", err)
			}
			checkBashSyntax(t, "runner", runner)
		})
	}
}

func skipIfNoBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
}

func checkBashSyntax(t *testing.T, name, script string) {
	t.Helper()
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("bash -n failed for %s:\n--- script ---\n%s--- error ---\n%s", name, script, output)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q\n--- output ---\n%s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output to NOT contain %q\n--- output ---\n%s", needle, haystack)
	}
}
