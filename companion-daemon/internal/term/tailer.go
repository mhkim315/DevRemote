package term

import (
	"bufio"
	"io"
	"os"
	"time"
)

// StartTailing acts like tail -F, continuously reading newly appended lines
// from the given filePath and sending them to lineCh.
// It is robust against the file not existing initially, truncation, and rotation.
func StartTailing(filePath string, lineCh chan<- string, stopCh <-chan struct{}) error {
	go func() {
		var file *os.File
		var reader *bufio.Reader
		isFirstOpen := true

		defer func() {
			if file != nil {
				file.Close()
			}
		}()

		for {
			// Wait for the file to exist and open it
			for file == nil {
				select {
				case <-stopCh:
					return
				default:
				}

				f, err := os.Open(filePath)
				if err != nil {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				file = f

				if isFirstOpen {
					// Start tailing from the end of the file on first open
					file.Seek(0, io.SeekEnd)
					isFirstOpen = false
				} else {
					// On file rotation/re-creation, start from the beginning
					file.Seek(0, io.SeekStart)
				}
				reader = bufio.NewReader(file)
			}

			// Read newly appended lines continuously
			for {
				select {
				case <-stopCh:
					return
				default:
				}

				line, err := reader.ReadString('\n')
				if err != nil {
					if err == io.EOF {
						// Check if file was truncated or rotated
						stat, statErr := os.Stat(filePath)
						if statErr != nil {
							// File might be deleted, close and wait for it to be recreated
							file.Close()
							file = nil
							break // Breaks inner loop, goes back to open loop
						}

						currentOffset, _ := file.Seek(0, io.SeekCurrent)
						if stat.Size() < currentOffset {
							// File truncated, seek to start
							file.Seek(0, io.SeekStart)
							reader.Reset(file)
							continue
						}

						// Check if file was rotated (same path, different file)
						fileStat, _ := file.Stat()
						if !os.SameFile(stat, fileStat) {
							file.Close()
							file = nil
							break // Breaks inner loop, goes back to open loop
						}

						// No new data, sleep and poll again
						time.Sleep(100 * time.Millisecond)
						continue
					}

					// Read error, wait a bit and retry
					time.Sleep(100 * time.Millisecond)
					continue
				}

				// Trim trailing newline characters
				if len(line) > 0 && line[len(line)-1] == '\n' {
					line = line[:len(line)-1]
				}
				if len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}

				// Send to channel, but also allow cancellation
				select {
				case <-stopCh:
					return
				case lineCh <- line:
				}
			}
		}
	}()

	return nil
}
