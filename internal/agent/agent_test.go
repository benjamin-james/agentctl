package agent

import "testing"

func TestSupportedAgentsContainsExpectedAgents(t *testing.T) {
	expected := []string{"codex", "opencode", "goose"}
	for _, name := range expected {
		if _, ok := GetAgent(name); !ok {
			t.Errorf("SupportedAgents missing %q", name)
		}
	}
}

func TestSupportedAgentsNoExtraAgents(t *testing.T) {
	SupportedAgents := AgentNames()
	if len(SupportedAgents) != 3 {
		t.Errorf("SupportedAgents has %d entries, want 3", len(SupportedAgents))
	}
}

func TestCodexAgentFields(t *testing.T) {
	a, ok := GetAgent("codex")
	if !ok {
		t.Errorf("codex not supported")
	}
	if a.AcpID != "codex-acp" {
		t.Errorf("codex AcpID = %q, want %q", a.AcpID, "codex-acp")
	}
	if a.AcpConfig != "$HOME/.codex/config.toml" {
		t.Errorf("codex AcpConfig = %q, want %q", a.AcpConfig, "$HOME/.codex/config.toml")
	}
	if a.AcpSecrets != "$HOME/.codex/auth.json" {
		t.Errorf("codex AcpSecrets = %q, want %q", a.AcpSecrets, "$HOME/.codex/auth.json")
	}
	if a.AcpConfigRequired {
		t.Error("codex AcpConfigRequired should be false")
	}
	if !a.AcpSecretsRequired {
		t.Error("codex AcpSecretsRequired should be true")
	}
}

func TestOpenCodeAgentFields(t *testing.T) {
	a, ok := GetAgent("opencode")
	if !ok {
		t.Errorf("opencode not supported")
	}
	if a.AcpID != "opencode" {
		t.Errorf("opencode AcpID = %q, want %q", a.AcpID, "opencode")
	}
	if a.AcpSecrets != "" {
		t.Errorf("opencode AcpSecrets = %q, want empty", a.AcpSecrets)
	}
	if !a.AcpConfigRequired {
		t.Error("opencode AcpConfigRequired should be true")
	}
	if a.AcpSecretsRequired {
		t.Error("opencode AcpSecretsRequired should be false")
	}
}

func TestGooseAgentFields(t *testing.T) {
	a, ok := GetAgent("goose")
	if !ok {
		t.Errorf("goose not supported")
	}
	if a.AcpID != "goose" {
		t.Errorf("goose AcpID = %q, want %q", a.AcpID, "goose")
	}
	if !a.AcpConfigRequired {
		t.Error("goose AcpConfigRequired should be true")
	}
	if a.AcpSecretsRequired {
		t.Error("goose AcpSecretsRequired should be false")
	}
}

func TestAgentStructDefaults(t *testing.T) {
	a := Agent{}
	if a.AcpConfigRequired {
		t.Error("default AcpConfigRequired should be false")
	}
	if a.AcpSecretsRequired {
		t.Error("default AcpSecretsRequired should be false")
	}
}
