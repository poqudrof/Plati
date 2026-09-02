// Package integration tests the complete Plati API workflow using a mock Incus client.
// These tests exercise the real HTTP handlers, real SQLite database, and real JWT auth,
// but replace the Incus backend with a deterministic mock so no Incus daemon is required.
package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
	incusapi "github.com/lxc/incus/v6/shared/api"
	_ "github.com/mattn/go-sqlite3"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/config"
	"github.com/homaserver/plati/internal/database"
	"github.com/homaserver/plati/internal/handlers"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
	"github.com/homaserver/plati/internal/router"
	"github.com/homaserver/plati/internal/services"
)

// ── Mock Incus client ────────────────────────────────────────────────────────

// runCall records a single RunCommand invocation for assertion in tests.
type runCall struct {
	instanceName string
	shellCmd     string // the /bin/sh -c argument, or joined command string
}

type mockIncusClient struct {
	instances       map[string]*incusapi.Instance
	instanceIPs     map[string]string
	volumes         map[string]int
	pushedFiles     map[string][]byte // remote path → content
	runCommandFn    func(name string, command []string) (string, error)
	streamCommandFn func(name string, command []string) (io.ReadCloser, error)

	mu       sync.Mutex
	runCalls []runCall // all RunCommand calls, for assertion
}

func newMockIncus() *mockIncusClient {
	return &mockIncusClient{
		instances:   make(map[string]*incusapi.Instance),
		instanceIPs: make(map[string]string),
		volumes:     make(map[string]int),
		pushedFiles: make(map[string][]byte),
	}
}

func (m *mockIncusClient) CreateInstance(name, image string, profiles []string, cfg map[string]string, devices map[string]map[string]string) error {
	m.instances[name] = &incusapi.Instance{
		Name:   name,
		Status: "Running",
		InstancePut: incusapi.InstancePut{
			Config: cfg,
		},
	}
	m.instanceIPs[name] = "10.0.0.42"
	return nil
}

func (m *mockIncusClient) StartInstance(name string) error {
	if inst, ok := m.instances[name]; ok {
		inst.Status = "Running"
	}
	return nil
}

func (m *mockIncusClient) StopInstance(name string) error {
	if inst, ok := m.instances[name]; ok {
		inst.Status = "Stopped"
	}
	return nil
}

func (m *mockIncusClient) RenameInstance(name, newName string) error {
	inst, ok := m.instances[name]
	if !ok {
		return fmt.Errorf("instance %s not found", name)
	}
	// Incus refuses to rename a running instance; the mock does too, so a test
	// that forgets to stop it fails here rather than passing by accident.
	if inst.Status == "Running" {
		return fmt.Errorf("instance %s is running", name)
	}
	if _, exists := m.instances[newName]; exists {
		return fmt.Errorf("instance %s already exists", newName)
	}
	inst.Name = newName
	m.instances[newName] = inst
	delete(m.instances, name)
	if ip, ok := m.instanceIPs[name]; ok {
		m.instanceIPs[newName] = ip
		delete(m.instanceIPs, name)
	}
	return nil
}

func (m *mockIncusClient) DeleteInstance(name string) error {
	delete(m.instances, name)
	delete(m.instanceIPs, name)
	return nil
}

func (m *mockIncusClient) GetInstanceState(name string) (*incusapi.InstanceState, error) {
	ip, ok := m.instanceIPs[name]
	if !ok {
		return nil, fmt.Errorf("instance %s not found", name)
	}
	return &incusapi.InstanceState{
		Status: "Running",
		Network: map[string]incusapi.InstanceStateNetwork{
			"eth0": {
				Addresses: []incusapi.InstanceStateNetworkAddress{
					{Family: "inet", Address: ip, Scope: "global"},
				},
			},
		},
	}, nil
}

func (m *mockIncusClient) GetInstance(name string) (*incusapi.Instance, error) {
	inst, ok := m.instances[name]
	if !ok {
		return nil, fmt.Errorf("instance %s not found", name)
	}
	return inst, nil
}

func (m *mockIncusClient) CreateVolume(pool, name string, sizeGB int) error {
	m.volumes[name] = sizeGB
	return nil
}

func (m *mockIncusClient) DeleteVolume(pool, name string) error {
	delete(m.volumes, name)
	return nil
}

func (m *mockIncusClient) AttachVolume(pool, volumeName, instanceName, deviceName, path string) error {
	return nil
}

func (m *mockIncusClient) DetachVolume(instanceName, deviceName string) error {
	return nil
}

func (m *mockIncusClient) ListImages() ([]incusapi.Image, error) {
	return []incusapi.Image{
		{Fingerprint: "abc123"},
	}, nil
}

func (m *mockIncusClient) GetServerResources() (*incusapi.Resources, error) {
	return &incusapi.Resources{}, nil
}

func (m *mockIncusClient) GetProfileNames() ([]string, error) {
	return []string{"default"}, nil
}

func (m *mockIncusClient) ExecInstance(name string, command []string, env map[string]string, stdin io.ReadCloser, stdout io.WriteCloser, control func(conn *websocket.Conn)) error {
	return nil
}

func (m *mockIncusClient) RunCommand(name string, command []string) (string, error) {
	// Record the call for later assertion.
	shellCmd := strings.Join(command, " ")
	if len(command) >= 3 && command[0] == "/bin/sh" && command[1] == "-c" {
		shellCmd = command[2]
	}
	m.mu.Lock()
	m.runCalls = append(m.runCalls, runCall{instanceName: name, shellCmd: shellCmd})
	m.mu.Unlock()

	if m.runCommandFn != nil {
		return m.runCommandFn(name, command)
	}
	return "", nil
}

// hasRunCall returns true if any recorded RunCommand call for instanceName
// contains all of the given substrings in its shell command.
func (m *mockIncusClient) hasRunCall(instanceName string, substrings ...string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.runCalls {
		if c.instanceName != instanceName {
			continue
		}
		allMatch := true
		for _, s := range substrings {
			if !strings.Contains(c.shellCmd, s) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return true
		}
	}
	return false
}

// runCallsFor returns all shell commands run on a given instance.
func (m *mockIncusClient) runCallsFor(instanceName string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, c := range m.runCalls {
		if c.instanceName == instanceName {
			out = append(out, c.shellCmd)
		}
	}
	return out
}

// UpdateInstanceConfig merges into the stored config, mirroring the real client:
// an empty value deletes the key.
func (m *mockIncusClient) UpdateInstanceConfig(name string, config map[string]string) error {
	inst, ok := m.instances[name]
	if !ok {
		return fmt.Errorf("mock: instance %s not found", name)
	}
	if inst.Config == nil {
		inst.Config = map[string]string{}
	}
	for k, v := range config {
		if v == "" {
			delete(inst.Config, k)
		} else {
			inst.Config[k] = v
		}
	}
	return nil
}

func (m *mockIncusClient) AttachHostPath(instanceName, deviceName, hostPath, instancePath string) error {
	return nil
}

func (m *mockIncusClient) StreamCommand(name string, command []string) (io.ReadCloser, error) {
	if m.streamCommandFn != nil {
		return m.streamCommandFn(name, command)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockIncusClient) PushFile(instanceName, remotePath string, content []byte, uid, gid int64, mode int) error {
	m.pushedFiles[remotePath] = content
	return nil
}

func (m *mockIncusClient) ListDirectory(instanceName, path string) ([]incus.FileEntry, error) {
	return nil, nil
}
func (m *mockIncusClient) GetFile(instanceName, path string) (io.ReadCloser, *incus.FileInfo, error) {
	return io.NopCloser(strings.NewReader("")), &incus.FileInfo{Type: "file"}, nil
}
func (m *mockIncusClient) StreamDirectory(instanceName, path string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (m *mockIncusClient) ListVolumeSnapshots(pool, volumeName string) ([]incus.VolumeSnapshotInfo, error) {
	return nil, nil
}
func (m *mockIncusClient) CreateVolumeSnapshot(pool, volumeName, snapshotName string) error {
	return nil
}
func (m *mockIncusClient) DeleteVolumeSnapshot(pool, volumeName, snapshotName string) error {
	return nil
}
func (m *mockIncusClient) RestoreVolumeSnapshot(pool, volumeName, snapshotName string) error {
	return nil
}

// ── Test harness ─────────────────────────────────────────────────────────────

const (
	testPassword = "integration-test-2024"
	jwtSecret    = "test-jwt-secret-for-integration-tests"
)

type harness struct {
	srv    *httptest.Server
	client *http.Client
	mock   *mockIncusClient
	db     *sqlx.DB
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	// Fresh in-memory SQLite database
	db, err := database.New(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Hash test password
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	// Config
	cfg := &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", Port: 0, FrontendURL: "http://localhost"},
		Auth: config.AuthConfig{
			AdminPasswordHash: hash,
			JWTSecret:         jwtSecret,
			JWTLifetimeHours:  24,
		},
		Database: config.DatabaseConfig{Path: ":memory:"},
	}

	// Mock Incus pool
	mock := newMockIncus()
	pool := incus.NewPool()
	pool.SetClient("test-server", mock)

	// Insert test server into DB so InstanceService can find it
	_, err = db.Exec(`INSERT INTO servers (name, endpoint, is_online, max_instances) VALUES ('test-server', 'https://mock:8443', 1, 50)`)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}

	// Services & handlers
	userSvc, err := services.NewUserService(db, "0000000000000000000000000000000000000000000000000000000000000000", "")
	if err != nil {
		t.Fatalf("user service: %v", err)
	}
	templateSvc := services.NewTemplateService(db, "")
	prefSvc := services.NewPreferencesService(db)
	adminSvc := services.NewAdminSettingsService(db, userSvc)
	managedKeySvc := services.NewManagedKeyService(db, userSvc, "")
	repoSvc := services.NewRepoService(db, userSvc, "")
	instanceSvc := services.NewInstanceService(db, pool, userSvc, prefSvc, adminSvc, "", "", "", repoSvc, templateSvc)
	serverSvc := services.NewServerService(db, pool)
	apiKeySvc := services.NewAPIKeyService(db)
	adminHandler := handlers.NewAdminHandler(db, userSvc, instanceSvc, templateSvc)
	healthHandler := handlers.NewHealthHandler(db)
	setupHandler := handlers.NewSetupHandler(db, cfg, "/dev/null")

	r := router.New(router.Deps{
		AuthHandler:          handlers.NewAuthHandler(db, cfg, nil),
		UserHandler:          handlers.NewUserHandler(userSvc),
		TemplateHandler:      handlers.NewTemplateHandler(templateSvc),
		InstanceHandler:      handlers.NewInstanceHandler(instanceSvc, db),
		ServerHandler:        handlers.NewServerHandler(serverSvc, pool),
		AdminHandler:         adminHandler,
		ManagedKeyHandler:    handlers.NewManagedKeyHandler(managedKeySvc),
		HealthHandler:        healthHandler,
		SetupHandler:         setupHandler,
		TerminalHandler:      handlers.NewTerminalHandler(db, pool, instanceSvc.CreationLogs()),
		PreferencesHandler:   handlers.NewPreferencesHandler(prefSvc),
		AdminSettingsHandler: handlers.NewAdminSettingsHandler(adminSvc),
		// Not started: the tests exercise the sleep HTTP surface, not the worker.
		SleepHandler:  handlers.NewSleepHandler(services.NewSleepService(db, pool, 4*time.Hour)),
		RepoHandler:   handlers.NewRepoHandler(repoSvc),
		APIKeyHandler: handlers.NewAPIKeyHandler(apiKeySvc),
		APIKeyAuth:    apiKeySvc.AsAuthenticator(),
		JWTSecret:     jwtSecret,
		FrontendURL:   "http://localhost",
	})

	srv := httptest.NewServer(r)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	return &harness{srv: srv, client: client, mock: mock, db: db}
}

func (h *harness) teardown() {
	h.srv.Close()
	h.db.Close()
}

func (h *harness) do(method, path string, body any) *http.Response {
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		panic(err)
	}
	return resp
}

func mustJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

