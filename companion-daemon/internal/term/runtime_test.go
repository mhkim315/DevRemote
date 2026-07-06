package term

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

func TestRegistryFromContextMissingReturnsError(t *testing.T) {
	t.Parallel()

	registry, err := RegistryFromContext(context.Background())
	if registry != nil {
		t.Fatalf("registry = %v, want nil", registry)
	}
	if !errors.Is(err, ErrRegistryMissing) {
		t.Fatalf("error = %v, want ErrRegistryMissing", err)
	}
}

func TestRequireRegistryReturns500WhenMissing(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/term/ws", nil)
	response := httptest.NewRecorder()

	registry, ok := requireRegistry(response, request)
	if ok || registry != nil {
		t.Fatalf("requireRegistry = (%v, %v), want (nil, false)", registry, ok)
	}
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestInjectRegistryKeepsHandlersIndependent(t *testing.T) {
	t.Parallel()

	registryA := mux.NewRegistry()
	registryB := mux.NewRegistry()

	handler := func(w http.ResponseWriter, r *http.Request) {
		registry, err := RegistryFromContext(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		switch registry {
		case registryA:
			w.WriteHeader(http.StatusCreated)
		case registryB:
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "unexpected registry", http.StatusInternalServerError)
		}
	}

	tests := []struct {
		name       string
		registry   *mux.Registry
		wantStatus int
	}{
		{name: "registry A", registry: registryA, wantStatus: http.StatusCreated},
		{name: "registry B", registry: registryB, wantStatus: http.StatusAccepted},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			response := httptest.NewRecorder()

			InjectRegistry(tt.registry, handler).ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
		})
	}
}
