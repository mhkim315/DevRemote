package webrtc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"devremote/companion-daemon/internal/detector"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

// Session manages the daemon-side WebRTC peer connection.
type Session struct {
	peerKey      string
	code         string
	signalingURL string
	stunServers  []string

	pc        *webrtc.PeerConnection
	dc        *webrtc.DataChannel
	sigConn   *websocket.Conn
	onAlert   func(detector.Alert)
	alertCh   chan detector.Alert

	mu     sync.Mutex
	closed bool
}

// New creates a WebRTC session. Loads or generates a persistent peer key.
func New(signalingURL string, stunServers []string, keyDir string) *Session {
	return &Session{
		signalingURL: signalingURL,
		stunServers:  stunServers,
		peerKey:      loadOrCreateKey(keyDir),
		code:         randomCode(),
		alertCh:      make(chan detector.Alert, 64),
	}
}

// Code returns the 6-digit session code for first-time pairing.
func (s *Session) Code() string { return s.code }

// HandleAlert enqueues an alert for sending over the data channel.
func (s *Session) HandleAlert(a detector.Alert) {
	select {
	case s.alertCh <- a:
	default:
	}
}

// Start connects to the signaling server and establishes the WebRTC peer.
func (s *Session) Start(onResponse func(clientIP string, msg map[string]interface{})) error {
	// 1. Connect to signaling server.
	conn, _, err := websocket.DefaultDialer.Dial(s.signalingURL, nil)
	if err != nil {
		return fmt.Errorf("signaling connect: %w", err)
	}
	s.sigConn = conn

	// 2. Join as daemon with code.
	if err := conn.WriteJSON(map[string]string{
		"type": "join",
		"role": "daemon",
		"code": s.code,
		"key":  s.peerKey,
	}); err != nil {
		return fmt.Errorf("join: %w", err)
	}

	// 3. Wait for joined acknowledgment.
	var ack map[string]interface{}
	if err := conn.ReadJSON(&ack); err != nil {
		return fmt.Errorf("read joined: %w", err)
	}
	if ack["type"] != "joined" {
		return fmt.Errorf("expected joined, got %v", ack["type"])
	}
	log.Printf("webrtc: joined signaling (code=%s)", s.code)

	// 4. Create peer connection.
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: s.stunServers},
		},
	})
	if err != nil {
		return fmt.Errorf("peer connection: %w", err)
	}
	s.pc = pc

	// 5. Create data channel.
	dc, err := pc.CreateDataChannel("devremote", nil)
	if err != nil {
		return fmt.Errorf("create data channel: %w", err)
	}
	s.dc = dc

	dc.OnOpen(func() {
		log.Printf("webrtc: data channel opened")
		go s.alertWriter()
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var m map[string]interface{}
		if json.Unmarshal(msg.Data, &m) == nil && onResponse != nil {
			onResponse("webrtc", m)
		}
	})

	// 6. Handle ICE candidates.
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candidate := c.ToJSON()
		conn.WriteJSON(map[string]interface{}{
			"type":          "ice",
			"candidate":     candidate.Candidate,
			"sdpMid":        candidate.SDPMid,
			"sdpMLineIndex": candidate.SDPMLineIndex,
		})
	})

	// 7. Start signaling message loop in background.
	go s.signalingLoop(onResponse)

	// 8. Create and send offer.
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("create offer: %w", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("set local: %w", err)
	}

	// Waiting for ICE gathering to complete before sending.
	<-webrtc.GatheringCompletePromise(pc)

	conn.WriteJSON(map[string]interface{}{
		"type": "sdp",
		"sdp":  pc.LocalDescription().SDP,
	})

	return nil
}

func (s *Session) signalingLoop(onResponse func(clientIP string, msg map[string]interface{})) {
	defer func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
	}()

	for {
		var msg map[string]interface{}
		if err := s.sigConn.ReadJSON(&msg); err != nil {
			log.Printf("webrtc: signaling read error: %v", err)
			return
		}

		switch msg["type"] {
		case "sdp":
			sdp := webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  msg["sdp"].(string),
			}
			if t, ok := msg["sdpType"].(string); ok && t == "answer" {
				sdp.Type = webrtc.SDPTypeAnswer
			}
			// Detect type from existing key if sdpType not present.
			if _, ok := msg["sdpType"]; !ok {
				if _, exists := msg["answer"]; exists {
					sdp.Type = webrtc.SDPTypeAnswer
				}
			}
			if err := s.pc.SetRemoteDescription(sdp); err != nil {
				log.Printf("webrtc: set remote: %v", err)
			}

		case "ice":
			candidate := webrtc.ICECandidateInit{
				Candidate: msg["candidate"].(string),
			}
			if mid, ok := msg["sdpMid"].(string); ok {
				candidate.SDPMid = &mid
			}
			if idx, ok := msg["sdpMLineIndex"].(float64); ok {
				i := uint16(idx)
				candidate.SDPMLineIndex = &i
			}
			if err := s.pc.AddICECandidate(candidate); err != nil {
				log.Printf("webrtc: add ice: %v", err)
			}

		case "key":
			// Received persistent key from signaling (already have it).
			if key, ok := msg["key"].(string); ok {
				log.Printf("webrtc: paired with key=%s", key)
				_ = key
			}

		case "peer_disconnected":
			log.Printf("webrtc: peer disconnected")
			return
		}
	}
}

func (s *Session) alertWriter() {
	for a := range s.alertCh {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		data, _ := json.Marshal(a)
		if err := s.dc.SendText(string(data)); err != nil {
			log.Printf("webrtc: send alert: %v", err)
			return
		}
	}
}

// Close tears down the session.
func (s *Session) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.dc != nil {
		s.dc.Close()
	}
	if s.pc != nil {
		s.pc.Close()
	}
	if s.sigConn != nil {
		s.sigConn.Close()
	}
}

func loadOrCreateKey(dir string) string {
	path := filepath.Join(dir, "peer_key")
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return string(data)
	}
	key := randomKey()
	if err := os.MkdirAll(dir, 0700); err == nil {
		os.WriteFile(path, []byte(key), 0600)
	}
	return key
}

func randomKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func randomCode() string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("%06d", int(b[0])<<16|int(b[1])<<8|int(b[2]))
}
