package dist

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const httpTimeout = 15 * time.Second

const maxRedirects = 3

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
