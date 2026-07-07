package term

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

func TestFileLinkStore_PutGetListDelete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "links.json")

	s, err := NewFileLinkStoreAt(path)
	if err != nil {
		t.Fatalf("NewFileLinkStoreAt: %v", err)
	}

	ctx := context.Background()

	// Put a link.
	if err := s.Put(ctx, SessionLink{SessionID: "tmux:test", Provider: "p", ExternalSessionID: "ext"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Get it back.
	link, ok := s.Get("tmux:test")
	if !ok {
		t.Fatal("Get returned false")
	}
	if link.Provider != "p" {
		t.Errorf("Provider = %q", link.Provider)
	}

	// List.
	all := s.List()
	if len(all) != 1 {
		t.Fatalf("List = %d, want 1", len(all))
	}

	// Delete.
	if err := s.Delete(ctx, "tmux:test"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get("tmux:test"); ok {
		t.Error("Get after Delete returned true")
	}
}

func TestFileLinkStore_Persistence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "links.json")

	// Create, put, then re-open.
	s1, _ := NewFileLinkStoreAt(path)
	s1.Put(context.Background(), SessionLink{SessionID: "tmux:s", Provider: "persist"})

	s2, _ := NewFileLinkStoreAt(path)
	s2.Load(context.Background())
	link, ok := s2.Get("tmux:s")
	if !ok || link.Provider != "persist" {
		t.Errorf("link did not persist: ok=%v provider=%q", ok, link.Provider)
	}
}

func TestFileLinkStore_InstanceIsolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s1, _ := NewFileLinkStoreAt(filepath.Join(dir, "a.json"))
	s2, _ := NewFileLinkStoreAt(filepath.Join(dir, "b.json"))

	s1.Put(context.Background(), SessionLink{SessionID: "tmux:a"})
	if _, ok := s2.Get("tmux:a"); ok {
		t.Error("link leaked between instances")
	}
}

func TestFileLinkStore_FilePermissions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "links.json")

	s, _ := NewFileLinkStoreAt(path)
	s.Put(context.Background(), SessionLink{SessionID: "tmux:p"})

	// Check file mode.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Mode().Perm()&0077 != 0 {
		t.Errorf("file perms %o, want no group/other access", fi.Mode().Perm())
	}
}

func TestFileLinkStore_LegacyIDMigration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "links.json")

	s, _ := NewFileLinkStoreAt(path)
	ctx := context.Background()

	// Put a legacy cmux ID.
	if err := s.Put(ctx, SessionLink{SessionID: "cmux:42", Provider: "legacy"}); err != nil {
		t.Fatalf("Put legacy: %v", err)
	}
	// Put the canonical version.
	if err := s.Put(ctx, SessionLink{SessionID: "cmux:surface:42", Provider: "canonical"}); err != nil {
		t.Fatalf("Put canonical: %v", err)
	}

	// Both should resolve to the same canonical entry.
	link, ok := s.Get("cmux:42") // legacy lookup
	if !ok || link.Provider != "canonical" {
		t.Errorf("legacy lookup: ok=%v provider=%q, want canonical", ok, link.Provider)
	}

	// List should not have duplicate.
	all := s.List()
	count := 0
	for _, l := range all {
		if l.Provider == "canonical" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("canonical entry appears %d times in List, want 1", count)
	}
}

func TestFileLinkStore_NilSafe(t *testing.T) {
	// Verify that NewNopLinkStore never panics on nil receiver.
	s := NewNopLinkStore()
	ctx := context.Background()
	s.Load(ctx)
	s.Get("x")
	s.List()
	s.Put(ctx, SessionLink{SessionID: "x"})
	s.Delete(ctx, "x")
}

