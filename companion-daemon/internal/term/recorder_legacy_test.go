package term

import "sync"

// legacyRecorderFixtures preserves the pre-V1 unit-test fixture surface only.
// It is compiled exclusively into the term package's tests; production has no
// recorder lookup map and every runtime path owns a direct reference.
var recorderRegistry = struct {
	mu         sync.Mutex
	recorders  map[string]*Recorder
	terminated map[string]bool
}{recorders: make(map[string]*Recorder), terminated: make(map[string]bool)}

func init() {
	recorderLifecycleObserver = func(r *Recorder, stopped bool) {
		recorderRegistry.mu.Lock()
		defer recorderRegistry.mu.Unlock()
		if stopped {
			if recorderRegistry.recorders[r.sessionID] == r {
				delete(recorderRegistry.recorders, r.sessionID)
			}
			return
		}
		recorderRegistry.recorders[r.sessionID] = r
	}
}

func GetRecorder(id string) *Recorder {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()
	return recorderRegistry.recorders[id]
}

func DeleteRecorder(id string) {
	recorderRegistry.mu.Lock()
	r := recorderRegistry.recorders[id]
	delete(recorderRegistry.recorders, id)
	delete(recorderRegistry.terminated, id)
	recorderRegistry.mu.Unlock()
	if r != nil {
		r.Stop()
	}
}

func (r *Recorder) unregisterSelf() {
	recorderRegistry.mu.Lock()
	if recorderRegistry.recorders[r.sessionID] == r {
		delete(recorderRegistry.recorders, r.sessionID)
	}
	recorderRegistry.mu.Unlock()
}
