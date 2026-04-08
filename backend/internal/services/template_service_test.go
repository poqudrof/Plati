package services

import (
	"os"
	"path/filepath"
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

	s := NewTemplateService(nil)
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
