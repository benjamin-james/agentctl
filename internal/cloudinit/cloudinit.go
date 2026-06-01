package cloudinit

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/benjamin-james/agentctl/internal/registry"
	"go.yaml.in/yaml/v4"
)

type CloudConfig struct {
	PackageUpdate bool        `yaml:"package_update"`
	Packages      []string    `yaml:"packages"`
	Users         []CloudUser `yaml:"users"`
	WriteFiles    []WriteFile `yaml:"write_files"`
	RunCmd        []string    `yaml:"runcmd"`
	Mounts        [][]string  `yaml:"mounts,omitempty"`
}

type CloudUser struct {
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

type CloudConfigOpts struct {
	User           string
	Agent          registry.ResolvedAgent
	AuthorizedKeys []string
	ExtraPackages  []string
	ConfigData     string
	SecretsData    string
	ShareData      bool
}

var safeNameRegex = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)
var safeArgRegex = regexp.MustCompile(`^[a-zA-Z0-9._/=:,+-]+$`)

func BuildCloudConfig(opts CloudConfigOpts) (*CloudConfig, error) {
	packages := append([]string{}, opts.ExtraPackages...)
	packages = append(packages, "qemu-guest-agent", "ca-certificates", "wget", "libnss-mdns",
		"bzip2", "libcap2", "zlib1g", "libssl-dev", "libgcc-s1", "libgomp1", "libzstd1", "rsync")
	runcmd := []string{
		"/usr/local/sbin/install-acp",
		"mkdir -p /data",
	}
	if opts.ShareData {
		runcmd = append(runcmd, "systemctl daemon-reload")
		runcmd = append(runcmd, "systemctl enable --now data.automount")
	}
	runcmd = append(runcmd, "chown -R agent:agent /data")
	if err := validateBinary(opts.Agent.Binary); err != nil {
		return nil, err
	}
	installAcp, err := GetInstaller(opts.Agent.Binary, opts.Agent.SHA256)
	if err != nil {
		return nil, err
	}
	writeFiles := []WriteFile{
		{
			Path:        "/usr/local/sbin/install-acp",
			Permissions: "0755",
			Owner:       "root:root",
			Content:     installAcp,
		},
		{
			Path:        "/usr/local/bin/acp-run",
			Permissions: "0755",
			Owner:       "root:root",
			Content:     GetAcpRun(opts.Agent.Binary),
		},
	}
	if opts.ShareData {
		writeFiles = append(writeFiles, WriteFile{

			Path:        "/etc/modules-load.d/9p.conf",
			Permissions: "0644",
			Owner:       "root:root",
			Content: `9p
9pnet
9pnet_virtio`,
		})
	}
	if opts.ConfigData != "" && opts.Agent.Agent.AcpConfig != "" {
		writeFiles = append(writeFiles, WriteFile{
			Path:        "/dev/shm/acp-config",
			Permissions: "0644",
			Owner:       "root:root",
			Content:     opts.ConfigData,
		})
		cfgdest := strings.Replace(opts.Agent.Agent.AcpConfig, "$HOME",
			fmt.Sprintf("/home/%s", opts.User), 1)
		runcmd = append(runcmd, fmt.Sprintf("install -D -m 644 -o \"%s\" -g \"%s\" /dev/shm/acp-config \"%s\"",
			opts.User, opts.User, cfgdest))
		runcmd = append(runcmd, "rm -f /dev/shm/acp-config")

	}
	if opts.SecretsData != "" && opts.Agent.Agent.AcpSecrets != "" {
		secdest := strings.Replace(opts.Agent.Agent.AcpSecrets, "$HOME",
			fmt.Sprintf("/home/%s", opts.User), 1)
		writeFiles = append(writeFiles, WriteFile{
			Path:        "/dev/shm/acp-secrets",
			Permissions: "0600",
			Owner:       "root:root",
			Content:     opts.SecretsData,
		})
		runcmd = append(runcmd, fmt.Sprintf("install -D -m 600 -o \"%s\" -g \"%s\" /dev/shm/acp-secrets \"%s\"", opts.User, opts.User, secdest))
		runcmd = append(runcmd, "rm -f /dev/shm/acp-secrets")
	}
	var mounts [][]string
	if opts.ShareData {
		mounts = [][]string{{"share", "/data/", "9p", "trans=virtio,version=9p2000.L,rw,_netdev,nofail,x-systemd.automount", "0", "0"}}
	}
	return &CloudConfig{
		PackageUpdate: true,
		Packages:      packages,
		Users: []CloudUser{
			{
				Name:           opts.User,
				Shell:          "/bin/bash",
				HomeDir:        fmt.Sprintf("/home/%s", opts.User),
				NoCreateHome:   false,
				Groups:         []string{"sudo"},
				Sudo:           []string{"ALL=(ALL) NOPASSWD:ALL"},
				AuthorizedKeys: opts.AuthorizedKeys,
				LockPasswd:     len(opts.AuthorizedKeys) > 0,
			},
		},
		WriteFiles: writeFiles,
		RunCmd:     runcmd,
		Mounts:     mounts,
	}, nil
}

