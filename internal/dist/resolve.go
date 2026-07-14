package dist

import (
	"errors"
	"fmt"
)

// Typed error returned by Resolve.
// To distinguish "agent unknown" from "arch unsupported"
type ResolveError struct {
	AgentID string
	Arch    string
	Kind    ResolveErrorKind
}

type ResolveErrorKind int

const (
	// ErrAgentUnknown means the agent ID is not in the registry at all.
	ErrAgentUnknown ResolveErrorKind = iota
	// ErrArchUnsupported means the agent exists, but has neither a binary for
	// the linux-$(arch) nor the npx distribution.
	ErrArchUnsupported
)

func (e *ResolveError) Error() string {
	switch e.Kind {
	case ErrAgentUnknown:
		return fmt.Sprintf("agent %s is not in the registry", e.AgentID)
	case ErrArchUnsupported:
		return fmt.Sprintf("agent %s is not supported for architecture %s", e.AgentID, e.Arch)
	default:
		return fmt.Sprintf("agent %s: unresolved", e.AgentID)
	}
}

// Resolve selects a distribution from a given agent ID on the current platform.
// Binary is preferred, npx is a fallback:
//   - if has binary, use it after checking SHA256
//   - else if npx, use it
//   - else ArchUnsupported
//
// pointer is returned so callers receive SHA populated on Binary by the fetcher,
// as Binary is a value type, preserving the mutation without forcing the caller to copy
func Resolve(reg *RegistryData, agentID string, plat Platform, fetcher IntegrityFetcher) (Distribution, error) {
	if fetcher == nil {
		// null fetcher to avoid network requirement
		fetcher = GitHubAPIFetcher{}
	}
	key := plat.Key()
	for _, a := range reg.Agents {
		if a.ID != agentID {
			continue
		}
		if bin, ok := a.Dist.Binary[key]; ok {
			sha, err := fetcher.FetchSHA256(bin.Archive)
			if err != nil {
				return nil, fmt.Errorf("fetching SHA256 for %s: %w", bin.Archive, err)
			}
			bin.SHA256 = sha
			if err := bin.Validate(); err != nil {
				return nil, fmt.Errorf("agent %q dist %q: %w", a.ID, key, err)
			}
			return &bin, nil
		}
		if a.Dist.Npx != nil {
			if err := a.Dist.Npx.Validate(); err != nil {
				return nil, fmt.Errorf("agent %q npx: %w", a.ID, err)
			}
			return a.Dist.Npx, nil
		}
		return nil, &ResolveError{AgentID: agentID, Arch: plat.Arch, Kind: ErrArchUnsupported}
	}
	return nil, &ResolveError{AgentID: agentID, Kind: ErrAgentUnknown}
}

// Deref. binary, erroring if not a binary dist.
// Convenience for callers that need it, e.g. SHA256
func AsBinary(d Distribution) (*Binary, bool) {
	b, ok := d.(*Binary)
	return b, ok
}

func AsNpx(d Distribution) (*Npx, bool) {
	n, ok := d.(*Npx)
	return n, ok
}

// Checks if error is unknown agent by type inference
func IsAgentUnknown(err error) bool {
	var re *ResolveError
	return errors.As(err, &re) && re.Kind == ErrAgentUnknown
}

// Checks if error is arch unsupported by type inference
func IsArchUnsupported(err error) bool {
	var re *ResolveError
	return errors.As(err, &re) && re.Kind == ErrArchUnsupported
}
