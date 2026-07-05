package main

import (
	"log"
	"time"
	"github.com/gorilla/websocket"
)

func main() {
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:9997/ws3", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	msg := "hello"
	for _, c := range msg {
		err = conn.WriteMessage(websocket.BinaryMessage, []byte{byte(c)})
		if err != nil {
			log.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
}
