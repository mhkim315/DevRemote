package term

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"

	"devremote/companion-daemon/internal/mux"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

// OwnerUUID is set by the daemon on startup. All JWT tokens must have this sub claim.
var OwnerUUID string

// SupabaseProjectRef is the Supabase project reference (e.g. "abcdefghijklmnop").
// Used to dynamically fetch the JWKS public key for RS256 token verification.
var SupabaseProjectRef string

var InsecureLocalOnly bool

// VerifyToken validates a Supabase JWT using RS256 (JWKS) or HS256 (dev fallback).
// It also checks that the token's sub claim matches the configured OwnerUUID.
func VerifyToken(tokenString string) bool {
	if tokenString == "" {
		if InsecureLocalOnly {
			log.Println("WARN: empty token allowed due to --insecure-local-only")
			return true
		}
		return false
	}

	// Dev mode: accept any token without signature verification ONLY if explicitly enabled
	if InsecureLocalOnly {
		parser := jwt.NewParser()
		token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
		if err != nil {
			log.Printf("JWT parse err (dev): %v", err)
			return false
		}
		if token != nil {
			sub, _ := token.Claims.(jwt.MapClaims)["sub"]
			log.Printf("WARN: accepted unverified token in insecure mode (sub=%v)", sub)
			return true
		}
		return false
	}

	// Production mode: fail closed if required config is missing
	if OwnerUUID == "" || SupabaseProjectRef == "" {
		log.Printf("ERR: Missing OwnerUUID or SupabaseProjectRef in production mode")
		return false
	}

	// Production mode: verify signature via Supabase JWKS
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		alg := token.Header["alg"]

		if _, ok := token.Method.(*jwt.SigningMethodRSA); ok {
			if SupabaseProjectRef == "" {
				return nil, fmt.Errorf("RS256 requires SupabaseProjectRef")
			}
			kid, _ := token.Header["kid"].(string)
			return fetchJWKSKey(SupabaseProjectRef, kid)
		}
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); ok {
			if SupabaseProjectRef == "" {
				return nil, fmt.Errorf("ES256 requires SupabaseProjectRef")
			}
			kid, _ := token.Header["kid"].(string)
			return fetchJWKSKey(SupabaseProjectRef, kid)
		}

		return nil, fmt.Errorf("unsupported signing method: %v", alg)
	}

	token, err := jwt.Parse(tokenString, keyFunc)
	if err != nil {
		log.Printf("WS pty start err: %v", err)
		log.Printf("JWT parse err: %v", err)
		return false
	}

	if !token.Valid {
		return false
	}

	// Check issuer
	iss, _ := token.Claims.GetIssuer()
	expectedIss := "https://" + SupabaseProjectRef + ".supabase.co/auth/v1"
	if iss != expectedIss {
		log.Printf("JWT rejected: invalid issuer %q", iss)
		return false
	}

	// Check audience
	aud, _ := token.Claims.GetAudience()
	validAud := false
	for _, a := range aud {
		if a == "authenticated" {
			validAud = true
			break
		}
	}
	if !validAud {
		log.Printf("JWT rejected: invalid audience %v", aud)
		return false
	}

	// Check owner UUID
	sub, _ := token.Claims.GetSubject()
	if sub == "" {
		log.Printf("JWT rejected: missing sub claim")
		return false
	}
	if sub != OwnerUUID {
		log.Printf("JWT rejected: sub=%q != owner=%q", sub, OwnerUUID)
		return false
	}

	return true
}

