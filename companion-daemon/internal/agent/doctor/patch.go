package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PatchOperation is a single immutable parsed patch entry. Validator and
// applier consume the SAME object — no path disagreement is possible.
type PatchOperation struct {
	OldPath  string // "--- a/X" stripped
	NewPath  string // "+++ b/Y" stripped
	DiffPath string // "diff --git a/X b/Y" → b/Y stripped

	OldMode string
	NewMode string

	IsNew    bool
	IsDelete bool
	IsRename bool
	IsBinary bool

	Hunks []PatchHunk
}

type PatchHunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
	Lines              []PatchLine
}

type PatchLine struct {
	Kind    byte   // ' ' context, '+' add, '-' remove
	Content string
}

var (
	ErrEmptyPatch     = errors.New("empty patch rejected")
	ErrMalformedPatch = errors.New("malformed patch — no valid diff headers")
	ErrPathMismatch   = errors.New("patch path mismatch — diff/---/+++ paths differ")
	ErrPatchTooLarge  = errors.New("patch exceeds MaxPatchBytes")
	ErrTooManyFiles   = errors.New("patch touches too many files")
	ErrBinaryPatch    = errors.New("binary patch rejected")
	ErrDeleteRejected = errors.New("delete rejected")
	ErrRenameRejected = errors.New("rename rejected")
	ErrExecutableMode = errors.New("executable mode change rejected")
)

const MaxPatchBytes = 1 << 20
const MaxChangedFiles = 32

// ParsePatch parses a unified diff into immutable PatchOperations.
func ParsePatch(patch []byte) ([]PatchOperation, error) {
	if len(patch) == 0 {
		return nil, ErrEmptyPatch
	}
	if len(patch) > MaxPatchBytes {
		return nil, ErrPatchTooLarge
	}
	text := string(patch)
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyPatch
	}

	lines := strings.Split(text, "\n")
	var ops []PatchOperation
	var cur *PatchOperation
	var curHunk *PatchHunk
	hasHeader := false

	flush := func() {
		if cur != nil {
			if curHunk != nil && len(curHunk.Lines) > 0 {
				cur.Hunks = append(cur.Hunks, *curHunk)
				curHunk = nil
			}
			ops = append(ops, *cur)
		}
		cur = nil
		curHunk = nil
	}

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		if strings.HasPrefix(line, "diff --git ") {
			flush()
			hasHeader = true
			cur = &PatchOperation{}
			o, n := parseDiffHeader(line)
			cur.OldPath, cur.NewPath, cur.DiffPath = o, n, n
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "new file mode "):
			cur.NewMode = strings.TrimPrefix(line, "new file mode ")
			cur.IsNew = true
		case strings.HasPrefix(line, "deleted file mode "):
			cur.OldMode = strings.TrimPrefix(line, "deleted file mode ")
			cur.IsDelete = true
		case strings.HasPrefix(line, "old mode ") && cur.OldMode == "":
			cur.OldMode = strings.TrimPrefix(line, "old mode ")
		case strings.HasPrefix(line, "new mode ") && cur.NewMode == "":
			cur.NewMode = strings.TrimPrefix(line, "new mode ")
		case strings.HasPrefix(line, "rename from ") || strings.HasPrefix(line, "rename to "):
			cur.IsRename = true
		case strings.HasPrefix(line, "Binary files "):
			cur.IsBinary = true
		case strings.HasPrefix(line, "--- "):
			if p := parsePathHeader(line, "--- "); p != "" {
				cur.OldPath = p
			}
		case strings.HasPrefix(line, "+++ "):
			if p := parsePathHeader(line, "+++ "); p != "" {
				cur.NewPath = p
			}
		case strings.HasPrefix(line, "@@ "):
			if curHunk != nil && len(curHunk.Lines) > 0 {
				cur.Hunks = append(cur.Hunks, *curHunk)
			}
			curHunk = &PatchHunk{}
			fmt.Sscanf(line, "@@ -%d,%d +%d,%d @@", &curHunk.OldStart, &curHunk.OldCount, &curHunk.NewStart, &curHunk.NewCount)
		default:
			if curHunk != nil {
				k := byte(' ')
				l := line
				if len(l) > 0 && (l[0] == '+' || l[0] == '-' || l[0] == ' ') {
					k = l[0]
					l = l[1:]
				}
				curHunk.Lines = append(curHunk.Lines, PatchLine{Kind: k, Content: l})
			}
		}
	}
	flush()

	if !hasHeader {
		return nil, ErrMalformedPatch
	}
	for i := range ops {
		if err := validatePathConsistency(&ops[i]); err != nil {
			return nil, err
		}
	}
	if len(ops) > MaxChangedFiles {
		return nil, fmt.Errorf("%w: %d files (max %d)", ErrTooManyFiles, len(ops), MaxChangedFiles)
	}
	return ops, nil
}

func parseDiffHeader(line string) (oldPath, newPath string) {
	rest := strings.TrimPrefix(line, "diff --git ")
	parts := splitParts(rest)
	if len(parts) >= 2 {
		return stripAPrefix(parts[0]), stripBPrefix(parts[1])
	}
	fs := strings.Fields(rest)
	if len(fs) >= 4 {
		return stripAPrefix(fs[2]), stripBPrefix(fs[3])
	}
	return "", ""
}

