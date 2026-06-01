package cloudinit

import (
	"slices"
	"strings"
	"testing"

	"github.com/benjamin-james/agentctl/internal/agent"
	"github.com/benjamin-james/agentctl/internal/registry"

	"go.yaml.in/yaml/v4"
)

func testResolvedAgent(user string, authorizedKeys []string, extraPackages []string, configData string, secretsData string, shareData bool) CloudConfigOpts {
	return CloudConfigOpts{
		User: user,
		Agent: registry.ResolvedAgent{
			Agent: agent.Agent{
				AcpID:              "codex-acp",
				AcpConfig:          "$HOME/.codex/config.toml",
				AcpSecrets:         "$HOME/.codex/auth.json",
				AcpConfigRequired:  false,
				AcpSecretsRequired: true,
			},
			Binary: registry.RegistryPlatformBinary{
				Archive: "https://example.com/codex.tar.gz",
				Cmd:     "codex",
				Args:    []string{"--serve"},
			},
		},
		AuthorizedKeys: authorizedKeys,
		ExtraPackages:  extraPackages,
		ConfigData:     configData,
		SecretsData:    secretsData,
		ShareData:      shareData,
	}
}

func TestBuildCloudConfigBasic(t *testing.T) {
	cfg := testResolvedAgent("agent", []string{"ssh-rsa AAA key"},
		nil, "", "", false)
	cc, err := BuildCloudConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cc.PackageUpdate {
		t.Error("PackageUpdate should be true")
	}
	if len(cc.Users) != 1 {
		t.Fatalf("Users len = %d, want 1", len(cc.Users))
	}
	if cc.Users[0].Name != "agent" {
		t.Errorf("User Name = %q, want %q", cc.Users[0].Name, "agent")
	}
	if cc.Users[0].Shell != "/bin/bash" {
		t.Errorf("User Shell = %q, want /bin/bash", cc.Users[0].Shell)
	}
	if len(cc.Users[0].AuthorizedKeys) != 1 {
		t.Errorf("AuthorizedKeys len = %d, want 1", len(cc.Users[0].AuthorizedKeys))
	}
	if !cc.Users[0].LockPasswd {
		t.Error("LockPasswd should be true when keys are provided")
	}
}

func TestBuildCloudConfigWithConfig(t *testing.T) {
	cfg := testResolvedAgent("agent", []string{"ssh-rsa AAA key"}, nil,
		"config-data", "", false)
	cc, err := BuildCloudConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, wf := range cc.WriteFiles {
		if wf.Path == "/dev/shm/acp-config" {
			found = true
			if wf.Content != "config-data" {
				t.Errorf("config content = %q, want %q", wf.Content, "config-data")
			}
		}
	}
	if !found {
		t.Error("expected /dev/shm/acp-config write file entry")
	}
	hasConfigInstall := false
	for _, cmd := range cc.RunCmd {
		if strings.Contains(cmd, "acp-config") && strings.Contains(cmd, "install") {
			hasConfigInstall = true
		}
	}
	if !hasConfigInstall {
		t.Error("expected runcmd to install acp-config")
	}
}

func TestBuildCloudConfigWithSecrets(t *testing.T) {
	cfg := testResolvedAgent("agent", []string{"ssh-rsa AAA key"}, nil, "", "secret-data", false)
	cc, err := BuildCloudConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, wf := range cc.WriteFiles {
		if wf.Path == "/dev/shm/acp-secrets" {
			found = true
			if wf.Permissions != "0600" {
				t.Errorf("secrets permissions = %q, want %q", wf.Permissions, "0600")
			}
		}
	}
	if !found {
		t.Error("expected /dev/shm/acp-secrets write file entry")
	}
}

func TestBuildCloudConfigWithShareData(t *testing.T) {
	cfg := testResolvedAgent("agent", []string{"ssh-rsa AAA key"}, nil, "", "", true)
	cc, err := BuildCloudConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cc.Mounts) != 1 {
		t.Fatalf("Mounts len = %d, want 1", len(cc.Mounts))
	}
	if cc.Mounts[0][1] != "/data/" {
		t.Errorf("mount target = %q, want /data/", cc.Mounts[0][1])
	}
	hasDaemonreload := slices.Contains(cc.RunCmd, "systemctl daemon-reload")
	hasAutomount := slices.Contains(cc.RunCmd, "systemctl enable --now data.automount")
	if !hasDaemonreload {
		t.Error("Must reload systemctl to discover automount if sharing 9p data")
	}
	if !hasAutomount {
		t.Error("Must automount if sharing 9p data")
	}
	has9pModule := false
	for _, wf := range cc.WriteFiles {
		if wf.Path == "/etc/modules-load.d/9p.conf" {
			has9pModule = true
		}
	}
	if !has9pModule {
		t.Error("expected 9p modules config file when shareData is true")
	}
}

func TestBuildCloudConfigNoShareData(t *testing.T) {
	cfg := testResolvedAgent("agent", []string{"ssh-rsa AAA key"}, nil, "", "", false)
	cc, err := BuildCloudConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cc.Mounts) != 0 {
		t.Errorf("Mounts len = %d, want 0", len(cc.Mounts))
	}
	hasAutomount := slices.Contains(cc.RunCmd, "systemctl enable --now data.automount")
	if hasAutomount {
		t.Error("Cannot automount if not sharing 9p data")
	}
	for _, wf := range cc.WriteFiles {
		if wf.Path == "/etc/modules-load.d/9p.conf" {
			t.Error("Shouldn't have 9p modules installed if not sharing data")
		}
	}
}

