package confy

import (
	"fmt"
	"strconv"
	"strings"
)

// Step is one segment of a Path: either a mapping key or a sequence index.
type Step struct {
	Key     string
	Index   int
	IsIndex bool
}

// Path locates a position in a Value: a.b[0].c.
type Path []Step

// String renders the path as a.b[0].c; the empty path renders as "(root)".
func (p Path) String() string {
	if len(p) == 0 {
		return "(root)"
	}
	var b strings.Builder
	for i, s := range p {
		if s.IsIndex {
			b.WriteString("[" + strconv.Itoa(s.Index) + "]")
			continue
		}
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s.Key)
	}
	return b.String()
}

// HasPrefix reports whether q is a prefix of p.
func (p Path) HasPrefix(q Path) bool {
	if len(q) > len(p) {
		return false
	}
	for i := range q {
		if p[i] != q[i] {
			return false
		}
	}
	return true
}

func (p Path) key(k string) Path {
	out := make(Path, len(p), len(p)+1)
	copy(out, p)
	return append(out, Step{Key: k})
}

func (p Path) index(i int) Path {
	out := make(Path, len(p), len(p)+1)
	copy(out, p)
	return append(out, Step{Index: i, IsIndex: true})
}

// Issue is one complaint about the input. Path is where; Pos is the source
// position if the Value adapter supplied one. When Secret is set, Error()
// prints a redacted message that never contains the offending value.
type Issue struct {
	Path   Path
	Msg    string
	Pos    *Pos
	Secret bool

	// safe is an alternative message with no input value in it; used when
	// Secret is set. Empty means Msg is already safe.
	safe string

	// rejected marks a kind mismatch: the whole subtree at Path was refused,
	// so Build does not also report keys beneath it as unused.
	rejected bool
}

func (i Issue) Error() string {
	msg := i.Msg
	if i.Secret && i.safe != "" {
		msg = i.safe
	}
	s := i.Path.String() + ": " + msg
	if i.Pos != nil {
		s += " (" + i.Pos.String() + ")"
	}
	return s
}

// Issues is every issue found in one pass. It implements error and
// Unwrap() []error, so it composes with errors.Is/As and errors.Join.
type Issues []Issue

func (is Issues) Error() string {
	parts := make([]string, len(is))
	for i, iss := range is {
		parts[i] = iss.Error()
	}
	return strings.Join(parts, "\n")
}

// Unwrap exposes each Issue as an error.
func (is Issues) Unwrap() []error {
	out := make([]error, len(is))
	for i, iss := range is {
		out[i] = iss
	}
	return out
}

// err returns nil for an empty list, so callers can return it as `error`.
func (is Issues) err() error {
	if len(is) == 0 {
		return nil
	}
	return is
}

// Pos is a source position supplied by a Value adapter.
type Pos struct {
	File      string
	Line, Col int
}

func (p Pos) String() string {
	if p.File == "" {
		return fmt.Sprintf("%d:%d", p.Line, p.Col)
	}
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
}