func parsePathHeader(line, prefix string) string {
	r := strings.TrimPrefix(line, prefix)
	if idx := strings.IndexByte(r, '\t'); idx >= 0 {
		r = r[:idx]
	}
	if r == "/dev/null" {
		return ""
	}
	return stripAPrefix(stripBPrefix(r))
}

func stripAPrefix(s string) string {
	if strings.HasPrefix(s, "a/") {
		return s[2:]
	}
	return s
}
func stripBPrefix(s string) string {
	if strings.HasPrefix(s, "b/") {
		return s[2:]
	}
	return s
}

func splitParts(s string) []string {
	var r []string
	for len(s) > 0 {
		s = strings.TrimLeft(s, " \t")
		if len(s) == 0 {
			break
		}
		if s[0] == '"' {
			p, rest := parseQuoted(s[1:])
			r = append(r, p)
			s = rest
		} else {
			end := strings.IndexAny(s, " \t")
			if end < 0 {
				r = append(r, s)
				break
			}
			r = append(r, s[:end])
			s = s[end:]
		}
	}
	return r
}

func parseQuoted(s string) (string, string) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			return b.String(), s[i+1:]
		}
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '\\', '"':
				b.WriteByte(s[i+1])
			case 't':
				b.WriteByte('\t')
			case 'n':
				b.WriteByte('\n')
			default:
				b.WriteByte('\\')
				b.WriteByte(s[i+1])
			}
			i++
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String(), ""
}

func validatePathConsistency(op *PatchOperation) error {
	ref := op.DiffPath
	if ref == "" {
		ref = op.NewPath
	}
	if ref == "" {
		return ErrMalformedPatch
	}
	// Rename and delete have legitimate different old/new paths.
	if !op.IsRename && !op.IsDelete {
		if op.OldPath != "" && !op.IsNew && op.OldPath != ref {
			return fmt.Errorf("%w: diff=%q ---=%q", ErrPathMismatch, ref, op.OldPath)
		}
		if op.NewPath != "" && op.NewPath != ref {
			return fmt.Errorf("%w: diff=%q +++=%q", ErrPathMismatch, ref, op.NewPath)
		}
	}
	if op.NewPath != "" {
		op.DiffPath = op.NewPath
	}
	return nil
}

// ApplyPatch writes patch operations into workspaceRoot. Every path is
// validated via filepath.Rel before write. All errors propagate.
func ApplyPatch(workspaceRoot string, ops []PatchOperation) error {
	abs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("cannot resolve workspace root: %w", err)
	}
	for _, op := range ops {
		if err := applyOne(abs, &op); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(absRoot string, op *PatchOperation) error {
	target := op.NewPath
	if target == "" {
		target = op.DiffPath
	}
	absTarget := filepath.Join(absRoot, target)
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("workspace escape: %s (rel=%s)", target, rel)
	}
	if _, err := HardenedPath(absRoot, target); err != nil {
		return err
	}

	// For new files, read no baseline.
	var baseline []string
	if !op.IsNew {
		data, err := os.ReadFile(absTarget)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read baseline %s: %w", absTarget, err)
		}
		if len(data) > 0 {
			baseline = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		}
	}

	// Apply hunks with context verification.
	result, err := applyHunks(baseline, op.Hunks)
	if err != nil {
		return fmt.Errorf("apply %s: %w", absTarget, err)
	}

	dir := filepath.Dir(absTarget)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return os.WriteFile(absTarget, []byte(result), 0644)
}

// applyHunks applies hunks to the baseline with strict context verification.
func applyHunks(baseline []string, hunks []PatchHunk) (string, error) {
	if len(hunks) == 0 {
		return strings.Join(baseline, "\n"), nil
	}
	isNew := len(baseline) == 0

	var out []string
	lineNum := 0

	for _, h := range hunks {
		oldStart := h.OldStart - 1
		if oldStart < 0 {
			oldStart = 0
		}
		if lineNum < oldStart && oldStart <= len(baseline) {
			out = append(out, baseline[lineNum:oldStart]...)
			lineNum = oldStart
		}

		if !isNew {
			contextIdx := lineNum
			for _, l := range h.Lines {
				switch l.Kind {
				case ' ':
					if contextIdx >= len(baseline) {
						return "", fmt.Errorf("context mismatch at line %d: expected %q, file too short", contextIdx+1, l.Content)
					}
					if baseline[contextIdx] != l.Content {
						return "", fmt.Errorf("context mismatch at line %d: expected %q, got %q", contextIdx+1, l.Content, baseline[contextIdx])
					}
					contextIdx++
				case '-':
					if contextIdx >= len(baseline) {
						return "", fmt.Errorf("removal at line %d: expected %q, file too short", contextIdx+1, l.Content)
					}
					if baseline[contextIdx] != l.Content {
						return "", fmt.Errorf("removal mismatch at line %d: expected %q, got %q", contextIdx+1, l.Content, baseline[contextIdx])
					}
					contextIdx++
				}
			}
			lineNum = contextIdx
		}

		for _, l := range h.Lines {
			if l.Kind != '-' {
				out = append(out, l.Content)
			}
		}
	}

	if !isNew && lineNum < len(baseline) {
		out = append(out, baseline[lineNum:]...)
	}

	result := strings.Join(out, "\n")
	if result != "" {
		result += "\n"
	}
	return result, nil
}
