package v3_0_0

import (
	"strconv"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

func TestConformance(t *testing.T) {
	fx := contract.ConformanceFixtures{
		DetectContext:    contract.SessionContext{SessionID: "s", ProcessName: "claude", CWD: "/tmp/.claude"},
		ExpectDetectKind: "claude",
		ValidRecords: []contract.RawRecord{
			{Bytes: []byte(`{"id":"e1","kind":"started","ts":1}`), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
			{Bytes: []byte(`{"id":"e2","kind":"message","ts":2}`), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		},
		ExpectTypes: []contract.AgentEventType{agent.EventAgentStarted, agent.EventAssistantMessage},
		DistinctRecord: func(i int) contract.RawRecord {
			return contract.RawRecord{
				Bytes:      []byte(`{"id":"d` + strconv.Itoa(i) + `","kind":"message","ts":` + strconv.Itoa(i) + `}`),
				Source:     agent.SourceJSONL,
				Provenance: contract.ProvenanceNativeLog,
			}
		},
		SizedRecord: func(size int) contract.RawRecord {
			if size < 50 {
				size = 50
			}
			// Build valid JSON of exactly `size` bytes by padding a text field.
			// Template: {"id":"sz","kind":"message","ts":0,"pad":"<N x chars>"}
			// Overhead (everything except the pad chars): 42 bytes
			prefix := `{"id":"sz","kind":"message","ts":0,"pad":"`
			suffix := `"}`
			overhead := len(prefix) + len(suffix)
			padLen := size - overhead
			if padLen < 0 {
				padLen = 0
			}
			b := make([]byte, 0, size)
			b = append(b, prefix...)
			for i := 0; i < padLen; i++ {
				b = append(b, 'x')
			}
			b = append(b, suffix...)
			return contract.RawRecord{Bytes: b, Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog}
		},
		FailingFactory: func(t *testing.T) contract.AgentAdapter { return NewFailingAdapter() },
		ApprovalRecords: []contract.RawRecord{
			{Bytes: []byte(`{"id":"a1","kind":"approval","ts":5}`), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog},
		},
	}
	contract.RunAgentContract(t, "claude-v3", func(t *testing.T) contract.AgentAdapter { return &Adapter{} }, fx)
}
