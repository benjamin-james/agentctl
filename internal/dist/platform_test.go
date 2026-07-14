package dist

import (
	"runtime"
	"testing"
)

func TestPlatformKey(t *testing.T) {
	cases := []struct {
		p    Platform
		want string
	}{
		{Platform{OS: "linux", Arch: "x86_64"}, "linux-x86_64"},
		{Platform{OS: "linux", Arch: "aarch64"}, "linux-aarch64"},
	}
	for _, c := range cases {
		if got := c.p.Key(); got != c.want {
			t.Errorf("Key() = %q, want %q", got, c.want)
		}
	}
}

func TestParsePlatform(t *testing.T) {
	p, err := ParsePlatform("linux-x86_64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.OS != "linux" || p.Arch != "x86_64" {
		t.Errorf("ParsePlatform = %+v, want linux/x86_64", p)
	}
	if _, err := ParsePlatform("bogus"); err == nil {
		t.Fatal("expected error for platform key without hyphen")
	}
}

func TestParsePlatformInverse(t *testing.T) {
	for _, key := range []string{"linux-x86_64", "linux-aarch64", "linux-386"} {
		p, err := ParsePlatform(key)
		if err != nil {
			t.Fatalf("ParsePlatform(%q): %v", key, err)
		}
		if got := p.Key(); got != key {
			t.Errorf("round-trip: Key() = %q, want %q", got, key)
		}
	}
}

func TestCurrentPlatformArch(t *testing.T) {
	p := CurrentPlatform()
	if p.OS != "linux" {
		t.Errorf("OS = %q, want linux", p.OS)
	}
	want := runtime.GOARCH
	switch runtime.GOARCH {
	case "amd64":
		want = "x86_64"
	case "arm64":
		want = "aarch64"
	}
	if p.Arch != want {
		t.Errorf("Arch = %q, want %q", p.Arch, want)
	}
}

func TestTranslateArch(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"amd64", "x86_64"},
		{"arm64", "aarch64"},
		{"386", "386"},       // unknown arches pass through
		{"mips", "mips"},     // unknown arches pass through
		{"x86_64", "x86_64"}, // already-translated passes through
	}
	for _, c := range cases {
		if got := translateArch(c.in); got != c.want {
			t.Errorf("translateArch(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
