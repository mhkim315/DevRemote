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
	ptm, _ := pty.Start(cmd)
	
	go io.Copy(os.Stdout, ptm)

	http.HandleFunc("/ws3", func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil { break }
			ptm.Write(msg)
		}
	})
	
	log.Println("Listening :9997")
	http.ListenAndServe(":9997", nil)
}
