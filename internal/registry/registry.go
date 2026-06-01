package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/benjamin-james/agentctl/internal/agent"
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

type RegistryDist struct {
	Binary map[string]RegistryPlatformBinary `json:"binary,omitempty"`
}

type RegistryPlatformBinary struct {
	Archive string            `json:"archive"`
	Cmd     string            `json:"cmd"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type ResolvedAgent struct {
	Agent  agent.Agent
	Binary RegistryPlatformBinary
	SHA256 string
}

var gitHubReleaseRe = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/releases/download/([^/]+)/`)

type gitHubRelease struct {
	Assets []gitHubAsset `json:"assets"`
}

type gitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

func fetchSHA256(archiveURL string) (string, error) {
	m := gitHubReleaseRe.FindStringSubmatch(archiveURL)
	if m == nil {
		return "", nil
	}
	owner, repo, tag := m[1], m[2], m[3]
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)
	return fetchSHA256FromRelease(apiURL, archiveURL)
}

func fetchSHA256FromRelease(apiURL, archiveURL string) (string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("redirect to different host %q not allowed", req.URL.Host)
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching GitHub release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", fmt.Errorf("reading GitHub API response: %w", err)
	}

	var release gitHubRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("parsing GitHub API response: %w", err)
	}

	for _, asset := range release.Assets {
		if asset.BrowserDownloadURL == archiveURL {
			if asset.Digest == "" {
				return "", nil
			}
			if !strings.HasPrefix(asset.Digest, "sha256:") {
				return "", fmt.Errorf("unexpected digest format %q for asset %q", asset.Digest, asset.Name)
			}
			return strings.TrimPrefix(asset.Digest, "sha256:"), nil
		}
	}

	return "", fmt.Errorf("no matching asset found for %q in GitHub release", archiveURL)
}

func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("URL %q must use https scheme", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("URL %q must have a host", raw)
	}
	return nil
}
func parseRegistry(data []byte) (*RegistryData, error) {
	var reg RegistryData
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	if len(reg.Agents) == 0 {
		return nil, fmt.Errorf("no agents in registry")
	}
	for _, a := range reg.Agents {
		for dist, bin := range a.Dist.Binary {
			if err := ValidateURL(bin.Archive); err != nil {
				return nil, fmt.Errorf("agent %q dist %q: %w", a.ID, dist, err)
			}
		}
	}
	return &reg, nil
}

func (reg *RegistryData) GetBinaryForAgent(requestedAgent agent.Agent) (ResolvedAgent, error) {
	dist := getdist()
	for _, agent := range reg.Agents {
		if agent.ID == requestedAgent.AcpID {
			if bin, ok := agent.Dist.Binary[dist]; ok {
				sha256, err := fetchSHA256(bin.Archive)
				if err != nil {
					return ResolvedAgent{}, fmt.Errorf("fetching SHA256: %w", err)
				}
				return ResolvedAgent{Agent: requestedAgent, Binary: bin, SHA256: sha256}, nil
			}
			return ResolvedAgent{}, fmt.Errorf("agent %s is not supported for architecture %s", requestedAgent.AcpID, dist)
		}
	}
	return ResolvedAgent{}, fmt.Errorf("agent %s is not supported", requestedAgent.AcpID)
}

func GetRegistry(url string) (*RegistryData, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("redirect to different host %q not allowd", req.URL.Host)
			}
			return nil
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("loading registry: %v", err)
	}
	return parseRegistry(data)
}

func getdist() string {
	//os := runtime.GOOS
	os := "linux" // always using linux client
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}
	return os + "-" + arch
}
