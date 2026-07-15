package agent

import (
	"testing"
)

func TestSupportedAgentsContainsExpectedAgents(t *testing.T) {
	want := map[string]bool{"codex": true, "opencode": true, "goose": true, "pi": true}
	for _, name := range AgentNames() {
		if !want[name] {
			t.Errorf("unexpected agent %q", name)
		}
		delete(want, name)
	}
	if len(want) > 0 {
		t.Errorf("missing agents: %v", want)
	}
}

func TestSupportedAgentsCount(t *testing.T) {
	if got := len(AgentNames()); got != 4 {
		t.Errorf("len(AgentNames()) = %d, want 4", got)
	}
}

func TestCodexAgentFields(t *testing.T) {
	a, ok := GetAgent("codex")
	if !ok {
		t.Fatal("codex not supported")
	}
	if a.AcpID != "codex-acp" {
		t.Errorf("AcpID = %q, want codex-acp", a.AcpID)
	}
	if a.AcpConfig != "$HOME/.codex/config.toml" {
		t.Errorf("AcpConfig = %q", a.AcpConfig)
	}
	if a.AcpSecrets != "$HOME/.codex/auth.json" {
		t.Errorf("AcpSecrets = %q", a.AcpSecrets)
	}
	if a.AcpConfigRequired {
		t.Error("codex config should not be required")
	}
	if !a.AcpSecretsRequired {
		t.Error("codex secrets should be required")
	}
}

func TestOpenCodeAgentFields(t *testing.T) {
	a, ok := GetAgent("opencode")
	if !ok {
		t.Fatal("opencode not supported")
	}
	if a.AcpID != "opencode" {
		t.Errorf("AcpID = %q, want opencode", a.AcpID)
	}
	if a.AcpConfig != "$HOME/.config/opencode/opencode.json" {
		t.Errorf("AcpConfig = %q", a.AcpConfig)
	}
	if a.AcpSecrets != "$HOME/.local/share/opencode/auth.json" {
		t.Errorf("AcpSecrets = %q, want $HOME/.local/share/opencode/auth.json", a.AcpSecrets)
	}
	if !a.AcpConfigRequired {
		t.Error("opencode config should be required")
	}
	if a.AcpSecretsRequired {
		t.Error("opencode secrets should not be required")
	}
}

func TestGooseAgentFields(t *testing.T) {
	a, ok := GetAgent("goose")
	if !ok {
		t.Fatal("goose not supported")
	}
	if a.AcpID != "goose" {
		t.Errorf("AcpID = %q, want goose", a.AcpID)
	}
	if a.AcpConfig != "$HOME/.config/goose/config.yaml" {
		t.Errorf("AcpConfig = %q", a.AcpConfig)
	}
	if a.AcpSecrets != "$HOME/.config/goose/secrets.yaml" {
		t.Errorf("AcpSecrets = %q", a.AcpSecrets)
	}
	if !a.AcpConfigRequired {
		t.Error("goose config should be required")
	}
	if a.AcpSecretsRequired {
		t.Error("goose secrets should not be required")
	}
}

func TestGetAgentUnsupported(t *testing.T) {
	if _, ok := GetAgent("nonexistent"); ok {
		t.Error("expected unsupported agent to return false")
	}
}

func TestAgentStructDefaults(t *testing.T) {
	var a Agent
	if a.AcpConfigRequired || a.AcpSecretsRequired {
		t.Error("zero-value Agent should have both Required flags false")
	}
}

func TestAgentNamesSorted(t *testing.T) {
	names := AgentNames()
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("AgentNames() not sorted: %v", names)
		}
	}
	// Determinism: two calls return the same slice contents.
	a := AgentNames()
	b := AgentNames()
	if len(a) != len(b) {
		t.Fatalf("AgentNames() length differs across calls: %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("AgentNames() not deterministic: %v vs %v", a, b)
		}
	}
}
