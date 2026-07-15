// Stepwise building of cloud-init user-data #cloud-config YAML
// Each Step mutates a CloudConfig in isolation
//
// Depends on internal/agent (config/secrets path metadata) and internal/dist (Distribution for installer/runner scripts)
// No distribution resolutoin or shell-script generation: those are in Distribution
package cloudinit

import (
	"fmt"

	"github.com/benjamin-james/agentctl/internal/agent"
	"github.com/benjamin-james/agentctl/internal/dist"
	"go.yaml.in/yaml/v4"
)

type CloudConfig struct {
	PackageUpdate bool        `yaml:"package_update"`
	Packages      []string    `yaml:"packages"`
	Users         []User      `yaml:"users"`
	WriteFiles    []WriteFile `yaml:"write_files"`
	RunCmd        []string    `yaml:"runcmd"`
	Mounts        [][]string  `yaml:"mounts,omitempty"`
}

// User is a cloud-init user entry.
type User struct {
	Name           string   `yaml:"name"`
	Shell          string   `yaml:"shell"`
	HomeDir        string   `yaml:"homedir"`
	NoCreateHome   bool     `yaml:"no_create_home"`
	Groups         []string `yaml:"groups"`
	Sudo           []string `yaml:"sudo"`
	AuthorizedKeys []string `yaml:"ssh_authorized_keys,omitempty"`
	LockPasswd     bool     `yaml:"lock_passwd"`
}

type WriteFile struct {
	Path        string `yaml:"path"`
	Permissions string `yaml:"permissions"`
	Owner       string `yaml:"owner"`
	Content     string `yaml:"content"`
}

// Instead of having each arg passed to Build, have a type
// encapsulating all arguments
type Options struct {
	User           string
	Agent          agent.Agent
	Dist           dist.Distribution
	AuthorizedKeys []string
	ExtraPackages  []string
	ConfigData     string
	SecretsData    string
	ShareData      bool
}

// Simple composable type. Modifies the pointer and ensures testability
// by assigning to a type.
type Step func(*CloudConfig) error

// Builds each step incrementally by applying steps in the correct order
// (1) Base packages
// (2) User
// (3) ACP
// (4) DataDir (optional)
// (5) StageConfig: config file
// (6) StageSecrets: secrets file
// This makes the runcmd and files explicit
func Build(opts Options) (*CloudConfig, error) {
	if err := dist.CheckUsername(opts.User); err != nil {
		return nil, err
	}
	if opts.Dist == nil {
		return nil, fmt.Errorf("Build: nil distribution")
	}
	ci := &CloudConfig{PackageUpdate: true}
	steps := []Step{
		BasePackages(opts.ExtraPackages, opts.Dist),
		UserStep(opts.User, opts.AuthorizedKeys),
		ACPStep(opts.Dist),
		DataDirStep(opts.User, opts.ShareData),
	}
	if opts.ConfigData != "" && opts.Agent.AcpConfig != "" {
		steps = append(steps, StageConfig(opts.Agent.AcpConfig, opts.ConfigData, opts.User))
	}
	if opts.SecretsData != "" && opts.Agent.AcpSecrets != "" {
		steps = append(steps, StageSecrets(opts.Agent.AcpSecrets, opts.SecretsData, opts.User))
	}
	for _, s := range steps {
		if err := s(ci); err != nil {
			return nil, err
		}
	}
	return ci, nil
}

func Marshal(cc *CloudConfig) (string, error) {
	out, err := yaml.Marshal(cc)
	if err != nil {
		return "", fmt.Errorf("marshaling cloud config: %v", err)
	}
	return "#cloud-config\n" + string(out), nil
}