func (h *Handlers) HandleSessionsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		h.HandleSessionsV2(w, r)
	} else {
		h.HandleSessionCRUD(w, r)
	}
}
func ExtractToken(r *http.Request) string {
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		return token[7:]
	}
	return r.URL.Query().Get("token")
}

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := ExtractToken(r)
		if !VerifyToken(token) {
			log.Printf("Auth failed for %s", r.URL.Path)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (h *Handlers) HandleSessionCRUD(w http.ResponseWriter, r *http.Request) {
	reg := h.Registry

	if r.Method == "POST" || r.Method == "PUT" {
		var req struct {
			ID          string `json:"id"`
			WorkspaceID string `json:"workspaceId"`
			Runner      string `json:"runner"`
			RunnerColor string `json:"runnerColor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		ref := mux.ParseSessionID(req.ID)

		adapterName := ref.Adapter
		if adapterName == "" {
			adapterName = "tmux" // Fallback
		}

		if r.Method == http.MethodPost {
			opts := mux.CreateOptions{Name: ref.RawID, WorkspaceID: req.WorkspaceID}
			createdID, err := reg.CreateSession(r.Context(), adapterName, opts)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to create session: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(fmt.Sprintf(`{"status":"ok","id":"%s"}`, createdID)))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
		return
	}

	if r.Method == "DELETE" {
		id := r.URL.Query().Get("id")
		if id != "" {
			ref := mux.ParseSessionID(id)
			adapterName := ref.Adapter
			if adapterName == "" {
				adapterName = "tmux"
			}

			if err := reg.TerminateSession(r.Context(), adapterName, ref.RawID); err != nil {
				http.Error(w, fmt.Sprintf("failed to terminate session: %v", err), http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
		return
	}

	http.Error(w, "Method not allowed", 405)
}

var (
	jwksCache       map[string]interface{}
	jwksCacheExpiry time.Time
	jwksCacheMu     sync.Mutex
)

func fetchJWKSKey(projectRef, kid string) (interface{}, error) {
	jwksCacheMu.Lock()
	defer jwksCacheMu.Unlock()

	needsFetch := jwksCache == nil || time.Now().After(jwksCacheExpiry)
	if !needsFetch {
		if _, ok := jwksCache[kid]; !ok {
			needsFetch = true
		}
	}

	if needsFetch {
		url := fmt.Sprintf("https://%s.supabase.co/auth/v1/.well-known/jwks.json", projectRef)
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(url)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
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
				if err := json.NewDecoder(resp.Body).Decode(&jwks); err == nil {
					newCache := make(map[string]interface{})
					for _, k := range jwks.Keys {
						if k.Kid == "" {
							continue
						}
						switch k.Kty {
						case "RSA":
							nBytes, err1 := base64.RawURLEncoding.DecodeString(k.N)
							eBytes, err2 := base64.RawURLEncoding.DecodeString(k.E)
							if err1 != nil || err2 != nil || len(eBytes) == 0 {
								continue
							}
							e := int(new(big.Int).SetBytes(eBytes).Int64())
							newCache[k.Kid] = &rsa.PublicKey{
								N: new(big.Int).SetBytes(nBytes),
								E: e,
							}
						case "EC":
							xb, err1 := base64.RawURLEncoding.DecodeString(k.X)
							yb, err2 := base64.RawURLEncoding.DecodeString(k.Y)
							if err1 != nil || err2 != nil {
								continue
							}
							newCache[k.Kid] = &ecdsa.PublicKey{
								Curve: elliptic.P256(),
								X:     new(big.Int).SetBytes(xb),
								Y:     new(big.Int).SetBytes(yb),
							}
						}
					}
					jwksCache = newCache
					jwksCacheExpiry = time.Now().Add(1 * time.Hour)
					log.Printf("jwks: loaded %d keys", len(jwksCache))
				} else {
					log.Printf("jwks decode err: %v", err)
				}
			} else {
				log.Printf("jwks fetch status %d", resp.StatusCode)
			}
		} else {
			log.Printf("jwks fetch err: %v", err)
		}
	}

	if key, ok := jwksCache[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("key %q not found in JWKS", kid)
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// OnApproval is called when Claude asks for user approval.
var OnApproval func(string)

func (h *Handlers) HandleWS(w http.ResponseWriter, r *http.Request) {
	reg := h.Registry

	// Extract JWT from Authorization header (preferred) or ?token= query param
	// Auth check is handled by middleware

	session := r.URL.Query().Get("session")
	if session == "" {
		session = "devremote"
	}

	var s mux.Session
	var err error
	s, err = reg.FindSession(r.Context(), session)
	if err != nil {
		log.Printf("WS session not found err: %v", err)
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var stream mux.TerminalStream
	if opener, ok := s.(mux.StreamOpener); ok {
		stream, err = opener.OpenStream(r.Context())
		if err != nil {
			log.Printf("WS stream open err: %v", err)
			http.Error(w, "stream failed", 500)
			return
		}
		defer stream.Close()
	} else {
		http.Error(w, "Session does not support streaming", http.StatusNotImplemented)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS upgrade err: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("WS [%s]: %s connected", session, r.RemoteAddr)

	type wsOutbound struct {
		messageType int
		payload     []byte
	}

	outbound := make(chan wsOutbound, 32)
	fatalErr := make(chan error, 1)
	fatalRequested := make(chan struct{})
	shutdown := make(chan struct{})
	writerDone := make(chan struct{})
	var closeOnce sync.Once
	var shutdownOnce sync.Once

	// Helper to trigger a graceful close
	triggerClose := func(err error) {
		closeOnce.Do(func() {
			close(fatalRequested)
			fatalErr <- err
		})
	}
	stopWriter := func() {
		shutdownOnce.Do(func() {
			close(shutdown)
		})
	}

	// Single writer goroutine to prevent Gorilla WS panic
	go func() {
		defer close(writerDone)
		defer conn.Close()
		for {
			select {
			case msg := <-outbound:
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := conn.WriteMessage(msg.messageType, msg.payload); err != nil {
					return
				}
			case err := <-fatalErr:
				// Send 1011 close frame
				reason := "stream error"
				if err != nil {
					// Ensure valid UTF-8 and < 125 bytes
					errMsg := err.Error()
					if len(errMsg) > 100 {
						errMsg = errMsg[:100] + "..."
					}
					reason = "error: " + errMsg
				}
				conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(1011, reason))
				return
			case <-shutdown:
				return
			}
		}
	}()

	if stream != nil {
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := stream.Read(buf)
				if err != nil {
					log.Printf("WS stream read err: %v", err)
					triggerClose(fmt.Errorf("stream failed"))
					break
				}
				// Copy buffer since we're passing it to channel
				payload := make([]byte, n)
				copy(payload, buf[:n])
				select {
				case outbound <- wsOutbound{messageType: websocket.BinaryMessage, payload: payload}:
				case <-writerDone:
					return
				case <-r.Context().Done():
					return
				}
			}
		}()
	}

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if writer, ok := s.(mux.InputWriter); ok {
			if inErr := writer.WriteInput(r.Context(), msg); inErr != nil {
				log.Printf("WS input write err: %v", inErr)
				triggerClose(fmt.Errorf("input failed"))
				break
			}
		} else if stream != nil {
			stream.Write(msg)
		}
	}

	// Ensure stream closes when client disconnects
	if stream != nil {
		stream.Close()
	}
	closeOnce.Do(func() {}) // prevent fatalErr channel block
	select {
	case <-fatalRequested:
		// The writer owns the 1011 close handshake.
	default:
		stopWriter()
	}
	<-writerDone
}

func (h *Handlers) HandleHTML(w http.ResponseWriter, r *http.Request) {
	// ... we will keep HandleHTML as is, though not heavily used
	// Auth check is handled by middleware

	io.WriteString(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/><script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.min.js"></script><style>*{margin:0;padding:0}html,body{width:100%;height:100%;background:#000}#t{width:100%;height:100%}#status{position:fixed;top:4px;right:8px;color:#888;font:12px monospace;z-index:9;padding:2px 8px;border-radius:4px;background:rgba(0,0,0,0.7);display:none}</style></head><body><div id="t"></div><div id="status" style="display:none"></div><script>var raw='',reconnecting=false;function connect(){if(reconnecting)return;var protocol=location.protocol==='https:'?'wss://':'ws://';var s=document.getElementById('status');if(window.ws)try{window.ws.onclose=null;window.ws.close()}catch(e){}var ws=new WebSocket(protocol+location.host+"/term/ws"+location.search);window.ws=ws;ws.binaryType='arraybuffer';ws.onopen=function(){reconnecting=false;setTimeout(function(){fitTerminal()},500)};ws.onmessage=function(e){var t=typeof e.data==='string'?e.data:new TextDecoder().decode(e.data);raw+=t;term.write(t)};ws.onclose=function(){if(!reconnecting){reconnecting=true;setTimeout(function(){reconnecting=false;connect()},2000)}};ws.onerror=function(){ws.close()}}var term=new Terminal({scrollback:50000,fontSize:12,fontFamily:'Menlo,Monaco,"Courier New",monospace',theme:{background:"#000",foreground:"#ccc"}});term.open(document.getElementById("t"));term.onData(function(d){var w=window.ws;if(w&&w.readyState===1)try{w.send(d)}catch(e){}});setTimeout(function(){term.focus();fitTerminal();},500);
function fitTerminal(){var h=document.getElementById('t').clientHeight;var w=document.getElementById('t').clientWidth;var rows=Math.floor(h/17);var cols=Math.floor(w/7.8);if(rows>0&&cols>0){var p=new URLSearchParams(location.search);var sess=p.get('session');var tok=p.get('token');var hdrs={};if(tok)hdrs['Authorization']='Bearer '+tok;fetch('/term/size?session='+encodeURIComponent(sess)+'&rows='+rows+'&cols='+cols,{method:'POST',headers:hdrs}).catch(function(){})}};
window.addEventListener('resize',function(){fitTerminal()});setInterval(function(){var p=new URLSearchParams(location.search);var sess=p.get('session');var tok=p.get('token');var hdrs={};if(tok)hdrs['Authorization']='Bearer '+tok;fetch("/debug/cmd?session="+encodeURIComponent(sess),{headers:hdrs}).then(function(r){return r.text()}).then(function(d){var w=window.ws;if(d&&w&&w.readyState===1)try{w.send(d+"\n")}catch(e){}}).catch(function(){})},2000);connect();</script></body></html>`)
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
