//go:build !windows

package detector

import (
	"fmt"
	"os"
	"path/filepath"
)

func findPIDForFile(path string) int {
	procs, _ := os.ReadDir("/proc")
	for _, p := range procs {
		if !p.IsDir() {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(p.Name(), "%d", &pid); err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", p.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err == nil && link == path {
				return pid
			}
		}
	}
	return 0
}
