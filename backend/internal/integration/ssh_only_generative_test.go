package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/homaserver/plati/internal/models"
)

// TestSSHOnlyUsers_Generative drives the SSH-only feature with random sequences of
// operations — create either kind of account, switch SSH-only on and off, change role
// and password, log in, reuse old sessions and API keys, add and remove public keys —
// and after every step compares the API and the database with a small reference model.
//
// Each run prints its seed. To replay a failure:
//
//	PLATI_GEN_SEED=<seed> go test ./internal/integration -run Generative -v
//
// PLATI_GEN_RUNS and PLATI_GEN_STEPS widen the search (defaults: 6 runs of 60 steps).
func TestSSHOnlyUsers_Generative(t *testing.T) {
	runs, steps := envInt("PLATI_GEN_RUNS", 6), envInt("PLATI_GEN_STEPS", 60)
	base := uint64(time.Now().UnixNano())
	if s := os.Getenv("PLATI_GEN_SEED"); s != "" {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			t.Fatalf("PLATI_GEN_SEED: %v", err)
		}
		base, runs = v, 1
	}
	if testing.Short() {
		runs = 1
	}
	for i := 0; i < runs; i++ {
		seed := base + uint64(i)
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			newGenRun(t, seed).run(steps)
		})
	}
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

// genUser is the model of one account: what the API and the DB must agree with.
type genUser struct {
	id        int64
	email     string
	name      string
	role      string
	sshOnly   bool
	password  string   // "" = no password
	former    []string // passwords it used to have; none of them may log in any more
	keys      []models.SSHKey
	apiKeys   []string // live keys: work exactly while the account is not SSH-only
	revoked   []string // keys deleted by a switch to SSH-only: refused forever
	switchedN int      // how many times it was switched to SSH-only, for the trace
}

// genSession is a cookie session opened by a successful login. Once its account has
// been switched to SSH-only it is dead for good — switching back must not revive it.
type genSession struct {
	user   *genUser
	client *http.Client
	alive  bool
}

type genRun struct {
	t        *testing.T
	h        *harness
	rng      *rand.Rand
	seed     uint64
	users    []*genUser
	sessions []*genSession
	instID   int64
	emailN   int
	trace    []string
}

func newGenRun(t *testing.T, seed uint64) *genRun {
	h := newHarness(t)
	t.Cleanup(h.teardown)
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// An instance of the admin's, whose SSH Access tab must offer every account's keys.
	templateID := seedTemplate(t, h)
	resp := h.do("POST", "/api/v1/instances", map[string]any{"name": "gen", "template_id": templateID})
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)

	g := &genRun{t: t, h: h, rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), seed: seed, instID: inst.ID}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("replay with PLATI_GEN_SEED=%d — trace:\n  %s", seed, strings.Join(g.trace, "\n  "))
		}
	})
	return g
}

func (g *genRun) run(steps int) {
	type op struct {
		weight int
		fn     func()
	}
	ops := []op{
		{4, g.createPlati},
		{4, g.createSSHOnly},
		{10, g.update},
		{6, g.login},
		{3, g.mintAPIKey},
		{3, g.addPublicKey},
		{2, g.deletePublicKey},
	}
	total := 0
	for _, o := range ops {
		total += o.weight
	}
	for i := 0; i < steps; i++ {
		n := g.rng.IntN(total)
		for _, o := range ops {
			if n < o.weight {
				o.fn()
				break
			}
			n -= o.weight
		}
		g.checkInvariants(i)
		if g.t.Failed() {
			return
		}
	}
}

func (g *genRun) log(format string, args ...any) {
	g.trace = append(g.trace, fmt.Sprintf(format, args...))
}

func (g *genRun) fail(format string, args ...any) {
	g.t.Helper()
	g.t.Fatalf("[seed %d, step %d] %s", g.seed, len(g.trace), fmt.Sprintf(format, args...))
}

// --- generators ---------------------------------------------------------------------

