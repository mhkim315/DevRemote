package term

// LogCursor manages incremental reading of large JSONL files
type LogCursor struct {
	Path       string
	Offset     int64
	Inode      uint64
	Discarding bool
}
