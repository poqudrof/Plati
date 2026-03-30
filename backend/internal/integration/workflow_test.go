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
	instances    map[string]*incusapi.Instance
	instanceIPs  map[string]string
	volumes      map[string]int
	cloudInitLog []string
}

func newMockIncus() *mockIncusClient {
	return &mockIncusClient{
		instances:   make(map[string]*incusapi.Instance),
		instanceIPs: make(map[string]string),
		volumes:     make(map[string]int),
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
	if ci, ok := cfg["user.user-data"]; ok {
		m.cloudInitLog = append(m.cloudInitLog, ci)
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
	userSvc, err := services.NewUserService(db, "0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("user service: %v", err)
	}
	templateSvc := services.NewTemplateService(db)
	instanceSvc := services.NewInstanceService(db, pool, userSvc)
	serverSvc := services.NewServerService(db, pool)
	adminHandler := handlers.NewAdminHandler(db, userSvc, instanceSvc)
	healthHandler := handlers.NewHealthHandler(db)
	setupHandler := handlers.NewSetupHandler(db, cfg, "/dev/null")

	r := router.New(router.Deps{
		AuthHandler:     handlers.NewAuthHandler(db, cfg, nil),
		UserHandler:     handlers.NewUserHandler(userSvc),
		TemplateHandler: handlers.NewTemplateHandler(templateSvc),
		InstanceHandler: handlers.NewInstanceHandler(instanceSvc, db),
		ServerHandler:   handlers.NewServerHandler(serverSvc, pool),
		AdminHandler:    adminHandler,
		HealthHandler:   healthHandler,
		SetupHandler:    setupHandler,
		TerminalHandler: handlers.NewTerminalHandler(db, pool),
		JWTSecret:       jwtSecret,
		FrontendURL:     "http://localhost",
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

func seedTemplate(t *testing.T, h *harness) int64 {
	t.Helper()
	// Use the Import endpoint which accepts the TemplateJSON format (proper types)
	body := strings.NewReader(`{
		"name": "node-dev",
		"slug": "node-dev",
		"description": "Node.js 22 LTS",
		"image": "images:ubuntu/24.04",
		"profiles": ["default"],
		"resources": {"cpu": "2", "memory": "4GB", "disk": "20GB"},
		"cloud_init": "#cloud-config\npackages:\n  - nodejs\n"
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
		"image": "images:ubuntu/24.04",
		"profiles": ["default"],
		"resources": {"cpu": "2", "memory": "4GB", "disk": "20GB"},
		"cloud_init": "#cloud-config\npackages:\n  - nodejs\n"
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

	// Add SSH key — it should be injected into cloud-init
	sshPubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKeyForCloudInit ci-user@laptop"
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
	var inst models.Instance
	mustJSON(t, resp, &inst)

	t.Logf("Instance created: id=%d name=%s incus=%s", inst.ID, inst.Name, inst.IncusName)

	// Verify mock received the create call
	if len(h.mock.instances) == 0 {
		t.Error("mock: no Incus instance was created")
	}

	// Verify SSH key was injected into cloud-init
	if len(h.mock.cloudInitLog) == 0 {
		t.Error("mock: no cloud-init data was sent")
	} else {
		ci := h.mock.cloudInitLog[0]
		if !strings.Contains(ci, sshPubKey) {
			t.Errorf("SSH key not found in cloud-init\ncloud-init:\n%s", ci)
		}
		t.Logf("SSH key correctly injected into cloud-init")
	}

	// Verify volume was created
	volName := inst.IncusName + "-workspace"
	if _, ok := h.mock.volumes[volName]; !ok {
		t.Errorf("mock: workspace volume %q not created", volName)
	}
	t.Logf("Workspace volume created: %s", volName)

	// Verify IP was stored
	if !inst.IPAddress.Valid || inst.IPAddress.String == "" {
		t.Error("instance IP not set after creation")
	} else {
		t.Logf("Instance IP: %s", inst.IPAddress.String)
	}

	// List instances → should see our instance
	resp = h.do("GET", "/api/v1/instances", nil)
	var instances []models.Instance
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
	var inst models.Instance
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
	var inst models.Instance
	mustJSON(t, resp, &inst)
	if inst.UserID != userID {
		t.Errorf("instance user_id = %d, want %d", inst.UserID, userID)
	}
	t.Logf("Admin created instance %d for user %d", inst.ID, userID)
}
