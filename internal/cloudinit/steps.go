package cloudinit

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/benjamin-james/agentctl/internal/dist"
)

// List of shared packages expected to be used.
var basePackageList = []string{
	"qemu-guest-agent", "ca-certificates", "wget", "libnss-mdns",
	"bzip2", "libcap2", "zlib1g", "libssl-dev", "libgcc-s1", "libgomp1",
	"libzstd1", "rsync",
}

// install debian packages. TODO consider in the future adding
// packages to `Agent` struct to split out some of these deps
func BasePackages(extra []string, d dist.Distribution) Step {
	return func(ci *CloudConfig) error {
		pkgs := make([]string, 0, len(extra)+len(basePackageList)+2)
		pkgs = append(pkgs, extra...)
		pkgs = append(pkgs, basePackageList...)
		pkgs = append(pkgs, d.Packages()...)
		ci.Packages = pkgs
		return nil
	}
}

// Exactly one user. Ensure /bin/bash shell, home under /home/,
// nopasswd sudo, and locked passwd iff SSH keys are added
func UserStep(user string, keys []string) Step {
	return func(ci *CloudConfig) error {
		ci.Users = []User{{
			Name:           user,
			Shell:          "/bin/bash",
			HomeDir:        fmt.Sprintf("/home/%s", user),
			NoCreateHome:   false,
			Groups:         []string{"sudo"},
			Sudo:           []string{"ALL=(ALL) NOPASSWD:ALL"},
			AuthorizedKeys: keys,
			LockPasswd:     len(keys) > 0,
		}}
		return nil
	}
}

// Installer of the ACP installer and ACP runner.
// Distribution's Installer/Runner methods perform their own validation.
func ACPStep(d dist.Distribution) Step {
	return func(ci *CloudConfig) error {
		installer, err := d.Installer()
		if err != nil {
			return fmt.Errorf("generating install-acp: %w", err)
		}
		runner, err := d.Runner()
		if err != nil {
			return fmt.Errorf("generating acp-run: %w", err)
		}
		ci.WriteFiles = append(ci.WriteFiles,
			WriteFile{Path: "/usr/local/sbin/install-acp", Permissions: "0755", Owner: "root:root", Content: installer},
			WriteFile{Path: "/usr/local/bin/acp-run", Permissions: "0755", Owner: "root:root", Content: runner},
		)
		ci.RunCmd = append(ci.RunCmd, "/usr/local/sbin/install-acp")
		return nil
	}
}

// Explicit creation and ownership of /data.
// If 9p sharing /data, create and automount, and ensure it is done before chown
func DataDirStep(user string, share bool) Step {
	return func(ci *CloudConfig) error {
		ci.RunCmd = append(ci.RunCmd, "mkdir -p /data")
		if share {
			ci.WriteFiles = append(ci.WriteFiles, WriteFile{
				Path:        "/etc/modules-load.d/9p.conf",
				Permissions: "0644",
				Owner:       "root:root",
				Content:     "9p\n9pnet\n9pnet_virtio",
			})
			ci.RunCmd = append(ci.RunCmd, "systemctl daemon-reload", "systemctl enable --now data.automount")
			ci.Mounts = [][]string{{
				"share", "/data/", "9p",
				"trans=virtio,version=9p2000.L,rw,_netdev,nofail,x-systemd.automount",
				"0", "0",
			}}
		}
		ci.RunCmd = append(ci.RunCmd, fmt.Sprintf("chown -R \"%s\":\"%s\" /data", user, user))
		return nil
	}
}

// Stages config file in /dev/shm and installs into $HOME under the config path
func StageConfig(acpPath, data, user string) Step {
	return stageFile(acpPath, data, user, "/dev/shm/acp-config", "0644", 0700, 0644)
}

// Secrets: like StageConfig but with 0600 for secrets
func StageSecrets(acpPath, data, user string) Step {
	return stageFile(acpPath, data, user, "/dev/shm/acp-secrets", "0600", 0700, 0600)
}

// Writing a raw file is two-staged: first, write to a hard location as root,
// then install to the user-owned path.
func stageFile(acpPath, data, user, stagePath, stagePerm string, dirPerm, filePerm int) Step {
	return func(ci *CloudConfig) error {
		ci.WriteFiles = append(ci.WriteFiles, WriteFile{
			Path:        stagePath,
			Permissions: stagePerm,
			Owner:       "root:root",
			Content:     data,
		})
		ci.RunCmd = append(ci.RunCmd, InstallToDir(acpPath, stagePath, user, user, dirPerm, filePerm)...)
		return nil
	}
}

// Build the runcmd sequence to install a staged file with filePerm chmod.
// Installs dirs as necessary with dirPerm in order, and thus reversed from walking up dirs to $HOME.
// User and paths are double-quoted.
func InstallToDir(dst, src, user, group string, dirPerm, filePerm int) []string {
	home := filepath.Clean(fmt.Sprintf("/home/%s", user))
	dst = filepath.Clean(strings.Replace(dst, "$HOME", home, 1))
	cmds := []string{
		fmt.Sprintf("rm -f \"%s\"", src),
		fmt.Sprintf("install -m %o -o \"%s\" -g \"%s\" \"%s\" \"%s\"", filePerm, user, group, src, dst),
	}
	for d := filepath.Clean(filepath.Dir(dst)); filepath.Clean(d) != home; d = filepath.Clean(filepath.Dir(d)) {
		cmds = append(cmds, fmt.Sprintf("install -d -m %o -o \"%s\" -g \"%s\" \"%s\"", dirPerm, user, group, d))
	}
	slices.Reverse(cmds)
	return cmds
}
