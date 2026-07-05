package main

import (
	"log"
	"net/http"
	"os/exec"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"io"
	"os"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func main() {
	cmd := exec.Command("sh", "-c", "claude")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptm, _ := pty.Start(cmd)
	pty.Setsize(ptm, &pty.Winsize{Rows: 24, Cols: 80})
	
	go io.Copy(os.Stdout, ptm)

	http.HandleFunc("/ws4", func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil { break }
			ptm.Write(msg)
		}
	})
	
	log.Println("Listening :9996")
	http.ListenAndServe(":9996", nil)
}
