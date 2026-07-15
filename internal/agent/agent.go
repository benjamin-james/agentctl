// Catalog of supported agents mapping their ACP
// registry to config/secrets
package agent

import "sort"

type Agent struct {
	AcpID              string
	AcpConfig          string
	AcpSecrets         string
	AcpConfigRequired  bool
	AcpSecretsRequired bool
}

var supportedAgents = map[string]Agent{
	"codex": {
		AcpID:              "codex-acp",
		AcpConfig:          "$HOME/.codex/config.toml",
		AcpSecrets:         "$HOME/.codex/auth.json",
		AcpConfigRequired:  false,
		AcpSecretsRequired: true,
	},
	"opencode": {
		AcpID:              "opencode",
		AcpConfig:          "$HOME/.config/opencode/opencode.json",
		AcpSecrets:         "$HOME/.local/share/opencode/auth.json",
		AcpConfigRequired:  true,
		AcpSecretsRequired: false,
	},
	"goose": {
		AcpID:              "goose",
		AcpConfig:          "$HOME/.config/goose/config.yaml",
		AcpSecrets:         "$HOME/.config/goose/secrets.yaml",
		AcpConfigRequired:  true,
		AcpSecretsRequired: false,
	},
	"pi": {
		AcpID:              "pi-acp",
		AcpConfig:          "$HOME/.pi/agent/settings.json",
		AcpSecrets:         "$HOME/.pi/agent/auth.json",
		AcpConfigRequired:  true,
		AcpSecretsRequired: false,
	},
}

// wrapper on the supportedAgents map
func GetAgent(name string) (Agent, bool) {
	a, ok := supportedAgents[name]
	return a, ok
}

func AgentNames() []string {
	names := make([]string, 0, len(supportedAgents))
	for k := range supportedAgents {
		names = append(names, k)
	}
	// sort for deterministic ordering
	sort.Strings(names)
	return names
}
