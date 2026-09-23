package services

import (
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
)

// InstanceAuthorizedKey is one registered public key a caller may install on an
// instance, flagged with whose it is and whether it is already in authorized_keys.
type InstanceAuthorizedKey struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	Present   bool   `json:"present"`
	// Owner identifies whose key this is: the instance owner's own keys (IsOwner),
	// every server administrator's — offered to anyone, so help can be asked for —
	// and, to an admin caller, every other account's as well.
	OwnerName  string `json:"owner_name"`
	OwnerEmail string `json:"owner_email,omitempty"`
	IsOwner    bool   `json:"is_owner"`
	IsAdmin    bool   `json:"is_admin"`
}

// clientForInstance resolves the Incus client handling the instance's server.
func (s *InstanceService) clientForInstance(inst *models.Instance) (incus.IncusClient, error) {
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}
	return client, nil
}

// keyBody returns the "<type> <base64>" part of an authorized_keys line, dropping
// the trailing comment so the same key with a different comment still matches.
func keyBody(publicKey string) string {
	fields := strings.Fields(strings.TrimSpace(publicKey))
	if len(fields) < 2 {
		return strings.TrimSpace(publicKey)
	}
	return fields[0] + " " + fields[1]
}

// installableKeys returns every key the caller may install on this instance. The
// instance owner's own keys come first, then:
//
//   - for anyone: the keys of every server administrator, so help can be asked for;
//   - for an admin caller: every other account's keys too, which is how an
//     administrator grants a colleague access to a workspace that is not theirs —
//     a hand-over in the making, or two people on one machine.
//
// A key is listed once: the owner's entry wins over the same key seen again below,
// so an admin looking at their own instance sees each of their keys once.
func (s *InstanceService) installableKeys(inst *models.Instance, actor auth.Actor) ([]InstanceAuthorizedKey, error) {
	ownerKeys, err := queries.ListSSHKeys(s.db, inst.UserID)
	if err != nil {
		return nil, fmt.Errorf("list public keys: %w", err)
	}
	ownerName := ""
	if u, err := queries.GetUserByID(s.db, inst.UserID); err == nil {
		ownerName = u.Name
	}

	out := make([]InstanceAuthorizedKey, 0, len(ownerKeys))
	seen := map[int64]bool{}
	for _, k := range ownerKeys {
		seen[k.ID] = true
		out = append(out, InstanceAuthorizedKey{
			ID: k.ID, Name: k.Name, PublicKey: k.PublicKey, OwnerName: ownerName,
			IsOwner: true,
		})
	}

	// An admin sees the whole platform's keys; everyone else only the administrators'.
	others, err := queries.ListAdminSSHKeys(s.db)
	if actor.Admin {
		others, err = queries.ListAllSSHKeysWithOwner(s.db)
	}
	if err != nil {
		log.Printf("warning: list other users' public keys: %v", err)
		return out, nil
	}
	for _, k := range others {
		if seen[k.ID] {
			continue
		}
		seen[k.ID] = true
		out = append(out, InstanceAuthorizedKey{
			ID: k.ID, Name: k.Name, PublicKey: k.PublicKey,
			OwnerName: k.UserName, OwnerEmail: k.UserEmail,
			IsAdmin: k.UserRole == "admin",
		})
	}
	return out, nil
}

