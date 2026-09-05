package confy

import (
	"fmt"
	"sort"
)

// Kind classifies a Value node.
type Kind int

const (
	KindAbsent Kind = iota
	KindScalar
	KindMapping
	KindSequence
)

func (k Kind) String() string {
	switch k {
	case KindAbsent:
		return "absent"
	case KindScalar:
		return "scalar"
	case KindMapping:
		return "mapping"
	case KindSequence:
		return "sequence"
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Value is the input tree a Confy reads. Adapters turn decoder output into a
// Value; source layering (env < file < flags) is done on Values, with Layer,
// before a Confy ever sees them.
type Value interface {
	Kind() Kind
	// Scalar returns the scalar payload when Kind()==KindScalar: a string,
	// bool, int64, float64, or whatever the adapter produced.
	Scalar() any
	// Get returns the child at key, or Absent if this is not a mapping or the
	// key is missing.
	Get(key string) Value
	// Keys lists mapping keys in a deterministic order.
	Keys() []string
	// Len is the number of elements when Kind()==KindSequence.
	Len() int
	// Index returns the i-th element, or Absent when out of range.
	Index(i int) Value
	// Pos reports the source position when the adapter has one.
	Pos() (Pos, bool)
}

// Absent is the Value at any position that does not exist.
var Absent Value = absent{}

type absent struct{}

func (absent) Kind() Kind       { return KindAbsent }
func (absent) Scalar() any      { return nil }
func (absent) Get(string) Value { return Absent }
func (absent) Keys() []string   { return nil }
func (absent) Len() int         { return 0 }
func (absent) Index(int) Value  { return Absent }
func (absent) Pos() (Pos, bool) { return Pos{}, false }
func (absent) String() string   { return "<absent>" }

// FromAny adapts the trees produced by encoding/json, yaml, toml and similar
// decoders: map[string]any and map[any]any are mappings, []any is a
// sequence, nil is Absent, everything else is a scalar.
func FromAny(v any) Value {
	switch x := v.(type) {
	case nil:
		return Absent
	case Value:
		return x
	case map[string]any:
		return anyMap(x)
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[fmt.Sprint(k)] = val
		}
		return anyMap(m)
	case []any:
		return anySeq(x)
	case []string:
		s := make([]any, len(x))
		for i, e := range x {
			s[i] = e
		}
		return anySeq(s)
	default:
		return anyScalar{x}
	}
}

type anyMap map[string]any

func (m anyMap) Kind() Kind  { return KindMapping }
func (m anyMap) Scalar() any { return nil }
func (m anyMap) Get(key string) Value {
	v, ok := m[key]
	if !ok {
		return Absent
	}
	return FromAny(v)
}
func (m anyMap) Keys() []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
func (m anyMap) Len() int         { return 0 }
func (m anyMap) Index(int) Value  { return Absent }
func (m anyMap) Pos() (Pos, bool) { return Pos{}, false }

type anySeq []any

func (s anySeq) Kind() Kind       { return KindSequence }
func (s anySeq) Scalar() any      { return nil }
func (s anySeq) Get(string) Value { return Absent }
func (s anySeq) Keys() []string   { return nil }
func (s anySeq) Len() int         { return len(s) }
func (s anySeq) Index(i int) Value {
	if i < 0 || i >= len(s) {
		return Absent
	}
	return FromAny(s[i])
}
func (s anySeq) Pos() (Pos, bool) { return Pos{}, false }

type anyScalar struct{ v any }

func (s anyScalar) Kind() Kind       { return KindScalar }
func (s anyScalar) Scalar() any      { return s.v }
func (s anyScalar) Get(string) Value { return Absent }
func (s anyScalar) Keys() []string   { return nil }
func (s anyScalar) Len() int         { return 0 }
func (s anyScalar) Index(int) Value  { return Absent }
func (s anyScalar) Pos() (Pos, bool) { return Pos{}, false }

// Layer merges over onto base: where both are mappings the result is a
// mapping whose children are layered recursively; otherwise over wins unless
// it is Absent. This is source precedence (env < file < flags) as a Value
// operation, kept outside the Confy DSL on purpose.
func Layer(base, over Value) Value {
	if over.Kind() == KindAbsent {
		return base
	}
	if base.Kind() == KindMapping && over.Kind() == KindMapping {
		return layered{base, over}
	}
	return over
}

type layered struct{ base, over Value }

func (l layered) Kind() Kind  { return KindMapping }
func (l layered) Scalar() any { return nil }
func (l layered) Get(key string) Value {
	return Layer(l.base.Get(key), l.over.Get(key))
}
func (l layered) Keys() []string {
	seen := map[string]bool{}
	var ks []string
	for _, k := range l.base.Keys() {
		if !seen[k] {
			seen[k] = true
			ks = append(ks, k)
		}
	}
	for _, k := range l.over.Keys() {
		if !seen[k] {
			seen[k] = true
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	return ks
}
func (l layered) Len() int        { return 0 }
func (l layered) Index(int) Value { return Absent }
func (l layered) Pos() (Pos, bool) {
	if p, ok := l.over.Pos(); ok {
		return p, true
	}
	return l.base.Pos()
}

// cursor is a Value together with the Path that reached it. Under extends
// both in lockstep, so an Issue takes its Path from the cursor at creation.
type cursor struct {
	v    Value
	path Path
}

func (c cursor) down(key string) cursor { return cursor{c.v.Get(key), c.path.key(key)} }
func (c cursor) at(i int) cursor        { return cursor{c.v.Index(i), c.path.index(i)} }

func (c cursor) issue(msg string) Issue {
	iss := Issue{Path: c.path, Msg: msg}
	if p, ok := c.v.Pos(); ok {
		iss.Pos = &p
	}
	return iss
}

func (c cursor) issues(msg string) Issues { return Issues{c.issue(msg)} }

// rejected reports a kind mismatch at the cursor; see Issue.rejected.
func (c cursor) rejected(msg string) Issues {
	iss := c.issue(msg)
	iss.rejected = true
	return Issues{iss}
}
