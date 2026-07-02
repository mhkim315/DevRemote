package term

import (
	"io"
	"net/http"
	"os/exec"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func HandleWS(w http.ResponseWriter, r *http.Request) {
	cmd := exec.Command("bash")
	tty, err := pty.Start(cmd)
	if err != nil { http.Error(w, "pty failed", 500); return }
	defer tty.Close()
	defer cmd.Process.Kill()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	defer conn.Close()
	go func() {
		buf := make([]byte, 4096)
		for { n, err := tty.Read(buf); if n > 0 { conn.WriteMessage(websocket.TextMessage, buf[:n]) }; if err != nil { return } }
	}()
	for { _, msg, err := conn.ReadMessage(); if err != nil { return }; tty.Write(msg) }
}

func HandleHTML(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/><script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.min.js"></script><style>*{margin:0;padding:0}html,body{width:100%;height:100%;background:#000}#t{width:100%;height:100%}</style></head><body><div id="t"></div><script>window.term=new Terminal({fontSize:12,fontFamily:'Menlo,Monaco,"Courier New",monospace',theme:{background:"#000",foreground:"#ccc"}});window.term.open(document.getElementById("t"));var ws=window.ws=new WebSocket("ws://"+location.host+"/term/ws");window.ws.onmessage=function(e){t.write(e.data)};t.onData(function(d){window.ws.send(d)});setTimeout(function(){t.textarea.focus()},500);</script></body></html>`)
}