// ListAuthorizedKeys returns the keys installable on this instance — see
// installableKeys for whose, which depends on the caller's role — and whether each one
// is already in authorized_keys. Presence is only checked while the instance is running.
func (s *InstanceService) ListAuthorizedKeys(instanceID int64, actor auth.Actor) ([]InstanceAuthorizedKey, error) {
	inst, err := queries.GetInstanceForActor(s.db, instanceID, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	keys, err := s.installableKeys(inst, actor)
	if err != nil {
		return nil, err
	}

	installed := ""
	if inst.Status == "running" {
		if client, err := s.clientForInstance(inst); err == nil {
			installed, _ = client.RunCommand(inst.IncusName,
				[]string{"/bin/sh", "-c", "cat /root/.ssh/authorized_keys 2>/dev/null"})
		}
	}

	for i := range keys {
		keys[i].Present = installed != "" && strings.Contains(installed, keyBody(keys[i].PublicKey))
	}
	return keys, nil
}

// AddAuthorizedKey appends one of the installable public keys to the running instance's
// authorized_keys (root, plus the template's terminal user when it exists).
//
// Which keys those are comes from installableKeys with the same actor, so the role check
// is the listing itself: a regular user can only install the owner's or an
// administrator's key, while an admin can install any account's. A key that is not on
// the caller's list is simply not found.
//
// The write is idempotent: a key already present is left untouched.
func (s *InstanceService) AddAuthorizedKey(instanceID int64, actor auth.Actor, keyID int64) error {
	inst, err := queries.GetInstanceForActor(s.db, instanceID, actor)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return fmt.Errorf("instance is not running")
	}

	keys, err := s.installableKeys(inst, actor)
	if err != nil {
		return err
	}
	var publicKey string
	for _, k := range keys {
		if k.ID == keyID {
			publicKey = strings.TrimSpace(k.PublicKey)
			break
		}
	}
	if publicKey == "" {
		return fmt.Errorf("public key not found among the ones you may install on this instance")
	}

	client, err := s.clientForInstance(inst)
	if err != nil {
		return err
	}

	terminalUser := ""
	if tmpl, err := queries.GetTemplate(s.db, inst.TemplateID); err == nil {
		terminalUser = tmpl.TerminalUser
	}

	out, err := client.RunCommand(inst.IncusName,
		[]string{"/bin/sh", "-c", authorizedKeyScript(publicKey, terminalUser)})
	if err != nil {
		return fmt.Errorf("install key: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// isSafeUsername guards the terminal user before it is interpolated into a shell script.
func isSafeUsername(name string) bool {
	if name == "" || len(name) > 32 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// authorizedKeyScript builds an idempotent /bin/sh script that appends the key to
// root's authorized_keys and, when present, the terminal user's. The key is passed
// base64-encoded so no shell quoting of its content is needed.
func authorizedKeyScript(publicKey, terminalUser string) string {
	encKey := base64.StdEncoding.EncodeToString([]byte(publicKey))
	encBody := base64.StdEncoding.EncodeToString([]byte(keyBody(publicKey)))

	var b strings.Builder
	fmt.Fprintf(&b, "k=$(printf %%s '%s' | base64 -d); b=$(printf %%s '%s' | base64 -d)\n", encKey, encBody)
	b.WriteString(`add_key() {
  dir="$1"
  mkdir -p "$dir" && chmod 700 "$dir"
  f="$dir/authorized_keys"
  [ -f "$f" ] || : > "$f"
  chmod 600 "$f"
  if grep -qF "$b" "$f"; then return 0; fi
  if [ -s "$f" ] && [ "$(tail -c1 "$f" | wc -l)" -eq 0 ]; then printf '\n' >> "$f"; fi
  printf '%s\n' "$k" >> "$f"
}
add_key /root/.ssh
`)
	if isSafeUsername(terminalUser) && terminalUser != "root" {
		// The home may be a freshly mounted volume owned by root:root; creating
		// ~/.ssh under it must not leave the user locked out of their own home.
		fmt.Fprintf(&b, `if id %[1]s >/dev/null 2>&1; then
  home=$(getent passwd %[1]s | cut -d: -f6)
  [ -n "$home" ] || home=/home/%[1]s
  mkdir -p "$home"
  if [ -n "$(find "$home" -maxdepth 0 -user root)" ]; then
    chown %[1]s:%[1]s "$home"
    chmod 750 "$home"
  fi
  add_key "$home/.ssh"
  chown -R %[1]s:%[1]s "$home/.ssh"
fi
`, terminalUser)
	}
	return b.String()
}
