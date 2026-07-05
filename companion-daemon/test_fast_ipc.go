package main

import (
	"net"
	"time"
)

func main() {
	conn, _ := net.Dial("unix", "/tmp/pokit.sock")
	conn.Write([]byte("cmd:claude\n\n"))
	
	time.Sleep(1 * time.Second)
	
	msg := "hello"
	for _, c := range msg {
		conn.Write([]byte{byte(c)})
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
}
