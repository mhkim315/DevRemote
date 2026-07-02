package webrtc

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"devremote/companion-daemon/internal/detector"

	"github.com/pion/webrtc/v4"
)

type signalingMsg struct {
	Seq int             `json:"seq"`
	Msg json.RawMessage `json:"msg"`
}

type Session struct {
	peerKey      string
	code         string
	signalingURL string
	stunServers  []string

	pc      *webrtc.PeerConnection
	dc      *webrtc.DataChannel
	alertCh chan detector.Alert

	mu        sync.Mutex
	closed    bool
	lastSeq   int
	pollStop  chan struct{}
}

func New(signalingURL string, stunServers []string, keyDir string) *Session {
	return &Session{
		signalingURL: signalingURL,
		stunServers:  stunServers,
		peerKey:      loadOrCreateKey(keyDir),
		code:         randomCode(),
		alertCh:      make(chan detector.Alert, 256),
		pollStop:     make(chan struct{}),
	}
}

func (s *Session) Code() string { return s.code }

func (s *Session) HandleAlert(a detector.Alert) {
	select {
	case s.alertCh <- a:
	default:
	}
}

func (s *Session) HandleRaw(a detector.Alert) {
	// Send raw events with type=raw over data channel.
	a.Type = "raw"
	select {
	case s.alertCh <- a:
	default:
	}
}

func (s *Session) Start(onResponse func(clientIP string, msg map[string]interface{})) error {
	// 1. Join session via HTTP.
	if err := s.httpPost("/join", map[string]interface{}{
		"code": s.code,
		"role": "daemon",
		"key":  s.peerKey,
	}); err != nil {
		return fmt.Errorf("join: %w", err)
	}
	log.Printf("webrtc: joined signaling (code=%s)", s.code)

	// 2. Create peer connection.
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: s.stunServers}},
	})
	if err != nil {
		return fmt.Errorf("peer connection: %w", err)
	}
	s.pc = pc

	dc, err := pc.CreateDataChannel("devremote", nil)
	if err != nil {
		return fmt.Errorf("create data channel: %w", err)
	}
	s.dc = dc

	dc.OnOpen(func() {
		log.Printf("webrtc: data channel opened")
		go s.alertWriter()
	})
	// Fix: if channel is already open, start alertWriter immediately.
	if dc.ReadyState() == webrtc.DataChannelStateOpen {
		log.Printf("webrtc: data channel already open, starting alertWriter")
		go s.alertWriter()
	}

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var m map[string]interface{}
		if json.Unmarshal(msg.Data, &m) == nil && onResponse != nil {
			onResponse("webrtc", m)
		}
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candidate := c.ToJSON()
		s.httpPost("/send", map[string]interface{}{
			"code": s.code,
			"role": "daemon",
			"msg": map[string]interface{}{
				"type":          "ice",
				"candidate":     candidate.Candidate,
				"sdpMid":        candidate.SDPMid,
				"sdpMLineIndex": candidate.SDPMLineIndex,
			},
		})
	})

	// 3. Start polling immediately so we can receive the mobile's answer.
	go s.pollLoop(onResponse)

	// 4. Create and send offer.
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("create offer: %w", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("set local: %w", err)
	}

	// Trickle ICE: send SDP offer immediately, ICE candidates follow as they arrive.
	// No need to block on GatheringCompletePromise.
	s.httpPost("/send", map[string]interface{}{
		"code": s.code,
		"role": "daemon",
		"msg": map[string]interface{}{
			"type": "sdp",
			"sdp":  pc.LocalDescription().SDP,
		},
	})

	return nil
}

