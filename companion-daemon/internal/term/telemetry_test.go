package term

import (
	"errors"
	"testing"
	"time"

	"devremote/companion-daemon/internal/models"
)

func TestEvaluateState(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name               string
		initialState       string
		initialLoad        int
		lastActivity       time.Time
		parsedNewEvents    bool
		lastEvent          models.AgentEvent
		logErr             error
		isWaiting          bool
		isThinkingFallback bool
		diffSize           int
		expectedState      string
		expectedLoad       int
	}{
		{
			name:          "Waiting state takes precedence",
			initialState:  "idle",
			logErr:        nil,
			isWaiting:     true,
			expectedState: "waiting",
			expectedLoad:  0,
		},
		{
			name:            "Parsed tool_use -> working",
			initialState:    "idle",
			parsedNewEvents: true,
			lastEvent:       models.AgentEvent{Type: "tool_use"},
			logErr:          nil,
			expectedState:   "working",
			expectedLoad:    100,
		},
		{
			name:            "Parsed user -> thinking",
			initialState:    "idle",
			parsedNewEvents: true,
			lastEvent:       models.AgentEvent{Type: "user"},
			logErr:          nil,
			expectedState:   "thinking",
			expectedLoad:    50,
		},
		{
			name:            "Parsed tool_result -> thinking",
			initialState:    "idle",
			parsedNewEvents: true,
			lastEvent:       models.AgentEvent{Type: "tool_result"},
			logErr:          nil,
			expectedState:   "thinking",
			expectedLoad:    50,
		},
		{
			name:            "Parsed message Thinking -> thinking",
			initialState:    "idle",
			parsedNewEvents: true,
			lastEvent:       models.AgentEvent{Type: "message", Summary: "Thinking about it"},
			logErr:          nil,
			expectedState:   "thinking",
			expectedLoad:    50,
		},
		{
			name:            "Parsed message normal -> idle",
			initialState:    "working",
			parsedNewEvents: true,
			lastEvent:       models.AgentEvent{Type: "message", Summary: "Claude"},
			logErr:          nil,
			expectedState:   "idle",
			expectedLoad:    0,
		},
		{
			name:          "JSONL log found, no events, prompt disappeared -> idle",
			initialState:  "waiting",
			logErr:        nil,
			isWaiting:     false,
			lastActivity:  now,
			expectedState: "idle",
			expectedLoad:  0,
		},
		{
			name:          "JSONL log found, working timeout -> thinking",
			initialState:  "working",
			logErr:        nil,
			isWaiting:     false,
			lastActivity:  now.Add(-15 * time.Second),
			expectedState: "thinking",
			expectedLoad:  50,
		},
		{
			name:          "JSONL log found, thinking timeout -> idle",
			initialState:  "thinking",
			logErr:        nil,
			isWaiting:     false,
			lastActivity:  now.Add(-15 * time.Second),
			expectedState: "idle",
			expectedLoad:  0,
		},
		{
			name:               "No JSONL log, fallback diff > 50 -> working",
			initialState:       "idle",
			logErr:             errors.New("no log"),
			isThinkingFallback: false,
			diffSize:           100,
			expectedState:      "working",
			expectedLoad:       100,
		},
		{
			name:               "No JSONL log, fallback isThinking -> thinking",
			initialState:       "idle",
			logErr:             errors.New("no log"),
			isThinkingFallback: true,
			diffSize:           0,
			expectedState:      "thinking",
			expectedLoad:       50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateData := &sessionStateData{
				State:        tt.initialState,
				Load:         tt.initialLoad,
				LastActivity: tt.lastActivity,
			}

			evaluateState(stateData, tt.parsedNewEvents, tt.lastEvent, tt.logErr, tt.isWaiting, tt.isThinkingFallback, tt.diffSize)

			if stateData.State != tt.expectedState {
				t.Errorf("expected state %s, got %s", tt.expectedState, stateData.State)
			}
			if stateData.Load != tt.expectedLoad {
				t.Errorf("expected load %d, got %d", tt.expectedLoad, stateData.Load)
			}
		})
	}
}
