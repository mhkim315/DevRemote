package term

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// ── SP0.5-B: authenticated managed event read + prompt write ──

func getEvents(t *testing.T, h *Handlers, id, query string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/managed-sessions/x/events?"+query, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.HandleManagedSessionEvents(rec, req)
	return rec.Code, rec.Body.String()
}

func postPrompt(t *testing.T, h *Handlers, id, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/managed-sessions/x/prompt", strings.NewReader(body))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.HandleManagedSessionPrompt(rec, req)
	return rec.Code, rec.Body.String()
}

// TestManagedEventsAPI_SnapshotCursorOrderingIdempotent: the read surface
// returns the bounded snapshot + strictly ordered events after the cursor;
// re-reading the same cursor is idempotent; the incremental cursor returns
// only newer events.
func TestManagedEventsAPI_SnapshotCursorOrderingIdempotent(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-B1", reply: "hello from codex"}
	managed, _ := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]
	h := &Handlers{Managed: managed}

	if err := managed.SubmitPrompt(id, 1, "hi", "test-device", 0); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitForStatus(t, managed.Registry(), id, ManagedStatusCompleted)

	code, body := getEvents(t, h, id, "epoch=1&cursor=0")
	if code != 200 {
		t.Fatalf("read code=%d body=%s", code, body)
	}
	var r ManagedEventsResponse
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.ContractVersion != managedEventContractVersion || r.Session.ID != id {
		t.Fatalf("response header = %+v", r)
	}
	kinds := []ManagedEventKind{}
	last := uint64(0)
	for _, ev := range r.Events {
		if ev.Seq <= last || ev.SessionID != id || ev.Epoch != 1 {
			t.Fatalf("bad event %+v (last=%d)", ev, last)
		}
		last = ev.Seq
		kinds = append(kinds, ev.Kind)
	}
	want := []ManagedEventKind{ManagedEventWorking, ManagedEventAssistant, ManagedEventCompleted}
	if len(kinds) != 3 || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	if r.NextCursor != last {
		t.Fatalf("nextCursor = %d, want %d", r.NextCursor, last)
	}

	// Duplicate cursor read is idempotent (byte-identical modulo nothing new).
	_, body2 := getEvents(t, h, id, "epoch=1&cursor=0")
	if body2 != body {
		t.Fatalf("replayed cursor read differs:\n%s\n%s", body, body2)
	}
	// Incremental read from nextCursor returns nothing new.
	code3, body3 := getEvents(t, h, id, "epoch=1&cursor="+utoa(r.NextCursor))
	if code3 != 200 {
		t.Fatalf("incremental code=%d", code3)
	}
	var r3 ManagedEventsResponse
	_ = json.Unmarshal([]byte(body3), &r3)
	if len(r3.Events) != 0 || r3.NextCursor != r.NextCursor {
		t.Fatalf("incremental read = %+v", r3)
	}
}

func utoa(v uint64) string { return strings.TrimSpace(strings.Join([]string{fmtUint(v)}, "")) }

func fmtUint(v uint64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestManagedEventsAPI_FailClosedBindings: wrong session, wrong/missing
// epoch, malformed cursor, cursor ahead of newest, and disabled service all
// fail closed.
func TestManagedEventsAPI_FailClosedBindings(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-B2"}
	managed, _ := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]
	h := &Handlers{Managed: managed}

	if code, _ := getEvents(t, h, "codex_app_server:nope", "epoch=1&cursor=0"); code != 404 {
		t.Fatalf("wrong session code = %d", code)
	}
	if code, _ := getEvents(t, h, id, "epoch=2&cursor=0"); code != 409 {
		t.Fatalf("stale epoch code = %d", code)
	}
	if code, _ := getEvents(t, h, id, "cursor=0"); code != 400 {
		t.Fatalf("missing epoch code = %d", code)
	}
	if code, _ := getEvents(t, h, id, "epoch=1&cursor=abc"); code != 400 {
		t.Fatalf("malformed cursor code = %d", code)
	}
	if code, _ := getEvents(t, h, id, "epoch=1&cursor=999"); code != 409 {
		t.Fatalf("cursor-ahead code = %d", code)
	}
	hOff := &Handlers{}
	if code, _ := getEvents(t, hOff, id, "epoch=1&cursor=0"); code != 404 {
		t.Fatalf("disabled service code = %d", code)
	}
}

