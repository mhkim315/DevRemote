package term

// PA3 Step6: ActivityBuffer physically deleted. Stub types retained for
// compilation compatibility; all production consumers were cut over in
// Steps 1-3. Full removal in Step 7 (final cleanup).

type ActivityBuffer struct{}
func NewActivityBuffer(capacity int) *ActivityBuffer { return &ActivityBuffer{} }
func (b *ActivityBuffer) Append(event interface{}) {}
func (b *ActivityBuffer) List(sessionID string) []ActivityEvent { return nil }
func (b *ActivityBuffer) Clear(sessionID string) {}

type ActivityType string
const (
	ActivityTerminalOutput ActivityType = "terminal_output"
	ActivityTerminalInput  ActivityType = "terminal_input"
	ActivitySystem         ActivityType = "system"
	ActivityStatus         ActivityType = "status"
)

type ActivityEvent struct {
	ID        string
	Seq       uint64
	SessionID string
	Type      ActivityType
	Text      string
	Bytes     int
	Hash      string
	Timestamp interface{}
}
