package service

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrForbidden is for a caller who may see the research but not do this to
	// it — a viewer trying to write. It is deliberately distinct from
	// ErrNotFound: hiding a research from someone who can already read it
	// protects nothing and reads as a bug.
	ErrForbidden            = errors.New("your role in this team does not allow this")
	ErrDuplicateSectionName = errors.New("section name already exists in this research")
	ErrSectionHasNoEntries  = errors.New("cannot complete a section with no entries")
	ErrTextReplaceNotFound  = errors.New("text_replace: from string not found in content")
	ErrTextReplaceOnBlocks  = errors.New("text_replace does not work on a blocks entry: it would edit the stored JSON as text. Use entry_patch to change one block, or send the whole document in content")
	ErrQuestionDepthLimit   = errors.New("question nesting depth limit exceeded (max 3 levels)")
	// ErrInvalidFieldSpec is a malformed section declaration — a reserved key, a
	// cap breached, an enum with no options. It is a sentinel so the handler
	// answers 400 with the reasons rather than 500: the caller can fix every one
	// of these, and a 500 tells them the server broke instead.
	ErrInvalidFieldSpec = errors.New("invalid field_spec")
	ErrAnswerRequired   = errors.New("answered questions must have a non-empty answer")
	// ErrValidation is a caller mistake with nothing more specific to say — a
	// missing required field, a value outside a closed vocabulary. It is one
	// sentinel rather than a family of them because the useful half of the
	// refusal is the message wrapped around it, and a handler only needs to know
	// this is a 400 the caller can fix.
	ErrValidation      = errors.New("invalid request")
	ErrMutualExclusion = errors.New("mutually exclusive fields provided")
)

// isCode returns true if s looks like a short code (e.g. R1, E23, SS1, T5, Q3) rather than a UUID.
func isCode(s string) bool {
	if len(s) < 2 || len(s) > 10 {
		return false
	}
	// Determine prefix length: SS, RM have 2-char prefix, others have 1-char
	prefixLen := 0
	switch {
	case len(s) >= 2 && (s[:2] == "SS" || s[:2] == "RM"):
		prefixLen = 2
	case s[0] == 'R' || s[0] == 'E' || s[0] == 'S' || s[0] == 'T' || s[0] == 'Q' || s[0] == 'N':
		prefixLen = 1
	default:
		return false
	}
	for _, c := range s[prefixLen:] {
		if !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

// IncompleteMetadataError refuses to call a document finished while required
// fields are unanswered.
//
// It carries the field keys because the only useful thing to do with this
// refusal is name them: a message saying "some fields are missing" makes the
// caller go looking for what the server already knows. The override is
// AllowIncomplete on the request — a decision, not a retry.
type IncompleteMetadataError struct {
	Missing []string
}

func (e *IncompleteMetadataError) Error() string {
	return "cannot complete: required metadata is unanswered: " + strings.Join(e.Missing, ", ")
}

// noAuthf is ErrNoAuth with something the reader can act on.
//
// ErrNoAuth's own text is "sign in to manage teams" — it was born in
// TeamService and reads as a non sequitur to a stdio session that asked to
// create a project and named no team. The wrapper keeps every errors.Is check
// and the 401 mapping working while saying what actually went wrong.
func noAuthf(format string, a ...any) error {
	return &noAuthError{msg: fmt.Sprintf(format, a...)}
}

type noAuthError struct{ msg string }

func (e *noAuthError) Error() string { return e.msg }
func (e *noAuthError) Unwrap() error { return ErrNoAuth }
