package term

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"

	"devremote/companion-daemon/internal/models"
)

const maxRecordSize = 5 * 1024 * 1024 // 5MB limit for single jsonl line

// ReadNewEvents reads from the cursor, handling file rotation/truncation
// and skips oversized records gracefully.
func ReadNewEvents(cursor *LogCursor, parser AgentLogParser, maxEvents int) ([]models.AgentEvent, error) {
	file, err := os.Open(cursor.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat log file: %w", err)
	}

	sysStat, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("failed to get underlying stat")
	}
	inode := sysStat.Ino

	// Detect rotation or truncation
	if cursor.Inode != inode || stat.Size() < cursor.Offset {
		cursor.Offset = 0
		cursor.Inode = inode
		cursor.Discarding = false
	}

	_, err = file.Seek(cursor.Offset, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("failed to seek: %w", err)
	}

	reader := bufio.NewReader(file)
	var events []models.AgentEvent

	var currentLine []byte
	var currentLineLen int64

	for len(events) < maxEvents {
		chunk, err := reader.ReadSlice('\n')
		currentLineLen += int64(len(chunk))

		if !cursor.Discarding {
			currentLine = append(currentLine, chunk...)
			if int64(len(currentLine)) > maxRecordSize {
				log.Printf("Warning: skipped oversized record (size > %d) at offset %d", maxRecordSize, cursor.Offset)
				cursor.Discarding = true
				currentLine = nil
			}
		}

		if err == bufio.ErrBufferFull {
			continue
		}

		if err == io.EOF {
			if len(chunk) > 0 && chunk[len(chunk)-1] != '\n' {
				break
			}
			if len(chunk) > 0 {
				cursor.Offset += currentLineLen
			}
			break
		}

		if err != nil {
			return events, fmt.Errorf("read error: %w", err)
		}

		cursor.Offset += currentLineLen
		currentLineLen = 0

		if cursor.Discarding {
			cursor.Discarding = false
			continue
		}

		parsedEvents, parseErr := parser.Parse(currentLine)
		currentLine = nil

		if parseErr != nil {
			continue
		}

		events = append(events, parsedEvents...)
	}

	return events, nil
}

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

	_, err = file.Seek(cursor.Offset, io.SeekStart)
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
