package dist

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

type RegistryData struct {
	Version string          `json:"version"`
	Agents  []RegistryAgent `json:"agents"`
}

type RegistryAgent struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Version string       `json:"version"`
	Dist    RegistryDist `json:"distribution"`
}

// binary is keyed by platform (e.g. `linux-x86_64`)
// npx is a single platform-agnostic entry key
// Value types are both the Binary and Npx types directly. Uvx is not implemented for now
type RegistryDist struct {
	Binary map[string]Binary `json:"binary,omitempty"`
	Npx    *Npx              `json:"npx,omitempty"`
	Uvx    json.RawMessage   `json:"uvx,omitempty"`
}

var gitHubReleaseRe = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/releases/download/([^/]+)/`)

// 5 MiB max response size as before
const maxReleaseBody = 5 << 20

// largest JSON registry to read (10 MiB)
const maxRegistryBody = 10 << 20

type gitHubRelease struct {
	Assets []gitHubAsset `json:"assets"`
}

type gitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

// Default IntegrityFetcher interface impl. Non-GH return nil as in dist.go
type GitHubAPIFetcher struct {
	Client *http.Client
}

// A nil Client uses the shared newHTTPClient()
func (f GitHubAPIFetcher) http() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return newHTTPClient()
}

// Implementation of IntegrityFetcher's only function
func (f GitHubAPIFetcher) FetchSHA256(archiveURL string) (string, error) {
	apiURL, ok := resolveGitHubAPIURL(archiveURL)
	if !ok {
		return "", nil
	}
	return f.fetchFromRelease(apiURL, archiveURL)
}

// resolves release-download URI into release-by-tag API URL.
func resolveGitHubAPIURL(archiveURL string) (string, bool) {
	m := gitHubReleaseRe.FindStringSubmatch(archiveURL)
	if m == nil {
		return "", false
	}
	owner, repo, tag := m[1], m[2], m[3]
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag), true
}

// Given an API URL, return the digest for the asset matching the URL.
// Fallback by basename in case exact `browser_download_url` doesn't match
func (f GitHubAPIFetcher) fetchFromRelease(apiURL, archiveURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := f.http().Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching GitHub release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseBody))
	if err != nil {
		return "", fmt.Errorf("reading GitHub API response: %w", err)
	}

	var release gitHubRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("parsing GitHub API response: %w", err)
	}

	archiveFilename := filepath.Base(archiveURL)
	var fallback *gitHubAsset
	for i := range release.Assets {
		asset := &release.Assets[i]
		if asset.BrowserDownloadURL == archiveURL {
			return parseDigest(*asset)
		}
		if filepath.Base(asset.BrowserDownloadURL) == archiveFilename {
			fallback = asset
		}
	}
	if fallback != nil {
		return parseDigest(*fallback)
	}
	return "", fmt.Errorf("no matching asset found for %q in GitHub release", archiveURL)
}

// Extracts/parses hex SHA256 from `sha256:<hex>` digest, if exists
func parseDigest(asset gitHubAsset) (string, error) {
	if asset.Digest == "" {
		return "", nil
	}
	if !strings.HasPrefix(asset.Digest, "sha256:") {
		return "", fmt.Errorf("unexpected digest format %q for asset %q", asset.Digest, asset.Name)
	}
	return strings.TrimPrefix(asset.Digest, "sha256:"), nil
}

func GetRegistry(rawURL string) (*RegistryData, error) {
	resp, err := newHTTPClient().Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry fetch returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRegistryBody))
	if err != nil {
		return nil, fmt.Errorf("loading registry: %v", err)
	}
	return parseRegistry(data)
}

// Decodes/validates registry JSON.
// Enforces:
//   - at least one agent;
//   - every agent has non-empty id, name, version;
//   - no duplicate agent ids;
//   - every agent has at least one distribution.
//
// Deeper validation is deferred to Resolve, which is only for the current platform.
func parseRegistry(data []byte) (*RegistryData, error) {
	var reg RegistryData
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	if len(reg.Agents) == 0 {
		return nil, fmt.Errorf("no agents in registry")
	}
	seen := make(map[string]bool, len(reg.Agents))
	for _, a := range reg.Agents {
		if a.ID == "" {
			return nil, fmt.Errorf("agent has empty id")
		}
		if a.Name == "" {
			return nil, fmt.Errorf("agent %q has empty name", a.ID)
		}
		if a.Version == "" {
			return nil, fmt.Errorf("agent %q has empty version", a.ID)
		}
		if seen[a.ID] {
			return nil, fmt.Errorf("duplicate agent id %q", a.ID)
		}
		seen[a.ID] = true
		if len(a.Dist.Binary) == 0 && a.Dist.Npx == nil && len(a.Dist.Uvx) == 0 {
			return nil, fmt.Errorf("agent %q has no distribution", a.ID)
		}
	}
	return &reg, nil
}