func (g *genRun) word(min, max int) string {
	const letters = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, min+g.rng.IntN(max-min+1))
	for i := range b {
		b[i] = letters[g.rng.IntN(len(letters))]
	}
	return string(b)
}

func (g *genRun) validPassword() string { return g.word(8, 16) }
func (g *genRun) shortPassword() string { return g.word(1, 7) }

// newEmail is usually fresh, sometimes an address that is already taken.
func (g *genRun) newEmail() (email string, taken bool) {
	if len(g.users) > 0 && g.rng.IntN(8) == 0 {
		return g.pickUser().email, true
	}
	g.emailN++
	return fmt.Sprintf("u%d@gen.example", g.emailN), false
}

func (g *genRun) pickUser() *genUser { return g.users[g.rng.IntN(len(g.users))] }

func (g *genRun) role() string {
	if g.rng.IntN(3) == 0 {
		return "admin"
	}
	return "user"
}

// --- operations ---------------------------------------------------------------------

func (g *genRun) createPlati() {
	email, taken := g.newEmail()
	name, role := g.word(3, 8), g.role()
	password, short := g.validPassword(), false
	if g.rng.IntN(5) == 0 {
		password, short = g.shortPassword(), true
	}
	g.log("create Plati account %s role=%s password=%q", email, role, password)

	resp := g.h.do("POST", "/api/v1/admin/users", map[string]any{
		"email": email, "name": name, "password": password, "role": role,
	})
	switch {
	case short:
		g.expectStatus(resp, http.StatusBadRequest, "password shorter than 8 at creation")
	case taken:
		g.expectRefused(resp, "email already taken")
	default:
		var u models.User
		g.expectCreated(resp, &u)
		g.users = append(g.users, &genUser{id: u.ID, email: email, name: name, role: role, password: password})
	}
}

func (g *genRun) createSSHOnly() {
	email, taken := g.newEmail()
	name := g.word(3, 8)
	key, keyName, badKey := "", "", false
	switch g.rng.IntN(4) {
	case 0: // no key: added later
	case 1:
		key, badKey = "ssh-ed25519 "+g.word(10, 30), true
	default:
		key, keyName = newPublicKey(g.t, g.word(3, 8)), g.word(3, 8)
	}
	g.log("create SSH-only account %s key=%t badKey=%t", email, key != "", badKey)

	resp := g.h.do("POST", "/api/v1/admin/users", map[string]any{
		"email": email, "name": name, "ssh_only": true, "public_key": key, "key_name": keyName,
	})
	switch {
	case badKey:
		g.expectStatus(resp, http.StatusBadRequest, "malformed key at creation")
		return
	case taken:
		g.expectRefused(resp, "email already taken")
		return
	}
	var u models.User
	g.expectCreated(resp, &u)
	if !u.SSHOnly || u.Role != "user" {
		g.fail("SSH-only creation returned %+v", u)
	}
	nu := &genUser{id: u.ID, email: email, name: name, role: "user", sshOnly: true}
	if key != "" {
		// The key given at creation is the account's only one; pick its id up.
		nu.keys = g.listPublicKeys(nu)
		if len(nu.keys) != 1 {
			g.fail("expected the creation key on %s, got %d keys", email, len(nu.keys))
		}
	}
	g.users = append(g.users, nu)
}

