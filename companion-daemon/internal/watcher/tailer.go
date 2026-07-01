package watcher

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// RawEvent is a single line from the JSONL log.
type RawEvent struct {
	Type       string          `json:"type"`
	ParentUUID string          `json:"parentUuid"`
	Message    json.RawMessage `json:"message"`
	Timestamp  string          `json:"timestamp"`
	SessionID  string          `json:"sessionId"`
	PID        int             `json:"-"` // populated by detector.FindClaudePIDs
}

// ToolUse extracted from an assistant message content block.
type ToolUse struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

type assistantMsg struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   json.RawMessage `json:"usage"`
}

type contentBlock struct {
	Type  string                 `json:"type"`
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
	Text  string                 `json:"text"`
}

// Callback receives every JSONL line parsed into a RawEvent.
type Callback func(RawEvent)

// Tailer watches a directory for new/modified .jsonl files and tails them.
type Tailer struct {
	dir      string
	callback Callback
	watcher  *fsnotify.Watcher
	files    map[string]*os.File
	mu       sync.Mutex
}

// New creates a Tailer for the given project directory.
func New(dir string, cb Callback) (*Tailer, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Tailer{
		dir:      dir,
		callback: cb,
		watcher:  w,
		files:    make(map[string]*os.File),
	}, nil
}

func (t *Tailer) tailFile(path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.files[path]; ok {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	t.files[path] = f

	go func() {
		reader := bufio.NewReader(f)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err.Error() == "EOF" {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var ev RawEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				log.Printf("watcher: skip unparseable line: %v", err)
				continue
			}
			t.callback(ev)
		}
	}()
	return nil
}

// Start begins watching the directory and tailing existing/new .jsonl files.
func (t *Tailer) Start() error {
	if err := t.watcher.Add(t.dir); err != nil {
		return err
	}

	entries, _ := os.ReadDir(t.dir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			t.tailFile(filepath.Join(t.dir, e.Name()))
		}
	}

	go func() {
		for event := range t.watcher.Events {
			if !strings.HasSuffix(event.Name, ".jsonl") {
				continue
			}
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
				t.tailFile(event.Name)
			}
		}
	}()

	go func() {
		for err := range t.watcher.Errors {
			log.Printf("watcher error: %v", err)
		}
	}()

	return nil
}

// Close stops the tailer.
func (t *Tailer) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, f := range t.files {
		f.Close()
	}
	return t.watcher.Close()
}

// ExtractToolUse tries to get a ToolUse from a RawEvent's message field.
func ExtractToolUse(ev RawEvent) *ToolUse {
	if ev.Type != "assistant" {
		return nil
	}

	var msg assistantMsg
	if err := json.Unmarshal(ev.Message, &msg); err != nil {
		return nil
	}

	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil
	}

	for _, b := range blocks {
		if b.Type == "tool_use" && b.ID != "" {
			return &ToolUse{
				ID:    b.ID,
				Name:  b.Name,
				Input: b.Input,
			}
		}
	}
	return nil
}

// IsToolResult checks if this user event contains a matching tool_result.
func IsToolResult(ev RawEvent, toolUseID string) bool {
	if ev.Type != "user" {
		return false
	}
	if ev.ParentUUID != toolUseID {
		return false
	}
	return true
}
