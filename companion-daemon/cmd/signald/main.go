package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"sync"
)

type message struct {
	Type          string `json:"type"`
	Role          string `json:"role,omitempty"`
	Code          string `json:"code,omitempty"`
	Key           string `json:"key,omitempty"`
	Peer          string `json:"peer,omitempty"`
	SDP           string `json:"sdp,omitempty"`
	SDPType       string `json:"sdpType,omitempty"`
	Candidate     string `json:"candidate,omitempty"`
	SDPMid        string `json:"sdpMid,omitempty"`
	SDPMLineIndex int    `json:"sdpMLineIndex,omitempty"`
	ErrorMessage  string `json:"message,omitempty"`
}

type queuedMsg struct {
	Seq int     `json:"seq"`
	Msg message `json:"msg"`
}

type session struct {
	mu          sync.Mutex
	daemonInbox []queuedMsg
	mobileInbox []queuedMsg
	daemonSeq   int
	mobileSeq   int
	nextSeq     int
	joined      map[string]bool // tracks which roles have already joined
}

func (s *session) isRejoin(role string) bool {
	return s.joined[role]
}

func (s *session) markJoined(role string) {
	if s.joined == nil {
		s.joined = make(map[string]bool)
	}
	s.joined[role] = true
}

func newSession() *session {
	return &session{nextSeq: 1}
}

func (s *session) push(role string, msg message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	qm := queuedMsg{Seq: s.nextSeq, Msg: msg}
	s.nextSeq++
	if role == "mobile" {
		s.daemonInbox = append(s.daemonInbox, qm)
	} else {
		s.mobileInbox = append(s.mobileInbox, qm)
	}
}

func (s *session) poll(role string, since int) []queuedMsg {
	s.mu.Lock()
	defer s.mu.Unlock()
	var inbox *[]queuedMsg
	var seen *int
	if role == "daemon" {
		inbox = &s.daemonInbox
		seen = &s.daemonSeq
	} else {
		inbox = &s.mobileInbox
		seen = &s.mobileSeq
	}
	var msgs []queuedMsg
	for _, m := range *inbox {
		if m.Seq > since {
			msgs = append(msgs, m)
			if m.Seq > *seen {
				*seen = m.Seq
			}
		}
	}
	// Prune processed messages to prevent unbounded growth (BUG-012).
	if len(msgs) > 0 && len(*inbox) > 32 {
		*inbox = (*inbox)[len(*inbox)-16:]
	}
	return msgs
}

// reset clears inboxes for a fresh reconnection (BUG-008).
func (s *session) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.daemonInbox = nil
	s.mobileInbox = nil
	s.daemonSeq = 0
	s.mobileSeq = 0
}

type hub struct {
	mu       sync.RWMutex
	sessions map[string]*session  // keyed by persistent key
	codes    map[string]string    // code -> key
}

func newHub() *hub {
	return &hub{
		sessions: make(map[string]*session),
		codes:    make(map[string]string),
	}
}

func (h *hub) resolveSession(code string) (string, *session, bool) {
	h.mu.RLock()
	key, ok := h.codes[code]
	h.mu.RUnlock()
	if ok {
		h.mu.RLock()
		sess := h.sessions[key]
		h.mu.RUnlock()
		return key, sess, sess != nil
	}
	return "", nil, false
}

func (h *hub) getOrCreate(key string) *session {
	h.mu.Lock()
	defer h.mu.Unlock()
	sess, ok := h.sessions[key]
	if !ok {
		sess = newSession()
		h.sessions[key] = sess
	}
	return sess
}

func (h *hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(200)
		return
	}

	path := r.URL.Path
	switch path {
	case "/join":
		h.handleJoin(w, r)
	case "/send":
		h.handleSend(w, r)
	case "/poll":
		h.handlePoll(w, r)
	case "/leave":
		h.handleLeave(w, r)
	default:
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"status": "not_found"})
	}
}

func (h *hub) handleJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		Role string `json:"role"`
		Key  string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "bad request")
		return
	}
	if req.Role != "daemon" && req.Role != "mobile" {
		writeError(w, "role must be daemon or mobile")
		return
	}

	sessionKey := ""
	if req.Code != "" {
		// Code always takes priority — resolves to the daemon-created session.
		key, _, ok := h.resolveSession(req.Code)
		if ok {
			sessionKey = key
		} else {
			sessionKey = randomKey()
			h.mu.Lock()
			h.codes[req.Code] = sessionKey
			h.sessions[sessionKey] = newSession()
			h.mu.Unlock()
		}
	} else if req.Key != "" {
		sessionKey = req.Key
	}
	if sessionKey == "" {
		writeError(w, "code or key required")
		return
	}

	sess := h.getOrCreate(sessionKey)

	// Track whether this peer is rejoining.
	isRejoin := sess.isRejoin(req.Role)

	// Reset inboxes on rejoin to prevent stale SDP/ICE replay (BUG-008).
	if isRejoin {
		sess.reset()
	}
	sess.markJoined(req.Role)

	// Push paired only when both peers have joined (at least one rejoin).
	if isRejoin {
		otherRole := "daemon"
		if req.Role == "daemon" {
			otherRole = "mobile"
		}
		sess.push(req.Role, message{Type: "paired", Peer: req.Role})
		sess.push(otherRole, message{Type: "paired", Peer: req.Role})
	}

	log.Printf("join: role=%s key=%s code=%s", req.Role, sessionKey, req.Code)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"key":    sessionKey,
	})
}

func (h *hub) handleSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string  `json:"code"`
		Role string  `json:"role"`
		Msg  message `json:"msg"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "bad request")
		return
	}

	key, _, ok := h.resolveSession(req.Code)
	if !ok {
		writeError(w, "invalid code")
		return
	}
	sess := h.getOrCreate(key)
	sess.push(req.Role, req.Msg)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *hub) handlePoll(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	role := r.URL.Query().Get("role")
	if code == "" || role == "" {
		writeError(w, "code and role required")
		return
	}

	var since int
	json.Unmarshal([]byte(r.URL.Query().Get("since")), &since)

	key, _, ok := h.resolveSession(code)
	if !ok {
		// No session yet — return empty.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"messages": []queuedMsg{},
			"since":    since,
		})
		return
	}

	sess := h.getOrCreate(key)
	msgs := sess.poll(role, since)

	lastSeq := since
	if len(msgs) > 0 {
		lastSeq = msgs[len(msgs)-1].Seq
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": msgs,
		"since":    lastSeq,
	})
}

func (h *hub) handleLeave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		Role string `json:"role"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	key, _, ok := h.resolveSession(req.Code)
	if !ok {
		writeError(w, "invalid code")
		return
	}
	sess := h.getOrCreate(key)
	sess.push(req.Role, message{Type: "peer_disconnected"})
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	_ = key
}

func writeError(w http.ResponseWriter, msg string) {
	w.WriteHeader(400)
	json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": msg})
}

func randomKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func main() {
	port := flag.String("port", "9173", "Signaling server port")
	flag.Parse()

	h := newHub()
	http.Handle("/", h)

	log.Printf("HTTP signaling server listening on :%s", *port)
	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("ListenAndServe: %v", err)
	}
}