func (s *Session) pollLoop(onResponse func(clientIP string, msg map[string]interface{})) {
	defer func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
	}()

	for {
		select {
		case <-s.pollStop:
			return
		case <-time.After(500 * time.Millisecond):
		}

		msgs, newSeq := s.httpPoll()
		s.lastSeq = newSeq

		for _, sm := range msgs {
			var m map[string]interface{}
			if err := json.Unmarshal(sm.Msg, &m); err != nil {
				continue
			}

			switch m["type"] {
			case "sdp":
				sdpType := webrtc.SDPTypeOffer
				if t, ok := m["sdpType"].(string); ok && t == "answer" {
					sdpType = webrtc.SDPTypeAnswer
				}
				if sdp, ok := m["sdp"].(string); ok {
					s.pc.SetRemoteDescription(webrtc.SessionDescription{
						Type: sdpType,
						SDP:  sdp,
					})
				}

			case "ice":
				candidate := webrtc.ICECandidateInit{
					Candidate: m["candidate"].(string),
				}
				if mid, ok := m["sdpMid"].(string); ok {
					candidate.SDPMid = &mid
				}
				if idx, ok := m["sdpMLineIndex"].(float64); ok {
					i := uint16(idx)
					candidate.SDPMLineIndex = &i
				}
				s.pc.AddICECandidate(candidate)

			case "paired":
				// Mobile reconnected — recreate peer connection + offer (BUG-008).
				log.Printf("webrtc: peer reconnected")
				s.mu.Lock()
				// Close old connection if any.
				if s.pc != nil {
					s.pc.Close()
				}
				pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
					ICEServers: []webrtc.ICEServer{{URLs: s.stunServers}},
				})
				if err != nil {
					log.Printf("webrtc: recreate pc: %v", err)
					s.mu.Unlock()
					continue
				}
				dc, err := pc.CreateDataChannel("devremote", nil)
				if err != nil {
					log.Printf("webrtc: recreate dc: %v", err)
					pc.Close()
					s.mu.Unlock()
					continue
				}
				s.pc = pc
				s.dc = dc
				dc.OnOpen(func() { log.Printf("webrtc: data channel opened"); go s.alertWriter() })
				dc.OnMessage(func(msg webrtc.DataChannelMessage) {
					var m map[string]interface{}
					if json.Unmarshal(msg.Data, &m) == nil && onResponse != nil {
						onResponse("webrtc", m)
					}
				})
				pc.OnICECandidate(func(c *webrtc.ICECandidate) {
					if c == nil { return }
					candidate := c.ToJSON()
					s.httpPost("/send", map[string]interface{}{
						"code": s.code, "role": "daemon",
						"msg": map[string]interface{}{
							"type": "ice", "candidate": candidate.Candidate,
							"sdpMid": candidate.SDPMid, "sdpMLineIndex": candidate.SDPMLineIndex,
						},
					})
				})
				s.mu.Unlock()

				offer, err := pc.CreateOffer(nil)
				if err != nil {
					log.Printf("webrtc: recreate offer: %v", err)
					continue
				}
				pc.SetLocalDescription(offer)
				s.httpPost("/send", map[string]interface{}{
					"code": s.code, "role": "daemon",
					"msg": map[string]interface{}{"type": "sdp", "sdp": pc.LocalDescription().SDP},
				})

			case "peer_disconnected":
				log.Printf("webrtc: peer disconnected")
				return
			}
		}
	}
}

func (s *Session) httpPoll() ([]signalingMsg, int) {
	url := fmt.Sprintf("%s/poll?code=%s&role=%s&since=%d",
		s.signalingURL, s.code, "daemon", s.lastSeq)

	resp, err := httpGet(url)
	if err != nil {
		return nil, s.lastSeq
	}

	var result struct {
		Messages []signalingMsg `json:"messages"`
		Since    int            `json:"since"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, s.lastSeq
	}
	return result.Messages, result.Since
}

func (s *Session) httpPost(path string, body interface{}) error {
	data, _ := json.Marshal(body)
	url := s.signalingURL + path
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func httpGet(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (s *Session) alertWriter() {
	for a := range s.alertCh {
		data, _ := json.Marshal(a)
		if err := s.dc.SendText(string(data)); err != nil {
			return
		}
	}
}

// SendRawBytes sends raw binary data directly through the data channel (no JSON).
func (s *Session) SendRawBytes(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.dc == nil || s.dc.ReadyState() != webrtc.DataChannelStateOpen {
		return fmt.Errorf("data channel not open")
	}
	return s.dc.Send(data)
}

func (s *Session) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.dc == nil {
		return false
	}
	return s.dc.ReadyState() == webrtc.DataChannelStateOpen
}

func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	close(s.pollStop)
	close(s.alertCh) // BUG-014: unblock alertWriter goroutine
	if s.dc != nil {
		s.dc.Close()
	}
	if s.pc != nil {
		s.pc.Close()
	}
	s.httpPost("/leave", map[string]interface{}{
		"code": s.code,
		"role": "daemon",
	})
}

func loadOrCreateKey(dir string) string {
	path := filepath.Join(dir, "peer_key")
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return string(data)
	}
	key := randomKeyStr()
	if err := os.MkdirAll(dir, 0700); err == nil {
		os.WriteFile(path, []byte(key), 0600)
	}
	return key
}

func randomKeyStr() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func randomCode() string {
	b := make([]byte, 3)
	rand.Read(b)
	n := int(b[0])<<16 | int(b[1])<<8 | int(b[2])
	return fmt.Sprintf("%06d", n%1000000)
}
