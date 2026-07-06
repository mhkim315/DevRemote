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

func TestHandlersKeepsRegistryIndependent(t *testing.T) {
	t.Parallel()

	registryA := mux.NewRegistry()
	registryB := mux.NewRegistry()

	handler := func(h *Handlers) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch h.Registry {
			case registryA:
				w.WriteHeader(http.StatusCreated)
			case registryB:
				w.WriteHeader(http.StatusAccepted)
			default:
				http.Error(w, "unexpected registry", http.StatusInternalServerError)
			}
		}
	}

	tests := []struct {
		name       string
		handlers   *Handlers
		wantStatus int
	}{
		{name: "registry A", handlers: &Handlers{Registry: registryA}, wantStatus: http.StatusCreated},
		{name: "registry B", handlers: &Handlers{Registry: registryB}, wantStatus: http.StatusAccepted},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			response := httptest.NewRecorder()

			handler(tt.handlers).ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
		})
	}
}
