package cloudinit

import (
	"strings"
	"testing"

	"github.com/benjamin-james/agentctl/internal/agent"
	"github.com/benjamin-james/agentctl/internal/dist"
)

// stubDist is unavailable as Distribution iface is closed.
//
//	So the closed-sum type is working as intended, and this woudl be the real ACPSteps
func binStub() dist.Distribution {
	return &dist.Binary{
		Archive: "https://example.com/codex.tar.gz",
		Cmd:     "codex",
		Args:    []string{"--acp"},
		Env:     map[string]string{"FOO": "bar"},
	}
}

func npxStub() dist.Distribution {
	return &dist.Npx{
		Package: "@scope/codex@1.0",
		Args:    []string{"--acp"},
		Env:     map[string]string{"FOO": "bar"},
	}
}

func TestBasePackages(t *testing.T) {
	ci := &CloudConfig{}
	// npxStub contributes nodejs/npm as dist extras.
	if err := BasePackages([]string{"vim"}, npxStub())(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantContains := []string{"vim", "qemu-guest-agent", "ca-certificates", "rsync", "nodejs", "npm"}
	for _, w := range wantContains {
		if !contains(ci.Packages, w) {
			t.Errorf("packages missing %q, got %v", w, ci.Packages)
		}
	}
	// extras must precede base, base must precede dist extras.
	if !before(ci.Packages, "vim", "qemu-guest-agent") {
		t.Errorf("extras should precede base: %v", ci.Packages)
	}
	if !before(ci.Packages, "rsync", "nodejs") {
		t.Errorf("base should precede dist extras: %v", ci.Packages)
	}
}

func TestBasePackagesBinaryHasNoExtraPackages(t *testing.T) {
	ci := &CloudConfig{}
	if err := BasePackages(nil, binStub())(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contains(ci.Packages, "nodejs") || contains(ci.Packages, "npm") {
		t.Errorf("binary dist should not add nodejs/npm: %v", ci.Packages)
	}
}

func TestBasePackagesNpxAddsNodePackages(t *testing.T) {
	ci := &CloudConfig{}
	if err := BasePackages(nil, npxStub())(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(ci.Packages, "nodejs") || !contains(ci.Packages, "npm") {
		t.Errorf("npx dist should add nodejs/npm: %v", ci.Packages)
	}
}

func TestUserStep(t *testing.T) {
	ci := &CloudConfig{}
	keys := []string{"ssh-rsa AAA key"}
	if err := UserStep("agent", keys)(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ci.Users) != 1 {
		t.Fatalf("Users len = %d", len(ci.Users))
	}
	u := ci.Users[0]
	if u.Name != "agent" || u.Shell != "/bin/bash" || u.HomeDir != "/home/agent" {
		t.Errorf("user = %+v", u)
	}
	if !contains(u.Groups, "sudo") {
		t.Errorf("Groups = %v, want sudo", u.Groups)
	}
	if !contains(u.Sudo, "ALL=(ALL) NOPASSWD:ALL") {
		t.Errorf("Sudo = %v", u.Sudo)
	}
	if len(u.AuthorizedKeys) != 1 || u.AuthorizedKeys[0] != keys[0] {
		t.Errorf("AuthorizedKeys = %v", u.AuthorizedKeys)
	}
	if !u.LockPasswd {
		t.Error("LockPasswd should be true when keys supplied")
	}
}

func TestUserStepNoKeysUnlocksPasswd(t *testing.T) {
	ci := &CloudConfig{}
	if err := UserStep("agent", nil)(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ci.Users[0].LockPasswd {
		t.Error("LockPasswd should be false when no keys supplied")
	}
}

func TestACPStepWritesInstallerAndRunner(t *testing.T) {
	ci := &CloudConfig{}
	d := binStub()
	wantInstaller, _ := d.Installer()
	wantRunner, _ := d.Runner()
	if err := ACPStep(d)(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	installer := findWriteFile(ci, "/usr/local/sbin/install-acp")
	if installer == nil {
		t.Fatal("missing install-acp write file")
	}
	if installer.Content != wantInstaller {
		t.Errorf("installer content mismatch: got %q want %q", installer.Content, wantInstaller)
	}
	if installer.Permissions != "0755" || installer.Owner != "root:root" {
		t.Errorf("installer perms/owner = %q/%q", installer.Permissions, installer.Owner)
	}
	runner := findWriteFile(ci, "/usr/local/bin/acp-run")
	if runner == nil {
		t.Fatal("missing acp-run write file")
	}
	if runner.Content != wantRunner {
		t.Errorf("runner content mismatch: got %q want %q", runner.Content, wantRunner)
	}
	if !contains(ci.RunCmd, "/usr/local/sbin/install-acp") {
		t.Errorf("runcmd should include install-acp: %v", ci.RunCmd)
	}
}

func TestACPStepRealBinary(t *testing.T) {
	ci := &CloudConfig{}
	if err := ACPStep(binStub())(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	installer := findWriteFile(ci, "/usr/local/sbin/install-acp").Content
	if !strings.Contains(installer, "wget") || !strings.Contains(installer, "find") {
		t.Errorf("binary installer should wget+find:\n%s", installer)
	}
	runner := findWriteFile(ci, "/usr/local/bin/acp-run").Content
	if !strings.Contains(runner, "exec codex '--acp'") {
		t.Errorf("binary runner should exec codex:\n%s", runner)
	}
}

func TestACPStepRealNpx(t *testing.T) {
	ci := &CloudConfig{}
	if err := ACPStep(npxStub())(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	installer := findWriteFile(ci, "/usr/local/sbin/install-acp").Content
	if !strings.Contains(installer, "npm install -g @scope/codex@1.0") {
		t.Errorf("npx installer should npm install -g:\n%s", installer)
	}
	if strings.Contains(installer, "wget") {
		t.Errorf("npx installer must not wget:\n%s", installer)
	}
	runner := findWriteFile(ci, "/usr/local/bin/acp-run").Content
	if !strings.Contains(runner, "exec npx '@scope/codex@1.0' '--acp'") {
		t.Errorf("npx runner should exec npx with package spec:\n%s", runner)
	}
	if !strings.Contains(runner, "export FOO='bar'") {
		t.Errorf("npx runner should export env:\n%s", runner)
	}
}

func TestACPStepInvalidDistribution(t *testing.T) {
	ci := &CloudConfig{}
	bad := &dist.Binary{Archive: "", Cmd: "x"}
	if err := ACPStep(bad)(ci); err == nil {
		t.Fatal("expected error from invalid distribution")
	}
}

func TestDataDirStepNoShare(t *testing.T) {
	ci := &CloudConfig{}
	if err := DataDirStep("agent", false)(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(ci.RunCmd, "mkdir -p /data") {
		t.Errorf("runcmd missing mkdir: %v", ci.RunCmd)
	}
	if !contains(ci.RunCmd, `chown -R "agent":"agent" /data`) {
		t.Errorf("runcmd missing chown: %v", ci.RunCmd)
	}
	if findWriteFile(ci, "/etc/modules-load.d/9p.conf") != nil {
		t.Error("9p conf should not be written when share=false")
	}
	if ci.Mounts != nil {
		t.Errorf("Mounts should be nil when share=false, got %v", ci.Mounts)
	}
}

func TestDataDirStepWithShare(t *testing.T) {
	ci := &CloudConfig{}
	if err := DataDirStep("agent", true)(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	conf := findWriteFile(ci, "/etc/modules-load.d/9p.conf")
	if conf == nil {
		t.Fatal("missing 9p.conf write file")
	}
	if conf.Content != "9p\n9pnet\n9pnet_virtio" {
		t.Errorf("9p.conf content = %q", conf.Content)
	}
	// runcmd order: mkdir, daemon-reload, enable automount, chown
	wantOrder := []string{"mkdir -p /data", "systemctl daemon-reload", "systemctl enable --now data.automount", `chown -R "agent":"agent" /data`}
	for i, w := range wantOrder {
		idx := indexOf(ci.RunCmd, w)
		if idx < 0 {
			t.Errorf("runcmd missing %q: %v", w, ci.RunCmd)
			continue
		}
		if i > 0 {
			prev := indexOf(ci.RunCmd, wantOrder[i-1])
			if idx < prev {
				t.Errorf("runcmd order wrong: %q before %q: %v", w, wantOrder[i-1], ci.RunCmd)
			}
		}
	}
	if len(ci.Mounts) != 1 || ci.Mounts[0][1] != "/data/" {
		t.Errorf("Mounts = %v", ci.Mounts)
	}
}

func TestStageConfig(t *testing.T) {
	ci := &CloudConfig{}
	if err := StageConfig("$HOME/.codex/config.toml", "CONFIGDATA", "agent")(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	staged := findWriteFile(ci, "/dev/shm/acp-config")
	if staged == nil {
		t.Fatal("missing staged config write file")
	}
	if staged.Permissions != "0644" || staged.Content != "CONFIGDATA" {
		t.Errorf("staged config = %+v", staged)
	}
	// runcmd should install into /home/agent/.codex/config.toml
	joined := strings.Join(ci.RunCmd, "\n")
	if !strings.Contains(joined, "/home/agent/.codex/config.toml") {
		t.Errorf("runcmd should install to expanded config path:\n%s", joined)
	}
	if !strings.Contains(joined, "install -m 644") {
		t.Errorf("runcmd should install with 644 perms:\n%s", joined)
	}
	// should create the .codex directory
	if !strings.Contains(joined, "install -d -m 700") {
		t.Errorf("runcmd should create parent dirs with 700:\n%s", joined)
	}
}

func TestStageSecrets(t *testing.T) {
	ci := &CloudConfig{}
	if err := StageSecrets("$HOME/.codex/auth.json", "SECRETS", "agent")(ci); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	staged := findWriteFile(ci, "/dev/shm/acp-secrets")
	if staged == nil {
		t.Fatal("missing staged secrets write file")
	}
	if staged.Permissions != "0600" {
		t.Errorf("secrets perms = %q, want 0600", staged.Permissions)
	}
	joined := strings.Join(ci.RunCmd, "\n")
	if !strings.Contains(joined, "install -m 600") {
		t.Errorf("runcmd should install secrets with 600 perms:\n%s", joined)
	}
}

func TestInstallToDirOrder(t *testing.T) {
	cmds := InstallToDir("$HOME/.codex/config.toml", "/dev/shm/acp-config", "agent", "agent", 0700, 0644)
	// Expected order: create /home/agent/.codex (700), install file (644), rm staging.
	if len(cmds) != 3 {
		t.Fatalf("cmds len = %d, want 3: %v", len(cmds), cmds)
	}
	if !strings.Contains(cmds[0], "install -d -m 700") || !strings.Contains(cmds[0], "/home/agent/.codex") {
		t.Errorf("first cmd should mkdir .codex: %q", cmds[0])
	}
	if !strings.Contains(cmds[1], "install -m 644") {
		t.Errorf("second cmd should install file: %q", cmds[1])
	}
	if !strings.HasPrefix(cmds[2], "rm -f") {
		t.Errorf("third cmd should rm staging: %q", cmds[2])
	}
}

func TestInstallToDirDeepNesting(t *testing.T) {
	cmds := InstallToDir("$HOME/a/b/c/file", "/dev/shm/x", "agent", "agent", 0700, 0644)
	// Should create /home/agent/a, /home/agent/a/b, /home/agent/a/b/c (3 mkdirs),
	// then install, then rm = 5 cmds.
	if len(cmds) != 5 {
		t.Fatalf("cmds len = %d, want 5: %v", len(cmds), cmds)
	}
}

func TestBuildBinaryIntegration(t *testing.T) {
	opts := Options{
		User:           "agent",
		Agent:          agent.Agent{AcpID: "codex-acp", AcpConfig: "$HOME/.codex/config.toml", AcpSecrets: "$HOME/.codex/auth.json", AcpSecretsRequired: true},
		Dist:           binStub(),
		AuthorizedKeys: []string{"ssh-rsa AAA key"},
		SecretsData:    "AUTHJSON",
	}
	cc, err := Build(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cc.PackageUpdate {
		t.Error("PackageUpdate should be true")
	}
	if !contains(cc.Packages, "rsync") {
		t.Errorf("packages missing rsync: %v", cc.Packages)
	}
	if contains(cc.Packages, "nodejs") {
		t.Errorf("binary build should not add nodejs: %v", cc.Packages)
	}
	if len(cc.Users) != 1 || cc.Users[0].Name != "agent" {
		t.Errorf("Users = %+v", cc.Users)
	}
	if findWriteFile(cc, "/usr/local/sbin/install-acp") == nil || findWriteFile(cc, "/usr/local/bin/acp-run") == nil {
		t.Error("missing install-acp or acp-run write files")
	}
	// runcmd order: install-acp, mkdir, chown, secrets-install
	if indexOf(cc.RunCmd, "/usr/local/sbin/install-acp") != 0 {
		t.Errorf("install-acp should be first runcmd: %v", cc.RunCmd)
	}
	if !before(cc.RunCmd, "mkdir -p /data", `chown -R "agent":"agent" /data`) {
		t.Errorf("mkdir should precede chown: %v", cc.RunCmd)
	}
	if findWriteFile(cc, "/dev/shm/acp-secrets") == nil {
		t.Error("missing staged secrets")
	}
}

func TestBuildNpxIntegration(t *testing.T) {
	opts := Options{
		User:           "agent",
		Agent:          agent.Agent{AcpID: "codex-acp", AcpConfig: "$HOME/.codex/config.toml"},
		Dist:           npxStub(),
		AuthorizedKeys: []string{"ssh-rsa AAA key"},
		ConfigData:     "CONFIGTOML",
		ShareData:      true,
	}
	cc, err := Build(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(cc.Packages, "nodejs") || !contains(cc.Packages, "npm") {
		t.Errorf("npx build should add nodejs/npm: %v", cc.Packages)
	}
	if findWriteFile(cc, "/etc/modules-load.d/9p.conf") == nil {
		t.Error("share build should write 9p.conf")
	}
	if len(cc.Mounts) != 1 {
		t.Errorf("Mounts = %v", cc.Mounts)
	}
	if findWriteFile(cc, "/dev/shm/acp-config") == nil {
		t.Error("missing staged config")
	}
}

func TestBuildInvalidUsername(t *testing.T) {
	opts := Options{User: "1bad", Dist: binStub()}
	if _, err := Build(opts); err == nil {
		t.Fatal("expected error for invalid username")
	}
}

func TestBuildNilDistribution(t *testing.T) {
	opts := Options{User: "agent"}
	if _, err := Build(opts); err == nil {
		t.Fatal("expected error for nil distribution")
	}
}

func TestBuildNoConfigNoSecrets(t *testing.T) {
	opts := Options{
		User:           "agent",
		Agent:          agent.Agent{AcpID: "x"},
		Dist:           binStub(),
		AuthorizedKeys: []string{"ssh-rsa AAA key"},
	}
	cc, err := Build(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findWriteFile(cc, "/dev/shm/acp-config") != nil || findWriteFile(cc, "/dev/shm/acp-secrets") != nil {
		t.Error("no staging files should be written when no config/secrets supplied")
	}
}

func TestMarshal(t *testing.T) {
	cc := &CloudConfig{
		PackageUpdate: true,
		Packages:      []string{"rsync"},
		Users:         []User{{Name: "agent", Shell: "/bin/bash"}},
	}
	out, err := Marshal(cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(out, "#cloud-config\n") {
		t.Errorf("output must start with #cloud-config magic, got: %q", out[:min(20, len(out))])
	}
	if !strings.Contains(out, "package_update: true") {
		t.Errorf("output missing package_update: %s", out)
	}
	if !strings.Contains(out, "rsync") {
		t.Errorf("output missing rsync: %s", out)
	}
}

func contains(slice []string, s string) bool {
	return indexOf(slice, s) >= 0
}

func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}

func before(slice []string, a, b string) bool {
	return indexOf(slice, a) >= 0 && indexOf(slice, b) >= 0 && indexOf(slice, a) < indexOf(slice, b)
}

func findWriteFile(ci *CloudConfig, path string) *WriteFile {
	for i := range ci.WriteFiles {
		if ci.WriteFiles[i].Path == path {
			return &ci.WriteFiles[i]
		}
	}
	return nil
}
