// Package mcp serves Plati as a remote MCP server (Streamable HTTP, stateless) at /mcp.
//
// It is a thin layer over the REST API: every tool forwards to an existing /api/v1 route
// with the caller's own Authorization header, so ownership checks, validation and
// side effects are exactly those of the API. What this package adds is policy — which
// routes are reachable at all (no delete, no rebuild), "your own instances only" even
// for an admin key, and the creation limits in limits.go.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/auth"
)

const serverVersion = "1.0.0"

// supportedVersions lists the MCP protocol revisions this server speaks, newest first.
var supportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

type Server struct {
	db          *sqlx.DB
	api         http.Handler
	frontendURL string
	createMu    sync.Mutex // serialises quota check → create → record
}

func NewServer(db *sqlx.DB, frontendURL string) *Server {
	return &Server{db: db, frontendURL: strings.TrimRight(frontendURL, "/")}
}

// SetAPI gives the server the router its tools forward to. Set once, by router.New.
func (s *Server) SetAPI(h http.Handler) { s.api = h }

// RequireBearer refuses anything but an API key: /mcp is for programs, and accepting the
// browser's session cookie would let any page the user visits drive their machines.
func RequireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="plati"`)
			http.Error(w, `{"error":"an API key is required: Authorization: Bearer <key>"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

const (
	errParse          = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// DNS-rebinding guard required by the Streamable HTTP transport: a browser origin
	// other than Plati's own frontend is refused.
	if o := r.Header.Get("Origin"); o != "" && strings.TrimRight(o, "/") != s.frontendURL {
		http.Error(w, `{"error":"origin not allowed"}`, http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		// Stateless server: no SSE stream to open (GET) and no session to end (DELETE).
		w.Header().Set("Allow", "POST")
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeRPC(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{Code: errParse, Message: "cannot read body"}})
		return
	}
	trimmed := strings.TrimSpace(string(body))

	// A batch (2025-03-26 and older) is answered as an array of the responses.
	if strings.HasPrefix(trimmed, "[") {
		var batch []rpcMessage
		if err := json.Unmarshal(body, &batch); err != nil || len(batch) == 0 {
			writeRPC(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: errParse, Message: "invalid JSON-RPC batch"}})
			return
		}
		var out []rpcResponse
		for _, m := range batch {
			if resp, ok := s.handle(r, user, m); ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeRPC(w, http.StatusOK, out)
		return
	}

	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		writeRPC(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{Code: errParse, Message: "invalid JSON"}})
		return
	}
	resp, ok := s.handle(r, user, msg)
	if !ok { // notification or client response: nothing to answer
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeRPC(w, http.StatusOK, resp)
}

// handle answers one message; ok is false for notifications and client responses.
func (s *Server) handle(r *http.Request, user *auth.ContextUser, m rpcMessage) (rpcResponse, bool) {
	if len(m.ID) == 0 || m.Method == "" {
		return rpcResponse{}, false
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: m.ID}
	if m.JSONRPC != "2.0" {
		resp.Error = &rpcError{Code: errInvalidRequest, Message: `jsonrpc must be "2.0"`}
		return resp, true
	}
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(m.Params, &p)
		version := supportedVersions[0]
		for _, v := range supportedVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		resp.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "plati", "title": "Plati", "version": serverVersion},
			"instructions":    s.instructions(),
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		list := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			list = append(list, t.descriptor())
		}
		resp.Result = map[string]any{"tools": list}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			resp.Error = &rpcError{Code: errInvalidParams, Message: "invalid params"}
			return resp, true
		}
		t := findTool(p.Name)
		if t == nil {
			resp.Error = &rpcError{Code: errInvalidParams, Message: fmt.Sprintf("unknown tool %q", p.Name)}
			return resp, true
		}
		args := p.Arguments
		if len(args) == 0 || string(args) == "null" {
			args = json.RawMessage("{}")
		}
		if err := t.checkArgs(args); err != nil {
			resp.Result = toolResult(nil, err)
			return resp, true
		}
		c := &call{s: s, r: r, user: user}
		result, err := t.run(c, args)
		resp.Result = toolResult(result, err)
	default:
		resp.Error = &rpcError{Code: errMethodNotFound, Message: fmt.Sprintf("method %q not supported", m.Method)}
	}
	return resp, true
}

func (s *Server) instructions() string {
	l := s.limits()
	return fmt.Sprintf(`Plati manages Incus dev machines ("instances"). Everything here acts as the owner of the API key, on their own instances only.
Typical flow: list_templates → create_instance → poll get_instance until status is "running" → get_instance_links for the URLs.
Limits: at most %d instances created through MCP at a time per user, and %d creations per hour (get_mcp_quota shows the current usage). Deleting and rebuilding instances is not available here — use the Plati web UI.`,
		l.MaxInstances, l.MaxCreationsPerHour)
}

// toolResult wraps a tool's output as an MCP CallToolResult. A tool failure is a result
// with isError set, not a JSON-RPC error, so the model can read and react to it.
func toolResult(v any, err error) map[string]any {
	if err != nil {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		}
	}
	text, _ := json.MarshalIndent(v, "", "  ")
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
		"isError": false,
	}
}

func writeRPC(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// call is one tools/call: the caller's identity plus the way back into the API.
type call struct {
	s    *Server
	r    *http.Request
	user *auth.ContextUser
}

// apiError is a non-2xx answer from the API, reported to the model as the API's message.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return fmt.Sprintf("Plati API error (%d): %s", e.status, e.msg) }

// api forwards one request to the REST API with the caller's credentials and decodes
// the JSON answer into out (when non-nil).
func (c *call) api(method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(data))
	}
	// chi keeps its routing state in the context; the outer /mcp request's state would
	// make the router treat this as a sub-route, so it is cleared.
	ctx := context.WithValue(c.r.Context(), chi.RouteCtxKey, nil)
	req, err := http.NewRequestWithContext(ctx, method, path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.r.Header.Get("Authorization"))
	req.Header.Set("User-Agent", "plati-mcp")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = c.r.RemoteAddr

	rec := httptest.NewRecorder()
	c.s.api.ServeHTTP(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(rec.Body.String())
		if json.Unmarshal(rec.Body.Bytes(), &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return &apiError{status: rec.Code, msg: msg}
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			return fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	}
	return nil
}
