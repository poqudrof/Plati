package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
)

// TransferOptions carries the two parts of a hand-over a caller may want to skip.
// Both zero values give the full hand-over — the container follows the new owner and
// the previous owner loses their SSH access — because that is what "give this
// workspace to someone else" means; keeping either has to be asked for.
type TransferOptions struct {
	// KeepContainerName leaves incus_name on the previous owner's prefix
	// (plati-{previous user}-{hostname}). Renaming costs a stop/start on a running
	// instance, which is the only reason to skip it — but then the previous owner
	// cannot reuse that workspace name: Create would derive the very same
	// incus_name and hit the (incus_name, server_id) unique constraint.
	KeepContainerName bool `json:"keep_container_name"`
	// KeepPreviousKeys leaves the previous owner's public keys in authorized_keys,
	// so they keep SSH access to a workspace that is no longer theirs.
	KeepPreviousKeys bool `json:"keep_previous_keys"`
}

// TransferResult reports the hand-over and how much of it reached the container.
type TransferResult struct {
	*models.Instance

	// PreviousUserID is the account the instance came from; PreviousName is set only
	// when the display name had to change to stay free in the target account.
	PreviousUserID int64  `json:"previous_user_id"`
	PreviousName   string `json:"previous_name,omitempty"`
	// NewOwnerEmail identifies the account it landed in, so the UI need not resolve it.
	NewOwnerEmail string `json:"new_owner_email"`

	// ContainerRenamed is true when incus_name now carries the new owner's id;
	// ContainerRenameError says why it does not, when the rename was attempted.
	ContainerRenamed     bool   `json:"container_renamed"`
	ContainerRenameError string `json:"container_rename_error,omitempty"`
	// Restarted is true when the instance was stopped and started again to let
	// Incus rename the container.
	Restarted bool `json:"restarted"`

	// KeysInstalled counts the new owner's public keys written to authorized_keys,
	// KeysRevoked the previous owner's keys removed from it. Both need the instance
	// running; Warnings says so when it was not.
	KeysInstalled int `json:"keys_installed"`
	KeysRevoked   int `json:"keys_revoked"`

	// Warnings holds what the hand-over could not do by itself — above all that the
	// previous owner's secrets are still inside the container until a rebuild.
	Warnings []string `json:"warnings,omitempty"`
}

// MarshalJSON emits the instance's fields plus the hand-over outcome. Instance
// defines MarshalJSON and embedding promotes it, so without this override every
// field above would silently vanish from the response — same trap as RenameResult
// and AdminInstance.
func (r TransferResult) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(r.Instance)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if err := json.Unmarshal(base, &fields); err != nil {
		return nil, err
	}
	fields["previous_user_id"] = r.PreviousUserID
	fields["new_owner_email"] = r.NewOwnerEmail
	fields["container_renamed"] = r.ContainerRenamed
	fields["restarted"] = r.Restarted
	fields["keys_installed"] = r.KeysInstalled
	fields["keys_revoked"] = r.KeysRevoked
	if r.PreviousName != "" {
		fields["previous_name"] = r.PreviousName
	}
	if r.ContainerRenameError != "" {
		fields["container_rename_error"] = r.ContainerRenameError
	}
	if len(r.Warnings) > 0 {
		fields["warnings"] = r.Warnings
	}
	return json.Marshal(fields)
}

// rebuildWarning is the one thing a transfer cannot fix from the outside: the
// container was provisioned with the previous owner's identity.
const rebuildWarning = "the previous owner's secrets and injected SSH key are still inside the workspace — rebuild it to provision the new owner's instead"

