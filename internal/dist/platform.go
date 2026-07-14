package dist

import (
	"fmt"
	"runtime"
)

type Platform struct {
	OS   string
	Arch string
}

func CurrentPlatform() Platform {
	return Platform{OS: "linux", Arch: translateArch(runtime.GOARCH)}
}

func (p Platform) Key() string {
	return p.OS + "-" + p.Arch
}

func (p Platform) String() string { return p.Key() }

func translateArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return goarch
	}
}

func ParsePlatform(key string) (Platform, error) {
	for i := 0; i < len(key); i++ {
		if key[i] == '-' {
			return Platform{OS: key[:i], Arch: key[i+1:]}, nil
		}
	}
	return Platform{}, fmt.Errorf("invalid platform key %q: expected os-arch", key)
}
