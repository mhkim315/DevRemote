package cockpit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCockpitHandlerReturnsEmptyJSON(t *testing.T) {
	s := NewCockpitStore()
	m := http.NewServeMux()
	RegisterCockpitHandler(m, s)
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/cockpit", nil))
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Code)
	}
	var state CockpitState
	if json.Unmarshal(rr.Body.Bytes(), &state) != nil || len(state.Sessions) != 0 {
		t.Fatal("invalid empty state")
	}
}
