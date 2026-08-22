package dist

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- parseRegistry ---

func TestParseRegistryBinaryAndNpx(t *testing.T) {
	input := `{
		"version": "1.0",
		"agents": [
			{
				"id": "codex-acp",
				"name": "Codex",
				"version": "1.1.0",
				"distribution": {
					"binary": {
						"linux-x86_64": {"archive": "https://example.com/codex.tar.gz", "cmd": "codex"}
					},
					"npx": {
						"package": "@agentclientprotocol/codex-acp@1.1.0",
						"args": ["--acp"],
						"env": {"FOO": "bar"}
					}
				}
			}
		]
	}`
	reg, err := parseRegistry([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.Version != "1.0" {
		t.Errorf("Version = %q", reg.Version)
	}
	if len(reg.Agents) != 1 {
		t.Fatalf("Agents len = %d", len(reg.Agents))
	}
	a := reg.Agents[0]
	if a.ID != "codex-acp" || a.Name != "Codex" || a.Version != "1.1.0" {
		t.Errorf("agent header = %+v", a)
	}
	bin, ok := a.Dist.Binary["linux-x86_64"]
	if !ok {
		t.Fatal("expected linux-x86_64 binary entry")
	}
	if bin.Archive != "https://example.com/codex.tar.gz" || bin.Cmd != "codex" {
		t.Errorf("binary = %+v", bin)
	}
	if bin.SHA256 != "" {
		t.Errorf("SHA256 should be empty pre-resolution, got %q", bin.SHA256)
	}
	if a.Dist.Npx == nil || a.Dist.Npx.Package != "@agentclientprotocol/codex-acp@1.1.0" {
		t.Errorf("npx = %+v", a.Dist.Npx)
	}
}

func TestParseRegistryNoAgents(t *testing.T) {
	input := `{"version":"1.0","agents":[]}`
	if _, err := parseRegistry([]byte(input)); err == nil {
		t.Fatal("expected error for no agents")
	}
}

func TestParseRegistryEmptyID(t *testing.T) {
	input := `{"version":"1.0","agents":[{"id":"","name":"X","version":"0.1","distribution":{"npx":{"package":"x@1"}}}]}`
	if _, err := parseRegistry([]byte(input)); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestParseRegistryEmptyName(t *testing.T) {
	input := `{"version":"1.0","agents":[{"id":"x","name":"","version":"0.1","distribution":{"npx":{"package":"x@1"}}}]}`
	if _, err := parseRegistry([]byte(input)); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestParseRegistryEmptyVersion(t *testing.T) {
	input := `{"version":"1.0","agents":[{"id":"x","name":"X","version":"","distribution":{"npx":{"package":"x@1"}}}]}`
	if _, err := parseRegistry([]byte(input)); err == nil {
		t.Fatal("expected error for empty version")
	}
}

func TestParseRegistryDuplicateID(t *testing.T) {
	input := `{"version":"1.0","agents":[
		{"id":"x","name":"X","version":"0.1","distribution":{"npx":{"package":"x@1"}}},
		{"id":"x","name":"X2","version":"0.2","distribution":{"npx":{"package":"x@2"}}}
	]}`
	if _, err := parseRegistry([]byte(input)); err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

func TestParseRegistryNoDistribution(t *testing.T) {
	input := `{"version":"1.0","agents":[{"id":"x","name":"X","version":"0.1"}]}`
	if _, err := parseRegistry([]byte(input)); err == nil {
		t.Fatal("expected error for agent with no distribution")
	}
}

func TestParseRegistryAcceptsCrossPlatformEntries(t *testing.T) {
	// Windows entries with backslashes in cmd must not cause parse failure,
	// they are only validated (against Linux shell-safety rules) when Resolve
	// selects them for the current platform.
	input := `{
		"version": "1.0",
		"agents": [
			{
				"id": "cross",
				"name": "Cross",
				"version": "1.0",
				"distribution": {
					"binary": {
						"linux-x86_64": {"archive": "https://example.com/agent-linux.tar.gz", "cmd": "./agent"},
						"windows-x86_64": {"archive": "https://example.com/agent-win.zip", "cmd": ".\\dist-package\\agent.cmd"}
					}
				}
			}
		]
	}`
	reg, err := parseRegistry([]byte(input))
	if err != nil {
		t.Fatalf("parseRegistry should accept cross-platform entries: %v", err)
	}
	if len(reg.Agents) != 1 {
		t.Fatalf("Agents len = %d, want 1", len(reg.Agents))
	}
}

func TestParseRegistryRoundTripJSON(t *testing.T) {
	original := RegistryData{
		Version: "1.0",
		Agents: []RegistryAgent{
			{
				ID: "codex-acp", Name: "Codex", Version: "0.1",
				Dist: RegistryDist{Npx: &Npx{Package: "codex-acp@0.1"}},
			},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	parsed, err := parseRegistry(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Agents[0].Dist.Npx.Package != "codex-acp@0.1" {
		t.Errorf("Package = %q", parsed.Agents[0].Dist.Npx.Package)
	}
}

// --- resolveGitHubAPIURL ---

func TestResolveGitHubAPIURL(t *testing.T) {
	cases := []struct {
		url    string
		wantOK bool
		want   string
	}{
		{
			"https://github.com/owner/repo/releases/download/v1.0/codex.tar.gz",
			true,
			"https://api.github.com/repos/owner/repo/releases/tags/v1.0",
		},
		{"https://example.com/codex.tar.gz", false, ""},
		{"https://github.com/owner/repo/archive/v1.0.zip", false, ""},
	}
	for _, c := range cases {
		got, ok := resolveGitHubAPIURL(c.url)
		if ok != c.wantOK {
			t.Errorf("resolveGitHubAPIURL(%q) ok=%v, want %v", c.url, ok, c.wantOK)
		}
		if ok && got != c.want {
			t.Errorf("resolveGitHubAPIURL(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

// --- GitHubAPIFetcher.FetchSHA256 ---

func TestFetchSHA256NonGitHubURL(t *testing.T) {
	f := GitHubAPIFetcher{}
	got, err := f.FetchSHA256("https://cdn.example.com/codex.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("non-GitHub URL should yield empty digest, got %q", got)
	}
}

func TestFetchSHA256FromReleaseExactMatch(t *testing.T) {
	archiveURL := "https://github.com/owner/repo/releases/download/v1.0/codex.tar.gz"
	hash := strings.Repeat("a", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); !strings.Contains(got, "vnd.github+json") {
			t.Errorf("expected GitHub Accept header, got %q", got)
		}
		release := gitHubRelease{Assets: []gitHubAsset{
			{Name: "codex.tar.gz", BrowserDownloadURL: archiveURL, Digest: "sha256:" + hash},
		}}
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	f := GitHubAPIFetcher{}
	got, err := f.fetchFromRelease(srv.URL, archiveURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != hash {
		t.Errorf("digest = %q, want %q", got, hash)
	}
}

func TestFetchSHA256FromReleaseBasenameFallback(t *testing.T) {
	// Archive URL points at github.com/owner/repo but the API returns assets
	// under a renamed/forked repo URL. Exact match fails; basename fallback
	// should resolve the asset.
	archiveURL := "https://github.com/owner/repo/releases/download/v1.0/codex.tar.gz"
	renamedURL := "https://github.com/renamed-owner/repo/releases/download/v1.0/codex.tar.gz"
	hash := strings.Repeat("b", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := gitHubRelease{Assets: []gitHubAsset{
			{Name: "codex.tar.gz", BrowserDownloadURL: renamedURL, Digest: "sha256:" + hash},
		}}
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	f := GitHubAPIFetcher{}
	got, err := f.fetchFromRelease(srv.URL, archiveURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != hash {
		t.Errorf("digest = %q, want %q (basename fallback)", got, hash)
	}
}

func TestFetchSHA256FromReleaseNoDigest(t *testing.T) {
	archiveURL := "https://github.com/owner/repo/releases/download/v1.0/codex.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := gitHubRelease{Assets: []gitHubAsset{
			{Name: "codex.tar.gz", BrowserDownloadURL: archiveURL, Digest: ""},
		}}
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	f := GitHubAPIFetcher{}
	got, err := f.fetchFromRelease(srv.URL, archiveURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("empty asset digest should yield empty hash, got %q", got)
	}
}

func TestFetchSHA256FromReleaseBadDigestFormat(t *testing.T) {
	archiveURL := "https://github.com/owner/repo/releases/download/v1.0/codex.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := gitHubRelease{Assets: []gitHubAsset{
			{Name: "codex.tar.gz", BrowserDownloadURL: archiveURL, Digest: "md5:abc"},
		}}
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	f := GitHubAPIFetcher{}
	if _, err := f.fetchFromRelease(srv.URL, archiveURL); err == nil {
		t.Fatal("expected error for non-sha256 digest")
	}
}

func TestFetchSHA256FromReleaseNoMatchingAsset(t *testing.T) {
	archiveURL := "https://github.com/owner/repo/releases/download/v1.0/codex.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := gitHubRelease{Assets: []gitHubAsset{
			{Name: "other.tar.gz", BrowserDownloadURL: "https://github.com/owner/repo/releases/download/v1.0/other.tar.gz", Digest: "sha256:" + strings.Repeat("c", 64)},
		}}
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	f := GitHubAPIFetcher{}
	if _, err := f.fetchFromRelease(srv.URL, archiveURL); err == nil {
		t.Fatal("expected error for no matching asset")
	}
}

func TestFetchSHA256FromReleaseHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := GitHubAPIFetcher{}
	if _, err := f.fetchFromRelease(srv.URL, "https://github.com/o/r/releases/download/v1/x.tar.gz"); err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

// GitHubAPIFetcher.FetchLatestAsset

func TestFetchLatestAssetSuccess(t *testing.T) {
	downloadURL := "https://github.com/owner/repo/releases/download/v0.0.42/agentctl-linux-x86_64.tar.gz"
	hash := strings.Repeat("a", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releases := []gitHubRelease{{
			Assets: []gitHubAsset{
				{Name: "CHECKSUMS", BrowserDownloadURL: "https://github.com/owner/repo/releases/download/v0.0.42/CHECKSUMS", Digest: ""},
				{Name: "agentctl-linux-x86_64.tar.gz", BrowserDownloadURL: downloadURL, Digest: "sha256:" + hash},
			},
		}}
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer srv.Close()

	f := GitHubAPIFetcher{}
	releases, err := f.fetchReleases(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}
	url, sha, err := fetchLatestFromReleases(releases, "agentctl-linux-x86_64.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != downloadURL {
		t.Errorf("download URL = %q, want %q", url, downloadURL)
	}
	if sha != hash {
		t.Errorf("sha256 = %q, want %q", sha, hash)
	}
}

func TestFetchLatestAssetNoReleases(t *testing.T) {
	if _, _, err := fetchLatestFromReleases(nil, "x"); err == nil {
		t.Fatal("expected error for no releases")
	}
}

func TestFetchLatestAssetNoMatchingAsset(t *testing.T) {
	releases := []gitHubRelease{{
		Assets: []gitHubAsset{
			{Name: "other.tar.gz", BrowserDownloadURL: "https://github.com/owner/repo/releases/download/v0.0.42/other.tar.gz", Digest: "sha256:" + strings.Repeat("c", 64)},
		},
	}}
	if _, _, err := fetchLatestFromReleases(releases, "agentctl-linux-x86_64.tar.gz"); err == nil {
		t.Fatal("expected error for no matching asset")
	}
}

func TestFetchLatestAssetHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := GitHubAPIFetcher{}
	if _, err := f.fetchReleases(srv.URL); err == nil {
		t.Fatal("expected error for HTTP 404")
	}
}

func TestFetchLatestAssetBadDigestFormat(t *testing.T) {
	releases := []gitHubRelease{{
		Assets: []gitHubAsset{
			{Name: "agentctl-linux-x86_64.tar.gz", BrowserDownloadURL: "https://github.com/owner/repo/releases/download/v0.0.42/agentctl-linux-x86_64.tar.gz", Digest: "md5:abc"},
		},
	}}
	if _, _, err := fetchLatestFromReleases(releases, "agentctl-linux-x86_64.tar.gz"); err == nil {
		t.Fatal("expected error for bad digest format")
	}
}

func TestFetchLatestAssetPicksFirstRelease(t *testing.T) {
	// The API returns newest-first; verify we pick the first one.
	newURL := "https://github.com/owner/repo/releases/download/v0.0.43/agentctl-linux-x86_64.tar.gz"
	oldURL := "https://github.com/owner/repo/releases/download/v0.0.42/agentctl-linux-x86_64.tar.gz"
	releases := []gitHubRelease{
		{Assets: []gitHubAsset{
			{Name: "agentctl-linux-x86_64.tar.gz", BrowserDownloadURL: newURL, Digest: "sha256:" + strings.Repeat("a", 64)},
		}},
		{Assets: []gitHubAsset{
			{Name: "agentctl-linux-x86_64.tar.gz", BrowserDownloadURL: oldURL, Digest: "sha256:" + strings.Repeat("b", 64)},
		}},
	}
	url, sha, err := fetchLatestFromReleases(releases, "agentctl-linux-x86_64.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != newURL {
		t.Errorf("download URL = %q, want %q (newest)", url, newURL)
	}
	if sha != strings.Repeat("a", 64) {
		t.Errorf("sha256 = %q, want newest digest", sha)
	}
}

func TestFetchLatestAssetEmptyDigest(t *testing.T) {
	downloadURL := "https://github.com/owner/repo/releases/download/v0.0.42/agentctl-linux-x86_64.tar.gz"
	releases := []gitHubRelease{{
		Assets: []gitHubAsset{
			{Name: "agentctl-linux-x86_64.tar.gz", BrowserDownloadURL: downloadURL, Digest: ""},
		},
	}}
	url, sha, err := fetchLatestFromReleases(releases, "agentctl-linux-x86_64.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != downloadURL {
		t.Errorf("download URL = %q, want %q", url, downloadURL)
	}
	if sha != "" {
		t.Errorf("expected empty digest, got %q", sha)
	}
}

// --- GetRegistry ---

func TestGetRegistrySuccess(t *testing.T) {
	body := `{"version":"1.0","agents":[{"id":"x","name":"X","version":"0.1","distribution":{"npx":{"package":"x@1"}}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	reg, err := GetRegistry(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.Agents) != 1 {
		t.Errorf("Agents len = %d", len(reg.Agents))
	}
}

func TestGetRegistryHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := GetRegistry(srv.URL); err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestGetRegistryInvalidURL(t *testing.T) {
	if _, err := GetRegistry("ftp://bad"); err == nil {
		t.Fatal("expected error for non-https registry URL")
	}
}
