package dist

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// httpTimeout is the single timeout used for all outbound HTTP (registry fetch
// and GitHub digest fetch). The prior code used 15s in both places; this keeps
// that contract in one constant.
const httpTimeout = 15 * time.Second

// maxRedirects is the maximum number of redirects the shared client follows.
const maxRedirects = 3

// newHTTPClient returns the shared HTTP client used for all dist-package
// outbound requests: a 15s timeout, at most 3 redirects, and a same-host
// redirect policy (a redirect to a different host is rejected).
//
// This consolidates the two previously copy-pasted client constructors (one in
// fetchSHA256FromRelease, one in GetRegistry) whose redirect policies diverged
// only in a misspelling ("not allowd" vs "not allowed").
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("redirect to different host %q not allowed", req.URL.Host)
			}
			return nil
		},
	}
}

// ValidateURL reports whether raw is an https URL with a non-empty host. It is
// used both for the registry URL itself and for each binary archive URL during
// registry parsing.
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
