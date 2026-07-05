package main

import (
	"log"
	"time"
	"github.com/gorilla/websocket"
)

func main() {
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:9996/ws4", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	err = conn.WriteMessage(websocket.BinaryMessage, []byte("hello"))
	if err != nil {
		log.Fatal(err)
	}
	time.Sleep(2 * time.Second)
}
