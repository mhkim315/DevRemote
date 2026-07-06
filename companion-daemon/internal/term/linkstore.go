package term

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"devremote/companion-daemon/internal/mux"
)

// LinkStore is the interface for session link persistence.
// Tests can inject a fake implementation.
type LinkStore interface {
	// Load reads all links from disk into memory.
	Load(ctx context.Context) error
	// Get returns a single link by session ID.
	Get(sessionID string) (SessionLink, bool)
	// List returns all links.
	List() []SessionLink
	// Put adds or updates a link and persists to disk.
	Put(ctx context.Context, link SessionLink) error
	// Delete removes a link and persists to disk.
	Delete(ctx context.Context, sessionID string) error
}

// memoryLinkStore is an in-memory LinkStore for tests.
type memoryLinkStore struct {
	mu    sync.RWMutex
	links map[string]SessionLink
}

func (s *memoryLinkStore) Load(_ context.Context) error {
	return nil
}

func (s *memoryLinkStore) Get(sessionID string) (SessionLink, bool) {
	sessionID = mux.MigrateLegacyID(sessionID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.links[sessionID]
	return l, ok
}

func (s *memoryLinkStore) List() []SessionLink {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var res []SessionLink
	for _, l := range s.links {
		res = append(res, l)
	}
	return res
}

func (s *memoryLinkStore) Put(_ context.Context, link SessionLink) error {
	link.SessionID = mux.MigrateLegacyID(link.SessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links[link.SessionID] = link
	return nil
}

func (s *memoryLinkStore) Delete(_ context.Context, sessionID string) error {
	sessionID = mux.MigrateLegacyID(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.links, sessionID)
	return nil
}

// NewNopLinkStore returns a LinkStore that stores links in memory.
// Useful for tests where no persistent storage is needed.
func NewNopLinkStore() LinkStore {
	return &memoryLinkStore{links: make(map[string]SessionLink)}
}

// NewFileLinkStore creates a LinkStore backed by ~/.devremote/links.json.
// Uses atomic temp-file + rename, directory 0700, file 0600.
func NewFileLinkStore() (LinkStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("linkstore: home dir: %w", err)
	}
	dir := filepath.Join(home, ".devremote")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("linkstore: mkdir %s: %w", dir, err)
	}
	return &fileLinkStore{
		links: make(map[string]SessionLink),
		path:  filepath.Join(dir, "links.json"),
	}, nil
}

// Path returns the on-disk path (for testing).
func (s *fileLinkStore) Path() string { return s.path }

type fileLinkStore struct {
	mu    sync.RWMutex
	links map[string]SessionLink
	path  string
}

func (s *fileLinkStore) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var links []SessionLink
	if err := json.Unmarshal(data, &links); err != nil {
		return err
	}
	s.links = make(map[string]SessionLink, len(links))
	for _, l := range links {
		s.links[l.SessionID] = l
	}
	return nil
}

func (s *fileLinkStore) Get(sessionID string) (SessionLink, bool) {
	sessionID = mux.MigrateLegacyID(sessionID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.links[sessionID]
	return l, ok
}

func (s *fileLinkStore) List() []SessionLink {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var res []SessionLink
	for _, l := range s.links {
		res = append(res, l)
	}
	return res
}

func (s *fileLinkStore) Put(ctx context.Context, link SessionLink) error {
	link.SessionID = mux.MigrateLegacyID(link.SessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links[link.SessionID] = link
	return s.saveLocked()
}

func (s *fileLinkStore) Delete(ctx context.Context, sessionID string) error {
	sessionID = mux.MigrateLegacyID(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.links, sessionID)
	return s.saveLocked()
}

func (s *fileLinkStore) saveLocked() error {
	var links []SessionLink
	for _, l := range s.links {
		links = append(links, l)
	}
	data, err := json.MarshalIndent(links, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	f.Close()
	return os.Rename(tmpPath, s.path)
}

// ── Staleness helpers ──

// checkStale marks a link as stale if its session title has changed.
func checkStale(link *SessionLink, reg *mux.Registry) {
	sess, err := reg.FindSession(context.Background(), link.SessionID)
	if err == nil && sess.Title() != link.SessionTitle {
		link.Stale = true
	}
}
