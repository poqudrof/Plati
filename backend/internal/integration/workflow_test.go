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
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	incusapi "github.com/lxc/incus/v6/shared/api"

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

type mockIncusClient struct {
	instances       map[string]*incusapi.Instance
	instanceIPs     map[string]string
	volumes         map[string]int
	pushedFiles     map[string][]byte // remote path → content
	runCommandFn    func(name string, command []string) (string, error)
	streamCommandFn func(name string, command []string) (io.ReadCloser, error)
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

func (m *mockIncusClient) ExecInstance(name string, command []string, env map[string]string, stdin io.ReadCloser, stdout io.WriteCloser, control func(conn *websocket.Conn)) error {
	return nil
}

func (m *mockIncusClient) RunCommand(name string, command []string) (string, error) {
	if m.runCommandFn != nil {
		return m.runCommandFn(name, command)
	}
	return "", nil
}

func (m *mockIncusClient) UpdateInstanceConfig(name string, config map[string]string) error {
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
	instanceSvc := services.NewInstanceService(db, pool, userSvc, prefSvc, adminSvc, "", "", repoSvc, templateSvc)
	serverSvc := services.NewServerService(db, pool)
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
		RepoHandler:          handlers.NewRepoHandler(repoSvc),
		JWTSecret:            jwtSecret,
		FrontendURL:          "http://localhost",
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
		"resources": {"cpu": "2", "memory": "4GB", "disk": "20GB"}
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

	testKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKeyForIntegration test@plati"

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
	sshPubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKeyForSetup ci-user@laptop"
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

	// Verify mock received the create call
	if len(h.mock.instances) == 0 {
		t.Error("mock: no Incus instance was created")
	}

	// Wait for async setup to complete (file push happens in background goroutine)
	for i := 0; i < 100; i++ {
		resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil)
		var updated models.InstanceJSON
		mustJSON(t, resp, &updated)
		if updated.Status == "running" || updated.Status == "error" {
			break
		}
		time.Sleep(20 * time.Millisecond)
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
	volName := inst.IncusName + "-workspace"
	if _, ok := h.mock.volumes[volName]; !ok {
		t.Errorf("mock: workspace volume %q not created", volName)
	}
	t.Logf("Workspace volume created: %s", volName)

	// Verify IP was stored
	if inst.IPAddress == nil || *inst.IPAddress == "" {
		t.Error("instance IP not set after creation")
	} else {
		t.Logf("Instance IP: %s", *inst.IPAddress)
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
		t.Error("mock: workspace volume was not deleted")
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
	volName := inst.IncusName + "-workspace"

	initialVolumeSize := h.mock.volumes[volName]
	t.Logf("Before rebuild: volume=%s size=%d", volName, initialVolumeSize)

	// Rebuild
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/rebuild", inst.ID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("rebuild: %d — %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// Volume should still exist (workspace preserved)
	if _, ok := h.mock.volumes[volName]; !ok {
		t.Error("workspace volume was deleted during rebuild — data would be lost!")
	}
	t.Logf("Rebuild OK: workspace volume preserved")

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
	sshPubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPhase2TestKey phase2@test"
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

	// 6. Sync repo → API accepts the request
	resp = h.do("POST", fmt.Sprintf("/api/v1/admin/repos/%d/sync", repoID), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("sync repo: %d — %s", resp.StatusCode, body)
	}
	var syncResp map[string]any
	mustJSON(t, resp, &syncResp)
	if syncResp["status"] != "syncing" {
		t.Errorf("sync status = %q, want syncing", syncResp["status"])
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
	volName := inst.IncusName + "-workspace"

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
	if d.MountPath != "/workspace" {
		t.Errorf("mount_path = %q, want /workspace", d.MountPath)
	}
	if d.DeviceName != "workspace" {
		t.Errorf("device_name = %q, want workspace", d.DeviceName)
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
				{"path":"/workspace","size":"20GB"},
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
	if mounts["/workspace"] != 20 {
		t.Errorf("/workspace size = %d, want 20", mounts["/workspace"])
	}
	if mounts["/data"] != 10 {
		t.Errorf("/data size = %d, want 10", mounts["/data"])
	}

	// Cross-check mock
	if len(h.mock.volumes) != 2 {
		t.Errorf("mock: expected 2 volumes, got %d", len(h.mock.volumes))
	}
	t.Logf("Multi-volume: 2 disks at /workspace(20GB) and /data(10GB)")

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
