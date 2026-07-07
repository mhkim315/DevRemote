package agent

import "testing"

func TestCodexParser_Contract(t *testing.T) {
	RunParserContract(t, "codex", func(t *testing.T) AgentParser {
		return NewCodexParser()
	})
}

func TestCodexDetector_Contract(t *testing.T) {
	RunDetectorContract(t, "codex",
		func(t *testing.T) AgentDetector { return NewCodexDetector() },
		func(t *testing.T) LogResolver { return NewCodexLogResolver() },
	)
}