// TransferToUser moves an instance to another account. Admin-only path: ownership of
// the source is not checked, which is what lets an administrator set a workspace up
// and then hand it to the user it was meant for.
//
// What actually changes:
//
//   - the row's user_id, and its name when the target account already uses that one —
//     the tailnet hostname and incus_name are both derived from (owner, name);
//   - incus_name, to the new owner's prefix, unless opts.KeepContainerName;
//   - authorized_keys: the new owner's public keys in, the previous owner's out
//     (unless opts.KeepPreviousKeys), on a running instance;
//   - the tailnet hostname, when the name had to change.
//
// Everything past the DB write is best-effort and reported in the result: a stopped or
// unreachable instance still changes hands.
func (s *InstanceService) TransferToUser(id, targetUserID int64, opts TransferOptions) (*TransferResult, error) {
	inst, err := queries.GetInstance(s.db, id)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if targetUserID == 0 {
		return nil, fmt.Errorf("user_id is required: it names the account to transfer to")
	}
	target, err := queries.GetUserByID(s.db, targetUserID)
	if err != nil {
		return nil, fmt.Errorf("target user not found: %w", err)
	}
	if inst.UserID == targetUserID {
		return &TransferResult{
			Instance:       inst,
			PreviousUserID: inst.UserID,
			NewOwnerEmail:  target.Email,
		}, nil
	}

	previousUserID, previousName := inst.UserID, inst.Name
	newName := s.freeInstanceName(targetUserID, inst.Name)
	hostname := sanitizeName(newName)
	if hostname == "" {
		return nil, fmt.Errorf("invalid name %q: leaves no usable characters for a tailnet hostname", newName)
	}

	// Read the previous owner's keys while the row still points at them.
	var revoke []string
	if !opts.KeepPreviousKeys {
		revoke = s.keysToRevoke(previousUserID, targetUserID)
	}

	if err := queries.TransferInstance(s.db, id, targetUserID, newName); err != nil {
		return nil, fmt.Errorf("transfer instance: %w", err)
	}
	inst.UserID = targetUserID
	inst.Name = newName

	result := &TransferResult{
		Instance:       inst,
		PreviousUserID: previousUserID,
		NewOwnerEmail:  target.Email,
		Warnings:       []string{rebuildWarning},
	}
	if newName != previousName {
		result.PreviousName = previousName
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("renamed to %q: %q was already taken in the target account", newName, previousName))
	}

	client, err := s.clientForInstance(inst)
	if err != nil {
		log.Printf("transfer instance %d: %v", id, err)
		result.Warnings = append(result.Warnings,
			"the workspace changed hands in Plati only: its Incus server is unreachable")
		return result, nil
	}

	// Rename the container first: everything below addresses it by name.
	if !opts.KeepContainerName {
		restarted, err := s.renameContainer(inst, client, hostname)
		result.Restarted = restarted
		if err != nil {
			log.Printf("transfer instance %d: rename container: %v", id, err)
			result.ContainerRenameError = err.Error()
		} else {
			result.ContainerRenamed = true
		}
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"the container is still named %q: the previous owner cannot reuse the workspace name %q until it is renamed",
			inst.IncusName, previousName))
	}

	renamed := hostname != sanitizeName(previousName)
	if renamed {
		if err := client.UpdateInstanceConfig(inst.IncusName, map[string]string{
			"environment.PLATI_TAILSCALE_HOSTNAME": hostname,
		}); err != nil {
			log.Printf("transfer instance %d: update PLATI_TAILSCALE_HOSTNAME on %s: %v", id, inst.IncusName, err)
		}
	}

	if inst.Status != "running" {
		if len(revoke) > 0 {
			result.Warnings = append(result.Warnings,
				"the instance is stopped, so the previous owner's SSH keys are still in authorized_keys — a rebuild removes them")
		}
		return result, nil
	}

	// A container just restarted for the rename is up but not yet accepting exec.
	attempts := 1
	if result.Restarted {
		attempts = 5
	}

	if renamed {
		script := strings.ReplaceAll(tailnetHostnameScript, "{{H}}", hostname)
		if out, err := s.runWithRetry(client, inst.IncusName, script, attempts); err != nil {
			log.Printf("transfer instance %d: apply tailnet hostname on %s: %v (%s)", id, inst.IncusName, err, strings.TrimSpace(out))
		}
		attempts = 1 // the container answered, no need to wait again
	}

	terminalUser := ""
	if tmpl, err := queries.GetTemplate(s.db, inst.TemplateID); err == nil {
		terminalUser = tmpl.TerminalUser
	}

	newKeys, err := queries.ListSSHKeys(s.db, targetUserID)
	if err != nil {
		log.Printf("transfer instance %d: list new owner's keys: %v", id, err)
	}
	for _, k := range newKeys {
		script := authorizedKeyScript(strings.TrimSpace(k.PublicKey), terminalUser)
		if out, err := s.runWithRetry(client, inst.IncusName, script, attempts); err != nil {
			log.Printf("transfer instance %d: install key %q: %v (%s)", id, k.Name, err, strings.TrimSpace(out))
			continue
		}
		attempts = 1
		result.KeysInstalled++
	}
	if len(newKeys) == 0 {
		result.Warnings = append(result.Warnings,
			"the new owner has no public key registered, so nobody can SSH in yet — add one under Admin → Users → Keys, then install it from the SSH Access tab")
	}

	if len(revoke) > 0 {
		out, err := s.runWithRetry(client, inst.IncusName, revokeKeysScript(revoke, terminalUser), attempts)
		if err != nil {
			log.Printf("transfer instance %d: revoke previous owner's keys: %v (%s)", id, err, strings.TrimSpace(out))
			result.Warnings = append(result.Warnings,
				"could not remove the previous owner's SSH keys from the workspace")
		} else {
			result.KeysRevoked = len(revoke)
		}
	}

	return result, nil
}

