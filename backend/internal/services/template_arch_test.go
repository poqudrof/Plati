package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// shippedTemplatesDir is the real config/templates checkout, resolved from this package.
func shippedTemplatesDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "config", "templates")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("templates dir not available: %v", err)
	}
	return dir
}

// loadShippedTemplate resolves one shipped template YAML against the real mixins,
// exactly as SyncFromDir does at startup.
func loadShippedTemplate(t *testing.T, slug string) (*TemplateService, []string, map[string]string) {
	t.Helper()
	dir := shippedTemplatesDir(t)

	s := NewTemplateService(nil, "")
	if err := s.LoadMixinsFromDir(dir); err != nil {
		t.Fatalf("load mixins: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, slug+".yaml"))
	if err != nil {
		t.Fatalf("read %s.yaml: %v", slug, err)
	}
	tmpl, err := s.templateFromYAML(data)
	if err != nil {
		t.Fatalf("parse %s.yaml: %v", slug, err)
	}

	var firstInit []string
	if err := json.Unmarshal([]byte(tmpl.FirstInitCommands), &firstInit); err != nil {
		t.Fatalf("unmarshal first_init: %v", err)
	}
	cfg := map[string]string{}
	if tmpl.IncusConfig != "" {
		if err := json.Unmarshal([]byte(tmpl.IncusConfig), &cfg); err != nil {
			t.Fatalf("%s: incus_config is not a JSON object: %v", slug, err)
		}
	}
	return s, firstInit, cfg
}

// A template that names a mixin which does not exist loses those commands silently:
// templateFromYAML only logs "mixin %q not found, skipping". A typo in an includes list
// would then ship a template that provisions an instance missing Docker or Tailscale,
// with nothing failing anywhere. This is the guard for that.
func TestShippedTemplates_IncludesResolveToRealMixins(t *testing.T) {
	dir := shippedTemplatesDir(t)

	s := NewTemplateService(nil, "")
	if err := s.LoadMixinsFromDir(dir); err != nil {
		t.Fatalf("load mixins: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no template YAML found")
	}

	checked := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var head struct {
			Includes []string `yaml:"includes"`
		}
		if err := yaml.Unmarshal(data, &head); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, inc := range head.Includes {
			if _, ok := s.GetMixin(inc); !ok {
				t.Errorf("%s includes mixin %q, which does not exist in mixins/ — its commands would be dropped silently",
					filepath.Base(f), inc)
			}
			checked++
		}
	}
	t.Logf("checked %d mixin references across %d templates", checked, len(files))
}

// The Arch template must resolve to a Debian-free command list. Mixin commands are
// prepended to first_init_commands, so including the apt-based `docker` or `tailscale`
// mixin here would not fail the parse — it would ship a template whose Docker install
// silently never runs (apt-get exits 127, logged as a WARN and skipped). Asserting on the
// resolved list is what catches that, since neither the YAML nor the mixins look wrong alone.
func TestArchTemplate_ResolvesWithoutDebianCommands(t *testing.T) {
	_, firstInit, cfg := loadShippedTemplate(t, "arch")
	joined := strings.Join(firstInit, "\n")

	for _, forbidden := range []string{"apt-get", "apt ", "dpkg", "add-apt-repository", "DEBIAN_FRONTEND"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("arch.yaml resolves to a command containing %q — that is a Debian-only command and fails on Arch:\n%s",
				forbidden, joined)
		}
	}
	if !strings.Contains(joined, "pacman") {
		t.Errorf("arch.yaml resolves to no pacman command at all:\n%s", joined)
	}

	// Docker must still come from a mixin, with the nesting it needs.
	if cfg["security.nesting"] != "true" {
		t.Errorf("security.nesting = %q, want \"true\" — Docker will not run in the instance", cfg["security.nesting"])
	}
}

// The Arch mixins have to actually contribute their commands, and before the template's
// own: first_init_commands runs `usermod -aG docker arch`, which only works because the
// docker group already exists — created by docker-arch, which pacman installs earlier.
func TestArchTemplate_MixinCommandsPrecedeTemplateCommands(t *testing.T) {
	_, firstInit, _ := loadShippedTemplate(t, "arch")
	joined := strings.Join(firstInit, "\n")

	for _, want := range []string{
		"--needed docker",      // docker-arch
		"fuse-overlayfs",       // docker-arch daemon.json
		"--needed tailscale",   // tailscale-arch
		"tailscaled",           // tailscale-arch
		"openvscode-server",    // openvscode-server mixin
		"claude.ai/install.sh", // claude-code mixin
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("resolved first_init_commands miss %q:\n%s", want, joined)
		}
	}

	dockerIdx := strings.Index(joined, "--needed docker")
	usermodIdx := strings.Index(joined, "usermod -aG docker arch")
	if dockerIdx < 0 || usermodIdx < 0 {
		t.Fatalf("expected both the docker install and the usermod in:\n%s", joined)
	}
	if dockerIdx >= usermodIdx {
		t.Errorf("docker-arch must install Docker before the template adds the user to the docker group, got install at %d and usermod at %d",
			dockerIdx, usermodIdx)
	}
}

// A rebuild recreates the container from the image and reruns only rebuild_commands —
// mixins do not run again. The Arch image ships no sshd, so a rebuild_commands list that
// merely restarts services would hand back an instance nobody can SSH into.
func TestArchTemplate_RebuildReinstallsWhatTheImageLacks(t *testing.T) {
	dir := shippedTemplatesDir(t)
	data, err := os.ReadFile(filepath.Join(dir, "arch.yaml"))
	if err != nil {
		t.Fatalf("read arch.yaml: %v", err)
	}
	var head struct {
		RebuildCommands []string `yaml:"rebuild_commands"`
	}
	if err := yaml.Unmarshal(data, &head); err != nil {
		t.Fatalf("parse arch.yaml: %v", err)
	}
	if len(head.RebuildCommands) == 0 {
		t.Fatal("arch.yaml has no rebuild_commands")
	}
	joined := strings.Join(head.RebuildCommands, "\n")

	// openssh is what makes this non-negotiable; docker and tailscale are gone too.
	for _, want := range []string{"openssh", "docker", "tailscale"} {
		if !strings.Contains(joined, want) {
			t.Errorf("rebuild_commands never reinstall %q — a rebuilt instance would come back without it:\n%s",
				want, joined)
		}
	}
	if strings.Contains(joined, "apt-get") {
		t.Errorf("rebuild_commands contain a Debian-only command:\n%s", joined)
	}
}