// TestManagedEventsAPI_OverflowGapAndTextBound: ring overflow yields an
// explicit gap for an old cursor (never a silently complete history), and
// assistant text is byte-bounded.
func TestManagedEventsAPI_OverflowGapAndTextBound(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-B3"}
	managed, _ := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]
	store, _, ok := managed.eventStoreFor(id)
	if !ok {
		t.Fatal("no store")
	}

	store.append(ManagedEventAssistant, strings.Repeat("x", managedEventTextMax+500))
	for i := 0; i < managedEventRingCap+50; i++ {
		store.append(ManagedEventWorking, "")
	}

	events, err := store.readAfter(0, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if events[0].Kind != ManagedEventGap {
		t.Fatalf("first event = %+v, want gap", events[0])
	}
	// The oversized text was truncated at append time (check the first event
	// is long gone but verify the bound through a fresh append).
	store.append(ManagedEventAssistant, strings.Repeat("y", managedEventTextMax+1))
	tail, _ := store.readAfter(store.newest()-1, 0)
	if len(tail) != 1 || len(tail[0].Text) != managedEventTextMax {
		t.Fatalf("text bound: got %d bytes", len(tail[0].Text))
	}

	// Multi-byte truncation preserves UTF-8 boundaries: a 4-byte emoji
	// straddling the cut is dropped whole, never split.
	emoji := strings.Repeat("😀", managedEventTextMax/4+10) // > 4096 bytes
	store.append(ManagedEventAssistant, emoji)
	tail2, _ := store.readAfter(store.newest()-1, 0)
	got := tail2[0].Text
	if len(got) > managedEventTextMax {
		t.Fatalf("multibyte bound exceeded: %d bytes", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("multibyte truncation split a UTF-8 sequence")
	}
	if len(got) != managedEventTextMax {
		// 4096 % 4 == 0, so the exact bound is reachable without a split.
		t.Fatalf("multibyte bound = %d, want exactly %d", len(got), managedEventTextMax)
	}
}

// TestManagedPromptAPI_FailClosed: the write surface enforces exact epoch
// binding, one-active-turn, closed field set, and body bounds — every
// rejection has zero provider writes beyond the accepted ones.
func TestManagedPromptAPI_FailClosed(t *testing.T) {
	gate := make(chan struct{})
	app := &interactiveAppServer{threadID: "thread-B4", reply: "r", hold: gate}
	managed, _ := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]
	h := &Handlers{Managed: managed}

	if code, body := postPrompt(t, h, id, `{"epoch":1,"text":"go"}`); code != 200 {
		t.Fatalf("accepted prompt code=%d body=%s", code, body)
	}
	if code, _ := postPrompt(t, h, id, `{"epoch":1,"text":"conflict"}`); code != 409 {
		t.Fatalf("conflict code = %d", code)
	}
	if code, _ := postPrompt(t, h, id, `{"epoch":9,"text":"stale"}`); code != 409 {
		t.Fatalf("stale epoch code = %d", code)
	}
	if code, _ := postPrompt(t, h, "codex_app_server:nope", `{"epoch":1,"text":"x"}`); code != 404 {
		t.Fatalf("wrong session code = %d", code)
	}
	if code, _ := postPrompt(t, h, id, `{"epoch":1,"text":"x","extra":true}`); code != 400 {
		t.Fatalf("unknown field code = %d", code)
	}
	huge := `{"epoch":1,"text":"` + strings.Repeat("z", managedPromptMaxBytes+2048) + `"}`
	if code, _ := postPrompt(t, h, id, huge); code != 400 {
		t.Fatalf("oversized body code = %d", code)
	}
	close(gate)
	deadline := time.Now().Add(2 * time.Second)
	for app.turnCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if app.turnCount() != 1 {
		t.Fatalf("provider writes = %d, want exactly 1", app.turnCount())
	}
}

// TestManagedEventsFixture_MatchesMobileDecoderInput: the exact JSON the
// backend marshals for the read surface equals the committed fixture the real
// mobile decoder test consumes — one contract, two verified ends.
func TestManagedEventsFixture_MatchesMobileDecoderInput(t *testing.T) {
	mk := func(seq uint64, kind ManagedEventKind, text, at string) ManagedEvent {
		return ManagedEvent{
			ContractVersion: managedEventContractVersion,
			SessionID:       "codex_app_server:codex-app-1",
			Epoch:           1, Seq: seq, Kind: kind, Text: text, ObservedAt: at,
		}
	}
	resp := ManagedEventsResponse{
		ContractVersion: managedEventContractVersion,
		Session: ManagedNativeStatusDTO{
			ID: "codex_app_server:codex-app-1", Provider: "codex", Version: "codex-cli 0.144.1",
			NativeStatus: "working", LaunchGen: 1,
			CreatedAt: "2026-07-15T00:00:00Z", StatusChangedAt: "2026-07-15T00:00:05Z", Exited: false,
		},
		Events: []ManagedEvent{
			mk(1, ManagedEventWorking, "", "2026-07-15T00:00:05Z"),
			mk(2, ManagedEventAssistant, "READY", "2026-07-15T00:00:06Z"),
		},
		NextCursor: 2,
	}
	got, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	fixture, err := os.ReadFile("../../../mobile/__tests__/fixtures/managedEvents.fixture.json")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if !bytes.Equal(got, bytes.TrimSpace(fixture)) {
		t.Fatalf("fixture drift:\n backend: %s\n fixture: %s", got, bytes.TrimSpace(fixture))
	}
}
