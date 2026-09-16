package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

// openVSCodePort is the port the openvscode-server mixin's unit listens on.
const openVSCodePort = 3463

// maxCustomLinks bounds the links noted on one instance.
const maxCustomLinks = 20

// CustomLink is a hand-entered link. Pinned ones are listed on the dashboard card; the
// others stay noted in the instance's Links tab.
type CustomLink struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	Pinned bool   `json:"pinned"`
}

// InstanceLinkSettings is the stored state behind the Links tab: which built-in links are
// pinned to the card, and the custom links.
type InstanceLinkSettings struct {
	Hostname   bool         `json:"hostname"`
	OpenVSCode bool         `json:"openvscode"`
	Sshx       bool         `json:"sshx"`
	Custom     []CustomLink `json:"custom"`
}

// customLinkInput decodes a custom link with Pinned optional: an entry that does not say
// is pinned. That covers both an API caller adding a link without the field and the rows
// migration 020 stored before pinning existed.
type customLinkInput struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	Pinned *bool  `json:"pinned"`
}

// UpdateLinksRequest is the PUT body. Every field is optional and an omitted one keeps
// its current value, like the sleep settings — so pinning one link is a one-field request.
// Custom, when present, replaces the whole list.
type UpdateLinksRequest struct {
	Hostname   *bool              `json:"hostname"`
	OpenVSCode *bool              `json:"openvscode"`
	Sshx       *bool              `json:"sshx"`
	Custom     *[]customLinkInput `json:"custom"`
}

