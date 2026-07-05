package main

import (
	"os/exec"
	"github.com/creack/pty"
	"io"
	"os"
	"time"
)

func main() {
	cmd := exec.Command("sh", "-c", "claude")
	ptm, _ := pty.Start(cmd)
	pty.Setsize(ptm, &pty.Winsize{Rows: 24, Cols: 80})
	go io.Copy(os.Stdout, ptm)
	
	time.Sleep(5 * time.Second)
	cmd.Process.Kill()
}