// runWithRetry runs a script inside the instance, retrying while the container is
// still coming back up after a rename-induced restart. attempts of 1 is a single try.
func (s *InstanceService) runWithRetry(client incus.IncusClient, incusName, script string, attempts int) (string, error) {
	var out string
	var err error
	for attempt := 1; ; attempt++ {
		out, err = client.RunCommand(incusName, []string{"/bin/sh", "-c", script})
		if err == nil || attempt >= attempts {
			return out, err
		}
		time.Sleep(2 * time.Second)
	}
}

// keysToRevoke lists the key bodies of the previous owner that the new owner does not
// also hold — the keys that would otherwise leave the previous owner with SSH access
// to a workspace that is no longer theirs.
//
// A previous owner who is an administrator loses their key here like anyone else; the
// instance page's SSH Access tab offers every admin key back in one click.
func (s *InstanceService) keysToRevoke(previousUserID, targetUserID int64) []string {
	previous, err := queries.ListSSHKeys(s.db, previousUserID)
	if err != nil {
		log.Printf("transfer: list previous owner's keys: %v", err)
		return nil
	}
	keep := map[string]bool{}
	if targetKeys, err := queries.ListSSHKeys(s.db, targetUserID); err == nil {
		for _, k := range targetKeys {
			keep[keyBody(k.PublicKey)] = true
		}
	}
	out := []string{}
	for _, k := range previous {
		body := keyBody(k.PublicKey)
		if body == "" || keep[body] {
			continue
		}
		keep[body] = true // the same key registered twice is one line to drop
		out = append(out, body)
	}
	return out
}

// revokeKeysScript builds a /bin/sh script that drops the given keys from root's
// authorized_keys and, when it exists, the terminal user's. The bodies are passed
// base64-encoded so no shell quoting of their content is needed.
//
// The rewrite goes through `cat tmp > "$f"`, never `mv`: mv would hand the terminal
// user's authorized_keys to root and lock them out of their own account.
func revokeKeysScript(bodies []string, terminalUser string) string {
	var b strings.Builder
	b.WriteString(`drop_key() {
  f="$1"; b="$2"
  [ -f "$f" ] || return 0
  t="$f.plati-revoke"
  grep -vF "$b" "$f" > "$t" 2>/dev/null || : > "$t"
  cat "$t" > "$f"
  rm -f "$t"
}
files="/root/.ssh/authorized_keys"
`)
	if isSafeUsername(terminalUser) && terminalUser != "root" {
		fmt.Fprintf(&b, `if id %[1]s >/dev/null 2>&1; then
  home=$(getent passwd %[1]s | cut -d: -f6)
  [ -n "$home" ] || home=/home/%[1]s
  files="$files $home/.ssh/authorized_keys"
fi
`, terminalUser)
	}
	for _, body := range bodies {
		fmt.Fprintf(&b, "b=$(printf %%s '%s' | base64 -d); for f in $files; do drop_key \"$f\" \"$b\"; done\n",
			base64.StdEncoding.EncodeToString([]byte(body)))
	}
	return b.String()
}
