package services

import (
	"encoding/base64"
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
	if err := os.WriteFile(filepath.Join(mixinsDir, "myfile.txt"), []byte("hello\n"), 0644); err != nil {
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

	s := NewTemplateService(nil)
	if err := s.LoadMixinsFromDir(dir); err != nil {
		t.Fatal(err)
	}

	m, ok := s.mixins["test"]
	if !ok {
		t.Fatal("mixin 'test' not loaded")
	}

	// Expect: write cmd + chmod cmd + echo done = 3 commands.
	if len(m.PostCreateCommands) != 3 {
		t.Fatalf("expected 3 PostCreateCommands, got %d: %v", len(m.PostCreateCommands), m.PostCreateCommands)
	}

	// Assert command[0] is the base64 write command for the correct dest.
	writeCmd := m.PostCreateCommands[0]
	if !strings.Contains(writeCmd, "base64 -d > /etc/test/myfile.txt") {
		t.Errorf("write command missing expected dest: %q", writeCmd)
	}

	// Decode the base64 payload and verify file content.
	// Command format: printf '%s' '<B64>' | base64 -d > <dest>
	start := strings.Index(writeCmd, "printf '%s' '") + len("printf '%s' '")
	end := strings.Index(writeCmd[start:], "'")
	if end <= 0 {
		t.Fatalf("could not extract base64 payload from: %q", writeCmd)
	}
	b64 := writeCmd[start : start+end]
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}
	if string(decoded) != "hello\n" {
		t.Errorf("decoded content = %q, want %q", decoded, "hello\n")
	}

	// Assert command[1] is the chmod command.
	if m.PostCreateCommands[1] != "chmod 0644 /etc/test/myfile.txt" {
		t.Errorf("command[1] = %q, want %q", m.PostCreateCommands[1], "chmod 0644 /etc/test/myfile.txt")
	}

	// Assert command[2] is the original post_create_command.
	if m.PostCreateCommands[2] != "echo done" {
		t.Errorf("command[2] = %q, want %q", m.PostCreateCommands[2], "echo done")
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

	s := NewTemplateService(nil)
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

	s := NewTemplateService(nil)
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