func TestRegression_FileLinkStore_LoadedLegacyIDIsAccessible(t *testing.T) {
	// Simulate an existing file containing a legacy cmux ID.
	// After Load(), Get() with the legacy ID must work via canonical migration.
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "links.json")

	// Write a file with a legacy cmux ID directly (bypass LinkStore).
	legacyData := `[{"sessionId":"cmux:42","provider":"legacy","externalSessionId":"ext"}]`
	if err := os.WriteFile(path, []byte(legacyData), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, _ := NewFileLinkStoreAt(path)
	if err := s.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Legacy lookup must succeed (migrated to canonical).
	l, ok := s.Get("cmux:42")
	if !ok {
		t.Fatal("Get(cmux:42) returned false after loading legacy file")
	}
	if l.Provider != "legacy" {
		t.Errorf("Provider = %q, want legacy", l.Provider)
	}

	// Canonical lookup must also work.
	l2, ok := s.Get("cmux:surface:42")
	if !ok {
		t.Fatal("Get(cmux:surface:42) returned false after legacy migration")
	}
	if l2.SessionID != "cmux:surface:42" {
		t.Errorf("SessionID = %q, want cmux:surface:42", l2.SessionID)
	}

	// List must not have duplicates.
	all := s.List()
	if len(all) != 1 {
		t.Errorf("List returned %d links after legacy migration, want 1: %+v", len(all), all)
	}
}

func TestFileLinkStore_LoadNormalizesToDisk(t *testing.T) {
	// After loading a legacy file, the normalized state must persist.
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "links.json")

	legacyData := `[{"sessionId":"cmux:42","provider":"legacy","externalSessionId":"ext"}]`
	os.WriteFile(path, []byte(legacyData), 0600)

	s, _ := NewFileLinkStoreAt(path)
	s.Load(context.Background())

	// Put another link to trigger a save.
	s.Put(context.Background(), SessionLink{SessionID: "tmux:x", Provider: "new"})

	// Re-open: must not see legacy key.
	s2, _ := NewFileLinkStoreAt(path)
	s2.Load(context.Background())
	if _, ok := s2.Get("cmux:42"); !ok {
		t.Fatal("legacy lookup failed after re-open")
	}
	all := s2.List()
	if len(all) != 2 {
		t.Errorf("List = %d after re-open, want 2", len(all))
	}
}

func TestFileLinkStore_ExistingFilePermissionCorrected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "links.json")

	// Create a file with insecure permissions.
	if err := os.WriteFile(path, []byte(`[{"sessionId":"x","provider":"p"}]`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Creating the store must correct permissions to 0600.
	_, err := NewFileLinkStoreAt(path)
	if err != nil {
		t.Fatalf("NewFileLinkStoreAt: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("file mode = %o after NewFileLinkStoreAt, want 0600", fi.Mode().Perm())
	}
}

func TestFileLinkStore_DeleteNonExistent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, _ := NewFileLinkStoreAt(filepath.Join(dir, "links.json"))
	// Deleting a non-existent key should not error.
	if err := s.Delete(context.Background(), "nonexistent"); err != nil {
		t.Errorf("Delete non-existent: %v", err)
	}
}

func TestHandleLinksAPI_ClearsCanonicalTelemetryForLegacyID(t *testing.T) {
	// When HandleLinksAPI receives a legacy cmux:42 link/unlink,
	// it must clear telemetry for the canonical cmux:surface:42 key.
	t.Parallel()

	dir := t.TempDir()
	store, _ := NewFileLinkStoreAt(filepath.Join(dir, "links.json"))
	events := NewMemoryEventStore()
	reg := mux.MustNewRegistry()
	telemetry := NewTelemetryService(reg, events, store, nil, nil, nil)

	// Pre-populate telemetry with state for the canonical key.
	telemetry.sessions["cmux:surface:42"] = &sessionStateData{State: "working"}

	h := &Handlers{Registry: reg, Links: store, Events: events, Telemetry: telemetry}

	// POST link with legacy ID.
	body := strings.NewReader(`{"sessionId":"cmux:42","provider":"p","externalSessionId":"ext"}`)
	req := httptest.NewRequest("POST", "/api/v2/links", body)
	rec := httptest.NewRecorder()
	h.HandleLinksAPI(rec, req)

	if rec.Code != 200 {
		t.Fatalf("POST status = %d", rec.Code)
	}
	// Telemetry must be cleared for the canonical key.
	if telemetry.sessions["cmux:surface:42"] != nil {
		t.Error("telemetry state was not cleared for canonical link ID after POST")
	}
}
