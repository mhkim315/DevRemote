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
	Load(ctx context.Context) error
	Get(sessionID string) (SessionLink, bool)
	List() []SessionLink
	Put(ctx context.Context, link SessionLink) error
	Delete(ctx context.Context, sessionID string) error
}

// ── fileLinkStore ──

// NewFileLinkStore creates a LinkStore backed by ~/.devremote/links.json.
func NewFileLinkStore() (LinkStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("linkstore: home dir: %w", err)
	}
	return NewFileLinkStoreAt(filepath.Join(home, ".devremote", "links.json"))
}

// NewFileLinkStoreAt creates a LinkStore at an explicit path (for testing).
func NewFileLinkStoreAt(path string) (LinkStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("linkstore: mkdir %s: %w", dir, err)
	}
	// Ensure directory has correct permissions even if it already existed.
	if fi, err := os.Stat(dir); err == nil && fi.Mode().Perm() != 0700 {
		_ = os.Chmod(dir, 0700)
	}
	return &fileLinkStore{
		links: make(map[string]SessionLink),
		path:  path,
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
	// Save to disk first; if it fails, memory is not changed.
	if err := s.saveLocked(link); err != nil {
		s.mu.Unlock()
		return err
	}
	s.links[link.SessionID] = link
	s.mu.Unlock()
	return nil
}

func (s *fileLinkStore) Delete(ctx context.Context, sessionID string) error {
	sessionID = mux.MigrateLegacyID(sessionID)

	s.mu.Lock()
	// Save to disk first; if it fails, memory is not changed.
	if err := s.saveLockedDeleting(sessionID); err != nil {
		s.mu.Unlock()
		return err
	}
	delete(s.links, sessionID)
	s.mu.Unlock()
	return nil
}

// saveLocked writes the current links + the new/updated link to disk atomically.
// Must be called with s.mu held (write lock).
func (s *fileLinkStore) saveLocked(newLink SessionLink) error {
	links := make([]SessionLink, 0, len(s.links)+1)
	found := false
	for _, l := range s.links {
		if l.SessionID == newLink.SessionID {
			links = append(links, newLink)
			found = true
		} else {
			links = append(links, l)
		}
	}
	if !found {
		links = append(links, newLink)
	}

	// Clean up legacy ID entries.
	links = deduplicateAndCleanLegacy(links)

	return s.writeAtomically(links)
}

// saveLockedDeleting writes the current links minus the deleted session to disk.
// Must be called with s.mu held (write lock).
func (s *fileLinkStore) saveLockedDeleting(sessionID string) error {
	var links []SessionLink
	for _, l := range s.links {
		if l.SessionID != sessionID {
			links = append(links, l)
		}
	}
	return s.writeAtomically(links)
}

func (s *fileLinkStore) writeAtomically(links []SessionLink) error {
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

	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	// Ensure final file has correct permissions.
	_ = os.Chmod(s.path, 0600)
	return nil
}

// deduplicateAndCleanLegacy removes entries keyed by legacy IDs when a
// canonical ID entry exists for the same adapter:session pair.
func deduplicateAndCleanLegacy(links []SessionLink) []SessionLink {
	canonical := make(map[string]bool)
	for _, l := range links {
		canonical[l.SessionID] = true
	}

	var out []SessionLink
	seen := make(map[string]bool)
	for _, l := range links {
		// If this is a legacy cmux:NN key and we have cmux:surface:NN, drop it.
		canonicalID := mux.MigrateLegacyID(l.SessionID)
		if canonicalID != l.SessionID && canonical[canonicalID] {
			continue
		}
		if !seen[l.SessionID] {
			seen[l.SessionID] = true
			out = append(out, l)
		}
	}
	return out
}

// ── memoryLinkStore (test / nop) ──

type memoryLinkStore struct {
	mu    sync.RWMutex
	links map[string]SessionLink
}

func newMemoryLinkStoreAt() *memoryLinkStore {
	return &memoryLinkStore{links: make(map[string]SessionLink)}
}

func NewNopLinkStore() LinkStore {
	return newMemoryLinkStoreAt()
}

func (s *memoryLinkStore) Load(_ context.Context) error { return nil }

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
