package incus

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	incusapi "github.com/lxc/incus/v6/shared/api"
)

// SetupConfig holds inputs for the Phase 2 exec-based initialization.
type SetupConfig struct {
	PublicKeys     []string
	PrivateKeyPEMs []string
	Secrets        map[string]string
	TerminalUser   string
	// IsFirstInit distinguishes first-create from rebuild.
	IsFirstInit bool
	// FirstInitCmds are run only on first create.
	FirstInitCmds []string
	// RebuildCmds are run only on rebuild.
	RebuildCmds []string
	// PostCreateCmds is the legacy field; used as FirstInitCmds when FirstInitCmds is empty.
	PostCreateCmds []string
	// SentinelPath is the file to touch after first-init to mark the volume initialized.
	// Empty string disables sentinel write.
	SentinelPath string
	// MixinFileSteps are file-push steps prepended before lifecycle commands (on first init).
	// Used for large mixin files (binaries, tarballs) pushed via PushFile instead of shell commands.
	MixinFileSteps []SetupStep
}

// SetupStep pairs a human-readable label with either a shell command or a file push.
// When FileContent is non-nil the step is executed via PushFile; otherwise via RunCommand.
// When FileSourcePath is non-empty, the file is read from disk and pushed (for large files).
type SetupStep struct {
	Label          string
	Cmd            []string // shell command; used when FileContent is nil and FileSourcePath is empty
	FileDest       string   // remote path for file push
	FileContent    []byte   // if set, use PushFile instead of RunCommand
	FileSourcePath string   // if set, read from disk and PushFile (used for large mixin binaries)
	FileMode       int      // unix mode (e.g. 0600)
	FileUID        int64
	FileGID        int64
}

// BuildSetupSteps returns an ordered list of labeled steps to run via incus exec
// after the instance starts. Each step has a Label for UI display and a Cmd to execute.
func BuildSetupSteps(cfg SetupConfig) []SetupStep {
	var steps []SetupStep

	steps = append(steps, buildSSHSetupSteps("/root/.ssh", cfg.PublicKeys, cfg.PrivateKeyPEMs, "")...)

	if cfg.TerminalUser != "" {
		// Wait for the terminal_user to exist (created by cloud-init on /cloud
		// images, or pre-existing in the image). Retries for up to 60s.
		steps = append(steps, SetupStep{
			Label: fmt.Sprintf("Wait for user %s", cfg.TerminalUser),
			Cmd:   []string{"/bin/sh", "-c", fmt.Sprintf("for i in $(seq 1 60); do id %s >/dev/null 2>&1 && break; sleep 1; done", cfg.TerminalUser)},
		})
		steps = append(steps, buildHomeOwnershipSteps(cfg.TerminalUser)...)
		steps = append(steps, buildHomeSkeletonSteps(cfg.TerminalUser)...)
		dir := fmt.Sprintf("/home/%s/.ssh", cfg.TerminalUser)
		steps = append(steps, buildSSHSetupSteps(dir, cfg.PublicKeys, cfg.PrivateKeyPEMs, cfg.TerminalUser)...)
	}

	if len(cfg.Secrets) > 0 {
		steps = append(steps, BuildSecretsEnvSteps(cfg.Secrets)...)
	}

	// Push mixin files before lifecycle commands (first init only).
	if cfg.IsFirstInit {
		steps = append(steps, cfg.MixinFileSteps...)
	}

	if cfg.IsFirstInit {
		initCmds := cfg.FirstInitCmds
		if len(initCmds) == 0 {
			initCmds = cfg.PostCreateCmds // backward-compat fallback
		}
		for _, cmd := range initCmds {
			label := cmd
			if len(label) > 72 {
				label = label[:69] + "..."
			}
			steps = append(steps, SetupStep{Label: label, Cmd: []string{"/bin/sh", "-c", cmd}})
		}
		// Write sentinel to mark the volume as initialized.
		if cfg.SentinelPath != "" {
			steps = append(steps, SetupStep{
				Label: "Mark workspace initialized",
				Cmd: []string{"/bin/sh", "-c",
					fmt.Sprintf("mkdir -p %s && touch %s",
						cfg.SentinelPath[:strings.LastIndex(cfg.SentinelPath, "/")],
						cfg.SentinelPath,
					),
				},
			})
		}
		// Anything root created under the home during setup (mixins, repo copies,
		// first_init_commands, the sentinel) is handed back to the terminal user.
		steps = append(steps, buildHomeContentOwnershipSteps(cfg.TerminalUser)...)
	} else {
		for _, cmd := range cfg.RebuildCmds {
			label := cmd
			if len(label) > 72 {
				label = label[:69] + "..."
			}
			steps = append(steps, SetupStep{Label: label, Cmd: []string{"/bin/sh", "-c", cmd}})
		}
	}

	return steps
}

