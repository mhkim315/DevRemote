package agent

import "testing"

func TestClaudeParser_Contract(t *testing.T) {
	RunParserContract(t, "claude", func(t *testing.T) AgentParser {
		return NewClaudeParser()
	})
}

func TestClaudeDetector_Contract(t *testing.T) {
	RunDetectorContract(t, "claude",
		func(t *testing.T) AgentDetector { return NewClaudeDetector() },
		func(t *testing.T) LogResolver { return NewClaudeLogResolver() },
	)
}
