package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func randomKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type message struct {
	Type          string `json:"type"`
	Role          string `json:"role,omitempty"`
	Code          string `json:"code,omitempty"`
	Key           string `json:"key,omitempty"`
	Peer          string `json:"peer,omitempty"`
	SDP           string `json:"sdp,omitempty"`
	Candidate     string `json:"candidate,omitempty"`
	SDPMid        string `json:"sdpMid,omitempty"`
	SDPMLineIndex int    `json:"sdpMLineIndex,omitempty"`
	ErrorMessage  string `json:"message,omitempty"`
}

type peer struct {
	role string
	key  string
	conn *websocket.Conn
	mu   sync.Mutex
}

func (p *peer) sendJSON(v interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn.WriteJSON(v)
}

type session struct {
	daemon *peer
	mobile *peer
	mu     sync.Mutex
}

func (s *session) pair(p *peer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p.role == "daemon" {
		s.daemon = p
	} else {
		s.mobile = p
	}

	p.sendJSON(message{Type: "joined"})

	if s.daemon != nil && s.mobile != nil {
		s.mobile.sendJSON(message{Type: "paired", Peer: "daemon"})
		s.daemon.sendJSON(message{Type: "paired", Peer: "mobile"})
	}
}

func (s *session) disconnect(p *peer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p.role == "daemon" || s.daemon == p {
		s.daemon = nil
	} else {
		s.mobile = nil
	}

	// Notify remaining peer.
	if s.daemon != nil {
		s.daemon.sendJSON(message{Type: "peer_disconnected"})
	}
	if s.mobile != nil {
		s.mobile.sendJSON(message{Type: "peer_disconnected"})
	}
}

// relayFrom receives messages from one peer and sends to the other.
func relayFrom(from, to *peer) {
	defer func() {
		from.conn.Close()
	}()
	for {
		var msg message
		if err := from.conn.ReadJSON(&msg); err != nil {
			return
		}
		// Forward sdp, ice, and error messages to the paired peer.
		if msg.Type == "sdp" || msg.Type == "ice" || msg.Type == "error" {
			if to != nil {
				to.sendJSON(msg)
			}
		}
	}
}

type hub struct {
	mu       sync.RWMutex
	sessions map[string]*session // keyed by persistent key
	codes    map[string]string   // code -> key mapping (for first-time pairing)
}

func newHub() *hub {
	return &hub{
		sessions: make(map[string]*session),
		codes:    make(map[string]string),
	}
}

func (h *hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade: %v", err)
		return
	}

	// Read join message (5s timeout).
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var join message
	if err := conn.ReadJSON(&join); err != nil || join.Type != "join" {
		conn.WriteJSON(message{Type: "error", ErrorMessage: "expected join message"})
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{}) // clear deadline

	if join.Role != "daemon" && join.Role != "mobile" {
		conn.WriteJSON(message{Type: "error", ErrorMessage: "role must be daemon or mobile"})
		conn.Close()
		return
	}

	// Resolve session by key or code.
	sessionKey := join.Key
	if sessionKey == "" && join.Code != "" {
		// First-time pairing: look up code.
		h.mu.RLock()
		key, ok := h.codes[join.Code]
		h.mu.RUnlock()
		if !ok {
			if join.Role == "daemon" {
				// Daemon creates new code.
				sessionKey = randomKey()
				h.mu.Lock()
				h.codes[join.Code] = sessionKey
				h.mu.Unlock()
			} else {
				conn.WriteJSON(message{Type: "error", ErrorMessage: "invalid code"})
				conn.Close()
				return
			}
		} else {
			sessionKey = key
		}
	}
	if sessionKey == "" {
		conn.WriteJSON(message{Type: "error", ErrorMessage: "key or code required"})
		conn.Close()
		return
	}

	log.Printf("join: role=%s key=%s code=%s", join.Role, sessionKey, join.Code)

	h.mu.Lock()
	sess, ok := h.sessions[sessionKey]
	if !ok {
		sess = &session{}
		h.sessions[sessionKey] = sess
	}
	h.mu.Unlock()

	p := &peer{role: join.Role, key: sessionKey, conn: conn}
	sess.pair(p)

	// Send the persistent key to mobile so it can store it.
	if join.Role == "mobile" && join.Code != "" {
		sess.mu.Lock()
		if sess.mobile != nil {
			sess.mobile.sendJSON(message{Type: "key", Key: sessionKey})
		}
		sess.mu.Unlock()
	}

	// Find the other peer for relaying.
	getOther := func() *peer {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		if p.role == "daemon" {
			return sess.mobile
		}
		return sess.daemon
	}

	// Block on reading from this peer and relay to the other.
	relayFrom(p, getOther())

	// Cleanup.
	sess.disconnect(p)
	h.mu.Lock()
	if sess.daemon == nil && sess.mobile == nil {
		delete(h.sessions, sessionKey)
	}
	h.mu.Unlock()
	log.Printf("disconnect: role=%s key=%s", join.Role, sessionKey)
}

func main() {
	port := flag.String("port", "9173", "Signaling server port")
	flag.Parse()

	h := newHub()
	http.Handle("/", h)

	log.Printf("Signaling server listening on :%s", *port)
	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("ListenAndServe: %v", err)
	}
}
