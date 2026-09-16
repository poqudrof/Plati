package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
)

// sshxBinaryPath is where the sshx mixin installs the binary; the status check and the
// redeploy both address it.
const sshxBinaryPath = "/usr/local/bin/sshx"

// sshxURLPipeline prints the session URL of the most recent sshx start, read from the
// unit's journal. It takes the URL on the "Link:" line rather than matching a host: up to
// 0.4.1 sessions lived on https://sshx.io, but the in-house 0.5.x build defaults to its
// own server (https://jla-dev.burro-piranha.ts.net), and a hardcoded host silently
// reported "waiting for session URL" forever. It always exits 0.
const sshxURLPipeline = `journalctl -u sshx.service -n 100 --no-pager -o cat 2>/dev/null | ` + sshxLinkExtract

// sshxLinkExtract is the parsing half of sshxURLPipeline, kept apart so a test can feed it
// real journal output.
const sshxLinkExtract = `sed 's/\x1b\[[0-9;]*[a-zA-Z]//g' | sed -n 's/.*Link:[[:space:]]*\(https:\/\/[^[:space:]]*\).*/\1/p' | tail -1`

// SshxStatusResult compares the sshx binary running in an instance with the one the server
// currently ships, so the UI can offer an update. sshx is built in-house here, so a new
// build is published by replacing the file the mixin points at — nothing else.
type SshxStatusResult struct {
	Installed     bool `json:"installed"`
	ServiceActive bool `json:"service_active"`
	// Version is what `sshx --version` prints inside the instance. Display only: a locally
	// modified build usually keeps the same version string, so two different binaries can
	// report the same version. UpdateAvailable is decided on the hash, never on this.
	Version string `json:"version,omitempty"`
	// InstalledHash is the sha256 of the binary in the instance, AvailableHash that of the
	// one the mixin ships on the server.
	InstalledHash string `json:"installed_hash,omitempty"`
	AvailableHash string `json:"available_hash,omitempty"`
	// RunAs is the account the unit runs as ("root" when it sets no User=), ExpectedRunAs
	// the template's terminal_user the mixin now assigns it.
	RunAs         string `json:"run_as,omitempty"`
	ExpectedRunAs string `json:"expected_run_as,omitempty"`
	// UpdateAvailable is also true when the binary is missing entirely — that is the
	// instance whose mixin file push failed at creation, leaving the unit crash-looping on
	// a binary that was never there. For it, the same redeploy is the repair. And it is
	// true when the unit runs as the wrong user: instances created before the mixin handed
	// sshx to the terminal user still serve a root shell, and re-running the mixin fixes it.
	UpdateAvailable bool `json:"update_available"`
}

// sshxSourcePath returns the host path of the binary the sshx mixin ships. It is taken
// from the mixin rather than hardcoded, so whichever dist/ file the mixin points at is the
// one deployed, and the mixin stays the single definition of what sshx is.
func (s *InstanceService) sshxSourcePath() (string, error) {
	if s.templateSvc == nil {
		return "", fmt.Errorf("template service unavailable")
	}
	for _, st := range s.templateSvc.GetMixinFileSteps([]string{"sshx"}) {
		if st.FileDest == sshxBinaryPath {
			return st.FileSourcePath, nil
		}
	}
	return "", fmt.Errorf("the sshx mixin ships no %s", sshxBinaryPath)
}

// sshxAvailableHash hashes the shipped binary on every call instead of caching it at
// startup: the whole point is that a fresh build can be dropped in place and picked up
// without restarting the server, which is also how runSetupSteps reads it at deploy time.
func (s *InstanceService) sshxAvailableHash() (string, error) {
	path, err := s.sshxSourcePath()
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read sshx binary: %w", err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

// GetSshxStatus reports what sshx the instance is running and whether the server ships a
// different build.
func (s *InstanceService) GetSshxStatus(id int64, actor auth.Actor) (*SshxStatusResult, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance not running")
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}

	res := &SshxStatusResult{ExpectedRunAs: "root"}
	if tmpl, err := queries.GetTemplate(s.db, inst.TemplateID); err == nil && tmpl.TerminalUser != "" {
		res.ExpectedRunAs = tmpl.TerminalUser
	}
	if hash, err := s.sshxAvailableHash(); err == nil {
		res.AvailableHash = hash
	}

	// One exec for every fact, and `exit 0` throughout: a missing binary is a state to
	// report, not an error — it is exactly the case the update button exists to repair.
	script := fmt.Sprintf(`[ -x %[1]s ] && echo "installed=1" || echo "installed=0"
echo "active=$(systemctl is-active sshx 2>/dev/null)"
echo "user=$(systemctl show -p User --value sshx 2>/dev/null)"
if [ -x %[1]s ]; then
  echo "hash=$(sha256sum %[1]s 2>/dev/null | cut -d' ' -f1)"
  echo "version=$(%[1]s --version 2>/dev/null | head -1)"
fi
exit 0`, sshxBinaryPath)
	out, err := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c", script})
	if err != nil {
		return res, nil
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "installed":
			res.Installed = v == "1"
		case "active":
			res.ServiceActive = v == "active"
		case "hash":
			res.InstalledHash = v
		case "version":
			res.Version = v
		case "user":
			res.RunAs = v
		}
	}
	if res.RunAs == "" {
		res.RunAs = "root" // no User= in the unit: systemd runs a system service as root
	}
	res.UpdateAvailable = res.AvailableHash != "" &&
		(res.InstalledHash != res.AvailableHash || res.RunAs != res.ExpectedRunAs)
	return res, nil
}

// UpdateSshx redeploys the shipped sshx binary into a running instance and restarts the
// service, then reports the resulting state. It re-runs the sshx mixin rather than pushing
// the file by hand, so the unit file and the enable/start sequence stay defined in one
// place — the mixin.
func (s *InstanceService) UpdateSshx(id int64, actor auth.Actor) (*SshxStatusResult, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance not running")
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}
	if _, err := s.sshxSourcePath(); err != nil {
		return nil, err
	}

	terminalUser := ""
	if tmpl, err := queries.GetTemplate(s.db, inst.TemplateID); err == nil {
		terminalUser = tmpl.TerminalUser
	}

	steps := s.templateSvc.MixinSetupSteps([]string{"sshx"}, terminalUser)
	if len(steps) == 0 {
		return nil, fmt.Errorf("sshx mixin not loaded on this server")
	}
	// The mixin ends on `systemctl start sshx`, which does nothing when the service is
	// already running — the old binary would go on running under the new file, and the
	// update would look like it worked. Restart explicitly, and clear the failure counter
	// that a unit crash-looping on a missing binary will have piled up.
	steps = append(steps, incus.SetupStep{
		Label: "[sshx] restart service",
		Cmd:   []string{"/bin/sh", "-c", "systemctl reset-failed sshx 2>/dev/null; systemctl restart sshx"},
	})
	s.runSetupSteps(client, inst.IncusName, steps, nil)

	return s.GetSshxStatus(id, actor)
}
