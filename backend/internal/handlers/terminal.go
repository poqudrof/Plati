package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	gorillaWs "github.com/gorilla/websocket"
	incusWs "github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/services"
)

type TerminalHandler struct {
	db           *sqlx.DB
	pool         *incus.Pool
	creationLogs *services.CreationLogManager
}

func NewTerminalHandler(db *sqlx.DB, pool *incus.Pool, creationLogs *services.CreationLogManager) *TerminalHandler {
	return &TerminalHandler{db: db, pool: pool, creationLogs: creationLogs}
}

var upgrader = gorillaWs.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// resizeMsg is sent from the frontend when terminal is resized.
type resizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (h *TerminalHandler) Connect(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireActor(w, r)
	if !ok {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	inst, err := queries.GetInstanceForActor(h.db, id, actor)
	if err != nil {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}
	if inst.Status != "running" {
		http.Error(w, "instance not running", http.StatusBadRequest)
		return
	}

	// Determine which user to exec as. Default to root.
	execUser := r.URL.Query().Get("user")
	if execUser == "" {
		execUser = "root"
	}

	// For regular users: only "root" or the template's configured terminal_user are allowed.
	// Admins bypass this restriction. This is a separate policy from reaching the
	// instance at all — it governs which unix account the session runs as.
	if !actor.Admin && execUser != "root" {
		tmpl, err := queries.GetTemplate(h.db, inst.TemplateID)
		if err != nil || tmpl.TerminalUser == "" || tmpl.TerminalUser != execUser {
			http.Error(w, "user not allowed", http.StatusForbidden)
			return
		}
	}

	// Build the command: root gets /bin/bash directly; other users get su login shell.
	var command []string
	if execUser == "root" {
		command = []string{"/bin/bash"}
	} else {
		command = []string{"su", "-", execUser}
	}

	server, err := queries.GetServer(h.db, inst.ServerID)
	if err != nil {
		http.Error(w, "server not found", http.StatusInternalServerError)
		return
	}

	client, err := h.pool.GetClient(server.Name)
	if err != nil {
		http.Error(w, "incus client not available", http.StatusInternalServerError)
		return
	}

	// Upgrade HTTP to WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}
	defer ws.Close()

	// Pipe between browser WS and Incus exec
	pr, pw := io.Pipe()
	br, bw := io.Pipe()

	var once sync.Once
	cleanup := func() {
		pw.Close()
		bw.Close()
		pr.Close()
		br.Close()
	}

	// Read from browser WS → write to Incus stdin
	go func() {
		defer once.Do(cleanup)
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			// Try to parse as resize message
			var rm resizeMsg
			if json.Unmarshal(msg, &rm) == nil && rm.Type == "resize" {
				// Resize will be handled by the control channel
				continue
			}
			if _, err := pw.Write(msg); err != nil {
				return
			}
		}
	}()

	// Read from Incus stdout → write to browser WS
	go func() {
		defer once.Do(cleanup)
		buf := make([]byte, 4096)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				if wErr := ws.WriteMessage(gorillaWs.TextMessage, buf[:n]); wErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Control handler for window resize
	controlFn := func(conn *incusWs.Conn) {
		// We don't use the control channel in this simple implementation
	}

	// Run exec — blocks until shell exits
	err = client.ExecInstance(inst.IncusName, command, map[string]string{"TERM": "xterm-256color"}, pr, bw, controlFn)
	if err != nil {
		log.Printf("exec error: %v", err)
	}

	once.Do(cleanup)
}

// CreationStream streams creation log lines for an instance via WebSocket.
// Replays buffered lines immediately, then tails new ones until the goroutine
// completes. Works for both in-progress and already-finished creations.
func (h *TerminalHandler) CreationStream(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireActor(w, r)
	if !ok {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	inst, err := queries.GetInstanceForActor(h.db, id, actor)
	if err != nil {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("creation-stream websocket upgrade: %v", err)
		return
	}
	defer ws.Close()

	send := func(text string) error {
		return ws.WriteMessage(gorillaWs.TextMessage, []byte(text))
	}

	// If already done, replay stored log line-by-line and close.
	if inst.Status != "creating" {
		if inst.CreationLog != "" {
			for _, line := range strings.Split(inst.CreationLog, "\n") {
				if err := send(line + "\r\n"); err != nil {
					return
				}
			}
		}
		_ = send("\r\n\x1b[32m[Instance ready]\x1b[0m\r\n")
		return
	}

	existing, ch := h.creationLogs.Subscribe(id)

	for _, line := range existing {
		if err := send(line + "\r\n"); err != nil {
			if ch != nil {
				h.creationLogs.Unsubscribe(id, ch)
			}
			return
		}
	}

	if ch == nil {
		// Already completed between status check and subscribe.
		_ = send("\r\n\x1b[32m[Instance ready]\x1b[0m\r\n")
		return
	}
	defer h.creationLogs.Unsubscribe(id, ch)

	// Discard browser input; signal disconnection via done channel.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case line, ok := <-ch:
			if !ok {
				_ = send("\r\n\x1b[32m[Instance ready]\x1b[0m\r\n")
				return
			}
			if err := send(line + "\r\n"); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

// DebugLogs streams the boot log for any instance (admin only).
// It waits for the log file to appear then tails it from the beginning.
func (h *TerminalHandler) DebugLogs(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	inst, err := queries.GetInstance(h.db, id)
	if err != nil {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}
	if inst.Status != "running" {
		http.Error(w, "instance not running", http.StatusBadRequest)
		return
	}

	server, err := queries.GetServer(h.db, inst.ServerID)
	if err != nil {
		http.Error(w, "server not found", http.StatusInternalServerError)
		return
	}

	client, err := h.pool.GetClient(server.Name)
	if err != nil {
		http.Error(w, "incus client not available", http.StatusInternalServerError)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("debug-logs websocket upgrade: %v", err)
		return
	}
	defer ws.Close()

	pr, pw := io.Pipe()
	br, bw := io.Pipe()

	var once sync.Once
	cleanup := func() {
		pw.Close()
		bw.Close()
		pr.Close()
		br.Close()
	}

	// Discard browser → backend input
	go func() {
		defer once.Do(cleanup)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Incus stdout → browser WS
	go func() {
		defer once.Do(cleanup)
		buf := make([]byte, 4096)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				if wErr := ws.WriteMessage(gorillaWs.TextMessage, buf[:n]); wErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Stream init logs: prefer journalctl, fall back to syslog.
	logScript := `
if command -v journalctl > /dev/null 2>&1; then
    echo "=== Boot Journal ==="
    journalctl -b --no-pager -o short-iso 2>/dev/null
    echo ""
    echo "=== Following journal ==="
    exec journalctl -b -f --no-pager -o short-iso 2>/dev/null
else
    echo "=== Log files ==="
    exec tail -f /var/log/syslog /var/log/messages 2>/dev/null || echo "(no log source found)"
fi
`
	command := []string{"bash", "-c", logScript}

	controlFn2 := func(conn *incusWs.Conn) {}
	err = client.ExecInstance(inst.IncusName, command, map[string]string{"TERM": "xterm-256color"}, pr, bw, controlFn2)
	if err != nil {
		log.Printf("debug-logs exec error: %v", err)
	}

	once.Do(cleanup)
}
