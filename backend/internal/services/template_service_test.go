package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMixins_FilesExpanded(t *testing.T) {
	dir := t.TempDir()
	mixinsDir := filepath.Join(dir, "mixins")
	if err := os.Mkdir(mixinsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write the asset file that the mixin references.
	assetContent := []byte("hello\n")
	assetPath := filepath.Join(mixinsDir, "myfile.txt")
	if err := os.WriteFile(assetPath, assetContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Write the mixin YAML.
	mixinYAML := `name: Test mixin
files:
  - src: myfile.txt
    dest: /etc/test/myfile.txt
    mode: "0644"
post_create_commands:
  - echo done
`
	if err := os.WriteFile(filepath.Join(mixinsDir, "test.yaml"), []byte(mixinYAML), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewTemplateService(nil, "")
	if err := s.LoadMixinsFromDir(dir); err != nil {
		t.Fatal(err)
	}

	m, ok := s.mixins["test"]
	if !ok {
		t.Fatal("mixin 'test' not loaded")
	}

	// PostCreateCommands must only contain the original command — no cp/chmod injected.
	if len(m.PostCreateCommands) != 1 {
		t.Fatalf("expected 1 PostCreateCommand, got %d: %v", len(m.PostCreateCommands), m.PostCreateCommands)
	}
	if m.PostCreateCommands[0] != "echo done" {
		t.Errorf("command[0] = %q, want %q", m.PostCreateCommands[0], "echo done")
	}

	// GetMixinFileSteps must return a PushFile step for the declared file.
	steps := s.GetMixinFileSteps([]string{"test"})
	if len(steps) != 1 {
		t.Fatalf("expected 1 mixin file step, got %d", len(steps))
	}
	step := steps[0]
	if step.FileSourcePath != assetPath {
		t.Errorf("FileSourcePath = %q, want %q", step.FileSourcePath, assetPath)
	}
	if step.FileDest != "/etc/test/myfile.txt" {
		t.Errorf("FileDest = %q, want %q", step.FileDest, "/etc/test/myfile.txt")
	}
	if step.FileMode != 0644 {
		t.Errorf("FileMode = %o, want 0644", step.FileMode)
	}
}

func TestLoadMixins_MissingFileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	mixinsDir := filepath.Join(dir, "mixins")
	if err := os.Mkdir(mixinsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Mixin references a src file that does not exist.
	mixinYAML := `name: Missing file mixin
files:
  - src: nonexistent.txt
    dest: /etc/nonexistent.txt
post_create_commands:
  - echo still here
`
	if err := os.WriteFile(filepath.Join(mixinsDir, "missing.yaml"), []byte(mixinYAML), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewTemplateService(nil, "")
	if err := s.LoadMixinsFromDir(dir); err != nil {
		t.Fatal(err)
	}

	m, ok := s.mixins["missing"]
	if !ok {
		t.Fatal("mixin 'missing' should still be loaded even when a file entry is absent")
	}

	// The missing file entry is skipped; only the original command remains.
	if len(m.PostCreateCommands) != 1 {
		t.Fatalf("expected 1 PostCreateCommand, got %d: %v", len(m.PostCreateCommands), m.PostCreateCommands)
	}
	if m.PostCreateCommands[0] != "echo still here" {
		t.Errorf("command[0] = %q, want %q", m.PostCreateCommands[0], "echo still here")
	}
}

func TestLoadMixins_NoFiles(t *testing.T) {
	dir := t.TempDir()
	mixinsDir := filepath.Join(dir, "mixins")
	if err := os.Mkdir(mixinsDir, 0755); err != nil {
		t.Fatal(err)
	}

	mixinYAML := `name: Simple mixin
post_create_commands:
  - apt-get install -y curl
  - echo ready
`
	if err := os.WriteFile(filepath.Join(mixinsDir, "simple.yaml"), []byte(mixinYAML), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewTemplateService(nil, "")
	if err := s.LoadMixinsFromDir(dir); err != nil {
		t.Fatal(err)
	}

	m, ok := s.mixins["simple"]
	if !ok {
		t.Fatal("mixin 'simple' not loaded")
	}

	if len(m.PostCreateCommands) != 2 {
		t.Fatalf("expected 2 PostCreateCommands, got %d: %v", len(m.PostCreateCommands), m.PostCreateCommands)
	}
	if m.PostCreateCommands[0] != "apt-get install -y curl" {
		t.Errorf("command[0] = %q, want %q", m.PostCreateCommands[0], "apt-get install -y curl")
	}
	if m.PostCreateCommands[1] != "echo ready" {
		t.Errorf("command[1] = %q, want %q", m.PostCreateCommands[1], "echo ready")
	}
}

// TestMixins_CommandsPrependedToFirstInit verifies that when a template has both
// first_init_commands and includes, the mixin commands are prepended to first_init_commands.
func TestMixins_CommandsPrependedToFirstInit(t *testing.T) {
	dir := t.TempDir()
	mixinsDir := filepath.Join(dir, "mixins")
	if err := os.Mkdir(mixinsDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(mixinsDir, "docker.yaml"), []byte(`name: Docker mixin
post_create_commands:
  - apt-get install -y docker-ce
  - systemctl enable docker
`), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewTemplateService(nil, "")
	if err := s.LoadMixinsFromDir(dir); err != nil {
		t.Fatal(err)
	}

	tmplYAML := `name: Ubuntu
slug: ubuntu-test
description: Test
image: images:ubuntu/24.04/cloud
profiles: [default]
resources:
  cpu: "2"
  memory: 4GB
  disk: 20GB
includes:
  - docker
first_init_commands:
  - apt-get install -y git
  - echo done
`
	tmpl, err := s.templateFromYAML([]byte(tmplYAML))
	if err != nil {
		t.Fatalf("templateFromYAML: %v", err)
	}

	var firstInit []string
	if err := json.Unmarshal([]byte(tmpl.FirstInitCommands), &firstInit); err != nil {
		t.Fatalf("unmarshal first_init: %v", err)
	}

	// Expected order: mixin commands first, then template commands
	if len(firstInit) != 4 {
		t.Fatalf("expected 4 first_init commands, got %d: %v", len(firstInit), firstInit)
	}
	if firstInit[0] != "apt-get install -y docker-ce" {
		t.Errorf("first_init[0] = %q, want mixin command", firstInit[0])
	}
	if firstInit[1] != "systemctl enable docker" {
		t.Errorf("first_init[1] = %q, want mixin command", firstInit[1])
	}
	if firstInit[2] != "apt-get install -y git" {
		t.Errorf("first_init[2] = %q, want template command", firstInit[2])
	}
	if firstInit[3] != "echo done" {
		t.Errorf("first_init[3] = %q, want template command", firstInit[3])
	}
}

// TestMixins_AllNonNvidiaCommandsPresent verifies that all non-NVIDIA mixin commands
// are included when a template includes tailscale, sshx, docker, openvscode-server,
// and claude-code.
func TestMixins_AllNonNvidiaCommandsPresent(t *testing.T) {
	dir := t.TempDir()
	mixinsDir := filepath.Join(dir, "mixins")
	if err := os.Mkdir(mixinsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create simplified mixin YAMLs matching the real mixin commands (no binary files needed).
	mixins := map[string]string{
		"tailscale.yaml": `name: Tailscale mixin
post_create_commands:
  - apt-get install -y -q iptables
  - sh /root/tailscale-install.sh
  - systemctl enable tailscaled
  - systemctl start tailscaled
  - '. /etc/profile.d/plati-env.sh && [ -n "$TAILSCALE_AUTH_KEY" ] && tailscale up --auth-key="$TAILSCALE_AUTH_KEY" || true'
`,
		"sshx.yaml": `name: SSHX mixin
post_create_commands:
  - systemctl daemon-reload
  - systemctl enable sshx
  - systemctl start sshx
`,
		"docker.yaml": `name: Docker mixin
post_create_commands:
  - apt-get update -y && apt-get install -y curl ca-certificates fuse-overlayfs
  - apt-get update -y && DEBIAN_FRONTEND=noninteractive apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  - mkdir -p /etc/docker && echo '{"storage-driver":"fuse-overlayfs"}' > /etc/docker/daemon.json
  - systemctl daemon-reload && systemctl enable containerd docker && systemctl start containerd && systemctl start docker
`,
		"openvscode-server.yaml": `name: OpenVSCode Server mixin
post_create_commands:
  - tar -xzf /root/openvscode-server.tar.gz -C /opt/
  - mv /opt/openvscode-server-v1.109.5-linux-x64 /opt/openvscode-server
  - systemctl daemon-reload
  - systemctl enable openvscode-server
  - systemctl start openvscode-server
`,
		"claude-code.yaml": `name: Claude Code mixin
post_create_commands:
  - curl -fsSL https://claude.ai/install.sh | bash
`,
	}

	for name, content := range mixins {
		if err := os.WriteFile(filepath.Join(mixinsDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	s := NewTemplateService(nil, "")
	if err := s.LoadMixinsFromDir(dir); err != nil {
		t.Fatal(err)
	}

	tmplYAML := `name: Ubuntu
slug: ubuntu
description: Ubuntu 24.04 LTS dev environment.
image: images:ubuntu/24.04/cloud
profiles: [default, docker]
resources:
  cpu: "2"
  memory: 4GB
  disk: 20GB
terminal_user: ubuntu
includes:
  - tailscale
  - sshx
  - docker
  - openvscode-server
  - claude-code
first_init_commands:
  - apt-get update -y
  - apt-get install -y git curl wget build-essential openssh-server
  - systemctl enable ssh
  - systemctl restart ssh
`
	tmpl, err := s.templateFromYAML([]byte(tmplYAML))
	if err != nil {
		t.Fatalf("templateFromYAML: %v", err)
	}

	var firstInit []string
	if err := json.Unmarshal([]byte(tmpl.FirstInitCommands), &firstInit); err != nil {
		t.Fatalf("unmarshal first_init: %v", err)
	}

	joined := strings.Join(firstInit, "\n")

	checks := []struct {
		mixin   string
		keyword string
	}{
		{"tailscale", "tailscale-install.sh"},
		{"tailscale", "tailscaled"},
		{"sshx", "systemctl enable sshx"},
		{"docker", "fuse-overlayfs"},
		{"docker", "docker-ce"},
		{"openvscode-server", "openvscode-server.tar.gz"},
		{"openvscode-server", "/opt/openvscode-server"},
		{"claude-code", "claude.ai/install.sh"},
	}

	for _, c := range checks {
		if !strings.Contains(joined, c.keyword) {
			t.Errorf("mixin %q: keyword %q not found in first_init_commands", c.mixin, c.keyword)
		}
	}

	// Mixin commands must appear BEFORE template's own commands.
	tailscaleIdx := strings.Index(joined, "tailscale-install.sh")
	aptGitIdx := strings.Index(joined, "apt-get install -y git")
	if tailscaleIdx >= aptGitIdx {
		t.Errorf("mixin commands should precede template first_init_commands, but tailscale index %d >= git index %d", tailscaleIdx, aptGitIdx)
	}

	// Template's own first_init_commands must still be present.
	if !strings.Contains(joined, "apt-get install -y git curl wget build-essential openssh-server") {
		t.Error("template's own first_init_commands not found")
	}

	// The includes field should list all non-NVIDIA mixins.
	var includes []string
	if err := json.Unmarshal([]byte(tmpl.Includes), &includes); err != nil {
		t.Fatalf("unmarshal includes: %v", err)
	}
	for _, expected := range []string{"tailscale", "sshx", "docker", "openvscode-server", "claude-code"} {
		found := false
		for _, inc := range includes {
			if inc == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("includes missing %q", expected)
		}
	}
}

// TestMixins_NvidiaExcluded verifies that NVIDIA mixin (which requires GPU hardware)
// is not included in the Ubuntu template's includes field.
func TestMixins_NvidiaExcluded(t *testing.T) {
	dir := t.TempDir()
	mixinsDir := filepath.Join(dir, "mixins")
	if err := os.Mkdir(mixinsDir, 0755); err != nil {
		t.Fatal(err)
	}

	for name, content := range map[string]string{
		"nvidia.yaml": `name: Nvidia mixin
post_create_commands:
  - nvidia-ctk runtime configure --runtime=docker
`,
		"docker.yaml": `name: Docker mixin
post_create_commands:
  - apt-get install -y docker-ce
`,
	} {
		if err := os.WriteFile(filepath.Join(mixinsDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	s := NewTemplateService(nil, "")
	if err := s.LoadMixinsFromDir(dir); err != nil {
		t.Fatal(err)
	}

	// Ubuntu template only includes docker (not nvidia).
	tmplYAML := `name: Ubuntu
slug: ubuntu-no-gpu
description: Ubuntu without GPU
image: images:ubuntu/24.04/cloud
profiles: [default]
resources:
  cpu: "2"
  memory: 4GB
  disk: 20GB
includes:
  - docker
first_init_commands:
  - echo init
`
	tmpl, err := s.templateFromYAML([]byte(tmplYAML))
	if err != nil {
		t.Fatalf("templateFromYAML: %v", err)
	}

	var firstInit []string
	if err := json.Unmarshal([]byte(tmpl.FirstInitCommands), &firstInit); err != nil {
		t.Fatalf("unmarshal first_init: %v", err)
	}

	joined := strings.Join(firstInit, "\n")
	if strings.Contains(joined, "nvidia-ctk") {
		t.Error("nvidia mixin commands should not appear in ubuntu template")
	}
	if !strings.Contains(joined, "docker-ce") {
		t.Error("docker mixin command should appear in template")
	}
}