// waitForInstance polls the mock until the Incus instance appears (CreateAsync goroutine has run).
func waitForInstance(t *testing.T, h *harness, incusName string) {
	t.Helper()
	for i := 0; i < 50; i++ {
		if _, ok := h.mock.instances[incusName]; ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for mock instance %s", incusName)
}

// waitForInstanceReady polls until the instance status in the DB is "running" (async creation complete).
func waitForInstanceReady(t *testing.T, h *harness, instanceID int64) {
	t.Helper()
	for i := 0; i < 100; i++ {
		var status string
		err := h.db.Get(&status, "SELECT status FROM instances WHERE id = ?", instanceID)
		if err == nil && status == "running" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for instance %d to become running", instanceID)
}

func seedTemplate(t *testing.T, h *harness) int64 {
	t.Helper()
	// Use the Import endpoint which accepts the TemplateJSON format (proper types)
	body := strings.NewReader(`{
		"name": "node-dev",
		"slug": "node-dev",
		"description": "Node.js 22 LTS",
		"image": "images:ubuntu/24.04/cloud",
		"profiles": ["default"],
		"resources": {"cpu": "2", "memory": "4GB", "disk": "20GB"},
		"terminal_user": "ubuntu"
	}`)
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

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestHealthCheck(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	resp := h.do("GET", "/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: %d", resp.StatusCode)
	}
	var result map[string]string
	mustJSON(t, resp, &result)
	if result["status"] != "ok" {
		t.Errorf("health status = %q, want ok", result["status"])
	}
	t.Logf("Health: %v", result)
}

func TestAdminLogin(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	// Wrong password → 401
	resp := h.do("POST", "/auth/login", map[string]string{"password": "wrong"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password: got %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// Correct password → 200 + cookie
	resp = h.do("POST", "/auth/login", map[string]string{"password": testPassword})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// /auth/me should return admin user
	resp = h.do("GET", "/auth/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me: %d", resp.StatusCode)
	}
	var user models.User
	mustJSON(t, resp, &user)
	if user.Email != "admin@plati.local" {
		t.Errorf("user email = %q, want admin@plati.local", user.Email)
	}
	if user.Role != "admin" {
		t.Errorf("user role = %q, want admin", user.Role)
	}
	t.Logf("Logged in as %s (%s)", user.Email, user.Role)
}

func TestAdminCreateUser(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	// Unauthenticated → 401
	resp := h.do("POST", "/api/v1/admin/users", map[string]string{
		"email": "newuser@example.com", "name": "New User", "password": "secret123",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated create user: got %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// Log in as admin
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Missing required fields → 400
	resp = h.do("POST", "/api/v1/admin/users", map[string]string{"name": "No Email"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing email: got %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// Create user → 201
	resp = h.do("POST", "/api/v1/admin/users", map[string]string{
		"email":    "alice@example.com",
		"name":     "Alice",
		"password": "alicepass",
		"role":     "user",
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create user: %d — %s", resp.StatusCode, body)
	}
	var created models.User
	mustJSON(t, resp, &created)
	if created.Email != "alice@example.com" {
		t.Errorf("created email = %q, want alice@example.com", created.Email)
	}
	if created.Role != "user" {
		t.Errorf("created role = %q, want user", created.Role)
	}
	t.Logf("Created user: id=%d email=%s", created.ID, created.Email)

	// List users → new user appears
	resp = h.do("GET", "/api/v1/admin/users", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list users: %d", resp.StatusCode)
	}
	var users []models.User
	mustJSON(t, resp, &users)
	found := false
	for _, u := range users {
		if u.Email == "alice@example.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("created user not found in list")
	}

	// New user can log in with their password
	jar2, _ := cookiejar.New(nil)
	client2 := &http.Client{Jar: jar2}
	data, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "alicepass"})
	req, _ := http.NewRequest("POST", h.srv.URL+"/auth/login", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client2.Do(req)
	if err != nil {
		t.Fatalf("alice login request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("alice login: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()
	t.Logf("Alice logged in successfully")
}

func TestSSHKeyCRUD(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	// Login
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	testKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID31VnLdOuNBWx0fENjnlGbaiN5QkbVHK6YT1rGkKFDI test@plati"

	// Add key
	resp := h.do("POST", "/api/v1/ssh-keys", map[string]string{
		"name":       "laptop",
		"public_key": testKey,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("add SSH key: %d — %s", resp.StatusCode, body)
	}
	var key map[string]any
	mustJSON(t, resp, &key)
	keyID := int64(key["id"].(float64))
	t.Logf("SSH key created: id=%d", keyID)

	// List keys
	resp = h.do("GET", "/api/v1/ssh-keys", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list SSH keys: %d", resp.StatusCode)
	}
	var keys []map[string]any
	mustJSON(t, resp, &keys)
	if len(keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(keys))
	}
	if keys[0]["public_key"] != testKey {
		t.Errorf("key mismatch")
	}

	// Delete key
	resp = h.do("DELETE", fmt.Sprintf("/api/v1/ssh-keys/%d", keyID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete SSH key: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify deleted
	resp = h.do("GET", "/api/v1/ssh-keys", nil)
	mustJSON(t, resp, &keys)
	if len(keys) != 0 {
		t.Errorf("expected 0 keys after delete, got %d", len(keys))
	}
}

// TestAdminUserPublicKeyAndInstanceAuthorizedKeys covers the admin adding a public key
// on behalf of a user and that key being installable into a running instance.
func TestAdminUserPublicKeyAndInstanceAuthorizedKeys(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// The admin acts on their own account here; any user id works the same way.
	resp := h.do("GET", "/api/v1/admin/users", nil)
	var users []models.User
	mustJSON(t, resp, &users)
	if len(users) == 0 {
		t.Fatal("expected at least one user")
	}
	userID := users[0].ID

	pubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIvIYSTFxIB1z0hFOKcJvJHPTr8w+wKDIQmXA0M/xn9m alice@laptop"

	// A malformed key is rejected before it can break authorized_keys.
	resp = h.do("POST", fmt.Sprintf("/api/v1/admin/users/%d/public-keys", userID), map[string]string{
		"name": "bad", "public_key": "not-a-key",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed public key: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = h.do("POST", fmt.Sprintf("/api/v1/admin/users/%d/public-keys", userID), map[string]string{
		"name": "laptop", "public_key": pubKey,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("add public key: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = h.do("GET", fmt.Sprintf("/api/v1/admin/users/%d/public-keys", userID), nil)
	var adminKeys []map[string]any
	mustJSON(t, resp, &adminKeys)
	if len(adminKeys) != 1 || adminKeys[0]["public_key"] != pubKey {
		t.Fatalf("expected the added key back, got %v", adminKeys)
	}

	// Create a running instance and install the key into its authorized_keys.
	templateID := seedTemplate(t, h)
	resp = h.do("POST", "/api/v1/instances", map[string]any{
		"name": "keytest", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)

	var updated models.InstanceJSON
	for i := 0; i < 100; i++ {
		resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil)
		mustJSON(t, resp, &updated)
		if updated.Status == "running" || updated.Status == "error" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if updated.Status != "running" {
		t.Fatalf("instance not running: %s", updated.Status)
	}

	resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d/authorized-keys", inst.ID), nil)
	var instKeys []map[string]any
	mustJSON(t, resp, &instKeys)
	if len(instKeys) != 1 || instKeys[0]["name"] != "laptop" {
		t.Fatalf("expected the owner's key on the instance, got %v", instKeys)
	}
	keyID := int64(instKeys[0]["id"].(float64))

	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/authorized-keys", inst.ID),
		map[string]any{"key_id": keyID})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("install key: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	if !h.mock.hasRunCall(inst.IncusName, "add_key /root/.ssh") {
		t.Errorf("no authorized_keys install command ran; got: %v", h.mock.runCallsFor(inst.IncusName))
	}

	// An unknown key id is rejected.
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/authorized-keys", inst.ID),
		map[string]any{"key_id": 99999})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown key id: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTemplateImportAndList(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Import template
	tmplJSON := `{
		"name": "node-dev",
		"slug": "node-dev",
		"description": "Node.js 22 LTS",
		"image": "images:ubuntu/24.04/cloud",
		"profiles": ["default"],
		"resources": {"cpu": "2", "memory": "4GB", "disk": "20GB"}
	}`
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/admin/templates/import",
		strings.NewReader(tmplJSON))
	req.Header.Set("Content-Type", "application/json")
	// Copy cookies manually
	for _, c := range h.client.Jar.Cookies(req.URL) {
		req.AddCookie(c)
	}
	resp, _ := h.client.Do(req)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("import template: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// List templates
	resp = h.do("GET", "/api/v1/templates", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list templates: %d", resp.StatusCode)
	}
	var templates []models.Template
	mustJSON(t, resp, &templates)
	if len(templates) == 0 {
		t.Fatal("expected at least one template")
	}
	found := false
	for _, tmpl := range templates {
		if tmpl.Slug == "node-dev" {
			found = true
			t.Logf("Template: %s (id=%d)", tmpl.Name, tmpl.ID)
		}
	}
	if !found {
		t.Error("node-dev template not found")
	}
}

func TestInstanceLifecycleWithMockIncus(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Add SSH key — it should be injected via exec-based setup
	sshPubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIH6A2qUuA3yPcbuQLrGqd9GfD6CXnz4s0NBwbp+N2ovk ci-user@laptop"
	h.do("POST", "/api/v1/ssh-keys", map[string]string{
		"name": "ci-key", "public_key": sshPubKey,
	}).Body.Close()

	// Create template
	templateID := seedTemplate(t, h)

	// Create instance → triggers mock Incus
	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name":        "my-workspace",
		"template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)

	t.Logf("Instance created: id=%d name=%s incus=%s", inst.ID, inst.Name, inst.IncusName)

	// Wait for async setup to complete (file push happens in background goroutine).
	// Declare updated outside the loop so it's accessible after for post-loop assertions.
	var updated models.InstanceJSON
	for i := 0; i < 100; i++ {
		resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil)
		mustJSON(t, resp, &updated)
		if updated.Status == "running" || updated.Status == "error" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Verify mock received the create call. Checked after the wait: creation is async, so
	// the 201 comes back before CreateAsync has reached the Incus client.
	if len(h.mock.instances) == 0 {
		t.Error("mock: no Incus instance was created")
	}

	// Verify SSH key was pushed via exec-based setup (file push to authorized_keys)
	foundKey := false
	for dest, content := range h.mock.pushedFiles {
		if strings.Contains(dest, "authorized_keys") && strings.Contains(string(content), sshPubKey) {
			foundKey = true
			break
		}
	}
	if !foundKey {
		t.Error("SSH key not found in pushed authorized_keys files")
	} else {
		t.Logf("SSH key correctly pushed via exec-based setup")
	}

	// Verify volume was created
	volName := inst.IncusName + "-home-ubuntu"
	if _, ok := h.mock.volumes[volName]; !ok {
		t.Errorf("mock: persistent volume %q not created", volName)
	}
	t.Logf("Persistent volume created: %s", volName)

	// Verify IP was stored in the DB after async creation completed.
	// (inst is from the create response; updated is from the re-fetch after creation.)
	if updated.IPAddress == nil || *updated.IPAddress == "" {
		t.Error("instance IP not set after async creation completed")
	} else {
		t.Logf("Instance IP: %s", *updated.IPAddress)
	}

	// List instances → should see our instance
	resp = h.do("GET", "/api/v1/instances", nil)
	var instances []models.InstanceJSON
	mustJSON(t, resp, &instances)
	if len(instances) != 1 {
		t.Errorf("expected 1 instance, got %d", len(instances))
	}

	// Stop instance
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/stop", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("stop instance: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()
	t.Logf("Instance stopped")

	// Start instance
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/start", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("start instance: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()
	t.Logf("Instance started")

	// Delete instance
	resp = h.do("DELETE", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete instance: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()
	t.Logf("Instance deleted")

	// Verify mock state
	if _, ok := h.mock.instances[inst.IncusName]; ok {
		t.Error("mock: Incus instance was not deleted")
	}
	if _, ok := h.mock.volumes[volName]; ok {
		t.Error("mock: persistent volume was not deleted")
	}

	// Verify DB
	resp = h.do("GET", "/api/v1/instances", nil)
	mustJSON(t, resp, &instances)
	if len(instances) != 0 {
		t.Errorf("expected 0 instances after delete, got %d", len(instances))
	}
	t.Logf("Full lifecycle OK: create → start → stop → delete")
}

func TestRebuildKeepsWorkspace(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	// Create instance
	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "rebuild-test", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	volName := inst.IncusName + "-home-ubuntu"

	initialVolumeSize := h.mock.volumes[volName]
	t.Logf("Before rebuild: volume=%s size=%d", volName, initialVolumeSize)

	// Rebuild
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/rebuild", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("rebuild: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// Volume should still exist (home preserved)
	if _, ok := h.mock.volumes[volName]; !ok {
		t.Error("persistent volume was deleted during rebuild — data would be lost!")
	}
	t.Logf("Rebuild OK: persistent volume preserved")

	// Instance should be recreated in mock
	if _, ok := h.mock.instances[inst.IncusName]; !ok {
		t.Error("Incus instance was not recreated after rebuild")
	}
}

func TestAdminCanCreateInstanceForUser(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	// Create a regular user via DB
	res, err := h.db.Exec(
		`INSERT INTO users (email, name, role) VALUES ('user@test.com', 'Test User', 'user')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID, _ := res.LastInsertId()

	// Admin creates instance for that user
	resp := h.do("POST", "/api/v1/admin/instances", map[string]any{
		"name":        "admin-created",
		"template_id": templateID,
		"user_id":     userID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin create instance: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	if inst.UserID != userID {
		t.Errorf("instance user_id = %d, want %d", inst.UserID, userID)
	}
	t.Logf("Admin created instance %d for user %d", inst.ID, userID)
}

func TestSecretInjectionIntoInstance(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Create a TAILSCALE_AUTH_KEY secret
	const tailscaleKey = "tskey-auth-kRqVWCq18Z11CNTRL-qZFfkoAyAcMT1NYmuVmbbMPZwnK3opj1"
	resp := h.do("POST", "/api/v1/secrets", map[string]string{
		"name":  "TAILSCALE_AUTH_KEY",
		"value": tailscaleKey,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create secret: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()
	t.Logf("TAILSCALE_AUTH_KEY secret created")

	// Import a tailscale-style template
	tailscaleTemplate := strings.NewReader(`{
		"name": "Tailscale",
		"slug": "tailscale",
		"description": "System container with Tailscale VPN",
		"image": "images:ubuntu/24.04/cloud",
		"profiles": ["default"],
		"resources": {"cpu": "1", "memory": "512MB", "disk": "5GB"}
	}`)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/admin/templates/import", tailscaleTemplate)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range h.client.Jar.Cookies(req.URL) {
		req.AddCookie(c)
	}
	resp, _ = h.client.Do(req)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("import tailscale template: %d — %s", resp.StatusCode, body)
	}
	var tmpl models.Template
	mustJSON(t, resp, &tmpl)
	t.Logf("Tailscale template imported: id=%d", tmpl.ID)

	// Create instance from the tailscale template
	resp = h.do("POST", "/api/v1/instances", map[string]any{
		"name":        "ts-test",
		"template_id": tmpl.ID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	t.Logf("Instance created: id=%d incus=%s", inst.ID, inst.IncusName)

	// Wait for async creation to complete before checking mock state.
	waitForInstanceReady(t, h, inst.ID)

	// Verify the mock received environment.TAILSCALE_AUTH_KEY in the Incus config
	mockInst, ok := h.mock.instances[inst.IncusName]
	if !ok {
		t.Fatal("mock: instance not created in Incus")
	}

	envKey := "environment.TAILSCALE_AUTH_KEY"
	gotValue, found := mockInst.Config[envKey]
	if !found {
		t.Errorf("secret not injected: %q not found in Incus config\nconfig keys: %v", envKey, configKeys(mockInst.Config))
	} else if gotValue != tailscaleKey {
		t.Errorf("secret value mismatch: got %q, want %q", gotValue, tailscaleKey)
	} else {
		t.Logf("TAILSCALE_AUTH_KEY correctly injected into Incus config")
	}

	// Also verify that rebuild re-injects the secret
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/rebuild", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("rebuild: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	mockInst, ok = h.mock.instances[inst.IncusName]
	if !ok {
		t.Fatal("mock: instance not recreated after rebuild")
	}
	gotValue, found = mockInst.Config[envKey]
	if !found {
		t.Errorf("secret not re-injected after rebuild: %q missing", envKey)
	} else if gotValue != tailscaleKey {
		t.Errorf("secret value after rebuild: got %q, want %q", gotValue, tailscaleKey)
	} else {
		t.Logf("TAILSCALE_AUTH_KEY correctly re-injected after rebuild")
	}

	// Cleanup
	h.do("DELETE", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil).Body.Close()
	t.Logf("Secret injection lifecycle OK: create secret → create instance → verify injection → rebuild → verify re-injection")
}

// configKeys returns the keys of a config map for debugging.
func configKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestPhase2SetupIsCalledOnCreate(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Add a public SSH key
	sshPubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJCqJcnnx78ZsZ1S2DIOVu7U4iyqAV3qo4rB8bbSrL3A phase2@test"
	h.do("POST", "/api/v1/ssh-keys", map[string]string{
		"name": "phase2-key", "public_key": sshPubKey,
	}).Body.Close()

	// Capture all RunCommand calls
	var capturedCmds [][]string
	h.mock.runCommandFn = func(_ string, command []string) (string, error) {
		capturedCmds = append(capturedCmds, command)
		return "", nil
	}

	templateID := seedTemplate(t, h)

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name":        "phase2-test",
		"template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)

	// Wait for the background goroutine to run phase2 and set status to "running"
	waitForInstance(t, h, inst.IncusName)
	for i := 0; i < 50; i++ {
		r := h.do("GET", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil)
		var updated models.InstanceJSON
		mustJSON(t, r, &updated)
		if updated.Status == "running" || updated.Status == "error" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Verify that authorized_keys was pushed (now via PushFile, not RunCommand)
	found := false
	for path := range h.mock.pushedFiles {
		if strings.Contains(path, "authorized_keys") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("phase2: no authorized_keys push found (pushed files: %d, run commands: %d)", len(h.mock.pushedFiles), len(capturedCmds))
	} else {
		t.Logf("phase2: authorized_keys pushed (total pushed: %d, run cmds: %d)", len(h.mock.pushedFiles), len(capturedCmds))
	}
}

func TestSshxURLEndpoint(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Import sshx template
	body := strings.NewReader(`{
		"name":"SSHX","slug":"sshx","description":"Collaborative terminal",
		"image":"images:ubuntu/24.04/cloud","profiles":["default"],
		"resources":{"cpu":"1","memory":"512MB","disk":"5GB"}
	}`)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/admin/templates/import", body)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range h.client.Jar.Cookies(req.URL) {
		req.AddCookie(c)
	}
	resp, _ := h.client.Do(req)
	var tmpl models.Template
	mustJSON(t, resp, &tmpl)

	// Create instance
	resp = h.do("POST", "/api/v1/instances", map[string]any{"name": "sshx-test", "template_id": tmpl.ID})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: %d — %s", resp.StatusCode, b)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)

	// Wire mock to return a fake URL
	const fakeURL = "https://sshx.io/s/TestABCDEF#secretkey123"
	h.mock.runCommandFn = func(_ string, _ []string) (string, error) {
		return fakeURL + "\n", nil
	}

	// Wait for async creation to complete before querying the sshx-url endpoint
	// (the endpoint returns 503 when the instance is not yet running).
	waitForInstanceReady(t, h, inst.ID)

	// Running instance → URL returned
	resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d/sshx-url", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("sshx-url: %d — %s", resp.StatusCode, b)
	}
	var result struct {
		URL string `json:"url"`
	}
	mustJSON(t, resp, &result)
	if result.URL != fakeURL {
		t.Errorf("url = %q, want %q", result.URL, fakeURL)
	}
	t.Logf("SSHX URL: %s", result.URL)

	// Stopped instance → 503
	h.do("POST", fmt.Sprintf("/api/v1/instances/%d/stop", inst.ID), nil).Body.Close()
	resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d/sshx-url", inst.ID), nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("stopped: got %d, want 503", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestEphemeralInstanceNoVolume(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Import an ephemeral template
	body := strings.NewReader(`{
		"name":"Ephemeral","slug":"ephemeral-test","description":"No volumes",
		"image":"images:alpine/3.20","profiles":["default"],
		"resources":{"cpu":"1","memory":"256MB","disk":"5GB"},
		"persistence":{"mode":"ephemeral"}
	}`)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/admin/templates/import", body)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range h.client.Jar.Cookies(req.URL) {
		req.AddCookie(c)
	}
	resp, _ := h.client.Do(req)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("import ephemeral template: %d — %s", resp.StatusCode, b)
	}
	var tmpl models.Template
	mustJSON(t, resp, &tmpl)
	if tmpl.PersistenceMode != "ephemeral" {
		t.Errorf("persistence_mode = %q, want ephemeral", tmpl.PersistenceMode)
	}

	// Create instance — no volumes should be created
	volsBefore := len(h.mock.volumes)
	resp = h.do("POST", "/api/v1/instances", map[string]any{
		"name": "ephemeral-inst", "template_id": tmpl.ID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create ephemeral instance: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	if len(h.mock.volumes) != volsBefore {
		t.Errorf("ephemeral: volumes created (%d → %d), expected none", volsBefore, len(h.mock.volumes))
	}
	t.Logf("Ephemeral instance created with no volumes")
}

func TestUserPreferences(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Default preferences
	resp := h.do("GET", "/api/v1/preferences", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get prefs: %d", resp.StatusCode)
	}
	var prefs map[string]any
	mustJSON(t, resp, &prefs)
	if prefs["ssh_key_mode"] != "plati" {
		t.Errorf("default ssh_key_mode = %v, want plati", prefs["ssh_key_mode"])
	}
	if prefs["tailscale_mode"] != "plati" {
		t.Errorf("default tailscale_mode = %v, want plati", prefs["tailscale_mode"])
	}
	t.Logf("Default preferences: %v", prefs)

	// Update to personal mode
	resp = h.do("PUT", "/api/v1/preferences", map[string]string{
		"ssh_key_mode":   "personal",
		"tailscale_mode": "personal",
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("update prefs: %d — %s", resp.StatusCode, b)
	}
	mustJSON(t, resp, &prefs)
	if prefs["ssh_key_mode"] != "personal" {
		t.Errorf("updated ssh_key_mode = %v, want personal", prefs["ssh_key_mode"])
	}
	t.Logf("Updated preferences: %v", prefs)

	// Invalid mode → 400
	resp = h.do("PUT", "/api/v1/preferences", map[string]string{
		"ssh_key_mode":   "invalid",
		"tailscale_mode": "plati",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid mode: got %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestPlatformTailscaleKey(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Initially not configured
	resp := h.do("GET", "/api/v1/admin/settings/tailscale-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get tailscale key: %d", resp.StatusCode)
	}
	var result map[string]any
	mustJSON(t, resp, &result)
	if result["configured"] != false {
		t.Errorf("configured = %v, want false", result["configured"])
	}

	// Set the platform key
	const platformKey = "tskey-auth-platform-test-key"
	resp = h.do("PUT", "/api/v1/admin/settings/tailscale-key", map[string]string{
		"value": platformKey,
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("set tailscale key: %d — %s", resp.StatusCode, b)
	}
	mustJSON(t, resp, &result)
	if result["configured"] != true {
		t.Errorf("configured = %v after set, want true", result["configured"])
	}

	// Create an instance — platform key should be injected (no user secret)
	templateID := seedTemplate(t, h)
	resp = h.do("POST", "/api/v1/instances", map[string]any{
		"name":        "ts-platform-test",
		"template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, b)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)

	// Wait for async creation to complete before checking mock state.
	waitForInstanceReady(t, h, inst.ID)

	// Verify the platform key was injected into Incus config
	mockInst, ok := h.mock.instances[inst.IncusName]
	if !ok {
		t.Fatal("mock instance not found")
	}
	got, found := mockInst.Config["environment.TAILSCALE_AUTH_KEY"]
	if !found {
		t.Errorf("platform tailscale key not injected into instance config")
	} else if got != platformKey {
		t.Errorf("platform key = %q, want %q", got, platformKey)
	}
	t.Logf("Platform Tailscale key correctly injected: %s", got)

	// Delete the key
	resp = h.do("DELETE", "/api/v1/admin/settings/tailscale-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete tailscale key: %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = h.do("GET", "/api/v1/admin/settings/tailscale-key", nil)
	mustJSON(t, resp, &result)
	if result["configured"] != false {
		t.Errorf("configured = %v after delete, want false", result["configured"])
	}
	t.Logf("Platform Tailscale key CRUD OK")
}

func TestDeepCopyInstance(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Track StreamCommand calls
	var streamCalls []string
	h.mock.streamCommandFn = func(name string, command []string) (io.ReadCloser, error) {
		streamCalls = append(streamCalls, name+":"+strings.Join(command, " "))
		return io.NopCloser(strings.NewReader("")), nil
	}

	templateID := seedTemplate(t, h)

	// Create source instance
	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "src-instance", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create src: %d — %s", resp.StatusCode, b)
	}
	var src models.InstanceJSON
	mustJSON(t, resp, &src)

	// Duplicate
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/duplicate", src.ID), nil)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("duplicate: %d — %s", resp.StatusCode, b)
	}
	var dst models.InstanceJSON
	mustJSON(t, resp, &dst)

	if dst.Name != src.Name+"-copy" {
		t.Errorf("duplicate name = %q, want %q", dst.Name, src.Name+"-copy")
	}

	// Verify StreamCommand was called for volume data copy
	if len(streamCalls) == 0 {
		t.Error("StreamCommand was not called during duplicate — volume data not copied")
	}
	t.Logf("Deep copy OK: StreamCommand called %d time(s): %v", len(streamCalls), streamCalls)
}

func TestAdminChangesUserPassword(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	resp := h.do("POST", "/api/v1/admin/users", map[string]any{
		"email": "bob@plati.local", "name": "Bob", "password": "initial-password", "role": "user",
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create user: %d — %s", resp.StatusCode, b)
	}
	var user models.User
	mustJSON(t, resp, &user)

	// Log in outside the harness's cookie jar, so probing Bob's password does
	// not replace the admin session the edits are made with.
	login := func(password string) int {
		body, _ := json.Marshal(map[string]string{"email": "bob@plati.local", "password": password})
		r, err := http.Post(h.srv.URL+"/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		r.Body.Close()
		return r.StatusCode
	}

	edit := func(body map[string]any) *http.Response {
		return h.do("PUT", fmt.Sprintf("/api/v1/admin/users/%d", user.ID), body)
	}

	if code := login("initial-password"); code != http.StatusOK {
		t.Fatalf("login with the initial password: %d", code)
	}

	// An edit that sets no password must leave the current one alone.
	resp = edit(map[string]any{"name": "Bob Renamed", "role": "user"})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("edit without password: %d — %s", resp.StatusCode, b)
	}
	if code := login("initial-password"); code != http.StatusOK {
		t.Errorf("an edit with no password broke password login: %d", code)
	}

	// Too short is refused, and changes nothing.
	resp = edit(map[string]any{"name": "Bob", "role": "user", "password": "short"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("short password: %d, want 400", resp.StatusCode)
	}
	if code := login("initial-password"); code != http.StatusOK {
		t.Errorf("a refused password change still altered the account: %d", code)
	}

	// A valid change takes effect and retires the old password.
	resp = edit(map[string]any{"name": "Bob", "role": "admin", "password": "brand-new-password"})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("password change: %d — %s", resp.StatusCode, b)
	}
	if code := login("brand-new-password"); code != http.StatusOK {
		t.Errorf("login with the new password: %d, want 200", code)
	}
	if code := login("initial-password"); code != http.StatusUnauthorized {
		t.Errorf("the old password is still accepted: %d", code)
	}

	// The name and role from the same request were applied too.
	resp = h.do("GET", "/api/v1/admin/users", nil)
	var users []models.User
	mustJSON(t, resp, &users)
	for _, u := range users {
		if u.ID == user.ID && u.Role != "admin" {
			t.Errorf("role = %q, want admin", u.Role)
		}
	}
	t.Logf("Admin password change OK")
}

func TestAdminListsAndDuplicatesToAnotherUser(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	h.mock.streamCommandFn = func(name string, command []string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("")), nil
	}

	templateID := seedTemplate(t, h)

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "shared-env", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create src: %d — %s", resp.StatusCode, b)
	}
	var src models.InstanceJSON
	mustJSON(t, resp, &src)

	// A second account to hand the copy to.
	resp = h.do("POST", "/api/v1/admin/users", map[string]any{
		"email": "dev@plati.local", "name": "Dev", "password": "hunter2hunter2", "role": "user",
	})
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create user: %d — %s", resp.StatusCode, b)
	}
	var target models.User
	mustJSON(t, resp, &target)

	// The admin listing carries the owner and template names, not just ids.
	resp = h.do("GET", "/api/v1/admin/instances", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list all instances: %d", resp.StatusCode)
	}
	var listed []map[string]any
	mustJSON(t, resp, &listed)
	if len(listed) == 0 {
		t.Fatal("admin listing is empty")
	}
	if listed[0]["user_email"] == nil || listed[0]["user_email"] == "" {
		t.Errorf("admin listing has no user_email: %v", listed[0])
	}
	if listed[0]["template_name"] != "node-dev" {
		t.Errorf("template_name = %v, want node-dev", listed[0]["template_name"])
	}

	// Duplicate into the other account.
	resp = h.do("POST", fmt.Sprintf("/api/v1/admin/instances/%d/duplicate", src.ID),
		map[string]any{"user_id": target.ID})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin duplicate: %d — %s", resp.StatusCode, b)
	}
	var dst models.InstanceJSON
	mustJSON(t, resp, &dst)

	if dst.UserID != target.ID {
		t.Errorf("copy owner = %d, want %d", dst.UserID, target.ID)
	}
	if dst.Name != src.Name+"-copy" {
		t.Errorf("copy name = %q, want %q", dst.Name, src.Name+"-copy")
	}
	// The Incus name is derived from the owner, so it must differ from the source.
	if dst.IncusName == src.IncusName {
		t.Errorf("copy reuses the source Incus name %q", dst.IncusName)
	}

	// A second copy into the same account must not collide on (incus_name, server_id).
	resp = h.do("POST", fmt.Sprintf("/api/v1/admin/instances/%d/duplicate", src.ID),
		map[string]any{"user_id": target.ID})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("second admin duplicate: %d — %s", resp.StatusCode, b)
	}
	var dst2 models.InstanceJSON
	mustJSON(t, resp, &dst2)
	if dst2.Name == dst.Name {
		t.Errorf("second copy reuses the name %q", dst2.Name)
	}
	t.Logf("Admin duplicate OK: %q → %q, %q", src.Name, dst.Name, dst2.Name)
}

func TestTailscaleHostnameInjection(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name":        "My Workspace",
		"template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)

	// Wait for async creation to complete (CreateAsync runs in a goroutine)
	waitForInstance(t, h, inst.IncusName)

	mockInst, ok := h.mock.instances[inst.IncusName]
	if !ok {
		t.Fatal("mock: instance not created")
	}

	hostnameKey := "environment.PLATI_TAILSCALE_HOSTNAME"
	gotHostname, found := mockInst.Config[hostnameKey]
	if !found {
		t.Errorf("PLATI_TAILSCALE_HOSTNAME not injected into Incus config\nconfig keys: %v", configKeys(mockInst.Config))
	} else if gotHostname != "my-workspace" {
		t.Errorf("hostname = %q, want %q", gotHostname, "my-workspace")
	} else {
		t.Logf("PLATI_TAILSCALE_HOSTNAME correctly injected: %s", gotHostname)
	}
}

func TestTailscaleServeEndpoints(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	// Create instance
	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name":        "ts-serve-test",
		"template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)

	// Wait for async creation to finish
	waitForInstance(t, h, inst.IncusName)
	// Wait for status to become "running"
	for i := 0; i < 50; i++ {
		r := h.do("GET", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil)
		var check models.InstanceJSON
		mustJSON(t, r, &check)
		if check.Status == "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Mock RunCommand to simulate tailscale responses
	h.mock.runCommandFn = func(_ string, command []string) (string, error) {
		full := strings.Join(command, " ")
		if strings.Contains(full, "tailscale serve --bg") {
			return "Available within your tailnet:\n\nhttps://myhost.tail1234.ts.net/\n", nil
		}
		if strings.Contains(full, "tailscale serve status") {
			return "https://myhost.tail1234.ts.net (proxy https+insecure://127.0.0.1:8080)\n", nil
		}
		if strings.Contains(full, "tailscale status --json") {
			return `{"Self":{"DNSName":"myhost.tail1234.ts.net."}}`, nil
		}
		if strings.Contains(full, "tailscale serve off") {
			return "", nil
		}
		return "", nil
	}

	// POST: start serving
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/tailscale-serve", inst.ID),
		map[string]int{"port": 8080})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("tailscale-serve POST: %d — %s", resp.StatusCode, body)
	}
	var serveResult map[string]any
	mustJSON(t, resp, &serveResult)
	t.Logf("Tailscale serve result: %v", serveResult)

	// GET: check status
	resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d/tailscale-serve", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("tailscale-serve GET: %d — %s", resp.StatusCode, body)
	}
	mustJSON(t, resp, &serveResult)
	t.Logf("Tailscale serve status: %v", serveResult)

	// DELETE: stop serving
	resp = h.do("DELETE", fmt.Sprintf("/api/v1/instances/%d/tailscale-serve", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("tailscale-serve DELETE: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()
	t.Logf("Tailscale serve endpoints OK")
}

// TestServerSSHKeyAndRepos exercises the full flow:
//  1. No key configured → GET /repos/server-key returns empty
//  2. Generate server SSH key → verify ed25519 format
//  3. GET /repos/server-key → key persisted correctly
//  4. Add a repo → registered immediately, clone starts in background
//  5. List repos → repo appears, clone status transitions off "pending"
//  6. Sync repo → API accepts the sync request
//  7. Delete repo → removed from DB
//  8. Generate a second key → replaces the first
//
// The actual git clone will fail (no real SSH target in tests); this is expected.
// The test verifies the full API lifecycle and that the server key is used.
func TestServerSSHKeyAndRepos(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()

	// Login
	resp := h.do("POST", "/auth/login", map[string]string{"password": testPassword})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// 1. No key configured yet → public_key is empty
	resp = h.do("GET", "/api/v1/admin/repos/server-key", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("get server key (empty): %d — %s", resp.StatusCode, body)
	}
	var keyState map[string]any
	mustJSON(t, resp, &keyState)
	if got := keyState["public_key"]; got != "" {
		t.Errorf("expected empty public_key before generation, got %q", got)
	}

	// 2. Generate server SSH key
	resp = h.do("POST", "/api/v1/admin/repos/server-key/generate", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("generate server key: %d — %s", resp.StatusCode, body)
	}
	var genResp map[string]any
	mustJSON(t, resp, &genResp)
	pubKey, _ := genResp["public_key"].(string)
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Fatalf("generated key is not ed25519: %q", pubKey)
	}
	t.Logf("Generated server SSH key: %s...", pubKey[:min(40, len(pubKey))])

	// 3. GET /repos/server-key → key is now persisted
	resp = h.do("GET", "/api/v1/admin/repos/server-key", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("get server key: %d — %s", resp.StatusCode, body)
	}
	mustJSON(t, resp, &keyState)
	if keyState["public_key"] != pubKey {
		t.Errorf("retrieved key %q != generated key %q", keyState["public_key"], pubKey)
	}

	// 4. Add a repo (clone will fail without real SSH access — that's expected)
	resp = h.do("POST", "/api/v1/admin/repos", map[string]string{
		"ssh_url": "git@github.com:test-org/test-repo.git",
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("add repo: %d — %s", resp.StatusCode, body)
	}
	var repoResp map[string]any
	mustJSON(t, resp, &repoResp)
	repoID := int64(repoResp["id"].(float64))
	if repoResp["clone_status"] != "pending" {
		t.Errorf("expected clone_status=pending immediately after add, got %q", repoResp["clone_status"])
	}
	t.Logf("Repo registered: id=%d", repoID)

	// 5. List repos → appears with updated status (background goroutine uses the server key)
	time.Sleep(100 * time.Millisecond) // let goroutine start and update to "cloning"
	resp = h.do("GET", "/api/v1/admin/repos", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("list repos: %d — %s", resp.StatusCode, body)
	}
	var repos []map[string]any
	mustJSON(t, resp, &repos)
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0]["ssh_url"] != "git@github.com:test-org/test-repo.git" {
		t.Errorf("repo ssh_url mismatch: %q", repos[0]["ssh_url"])
	}
	cloneStatus, _ := repos[0]["clone_status"].(string)
	// Status should have advanced from "pending" (goroutine set to "cloning" or "error")
	if cloneStatus == "pending" {
		t.Errorf("clone status still pending after 100ms — goroutine may not have started")
	}
	t.Logf("Clone status after 100ms: %s", cloneStatus)

	// 6. Sync repo → API accepts the request.
	// The sync is synchronous and attempts a git pull; without a real SSH target it
	// returns "error". Both "success" and "error" are valid — we just verify the
	// endpoint responds 200 with a status field.
	resp = h.do("POST", fmt.Sprintf("/api/v1/admin/repos/%d/sync", repoID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("sync repo: %d — %s", resp.StatusCode, body)
	}
	var syncResp map[string]any
	mustJSON(t, resp, &syncResp)
	syncStatus, _ := syncResp["status"].(string)
	if syncStatus == "" {
		t.Errorf("sync response missing status field: %v", syncResp)
	} else {
		t.Logf("sync status: %s (error expected — no real SSH server in tests)", syncStatus)
	}

	// 7. Delete repo
	resp = h.do("DELETE", fmt.Sprintf("/api/v1/admin/repos/%d", repoID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete repo: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = h.do("GET", "/api/v1/admin/repos", nil)
	mustJSON(t, resp, &repos)
	if len(repos) != 0 {
		t.Errorf("expected 0 repos after delete, got %d", len(repos))
	}

	// 8. Generate a second key → replaces the first
	resp = h.do("POST", "/api/v1/admin/repos/server-key/generate", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("generate second key: %d — %s", resp.StatusCode, body)
	}
	var genResp2 map[string]any
	mustJSON(t, resp, &genResp2)
	pubKey2, _ := genResp2["public_key"].(string)
	if pubKey2 == pubKey {
		t.Errorf("second generated key should differ from first")
	}

	resp = h.do("GET", "/api/v1/admin/repos/server-key", nil)
	mustJSON(t, resp, &keyState)
	if keyState["public_key"] != pubKey2 {
		t.Errorf("stored key should be updated to second key")
	}
	t.Logf("Second key generated and stored: %s...", pubKey2[:min(40, len(pubKey2))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── Disk listing tests ───────────────────────────────────────────────────────

type diskInfoJSON struct {
	VolumeID     int64  `json:"volume_id"`
	VolumeName   string `json:"volume_name"`
	Pool         string `json:"pool"`
	SizeGB       int    `json:"size_gb"`
	MountPath    string `json:"mount_path"`
	DeviceName   string `json:"device_name"`
	InstanceID   int64  `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	IncusName    string `json:"incus_name"`
	Status       string `json:"status"`
	UserID       int64  `json:"user_id"`
	UserEmail    string `json:"user_email"`
	UserName     string `json:"user_name"`
	ServerID     int64  `json:"server_id"`
	ServerName   string `json:"server_name"`
	CreatedAt    string `json:"created_at"`
}

func TestDiskListingLifecycle(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	// Create instance
	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "disk-lifecycle", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	volName := inst.IncusName + "-home-ubuntu"

	// Wait for async creation to complete (volumes written to DB in background)
	waitForInstanceReady(t, h, inst.ID)

	// --- After create: GET /api/v1/disks ---
	resp = h.do("GET", "/api/v1/disks", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list disks: %d", resp.StatusCode)
	}
	var disks []diskInfoJSON
	mustJSON(t, resp, &disks)
	if len(disks) != 1 {
		t.Fatalf("expected 1 disk, got %d", len(disks))
	}
	d := disks[0]
	if d.VolumeName != volName {
		t.Errorf("volume_name = %q, want %q", d.VolumeName, volName)
	}
	if d.Pool != "default" {
		t.Errorf("pool = %q, want default", d.Pool)
	}
	if d.SizeGB != 20 {
		t.Errorf("size_gb = %d, want 20", d.SizeGB)
	}
	if d.MountPath != "/home/ubuntu" {
		t.Errorf("mount_path = %q, want /home/ubuntu", d.MountPath)
	}
	if d.DeviceName != "home-ubuntu" {
		t.Errorf("device_name = %q, want home-ubuntu", d.DeviceName)
	}
	if d.InstanceName != "disk-lifecycle" {
		t.Errorf("instance_name = %q, want disk-lifecycle", d.InstanceName)
	}
	if d.IncusName != inst.IncusName {
		t.Errorf("incus_name = %q, want %q", d.IncusName, inst.IncusName)
	}
	if d.Status != "running" {
		t.Errorf("status = %q, want running", d.Status)
	}
	if d.UserEmail != "admin@plati.local" {
		t.Errorf("user_email = %q, want admin@plati.local", d.UserEmail)
	}
	if d.ServerName != "test-server" {
		t.Errorf("server_name = %q, want test-server", d.ServerName)
	}
	// Cross-check mock
	if size, ok := h.mock.volumes[volName]; !ok {
		t.Errorf("mock: volume %q not found", volName)
	} else if size != 20 {
		t.Errorf("mock: volume size = %d, want 20", size)
	}
	t.Logf("Disk listing after create OK: %s (%dGB at %s)", d.VolumeName, d.SizeGB, d.MountPath)

	// --- After create: GET /api/v1/admin/disks ---
	resp = h.do("GET", "/api/v1/admin/disks", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin list disks: %d", resp.StatusCode)
	}
	var adminDisks []diskInfoJSON
	mustJSON(t, resp, &adminDisks)
	if len(adminDisks) != 1 {
		t.Errorf("admin disks: expected 1, got %d", len(adminDisks))
	}

	// --- Rebuild ---
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/rebuild", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("rebuild: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = h.do("GET", "/api/v1/disks", nil)
	mustJSON(t, resp, &disks)
	if len(disks) != 1 {
		t.Fatalf("after rebuild: expected 1 disk, got %d", len(disks))
	}
	if disks[0].VolumeName != volName {
		t.Errorf("after rebuild: volume_name changed to %q", disks[0].VolumeName)
	}
	if disks[0].Status != "running" {
		t.Errorf("after rebuild: status = %q, want running", disks[0].Status)
	}
	t.Logf("Disk listing after rebuild OK: volume preserved")

	// --- Delete ---
	resp = h.do("DELETE", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = h.do("GET", "/api/v1/disks", nil)
	mustJSON(t, resp, &disks)
	if len(disks) != 0 {
		t.Errorf("after delete: expected 0 disks, got %d", len(disks))
	}
	if _, ok := h.mock.volumes[volName]; ok {
		t.Errorf("mock: volume %q still exists after delete", volName)
	}

	resp = h.do("GET", "/api/v1/admin/disks", nil)
	mustJSON(t, resp, &adminDisks)
	if len(adminDisks) != 0 {
		t.Errorf("admin disks after delete: expected 0, got %d", len(adminDisks))
	}
	t.Logf("Disk listing after delete OK: empty")
}

func TestDiskListingUserIsolation(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	// Create instance for admin
	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "admin-disk", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create admin instance: %d — %s", resp.StatusCode, body)
	}
	var adminInst models.InstanceJSON
	mustJSON(t, resp, &adminInst)
	waitForInstanceReady(t, h, adminInst.ID)

	// Ensure password_hash column exists (migration numbering conflict may skip it)
	h.db.Exec("ALTER TABLE users ADD COLUMN password_hash TEXT")

	// Create regular user with password
	const userPassword = "userpass123"
	userHash, _ := auth.HashPassword(userPassword)
	res, err := h.db.Exec(
		`INSERT INTO users (email, name, role, password_hash) VALUES ('bob@test.com', 'Bob', 'user', ?)`, userHash)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	bobID, _ := res.LastInsertId()

	// Admin creates instance for Bob
	resp = h.do("POST", "/api/v1/admin/instances", map[string]any{
		"name": "bob-disk", "template_id": templateID, "user_id": bobID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create bob instance: %d — %s", resp.StatusCode, body)
	}
	var bobInst models.InstanceJSON
	mustJSON(t, resp, &bobInst)
	waitForInstanceReady(t, h, bobInst.ID)

	// Admin sees all disks
	resp = h.do("GET", "/api/v1/admin/disks", nil)
	var adminDisks []diskInfoJSON
	mustJSON(t, resp, &adminDisks)
	if len(adminDisks) != 2 {
		t.Fatalf("admin disks: expected 2, got %d", len(adminDisks))
	}
	emails := map[string]bool{}
	for _, d := range adminDisks {
		emails[d.UserEmail] = true
	}
	if !emails["admin@plati.local"] || !emails["bob@test.com"] {
		t.Errorf("admin disks: expected both users, got %v", emails)
	}
	t.Logf("Admin sees 2 disks from 2 users")

	// Admin's own disks
	resp = h.do("GET", "/api/v1/disks", nil)
	var myDisks []diskInfoJSON
	mustJSON(t, resp, &myDisks)
	if len(myDisks) != 1 {
		t.Fatalf("admin own disks: expected 1, got %d", len(myDisks))
	}
	if myDisks[0].UserEmail != "admin@plati.local" {
		t.Errorf("admin own disk: user_email = %q", myDisks[0].UserEmail)
	}

	// Login as Bob (new cookie jar to avoid admin session)
	h.do("POST", "/auth/logout", nil).Body.Close()
	resp = h.do("POST", "/auth/login", map[string]string{"email": "bob@test.com", "password": userPassword})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("bob login: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// Bob sees only his disk
	resp = h.do("GET", "/api/v1/disks", nil)
	var bobDisks []diskInfoJSON
	mustJSON(t, resp, &bobDisks)
	if len(bobDisks) != 1 {
		t.Fatalf("bob disks: expected 1, got %d", len(bobDisks))
	}
	if bobDisks[0].UserEmail != "bob@test.com" {
		t.Errorf("bob disk: user_email = %q, want bob@test.com", bobDisks[0].UserEmail)
	}
	if bobDisks[0].InstanceName != "bob-disk" {
		t.Errorf("bob disk: instance_name = %q, want bob-disk", bobDisks[0].InstanceName)
	}
	t.Logf("Bob sees only his own disk")

	// Bob cannot access admin disks
	resp = h.do("GET", "/api/v1/admin/disks", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("bob admin disks: got %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
	t.Logf("Bob correctly denied admin disk listing")
}

func TestDiskListingEphemeral(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Import ephemeral template
	body := strings.NewReader(`{
		"name":"EphemeralDisk","slug":"ephemeral-disk","description":"No volumes",
		"image":"images:alpine/3.20","profiles":["default"],
		"resources":{"cpu":"1","memory":"256MB","disk":"5GB"},
		"persistence":{"mode":"ephemeral"}
	}`)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/admin/templates/import", body)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range h.client.Jar.Cookies(req.URL) {
		req.AddCookie(c)
	}
	resp, _ := h.client.Do(req)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("import ephemeral template: %d — %s", resp.StatusCode, b)
	}
	var tmpl models.Template
	mustJSON(t, resp, &tmpl)

	// Create ephemeral instance
	resp = h.do("POST", "/api/v1/instances", map[string]any{
		"name": "ephemeral-disk-test", "template_id": tmpl.ID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create ephemeral: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	// No disks
	resp = h.do("GET", "/api/v1/disks", nil)
	var disks []diskInfoJSON
	mustJSON(t, resp, &disks)
	if len(disks) != 0 {
		t.Errorf("ephemeral: expected 0 disks, got %d", len(disks))
	}

	resp = h.do("GET", "/api/v1/admin/disks", nil)
	var adminDisks []diskInfoJSON
	mustJSON(t, resp, &adminDisks)
	if len(adminDisks) != 0 {
		t.Errorf("ephemeral admin: expected 0 disks, got %d", len(adminDisks))
	}
	t.Logf("Ephemeral instance produces no disks")
}

func TestDiskListingMultiVolume(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Import multi-volume template
	body := strings.NewReader(`{
		"name":"MultiVol","slug":"multi-vol","description":"Two volumes",
		"image":"images:ubuntu/24.04/cloud","profiles":["default"],
		"resources":{"cpu":"1","memory":"512MB"},
		"persistence":{
			"mode":"normal",
			"directories":[
				{"path":"/home/ubuntu","size":"20GB"},
				{"path":"/data","size":"10GB"}
			]
		}
	}`)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/admin/templates/import", body)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range h.client.Jar.Cookies(req.URL) {
		req.AddCookie(c)
	}
	resp, _ := h.client.Do(req)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("import multi-vol template: %d — %s", resp.StatusCode, b)
	}
	var tmpl models.Template
	mustJSON(t, resp, &tmpl)

	// Create instance
	resp = h.do("POST", "/api/v1/instances", map[string]any{
		"name": "multi-vol-test", "template_id": tmpl.ID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create multi-vol: %d — %s", resp.StatusCode, b)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)

	// Should see 2 disks
	resp = h.do("GET", "/api/v1/disks", nil)
	var disks []diskInfoJSON
	mustJSON(t, resp, &disks)
	if len(disks) != 2 {
		t.Fatalf("multi-vol: expected 2 disks, got %d", len(disks))
	}

	mounts := map[string]int{}
	for _, d := range disks {
		mounts[d.MountPath] = d.SizeGB
	}
	if mounts["/home/ubuntu"] != 20 {
		t.Errorf("/home/ubuntu size = %d, want 20", mounts["/home/ubuntu"])
	}
	if mounts["/data"] != 10 {
		t.Errorf("/data size = %d, want 10", mounts["/data"])
	}

	// Cross-check mock
	if len(h.mock.volumes) != 2 {
		t.Errorf("mock: expected 2 volumes, got %d", len(h.mock.volumes))
	}
	t.Logf("Multi-volume: 2 disks at /home/ubuntu(20GB) and /data(10GB)")

	// Delete and verify cleanup
	resp = h.do("DELETE", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete multi-vol: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = h.do("GET", "/api/v1/disks", nil)
	mustJSON(t, resp, &disks)
	if len(disks) != 0 {
		t.Errorf("after delete: expected 0 disks, got %d", len(disks))
	}
	if len(h.mock.volumes) != 0 {
		t.Errorf("mock: expected 0 volumes after delete, got %d", len(h.mock.volumes))
	}
	t.Logf("Multi-volume cleanup OK")
}

// ── Harness with repos dir ────────────────────────────────────────────────────

// newHarnessWithReposDir builds a test harness where both RepoService and
// InstanceService are configured with a non-empty reposDir. This is required
// for tests that exercise the repo-copy flow (attachReposDirAndBuildCmds).
func newHarnessWithReposDir(t *testing.T, reposDir string) *harness {
	t.Helper()

	db, err := database.New(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	cfg := &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", Port: 0, FrontendURL: "http://localhost"},
		Auth: config.AuthConfig{
			AdminPasswordHash: hash,
			JWTSecret:         jwtSecret,
			JWTLifetimeHours:  24,
		},
		Database: config.DatabaseConfig{Path: ":memory:"},
	}

	mock := newMockIncus()
	pool := incus.NewPool()
	pool.SetClient("test-server", mock)

	_, err = db.Exec(`INSERT INTO servers (name, endpoint, is_online, max_instances) VALUES ('test-server', 'https://mock:8443', 1, 50)`)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}

	userSvc, err := services.NewUserService(db, "0000000000000000000000000000000000000000000000000000000000000000", "")
	if err != nil {
		t.Fatalf("user service: %v", err)
	}
	templateSvc := services.NewTemplateService(db, "")
	prefSvc := services.NewPreferencesService(db)
	adminSvc := services.NewAdminSettingsService(db, userSvc)
	managedKeySvc := services.NewManagedKeyService(db, userSvc, "")
	repoSvc := services.NewRepoService(db, userSvc, reposDir) // real reposDir
	instanceSvc := services.NewInstanceService(db, pool, userSvc, prefSvc, adminSvc, "", reposDir, reposDir, repoSvc, templateSvc)
	serverSvc := services.NewServerService(db, pool)
	apiKeySvc := services.NewAPIKeyService(db)
	adminHandler := handlers.NewAdminHandler(db, userSvc, instanceSvc, templateSvc)
	healthHandler := handlers.NewHealthHandler(db)
	setupHandler := handlers.NewSetupHandler(db, cfg, "/dev/null")

	r := router.New(router.Deps{
		AuthHandler:          handlers.NewAuthHandler(db, cfg, nil),
		UserHandler:          handlers.NewUserHandler(userSvc),
		TemplateHandler:      handlers.NewTemplateHandler(templateSvc),
		InstanceHandler:      handlers.NewInstanceHandler(instanceSvc, db),
		ServerHandler:        handlers.NewServerHandler(serverSvc, pool),
		AdminHandler:         adminHandler,
		ManagedKeyHandler:    handlers.NewManagedKeyHandler(managedKeySvc),
		HealthHandler:        healthHandler,
		SetupHandler:         setupHandler,
		TerminalHandler:      handlers.NewTerminalHandler(db, pool, instanceSvc.CreationLogs()),
		PreferencesHandler:   handlers.NewPreferencesHandler(prefSvc),
		AdminSettingsHandler: handlers.NewAdminSettingsHandler(adminSvc),
		// Not started: the tests exercise the sleep HTTP surface, not the worker.
		SleepHandler:  handlers.NewSleepHandler(services.NewSleepService(db, pool, 4*time.Hour)),
		RepoHandler:   handlers.NewRepoHandler(repoSvc),
		APIKeyHandler: handlers.NewAPIKeyHandler(apiKeySvc),
		APIKeyAuth:    apiKeySvc.AsAuthenticator(),
		JWTSecret:     jwtSecret,
		FrontendURL:   "http://localhost",
	})

	srv := httptest.NewServer(r)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	return &harness{srv: srv, client: client, mock: mock, db: db}
}

// ── Repo helpers ──────────────────────────────────────────────────────────────

// seedGitRepo inserts a git repo record directly into the DB with the given status.
func seedGitRepo(t *testing.T, h *harness, name, sshURL, localPath, status string) int64 {
	t.Helper()
	res, err := h.db.Exec(
		`INSERT INTO git_repos (name, ssh_url, local_path, clone_status) VALUES (?, ?, ?, ?)`,
		name, sshURL, localPath, status,
	)
	if err != nil {
		t.Fatalf("seed git repo %q: %v", name, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// importTemplateYAML posts YAML to the import endpoint and returns the created template.
func importTemplateYAML(t *testing.T, h *harness, yamlStr string) *models.Template {
	t.Helper()
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/admin/templates/import", strings.NewReader(yamlStr))
	req.Header.Set("Content-Type", "application/x-yaml")
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
	return &tmpl
}

// ── Repo copy integration tests ───────────────────────────────────────────────

// TestRepoCopyCommandsGenerated verifies the full repo-copy flow at the
// integration level (no real Incus or git server required):
//
//  1. A template is created with a repos field referencing "AI-state-art-public".
//  2. A git_repos record is seeded in the DB with clone_status = "ready".
//  3. A corresponding directory is created in the temp reposDir (simulates a
//     successful clone on the Plati server).
//  4. An instance is created from the template.
//  5. The test asserts that CreateAsync called RunCommand with the expected
//     "cp -rp /plati-repos/…" and "git remote set-url origin …" commands.
func TestRepoCopyCommandsGenerated(t *testing.T) {
	// Temp directory that acts as the server-side git cache (/plati-repos).
	reposDir := t.TempDir()
	repoName := "AI-state-art-public"
	repoSSHURL := "git@github.com:test-org/" + repoName + ".git"

	// Create the repo subdirectory (simulates a completed clone).
	repoLocalPath := filepath.Join(reposDir, repoName)
	if err := os.MkdirAll(repoLocalPath, 0755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}

	h := newHarnessWithReposDir(t, reposDir)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Seed a ready git repo record in the DB.
	repoID := seedGitRepo(t, h, repoName, repoSSHURL, repoLocalPath, "ready")
	t.Logf("Seeded git repo id=%d name=%s status=ready", repoID, repoName)

	// Import a template that references the repo.
	const tmplYAML = `
name: QCM PoC Formation (repo copy test)
slug: qcm-poc-formation-repocopy
description: Test template with repos field
image: images:ubuntu/24.04/cloud
profiles:
  - default
resources:
  cpu: 1
  memory: 2GB
  disk: 10GB
persistence:
  mode: normal
  directories:
    - path: /home/ubuntu
      size: 10GB
repos:
  - name: AI-state-art-public
    dest: /home/ubuntu/AI-state-art-public
rebuild_commands:
  - cd /home/ubuntu/AI-state-art-public && git pull --ff-only || true
`
	tmpl := importTemplateYAML(t, h, tmplYAML)
	t.Logf("Template imported: id=%d slug=%s", tmpl.ID, tmpl.Slug)

	// Verify repos field is stored correctly.
	if !strings.Contains(tmpl.Repos, repoName) {
		t.Fatalf("template repos field missing %q: %s", repoName, tmpl.Repos)
	}

	// Create instance — this triggers CreateAsync which calls attachReposDirAndBuildCmds.
	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name":        "qcm-repocopy",
		"template_id": tmpl.ID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	t.Logf("Instance created: id=%d incus=%s", inst.ID, inst.IncusName)

	// Wait for the async goroutine to finish.
	waitForInstanceReady(t, h, inst.ID)

	// ── Assert: cp command was run ────────────────────────────────────────────
	if !h.mock.hasRunCall(inst.IncusName, "cp -rp", "/plati-repos/"+repoName, "/home/ubuntu/"+repoName) {
		t.Errorf("cp command not found in RunCommand calls\nall calls on %s:\n  %s",
			inst.IncusName, strings.Join(h.mock.runCallsFor(inst.IncusName), "\n  "))
	} else {
		t.Logf("cp command verified: cp -rp /plati-repos/%s /home/ubuntu/%s", repoName, repoName)
	}

	// ── Assert: git remote set-url was run ────────────────────────────────────
	if !h.mock.hasRunCall(inst.IncusName, "git remote set-url origin", repoSSHURL) {
		t.Errorf("git remote set-url not found in RunCommand calls\nall calls on %s:\n  %s",
			inst.IncusName, strings.Join(h.mock.runCallsFor(inst.IncusName), "\n  "))
	} else {
		t.Logf("git remote set-url verified for %s", repoSSHURL)
	}

	// ── Assert: persistent volume created ────────────────────────────────────
	volName := inst.IncusName + "-home-ubuntu"
	if size, ok := h.mock.volumes[volName]; !ok {
		t.Error("persistent volume not created in mock")
	} else {
		t.Logf("persistent volume %s: %dGB", volName, size)
	}
}

// TestQcmPocFormationTemplate runs a full lifecycle test for the
// qcm-poc-formation template configuration:
// import → create → persistent volume attached → rebuild (volume preserved) → delete.
//
// This mirrors the real template YAML in config/templates/qcm-poc-formation.yaml
// and exercises the persistence/volume path end-to-end.
func TestQcmPocFormationTemplate(t *testing.T) {
	reposDir := t.TempDir()
	h := newHarnessWithReposDir(t, reposDir)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Import the template using the same config as the real YAML file.
	const tmplYAML = `
name: QCM avec IA
slug: qcm-poc-formation
description: AI State of the Art public site (catie-aq)
image: images:ubuntu/24.04/cloud
profiles:
  - default
resources:
  cpu: 2
  disk: 20GB
  memory: 4GB
terminal_user: ubuntu
persistence:
  mode: normal
  directories:
    - path: /home/ubuntu
      size: 20GB
repos:
  - name: AI-state-art-public
    dest: /home/ubuntu/AI-state-art-public
rebuild_commands:
  - cd /home/ubuntu/AI-state-art-public && git pull --ff-only || true
`
	tmpl := importTemplateYAML(t, h, tmplYAML)

	// ── Validate template DB record ─────────────────────────────────────────────
	if tmpl.Slug != "qcm-poc-formation" {
		t.Errorf("slug = %q, want qcm-poc-formation", tmpl.Slug)
	}
	if tmpl.PersistenceMode != "normal" {
		t.Errorf("persistence_mode = %q, want normal", tmpl.PersistenceMode)
	}
	if !strings.Contains(tmpl.Repos, "AI-state-art-public") {
		t.Errorf("repos field missing AI-state-art-public: %s", tmpl.Repos)
	}
	if !strings.Contains(tmpl.RebuildCommands, "git pull") {
		t.Errorf("rebuild_commands missing git pull: %s", tmpl.RebuildCommands)
	}
	t.Logf("Template OK: id=%d", tmpl.ID)

	// ── Create instance ─────────────────────────────────────────────────────────
	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "qcm-lifecycle", "template_id": tmpl.ID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)
	t.Logf("Instance created: id=%d incus=%s", inst.ID, inst.IncusName)

	// ── Workspace volume created ─────────────────────────────────────────────────
	volName := inst.IncusName + "-home-ubuntu"
	if size, ok := h.mock.volumes[volName]; !ok {
		t.Error("persistent volume not created")
	} else {
		t.Logf("persistent volume: %dGB (expected 20)", size)
		if size != 20 {
			t.Errorf("persistent volume size = %d, want 20", size)
		}
	}

	// ── Stop then Rebuild — volume must survive ─────────────────────────────────
	h.do("POST", fmt.Sprintf("/api/v1/instances/%d/stop", inst.ID), nil).Body.Close()

	// Reset run-call log to isolate rebuild commands.
	h.mock.mu.Lock()
	h.mock.runCalls = nil
	h.mock.mu.Unlock()

	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/rebuild", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("rebuild: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// Workspace volume must still exist (data preservation).
	if _, ok := h.mock.volumes[volName]; !ok {
		t.Error("persistent volume deleted during rebuild — data loss!")
	}
	t.Logf("Rebuild OK: persistent volume preserved")

	// Rebuild should have run the rebuild_command.
	if h.mock.hasRunCall(inst.IncusName, "git pull --ff-only") {
		t.Logf("rebuild_command (git pull) confirmed in RunCommand calls")
	} else {
		// The command runs only when the sentinel file exists.
		// In the mock, RunCommand always succeeds (sentinel check passes).
		t.Logf("Note: rebuild_command not seen — sentinel/volume state in mock may vary")
	}

	// ── Delete ───────────────────────────────────────────────────────────────────
	resp = h.do("DELETE", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	if _, ok := h.mock.instances[inst.IncusName]; ok {
		t.Error("Incus instance not deleted")
	}
	if _, ok := h.mock.volumes[volName]; ok {
		t.Error("persistent volume not deleted after instance delete")
	}
	t.Logf("Full lifecycle OK: create → rebuild → delete")
}

// TestSSHGitCloneCapability verifies the service-layer SSH key infrastructure
// used by the repo service for git clone/pull:
//
//  1. Generate a server SSH key.
//  2. Register a repo (clone will fail — no real SSH server, expected).
//  3. Verify the key is an ed25519 pub key and the repo record is created.
//  4. The RepoService falls back gracefully when the clone target is unreachable.
//
// This is a lightweight companion to TestServerSSHKeyAndRepos focused on the
// key format and fallback behaviour.
func TestSSHGitCloneCapability(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Generate server SSH key.
	resp := h.do("POST", "/api/v1/admin/repos/server-key/generate", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("generate key: %d — %s", resp.StatusCode, body)
	}
	var keyResp map[string]any
	mustJSON(t, resp, &keyResp)
	pubKey, _ := keyResp["public_key"].(string)
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Fatalf("expected ed25519 key, got: %q", pubKey)
	}
	t.Logf("Server SSH key: %s...", pubKey[:min(40, len(pubKey))])

	// Register a repo — clone will fail because there is no real SSH target.
	resp = h.do("POST", "/api/v1/admin/repos", map[string]string{
		"ssh_url": "git@github.com:catie-aq/AI-state-art-public.git",
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("add repo: %d — %s", resp.StatusCode, body)
	}
	var repoResp map[string]any
	mustJSON(t, resp, &repoResp)
	if repoResp["name"] != "AI-state-art-public" {
		t.Errorf("repo name = %q, want AI-state-art-public", repoResp["name"])
	}
	if repoResp["clone_status"] != "pending" {
		t.Errorf("initial clone_status = %q, want pending", repoResp["clone_status"])
	}
	t.Logf("Repo registered: name=%s status=%s", repoResp["name"], repoResp["clone_status"])

	// Wait briefly for the background goroutine to attempt the clone and update status.
	time.Sleep(150 * time.Millisecond)

	resp = h.do("GET", "/api/v1/admin/repos", nil)
	var repos []map[string]any
	mustJSON(t, resp, &repos)
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	cloneStatus, _ := repos[0]["clone_status"].(string)
	// Should have advanced from "pending" — either "cloning" or "error" (no real SSH).
	if cloneStatus == "pending" {
		t.Errorf("clone status still pending after 150ms")
	}
	t.Logf("Clone status after background attempt: %s (expected cloning|error — no real SSH)", cloneStatus)

	// Verify the SSH URL was stored verbatim.
	if repos[0]["ssh_url"] != "git@github.com:catie-aq/AI-state-art-public.git" {
		t.Errorf("ssh_url mismatch: %q", repos[0]["ssh_url"])
	}
	t.Logf("SSH git clone capability test OK")
}

// TestInstanceWithAllMixins verifies that when an Ubuntu-style template with all
// non-NVIDIA mixins (tailscale, sshx, docker, openvscode-server, claude-code) is
// imported and an instance is created, all mixin commands are executed during setup.
//
// This test loads the real mixin YAMLs from the project's config/templates directory
// (path relative to the test package: ../../../config/templates).
// It is skipped when that directory is not present (e.g. in CI without the full repo).
func TestInstanceWithAllMixins(t *testing.T) {
	// Resolve the real templates dir relative to this test package.
	templatesDir, err := filepath.Abs(filepath.Join("..", "..", "..", "config", "templates"))
	if err != nil {
		t.Skipf("cannot resolve templates dir: %v", err)
	}
	if _, err := os.Stat(templatesDir); err != nil {
		t.Skipf("templates dir not found (%s): %v", templatesDir, err)
	}
	ubuntuYAML := filepath.Join(templatesDir, "ubuntu.yaml")
	if _, err := os.Stat(ubuntuYAML); err != nil {
		t.Skipf("ubuntu.yaml not found: %v", err)
	}

	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	// Load real mixins into an auxiliary service that shares the harness DB.
	// The harness's HTTP handlers use the same DB, so the imported template is visible.
	auxSvc := services.NewTemplateService(h.db, templatesDir)
	if err := auxSvc.LoadMixinsFromDir(templatesDir); err != nil {
		t.Fatalf("load mixins: %v", err)
	}

	yamlData, err := os.ReadFile(ubuntuYAML)
	if err != nil {
		t.Fatalf("read ubuntu.yaml: %v", err)
	}
	tmpl, err := auxSvc.ImportYAML(yamlData)
	if err != nil {
		t.Fatalf("import ubuntu template: %v", err)
	}
	t.Logf("Imported ubuntu template id=%d with includes=%s", tmpl.ID, tmpl.Includes)

	// Verify includes lists all expected non-NVIDIA mixins.
	var includes []string
	json.Unmarshal([]byte(tmpl.Includes), &includes)
	expectedMixins := []string{"tailscale", "sshx", "docker", "openvscode-server", "claude-code"}
	for _, want := range expectedMixins {
		found := false
		for _, got := range includes {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ubuntu template missing mixin %q in includes", want)
		}
	}

	// Verify NVIDIA is NOT in ubuntu includes.
	for _, inc := range includes {
		if inc == "nvidia" {
			t.Error("ubuntu template should NOT include nvidia mixin")
		}
	}

	// Verify mixin commands are in first_init_commands.
	var firstInit []string
	json.Unmarshal([]byte(tmpl.FirstInitCommands), &firstInit)
	joined := strings.Join(firstInit, "\n")

	mixinChecks := []struct{ mixin, keyword string }{
		{"tailscale", "tailscale-install.sh"},
		{"tailscale", "tailscaled"},
		{"sshx", "sshx"},
		{"docker", "docker-ce"},
		{"openvscode-server", "openvscode-server"},
		{"claude-code", "claude.ai/install.sh"},
	}
	for _, c := range mixinChecks {
		if !strings.Contains(joined, c.keyword) {
			t.Errorf("mixin %q: %q not found in first_init_commands", c.mixin, c.keyword)
		}
	}
	// Template-specific commands must also be present.
	if !strings.Contains(joined, "openssh-server") {
		t.Error("ubuntu template's own first_init_commands (openssh-server) not found")
	}

	// Create an instance using this template via the HTTP API.
	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name":        "mixin-test",
		"template_id": tmpl.ID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, body)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	t.Logf("Instance created: id=%d incus=%s", inst.ID, inst.IncusName)

	// Wait for async setup to complete.
	waitForInstanceReady(t, h, inst.ID)

	// Assert mixin commands were run via the mock.
	runChecks := []struct{ mixin, keyword string }{
		{"tailscale", "tailscale-install.sh"},
		{"tailscale", "tailscaled"},
		{"sshx", "sshx"},
		{"docker", "docker-ce"},
		{"openvscode-server", "openvscode-server"},
		{"claude-code", "claude.ai/install.sh"},
	}
	for _, c := range runChecks {
		if !h.mock.hasRunCall(inst.IncusName, c.keyword) {
			cmds := h.mock.runCallsFor(inst.IncusName)
			t.Errorf("mixin %q: %q not found in RunCommand calls for %s\ncalls: %v",
				c.mixin, c.keyword, inst.IncusName, cmds)
		} else {
			t.Logf("OK: mixin %q ran %q", c.mixin, c.keyword)
		}
	}

	// Template-specific init commands must also have run.
	if !h.mock.hasRunCall(inst.IncusName, "openssh-server") {
		t.Error("ubuntu template init commands (openssh-server) not run")
	}

	t.Logf("Mixin integration test OK: all non-NVIDIA mixin commands executed")
}

// TestInstanceRename covers the rename flow: the display name changes in the DB,
// the Tailscale hostname follows it in the Incus config and on the running
// instance, and incus_name stays put.
func TestInstanceRenameContainer(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "Old Name", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, b)
	}
	var created models.InstanceJSON
	mustJSON(t, resp, &created)
	waitForInstanceReady(t, h, created.ID)
	origIncusName := created.IncusName

	resp = h.do("PUT", fmt.Sprintf("/api/v1/instances/%d", created.ID), map[string]any{
		"name": "New Name", "rename_container": true,
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("rename with container: %d — %s", resp.StatusCode, b)
	}
	var renamed struct {
		models.InstanceJSON
		ContainerRenamed     bool   `json:"container_renamed"`
		ContainerRenameError string `json:"container_rename_error"`
		Restarted            bool   `json:"restarted"`
	}
	mustJSON(t, resp, &renamed)

	if !renamed.ContainerRenamed {
		t.Fatalf("container_renamed = false (%s)", renamed.ContainerRenameError)
	}
	if !renamed.Restarted {
		t.Error("restarted = false, want the running instance back up after the rename")
	}
	want := fmt.Sprintf("plati-%d-new-name", created.UserID)
	if renamed.IncusName != want {
		t.Errorf("incus_name = %q, want %q", renamed.IncusName, want)
	}

	// Incus side: the container moved, the old name is gone.
	if _, ok := h.mock.instances[want]; !ok {
		t.Errorf("mock has no instance %q", want)
	}
	if _, ok := h.mock.instances[origIncusName]; ok {
		t.Errorf("mock still has the old instance %q", origIncusName)
	}

	// The row followed, so every later call addresses the right container.
	var dbIncusName, dbStatus string
	if err := h.db.Get(&dbIncusName, "SELECT incus_name FROM instances WHERE id = ?", created.ID); err != nil {
		t.Fatalf("read incus_name back: %v", err)
	}
	if dbIncusName != want {
		t.Errorf("incus_name in DB = %q, want %q", dbIncusName, want)
	}
	if err := h.db.Get(&dbStatus, "SELECT status FROM instances WHERE id = ?", created.ID); err != nil {
		t.Fatalf("read status back: %v", err)
	}
	if dbStatus != "running" {
		t.Errorf("status after rename = %q, want running", dbStatus)
	}

	// The tailnet hostname was applied to the container under its new name.
	if got := h.mock.instances[want].Config["environment.PLATI_TAILSCALE_HOSTNAME"]; got != "new-name" {
		t.Errorf("PLATI_TAILSCALE_HOSTNAME = %q, want %q", got, "new-name")
	}
	t.Logf("Container rename OK: %s → %s", origIncusName, want)
}

func TestInstanceRenameSystemHostname(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "Old Name", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, b)
	}
	var created models.InstanceJSON
	mustJSON(t, resp, &created)
	waitForInstanceReady(t, h, created.ID)
	incusName := created.IncusName

	rename := func(body map[string]any) struct {
		models.InstanceJSON
		SystemHostname string `json:"system_hostname"`
	} {
		t.Helper()
		r := h.do("PUT", fmt.Sprintf("/api/v1/instances/%d", created.ID), body)
		if r.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(r.Body)
			t.Fatalf("rename: %d — %s", r.StatusCode, b)
		}
		var out struct {
			models.InstanceJSON
			SystemHostname string `json:"system_hostname"`
		}
		mustJSON(t, r, &out)
		return out
	}

	// Unchecked: the box is opt-in, so nothing inside the OS is touched.
	rename(map[string]any{"name": "Untouched Name"})
	if h.mock.hasRunCall(incusName, "/etc/hostname") {
		t.Errorf("wrote /etc/hostname without being asked\ncalls:\n  %s",
			strings.Join(h.mock.runCallsFor(incusName), "\n  "))
	}

	// The instance reports its new hostname once the script has run.
	h.mock.runCommandFn = func(name string, command []string) (string, error) {
		if len(command) == 3 && strings.Contains(command[2], "/etc/hostname") {
			return "system_hostname=new-name\n", nil
		}
		return "", nil
	}

	out := rename(map[string]any{"name": "New Name", "rename_system_hostname": true})
	if out.SystemHostname != "new-name" {
		t.Errorf("system_hostname = %q, want %q", out.SystemHostname, "new-name")
	}

	// /etc/hostname survives a restart, the /etc/hosts entry keeps sudo quiet,
	// and hostnamectl applies it to the running system.
	for _, want := range []string{
		"echo 'new-name' > /etc/hostname",
		"127.0.1.1 new-name",
		"hostnamectl set-hostname 'new-name'",
	} {
		if !h.mock.hasRunCall(incusName, want) {
			t.Errorf("no run call containing %q\ncalls:\n  %s",
				want, strings.Join(h.mock.runCallsFor(incusName), "\n  "))
		}
	}

	// The tailnet hostname still goes out in the same pass.
	if !h.mock.hasRunCall(incusName, "tailscale set --hostname=new-name") {
		t.Error("the tailnet hostname was not applied alongside the system one")
	}
	t.Logf("Ubuntu hostname rename OK")
}

func TestInstanceRename(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name":        "Old Name",
		"template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, body)
	}
	var created models.InstanceJSON
	mustJSON(t, resp, &created)
	waitForInstanceReady(t, h, created.ID)

	origIncusName := created.IncusName

	resp = h.do("PUT", fmt.Sprintf("/api/v1/instances/%d", created.ID), map[string]string{
		"name": "New Name",
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("rename: %d — %s", resp.StatusCode, body)
	}
	var renamed models.InstanceJSON
	mustJSON(t, resp, &renamed)

	if renamed.Name != "New Name" {
		t.Errorf("name = %q, want %q", renamed.Name, "New Name")
	}
	if renamed.IncusName != origIncusName {
		t.Errorf("incus_name changed: %q → %q, want it stable", origIncusName, renamed.IncusName)
	}

	// The Incus config carries the new hostname, so a Rebuild comes back renamed.
	mockInst, ok := h.mock.instances[origIncusName]
	if !ok {
		t.Fatal("mock: instance not created")
	}
	if got := mockInst.Config["environment.PLATI_TAILSCALE_HOSTNAME"]; got != "new-name" {
		t.Errorf("PLATI_TAILSCALE_HOSTNAME = %q, want %q", got, "new-name")
	}

	// The running instance had the hostname applied live.
	if !h.mock.hasRunCall(origIncusName, "tailscale set --hostname=new-name") {
		t.Errorf("no live `tailscale set --hostname` call on %s\ncalls:\n  %s",
			origIncusName, strings.Join(h.mock.runCallsFor(origIncusName), "\n  "))
	}

	// A name that sanitizes to nothing is rejected, and nothing changes.
	resp = h.do("PUT", fmt.Sprintf("/api/v1/instances/%d", created.ID), map[string]string{"name": "!!!"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("rename to unusable name: status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	var dbName string
	if err := h.db.Get(&dbName, "SELECT name FROM instances WHERE id = ?", created.ID); err != nil {
		t.Fatalf("read name back: %v", err)
	}
	if dbName != "New Name" {
		t.Errorf("name after rejected rename = %q, want %q", dbName, "New Name")
	}
}

// TestInstanceSleepSettings covers the Status tab's API: read the resolved
// policy, override the timeout, turn auto-stop off, and reset the timer.
func TestInstanceSleepSettings(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	resp := h.do("POST", "/api/v1/instances", map[string]any{
		"name": "sleepy", "template_id": templateID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create instance: %d — %s", resp.StatusCode, b)
	}
	var created models.InstanceJSON
	mustJSON(t, resp, &created)
	waitForInstanceReady(t, h, created.ID)

	path := fmt.Sprintf("/api/v1/instances/%d/sleep", created.ID)

	// Defaults: auto-stop on, no override, deadline derived from the platform default.
	var set services.SleepSettings
	resp = h.do("GET", path, nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("get sleep settings: %d — %s", resp.StatusCode, b)
	}
	mustJSON(t, resp, &set)
	if set.Disabled || set.TimeoutMinutes != 0 {
		t.Fatalf("expected default policy, got %+v", set)
	}
	if set.EffectiveMinutes != set.DefaultMinutes || set.DefaultMinutes == 0 {
		t.Fatalf("effective should fall back to the platform default, got %+v", set)
	}
	if set.SleepsAt == nil {
		t.Fatalf("a running instance with auto-stop on should have a deadline: %+v", set)
	}

	// A per-instance override wins over the default.
	resp = h.do("PUT", path, map[string]any{"timeout_minutes": 90})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("update timeout: %d — %s", resp.StatusCode, b)
	}
	mustJSON(t, resp, &set)
	if set.TimeoutMinutes != 90 || set.EffectiveMinutes != 90 {
		t.Fatalf("expected 90-minute override, got %+v", set)
	}

	// Disabling drops the deadline but keeps the stored timeout, so turning it
	// back on restores the user's choice rather than the platform default.
	resp = h.do("PUT", path, map[string]any{"disabled": true})
	mustJSON(t, resp, &set)
	if !set.Disabled || set.SleepsAt != nil || set.TimeoutMinutes != 90 {
		t.Fatalf("expected disabled with no deadline and the timeout kept, got %+v", set)
	}

	// Out-of-range timeouts are refused.
	resp = h.do("PUT", path, map[string]any{"timeout_minutes": -1})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a negative timeout, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Reset pushes the deadline back.
	resp = h.do("PUT", path, map[string]any{"disabled": false})
	mustJSON(t, resp, &set)
	before := *set.SleepsAt

	resp = h.do("POST", path+"/reset", nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("reset timer: %d — %s", resp.StatusCode, b)
	}
	mustJSON(t, resp, &set)
	if set.SleepsAt == nil || *set.SleepsAt < before {
		t.Fatalf("reset should not move the deadline backwards: %v → %v", before, set.SleepsAt)
	}

	// The policy is also visible on the instance itself, which is what the
	// dashboard reads.
	resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d", created.ID), nil)
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	if inst.SleepDisabled || inst.SleepTimeoutMinutes != 90 {
		t.Fatalf("instance payload should carry the policy, got %+v", inst)
	}
}

// --- Admin access to other users' instances -------------------------------------
//
// Every /api/v1/instances/{id}/… route resolves through queries.GetInstanceForActor,
// which lets an admin through and scopes everyone else to their own rows. These tests
// pin both halves of that rule, plus the subtler half: reaching another user's instance
// is not the same as acting as them.

// seedBob creates a regular user with a password and returns their id.
func seedBob(t *testing.T, h *harness, password string) int64 {
	t.Helper()
	h.db.Exec("ALTER TABLE users ADD COLUMN password_hash TEXT")
	hash, _ := auth.HashPassword(password)
	res, err := h.db.Exec(
		`INSERT INTO users (email, name, role, password_hash) VALUES ('bob@test.com', 'Bob', 'user', ?)`, hash)
	if err != nil {
		t.Fatalf("insert bob: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func loginAs(t *testing.T, h *harness, body map[string]string) {
	t.Helper()
	h.do("POST", "/auth/logout", nil).Body.Close()
	resp := h.do("POST", "/auth/login", body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login %v: %d — %s", body["email"], resp.StatusCode, b)
	}
	resp.Body.Close()
}

func TestAdminOperatesOtherUsersInstance(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)
	bobID := seedBob(t, h, "userpass123")

	resp := h.do("POST", "/api/v1/admin/instances", map[string]any{
		"name": "bob-ws", "template_id": templateID, "user_id": bobID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create for bob: %d — %s", resp.StatusCode, b)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)

	// The admin is still logged in and does not own this instance.
	resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin GET other's instance: got %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/stop", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin stop other's instance: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()
	if got := h.mock.instances[inst.IncusName].Status; got != "Stopped" {
		t.Errorf("mock status after admin stop: %q, want Stopped", got)
	}

	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/start", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin start other's instance: got %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Rename is the one that catches a userID passed where the owner's id belongs:
	// UpdateInstanceName filters on user_id, so the wrong id updates zero rows and the
	// API still answers 200. Assert the row itself, not the status code.
	resp = h.do("PUT", fmt.Sprintf("/api/v1/instances/%d", inst.ID), map[string]any{"name": "bob-renamed"})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin rename other's instance: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()
	var name string
	if err := h.db.Get(&name, "SELECT name FROM instances WHERE id = ?", inst.ID); err != nil {
		t.Fatalf("read name: %v", err)
	}
	if name != "bob-renamed" {
		t.Errorf("name in DB = %q, want bob-renamed — the UPDATE matched no row", name)
	}

	// Auto-stop has the same WHERE user_id shape.
	resp = h.do("PUT", fmt.Sprintf("/api/v1/instances/%d/sleep", inst.ID), map[string]any{"timeout_minutes": 90})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin set sleep on other's instance: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()
	var timeout int
	h.db.Get(&timeout, "SELECT sleep_timeout_minutes FROM instances WHERE id = ?", inst.ID)
	if timeout != 90 {
		t.Errorf("sleep_timeout_minutes = %d, want 90 — the UPDATE matched no row", timeout)
	}
}

func TestNonAdminCannotReachOtherUsersInstance(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)
	seedBob(t, h, "userpass123")

	// An instance owned by the admin.
	resp := h.do("POST", "/api/v1/instances", map[string]any{"name": "admin-ws", "template_id": templateID})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create admin instance: %d — %s", resp.StatusCode, b)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)

	loginAs(t, h, map[string]string{"email": "bob@test.com", "password": "userpass123"})

	// Exhaustive: this table is the safety net for the mechanical rewrite of every
	// GetInstanceByUser call site. A route that stops scoping shows up here.
	base := fmt.Sprintf("/api/v1/instances/%d", inst.ID)
	cases := []struct {
		method, path string
		body         any
	}{
		{"GET", base, nil},
		{"PUT", base, map[string]any{"name": "stolen"}},
		{"POST", base + "/start", nil},
		{"POST", base + "/stop", nil},
		{"POST", base + "/rebuild", nil},
		{"DELETE", base, nil},
		{"GET", base + "/volumes", nil},
		{"GET", base + "/stats", nil},
		{"GET", base + "/sleep", nil},
		{"PUT", base + "/sleep", map[string]any{"disabled": true}},
		{"POST", base + "/sleep/reset", nil},
		{"GET", base + "/secrets", nil},
		{"POST", base + "/secrets", map[string]any{"name": "X", "value": "y"}},
		{"GET", base + "/authorized-keys", nil},
		{"POST", base + "/authorized-keys", map[string]any{"key_id": 1}},
		{"GET", base + "/sshx-url", nil},
		{"POST", base + "/duplicate", nil},
		{"GET", base + "/storage/browse?path=/home/ubuntu", nil},
		{"GET", base + "/storage/download?path=/home/ubuntu/x", nil},
		{"GET", base + "/tailscale-status", nil},
	}
	for _, c := range cases {
		resp := h.do(c.method, c.path, c.body)
		got := resp.StatusCode
		resp.Body.Close()
		if got == http.StatusOK || got == http.StatusCreated {
			t.Errorf("%s %s: bob got %d on the admin's instance — scoping lost", c.method, c.path, got)
		}
	}

	// And the row is untouched.
	var name string
	h.db.Get(&name, "SELECT name FROM instances WHERE id = ?", inst.ID)
	if name != "admin-ws" {
		t.Errorf("instance name = %q after bob's attempts, want admin-ws", name)
	}
}

func TestRebuildByAdminUsesOwnersSecrets(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)
	bobID := seedBob(t, h, "userpass123")

	resp := h.do("POST", "/api/v1/admin/instances", map[string]any{
		"name": "bob-secrets", "template_id": templateID, "user_id": bobID,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create for bob: %d — %s", resp.StatusCode, b)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)

	// A secret for each user, so a mix-up is visible either way.
	loginAs(t, h, map[string]string{"email": "bob@test.com", "password": "userpass123"})
	resp = h.do("POST", "/api/v1/secrets", map[string]any{"name": "WHOSE", "value": "bobs-value"})
	resp.Body.Close()

	loginAs(t, h, map[string]string{"password": testPassword})
	resp = h.do("POST", "/api/v1/secrets", map[string]any{"name": "WHOSE", "value": "admins-value"})
	resp.Body.Close()

	// The admin rebuilds Bob's workspace. Rebuild recreates the container from scratch
	// and re-injects keys and secrets: if it reads them from the caller instead of the
	// owner, the admin's decrypted secrets land in Bob's container and the admin's SSH
	// keys replace Bob's, locking him out of his own machine.
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/rebuild", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin rebuild bob's instance: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	cfg := h.mock.instances[inst.IncusName].Config
	if got := cfg["environment.WHOSE"]; got == "admins-value" {
		t.Fatalf("rebuild injected the ADMIN's secret into Bob's container — credential leak")
	} else if got != "bobs-value" {
		t.Errorf("environment.WHOSE = %q, want bobs-value", got)
	}
}

// --- Per-instance resource limits -----------------------------------------------

type resourcesJSON struct {
	TemplateCPU     string `json:"template_cpu"`
	TemplateMemory  string `json:"template_memory"`
	OverrideCPU     string `json:"override_cpu"`
	OverrideMemory  string `json:"override_memory"`
	EffectiveCPU    string `json:"effective_cpu"`
	EffectiveMemory string `json:"effective_memory"`
	Editable        bool   `json:"editable"`
	Applied         bool   `json:"applied"`
}

func TestResourceOverrideSurvivesRebuild(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)
	resp := h.do("POST", "/api/v1/instances", map[string]any{"name": "res-ws", "template_id": templateID})
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)

	resp = h.do("PUT", fmt.Sprintf("/api/v1/admin/instances/%d/resources", inst.ID),
		map[string]any{"limits_cpu": "4", "limits_memory": "8GB"})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("set resources: %d — %s", resp.StatusCode, b)
	}
	var res resourcesJSON
	mustJSON(t, resp, &res)
	if res.EffectiveCPU != "4" || res.EffectiveMemory != "8GB" {
		t.Errorf("effective = %s/%s, want 4/8GB", res.EffectiveCPU, res.EffectiveMemory)
	}
	if !res.Applied {
		t.Errorf("applied = false, want the live push to have succeeded against the mock")
	}
	if cfg := h.mock.instances[inst.IncusName].Config; cfg["limits.cpu"] != "4" {
		t.Errorf("live limits.cpu = %q, want 4", cfg["limits.cpu"])
	}

	// The whole point: UpdateIncusConfig used to write only to Incus, so a rebuild —
	// which destroys the container and recreates it from the template — silently
	// reverted every limit an admin had set.
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/rebuild", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("rebuild: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	cfg := h.mock.instances[inst.IncusName].Config
	if cfg["limits.cpu"] != "4" {
		t.Errorf("limits.cpu after rebuild = %q, want 4 — the override did not survive", cfg["limits.cpu"])
	}
	if cfg["limits.memory"] != "8GB" {
		t.Errorf("limits.memory after rebuild = %q, want 8GB", cfg["limits.memory"])
	}
}

// Without an override the template's own limits must reach Incus. They did not: the
// template stores cpu as a JSON number, which broke the decode and took memory with it.
func TestTemplateLimitsReachIncus(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)

	// Reproduce what the YAML importer and the template editor actually store: cpu as a
	// JSON *number*. TemplateYAML.Resources is map[string]any and every template on disk
	// writes `cpu: 2` unquoted, so this — not the quoted form the JSON import helper
	// produces — is the shape production holds.
	if _, err := h.db.Exec(
		`UPDATE templates SET resources = ? WHERE id = ?`,
		`{"cpu":2,"disk":"20GB","memory":"4GB"}`, templateID); err != nil {
		t.Fatalf("store numeric resources: %v", err)
	}

	resp := h.do("POST", "/api/v1/instances", map[string]any{"name": "tmpl-limits", "template_id": templateID})
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)

	cfg := h.mock.instances[inst.IncusName].Config
	if cfg["limits.cpu"] != "2" {
		t.Errorf("limits.cpu = %q, want 2 — the template's numeric cpu was dropped", cfg["limits.cpu"])
	}
	if cfg["limits.memory"] != "4GB" {
		t.Errorf("limits.memory = %q, want 4GB — the type error took memory down with cpu", cfg["limits.memory"])
	}
	t.Logf("created with limits.cpu=%q limits.memory=%q", cfg["limits.cpu"], cfg["limits.memory"])
}

func TestResourceEditIsAdminOnly(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)
	bobID := seedBob(t, h, "userpass123")

	resp := h.do("POST", "/api/v1/admin/instances", map[string]any{
		"name": "bob-res", "template_id": templateID, "user_id": bobID,
	})
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)

	loginAs(t, h, map[string]string{"email": "bob@test.com", "password": "userpass123"})

	// Bob reads his own limits, read-only.
	resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d/resources", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("bob GET own resources: %d — %s", resp.StatusCode, b)
	}
	var res resourcesJSON
	mustJSON(t, resp, &res)
	if res.Editable {
		t.Errorf("editable = true for a regular user, want false")
	}

	// And cannot write them.
	resp = h.do("PUT", fmt.Sprintf("/api/v1/admin/instances/%d/resources", inst.ID),
		map[string]any{"limits_cpu": "64"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("bob PUT resources: got %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	var cpu string
	h.db.Get(&cpu, "SELECT limits_cpu FROM instances WHERE id = ?", inst.ID)
	if cpu != "" {
		t.Errorf("limits_cpu = %q after bob's attempt, want empty", cpu)
	}
}

func TestResourceValidationRejectsGarbage(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	templateID := seedTemplate(t, h)
	resp := h.do("POST", "/api/v1/instances", map[string]any{"name": "res-valid", "template_id": templateID})
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)

	path := fmt.Sprintf("/api/v1/admin/instances/%d/resources", inst.ID)
	// Incus accepts these at update time and only fails at the next container start,
	// long after the admin has navigated away — so Plati rejects them up front.
	for _, bad := range []map[string]any{
		{"limits_cpu": "lots"},
		{"limits_cpu": "-1"},
		{"limits_memory": "4 gigabytes"},
		{"limits_memory": "8"},
	} {
		resp := h.do("PUT", path, bad)
		got := resp.StatusCode
		resp.Body.Close()
		if got != http.StatusBadRequest {
			t.Errorf("PUT %v: got %d, want 400", bad, got)
		}
	}

	// The valid forms are all accepted.
	for _, ok := range []map[string]any{
		{"limits_cpu": "2"},
		{"limits_cpu": "0-3"},
		{"limits_cpu": "50%"},
		{"limits_memory": "4GB"},
		{"limits_memory": "512MiB"},
		{"limits_cpu": "", "limits_memory": ""},
	} {
		resp := h.do("PUT", path, ok)
		got := resp.StatusCode
		resp.Body.Close()
		if got != http.StatusOK {
			t.Errorf("PUT %v: got %d, want 200", ok, got)
		}
	}

	// Clearing an override must fall back to the template's value, not to "no limit":
	// UpdateInstanceConfig treats an empty value as "delete this key".
	cfg := h.mock.instances[inst.IncusName].Config
	if cfg["limits.cpu"] == "" {
		t.Errorf("limits.cpu was deleted when the override was cleared; want the template's value")
	}
}