func (g *genRun) update() {
	if len(g.users) == 0 {
		return
	}
	u := g.pickUser()
	name, role := g.word(3, 8), g.role()
	body := map[string]any{"name": name, "role": role}

	var sshOnly *bool
	switch g.rng.IntN(4) {
	case 0:
		v := true
		sshOnly = &v
	case 1, 2: // the interesting direction: back to a Plati account
		v := false
		sshOnly = &v
	}
	if sshOnly != nil {
		body["ssh_only"] = *sshOnly
	}

	password := ""
	switch g.rng.IntN(3) {
	case 0:
		password = g.validPassword()
	case 1:
		if g.rng.IntN(3) == 0 {
			password = g.shortPassword()
		}
	}
	body["password"] = password
	g.log("update %s (ssh_only=%t) ← ssh_only=%s role=%s password=%q",
		u.email, u.sshOnly, fmtBoolPtr(sshOnly), role, password)

	resp := g.h.do("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.id), body)

	effective := u.sshOnly
	if sshOnly != nil {
		effective = *sshOnly
	}
	if effective && (password != "" || role == "admin") {
		g.expectStatus(resp, http.StatusBadRequest, "SSH-only account given a password or the admin role")
		return
	}
	if password != "" && len(password) < 8 {
		g.expectStatus(resp, http.StatusBadRequest, "password shorter than 8")
		return
	}
	g.expectStatus(resp, http.StatusOK, "valid update")

	u.name, u.role = name, role
	if sshOnly != nil {
		if *sshOnly {
			if u.password != "" {
				u.former = append(u.former, u.password)
			}
			u.sshOnly, u.password, u.role = true, "", "user"
			u.switchedN++
			// Every switch deletes the account's API keys, even one onto an account
			// that already was SSH-only.
			u.revoked = append(u.revoked, u.apiKeys...)
			u.apiKeys = nil
			for _, s := range g.sessions {
				if s.user == u {
					s.alive = false
				}
			}
		} else {
			u.sshOnly = false
		}
	}
	if password != "" {
		if u.password != "" {
			u.former = append(u.former, u.password)
		}
		u.password = password
	}
}

func (g *genRun) login() {
	if len(g.users) == 0 {
		return
	}
	u := g.pickUser()
	attempt, kind := u.password, "current"
	switch g.rng.IntN(4) {
	case 0:
		attempt, kind = g.validPassword(), "wrong"
	case 1:
		if len(u.former) > 0 {
			attempt, kind = u.former[g.rng.IntN(len(u.former))], "former"
		}
	}
	if attempt == "" { // no password at all: try the empty one, and a former one if any
		kind = "empty"
	}
	g.log("login %s with %s password", u.email, kind)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"email": u.email, "password": attempt})
	resp, err := client.Post(g.h.srv.URL+"/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		g.fail("login: %v", err)
	}
	resp.Body.Close()

	shouldWork := !u.sshOnly && u.password != "" && attempt == u.password
	if (resp.StatusCode == http.StatusOK) != shouldWork {
		g.fail("login %s (ssh_only=%t, has password=%t, %s password): got %d, expected success=%t",
			u.email, u.sshOnly, u.password != "", kind, resp.StatusCode, shouldWork)
	}
	if shouldWork {
		g.sessions = append(g.sessions, &genSession{user: u, client: client, alive: true})
	}
}

func (g *genRun) mintAPIKey() {
	if len(g.users) == 0 {
		return
	}
	u := g.pickUser()
	g.log("mint API key for %s (ssh_only=%t)", u.email, u.sshOnly)
	resp := g.h.do("POST", fmt.Sprintf("/api/v1/admin/users/%d/api-keys", u.id), map[string]string{"name": g.word(3, 8)})
	var minted struct {
		Key string `json:"key"`
	}
	g.expectCreated(resp, &minted)
	if minted.Key == "" {
		g.fail("minted API key is empty")
	}
	u.apiKeys = append(u.apiKeys, minted.Key)
}

// apiKeyStatus is the status an authenticated route answers to a bearer API key.
func (g *genRun) apiKeyStatus(key string) int {
	req, _ := http.NewRequest("GET", g.h.srv.URL+"/api/v1/instances", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		g.fail("API key request: %v", err)
	}
	r.Body.Close()
	return r.StatusCode
}

