package dist

import (
	"fmt"
	"runtime"
)

// Platform is the target VM platform a distribution must match. The OS is
// always "linux" today (cloud-init targets a Linux guest), but the field is
// retained so the type is self-documenting and extensible.
type Platform struct {
	OS   string
	Arch string
}

// CurrentPlatform returns the platform the agentctl binary is running on,
// translating Go's GOARCH names to the GNU-style arch names used as registry
// keys. The OS is hardcoded to "linux" because the generated cloud-init always
// targets a Linux guest VM (the original getdist() carried this same
// assumption explicitly).
func CurrentPlatform() Platform {
	return Platform{OS: "linux", Arch: translateArch(runtime.GOARCH)}
}

// Key returns the registry distribution-map key for this platform, e.g.
// "linux-x86_64". This is the abbreviated form (not a full target triple
// like x86_64-unknown-linux-gnu); the registry JSON uses this abbreviated key.
func (p Platform) Key() string {
	return p.OS + "-" + p.Arch
}

// String renders the platform for error messages.
func (p Platform) String() string { return p.Key() }

func translateArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		// Unknown arches pass through unchanged. Resolution will then fail
		// to find a matching binary and fall back to npx (or error), which is
		// the correct behavior rather than silently mistranslating.
		return goarch
	}
}

// ParsePlatform parses a "os-arch" key back into a Platform. It is the inverse
// of Key and is used by tests to construct platforms without depending on the
// host GOARCH.
func ParsePlatform(key string) (Platform, error) {
	for i := 0; i < len(key); i++ {
		if key[i] == '-' {
			return Platform{OS: key[:i], Arch: key[i+1:]}, nil
		}
	}
	return Platform{}, fmt.Errorf("invalid platform key %q: expected os-arch", key)
}
