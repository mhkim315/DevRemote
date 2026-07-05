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
	
	// Start with 0x0, wait 1 second
	time.Sleep(1 * time.Second)
	
	// Resize to 80x24
	pty.Setsize(ptm, &pty.Winsize{Rows: 24, Cols: 80})
	
	go io.Copy(os.Stdout, ptm)
	
	time.Sleep(3 * time.Second)
	
	// Send "h"
	ptm.Write([]byte("h"))
	time.Sleep(100 * time.Millisecond)
	ptm.Write([]byte("e"))
	
	time.Sleep(5 * time.Second)
	cmd.Process.Kill()
}
