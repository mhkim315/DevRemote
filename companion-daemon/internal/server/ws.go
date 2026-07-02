package server

import (
	"encoding/json"
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
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		return
	}
	if req.Text == "" {
		w.WriteHeader(200)
		return
	}
	log.Printf("pty: %d chars → %d clients", len(req.Text), h.ActiveClientCount())
	h.SendRaw(detector.Alert{
		Type:        "pty",
		Description: req.Text,
	})
	w.WriteHeader(200)
}

// Start begins listening on the given address (e.g. ":9171").
func (h *Hub) Start(addr string) error {
	http.Handle("/ws", h)
	http.HandleFunc("/approval", h.handleApproval)
	http.HandleFunc("/pty", h.handlePTY)
	log.Printf("WebSocket server listening on %s", addr)
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
