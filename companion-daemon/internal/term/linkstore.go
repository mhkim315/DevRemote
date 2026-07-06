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
	if fi, err := os.Stat(dir); err == nil && fi.Mode().Perm() != 0700 {
		_ = os.Chmod(dir, 0700)
	}
	// Correct existing file permissions to 0600.
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() != 0600 {
		if err := os.Chmod(path, 0600); err != nil {
			return nil, fmt.Errorf("linkstore: chmod %s: %w", path, err)
		}
	}
	return &fileLinkStore{
		links: make(map[string]SessionLink),
		path:  path,
	}, nil
}

func (s *fileLinkStore) Path() string { return s.path }

type fileLinkStore struct {
	mu    sync.RWMutex
	links map[string]SessionLink
	path  string
}

func (s *fileLinkStore) Load(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

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

	// Normalise: migrate legacy IDs to canonical and deduplicate.
	normalised := make(map[string]SessionLink, len(links))
	for _, l := range links {
		canonicalID := mux.MigrateLegacyID(l.SessionID)
		l.SessionID = canonicalID
		// Last-write-wins for duplicate canonical IDs.
		normalised[canonicalID] = l
	}
	s.links = normalised

	// If migration changed any IDs, persist the normalised state.
	for _, l := range links {
		if mux.MigrateLegacyID(l.SessionID) != l.SessionID {
			if err := s.saveLocked(s.links); err != nil {
				return fmt.Errorf("linkstore: migrate save: %w", err)
			}
			break
		}
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
	if err := ctx.Err(); err != nil {
		return err
	}

	link.SessionID = mux.MigrateLegacyID(link.SessionID)

	s.mu.Lock()
	// Clone map, apply change, save; on success replace memory.
	cloned := cloneMap(s.links)
	cloned[link.SessionID] = link
	if err := s.saveLocked(cloned); err != nil {
		s.mu.Unlock()
		return err
	}
	s.links = cloned
	s.mu.Unlock()
	return nil
}

func (s *fileLinkStore) Delete(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	sessionID = mux.MigrateLegacyID(sessionID)

	s.mu.Lock()
	cloned := cloneMap(s.links)
	delete(cloned, sessionID)
	if err := s.saveLocked(cloned); err != nil {
		s.mu.Unlock()
		return err
	}
	s.links = cloned
	s.mu.Unlock()
	return nil
}

func cloneMap(src map[string]SessionLink) map[string]SessionLink {
	dst := make(map[string]SessionLink, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (s *fileLinkStore) saveLocked(links map[string]SessionLink) error {
	list := make([]SessionLink, 0, len(links))
	for _, l := range links {
		list = append(list, l)
	}

	data, err := json.MarshalIndent(list, "", "  ")
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
	_ = os.Chmod(s.path, 0600)
	return nil
}

// ── memoryLinkStore ──

type memoryLinkStore struct {
	mu    sync.RWMutex
	links map[string]SessionLink
}

func NewNopLinkStore() LinkStore {
	return &memoryLinkStore{links: make(map[string]SessionLink)}
}

func (s *memoryLinkStore) Load(ctx context.Context) error {
	return ctx.Err()
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

func (s *memoryLinkStore) Put(ctx context.Context, link SessionLink) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	link.SessionID = mux.MigrateLegacyID(link.SessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links[link.SessionID] = link
	return nil
}

func (s *memoryLinkStore) Delete(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sessionID = mux.MigrateLegacyID(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.links, sessionID)
	return nil
}