// BuildSetupCommands extracts commands from BuildSetupSteps for backward compatibility.
func BuildSetupCommands(cfg SetupConfig) [][]string {
	steps := BuildSetupSteps(cfg)
	cmds := make([][]string, len(steps))
	for i, s := range steps {
		cmds[i] = s.Cmd
	}
	return cmds
}

// buildHomeOwnershipSteps makes sure the terminal user's home exists and belongs to them
// before anything is written into it. When a template persists /home/<user>, Incus creates
// the volume mount point as root:root 0711; a plain "mkdir -p ~/.ssh" would then set up a
// correct .ssh inside a home the user cannot write to. Ownership is only corrected while
// the directory is still root-owned, so a home already set up by the image is left alone.
func buildHomeOwnershipSteps(user string) []SetupStep {
	if user == "" || user == "root" {
		return nil
	}
	script := fmt.Sprintf(`home=$(getent passwd %[1]s | cut -d: -f6)
[ -n "$home" ] || home=/home/%[1]s
mkdir -p "$home"
if [ -n "$(find "$home" -maxdepth 0 -user root)" ]; then
  chown %[1]s:%[1]s "$home"
  chmod 750 "$home"
fi
`, user)
	return []SetupStep{{
		Label: fmt.Sprintf("Home (%s): ensure ownership", user),
		Cmd:   []string{"/bin/sh", "-c", script},
	}}
}

// buildHomeSkeletonSteps populates a home that a persistent volume mounted over. The image
// creates the user with /etc/skel copied into /home/<user>, but the volume is attached on
// top of that directory, so the shell dotfiles are hidden and the user logs into a home
// with no .bashrc and no .profile — which is where Ubuntu puts ~/.local/bin on PATH.
// Existing files are never overwritten (cp -n), so this is a no-op on later rebuilds.
func buildHomeSkeletonSteps(user string) []SetupStep {
	if user == "" || user == "root" {
		return nil
	}
	script := fmt.Sprintf(`home=$(getent passwd %[1]s | cut -d: -f6)
[ -n "$home" ] || home=/home/%[1]s
# Copy the skel entries one at a time: "cp -a /etc/skel/. $home/" would also stamp
# /etc/skel's own root:root ownership, mode and mtime onto the home directory itself,
# locking the user out of the home this very function is meant to make usable.
for f in /etc/skel/.[!.]* /etc/skel/*; do
  [ -e "$f" ] || continue
  cp -a -n "$f" "$home/" 2>/dev/null
  chown -R %[1]s:%[1]s "$home/$(basename "$f")" 2>/dev/null
done
# Tools installed by run_as: user mixins land in ~/.local/bin; .profile only covers login
# shells, so put it on PATH for interactive shells too.
if ! grep -qs '.local/bin' "$home/.bashrc"; then
  printf '\n# Added by Plati: tools installed into the user home\nexport PATH="$HOME/.local/bin:$PATH"\n' >> "$home/.bashrc"
  chown %[1]s:%[1]s "$home/.bashrc" 2>/dev/null
fi
exit 0
`, user)
	return []SetupStep{{
		Label: fmt.Sprintf("Home (%s): seed shell dotfiles and PATH", user),
		Cmd:   []string{"/bin/sh", "-c", script},
	}}
}

// buildHomeContentOwnershipSteps reassigns the top-level entries of the terminal user's
// home that setup created as root (repo copies, mixin installs, the sentinel file).
// Entries already owned by someone else are left untouched.
func buildHomeContentOwnershipSteps(user string) []SetupStep {
	if user == "" || user == "root" {
		return nil
	}
	script := fmt.Sprintf(`home=$(getent passwd %[1]s | cut -d: -f6)
[ -n "$home" ] || home=/home/%[1]s
[ -d "$home" ] || exit 0
if [ -n "$(find "$home" -maxdepth 0 -user root)" ]; then
  chown %[1]s:%[1]s "$home"
  chmod 750 "$home"
fi
find "$home" -mindepth 1 -maxdepth 1 -user root -exec chown -R %[1]s:%[1]s {} +
`, user)
	return []SetupStep{{
		Label: fmt.Sprintf("Home (%s): fix ownership of setup files", user),
		Cmd:   []string{"/bin/sh", "-c", script},
	}}
}

