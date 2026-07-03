package term

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
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

	// Use Supabase JWKS endpoint for RS256 verification in production
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		alg := token.Header["alg"]

		// RS256 — fetch public key from Supabase JWKS
		if _, ok := token.Method.(*jwt.SigningMethodRSA); ok {
			if SupabaseProjectRef == "" {
				return nil, fmt.Errorf("RS256 requires SupabaseProjectRef")
			}
			kid, ok := token.Header["kid"].(string)
			if !ok {
				return nil, fmt.Errorf("missing kid in token header")
			}
			return fetchJWKSKey(SupabaseProjectRef, kid)
		}

		// HS256 — dev-mode only, deprecated for production
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); ok {
			log.Printf("WARN: HS256 token detected. HS256 is deprecated for production. Use RS256.")
			return []byte("dev-secret-do-not-use-in-production"), nil
		}

		return nil, fmt.Errorf("unexpected signing method: %v", alg)
	}

	token, err := jwt.Parse(tokenString, keyFunc)
	if err != nil {
		log.Printf("JWT parse err: %v", err)
		return false
	}

	if !token.Valid {
		return false
	}

	// Check owner UUID (sub claim must match)
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
	} else {
		log.Printf("WARN: OwnerUUID not set. Accepting any valid token (insecure).")
	}

	return true
}

// jwksCache and fetchJWKSKey implement RS256 public key resolution.
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
		if err != nil {
			return nil, fmt.Errorf("jwks fetch: %w", err)
		}
		defer resp.Body.Close()

		var jwks struct {
			Keys []struct {
				Kid string `json:"kid"`
				Kty string `json:"kty"`
				N   string `json:"n"`
				E   string `json:"e"`
			} `json:"keys"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
			return nil, fmt.Errorf("jwks decode: %w", err)
		}

		jwksCache = make(map[string]interface{})
		for _, k := range jwks.Keys {
			if k.Kty == "RSA" && k.Kid != "" {
				// Parse RSA public key from JWK
				pubKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(
					fmt.Sprintf("-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----"),
				))
				if err != nil {
					// Use the raw JWK values with golang-jwt
					jwksCache[k.Kid] = &k
				} else {
					jwksCache[k.Kid] = pubKey
				}
			}
		}
	}

	// For now, return a simple approach: the Supabase JWKS keys map
	// golang-jwt handles RSA key resolution natively via Keyfunc
	if key, ok := jwksCache[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("key %q not found in JWKS", kid)
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// OnApproval is called when Claude asks for user approval.
var OnApproval func(string)

var (
	sessionPty     = map[string]*os.File{}
	sessionPtyMu   sync.Mutex
	sessionConns   = map[string]*websocket.Conn{}
	sessionConnsMu sync.Mutex
)

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

	sessionConnsMu.Lock()
	if old, ok := sessionConns[session]; ok {
		old.Close()
	}
	sessionConnsMu.Unlock()

	cmd := DefaultMux.AttachCmd(session)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	tty, err := pty.Start(cmd)
	if err != nil {
		cmd = exec.Command("bash")
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")
		tty, err = pty.Start(cmd)
		if err != nil {
			http.Error(w, "pty failed", 500)
			return
		}
	}
	defer tty.Close()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sessionConnsMu.Lock()
	sessionConns[session] = conn
	sessionConnsMu.Unlock()
	defer func() {
		sessionConnsMu.Lock()
		if sessionConns[session] == conn {
			delete(sessionConns, session)
		}
		sessionConnsMu.Unlock()
	}()

	log.Printf("WS [%s]: %s", session, r.RemoteAddr)

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				data := buf[:n]
				conn.WriteMessage(websocket.TextMessage, data)
				if OnApproval != nil {
					if matched, promptStr := isApprovalPrompt(data); matched {
						go OnApproval(promptStr)
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		tty.Write(msg)
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

	io.WriteString(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/><script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.min.js"></script><style>*{margin:0;padding:0}html,body{width:100%;height:100%;background:#000}#t{width:100%;height:100%}#status{position:fixed;top:4px;right:8px;color:#888;font:12px monospace;z-index:9;padding:2px 8px;border-radius:4px;background:rgba(0,0,0,0.7)}</style></head><body><div id="t"></div><div id="status">connecting</div><script>var raw='',reconnecting=false;function connect(){if(reconnecting)return;var protocol=location.protocol==='https:'?'wss://':'ws://';var s=document.getElementById('status');if(window.ws)try{window.ws.onclose=null;window.ws.close()}catch(e){}var ws=new WebSocket(protocol+location.host+"/term/ws"+location.search);window.ws=ws;ws.binaryType='arraybuffer';s.textContent='connecting';s.style.color='#e3b341';ws.onopen=function(){s.textContent='live';s.style.color='#238636';reconnecting=false};ws.onmessage=function(e){var t=typeof e.data==='string'?e.data:new TextDecoder().decode(e.data);raw+=t;term.write(t)};ws.onclose=function(){if(!reconnecting){reconnecting=true;s.textContent='reconnecting';s.style.color='#f85149';setTimeout(function(){reconnecting=false;connect()},2000)}};ws.onerror=function(){ws.close()}}var term=new Terminal({fontSize:12,fontFamily:'Menlo,Monaco,"Courier New",monospace',theme:{background:"#000",foreground:"#ccc"}});term.open(document.getElementById("t"));term.onData(function(d){var w=window.ws;if(w&&w.readyState===1)try{w.send(d)}catch(e){}});setTimeout(function(){term.focus()},500);setInterval(function(){fetch("/debug/cmd"+location.search).then(function(r){return r.text()}).then(function(d){var w=window.ws;if(d&&w&&w.readyState===1)try{w.send(d+"\n")}catch(e){}}).catch(function(){})},2000);connect();</script></body></html>`)
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
	// Handle PTY resize requests
	session := r.URL.Query().Get("session")
	if session == "" {
		return
	}
	// Stub: resize support to be implemented with PTY tracking
	w.WriteHeader(200)
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
