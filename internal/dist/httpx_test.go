package dist

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://example.com/registry.json", true},
		{"https://github.com/owner/repo/releases/download/v1/x.tar.gz", true},
		{"http://example.com/x", false},
		{"ftp://example.com/x", false},
		{"example.com/x", false},
		{"https:///nohost/x", false},
		{"", false},
	}
	for _, c := range cases {
		err := ValidateURL(c.in)
		if (err == nil) != c.want {
			t.Errorf("ValidateURL(%q) err=%v, want ok=%v", c.in, err, c.want)
		}
	}
}

func TestNewHTTPClientTimeout(t *testing.T) {
	c := newHTTPClient()
	if c.Timeout != httpTimeout {
		t.Errorf("Timeout = %v, want %v", c.Timeout, httpTimeout)
	}
	if c.CheckRedirect == nil {
		t.Error("CheckRedirect must be set")
	}
}

func TestNewHTTPClientSameHostRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/final", http.StatusFound)
	}))
	defer target.Close()

	// Same-host redirect (within target) should be followed.
	c := newHTTPClient()
	resp, err := c.Get(target.URL + "/start")
	if err != nil {
		t.Fatalf("unexpected error following same-host redirect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 after same-host redirect chain", resp.StatusCode)
	}
}

func TestNewHTTPClientCrossHostRedirectRejected(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer evil.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/stolen", http.StatusFound)
	}))
	defer origin.Close()

	c := newHTTPClient()
	resp, err := c.Get(origin.URL + "/start")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected error for cross-host redirect, got nil")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("error should mention host policy, got: %v", err)
	}
}
