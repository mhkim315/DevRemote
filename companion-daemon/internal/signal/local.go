package signal

import (
	"encoding/json"
	"net/http"
	"sync"
)

type Msg struct {
	Seq int             `json:"seq"`
	Msg json.RawMessage `json:"msg"`
}

type Store struct {
	mu       sync.Mutex
	messages []Msg
	seq      int
}

func NewStore() *Store { return &Store{} }

func (s *Store) Join(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }

func (s *Store) Poll(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	since := 0
	json.Unmarshal([]byte(r.URL.Query().Get("since")), &since)
	var msgs []Msg
	for _, m := range s.messages {
		if m.Seq > since { msgs = append(msgs, m) }
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"messages": msgs, "since": s.seq})
}

func (s *Store) Send(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string          `json:"code"`
		Role string          `json:"role"`
		Msg  json.RawMessage `json:"msg"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	s.mu.Lock()
	s.seq++
	s.messages = append(s.messages, Msg{Seq: s.seq, Msg: req.Msg})
	s.mu.Unlock()
	w.WriteHeader(200)
}