// buildSSHSetupSteps generates labeled exec steps to set up .ssh/ in the given directory.
// If owner is non-empty, a chown -R step is appended.
func buildSSHSetupSteps(sshDir string, publicKeys, privateKeyPEMs []string, owner string) []SetupStep {
	var steps []SetupStep

	who := "root"
	if owner != "" {
		who = owner
	}
	pfx := "SSH (" + who + ")"

	steps = append(steps, SetupStep{
		Label: pfx + ": init .ssh dir",
		Cmd:   []string{"/bin/sh", "-c", fmt.Sprintf("mkdir -p %s && chmod 700 %s", sshDir, sshDir)},
	})

	if len(publicKeys) > 0 {
		authorizedKeys := strings.Join(publicKeys, "\n") + "\n"
		steps = append(steps, SetupStep{
			Label:       pfx + ": write authorized_keys",
			FileDest:    sshDir + "/authorized_keys",
			FileContent: []byte(authorizedKeys),
			FileMode:    0600,
		})
	}

	if len(privateKeyPEMs) > 0 {
		var sshCfg strings.Builder
		sshCfg.WriteString("Host *\n")
		sshCfg.WriteString("  StrictHostKeyChecking accept-new\n")

		for i, pem := range privateKeyPEMs {
			keyFile := fmt.Sprintf("%s/id_rsa", sshDir)
			steps = append(steps, SetupStep{
				Label:       fmt.Sprintf("%s: write private key %d", pfx, i),
				FileDest:    keyFile,
				FileContent: []byte(pem),
				FileMode:    0600,
			})
			sshCfg.WriteString("  IdentityFile ~/.ssh/id_rsa\n")
		}

		steps = append(steps, SetupStep{
			Label:       pfx + ": write SSH config",
			FileDest:    sshDir + "/config",
			FileContent: []byte(sshCfg.String()),
			FileMode:    0600,
		})
	}

	if owner != "" {
		steps = append(steps, SetupStep{
			Label: fmt.Sprintf("%s: set ownership", pfx),
			Cmd:   []string{"/bin/sh", "-c", fmt.Sprintf("chown -R %s:%s %s", owner, owner, sshDir)},
		})
	}

	return steps
}

// BuildSecretsEnvSteps generates labeled exec steps to write /etc/profile.d/plati-env.sh.
func BuildSecretsEnvSteps(secrets map[string]string) []SetupStep {
	keys := make([]string, 0, len(secrets))
	for k := range secrets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	for _, k := range keys {
		encoded := base64.StdEncoding.EncodeToString([]byte(secrets[k]))
		sb.WriteString(fmt.Sprintf("export %s=\"$(printf '%%s' '%s' | base64 -d)\"\n", k, encoded))
	}

	return []SetupStep{
		{Label: "Secrets: write environment file", Cmd: writeFile("/etc/profile.d/plati-env.sh", sb.String())},
		{Label: "Secrets: set permissions", Cmd: []string{"/bin/sh", "-c", "chmod 644 /etc/profile.d/plati-env.sh"}},
	}
}

// writeFile returns an exec command that writes content to path using base64 -d.
// base64 alphabet [A-Za-z0-9+/=] contains no single quotes, so it is safe inside '...'.
func writeFile(path, content string) []string {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	return []string{"/bin/sh", "-c", fmt.Sprintf("printf '%%s' '%s' | base64 -d > %s", encoded, path)}
}

// GetInstanceIP returns the first IPv4 address of an instance (from eth0).
func GetInstanceIP(client IncusClient, name string) (string, error) {
	state, err := client.GetInstanceState(name)
	if err != nil {
		return "", err
	}

	for netName, net := range state.Network {
		if netName == "lo" {
			continue
		}
		for _, addr := range net.Addresses {
			if addr.Family == "inet" && addr.Scope == "global" {
				return addr.Address, nil
			}
		}
	}
	return "", fmt.Errorf("no IPv4 address found for %s", name)
}

// BuildInstanceConfig builds Incus config from resource limits, then applies the
// template's own keys. extra comes from the template's incus_config (itself the merge
// of what its mixins require, e.g. security.nesting for Docker) and wins on conflict.
func BuildInstanceConfig(resources, extra map[string]string) map[string]string {
	config := map[string]string{}

	if cpu, ok := resources["cpu"]; ok {
		config["limits.cpu"] = cpu
	}
	if mem, ok := resources["memory"]; ok {
		config["limits.memory"] = mem
	}

	for k, v := range extra {
		config[k] = v
	}

	return config
}

// InstanceInfo is a summary of instance state for API responses.
type InstanceInfo struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	IPAddress string `json:"ip_address,omitempty"`
}

// IncusNetworkAddress is an IP address on a network interface.
type IncusNetworkAddress struct {
	Family  string `json:"family"`
	Address string `json:"address"`
	Scope   string `json:"scope"`
}

// IncusNetworkInterface holds counters and addresses for one network interface.
type IncusNetworkInterface struct {
	Addresses []IncusNetworkAddress `json:"addresses"`
	RxBytes   int64                 `json:"rx_bytes"`
	TxBytes   int64                 `json:"tx_bytes"`
}

