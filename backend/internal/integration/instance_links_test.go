// Tests for instance links: the built-in ones (tailnet hostname, OpenVSCode, SSHX) resolve
// to live URLs read from inside the instance, custom links are noted as typed, and every
// link carries a pinned flag deciding whether the dashboard card lists it. Only http(s) is
// accepted since links are rendered as hrefs.
package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"testing"
)

type resolvedLink struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
	Pinned bool   `json:"pinned"`
	Index  int    `json:"index"`
	Note   string `json:"note"`
}

type linksResponse struct {
	Settings struct {
		Hostname   bool `json:"hostname"`
		OpenVSCode bool `json:"openvscode"`
		Sshx       bool `json:"sshx"`
		Custom     []struct {
			Label  string `json:"label"`
			URL    string `json:"url"`
			Pinned bool   `json:"pinned"`
		} `json:"custom"`
	} `json:"settings"`
	Links []resolvedLink `json:"links"`
}

func (r linksResponse) byKind(kind string) []resolvedLink {
	var out []resolvedLink
	for _, l := range r.Links {
		if l.Kind == kind {
			out = append(out, l)
		}
	}
	return out
}

func getLinks(t *testing.T, h *harness, id int64) linksResponse {
	t.Helper()
	resp := h.do("GET", fmt.Sprintf("/api/v1/instances/%d/links", id), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("get links: %d — %s", resp.StatusCode, b)
	}
	var got linksResponse
	mustJSON(t, resp, &got)
	return got
}

func putLinks(t *testing.T, h *harness, id int64, body map[string]any) linksResponse {
	t.Helper()
	resp := h.do("PUT", fmt.Sprintf("/api/v1/instances/%d/links", id), body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("put links: %d — %s", resp.StatusCode, b)
	}
	var got linksResponse
	mustJSON(t, resp, &got)
	return got
}

func TestLinks_ResolveBuiltinsAndNoteCustomOnes(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	id := newRunningInstance(t, h, "links")

	// A fresh instance pins only the hostname link — which the card shows only once
	// something serves 443 — so the card does not change until the user opts in.
	fresh := getLinks(t, h, id)
	if !fresh.Settings.Hostname || fresh.Settings.OpenVSCode || fresh.Settings.Sshx || len(fresh.Settings.Custom) != 0 {
		t.Fatalf("fresh settings = %+v", fresh.Settings)
	}

	h.mock.runCommandFn = func(_ string, _ []string) (string, error) {
		return "vscode=active\nsshx=active\ndns=links.tail1234.ts.net\nserve443=1\nsshx_url=https://sshx.io/s/abc#key\n", nil
	}

	got := putLinks(t, h, id, map[string]any{
		"openvscode": true,
		"custom": []map[string]any{
			{"label": "App", "url": "https://links.tail1234.ts.net:8080/"},        // no pinned field: pinned
			{"label": "Docs", "url": "https://example.com/docs", "pinned": false}, // noted only
			{"label": "", "url": ""}, // blank row dropped
		},
	})

	if l := got.byKind("hostname"); len(l) != 1 || l[0].URL != "https://links.tail1234.ts.net/" || !l[0].Pinned {
		t.Errorf("hostname link = %+v", l)
	}
	if l := got.byKind("openvscode"); len(l) != 1 || !l[0].Pinned || !l[0].Active ||
		l[0].URL != "http://links.tail1234.ts.net:3463/?folder=%2Fhome%2Fubuntu" {
		t.Errorf("openvscode link = %+v", l)
	}
	// Not pinned, yet still listed with its URL: the Links tab needs it to offer the pin.
	if l := got.byKind("sshx"); len(l) != 1 || l[0].Pinned || l[0].URL != "https://sshx.io/s/abc#key" {
		t.Errorf("sshx link = %+v", l)
	}
	custom := got.byKind("custom")
	if len(custom) != 2 {
		t.Fatalf("custom links = %+v, want 2", custom)
	}
	if custom[0].Label != "App" || !custom[0].Pinned || custom[0].Index != 0 {
		t.Errorf("custom[0] = %+v", custom[0])
	}
	if custom[1].Label != "Docs" || custom[1].Pinned || custom[1].Index != 1 {
		t.Errorf("custom[1] = %+v", custom[1])
	}

	// A partial update touches only what it names: unpinning the hostname keeps the
	// custom list and the OpenVSCode pin.
	got = putLinks(t, h, id, map[string]any{"hostname": false})
	if got.Settings.Hostname || !got.Settings.OpenVSCode || len(got.Settings.Custom) != 2 {
		t.Errorf("after partial update: %+v", got.Settings)
	}

	// Persisted, not just echoed back.
	again := getLinks(t, h, id)
	if again.Settings.Hostname || !again.Settings.OpenVSCode || len(again.Settings.Custom) != 2 ||
		again.Settings.Custom[1].Pinned {
		t.Errorf("settings not persisted: %+v", again.Settings)
	}
}

func TestLinks_RejectNonHTTPURLs(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	id := newRunningInstance(t, h, "links-bad")

	for _, raw := range []string{"javascript:alert(1)", "example.com", "ftp://example.com"} {
		resp := h.do("PUT", fmt.Sprintf("/api/v1/instances/%d/links", id), map[string]any{
			"custom": []map[string]string{{"label": "x", "url": raw}},
		})
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("url %q: status %d, want 400", raw, resp.StatusCode)
		}
	}
}

// Before an sshx session exists, the link is listed without a URL and says why, instead
// of vanishing or pointing nowhere.
func TestLinks_SshxWithoutSessionYet(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	id := newRunningInstance(t, h, "links-wait")
	h.mock.runCommandFn = func(_ string, _ []string) (string, error) {
		return "vscode=inactive\nsshx=active\ndns=\nsshx_url=\n", nil
	}

	got := putLinks(t, h, id, map[string]any{"sshx": true})
	if l := got.byKind("sshx"); len(l) != 1 || l[0].URL != "" || l[0].Note == "" || !l[0].Pinned {
		t.Errorf("sshx link without session = %+v", l)
	}
}

// The 443 link needs the tailnet name and something serving: a listener with no Tailscale
// DNS name yields no URL, never one built on the bare IP.
func TestLinks_HostnameNeedsTheTailnetName(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	id := newRunningInstance(t, h, "links-443")

	h.mock.runCommandFn = func(_ string, _ []string) (string, error) {
		return "dns=\nlisten443=1\n", nil
	}
	if l := getLinks(t, h, id).byKind("hostname"); len(l) != 1 || l[0].URL != "" || l[0].Note == "" {
		t.Errorf("hostname link without a tailnet name = %+v", l)
	}

	h.mock.runCommandFn = func(_ string, _ []string) (string, error) {
		return "dns=links-443.tail1234.ts.net\n", nil
	}
	if l := getLinks(t, h, id).byKind("hostname"); len(l) != 1 || l[0].URL != "" {
		t.Errorf("hostname link with nothing on 443 = %+v", l)
	}

	h.mock.runCommandFn = func(_ string, _ []string) (string, error) {
		return "dns=links-443.tail1234.ts.net\nlisten443=1\n", nil
	}
	if l := getLinks(t, h, id).byKind("hostname"); len(l) != 1 || l[0].URL != "https://links-443.tail1234.ts.net/" {
		t.Errorf("hostname link with a listener on 443 = %+v", l)
	}
}
