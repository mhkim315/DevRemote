package term

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"sync"

	"devremote/companion-daemon/internal/mux"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

// OwnerUUID is set by the daemon on startup. All JWT tokens must have this sub claim.
var OwnerUUID string

// SupabaseProjectRef is the Supabase project reference (e.g. "abcdefghijklmnop").
// Used to dynamically fetch the JWKS public key for RS256 token verification.
var SupabaseProjectRef string

// verifyToken validates a Supabase JWT using RS256 (JWKS) or HS256 (dev fallback).
// It also checks that the token's sub claim matches the configured OwnerUUID.
func verifyToken(tokenString string) bool {
	if tokenString == "" {
		log.Println("DEV: empty token, allowing (--owner-uuid not set)")
		return OwnerUUID == ""
	}

	// Dev mode: accept any token without signature verification
	if OwnerUUID == "" && SupabaseProjectRef == "" {
		parser := jwt.NewParser()
		token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
		if err != nil { log.Printf("WS pty start err: %v", err)
			log.Printf("JWT parse err (dev): %v", err)
			return false
		}
		if token != nil {
			sub, _ := token.Claims.(jwt.MapClaims)["sub"]
			log.Printf("DEV: accepted token (sub=%v)", sub)
			return true
		}
		return false
	}

	// Production mode: verify signature via Supabase JWKS
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		alg := token.Header["alg"]

		if _, ok := token.Method.(*jwt.SigningMethodECDSA); ok {
			if SupabaseProjectRef == "" {
				return nil, fmt.Errorf("ES256 requires SupabaseProjectRef")
			}
			kid, _ := token.Header["kid"].(string)
			return fetchJWKSKey(SupabaseProjectRef, kid)
		}
		if _, ok := token.Method.(*jwt.SigningMethodRSA); ok {
			if SupabaseProjectRef == "" {
				return nil, fmt.Errorf("RS256 requires SupabaseProjectRef")
			}
			kid, _ := token.Header["kid"].(string)
			return fetchJWKSKey(SupabaseProjectRef, kid)
		}
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); ok {
			return []byte("dev-secret-do-not-use-in-production"), nil
		}

		return nil, fmt.Errorf("unsupported signing method: %v", alg)
	}

	token, err := jwt.Parse(tokenString, keyFunc)
	if err != nil { log.Printf("WS pty start err: %v", err)
		log.Printf("JWT parse err: %v", err)
		return false
	}

	if !token.Valid {
		return false
	}

	// Check owner UUID
	if OwnerUUID != "" {
		sub, _ := token.Claims.GetSubject()
		if sub == "" {
			log.Printf("JWT rejected: missing sub claim")
			return false
		}
		if sub != OwnerUUID {
			log.Printf("JWT rejected: sub=%q != owner=%q", sub, OwnerUUID)
			return false
		}
	}

	return true
}

func HandleSessionsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		HandleSessionsV2(w, r)
	} else {
		HandleSessionCRUD(w, r)
	}
}

