package main

import (
	"os/exec"
	"github.com/creack/pty"
	"io"
	"os"
	"time"
)

func main() {
	cmd := exec.Command("bash")
	ptm, _ := pty.Start(cmd)
	go io.Copy(os.Stdout, ptm)
	
	time.Sleep(1 * time.Second)
	ptm.Write([]byte("node -e 'process.stdin.setRawMode(true); process.stdin.on(\"data\", d => console.log(\"RCV\", d));'\n"))
	
	time.Sleep(1 * time.Second)
	ptm.Write([]byte("a"))
	
	time.Sleep(1 * time.Second)
	cmd.Process.Kill()
}
