package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
)

// profileStubClient implements just enough of incus.IncusClient to exercise
// verifyProfiles. The embedded nil interface supplies the rest of the method set;
// none of it is reached.
type profileStubClient struct {
	incus.IncusClient
	names []string
	err   error
}

func (c profileStubClient) GetProfileNames() ([]string, error) { return c.names, c.err }

func TestTemplateProfiles_FallbackToDefault(t *testing.T) {
	tests := []struct {
		name   string
		column string
		want   []string
	}{
		{"empty column", "", []string{"default"}},
		{"json null", "null", []string{"default"}},
		{"malformed", "not json", []string{"default"}},
		// An empty list must not reach Incus verbatim: Incus reads it as "no profiles
		// at all", so the container loses the default profile's network and root disk.
		{"empty list", "[]", []string{"default"}},
		{"explicit", `["default","docker"]`, []string{"default", "docker"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := templateProfiles(&models.Template{Profiles: tc.column})
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("templateProfiles(%q) = %v, want %v", tc.column, got, tc.want)
			}
		})
	}
}

// A template asking for a profile the server does not have must be refused up front.
// Incus otherwise reports it only from CreateInstance, once the DB row already exists —
// which is how instance "sentinel-care" ended up stuck in `error` with the reason
// ("Requested profile \"docker\" doesn't exist") buried in its creation log.
func TestVerifyProfiles_MissingProfileIsRefused(t *testing.T) {
	client := profileStubClient{names: []string{"default"}}

	err := verifyProfiles(client, "local", []string{"default", "docker"})
	if err == nil {
		t.Fatal("verifyProfiles accepted a profile the server does not have")
	}
	// The message has to name the culprit, otherwise it is no better than the
	// Incus error it replaces.
	if !strings.Contains(err.Error(), "docker") {
		t.Errorf("error does not name the missing profile: %v", err)
	}
	if !strings.Contains(err.Error(), "local") {
		t.Errorf("error does not name the server: %v", err)
	}
}

func TestVerifyProfiles_AllPresent(t *testing.T) {
	client := profileStubClient{names: []string{"default", "docker", "nvidia"}}

	if err := verifyProfiles(client, "local", []string{"default", "docker"}); err != nil {
		t.Errorf("verifyProfiles rejected profiles the server has: %v", err)
	}
}

// A server that cannot be listed must not block a creation that would otherwise
// have been attempted: the check is a better error message, not a new failure mode.
func TestVerifyProfiles_ListFailureDoesNotBlock(t *testing.T) {
	client := profileStubClient{err: os.ErrDeadlineExceeded}

	if err := verifyProfiles(client, "local", []string{"default", "docker"}); err != nil {
		t.Errorf("a profile-listing failure blocked creation: %v", err)
	}
}

// The regression guard for the actual outage: Docker inside an instance comes from the
// docker *mixin* (which contributes security.nesting), never from an Incus profile named
// "docker". The `ubuntu` template used to declare both, and the profile — which no Incus
// server here has ever had — is what broke every creation from it. If someone reinstates
// that profile to "get Docker back", this test says where Docker actually comes from.
func TestShippedTemplates_DockerComesFromMixinNotProfile(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "config", "templates")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("templates dir not available: %v", err)
	}

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
		// Any Docker mixin counts, not just the apt one: docker-arch installs Docker from
		// the Arch repos and contributes the same security.nesting. Matching only "docker"
		// would silently skip every Arch template and leave the invariant untested there.
		i := slices.IndexFunc(head.Includes, func(inc string) bool {
			return inc == "docker" || strings.HasPrefix(inc, "docker-")
		})
		if i < 0 {
			continue
		}
		dockerMixin := head.Includes[i]
		tmpl, err := s.templateFromYAML(data)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		checked++

		var cfg map[string]string
		if tmpl.IncusConfig != "" {
			if err := json.Unmarshal([]byte(tmpl.IncusConfig), &cfg); err != nil {
				t.Fatalf("%s: incus_config is not a JSON object: %v", filepath.Base(f), err)
			}
		}
		if cfg["security.nesting"] != "true" {
			t.Errorf("%s includes the %s mixin but resolves to security.nesting=%q, want \"true\" — Docker will not run in it",
				filepath.Base(f), dockerMixin, cfg["security.nesting"])
		}

		for _, p := range templateProfiles(&models.Template{Profiles: tmpl.Profiles}) {
			if p == "docker" {
				t.Errorf("%s declares the Incus profile \"docker\"; nesting already comes from the docker mixin, and requiring a profile that must be created out of band on every Incus server breaks creation there",
					filepath.Base(f))
			}
		}
	}

	if checked == 0 {
		t.Fatal("no shipped template includes a docker mixin — the guard tested nothing")
	}
	t.Logf("checked %d templates including a docker mixin", checked)
}
