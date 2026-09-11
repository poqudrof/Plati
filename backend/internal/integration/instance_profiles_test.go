// Regression tests for the two failures that left instance "sentinel-care" unusable:
// a template requiring an Incus profile the server does not have, and a Rebuild that
// could not repair the instance the first failure left behind.
package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homaserver/plati/internal/models"
)

// seedTemplateWithProfiles imports a template declaring an arbitrary profile list.
// seedTemplate is hardcoded to ["default"], which is exactly the case that never broke.
func seedTemplateWithProfiles(t *testing.T, h *harness, slug string, profiles []string) int64 {
	t.Helper()

	quoted := make([]string, len(profiles))
	for i, p := range profiles {
		quoted[i] = `"` + p + `"`
	}
	body := strings.NewReader(fmt.Sprintf(`{
		"name": %q,
		"slug": %q,
		"description": "profile test",
		"image": "images:ubuntu/24.04/cloud",
		"profiles": [%s],
		"resources": {"cpu": "2", "memory": "4GB", "disk": "20GB"},
		"terminal_user": "ubuntu"
	}`, slug, slug, strings.Join(quoted, ",")))

	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/admin/templates/import", body)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range h.client.Jar.Cookies(req.URL) {
		req.AddCookie(c)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("import template: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("import template: %d — %s", resp.StatusCode, b)
	}
	var tmpl models.Template
	mustJSON(t, resp, &tmpl)
	return tmpl.ID
}

// The mock server has only the "default" profile, like the real one. Creating from a
// template that also wants "docker" must fail synchronously, with a message naming the
// profile — and must not leave a row behind. Before the fix the request returned 201 and
// the instance flipped to `error` seconds later, with the reason only in the creation log.
func TestCreateInstance_MissingProfileFailsFastWithoutOrphanRow(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplateWithProfiles(t, h, "needs-docker-profile", []string{"default", "docker"})

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name":        "sentinel-care",
		"template_id": templateID,
	})
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		t.Fatalf("create accepted a template requiring a profile the server lacks (got 201: %s)", body)
	}
	if !strings.Contains(string(body), "docker") {
		t.Errorf("error body does not name the missing profile: %s", body)
	}

	// Nothing may be left behind: no Incus container, and above all no DB row for the
	// user to find stuck in `error`.
	if len(h.mock.instances) != 0 {
		t.Errorf("an Incus container was created despite the missing profile: %v", h.mock.instances)
	}

	resp = h.do("GET", "/api/v1/instances", nil)
	var instances []models.InstanceJSON
	mustJSON(t, resp, &instances)
	if len(instances) != 0 {
		t.Errorf("a failed creation left %d instance row(s) behind: %+v", len(instances), instances)
	}
}

// An instance whose container is gone — what a half-failed creation leaves behind — must
// be repairable. Rebuild used to delete first and bail out on "not found", so the only
// operation meant to repair an instance was the one that could not repair this one.
func TestRebuild_RepairsInstanceWithNoContainer(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "sentinel-care", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: %d — %s", resp.StatusCode, b)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)

	// Let the async creation finish so the container exists before we remove it.
	for i := 0; i < 100; i++ {
		var cur models.InstanceJSON
		mustJSON(t, h.do("GET", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil), &cur)
		if cur.Status == "running" || cur.Status == "error" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Reproduce the broken state: the row survives, the container does not.
	delete(h.mock.instances, inst.IncusName)
	if _, err := h.db.Exec(`UPDATE instances SET status = 'error' WHERE id = ?`, inst.ID); err != nil {
		t.Fatalf("mark instance as failed: %v", err)
	}

	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/rebuild", inst.ID), nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rebuild could not repair an instance with no container: %d — %s", resp.StatusCode, body)
	}

	if _, ok := h.mock.instances[inst.IncusName]; !ok {
		t.Errorf("rebuild returned OK but did not recreate container %q", inst.IncusName)
	}

	// The workspace volume predates the failed container and must survive the repair.
	volName := inst.IncusName + "-home-ubuntu"
	if _, ok := h.mock.volumes[volName]; !ok {
		t.Errorf("persistent volume %q was lost during the repair", volName)
	}
}

// The half-failed creation also leaves the instance with no volume rows. Rebuild only
// ever reattached what the join table listed, so repairing such an instance used to hand
// back a container whose /home lived in the rootfs — wiped, silently, by the next rebuild.
func TestRebuild_ProvisionsVolumesWhenNoneRecorded(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "sentinel-care", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: %d — %s", resp.StatusCode, b)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)

	for i := 0; i < 100; i++ {
		var cur models.InstanceJSON
		mustJSON(t, h.do("GET", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil), &cur)
		if cur.Status == "running" || cur.Status == "error" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Reproduce what a creation that died before its volume step leaves behind: a row,
	// no container, and no volume anywhere.
	volName := inst.IncusName + "-home-ubuntu"
	delete(h.mock.instances, inst.IncusName)
	delete(h.mock.volumes, volName)
	if _, err := h.db.Exec(`DELETE FROM instance_volumes WHERE instance_id = ?`, inst.ID); err != nil {
		t.Fatalf("clear instance_volumes: %v", err)
	}
	if _, err := h.db.Exec(`DELETE FROM volumes`); err != nil {
		t.Fatalf("clear volumes: %v", err)
	}

	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/rebuild", inst.ID), nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rebuild: %d — %s", resp.StatusCode, body)
	}

	if _, ok := h.mock.volumes[volName]; !ok {
		t.Errorf("rebuild did not provision the missing persistent volume %q; the repaired instance would lose its home at the next rebuild", volName)
	}

	// And it must be recorded, or the *next* rebuild would detach nothing and lose it.
	var n int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM instance_volumes WHERE instance_id = ?`, inst.ID).Scan(&n); err != nil {
		t.Fatalf("count instance_volumes: %v", err)
	}
	if n == 0 {
		t.Error("volume was created but not recorded in instance_volumes")
	}
}