func hasPrefixIn(keys []string, prefix string) bool {
	for _, k := range keys {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func (g *genRun) addPublicKey() {
	if len(g.users) == 0 {
		return
	}
	u := g.pickUser()
	bad := g.rng.IntN(5) == 0
	key := newPublicKey(g.t, g.word(3, 8))
	if bad {
		key = "not a key " + g.word(4, 10)
	}
	g.log("add public key to %s (bad=%t)", u.email, bad)
	resp := g.h.do("POST", fmt.Sprintf("/api/v1/admin/users/%d/public-keys", u.id),
		map[string]string{"name": g.word(3, 8), "public_key": key})
	if bad {
		g.expectStatus(resp, http.StatusBadRequest, "malformed public key")
		return
	}
	var k models.SSHKey
	g.expectCreated(resp, &k)
	u.keys = append(u.keys, k)
}

func (g *genRun) deletePublicKey() {
	var owners []*genUser
	for _, u := range g.users {
		if len(u.keys) > 0 {
			owners = append(owners, u)
		}
	}
	if len(owners) == 0 {
		return
	}
	u := owners[g.rng.IntN(len(owners))]
	i := g.rng.IntN(len(u.keys))
	g.log("delete public key %d of %s", u.keys[i].ID, u.email)
	resp := g.h.do("DELETE", fmt.Sprintf("/api/v1/admin/users/%d/public-keys/%d", u.id, u.keys[i].ID), nil)
	g.expectStatus(resp, http.StatusOK, "delete public key")
	u.keys = append(u.keys[:i], u.keys[i+1:]...)
}

// --- invariants ---------------------------------------------------------------------

func (g *genRun) checkInvariants(step int) {
	g.t.Helper()

	// The admin listing agrees with the model.
	resp := g.h.do("GET", "/api/v1/admin/users", nil)
	var listed []models.User
	mustJSON(g.t, resp, &listed)
	byID := map[int64]models.User{}
	for _, u := range listed {
		byID[u.ID] = u
	}
	for _, u := range g.users {
		got, ok := byID[u.id]
		if !ok {
			g.fail("%s missing from the admin listing", u.email)
		}
		if got.SSHOnly != u.sshOnly || got.Role != u.role || got.Name != u.name || got.Email != u.email {
			g.fail("listing of %s: got ssh_only=%t role=%s name=%s, model ssh_only=%t role=%s name=%s",
				u.email, got.SSHOnly, got.Role, got.Name, u.sshOnly, u.role, u.name)
		}

		// The DB holds a password exactly when the model does, and an SSH-only row
		// never holds one nor the admin role.
		var hasHash bool
		if err := g.h.db.Get(&hasHash, "SELECT password_hash IS NOT NULL FROM users WHERE id = ?", u.id); err != nil {
			g.fail("read password_hash of %s: %v", u.email, err)
		}
		if hasHash != (u.password != "") {
			g.fail("%s: password_hash present=%t, model has password=%t", u.email, hasHash, u.password != "")
		}
		if u.sshOnly && (hasHash || got.Role != "user") {
			g.fail("%s is SSH-only but has password=%t role=%s", u.email, hasHash, got.Role)
		}

		// Public keys survive every switch, both ways.
		if keys := g.listPublicKeys(u); !sameKeyIDs(keys, u.keys) {
			g.fail("%s public keys: got %v, model %v (switched to SSH-only %d times)",
				u.email, keyIDs(keys), keyIDs(u.keys), u.switchedN)
		}

		// Live API keys work exactly while the account is not SSH-only; a key revoked
		// by a switch to SSH-only stays refused after the switch back.
		for _, k := range u.apiKeys {
			if got, want := g.apiKeyStatus(k), expectAuth(!u.sshOnly); got != want {
				g.fail("API key of %s (ssh_only=%t): got %d, expected %d", u.email, u.sshOnly, got, want)
			}
		}
		for _, k := range u.revoked {
			if got := g.apiKeyStatus(k); got != http.StatusUnauthorized {
				g.fail("revoked API key of %s (ssh_only=%t, switched %d times): got %d, expected 401",
					u.email, u.sshOnly, u.switchedN, got)
			}
		}

		// The admin listing of the account's API keys holds the live ones only.
		var listedKeys []struct {
			KeyPrefix string `json:"key_prefix"`
		}
		mustJSON(g.t, g.h.do("GET", fmt.Sprintf("/api/v1/admin/users/%d/api-keys", u.id), nil), &listedKeys)
		if len(listedKeys) != len(u.apiKeys) {
			g.fail("%s: admin lists %d API keys, model has %d live (%d revoked)",
				u.email, len(listedKeys), len(u.apiKeys), len(u.revoked))
		}
		for _, lk := range listedKeys {
			for _, k := range u.revoked {
				if lk.KeyPrefix != "" && strings.HasPrefix(k, lk.KeyPrefix) && !hasPrefixIn(u.apiKeys, lk.KeyPrefix) {
					g.fail("%s: revoked API key %s… still listed", u.email, lk.KeyPrefix)
				}
			}
		}
	}

	// Sessions: refused while SSH-only, and a session cut by a switch stays cut even
	// after the account is switched back.
	for i, s := range g.sessions {
		r := s.do(g, "GET", "/api/v1/instances")
		want := expectAuth(s.alive && !s.user.sshOnly)
		if r != want {
			g.fail("session #%d of %s (alive=%t, ssh_only=%t, switched %d times): got %d, expected %d",
				i, s.user.email, s.alive, s.user.sshOnly, s.user.switchedN, r, want)
		}
	}

	// Every account's keys are offered to the admin on the SSH Access tab, SSH-only or not.
	offered := offeredKeys(g.t, g.h.do, g.instID)
	for _, u := range g.users {
		for _, k := range u.keys {
			o := keyWithID(offered, k.ID)
			if o == nil {
				g.fail("key %d of %s (ssh_only=%t) not offered on the SSH Access tab", k.ID, u.email, u.sshOnly)
			}
			if o.OwnerEmail != u.email || o.PublicKey != k.PublicKey {
				g.fail("key %d offered as %s/%q, expected %s/%q", k.ID, o.OwnerEmail, o.PublicKey, u.email, k.PublicKey)
			}
		}
	}
	_ = step
}

func (s *genSession) do(g *genRun, method, path string) int {
	req, _ := http.NewRequest(method, g.h.srv.URL+path, nil)
	r, err := s.client.Do(req)
	if err != nil {
		g.fail("session request: %v", err)
	}
	r.Body.Close()
	return r.StatusCode
}

func expectAuth(ok bool) int {
	if ok {
		return http.StatusOK
	}
	return http.StatusUnauthorized
}

// --- helpers ------------------------------------------------------------------------

func (g *genRun) listPublicKeys(u *genUser) []models.SSHKey {
	resp := g.h.do("GET", fmt.Sprintf("/api/v1/admin/users/%d/public-keys", u.id), nil)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		g.fail("list public keys of %s: %d", u.email, resp.StatusCode)
	}
	var keys []models.SSHKey
	mustJSON(g.t, resp, &keys)
	return keys
}

func (g *genRun) expectStatus(resp *http.Response, want int, what string) {
	g.t.Helper()
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != want {
		g.fail("%s: got %d, expected %d — %s", what, resp.StatusCode, want, b)
	}
}

// expectRefused accepts any error status: what matters is that nothing was created.
func (g *genRun) expectRefused(resp *http.Response, what string) {
	g.t.Helper()
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 400 {
		g.fail("%s: got %d, expected a refusal — %s", what, resp.StatusCode, b)
	}
}

func (g *genRun) expectCreated(resp *http.Response, v any) {
	g.t.Helper()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		g.fail("expected 201, got %d — %s", resp.StatusCode, b)
	}
	mustJSON(g.t, resp, v)
}

func keyIDs(keys []models.SSHKey) []int64 {
	ids := make([]int64, len(keys))
	for i, k := range keys {
		ids[i] = k.ID
	}
	return ids
}

func sameKeyIDs(a, b []models.SSHKey) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[int64]bool{}
	for _, k := range a {
		seen[k.ID] = true
	}
	for _, k := range b {
		if !seen[k.ID] {
			return false
		}
	}
	return true
}

func fmtBoolPtr(b *bool) string {
	if b == nil {
		return "absent"
	}
	return strconv.FormatBool(*b)
}
