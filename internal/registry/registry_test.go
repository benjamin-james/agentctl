package registry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/benjamin-james/agentctl/internal/agent"
)

func TestParseRegistryValid(t *testing.T) {
	reg := RegistryData{
		Version: "1.0",
		Agents: []RegistryAgent{
			{ID: "codex-acp", Name: "Codex", Version: "0.1"},
		},
	}
	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseRegistry(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Version != "1.0" {
		t.Errorf("Version = %q, want %q", parsed.Version, "1.0")
	}
	if len(parsed.Agents) != 1 {
		t.Fatalf("Agents len = %d, want 1", len(parsed.Agents))
	}
	if parsed.Agents[0].ID != "codex-acp" {
		t.Errorf("Agent ID = %q, want %q", parsed.Agents[0].ID, "codex-acp")
	}
}

func TestParseRegistryInvalidJSON(t *testing.T) {
	_, err := parseRegistry([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseRegistryNoAgents(t *testing.T) {
	data, _ := json.Marshal(RegistryData{Version: "1.0", Agents: []RegistryAgent{}})
	_, err := parseRegistry(data)
	if err == nil {
		t.Fatal("expected error for empty agents list")
	}
}

func TestParseRegistryMissingAgentsField(t *testing.T) {
	_, err := parseRegistry([]byte(`{"version":"1.0"}`))
	if err == nil {
		t.Fatal("expected error for missing agents field")
	}
}

func TestGetBinaryForAgentFound(t *testing.T) {
	dist := getdist()
	reg := &RegistryData{
		Agents: []RegistryAgent{
			{
				ID: "codex-acp",
				Dist: RegistryDist{
					Binary: map[string]RegistryPlatformBinary{
						dist: {Archive: "https://example.com/codex.tar.gz", Cmd: "codex"},
					},
				},
			},
		},
	}
	a, ok := agent.GetAgent("codex")
	if !ok {
		t.Errorf("codex not supported")
	}
	resolved, err := reg.GetBinaryForAgent(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Binary.Archive != "https://example.com/codex.tar.gz" {
		t.Errorf("Archive = %q, want expected URL", resolved.Binary.Archive)
	}
	if resolved.Binary.Cmd != "codex" {
		t.Errorf("Cmd = %q, want %q", resolved.Binary.Cmd, "codex")
	}
}

func TestGetBinaryForAgentNotFound(t *testing.T) {
	reg := &RegistryData{
		Agents: []RegistryAgent{
			{ID: "other-agent"},
		},
	}
	a, ok := agent.GetAgent("codex")
	if !ok {
		t.Errorf("codex not supported")
	}
	_, err := reg.GetBinaryForAgent(a)
	if err == nil {
		t.Fatal("expected error for agent not in registry")
	}
}

func TestGetBinaryForAgentArchNotSupported(t *testing.T) {
	reg := &RegistryData{
		Agents: []RegistryAgent{
			{
				ID: "codex-acp",
				Dist: RegistryDist{
					Binary: map[string]RegistryPlatformBinary{
						"unsupported-arch": {Archive: "https://example.com/codex.tar.gz", Cmd: "codex"},
					},
				},
			},
		},
	}
	a, ok := agent.GetAgent("codex")
	if !ok {
		t.Errorf("codex not supported")
	}
	_, err := reg.GetBinaryForAgent(a)
	if err == nil {
		t.Fatal("expected error for unsupported architecture")
	}
}

func TestGetdistFormat(t *testing.T) {
	d := getdist()
	if d == "" {
		t.Fatal("getdist returned empty string")
	}
	expectedArch := runtime.GOARCH
	switch runtime.GOARCH {
	case "amd64":
		expectedArch = "x86_64"
	case "arm64":
		expectedArch = "aarch64"
	}
	expected := "linux-" + expectedArch
	if d != expected {
		t.Errorf("getdist() = %q, want %q", d, expected)
	}
}

func TestParseRegistryWithBinaryDistribution(t *testing.T) {
	input := `{
		"version": "1.0",
		"agents": [
			{
				"id": "codex-acp",
				"name": "Codex",
				"version": "0.1",
				"distribution": {
					"binary": {
						"linux-x86_64": {
							"archive": "https://example.com/codex.tar.gz",
							"cmd": "codex",
							"args": ["--serve"],
							"env": {"FOO": "bar"}
						}
					}
				}
			}
		]
	}`
	parsed, err := parseRegistry([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bin := parsed.Agents[0].Dist.Binary["linux-x86_64"]
	if bin.Archive != "https://example.com/codex.tar.gz" {
		t.Errorf("Archive = %q", bin.Archive)
	}
	if bin.Cmd != "codex" {
		t.Errorf("Cmd = %q", bin.Cmd)
	}
	if len(bin.Args) != 1 || bin.Args[0] != "--serve" {
		t.Errorf("Args = %v", bin.Args)
	}
	if bin.Env["FOO"] != "bar" {
		t.Errorf("Env = %v", bin.Env)
	}
}

func TestFetchSHA256NonGitHubURL(t *testing.T) {
	sha256, err := fetchSHA256("https://cdn.example.com/codex.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha256 != "" {
		t.Errorf("expected empty SHA256 for non-GitHub URL, got %q", sha256)
	}
}

func TestFetchSHA256FromRelease(t *testing.T) {
	releaseResp := `{
		"assets": [
			{
				"name": "codex.tar.gz",
				"browser_download_url": "https://github.com/owner/repo/releases/download/v1.0.0/codex.tar.gz",
				"digest": "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
			},
			{
				"name": "other.tar.gz",
				"browser_download_url": "https://github.com/owner/repo/releases/download/v1.0.0/other.tar.gz",
				"digest": "sha256:0000000000000000000000000000000000000000000000000000000000000000"
			}
		]
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("expected Accept header application/vnd.github+json, got %q", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(releaseResp))
	}))
	defer server.Close()

	sha256, err := fetchSHA256FromRelease(server.URL, "https://github.com/owner/repo/releases/download/v1.0.0/codex.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha256 != "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890" {
		t.Errorf("SHA256 = %q, want expected hash", sha256)
	}
}

func TestFetchSHA256FromReleaseNoDigest(t *testing.T) {
	releaseResp := `{
		"assets": [
			{
				"name": "codex.tar.gz",
				"browser_download_url": "https://github.com/owner/repo/releases/download/v1.0.0/codex.tar.gz",
				"digest": ""
			}
		]
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(releaseResp))
	}))
	defer server.Close()

	sha256, err := fetchSHA256FromRelease(server.URL, "https://github.com/owner/repo/releases/download/v1.0.0/codex.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha256 != "" {
		t.Errorf("expected empty SHA256 when digest is absent, got %q", sha256)
	}
}

func TestFetchSHA256FromReleaseAssetNotFound(t *testing.T) {
	releaseResp := `{
		"assets": [
			{
				"name": "other.tar.gz",
				"browser_download_url": "https://github.com/owner/repo/releases/download/v1.0.0/other.tar.gz",
				"digest": "sha256:aaa"
			}
		]
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(releaseResp))
	}))
	defer server.Close()

	_, err := fetchSHA256FromRelease(server.URL, "https://github.com/owner/repo/releases/download/v1.0.0/codex.tar.gz")
	if err == nil {
		t.Fatal("expected error when no matching asset found")
	}
}

func TestFetchSHA256FromReleaseBadDigest(t *testing.T) {
	releaseResp := `{
		"assets": [
			{
				"name": "codex.tar.gz",
				"browser_download_url": "https://github.com/owner/repo/releases/download/v1.0.0/codex.tar.gz",
				"digest": "md5:abcdef"
			}
		]
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(releaseResp))
	}))
	defer server.Close()

	_, err := fetchSHA256FromRelease(server.URL, "https://github.com/owner/repo/releases/download/v1.0.0/codex.tar.gz")
	if err == nil {
		t.Fatal("expected error for non-sha256 digest format")
	}
}

func TestFetchSHA256FromReleaseHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := fetchSHA256FromRelease(server.URL, "https://github.com/owner/repo/releases/download/v1.0.0/codex.tar.gz")
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
}

func TestFetchSHA256FromReleaseRepoRename(t *testing.T) {
	releaseResp := `{
		"assets": [
			{
				"name": "goose-x86_64-unknown-linux-gnu.tar.bz2",
				"browser_download_url": "https://github.com/aaif-goose/goose/releases/download/v1.36.0/goose-x86_64-unknown-linux-gnu.tar.bz2",
				"digest": "sha256:aaaabbbbccccddddaaaabbbbccccddddaaaabbbbccccddddaaaabbbbccccdddd"
			}
		]
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(releaseResp))
	}))
	defer server.Close()

	sha256, err := fetchSHA256FromRelease(server.URL, "https://github.com/block/goose/releases/download/v1.36.0/goose-x86_64-unknown-linux-gnu.tar.bz2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha256 != "aaaabbbbccccddddaaaabbbbccccddddaaaabbbbccccddddaaaabbbbccccdddd" {
		t.Errorf("SHA256 = %q, want expected hash", sha256)
	}
}