// IncusDetail is the live Incus view of an instance for admin display.
type IncusDetail struct {
	Type              string                           `json:"type"`
	Architecture      string                           `json:"architecture"`
	Profiles          []string                         `json:"profiles"`
	LimitsCPU         string                           `json:"limits_cpu"`
	LimitsMemory      string                           `json:"limits_memory"`
	LimitsMemorySwap  string                           `json:"limits_memory_swap"`
	LimitsProcesses   string                           `json:"limits_processes"`
	BootAutostart     string                           `json:"boot_autostart"`
	BootAutostartDelay string                          `json:"boot_autostart_delay"`
	SecurityNesting   string                           `json:"security_nesting"`
	SecurityPrivileged string                          `json:"security_privileged"`
	CPUUsageNs        int64                            `json:"cpu_usage_ns"`
	MemoryUsage       int64                            `json:"memory_usage"`
	MemoryPeak        int64                            `json:"memory_peak"`
	Processes         int64                            `json:"processes"`
	Network           map[string]IncusNetworkInterface `json:"network"`
	DiskUsage         map[string]int64                 `json:"disk_usage"`
}

// GetIncusDetail queries Incus directly for live instance state and metadata.
func GetIncusDetail(client IncusClient, name string) (*IncusDetail, error) {
	inst, err := client.GetInstance(name)
	if err != nil {
		return nil, fmt.Errorf("get instance: %w", err)
	}

	d := &IncusDetail{
		Type:               inst.Type,
		Architecture:       inst.Architecture,
		Profiles:           inst.Profiles,
		LimitsCPU:          inst.Config["limits.cpu"],
		LimitsMemory:       inst.Config["limits.memory"],
		LimitsMemorySwap:   inst.Config["limits.memory.swap"],
		LimitsProcesses:    inst.Config["limits.processes"],
		BootAutostart:      inst.Config["boot.autostart"],
		BootAutostartDelay: inst.Config["boot.autostart.delay"],
		SecurityNesting:    inst.Config["security.nesting"],
		SecurityPrivileged: inst.Config["security.privileged"],
	}

	state, err := client.GetInstanceState(name)
	if err != nil {
		return d, nil // return metadata even if state unavailable
	}

	d.CPUUsageNs = state.CPU.Usage
	d.MemoryUsage = state.Memory.Usage
	d.MemoryPeak = state.Memory.UsagePeak
	d.Processes = state.Processes

	d.Network = make(map[string]IncusNetworkInterface)
	for iface, net := range state.Network {
		addrs := make([]IncusNetworkAddress, 0, len(net.Addresses))
		for _, a := range net.Addresses {
			addrs = append(addrs, IncusNetworkAddress{
				Family:  a.Family,
				Address: a.Address,
				Scope:   a.Scope,
			})
		}
		d.Network[iface] = IncusNetworkInterface{
			Addresses: addrs,
			RxBytes:   net.Counters.BytesReceived,
			TxBytes:   net.Counters.BytesSent,
		}
	}

	d.DiskUsage = make(map[string]int64)
	for dev, disk := range state.Disk {
		d.DiskUsage[dev] = disk.Usage
	}

	return d, nil
}

// GetInstanceInfo retrieves instance status and IP.
func GetInstanceInfo(client IncusClient, name string) (*InstanceInfo, error) {
	inst, err := client.GetInstance(name)
	if err != nil {
		return nil, err
	}

	info := &InstanceInfo{
		Name:   inst.Name,
		Status: strings.ToLower(inst.Status),
	}

	if info.Status == "running" {
		ip, _ := GetInstanceIP(client, name)
		info.IPAddress = ip
	}

	return info, nil
}

// ImageSummary is a simplified image representation.
type ImageSummary struct {
	Fingerprint string            `json:"fingerprint"`
	Aliases     []string          `json:"aliases"`
	Size        int64             `json:"size"`
	Description string            `json:"description"`
	Properties  map[string]string `json:"properties"`
}

// SummarizeImages converts Incus images to API-friendly summaries.
func SummarizeImages(images []incusapi.Image) []ImageSummary {
	summaries := make([]ImageSummary, 0, len(images))
	for _, img := range images {
		aliases := make([]string, 0, len(img.Aliases))
		for _, a := range img.Aliases {
			aliases = append(aliases, a.Name)
		}
		summaries = append(summaries, ImageSummary{
			Fingerprint: img.Fingerprint,
			Aliases:     aliases,
			Size:        img.Size,
			Description: img.Properties["description"],
			Properties:  img.Properties,
		})
	}
	return summaries
}
