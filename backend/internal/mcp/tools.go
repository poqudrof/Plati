package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
)

// tool is one MCP tool. Deliberately absent: delete_instance and rebuild_instance — they
// destroy data and stay in the web UI.
type tool struct {
	name        string
	title       string
	description string
	schema      map[string]any
	readOnly    bool
	destructive bool
	idempotent  bool
	run         func(c *call, args json.RawMessage) (any, error)
}

func (t tool) descriptor() map[string]any {
	return map[string]any{
		"name":        t.name,
		"title":       t.title,
		"description": t.description,
		"inputSchema": t.schema,
		"annotations": map[string]any{
			"title":           t.title,
			"readOnlyHint":    t.readOnly,
			"destructiveHint": t.destructive,
			"idempotentHint":  t.idempotent,
			"openWorldHint":   false,
		},
	}
}

func findTool(name string) *tool {
	for i := range tools {
		if tools[i].name == name {
			return &tools[i]
		}
	}
	return nil
}

// --- schemas ------------------------------------------------------------------------

func object(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var instanceIDProp = map[string]any{"type": "integer", "minimum": 1, "description": "Instance id, from list_instances"}

func instanceOnly() map[string]any {
	return object(map[string]any{"instance_id": instanceIDProp}, "instance_id")
}

// decode reads the arguments strictly: an unknown field is an error the model can fix,
// rather than a silently ignored typo.
func decode(args json.RawMessage, v any) error {
	d := json.NewDecoder(bytes.NewReader(args))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

// ownInstance resolves an instance the caller owns. An admin key gets no wider reach
// here than anyone else's: MCP acts on your own machines only.
func (c *call) ownInstance(id int64) error {
	if id <= 0 {
		return fmt.Errorf("instance_id is required")
	}
	if _, err := queries.GetInstanceForActor(c.s.db, id, auth.Actor{UserID: c.user.ID}); err != nil {
		return fmt.Errorf("instance %d not found among your instances", id)
	}
	return nil
}

type instanceArgs struct {
	InstanceID int64 `json:"instance_id"`
}

// instanceTool builds the common "check ownership, then forward" tool body.
func instanceTool(fn func(c *call, id int64) (any, error)) func(*call, json.RawMessage) (any, error) {
	return func(c *call, args json.RawMessage) (any, error) {
		var a instanceArgs
		if err := decode(args, &a); err != nil {
			return nil, err
		}
		if err := c.ownInstance(a.InstanceID); err != nil {
			return nil, err
		}
		return fn(c, a.InstanceID)
	}
}

func instancePath(id int64, suffix string) string {
	return fmt.Sprintf("/api/v1/instances/%d%s", id, suffix)
}

// --- tools --------------------------------------------------------------------------

var tools []tool

func init() {
	tools = []tool{
		{
			name:        "get_mcp_quota",
			title:       "MCP creation quota",
			description: "Show the limits on creating instances through MCP and how much of them you have used.",
			schema:      object(map[string]any{}),
			readOnly:    true, idempotent: true,
			run: func(c *call, args json.RawMessage) (any, error) {
				return c.s.quota(c.user.ID)
			},
		},
		{
			name:        "list_templates",
			title:       "List templates",
			description: "List the active templates an instance can be created from (id, name, description, image, resources).",
			schema:      object(map[string]any{}),
			readOnly:    true, idempotent: true,
			run: func(c *call, args json.RawMessage) (any, error) {
				return c.activeTemplates()
			},
		},
		{
			name:        "list_instances",
			title:       "List my instances",
			description: "List your instances with their status. created_via_mcp marks the ones that count against the MCP quota.",
			schema:      object(map[string]any{}),
			readOnly:    true, idempotent: true,
			run: func(c *call, args json.RawMessage) (any, error) {
				var list []map[string]any
				if err := c.api("GET", "/api/v1/instances", nil, &list); err != nil {
					return nil, err
				}
				ids, err := queries.ListMCPInstanceIDs(c.s.db, c.user.ID)
				if err != nil {
					return nil, err
				}
				viaMCP := map[int64]bool{}
				for _, id := range ids {
					viaMCP[id] = true
				}
				for _, inst := range list {
					if id, ok := inst["id"].(float64); ok {
						inst["created_via_mcp"] = viaMCP[int64(id)]
					}
				}
				return list, nil
			},
		},
		{
			name:        "get_instance",
			title:       "Get instance",
			description: "Details of one instance: status, template, IP, auto-stop policy and its deadline, CPU and memory limits.",
			schema:      instanceOnly(),
			readOnly:    true, idempotent: true,
			run: instanceTool(func(c *call, id int64) (any, error) {
				var inst, sleep, resources any
				if err := c.api("GET", instancePath(id, ""), nil, &inst); err != nil {
					return nil, err
				}
				out := map[string]any{"instance": inst}
				if err := c.api("GET", instancePath(id, "/sleep"), nil, &sleep); err != nil {
					out["auto_stop_error"] = err.Error()
				} else {
					out["auto_stop"] = sleep
				}
				if err := c.api("GET", instancePath(id, "/resources"), nil, &resources); err != nil {
					out["resources_error"] = err.Error()
				} else {
					out["resources"] = resources
				}
				return out, nil
			}),
		},
		{
			name:        "get_instance_stats",
			title:       "Get instance usage",
			description: "Live CPU, memory, disk and network usage of a running instance.",
			schema:      instanceOnly(),
			readOnly:    true, idempotent: true,
			run: instanceTool(func(c *call, id int64) (any, error) {
				return c.fetch("GET", instancePath(id, "/stats"), nil)
			}),
		},
		{
			name:        "get_instance_links",
			title:       "Get instance links",
			description: "The instance's URLs: tailnet hostname, OpenVSCode, SSHX and custom links. A link without url carries a note explaining why it cannot be opened right now.",
			schema:      instanceOnly(),
			readOnly:    true, idempotent: true,
			run: instanceTool(func(c *call, id int64) (any, error) {
				return c.fetch("GET", instancePath(id, "/links"), nil)
			}),
		},
		{
			name:  "create_instance",
			title: "Create instance",
			description: "Create a new instance from an active template. Creation continues in the background: poll get_instance until status is \"running\" (or \"error\"). " +
				"Limited per user — see get_mcp_quota.",
			schema: object(map[string]any{
				"name":           map[string]any{"type": "string", "minLength": 1, "maxLength": 63, "description": "Instance name; also its tailnet hostname"},
				"template_id":    map[string]any{"type": "integer", "minimum": 1, "description": "Template id, from list_templates"},
				"ssh_key_mode":   map[string]any{"type": "string", "enum": []string{"plati", "personal"}, "description": "Optional override of your SSH key preference"},
				"tailscale_mode": map[string]any{"type": "string", "enum": []string{"plati", "personal"}, "description": "Optional override of your Tailscale preference"},
			}, "name", "template_id"),
			run: createInstance,
		},
		{
			name:        "start_instance",
			title:       "Start instance",
			description: "Start a stopped instance. Restarts its auto-stop timer.",
			schema:      instanceOnly(),
			idempotent:  true,
			run: instanceTool(func(c *call, id int64) (any, error) {
				if err := c.api("POST", instancePath(id, "/start"), nil, nil); err != nil {
					return nil, err
				}
				return c.instanceStatus(id, "started")
			}),
		},
		{
			name:        "stop_instance",
			title:       "Stop instance",
			description: "Stop a running instance. Its persistent home survives; anything running in it is interrupted.",
			schema:      instanceOnly(),
			idempotent:  true,
			run: instanceTool(func(c *call, id int64) (any, error) {
				if err := c.api("POST", instancePath(id, "/stop"), nil, nil); err != nil {
					return nil, err
				}
				return c.instanceStatus(id, "stopped")
			}),
		},
		{
			name:        "restart_instance",
			title:       "Restart instance",
			description: "Stop the instance if it is running, then start it again.",
			schema:      instanceOnly(),
			run: instanceTool(func(c *call, id int64) (any, error) {
				var inst struct {
					Status string `json:"status"`
				}
				if err := c.api("GET", instancePath(id, ""), nil, &inst); err != nil {
					return nil, err
				}
				if inst.Status == "creating" {
					return nil, fmt.Errorf("instance %d is still being created; wait until it is running", id)
				}
				if inst.Status == "running" {
					if err := c.api("POST", instancePath(id, "/stop"), nil, nil); err != nil {
						return nil, err
					}
				}
				if err := c.api("POST", instancePath(id, "/start"), nil, nil); err != nil {
					return nil, err
				}
				return c.instanceStatus(id, "restarted")
			}),
		},
		{
			name:  "rename_instance",
			title: "Rename instance",
			description: "Change the display name, which also changes the tailnet hostname. rename_system_hostname also sets the hostname inside Ubuntu (instance must be running); " +
				"rename_container renames the Incus container too, which stops and restarts the instance.",
			schema: object(map[string]any{
				"instance_id":            instanceIDProp,
				"name":                   map[string]any{"type": "string", "minLength": 1, "maxLength": 63},
				"rename_system_hostname": map[string]any{"type": "boolean", "default": false},
				"rename_container":       map[string]any{"type": "boolean", "default": false},
			}, "instance_id", "name"),
			run: func(c *call, args json.RawMessage) (any, error) {
				var a struct {
					InstanceID           int64  `json:"instance_id"`
					Name                 string `json:"name"`
					RenameSystemHostname bool   `json:"rename_system_hostname"`
					RenameContainer      bool   `json:"rename_container"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if err := c.ownInstance(a.InstanceID); err != nil {
					return nil, err
				}
				if strings.TrimSpace(a.Name) == "" {
					return nil, fmt.Errorf("name is required")
				}
				return c.fetch("PUT", instancePath(a.InstanceID, ""), map[string]any{
					"name": a.Name, "rename_system_hostname": a.RenameSystemHostname, "rename_container": a.RenameContainer,
				})
			},
		},
		{
			name:  "update_auto_stop",
			title: "Update auto-stop",
			description: "Change the instance's auto-stop policy. disabled=true keeps it running until stopped by hand; timeout_minutes sets how long after its last start it is stopped " +
				"(0 = platform default). Omitted fields keep their value.",
			schema: object(map[string]any{
				"instance_id":     instanceIDProp,
				"disabled":        map[string]any{"type": "boolean"},
				"timeout_minutes": map[string]any{"type": "integer", "minimum": 0},
			}, "instance_id"),
			idempotent: true,
			run: func(c *call, args json.RawMessage) (any, error) {
				var a struct {
					InstanceID     int64 `json:"instance_id"`
					Disabled       *bool `json:"disabled"`
					TimeoutMinutes *int  `json:"timeout_minutes"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if err := c.ownInstance(a.InstanceID); err != nil {
					return nil, err
				}
				body := map[string]any{}
				if a.Disabled != nil {
					body["disabled"] = *a.Disabled
				}
				if a.TimeoutMinutes != nil {
					body["timeout_minutes"] = *a.TimeoutMinutes
				}
				if len(body) == 0 {
					return nil, fmt.Errorf("give disabled and/or timeout_minutes")
				}
				return c.fetch("PUT", instancePath(a.InstanceID, "/sleep"), body)
			},
		},
		{
			name:        "reset_auto_stop_timer",
			title:       "Reset auto-stop timer",
			description: "Buy the instance another full auto-stop timeout, counted from now.",
			schema:      instanceOnly(),
			run: instanceTool(func(c *call, id int64) (any, error) {
				return c.fetch("POST", instancePath(id, "/sleep/reset"), nil)
			}),
		},
		{
			name:  "update_links",
			title: "Update links",
			description: "Choose which built-in links are pinned on the dashboard card (hostname, openvscode, sshx) and replace the custom links. " +
				"Omitted fields keep their value; custom, when given, replaces the whole list (http/https URLs only).",
			schema: object(map[string]any{
				"instance_id": instanceIDProp,
				"hostname":    map[string]any{"type": "boolean"},
				"openvscode":  map[string]any{"type": "boolean"},
				"sshx":        map[string]any{"type": "boolean"},
				"custom": map[string]any{"type": "array", "items": object(map[string]any{
					"label":  map[string]any{"type": "string"},
					"url":    map[string]any{"type": "string"},
					"pinned": map[string]any{"type": "boolean"},
				}, "label", "url")},
			}, "instance_id"),
			idempotent: true,
			run: func(c *call, args json.RawMessage) (any, error) {
				var a struct {
					InstanceID int64            `json:"instance_id"`
					Hostname   *bool            `json:"hostname"`
					OpenVSCode *bool            `json:"openvscode"`
					SSHX       *bool            `json:"sshx"`
					Custom     []map[string]any `json:"custom"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if err := c.ownInstance(a.InstanceID); err != nil {
					return nil, err
				}
				body := map[string]any{}
				if a.Hostname != nil {
					body["hostname"] = *a.Hostname
				}
				if a.OpenVSCode != nil {
					body["openvscode"] = *a.OpenVSCode
				}
				if a.SSHX != nil {
					body["sshx"] = *a.SSHX
				}
				if a.Custom != nil {
					body["custom"] = a.Custom
				}
				return c.fetch("PUT", instancePath(a.InstanceID, "/links"), body)
			},
		},
	}
}

func createInstance(c *call, args json.RawMessage) (any, error) {
	var a struct {
		Name          string `json:"name"`
		TemplateID    int64  `json:"template_id"`
		SSHKeyMode    string `json:"ssh_key_mode"`
		TailscaleMode string `json:"tailscale_mode"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" || a.TemplateID <= 0 {
		return nil, fmt.Errorf("name and template_id are required")
	}
	for field, v := range map[string]string{"ssh_key_mode": a.SSHKeyMode, "tailscale_mode": a.TailscaleMode} {
		if v != "" && v != "plati" && v != "personal" {
			return nil, fmt.Errorf(`%s must be "plati" or "personal"`, field)
		}
	}

	// The API accepts any template id; MCP only offers the active ones.
	templates, err := c.activeTemplates()
	if err != nil {
		return nil, err
	}
	found := false
	for _, t := range templates {
		if t.ID == a.TemplateID {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("template %d is not an active template; see list_templates", a.TemplateID)
	}

	// Check, create and record under one lock so parallel calls cannot overshoot.
	c.s.createMu.Lock()
	defer c.s.createMu.Unlock()
	q, err := c.s.quota(c.user.ID)
	if err != nil {
		return nil, err
	}
	if err := q.checkCreate(); err != nil {
		return nil, err
	}

	body := map[string]any{"name": a.Name, "template_id": a.TemplateID}
	if a.SSHKeyMode != "" {
		body["ssh_key_mode"] = a.SSHKeyMode
	}
	if a.TailscaleMode != "" {
		body["tailscale_mode"] = a.TailscaleMode
	}
	var inst map[string]any
	if err := c.api("POST", "/api/v1/instances", body, &inst); err != nil {
		return nil, err
	}
	id, _ := inst["id"].(float64)
	if id <= 0 {
		return nil, fmt.Errorf("the API created the instance but returned no id")
	}
	if err := queries.RecordMCPInstanceCreation(c.s.db, c.user.ID, int64(id)); err != nil {
		return nil, fmt.Errorf("instance %d created, but recording it against your MCP quota failed: %v", int64(id), err)
	}
	q.Instances++
	q.CreationsLastHour++
	inst["created_via_mcp"] = true
	return map[string]any{
		"instance": inst,
		"quota":    q,
		"next":     "Creation runs in the background: poll get_instance until status is \"running\".",
	}, nil
}

type templateSummary struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description string          `json:"description"`
	Image       string          `json:"image"`
	Resources   json.RawMessage `json:"resources,omitempty"`
	IsActive    bool            `json:"-"`
}

// activeTemplates lists the templates a user may create from, trimmed to what helps
// choosing one (the full rows carry setup scripts and YAML).
func (c *call) activeTemplates() ([]templateSummary, error) {
	var raw []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		Image       string `json:"image"`
		Resources   string `json:"resources"`
		IsActive    bool   `json:"is_active"`
	}
	if err := c.api("GET", "/api/v1/templates", nil, &raw); err != nil {
		return nil, err
	}
	out := []templateSummary{}
	for _, t := range raw {
		if !t.IsActive {
			continue
		}
		s := templateSummary{ID: t.ID, Name: t.Name, Slug: t.Slug, Description: t.Description, Image: t.Image}
		if json.Valid([]byte(t.Resources)) {
			s.Resources = json.RawMessage(t.Resources)
		}
		out = append(out, s)
	}
	return out, nil
}

// instanceStatus re-reads the instance after an action so the answer shows its state.
func (c *call) instanceStatus(id int64, action string) (any, error) {
	var inst map[string]any
	if err := c.api("GET", instancePath(id, ""), nil, &inst); err != nil {
		return map[string]any{"result": action}, nil
	}
	return map[string]any{"result": action, "instance": inst}, nil
}

// fetch forwards a request and returns its decoded JSON answer.
func (c *call) fetch(method, path string, body any) (any, error) {
	var out any
	if err := c.api(method, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// checkArgs refuses arguments that are not an object or that the schema does not
// declare — every schema is additionalProperties: false.
func (t tool) checkArgs(args json.RawMessage) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(args, &m); err != nil {
		return fmt.Errorf("invalid arguments: expected a JSON object")
	}
	props, _ := t.schema["properties"].(map[string]any)
	for k := range m {
		if _, ok := props[k]; !ok {
			return fmt.Errorf("invalid arguments: unknown field %q", k)
		}
	}
	return nil
}