// ResolvedLink is one available link. URL is empty when it cannot be opened right now —
// the instance is stopped, sshx has not printed its session URL yet, nothing serves 443 —
// and Note says why. Active reports the tool's systemd unit; it is always true for a
// custom link, which Plati cannot probe. Index is the position in settings.custom for a
// custom link, so the tab can edit the entry it shows; -1 otherwise.
type ResolvedLink struct {
	Kind   string `json:"kind"` // "hostname" | "openvscode" | "sshx" | "custom"
	Label  string `json:"label"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
	Pinned bool   `json:"pinned"`
	Index  int    `json:"index"`
	Note   string `json:"note,omitempty"`
}

type InstanceLinksResult struct {
	Settings InstanceLinkSettings `json:"settings"`
	// Links lists every link, pinned or not: the card keeps the pinned ones, the tab
	// shows them all.
	Links []ResolvedLink `json:"links"`
}

func decodeCustomLinks(raw string) []CustomLink {
	var in []customLinkInput
	if raw == "" || json.Unmarshal([]byte(raw), &in) != nil {
		return []CustomLink{}
	}
	out := make([]CustomLink, 0, len(in))
	for _, l := range in {
		out = append(out, CustomLink{Label: l.Label, URL: l.URL, Pinned: l.Pinned == nil || *l.Pinned})
	}
	return out
}

func linkSettingsOf(inst *models.Instance) InstanceLinkSettings {
	return InstanceLinkSettings{
		Hostname:   inst.LinkHostname,
		OpenVSCode: inst.LinkOpenVSCode,
		Sshx:       inst.LinkSshx,
		Custom:     decodeCustomLinks(inst.CustomLinks),
	}
}

// normalizeCustomLinks drops blank rows and rejects anything that is not an absolute
// http(s) URL: the card renders these as hrefs, so a javascript: URL must never get in.
func normalizeCustomLinks(in []customLinkInput) ([]CustomLink, error) {
	out := []CustomLink{}
	for _, l := range in {
		label := strings.TrimSpace(l.Label)
		raw := strings.TrimSpace(l.URL)
		if raw == "" && label == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("invalid link URL %q: must be an absolute http(s) URL", raw)
		}
		if label == "" {
			label = u.Host
		}
		if len(label) > 60 {
			return nil, fmt.Errorf("link label %q is longer than 60 characters", label)
		}
		out = append(out, CustomLink{Label: label, URL: u.String(), Pinned: l.Pinned == nil || *l.Pinned})
	}
	if len(out) > maxCustomLinks {
		return nil, fmt.Errorf("at most %d custom links", maxCustomLinks)
	}
	return out, nil
}

// UpdateLinks applies a partial update to the link settings and returns them resolved.
func (s *InstanceService) UpdateLinks(id int64, actor auth.Actor, req UpdateLinksRequest) (*InstanceLinksResult, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	set := linkSettingsOf(inst)
	if req.Hostname != nil {
		set.Hostname = *req.Hostname
	}
	if req.OpenVSCode != nil {
		set.OpenVSCode = *req.OpenVSCode
	}
	if req.Sshx != nil {
		set.Sshx = *req.Sshx
	}
	if req.Custom != nil {
		if set.Custom, err = normalizeCustomLinks(*req.Custom); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(set.Custom)
	if err != nil {
		return nil, err
	}
	// inst.UserID, not actor.UserID: the update filters on the owner, and an admin
	// editing someone's links would otherwise update zero rows and still succeed.
	if err := queries.UpdateInstanceLinks(s.db, inst.ID, inst.UserID,
		set.Hostname, set.OpenVSCode, set.Sshx, string(encoded)); err != nil {
		return nil, fmt.Errorf("save links: %w", err)
	}
	return s.GetLinks(id, actor)
}

// GetLinks returns the link settings and every link they resolve to, pinned or not. A
// running instance costs one exec, which reads everything the links need at once.
//
// The "hostname" link is https://<tailnet name>/ and is only openable when the instance
// answers on 443 — Tailscale Serve on 443, or a process listening on :443 inside the
// instance. Only the Tailscale name is used: the bare IP carries no certificate matching
// it, and is not reachable from outside the host anyway.
func (s *InstanceService) GetLinks(id int64, actor auth.Actor) (*InstanceLinksResult, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	set := linkSettingsOf(inst)
	res := &InstanceLinksResult{Settings: set}

	hostname := ResolvedLink{Kind: "hostname", Label: "Tailscale hostname (443)", Pinned: set.Hostname, Index: -1, Active: true}
	vscode := ResolvedLink{Kind: "openvscode", Label: "OpenVSCode", Pinned: set.OpenVSCode, Index: -1}
	sshx := ResolvedLink{Kind: "sshx", Label: "SSHX", Pinned: set.Sshx, Index: -1}

	if inst.Status != "running" {
		note := "instance " + inst.Status
		hostname.Note, vscode.Note, sshx.Note = note, note, note
	} else {
		probe := s.probeLinkTools(inst)
		dns := probe["dns"]

		switch {
		case dns == "":
			hostname.Note = "no tailnet name"
		case probe["serve443"] != "1" && probe["listen443"] != "1":
			hostname.Label = dns
			hostname.Note = "nothing serving on 443"
		default:
			hostname.Label = dns
			hostname.URL = "https://" + dns + "/"
		}

		vscode.Active = probe["vscode"] == "active"
		host := dns
		if host == "" && inst.IPAddress.Valid {
			host = inst.IPAddress.String
		}
		if host == "" {
			vscode.Note = "no address"
		} else {
			home := "/root"
			if tmpl, err := queries.GetTemplate(s.db, inst.TemplateID); err == nil && tmpl.TerminalUser != "" && tmpl.TerminalUser != "root" {
				home = "/home/" + tmpl.TerminalUser
			}
			vscode.URL = fmt.Sprintf("http://%s:%d/?folder=%s", host, openVSCodePort, url.QueryEscape(home))
			if !vscode.Active {
				vscode.Note = "service not running"
			}
		}

		sshx.Active = probe["sshx"] == "active"
		sshx.URL = probe["sshx_url"]
		switch {
		case !sshx.Active:
			sshx.Note = "service not running"
		case sshx.URL == "":
			sshx.Note = "waiting for session URL"
		}
	}

	res.Links = []ResolvedLink{hostname, vscode, sshx}
	for i, c := range set.Custom {
		res.Links = append(res.Links, ResolvedLink{Kind: "custom", Label: c.Label, URL: c.URL, Active: true, Pinned: c.Pinned, Index: i})
	}
	return res, nil
}

// probeLinkTools reads, in a single exec, the tailnet DNS name, whether anything serves
// 443, both units' state and the last sshx session URL. Failures leave the map empty: the card then shows the links as
// unavailable rather than failing the whole request.
func (s *InstanceService) probeLinkTools(inst *models.Instance) map[string]string {
	facts := map[string]string{}
	client, err := s.clientForInstance(inst)
	if err != nil {
		return facts
	}
	script := `echo "vscode=$(systemctl is-active openvscode-server 2>/dev/null)"
echo "sshx=$(systemctl is-active sshx 2>/dev/null)"
echo "dns=$(tailscale status --json 2>/dev/null | tr -d ' \n' | grep -o '"DNSName":"[^"]*"' | head -1 | cut -d'"' -f4 | sed 's/\.$//')"
tailscale serve status --json 2>/dev/null | tr -d ' \n' | grep -q '"443":{' && echo "serve443=1"
if command -v ss >/dev/null 2>&1; then
  [ -n "$(ss -Hltn 'sport = :443' 2>/dev/null)" ] && echo "listen443=1"
else
  grep -qiE ':01BB [0-9A-F]+:0000 0A ' /proc/net/tcp /proc/net/tcp6 2>/dev/null && echo "listen443=1"
fi
echo "sshx_url=$(journalctl -u sshx.service -n 100 --no-pager -o cat 2>/dev/null | sed 's/\x1b\[[0-9;]*[a-zA-Z]//g' | grep -oE 'https://sshx\.io/s/[^[:space:]]+' | tail -1)"
exit 0`
	out, err := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c", script})
	if err != nil {
		log.Printf("links probe %s: %v", inst.IncusName, err)
		return facts
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			facts[k] = strings.TrimSpace(v)
		}
	}
	return facts
}