func GetInstaller(bin registry.RegistryPlatformBinary, sha256 string) (string, error) {
	if bin.Cmd == "" {
		return "", fmt.Errorf("empty command")
	}
	cmd := filepath.Base(bin.Cmd)
	base := filepath.Base(bin.Archive)
	out := make([]string, 0)
	out = append(out, "#!/usr/bin/env bash")
	out = append(out, "set -euo pipefail")
	out = append(out, "tmp=$(mktemp -d)")
	out = append(out, "cleanup() { rm -rf \"${tmp}\"; }")
	out = append(out, "trap cleanup EXIT")
	out = append(out, fmt.Sprintf("wget -O \"${tmp}\"/%s \"%s\"", base, bin.Archive))
	if sha256 != "" {
		out = append(out, fmt.Sprintf("echo \"%s  ${tmp}/%s\" | sha256sum -c -", sha256, base))
	}
	if strings.HasSuffix(bin.Archive, "tar.gz") {
		out = append(out, fmt.Sprintf("tar -xzf \"${tmp}\"/%s -C \"${tmp}\" %s", base, cmd))
	} else if strings.HasSuffix(bin.Archive, "tar.bz2") {
		out = append(out, fmt.Sprintf("tar -xjf \"${tmp}\"/%s -C \"${tmp}\" %s", base, cmd))
	} else {
		return "",
			fmt.Errorf("invalid archive format for %q", bin.Archive)
	}
	// TODO use find(1) instead in case of nested tarball extract
	out = append(out, fmt.Sprintf("install -m 0755 \"${tmp}\"/%s /usr/local/bin/%s", cmd, cmd))
	return strings.Join(out, "\n"), nil
}

func GetAcpRun(bin registry.RegistryPlatformBinary) string {
	acpRunScript := `#!/usr/bin/env bash
set -euo pipefail
umask 077
: "${ACP_WORKDIR:=/data}"
mkdir -p "${ACP_WORKDIR}"
cd "${ACP_WORKDIR}"
exec %s
`
	total := filepath.Base(bin.Cmd) + " " + strings.Join(bin.Args, " ")
	return fmt.Sprintf(acpRunScript, total)
}

func MarshalCloudConfig(cc CloudConfig) (string, error) {
	out, err := yaml.Marshal(cc)
	if err != nil {
		return "", fmt.Errorf("marshaling of cloud config: %v", err)
	}
	return "#cloud-config\n" + string(out), nil
}

func validateBinary(bin registry.RegistryPlatformBinary) error {
	cmd := filepath.Base(bin.Cmd)
	if !safeNameRegex.MatchString(cmd) {
		return fmt.Errorf("invalid command name %q: must match [a-zA-Z0-9._/-]+", cmd)
	}
	for _, arg := range bin.Args {
		if !safeArgRegex.MatchString(arg) {
			return fmt.Errorf("invalid argument %q: must match [a-zA-Z0-9._/=:,+-]+", arg)
		}
	}
	return nil
}
