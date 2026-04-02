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
}

// SetupStep pairs a human-readable label with either a shell command or a file push.
// When FileContent is non-nil the step is executed via PushFile; otherwise via RunCommand.
type SetupStep struct {
	Label       string
	Cmd         []string // shell command; used when FileContent is nil
	FileDest    string   // remote path for file push
	FileContent []byte   // if set, use PushFile instead of RunCommand
	FileMode    int      // unix mode (e.g. 0600)
	FileUID     int64
	FileGID     int64
}

// BuildSetupSteps returns an ordered list of labeled steps to run via incus exec
// after the instance starts. Each step has a Label for UI display and a Cmd to execute.
func BuildSetupSteps(cfg SetupConfig) []SetupStep {
	var steps []SetupStep

	steps = append(steps, buildSSHSetupSteps("/root/.ssh", cfg.PublicKeys, cfg.PrivateKeyPEMs, "")...)

	if cfg.TerminalUser != "" {
		dir := fmt.Sprintf("/home/%s/.ssh", cfg.TerminalUser)
		steps = append(steps, buildSSHSetupSteps(dir, cfg.PublicKeys, cfg.PrivateKeyPEMs, cfg.TerminalUser)...)
	}

	if len(cfg.Secrets) > 0 {
		steps = append(steps, buildSecretsEnvFileSteps(cfg.Secrets)...)
	}

	// Choose which lifecycle commands to run.
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

// buildSecretsEnvFileSteps generates labeled exec steps to write /etc/profile.d/plati-env.sh.
func buildSecretsEnvFileSteps(secrets map[string]string) []SetupStep {
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

// BuildInstanceConfig builds Incus config from template data, SSH public keys, and SSH private key PEMs.
// sshPublicKeys are added to cloud-init ssh_authorized_keys (allows SSH into the instance).
// privateKeyPEMs are written to ~/.ssh/ inside the instance (allows git operations from the instance).
func BuildInstanceConfig(cloudInit string, sshPublicKeys []string, privateKeyPEMs []string, resources map[string]string) map[string]string {
	config := map[string]string{}

	if cpu, ok := resources["cpu"]; ok {
		config["limits.cpu"] = cpu
	}
	if mem, ok := resources["memory"]; ok {
		config["limits.memory"] = mem
	}

	if cloudInit != "" {
		if len(sshPublicKeys) > 0 {
			keyLines := strings.Join(sshPublicKeys, "\n    - ")
			cloudInit += fmt.Sprintf("\nssh_authorized_keys:\n    - %s\n", keyLines)
		}
		if len(privateKeyPEMs) > 0 {
			cloudInit += buildWriteFilesBlock(privateKeyPEMs)
		}
		config["user.user-data"] = cloudInit
	}

	return config
}

// buildWriteFilesBlock generates a cloud-init write_files block that writes
// private key files and an SSH config to /home/ubuntu/.ssh/.
// Assumes the default instance user is "ubuntu" (standard for Ubuntu cloud images).
// Note: if the template already contains a write_files key, append this block
// after it or merge manually — duplicate top-level YAML keys are implementation-defined.
func buildWriteFilesBlock(privateKeyPEMs []string) string {
	var sb strings.Builder
	sb.WriteString("\nwrite_files:\n")

	for i, keyPEM := range privateKeyPEMs {
		keyFile := fmt.Sprintf("plati_key_%d", i)
		sb.WriteString(fmt.Sprintf("  - path: /home/ubuntu/.ssh/%s\n", keyFile))
		sb.WriteString("    content: |\n")
		sb.WriteString(indentContent(keyPEM, "      "))
		sb.WriteString("    owner: ubuntu:ubuntu\n")
		sb.WriteString("    permissions: '0600'\n")
	}

	// Write SSH config so all keys are tried automatically.
	var sshCfg strings.Builder
	sshCfg.WriteString("Host *\n")
	sshCfg.WriteString("  StrictHostKeyChecking accept-new\n")
	for i := range privateKeyPEMs {
		sshCfg.WriteString(fmt.Sprintf("  IdentityFile ~/.ssh/plati_key_%d\n", i))
	}

	sb.WriteString("  - path: /home/ubuntu/.ssh/config\n")
	sb.WriteString("    content: |\n")
	sb.WriteString(indentContent(sshCfg.String(), "      "))
	sb.WriteString("    owner: ubuntu:ubuntu\n")
	sb.WriteString("    permissions: '0600'\n")

	return sb.String()
}

// indentContent prefixes every non-empty line with the given indent string.
func indentContent(s, indent string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var out strings.Builder
	for _, line := range lines {
		if line == "" {
			out.WriteByte('\n')
		} else {
			out.WriteString(indent + line + "\n")
		}
	}
	return out.String()
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
