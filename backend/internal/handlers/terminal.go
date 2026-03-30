package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"

	gorillaWs "github.com/gorilla/websocket"
	incusWs "github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
)

type TerminalHandler struct {
	db   *sqlx.DB
	pool *incus.Pool
}

func NewTerminalHandler(db *sqlx.DB, pool *incus.Pool) *TerminalHandler {
	return &TerminalHandler{db: db, pool: pool}
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
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	inst, err := queries.GetInstanceByUser(h.db, id, user.ID)
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
	err = client.ExecInstance(inst.IncusName, []string{"/bin/bash"}, map[string]string{"TERM": "xterm-256color"}, pr, bw, controlFn)
	if err != nil {
		log.Printf("exec error: %v", err)
	}

	once.Do(cleanup)
}
