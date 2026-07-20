package term

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
)

const maxRecordSize = 5 * 1024 * 1024 // 5MB limit for single jsonl line

// RawLinesResult bundles raw lines with a generation-changed signal.
// The caller must check GenerationChanged before using the lines with
// stream-derived adapter state.
type RawLinesResult struct {
	Lines             [][]byte
	GenerationChanged bool // inode change or truncation detected by reader
}

// ReadRawLines reads raw JSONL lines from the cursor position and
// reports whether the stream generation changed (inode or truncation).
func ReadRawLines(cursor *LogCursor, maxLines int) (RawLinesResult, error) {
	file, err := os.Open(cursor.Path)
	if err != nil {
		return RawLinesResult{}, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return RawLinesResult{}, fmt.Errorf("failed to stat log file: %w", err)
	}

	sysStat, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		return RawLinesResult{}, fmt.Errorf("failed to get underlying stat")
	}
	genChanged := cursor.Inode != sysStat.Ino || stat.Size() < cursor.Offset
	cursor.Inode = sysStat.Ino
	if genChanged {
		cursor.Offset = 0
	}

	_, err = file.Seek(cursor.Offset, 0)
	if err != nil {
		return RawLinesResult{}, err
	}

	reader := bufio.NewReader(file)
	var lines [][]byte

	for len(lines) < maxLines {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
			if len(line) > 0 && len(line) < maxRecordSize {
				lines = append(lines, line)
			}
			cursor.Offset += int64(len(line)) + 1
		} else if len(line) > 0 {
			break // partial EOF, retry next poll
		}
		if err != nil {
			break
		}
	}
	return RawLinesResult{Lines: lines, GenerationChanged: genChanged}, nil
}
