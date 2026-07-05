package main

import (
	"os/exec"
	"github.com/creack/pty"
	"io"
	"os"
)

func main() {
	cmd := exec.Command("sh", "-c", "node -e 'console.log(process.stdin.isTTY)'")
	ptm, _ := pty.Start(cmd)
	go io.Copy(os.Stdout, ptm)
	cmd.Wait()
}
