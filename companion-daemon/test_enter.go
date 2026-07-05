package main
import (
	"net"
)
func main() {
	conn, _ := net.Dial("unix", "/tmp/pokit.sock")
	conn.Write([]byte("cmd:claude\n\n"))
	conn.Write([]byte("test\r"))
}
