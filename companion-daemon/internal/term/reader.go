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

	// We read line by line. We keep track of how many bytes we've consumed for the current line
	// so that if we hit EOF without \n, we can just return and let the next poll read from cursor.Offset.

	var currentLine []byte
	var currentLineLen int64

	for len(events) < maxEvents {
		chunk, err := reader.ReadSlice('\n')

		// Accumulate bytes for the current line size
		currentLineLen += int64(len(chunk))

		if !cursor.Discarding {
			currentLine = append(currentLine, chunk...)
			if int64(len(currentLine)) > maxRecordSize {
				log.Printf("Warning: skipped oversized record (size > %d) at offset %d", maxRecordSize, cursor.Offset)
				cursor.Discarding = true
				currentLine = nil // Free memory
			}
		}

		if err == bufio.ErrBufferFull {
			// Line is longer than buffer, continue reading next chunk of the same line
			continue
		}

		if err == io.EOF {
			if len(chunk) > 0 && chunk[len(chunk)-1] != '\n' {
				// Partial write at end of file. Do not increment cursor offset for this line!
				// We'll read it again next time.
				break
			}
			// Exact EOF after a newline, or empty EOF
			if len(chunk) > 0 {
				cursor.Offset += currentLineLen
			}
			break
		}

		if err != nil {
			return events, fmt.Errorf("read error: %w", err)
		}

		// We reached a newline
		cursor.Offset += currentLineLen
		currentLineLen = 0

		if cursor.Discarding {
			cursor.Discarding = false
			continue // Skip processing this discarded line
		}

		// Parse the complete line
		parsedEvents, parseErr := parser.Parse(currentLine)
		currentLine = nil // Reset for next line

		if parseErr != nil {
			// Skip malformed lines gracefully
			continue
		}

		events = append(events, parsedEvents...)
	}

	return events, nil
}
