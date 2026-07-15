// Command agentctl generates a cloud-init user-data YAML file for an ACP-enabled
// agentic VM. It resolves the requested agent's distribution from the ACP
// registry (binary preferred, npx fallback), then composes a cloud-config from
// composable steps.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/benjamin-james/agentctl/internal/agent"
	"github.com/benjamin-james/agentctl/internal/cloudinit"
	"github.com/benjamin-james/agentctl/internal/dist"
)

// CLI holds cmdline flags. Unexported fields are filled by Validate.
type CLI struct {
	Agent         string   `required:"" short:"a" help:"Agent type"`
	ConfigFile    string   `name:"config" short:"c" help:"Path to config file"`
	Output        string   `required:"" short:"o" help:"Output cloud-init YAML file path (parent directory must be writable)"`
	RegistryURL   string   `default:"https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json" short:"r" help:"URL for ACP registry"`
	User          string   `default:"agent" short:"u" help:"Username of VM"`
	SecretsFile   string   `name:"secrets" short:"s" help:"Secrets file for the agent, like auth.json in Codex"`
	SSHKey        []string `short:"k" help:"Path(s) to SSH public key files"`
	SSHKeyString  []string `help:"Raw SSH public key string(s)"`
	ExtraPackages []string `short:"p" help:"Comma-separated extra packages to install"`
	ShareData     bool     `short:"d" help:"Whether to mount a 9p share as /data"`

	sshKeys        []string
	requestedAgent agent.Agent
	config         string
	secrets        string
}

// we love getters don't we
func (c *CLI) SSHKeys() []string           { return c.sshKeys }
func (c *CLI) Config() string              { return c.config }
func (c *CLI) Secrets() string             { return c.secrets }
func (c *CLI) RequestedAgent() agent.Agent { return c.requestedAgent }

func (c *CLI) resolveSSHKeys() error {
	var keys []string
	for _, p := range c.SSHKey {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("failed to read SSH key file %q: %v", p, err)
		}
		keys = append(keys, strings.TrimSpace(string(data)))
	}
	keys = append(keys, c.SSHKeyString...)
	if len(keys) == 0 {
		return fmt.Errorf("at least one SSH key is required: use --ssh-key or --ssh-key-string")
	}
	c.sshKeys = keys
	return nil
}

// Populates unexported, resolved fields.
// Collects all errors via Join rather than failing on first.
func (c *CLI) Validate() error {
	var errs []error

	if err := dist.CheckUsername(c.User); err != nil {
		errs = append(errs, err)
	}
	var agentOK bool
	if c.Agent == "" {
		errs = append(errs, fmt.Errorf("-a/--agent is a required argument"))
	} else if c.requestedAgent, agentOK = agent.GetAgent(c.Agent); !agentOK {
		errs = append(errs, fmt.Errorf("agent '%s' is not supported", c.Agent))
	}

	if err := c.resolveSSHKeys(); err != nil {
		errs = append(errs, err)
	}
	if c.ConfigFile != "" {
		content, err := os.ReadFile(c.ConfigFile)
		if err != nil {
			errs = append(errs, fmt.Errorf("config file '%s' is not readable or does not exist: %w", c.ConfigFile, err))
		} else {
			c.config = string(content)
		}
	}

	if c.SecretsFile != "" && agentOK && c.requestedAgent.AcpSecrets == "" {
		errs = append(errs, fmt.Errorf("secrets file is not accepted for agent '%s'", c.Agent))
	}

	if c.SecretsFile != "" {
		content, err := os.ReadFile(c.SecretsFile)
		if err != nil {
			errs = append(errs, fmt.Errorf("secrets file '%s' is not readable or does not exist: %w", c.SecretsFile, err))
		} else {
			c.secrets = string(content)
		}
	}
	if agentOK {
		if c.SecretsFile == "" && c.requestedAgent.AcpSecretsRequired {
			errs = append(errs, fmt.Errorf("-s/--secrets is required for agent '%s'", c.Agent))
		}
		if c.ConfigFile == "" && c.requestedAgent.AcpConfigRequired {
			errs = append(errs, fmt.Errorf("-c/--config is required for agent '%s'", c.Agent))
		}
	}
	if c.Output == "" {
		errs = append(errs, fmt.Errorf("-o/--output is a required argument"))
	}
	outDir := filepath.Dir(c.Output)
	f, err := os.CreateTemp(outDir, ".writability-check-")
	if err != nil {
		errs = append(errs, fmt.Errorf("output parent directory '%s' is not writable or does not exist: %w", outDir, err))
	} else {
		name := f.Name()
		if err := f.Close(); err != nil {
			_ = os.Remove(name)
			errs = append(errs, fmt.Errorf("failed to close temp file %q: %w", name, err))
		} else if err := os.Remove(name); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove temp file %q: %w", name, err))
		}
	}
	if err := dist.ValidateURL(c.RegistryURL); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func main() {
	cli := new(CLI)
	// kong.Parse auto-invokes cli.Validate() (kong's Validator hook) after
	// decoding flags; on validation error it prints usage and exits.
	kong.Parse(cli)
	rd, err := dist.GetRegistry(cli.RegistryURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not get registry: %v\n", err)
		os.Exit(1)
	}
	d, err := dist.Resolve(rd, cli.requestedAgent.AcpID, dist.CurrentPlatform(), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "distribution doesn't exist for agent %s: %v\n", cli.Agent, err)
		os.Exit(1)
	}
	cc, err := cloudinit.Build(cloudinit.Options{
		User:           cli.User,
		Agent:          cli.requestedAgent,
		Dist:           d,
		AuthorizedKeys: cli.SSHKeys(),
		ExtraPackages:  cli.ExtraPackages,
		ConfigData:     cli.Config(),
		SecretsData:    cli.Secrets(),
		ShareData:      cli.ShareData,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	userData, err := cloudinit.Marshal(cc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not marshal cloud config: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(cli.Output, []byte(userData), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "could not write cloud-init user-data to disk: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Successfully wrote to %s\n", cli.Output)
}
