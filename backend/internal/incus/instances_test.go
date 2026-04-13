package incus

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildInstanceConfig_Resources(t *testing.T) {
	resources := map[string]string{"cpu": "2", "memory": "4GB"}
	cfg := BuildInstanceConfig(resources)

	if cfg["limits.cpu"] != "2" {
		t.Errorf("limits.cpu = %q, want %q", cfg["limits.cpu"], "2")
	}
	if cfg["limits.memory"] != "4GB" {
		t.Errorf("limits.memory = %q, want %q", cfg["limits.memory"], "4GB")
	}
}

// ── BuildSetupCommands tests ──────────────────────────────────────────────────

func TestBuildSetupCommands_RootSSHAlways(t *testing.T) {
	steps := BuildSetupSteps(SetupConfig{
		PublicKeys: []string{"ssh-ed25519 AAAA test@host"},
	})

	if len(steps) == 0 {
		t.Fatal("expected steps, got none")
	}

	// First step must create /root/.ssh
	joined := strings.Join(steps[0].Cmd, " ")
	if !strings.Contains(joined, "/root/.ssh") {
		t.Errorf("first step should create /root/.ssh, got: %s", joined)
	}

	// Find authorized_keys push step
	found := false
	for _, step := range steps {
		if strings.Contains(step.FileDest, "/root/.ssh/authorized_keys") {
			found = true
			if !strings.Contains(string(step.FileContent), "ssh-ed25519 AAAA test@host") {
				t.Errorf("authorized_keys content missing expected key, got: %s", step.FileContent)
			}
			break
		}
	}
	if !found {
		t.Error("no authorized_keys push step found for root")
	}
}

func TestBuildSetupCommands_TerminalUser(t *testing.T) {
	steps := BuildSetupSteps(SetupConfig{
		PublicKeys:   []string{"ssh-ed25519 AAAA k"},
		TerminalUser: "ubuntu",
	})

	// Must have a "Wait for user ubuntu" step
	foundWait := false
	foundDir := false
	foundChown := false
	for _, step := range steps {
		if strings.Contains(step.Label, "Wait for user ubuntu") {
			foundWait = true
		}
		full := strings.Join(step.Cmd, " ")
		if strings.Contains(full, "/home/ubuntu/.ssh") {
			foundDir = true
		}
		if strings.Contains(full, "chown -R ubuntu:ubuntu") {
			foundChown = true
		}
	}
	if !foundWait {
		t.Error("expected 'Wait for user ubuntu' step")
	}
	if !foundDir {
		t.Error("expected /home/ubuntu/.ssh setup commands")
	}
	if !foundChown {
		t.Error("expected chown -R ubuntu:ubuntu command")
	}
}

func TestBuildSetupCommands_Secrets(t *testing.T) {
	cmds := BuildSetupCommands(SetupConfig{
		Secrets: map[string]string{"MY_TOKEN": "secret-value"},
	})

	found := false
	for _, cmd := range cmds {
		full := strings.Join(cmd, " ")
		if strings.Contains(full, "/etc/profile.d/plati-env.sh") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected /etc/profile.d/plati-env.sh write command")
	}
}

func TestBuildSetupCommands_SecretsContainHostname(t *testing.T) {
	cmds := BuildSetupCommands(SetupConfig{
		Secrets: map[string]string{
			"MY_TOKEN":                 "secret-value",
			"PLATI_TAILSCALE_HOSTNAME": "my-workspace",
		},
	})

	found := false
	for _, cmd := range cmds {
		full := strings.Join(cmd, " ")
		if strings.Contains(full, "plati-env.sh") {
			// The writeFile command is: printf '%s' '<B64>' | base64 -d > /etc/profile.d/plati-env.sh
			// Extract the base64 payload between the first pair of single quotes
			start := strings.Index(full, "printf '%s' '") + len("printf '%s' '")
			end := strings.Index(full[start:], "'")
			if end > 0 {
				b64 := full[start : start+end]
				decoded, err := base64.StdEncoding.DecodeString(b64)
				if err == nil && strings.Contains(string(decoded), "PLATI_TAILSCALE_HOSTNAME") {
					found = true
				}
			}
			break
		}
	}
	if !found {
		t.Error("PLATI_TAILSCALE_HOSTNAME not found in secrets env file")
	}
}

func TestBuildSetupCommands_PostCreateCmds(t *testing.T) {
	cmds := BuildSetupCommands(SetupConfig{
		IsFirstInit:    true,
		PostCreateCmds: []string{"apk add git", "mkdir -p /workspace"},
	})

	if len(cmds) < 2 {
		t.Fatalf("expected at least 2 commands, got %d", len(cmds))
	}

	// Custom commands should be the last entries
	last := cmds[len(cmds)-1]
	if len(last) < 3 || last[2] != "mkdir -p /workspace" {
		t.Errorf("last command should be 'mkdir -p /workspace', got %v", last)
	}
	secondLast := cmds[len(cmds)-2]
	if len(secondLast) < 3 || secondLast[2] != "apk add git" {
		t.Errorf("second-to-last command should be 'apk add git', got %v", secondLast)
	}
}
