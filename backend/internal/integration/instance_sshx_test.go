// Tests for the sshx build endpoints. sshx is maintained in-house here, so an instance can
// end up running an older binary than the one the server ships — or, as happened on
// plati-1-gaaspard-dev, no binary at all, because the mixin's file push failed at creation
// and left the unit crash-looping. Both are what these endpoints exist to report and repair.
package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/homaserver/plati/internal/models"
)

// newRunningInstance seeds a template, creates an instance and waits for it to be running,
// returning its id. The sshx endpoints all require a running instance.
func newRunningInstance(t *testing.T, h *harness, name string) int64 {
	t.Helper()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	templateID := seedTemplate(t, h)

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name":        name,
		"template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create instance: %d — %s", resp.StatusCode, b)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)
	return inst.ID
}

// The status route has to actually exist and decode what the instance reports. Curling it
// on the running server proves nothing: the auth middleware answers 401 ahead of routing,
// so an unregistered route is indistinguishable from a registered one there. Here a missing
// route would be a 404.
func TestSshxStatus_ReportsTheBinaryTheInstanceRuns(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	id := newRunningInstance(t, h, "sshx-status")

	// Scripted after creation so the setup commands are not intercepted too.
	const hash = "18e167a63ec08d34574f9d969b31c13daadf9fbefd18dc21844f2d9430704315"
	h.mock.runCommandFn = func(_ string, _ []string) (string, error) {
		return fmt.Sprintf("installed=1\nactive=active\nhash=%s\nversion=sshx 0.4.1\n", hash), nil
	}

	resp := h.do("GET", fmt.Sprintf("/api/v1/instances/%d/sshx", id), nil)
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		t.Fatal("GET /instances/{id}/sshx is not registered in the router")
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("sshx status: %d — %s", resp.StatusCode, b)
	}
	var got struct {
		Installed       bool   `json:"installed"`
		ServiceActive   bool   `json:"service_active"`
		Version         string `json:"version"`
		InstalledHash   string `json:"installed_hash"`
		AvailableHash   string `json:"available_hash"`
		UpdateAvailable bool   `json:"update_available"`
	}
	mustJSON(t, resp, &got)

	if !got.Installed || !got.ServiceActive {
		t.Errorf("installed=%v service_active=%v, want both true", got.Installed, got.ServiceActive)
	}
	if got.Version != "sshx 0.4.1" {
		t.Errorf("version = %q, want %q", got.Version, "sshx 0.4.1")
	}
	if got.InstalledHash != hash {
		t.Errorf("installed_hash = %q, want %q", got.InstalledHash, hash)
	}

	// This harness builds its TemplateService with no templates directory, so no sshx mixin
	// is loaded and the server ships nothing to compare against. update_available must stay
	// false: offering an update that would deploy nothing is worse than offering none.
	if got.AvailableHash != "" {
		t.Errorf("available_hash = %q, want empty when no mixin is loaded", got.AvailableHash)
	}
	if got.UpdateAvailable {
		t.Error("update_available is true although the server ships no sshx binary")
	}
}

// A binary that is not there at all is a state to report, not an error — it is the very
// case the update button repairs.
func TestSshxStatus_ReportsAMissingBinary(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	id := newRunningInstance(t, h, "sshx-missing")

	h.mock.runCommandFn = func(_ string, _ []string) (string, error) {
		return "installed=0\nactive=activating\n", nil
	}

	resp := h.do("GET", fmt.Sprintf("/api/v1/instances/%d/sshx", id), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("sshx status: %d — %s", resp.StatusCode, b)
	}
	var got struct {
		Installed     bool   `json:"installed"`
		ServiceActive bool   `json:"service_active"`
		InstalledHash string `json:"installed_hash"`
	}
	mustJSON(t, resp, &got)

	if got.Installed {
		t.Error("installed is true although the instance reported installed=0")
	}
	// "activating" is what a unit crash-looping on a missing binary reports, and it is not
	// "active": reading it as active would show a green light over a dead service.
	if got.ServiceActive {
		t.Error("service_active is true for a unit merely activating")
	}
	if got.InstalledHash != "" {
		t.Errorf("installed_hash = %q, want empty when nothing is installed", got.InstalledHash)
	}
}

// An exec that fails must not fail the whole request: the panel still needs to render, and
// "we could not look" is reported as "not installed", never as a 500.
func TestSshxStatus_SurvivesAFailingExec(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	id := newRunningInstance(t, h, "sshx-exec-fails")

	h.mock.runCommandFn = func(_ string, _ []string) (string, error) {
		return "", fmt.Errorf("exec refused")
	}

	resp := h.do("GET", fmt.Sprintf("/api/v1/instances/%d/sshx", id), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("a failing exec turned into %d — %s", resp.StatusCode, b)
	}
	var got struct {
		Installed bool `json:"installed"`
	}
	mustJSON(t, resp, &got)
	if got.Installed {
		t.Error("installed is true although the exec failed")
	}
}

// The update must refuse loudly when the server has no binary to deploy, rather than run
// the mixin's systemctl steps and report success over an unchanged instance.
func TestSshxUpdate_RefusesWhenTheMixinShipsNoBinary(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	id := newRunningInstance(t, h, "sshx-update")

	resp := h.do("POST", fmt.Sprintf("/api/v1/instances/%d/sshx/update", id), nil)
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		t.Fatal("POST /instances/{id}/sshx/update is not registered in the router")
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatalf("update reported success although no sshx binary is available: %s", body)
	}
	if !strings.Contains(string(body), "sshx") {
		t.Errorf("error body does not say what is missing: %s", body)
	}
}

// sshx used to run as root, so anyone with the session link got a root shell. The mixin now
// hands the unit to the template's terminal_user; the status must say who it runs as, so an
// instance created before that change can be spotted — and repaired by the same update.
func TestSshxStatus_ReportsTheUserTheServiceRunsAs(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	id := newRunningInstance(t, h, "sshx-user")

	for _, c := range []struct{ user, want string }{
		{"", "root"}, // no User= in the unit
		{"ubuntu", "ubuntu"},
	} {
		h.mock.runCommandFn = func(_ string, _ []string) (string, error) {
			return "installed=1\nactive=active\nuser=" + c.user + "\nhash=abc\n", nil
		}
		resp := h.do("GET", fmt.Sprintf("/api/v1/instances/%d/sshx", id), nil)
		var got struct {
			RunAs         string `json:"run_as"`
			ExpectedRunAs string `json:"expected_run_as"`
		}
		mustJSON(t, resp, &got)
		if got.RunAs != c.want {
			t.Errorf("unit User=%q: run_as = %q, want %q", c.user, got.RunAs, c.want)
		}
		// The seeded template's terminal_user.
		if got.ExpectedRunAs != "ubuntu" {
			t.Errorf("expected_run_as = %q, want ubuntu", got.ExpectedRunAs)
		}
	}
}