func HandleSessionCRUD(w http.ResponseWriter, r *http.Request) {
	// Auth check
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	} else {
		token = r.URL.Query().Get("token")
	}
	if !verifyToken(token) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method == "POST" || r.Method == "PUT" {
		var req struct {
			ID          string `json:"id"`
			Runner      string `json:"runner"`
			RunnerColor string `json:"runnerColor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		if r.Method == "POST" {
			mux.NewSession(req.ID, "bash")
		}
		
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
		return
	}

	if r.Method == "DELETE" {
		id := r.URL.Query().Get("id")
		if id != "" {
			if s, ok := mux.GetSession(id); ok {
				s.PTY.Close()
			}
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
		return
	}

	http.Error(w, "Method not allowed", 405)
}

var (
	jwksCache   map[string]interface{}
	jwksCacheMu sync.Mutex
)

func fetchJWKSKey(projectRef, kid string) (interface{}, error) {
	jwksCacheMu.Lock()
	defer jwksCacheMu.Unlock()

	if jwksCache == nil {
		url := fmt.Sprintf("https://%s.supabase.co/auth/v1/.well-known/jwks.json", projectRef)
		resp, err := http.Get(url)
		if err != nil { log.Printf("WS pty start err: %v", err)
			return nil, fmt.Errorf("jwks fetch: %w", err)
		}
		defer resp.Body.Close()

		var jwks struct {
			Keys []struct {
				Kid string `json:"kid"`
				Kty string `json:"kty"`
				X   string `json:"x"`
				Y   string `json:"y"`
				N   string `json:"n"`
				E   string `json:"e"`
			} `json:"keys"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
			return nil, fmt.Errorf("jwks decode: %w", err)
		}

		jwksCache = make(map[string]interface{})
		for _, k := range jwks.Keys {
			if k.Kid == "" {
				continue
			}
			switch k.Kty {
			case "EC":
				xb, _ := base64.RawURLEncoding.DecodeString(k.X)
				yb, _ := base64.RawURLEncoding.DecodeString(k.Y)
				jwksCache[k.Kid] = &ecdsa.PublicKey{
					Curve: elliptic.P256(),
					X:     new(big.Int).SetBytes(xb),
					Y:     new(big.Int).SetBytes(yb),
				}
			}
		}
		log.Printf("jwks: loaded %d keys", len(jwksCache))
	}

	if key, ok := jwksCache[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("key %q not found in JWKS", kid)
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// OnApproval is called when Claude asks for user approval.
var OnApproval func(string)

func HandleWS(w http.ResponseWriter, r *http.Request) {
	// Extract JWT from Authorization header (preferred) or ?token= query param
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	} else {
		token = r.URL.Query().Get("token")
	}

	if !verifyToken(token) {
		log.Printf("WS Unauthorized from %s", r.RemoteAddr)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}

	s, ok := mux.GetSession(session)
	if !ok {
		var err error
		s, err = mux.NewSession(session, "bash")
		if err != nil {
			log.Printf("WS new session err: %v", err)
			http.Error(w, "session failed", 500)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { log.Printf("WS upgrade err: %v", err)
		return
	}
	defer conn.Close()

	ch := make(chan []byte, 100)
	s.AddListener(ch)
	defer s.RemoveListener(ch)

	log.Printf("WS [%s]: %s connected", session, r.RemoteAddr)

	go func() {
		for data := range ch {
			if writeErr := conn.WriteMessage(websocket.BinaryMessage, data); writeErr != nil {
				return
			}
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		s.Write(msg)
	}
}

func isApprovalPrompt(data []byte) (bool, string) {
	if !bytes.Contains(data, []byte("Do you want")) &&
		!bytes.Contains(data, []byte("proceed?")) &&
		!bytes.Contains(data, []byte("(y/n)")) &&
		!bytes.Contains(data, []byte("(y/N)")) &&
		!(bytes.Contains(data, []byte("1. Yes")) && bytes.Contains(data, []byte("No"))) {
		return false, ""
	}

	cleanLine := ""
	inEsc := false
	for _, b := range data {
		if b == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') {
				inEsc = false
			}
			continue
		}
		if b >= 32 && b <= 126 || b == '\n' || b == '\t' {
			cleanLine += string(b)
		}
	}

	if len(cleanLine) > 150 {
		cleanLine = "..." + cleanLine[len(cleanLine)-150:]
	}
	return true, "Agent: " + cleanLine
}

func HandleHTML(w http.ResponseWriter, r *http.Request) {
	// ... we will keep HandleHTML as is, though not heavily used
	// Auth check
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	} else {
		token = r.URL.Query().Get("token")
	}
	if !verifyToken(token) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	io.WriteString(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/><script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.min.js"></script><style>*{margin:0;padding:0}html,body{width:100%;height:100%;background:#000}#t{width:100%;height:100%}#status{position:fixed;top:4px;right:8px;color:#888;font:12px monospace;z-index:9;padding:2px 8px;border-radius:4px;background:rgba(0,0,0,0.7);display:none}</style></head><body><div id="t"></div><div id="status" style="display:none"></div><script>var raw='',reconnecting=false;function connect(){if(reconnecting)return;var protocol=location.protocol==='https:'?'wss://':'ws://';var s=document.getElementById('status');if(window.ws)try{window.ws.onclose=null;window.ws.close()}catch(e){}var ws=new WebSocket(protocol+location.host+"/term/ws"+location.search);window.ws=ws;ws.binaryType='arraybuffer';ws.onopen=function(){reconnecting=false;setTimeout(function(){fitTerminal()},500)};ws.onmessage=function(e){var t=typeof e.data==='string'?e.data:new TextDecoder().decode(e.data);raw+=t;term.write(t)};ws.onclose=function(){if(!reconnecting){reconnecting=true;setTimeout(function(){reconnecting=false;connect()},2000)}};ws.onerror=function(){ws.close()}}var term=new Terminal({scrollback:50000,fontSize:12,fontFamily:'Menlo,Monaco,"Courier New",monospace',theme:{background:"#000",foreground:"#ccc"}});term.open(document.getElementById("t"));term.onData(function(d){var w=window.ws;if(w&&w.readyState===1)try{w.send(d)}catch(e){}});setTimeout(function(){term.focus();fitTerminal();},500);
function fitTerminal(){var h=document.getElementById('t').clientHeight;var w=document.getElementById('t').clientWidth;var rows=Math.floor(h/17);var cols=Math.floor(w/7.8);if(rows>0&&cols>0){fetch('/term/size'+location.search+'&rows='+rows+'&cols='+cols,{method:'POST'}).catch(function(){})}};
window.addEventListener('resize',function(){fitTerminal()});setInterval(function(){fetch("/debug/cmd"+location.search).then(function(r){return r.text()}).then(function(d){var w=window.ws;if(d&&w&&w.readyState===1)try{w.send(d+"\n")}catch(e){}}).catch(function(){})},2000);connect();</script></body></html>`)
}

var (
	pendingCmds = make(map[string]string)
	cmdMu       sync.Mutex
)

func HandleCmd(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}
	if r.Method == "POST" {
		body, _ := io.ReadAll(r.Body)
		cmdMu.Lock()
		pendingCmds[session] = string(body)
		log.Printf("CMD POST [%s]: %q", session, pendingCmds[session])
		cmdMu.Unlock()
		w.WriteHeader(200)
		return
	}
	cmdMu.Lock()
	cmd := pendingCmds[session]
	if cmd != "" {
		pendingCmds[session] = ""
	}
	cmdMu.Unlock()
	w.Write([]byte(cmd))
}

func HandleSize(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		return
	}
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	if rows > 0 && cols > 0 {
		if s, ok := mux.GetSession(session); ok {
			s.Resize(rows, cols)
		}
	}
}

func HandleDump(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}
	body, _ := io.ReadAll(r.Body)
	if len(body) > 0 {
		log.Printf("PHONE [%s]: %s", session, string(body))
	}
}
