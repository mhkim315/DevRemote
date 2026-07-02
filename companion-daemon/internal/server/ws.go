package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"devremote/companion-daemon/internal/detector"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type AlertHandler func(detector.Alert)
type RawEventHandler func(detector.Alert)

// Hub manages connected mobile clients and broadcasts alerts.
type Hub struct {
	mu            sync.RWMutex
	clients       map[*websocket.Conn]bool
	listeners     []AlertHandler
	rawListeners  []RawEventHandler
	OnRawAlert    func(detector.Alert)
	OnResponse    func(clientIP string, msg map[string]interface{})
	OnRawBytes    func([]byte) error // direct binary PTY streaming
}

// NewHub creates a Hub.
func NewHub(onResponse func(clientIP string, msg map[string]interface{})) *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		OnResponse: onResponse,
	}
}

// HandleAlert broadcasts to all clients and listeners.
func (h *Hub) HandleAlert(a detector.Alert) {
	data, _ := json.Marshal(a)
	h.mu.RLock()
	// Copy listeners to avoid holding lock during callbacks.
	listeners := make([]AlertHandler, len(h.listeners))
	copy(listeners, h.listeners)
	for conn := range h.clients {
		go conn.WriteMessage(websocket.TextMessage, data) // BUG-015: async writes
	}
	h.mu.RUnlock()
	for _, l := range listeners {
		l(a)
	}
}

// SendRaw broadcasts a raw JSONL event to all clients and listeners.
func (h *Hub) SendRaw(a detector.Alert) {
	data, _ := json.Marshal(a)
	h.mu.RLock()
	listeners := make([]RawEventHandler, len(h.rawListeners))
	copy(listeners, h.rawListeners)
	for conn := range h.clients {
		go conn.WriteMessage(websocket.TextMessage, data) // BUG-015: async writes
	}
	h.mu.RUnlock()
	for _, l := range listeners {
		l(a)
	}
}

// AddRawListener registers a handler for raw JSONL events.
func (h *Hub) AddRawListener(handler RawEventHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rawListeners = append(h.rawListeners, handler)
}

// AddListener registers a handler that receives all alerts (e.g., WebRTC).
func (h *Hub) AddListener(handler AlertHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listeners = append(h.listeners, handler)
}

// ServeHTTP handles WebSocket upgrades.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws: upgrade failed: %v", err)
		return
	}

	clientIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}
	log.Printf("ws: %s connected", clientIP)

	h.mu.Lock()
	h.clients[conn] = true
	h.mu.Unlock()

	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			conn.Close()
			log.Printf("ws: %s disconnected", clientIP)
		}()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			log.Printf("ws: response from %s: %s", clientIP, string(message))

			var msg map[string]interface{}
			if json.Unmarshal(message, &msg) == nil && h.OnResponse != nil {
				h.OnResponse(clientIP, msg)
			}
		}
	}()
}

// handleApproval receives approval prompts from wrap PTY stdout scanner.
func (h *Hub) handleApproval(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var req struct {
		Type        string `json:"type"`
		Description string `json:"description"`
		ToolName    string `json:"toolName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		return
	}
	log.Printf("approval: prompt from wrap: %q", req.Description)

	// Forward directly to all mobile clients.
	h.HandleAlert(detector.Alert{
		SessionID:   "wrap",
		ToolUseID:   "",
		ToolName:    req.ToolName,
		Description: req.Description,
	})
	w.WriteHeader(200)
}

// handlePTY receives raw PTY output from wrap and broadcasts to mobile clients.
func (h *Hub) handlePTY(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var req struct {
		Text   string `json:"text"`
		Base64 string `json:"base64"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		return
	}
	text := req.Base64
	if text == "" {
		text = req.Text
	}
	if text == "" {
		w.WriteHeader(200)
		return
	}
	h.SendRaw(detector.Alert{
		Type:        "pty",
		Description: text,
	})
	w.WriteHeader(200)
}

// Omnara-compatible REST API handlers (MVP stubs).

func (h *Hub) handleGetAgents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"agent_types": []map[string]interface{}{
			{
				"id": "claude-code", "name": "Claude Code",
				"recent_instances": []map[string]interface{}{
					{"id": "session-1", "agent_type_id": "claude-code", "status": "ACTIVE", "started_at": "2026-07-02T00:00:00Z"},
				},
			},
		},
	})
}

func (h *Hub) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id": "session-1", "agent_type_id": "claude-code", "status": "ACTIVE",
		"started_at": "2026-07-02T00:00:00Z",
	})
}

func (h *Hub) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id": "session-1", "status": "ACTIVE", "started_at": "2026-07-02T00:00:00Z",
		"messages": []map[string]interface{}{
			{"id": "msg-1", "content": "DevRemote connected", "sender_type": "AGENT", "created_at": "2026-07-02T00:00:00Z", "requires_user_input": false},
		},
	})
}

func (h *Hub) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// SSE endpoint — WebRTC DataChannel handles the actual streaming.
	// Return a keep-alive comment for now.
	fmt.Fprintf(w, ": connected to DevRemote\n\n")
}

func (h *Hub) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	log.Printf("api: message received: %q", req.Content)
	w.WriteHeader(201)
}

// Start begins listening on the given address (e.g. ":9171").
func (h *Hub) Start(addr string) error {
	http.Handle("/ws", h)
	http.HandleFunc("/approval", h.handleApproval)
	http.HandleFunc("/pty", h.handlePTY)
	// Omnara-compatible REST API.
	http.HandleFunc("/api/v1/agents", h.handleGetAgents)
	http.HandleFunc("/api/v1/agent-instances", h.handleCreateInstance)
	http.HandleFunc("/api/v1/agent-instances/", h.handleGetInstance)
	http.HandleFunc("/api/v1/agent-instances/session-1/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			h.handleGetMessages(w, r)
		} else {
			h.handlePostMessage(w, r)
		}
	})
	log.Printf("WebSocket + REST API listening on %s", addr)
	return http.ListenAndServe(addr, nil)
}

// ActiveClientCount returns the number of connected WebSocket clients.
func (h *Hub) ActiveClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// LocalIPs returns non-loopback local IP addresses for discovery.
func LocalIPs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []string
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ips = append(ips, ipnet.IP.String())
		}
	}
	return ips
}
