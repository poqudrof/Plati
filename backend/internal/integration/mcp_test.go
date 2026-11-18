package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/homaserver/plati/internal/models"
)

// mcpClient speaks JSON-RPC to /mcp with one API key.
type mcpClient struct {
	t   *testing.T
	h   *harness
	key string
	n   int
}

type mcpReply struct {
	Status int
	Result json.RawMessage
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
}

func (c *mcpClient) rpc(method string, params any) mcpReply {
	c.t.Helper()
	c.n++
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.n, "method": method, "params": params})
	req, _ := http.NewRequest("POST", c.h.srv.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("mcp %s: %v", method, err)
	}
	defer resp.Body.Close()
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			c.t.Fatalf("mcp %s: decode %q: %v", method, raw, err)
		}
	}
	return mcpReply{Status: resp.StatusCode, Result: out.Result, Error: out.Error}
}

// tool calls a tool and returns its text and whether it is an error result.
func (c *mcpClient) tool(name string, args map[string]any) (string, bool) {
	c.t.Helper()
	r := c.rpc("tools/call", map[string]any{"name": name, "arguments": args})
	if r.Error != nil {
		c.t.Fatalf("tools/call %s: JSON-RPC error %d %s", name, r.Error.Code, r.Error.Message)
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil || len(res.Content) == 0 {
		c.t.Fatalf("tools/call %s: bad result %s", name, r.Result)
	}
	return res.Content[0].Text, res.IsError
}

// mustTool calls a tool that must succeed and decodes its JSON output into v.
func (c *mcpClient) mustTool(name string, args map[string]any, v any) {
	c.t.Helper()
	text, isErr := c.tool(name, args)
	if isErr {
		c.t.Fatalf("%s %v: tool error: %s", name, args, text)
	}
	if v != nil {
		if err := json.Unmarshal([]byte(text), v); err != nil {
			c.t.Fatalf("%s: decode %q: %v", name, text, err)
		}
	}
}

func (c *mcpClient) mustFail(name string, args map[string]any, contains string) {
	c.t.Helper()
	text, isErr := c.tool(name, args)
	if !isErr {
		c.t.Fatalf("%s %v: expected a tool error containing %q, got success: %s", name, args, contains, text)
	}
	if !strings.Contains(text, contains) {
		c.t.Fatalf("%s %v: expected error containing %q, got %q", name, args, contains, text)
	}
}

// mintKey creates an API key for userID through the admin route.
func mintKey(t *testing.T, h *harness, userID int64) string {
	t.Helper()
	resp := h.do("POST", fmt.Sprintf("/api/v1/admin/users/%d/api-keys", userID), map[string]string{"name": "mcp"})
	var minted struct {
		Key string `json:"key"`
	}
	mustJSON(t, resp, &minted)
	if minted.Key == "" {
		t.Fatal("empty API key")
	}
	return minted.Key
}

func newMCPUser(t *testing.T, h *harness, email string) (*session, *mcpClient) {
	t.Helper()
	s := newUserSession(t, h, email, strings.Split(email, "@")[0], "password1234")
	return s, &mcpClient{t: t, h: h, key: mintKey(t, h, s.user.ID)}
}

func setMCPLimits(t *testing.T, h *harness, maxInstances, perHour int) {
	t.Helper()
	resp := h.do("PUT", "/api/v1/admin/settings/mcp-limits", map[string]int{
		"max_instances": maxInstances, "max_creations_per_hour": perHour,
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("set MCP limits: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()
}

type mcpCreated struct {
	Instance models.InstanceJSON `json:"instance"`
	Quota    struct {
		Instances         int `json:"instances"`
		CreationsLastHour int `json:"creations_last_hour"`
	} `json:"quota"`
}

func TestMCP_TransportAndAuth(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	_, c := newMCPUser(t, h, "alice@example.com")

	post := func(headers map[string]string, body string) *http.Response {
		req, _ := http.NewRequest("POST", h.srv.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`

	// No key → 401 with a Bearer challenge. The admin's session cookie does not count.
	if r := post(nil, ping); r.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(r.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Errorf("no key: expected 401 + WWW-Authenticate, got %d %q", r.StatusCode, r.Header.Get("WWW-Authenticate"))
	}
	if resp := h.do("POST", "/mcp", json.RawMessage(ping)); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("session cookie only: expected 401, got %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if r := post(map[string]string{"Authorization": "Bearer plati_nope"}, ping); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad key: expected 401, got %d", r.StatusCode)
	}
	// A foreign browser origin is refused (DNS-rebinding guard).
	if r := post(map[string]string{"Authorization": "Bearer " + c.key, "Origin": "https://evil.example"}, ping); r.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: expected 403, got %d", r.StatusCode)
	}
	// Notifications are acknowledged with 202 and no body.
	if r := post(map[string]string{"Authorization": "Bearer " + c.key}, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); r.StatusCode != http.StatusAccepted {
		t.Errorf("notification: expected 202, got %d", r.StatusCode)
	}
	// No SSE stream: GET is 405.
	req, _ := http.NewRequest("GET", h.srv.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+c.key)
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: expected 405, got %d", r.StatusCode)
	}

	init := c.rpc("initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "test", "version": "1"},
	})
	var ir struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(init.Result, &ir); err != nil || ir.ProtocolVersion != "2025-06-18" || ir.ServerInfo.Name != "plati" || ir.Capabilities["tools"] == nil {
		t.Errorf("initialize: %s", init.Result)
	}
	// An unknown revision gets the newest one we speak.
	init = c.rpc("initialize", map[string]any{"protocolVersion": "1999-01-01"})
	json.Unmarshal(init.Result, &ir)
	if ir.ProtocolVersion != "2025-06-18" {
		t.Errorf("unknown protocol version: answered %q", ir.ProtocolVersion)
	}

	if r := c.rpc("does/not/exist", nil); r.Error == nil || r.Error.Code != -32601 {
		t.Errorf("unknown method: expected -32601, got %+v", r.Error)
	}
	if r := c.rpc("tools/call", map[string]any{"name": "delete_instance", "arguments": map[string]any{"instance_id": 1}}); r.Error == nil {
		t.Errorf("delete_instance must not exist")
	}

	// The tool list: no destructive operation is exposed.
	var tl struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	json.Unmarshal(c.rpc("tools/list", nil).Result, &tl)
	names := map[string]bool{}
	for _, tool := range tl.Tools {
		names[tool.Name] = true
		if tool.InputSchema["type"] != "object" {
			t.Errorf("tool %s: inputSchema type %v", tool.Name, tool.InputSchema["type"])
		}
	}
	for _, want := range []string{"list_templates", "list_instances", "get_instance", "create_instance", "start_instance",
		"stop_instance", "restart_instance", "rename_instance", "update_auto_stop", "get_mcp_quota"} {
		if !names[want] {
			t.Errorf("tool %s missing", want)
		}
	}
	for name := range names {
		if strings.Contains(name, "delete") || strings.Contains(name, "rebuild") {
			t.Errorf("destructive tool exposed: %s", name)
		}
	}

	// Unknown arguments are refused, not ignored.
	c.mustFail("list_instances", map[string]any{"user_id": 1}, "invalid arguments")
}

func TestMCP_LifecycleAndLimits(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	templateID := seedTemplate(t, h)
	alice, c := newMCPUser(t, h, "alice@example.com")

	setMCPLimits(t, h, 2, 3)

	var templates []struct {
		ID int64 `json:"id"`
	}
	c.mustTool("list_templates", nil, &templates)
	if len(templates) != 1 || templates[0].ID != templateID {
		t.Fatalf("list_templates: %+v", templates)
	}

	var first, second mcpCreated
	c.mustTool("create_instance", map[string]any{"name": "agent-1", "template_id": templateID}, &first)
	if first.Instance.ID == 0 || first.Quota.Instances != 1 || first.Quota.CreationsLastHour != 1 {
		t.Fatalf("first create: %+v", first)
	}
	waitForInstanceReady(t, h, first.Instance.ID)
	c.mustTool("create_instance", map[string]any{"name": "agent-2", "template_id": templateID}, &second)
	waitForInstanceReady(t, h, second.Instance.ID)

	// Quota: 2 owned.
	c.mustFail("create_instance", map[string]any{"name": "agent-3", "template_id": templateID}, "quota reached")

	// Instances made in the web UI do not count against it.
	resp := alice.do("POST", "/api/v1/instances", map[string]any{"name": "by-hand", "template_id": templateID})
	var byHand models.InstanceJSON
	mustJSON(t, resp, &byHand)
	waitForInstanceReady(t, h, byHand.ID)

	var list []map[string]any
	c.mustTool("list_instances", nil, &list)
	if len(list) != 3 {
		t.Fatalf("list_instances: expected 3, got %d", len(list))
	}
	for _, inst := range list {
		viaMCP := inst["name"] != "by-hand"
		if inst["created_via_mcp"] != viaMCP {
			t.Errorf("%v: created_via_mcp=%v, expected %v", inst["name"], inst["created_via_mcp"], viaMCP)
		}
	}

	// Deleting one (in the UI — MCP cannot) frees a slot…
	if r := alice.do("DELETE", fmt.Sprintf("/api/v1/instances/%d", first.Instance.ID), nil); r.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
	var third mcpCreated
	c.mustTool("create_instance", map[string]any{"name": "agent-3", "template_id": templateID}, &third)
	waitForInstanceReady(t, h, third.Instance.ID)
	if third.Quota.CreationsLastHour != 3 {
		t.Errorf("creations in the last hour: expected 3, got %d", third.Quota.CreationsLastHour)
	}

	// …but delete-and-recreate does not get around the hourly rate.
	alice.do("DELETE", fmt.Sprintf("/api/v1/instances/%d", second.Instance.ID), nil).Body.Close()
	c.mustFail("create_instance", map[string]any{"name": "agent-4", "template_id": templateID}, "rate limit reached")

	// Once the window has passed, creation works again.
	h.db.Exec("UPDATE mcp_instance_creations SET created_at = datetime('now', '-2 hours')")
	var fourth mcpCreated
	c.mustTool("create_instance", map[string]any{"name": "agent-4", "template_id": templateID}, &fourth)
	waitForInstanceReady(t, h, fourth.Instance.ID)

	var quota struct {
		MaxInstances      int `json:"max_instances"`
		Instances         int `json:"instances"`
		CreationsLastHour int `json:"creations_last_hour"`
	}
	c.mustTool("get_mcp_quota", nil, &quota)
	if quota.MaxInstances != 2 || quota.Instances != 2 || quota.CreationsLastHour != 1 {
		t.Errorf("quota: %+v", quota)
	}

	// 0 turns creation through MCP off.
	setMCPLimits(t, h, 0, 3)
	c.mustFail("create_instance", map[string]any{"name": "agent-5", "template_id": templateID}, "disabled")
	setMCPLimits(t, h, 5, 5)

	// Inactive templates and bad modes are refused before anything is created.
	h.db.Exec("UPDATE templates SET is_active = 0 WHERE id = ?", templateID)
	c.mustFail("create_instance", map[string]any{"name": "agent-6", "template_id": templateID}, "not an active template")
	h.db.Exec("UPDATE templates SET is_active = 1 WHERE id = ?", templateID)
	c.mustFail("create_instance", map[string]any{"name": "agent-6", "template_id": templateID, "ssh_key_mode": "root"}, "ssh_key_mode")

	// Lifecycle on an MCP-created instance.
	id := fourth.Instance.ID
	var got struct {
		Instance models.InstanceJSON `json:"instance"`
		AutoStop map[string]any      `json:"auto_stop"`
	}
	c.mustTool("get_instance", map[string]any{"instance_id": id}, &got)
	if got.Instance.Status != "running" || got.AutoStop == nil {
		t.Fatalf("get_instance: %+v", got)
	}
	var action struct {
		Instance models.InstanceJSON `json:"instance"`
	}
	c.mustTool("stop_instance", map[string]any{"instance_id": id}, &action)
	if action.Instance.Status != "stopped" {
		t.Errorf("after stop_instance: status %q", action.Instance.Status)
	}
	c.mustTool("start_instance", map[string]any{"instance_id": id}, &action)
	if action.Instance.Status != "running" {
		t.Errorf("after start_instance: status %q", action.Instance.Status)
	}
	c.mustTool("restart_instance", map[string]any{"instance_id": id}, &action)
	if action.Instance.Status != "running" {
		t.Errorf("after restart_instance: status %q", action.Instance.Status)
	}

	// Settings.
	c.mustTool("update_auto_stop", map[string]any{"instance_id": id, "timeout_minutes": 90}, nil)
	var minutes int
	h.db.Get(&minutes, "SELECT sleep_timeout_minutes FROM instances WHERE id = ?", id)
	if minutes != 90 {
		t.Errorf("update_auto_stop: sleep_timeout_minutes = %d", minutes)
	}
	c.mustFail("update_auto_stop", map[string]any{"instance_id": id}, "disabled and/or timeout_minutes")
	c.mustTool("rename_instance", map[string]any{"instance_id": id, "name": "renamed"}, nil)
	var name string
	h.db.Get(&name, "SELECT name FROM instances WHERE id = ?", id)
	if name != "renamed" {
		t.Errorf("rename_instance: name = %q", name)
	}
	c.mustTool("update_links", map[string]any{"instance_id": id, "custom": []map[string]any{{"label": "app", "url": "https://example.com"}}}, nil)
	c.mustFail("update_links", map[string]any{"instance_id": id, "custom": []map[string]any{{"label": "x", "url": "javascript:alert(1)"}}}, "Plati API error")
}

func TestMCP_OwnInstancesOnly(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	templateID := seedTemplate(t, h)

	bob, bobMCP := newMCPUser(t, h, "bob@example.com")
	resp := bob.do("POST", "/api/v1/instances", map[string]any{"name": "bobs", "template_id": templateID})
	var bobs models.InstanceJSON
	mustJSON(t, resp, &bobs)
	waitForInstanceReady(t, h, bobs.ID)

	_, carolMCP := newMCPUser(t, h, "carol@example.com")
	carolMCP.mustFail("stop_instance", map[string]any{"instance_id": bobs.ID}, "not found among your instances")

	// An admin's key gets no wider reach through MCP than anyone's.
	resp = h.do("GET", "/auth/me", nil)
	var admin models.User
	mustJSON(t, resp, &admin)
	adminMCP := &mcpClient{t: t, h: h, key: mintKey(t, h, admin.ID)}
	for _, tool := range []string{"get_instance", "stop_instance", "restart_instance", "get_instance_links"} {
		adminMCP.mustFail(tool, map[string]any{"instance_id": bobs.ID}, "not found among your instances")
	}
	adminMCP.mustFail("rename_instance", map[string]any{"instance_id": bobs.ID, "name": "pwned"}, "not found among your instances")
	var status string
	h.db.Get(&status, "SELECT status FROM instances WHERE id = ?", bobs.ID)
	if status != "running" {
		t.Errorf("bob's instance was touched: status %q", status)
	}

	// The owner can.
	bobMCP.mustTool("get_instance", map[string]any{"instance_id": bobs.ID}, nil)

	// An SSH-only account's key is refused at the door.
	h.do("PUT", fmt.Sprintf("/api/v1/admin/users/%d", bob.user.ID), map[string]any{"name": "bob", "role": "user", "ssh_only": true}).Body.Close()
	if r := bobMCP.rpc("ping", nil); r.Status != http.StatusUnauthorized {
		t.Errorf("SSH-only user's key: expected 401, got %d", r.Status)
	}
}

func TestMCP_AdminLimitsEndpoint(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	resp := h.do("GET", "/api/v1/admin/settings/mcp-limits", nil)
	var l struct {
		MaxInstances        int `json:"max_instances"`
		MaxCreationsPerHour int `json:"max_creations_per_hour"`
	}
	mustJSON(t, resp, &l)
	if l.MaxInstances != 3 || l.MaxCreationsPerHour != 5 {
		t.Errorf("defaults: %+v", l)
	}
	for _, body := range []map[string]any{
		{"max_instances": -1, "max_creations_per_hour": 5},
		{"max_instances": 3, "max_creations_per_hour": 1000},
		{"max_instances": 3},
	} {
		r := h.do("PUT", "/api/v1/admin/settings/mcp-limits", body)
		if r.StatusCode != http.StatusBadRequest {
			t.Errorf("PUT %v: expected 400, got %d", body, r.StatusCode)
		}
		r.Body.Close()
	}
	user := newUserSession(t, h, "dan@example.com", "Dan", "password1234")
	if r := user.do("PUT", "/api/v1/admin/settings/mcp-limits", map[string]int{"max_instances": 50, "max_creations_per_hour": 100}); r.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin PUT: expected 403, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
}

func TestMCP_QuotaRoute(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	user := newUserSession(t, h, "erin@example.com", "Erin", "password1234")
	resp := user.do("GET", "/api/v1/mcp/quota", nil)
	var q struct {
		MaxInstances int `json:"max_instances"`
		Instances    int `json:"instances"`
	}
	mustJSON(t, resp, &q)
	if q.MaxInstances != 3 || q.Instances != 0 {
		t.Errorf("quota: %+v", q)
	}

	// /mcp/info lists the same tools as tools/list.
	resp = user.do("GET", "/api/v1/mcp/info", nil)
	var info struct {
		Quota struct {
			MaxInstances int `json:"max_instances"`
		} `json:"quota"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	mustJSON(t, resp, &info)
	c := &mcpClient{t: t, h: h, key: mintKey(t, h, user.user.ID)}
	var tl struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	json.Unmarshal(c.rpc("tools/list", nil).Result, &tl)
	if info.Quota.MaxInstances != 3 || len(info.Tools) == 0 || len(info.Tools) != len(tl.Tools) {
		t.Errorf("info: %d tools, tools/list: %d", len(info.Tools), len(tl.Tools))
	}
	for i := range info.Tools {
		if info.Tools[i].Name != tl.Tools[i].Name {
			t.Errorf("tool %d: info %s, tools/list %s", i, info.Tools[i].Name, tl.Tools[i].Name)
		}
	}
}
