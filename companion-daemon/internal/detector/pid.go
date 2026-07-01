package detector

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"devremote/companion-daemon/internal/watcher"
)

// FindClaudePIDs returns PIDs of processes writing to .jsonl files in the given directory.
// On Linux/macOS: uses lsof for quick lookup. On Windows: uses process enumeration.
func FindClaudePIDs(jsonlDir string) map[string]int {
	result := make(map[string]int)

	entries, err := os.ReadDir(jsonlDir)
	if err != nil {
		return result
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}

		sessionID := e.Name()[:len(e.Name())-6] // strip .jsonl
		pid := findPIDForFile(filepath.Join(jsonlDir, e.Name()))
		if pid > 0 {
			result[sessionID] = pid
		}
	}

	return result
}

// findPIDForFile finds the PID of the process that has the given file open.
func findPIDForFile(path string) int {
	switch runtime.GOOS {
	case "linux", "darwin":
		return findPIDUnix(path)
	case "windows":
		return findPIDWindows(path)
	default:
		return 0
	}
}

func findPIDUnix(path string) int {
	procs, _ := os.ReadDir("/proc")
	for _, p := range procs {
		if !p.IsDir() {
			continue
		}
		// Check if dir name is a number (PID)
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

// Windows PID discovery: enumerate processes using Toolhelp32 + check handles.
// This is a best-effort approach. For production, sysinternals handle.exe is more reliable.
func findPIDWindows(path string) int {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	createToolhelp32Snapshot := kernel32.NewProc("CreateToolhelp32Snapshot")
	process32First := kernel32.NewProc("Process32FirstW")
	process32Next := kernel32.NewProc("Process32NextW")
	closeHandle := kernel32.NewProc("CloseHandle")

	type processEntry32W struct {
		Size              uint32
		CntUsage          uint32
		ProcessID         uint32
		DefaultHeapID     uintptr
		ModuleID          uint32
		CntThreads        uint32
		ParentProcessID   uint32
		PriClassBase      int32
		Flags             uint32
		ExeFile           [260]uint16
	}

	const TH32CS_SNAPPROCESS = 0x00000002

	snap, _, _ := createToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == 0 || snap == uintptr(syscall.InvalidHandle) {
		return 0
	}
	defer closeHandle.Call(snap)

	var pe processEntry32W
	pe.Size = uint32(unsafe.Sizeof(pe))

	ret, _, _ := process32First.Call(snap, uintptr(unsafe.Pointer(&pe)))
	if ret == 0 {
		return 0
	}

	for ret != 0 {
		pid := int(pe.ProcessID)
		// Check if this process has .jsonl in its command line.
		if pid > 0 && isClaudeProcess(pid) {
			return pid
		}
		ret, _, _ = process32Next.Call(snap, uintptr(unsafe.Pointer(&pe)))
	}

	return 0
}

func isClaudeProcess(pid int) bool {
	// Simple heuristic: check if the process name matches known AI CLI tools.
	// A more robust approach would use NtQuerySystemInformation to check file handles.
	name := processName(pid)
	switch name {
	case "claude", "claude.exe", "codex", "codex.exe",
		"gemini", "node.exe", "node", "python", "python3",
		"aider", "reasonix":
		return true
	}
	return false
}

func processName(pid int) string {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	openProcess := kernel32.NewProc("OpenProcess")
	queryFullProcessImageName := kernel32.NewProc("QueryFullProcessImageNameW")
	closeHandle := kernel32.NewProc("CloseHandle")

	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000

	h, _, _ := openProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer closeHandle.Call(h)

	var buf [1024]uint16
	bufSize := uint32(len(buf))
	ret, _, _ := queryFullProcessImageName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bufSize)))
	if ret == 0 {
		return ""
	}

	name := syscall.UTF16ToString(buf[:bufSize])
	return filepath.Base(name)
}

// PIDToSession maps discovered PIDs to session IDs and updates RawEvent.
func PIDToSession(pids map[string]int, ev *watcher.RawEvent) {
	if pid, ok := pids[ev.SessionID]; ok && pid > 0 {
		ev.PID = pid
	}
}