func TestBuildCloudConfigNoKeysUnlocksPasswd(t *testing.T) {
	cfg := testResolvedAgent("agent", nil, nil, "", "", false)
	cc, err := BuildCloudConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cc.Users[0].LockPasswd {
		t.Error("LockPasswd should be false when no keys provided")
	}
}

func TestGetInstallerTarGz(t *testing.T) {
	bin := registry.RegistryPlatformBinary{
		Archive: "https://example.com/codex.tar.gz",
		Cmd:     "codex",
	}
	script, err := GetInstaller(bin, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "#!/usr/bin/env bash") {
		t.Error("installer should start with bash shebang")
	}
	if !strings.Contains(script, "tar -xzf") {
		t.Error("installer should use tar -xzf for .tar.gz")
	}
	if !strings.Contains(script, "wget") {
		t.Error("installer should use wget")
	}
	if !strings.Contains(script, "codex.tar.gz") {
		t.Error("installer should reference archive filename")
	}
}

func TestGetInstallerTarBz2(t *testing.T) {
	bin := registry.RegistryPlatformBinary{
		Archive: "https://example.com/codex.tar.bz2",
		Cmd:     "codex",
	}
	script, err := GetInstaller(bin, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "tar -xjf") {
		t.Error("installer should use tar -xjf for .tar.bz2")
	}
}

func TestGetInstallerInvalidFormat(t *testing.T) {
	bin := registry.RegistryPlatformBinary{
		Archive: "https://example.com/codex.zip",
		Cmd:     "codex",
	}
	_, err := GetInstaller(bin, "")
	if err == nil {
		t.Fatal("expected error for invalid archive format")
	}
}

func TestGetInstallerEmptyCmd(t *testing.T) {
	bin := registry.RegistryPlatformBinary{
		Archive: "https://example.com/codex.tar.gz",
		Cmd:     "",
	}
	_, err := GetInstaller(bin, "")
	if err == nil {
		t.Fatal("expected error for empty Cmd")
	}
}

func TestGetInstallerWithSHA256(t *testing.T) {
	bin := registry.RegistryPlatformBinary{
		Archive: "https://example.com/codex.tar.gz",
		Cmd:     "codex",
	}
	sha256 := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	script, err := GetInstaller(bin, sha256)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "sha256sum -c -") {
		t.Error("installer should include sha256sum check when SHA256 is provided")
	}
	if !strings.Contains(script, sha256) {
		t.Error("installer should include the SHA256 hash in the check")
	}
}

func TestGetInstallerWithoutSHA256(t *testing.T) {
	bin := registry.RegistryPlatformBinary{
		Archive: "https://example.com/codex.tar.gz",
		Cmd:     "codex",
	}
	script, err := GetInstaller(bin, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(script, "sha256sum") {
		t.Error("installer should not include sha256sum check when SHA256 is empty")
	}
}

func TestGetAcpRun(t *testing.T) {
	bin := registry.RegistryPlatformBinary{
		Cmd:  "/usr/local/bin/codex",
		Args: []string{"--serve", "--port", "8080"},
	}
	script := GetAcpRun(bin)
	if !strings.Contains(script, "#!/usr/bin/env bash") {
		t.Error("acp-run should start with bash shebang")
	}
	if !strings.Contains(script, "codex --serve --port 8080") {
		t.Errorf("acp-run should contain command with args, got: %s", script)
	}
	if !strings.Contains(script, "ACP_WORKDIR") {
		t.Error("acp-run should reference ACP_WORKDIR")
	}
}

func TestGetAcpRunNoArgs(t *testing.T) {
	bin := registry.RegistryPlatformBinary{
		Cmd:  "myagent",
		Args: nil,
	}
	script := GetAcpRun(bin)
	if !strings.Contains(script, "myagent") {
		t.Error("acp-run should contain command name")
	}
}

func TestMarshalCloudConfig(t *testing.T) {
	cc := CloudConfig{
		PackageUpdate: true,
		Packages:      []string{"wget"},
		Users: []CloudUser{
			{
				Name:   "agent",
				Shell:  "/bin/bash",
				Groups: []string{"sudo"},
			},
		},
	}
	out, err := MarshalCloudConfig(cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(out, "#cloud-config\n") {
		t.Error("output should start with #cloud-config header")
	}
	var parsed CloudConfig
	if err := yaml.Unmarshal([]byte(out[len("#cloud-config\n"):]), &parsed); err != nil {
		t.Fatalf("output should be valid YAML: %v", err)
	}
	if !parsed.PackageUpdate {
		t.Error("parsed PackageUpdate should be true")
	}
	if len(parsed.Packages) != 1 || parsed.Packages[0] != "wget" {
		t.Errorf("parsed Packages = %v", parsed.Packages)
	}
}

func TestBuildCloudConfigWithConfigAndSecrets(t *testing.T) {
	cfg := testResolvedAgent("myuser", []string{"ssh-rsa AAA key"}, nil, "cfgdata", "secdata", false)
	cc, err := BuildCloudConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hasCfg := false
	hasSec := false
	for _, wf := range cc.WriteFiles {
		if wf.Path == "/dev/shm/acp-config" {
			hasCfg = true
		}
		if wf.Path == "/dev/shm/acp-secrets" {
			hasSec = true
		}
	}
	if !hasCfg {
		t.Error("expected config write file")
	}
	if !hasSec {
		t.Error("expected secrets write file")
	}
	for _, cmd := range cc.RunCmd {
		if strings.Contains(cmd, "install") && strings.Contains(cmd, "acp-config") {
			if !strings.Contains(cmd, "/home/myuser") {
				t.Errorf("config install should use /home/myuser, got: %s", cmd)
			}
		}
	}
}
