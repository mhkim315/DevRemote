package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/devicetrust"
)

// TestInsecureLocalOnlyProductionCompositionUsesLocalAuthority exercises the
// real application composition (no test mutation authorizer seam). A local
// profile create carries the empty identity/epoch and must reach the actual
// PTY launch boundary in insecure local mode.
func TestInsecureLocalOnlyProductionCompositionUsesLocalAuthority(t *testing.T) {
	dir := t.TempDir()
	identity, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: filepath.Join(dir, "host.json")})
	if err != nil {
		t.Fatalf("host identity: %v", err)
	}
	registry, err := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: filepath.Join(dir, "devices.json")})
	if err != nil {
		t.Fatalf("device registry: %v", err)
	}
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, Dependencies{
		HostIdentity:   identity,
		DeviceRegistry: registry,
	})
	if err != nil {
		t.Fatalf("production app composition: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"profileId": "shell", "name": "local-regression"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	app.handlers.HandleSessionCRUD(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("local create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !strings.HasPrefix(created.ID, "controlled_pty:") {
		t.Fatalf("created id=%q", created.ID)
	}
	if _, err := app.lifecycle.Kill(context.Background(), created.ID, "", 0); err != nil {
		t.Fatalf("local cleanup kill: %v", err)
	}
}
