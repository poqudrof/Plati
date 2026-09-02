package incus

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildInstanceConfig_Resources(t *testing.T) {
	resources := map[string]string{"cpu": "2", "memory": "4GB"}
	cfg := BuildInstanceConfig(resources, nil)

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
		PostCreateCmds: []string{"apk add git", "mkdir -p /opt/data"},
	})

	if len(cmds) < 2 {
		t.Fatalf("expected at least 2 commands, got %d", len(cmds))
	}

	// Custom commands should be the last entries
	last := cmds[len(cmds)-1]
	if len(last) < 3 || last[2] != "mkdir -p /opt/data" {
		t.Errorf("last command should be 'mkdir -p /opt/data', got %v", last)
	}
	secondLast := cmds[len(cmds)-2]
	if len(secondLast) < 3 || secondLast[2] != "apk add git" {
		t.Errorf("second-to-last command should be 'apk add git', got %v", secondLast)
	}
}

// The terminal user's home is often an Incus volume mount point owned by root:root,
// so setup must take ownership of the home itself before creating .ssh under it.
func TestBuildSetupSteps_HomeOwnershipBeforeSSH(t *testing.T) {
	steps := BuildSetupSteps(SetupConfig{
		PublicKeys:   []string{"ssh-ed25519 AAAA k"},
		TerminalUser: "ubuntu",
		IsFirstInit:  true,
	})

	homeIdx, sshIdx, fixIdx := -1, -1, -1
	for i, step := range steps {
		full := strings.Join(step.Cmd, " ")
		switch {
		case strings.Contains(step.Label, "Home (ubuntu): ensure ownership"):
			homeIdx = i
			if !strings.Contains(full, "chown ubuntu:ubuntu \"$home\"") {
				t.Errorf("home step should chown the home dir, got: %s", full)
			}
			if !strings.Contains(full, "-maxdepth 0 -user root") {
				t.Errorf("home step should only act on a root-owned home, got: %s", full)
			}
		case strings.Contains(step.Label, "Home (ubuntu): fix ownership of setup files"):
			fixIdx = i
		case step.FileDest == "/home/ubuntu/.ssh/authorized_keys":
			sshIdx = i
		}
	}

	if homeIdx == -1 {
		t.Fatal("expected a home ownership step for the terminal user")
	}
	if sshIdx == -1 {
		t.Fatal("expected an authorized_keys push for the terminal user")
	}
	if homeIdx > sshIdx {
		t.Errorf("home ownership (step %d) must run before writing .ssh (step %d)", homeIdx, sshIdx)
	}
	if fixIdx == -1 || fixIdx != len(steps)-1 {
		t.Errorf("expected the home content ownership fix as the last first-init step, got index %d of %d", fixIdx, len(steps))
	}
}

func TestBuildSetupSteps_NoHomeStepsForRoot(t *testing.T) {
	steps := BuildSetupSteps(SetupConfig{
		PublicKeys:   []string{"ssh-ed25519 AAAA k"},
		TerminalUser: "root",
		IsFirstInit:  true,
	})
	for _, step := range steps {
		if strings.Contains(step.Label, "Home (") {
			t.Errorf("root terminal user should not get home ownership steps, got: %s", step.Label)
		}
	}
}

// A persistent volume mounted over the home hides the image's skel files, so setup must
// restore them and put ~/.local/bin on PATH — that is where run_as: user mixins install.
func TestBuildSetupSteps_HomeSkeletonAndPath(t *testing.T) {
	steps := BuildSetupSteps(SetupConfig{TerminalUser: "ubuntu", IsFirstInit: true})

	ownIdx, skelIdx := -1, -1
	for i, step := range steps {
		full := strings.Join(step.Cmd, " ")
		switch {
		case strings.Contains(step.Label, "ensure ownership"):
			ownIdx = i
		case strings.Contains(step.Label, "seed shell dotfiles and PATH"):
			skelIdx = i
			if !strings.Contains(full, "cp -a -n") || !strings.Contains(full, "/etc/skel/") {
				t.Errorf("step should seed the home from /etc/skel without clobbering, got: %s", full)
			}
			// Regression: copying the skel directory itself stamps its root:root
			// ownership and mode onto the home, locking the user out of it.
			if strings.Contains(full, `cp -a -n /etc/skel/. "$home/"`) {
				t.Errorf("must copy skel entries individually, not the directory itself: %s", full)
			}
			if !strings.Contains(full, `export PATH="$HOME/.local/bin:$PATH"`) {
				t.Errorf("step should extend PATH with ~/.local/bin, got: %s", full)
			}
			if !strings.Contains(full, "grep -qs") {
				t.Errorf("PATH line must be appended idempotently, got: %s", full)
			}
		}
	}

	if skelIdx == -1 {
		t.Fatal("expected a dotfile seeding step for the terminal user")
	}
	if ownIdx == -1 || ownIdx > skelIdx {
		t.Errorf("home must be owned by the user (step %d) before seeding dotfiles (step %d)", ownIdx, skelIdx)
	}

	// Mixin and first-init commands must be able to rely on the PATH being set.
	for i, step := range steps {
		if len(step.Cmd) > 0 && strings.Contains(strings.Join(step.Cmd, " "), "su -l ubuntu") && i < skelIdx {
			t.Errorf("run_as: user command at step %d runs before dotfiles are seeded (%d)", i, skelIdx)
		}
	}
}

// Templates (and the mixins they include) can require Incus keys such as the
// security.nesting Docker needs; those take precedence over derived limits.
func TestBuildInstanceConfig_ExtraKeys(t *testing.T) {
	cfg := BuildInstanceConfig(
		map[string]string{"cpu": "2", "memory": "4GB"},
		map[string]string{"security.nesting": "true", "limits.cpu": "8"},
	)

	if cfg["security.nesting"] != "true" {
		t.Errorf("security.nesting = %q, want %q", cfg["security.nesting"], "true")
	}
	if cfg["limits.cpu"] != "8" {
		t.Errorf("template config should override derived limits: limits.cpu = %q, want %q", cfg["limits.cpu"], "8")
	}
	if cfg["limits.memory"] != "4GB" {
		t.Errorf("limits.memory = %q, want %q", cfg["limits.memory"], "4GB")
	}
}
