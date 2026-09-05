package confy

import (
	"fmt"
	"sort"
	"strings"
)

// Optional makes a reader tolerate absence: nil when its input is not
// present (see present), otherwise the inner result. The schema marks the
// inner node optional.
func Optional[A any](c Confy[A]) Confy[*A] {
	node := markOptional(c.node)
	return Confy[*A]{node: node, defs: c.defs, run: func(cur cursor) (*A, Issues) {
		if !present(c.node, cur.v, c.defs) {
			return nil, nil
		}
		a, e := c.run(cur)
		if len(e) > 0 {
			return nil, e
		}
		return &a, nil
	}}
}

// Seq reads the cursor as a sequence, applying elem to every element.
func Seq[A any](elem Confy[A]) Confy[[]A] {
	return Confy[[]A]{node: Array{Elem: elem.node}, defs: elem.defs, run: func(cur cursor) ([]A, Issues) {
		switch cur.v.Kind() {
		case KindAbsent:
			return nil, cur.issues("missing")
		case KindSequence:
		default:
			return nil, cur.rejected("expected sequence, got " + cur.v.Kind().String())
		}
		var all Issues
		out := make([]A, 0, cur.v.Len())
		for i := 0; i < cur.v.Len(); i++ {
			a, e := elem.run(cur.at(i))
			all = append(all, e...)
			out = append(out, a)
		}
		if len(all) > 0 {
			return nil, all
		}
		return out, nil
	}}
}

// List reads key as a sequence of elem: Under(key, Seq(elem)).
func List[A any](key string, elem Confy[A]) Confy[[]A] { return Under(key, Seq(elem)) }

// Table reads the cursor as a mapping with free keys, applying elem to every
// value. Keys are in the Value's order (sorted for FromAny).
func Dict[A any](elem Confy[A]) Confy[map[string]A] {
	return Confy[map[string]A]{node: Table{Elem: elem.node}, defs: elem.defs, run: func(cur cursor) (map[string]A, Issues) {
		switch cur.v.Kind() {
		case KindAbsent:
			return nil, cur.issues("missing")
		case KindMapping:
		default:
			return nil, cur.rejected("expected mapping, got " + cur.v.Kind().String())
		}
		var all Issues
		out := map[string]A{}
		for _, k := range cur.v.Keys() {
			a, e := elem.run(cur.down(k))
			all = append(all, e...)
			out[k] = a
		}
		if len(all) > 0 {
			return nil, all
		}
		return out, nil
	}}
}

// MapOf reads key as a mapping of elem: Under(key, Dict(elem)).
func MapOf[A any](key string, elem Confy[A]) Confy[map[string]A] { return Under(key, Dict(elem)) }

// SwitchArm is one named alternative for Switch.
type SwitchArm[I any] struct {
	tag string
	cfg Confy[I]
}

// Case is a Switch arm whose reader yields a variant C of the sum type I;
// widen is the (compiler-checked) conversion. Both type parameters infer:
//
//	Case("acme", acme, func(a Acme) TLS { return a })
func Case[I, C any](tag string, c Confy[C], widen func(C) I) SwitchArm[I] {
	return SwitchArm[I]{tag: tag, cfg: c.Map(widen)}
}

// Arm is a Switch arm whose reader already yields I.
func Arm[I any](tag string, c Confy[I]) SwitchArm[I] { return SwitchArm[I]{tag: tag, cfg: c} }

// Switch selects an arm by the string at key, then runs that arm at the
// same cursor. The schema is the union of all arms, tagged by name: a
// sound over-approximation of what run will read.
func Switch[I any](key string, arms ...SwitchArm[I]) Confy[I] {
	if len(arms) == 0 {
		panic("confy: Switch needs at least one arm")
	}
	u := Union{Discriminant: Discriminant{Kind: ByTag, Key: key}}
	var defs map[string]Node
	tags := make([]string, 0, len(arms))
	seen := map[string]bool{}
	for _, a := range arms {
		if seen[a.tag] {
			panic(fmt.Sprintf("confy: Switch %q has duplicate arm %q", key, a.tag))
		}
		seen[a.tag] = true
		if _, isLeaf := a.cfg.node.(Leaf); isLeaf {
			panic(fmt.Sprintf("confy: Switch %q arm %q reads the cursor as a scalar, but the tag makes it a mapping", key, a.tag))
		}
		tags = append(tags, a.tag)
		u.Arms = append(u.Arms, Variant{Tag: a.tag, Node: a.cfg.node})
		defs = mergeDefs(defs, a.cfg.defs)
	}
	oneOf := "one of: " + strings.Join(tags, ", ")
	return Confy[I]{node: u, defs: defs, run: func(cur cursor) (I, Issues) {
		var z I
		tagCur := cur.down(key)
		switch tagCur.v.Kind() {
		case KindAbsent:
			return z, tagCur.issues("missing (" + oneOf + ")")
		case KindScalar:
		default:
			return z, tagCur.issues("expected tag string, got " + tagCur.v.Kind().String())
		}
		tag := fmt.Sprint(tagCur.v.Scalar())
		for _, a := range arms {
			if a.tag == tag {
				return a.cfg.run(cur)
			}
		}
		return z, tagCur.issues(fmt.Sprintf("unknown tag %q (%s)", tag, oneOf))
	}}
}

// ShapeArm is one alternative for Shape, selected by node kind.
type ShapeArm[I any] struct {
	kind Kind
	cfg  Confy[I]
}

// KindCase is a Shape arm for nodes of kind k whose reader yields variant C.
func KindCase[I, C any](k Kind, c Confy[C], widen func(C) I) ShapeArm[I] {
	return ShapeArm[I]{kind: k, cfg: c.Map(widen)}
}

// KindArm is a Shape arm whose reader already yields I.
func KindArm[I any](k Kind, c Confy[I]) ShapeArm[I] { return ShapeArm[I]{kind: k, cfg: c} }

// Shape selects an arm by the kind of the node at the cursor (scalar,
// mapping or sequence) — the "short syntax or long syntax" pattern. The
// schema is the union of the arms, named by kind.
func Shape[I any](arms ...ShapeArm[I]) Confy[I] {
	if len(arms) == 0 {
		panic("confy: Shape needs at least one arm")
	}
	u := Union{Discriminant: Discriminant{Kind: ByKind}}
	var defs map[string]Node
	seen := map[Kind]bool{}
	names := make([]string, 0, len(arms))
	for _, a := range arms {
		if a.kind == KindAbsent {
			panic("confy: Shape cannot have an arm for the absent kind; use Optional")
		}
		if seen[a.kind] {
			panic("confy: Shape has two arms for kind " + a.kind.String())
		}
		seen[a.kind] = true
		names = append(names, a.kind.String())
		u.Arms = append(u.Arms, Variant{Tag: a.kind.String(), Node: a.cfg.node})
		defs = mergeDefs(defs, a.cfg.defs)
	}
	expected := strings.Join(names, " or ")
	return Confy[I]{node: u, defs: defs, run: func(cur cursor) (I, Issues) {
		var z I
		if cur.v.Kind() == KindAbsent {
			return z, cur.issues("missing")
		}
		for _, a := range arms {
			if a.kind == cur.v.Kind() {
				return a.cfg.run(cur)
			}
		}
		return z, cur.rejected("expected " + expected + ", got " + cur.v.Kind().String())
	}}
}

// Rec ties a recursive knot: f receives a placeholder for the reader being
// defined and returns the definition. The schema stays finite: the
// placeholder is a Ref to name, and the definition is recorded in
// Schema.Defs.
func Rec[A any](name string, f func(self Confy[A]) Confy[A]) Confy[A] {
	var inner Confy[A]
	self := Confy[A]{node: Ref{Name: name}, run: func(cur cursor) (A, Issues) { return inner.run(cur) }}
	inner = f(self)
	defs := mergeDefs(inner.defs, map[string]Node{name: inner.node})
	return Confy[A]{node: Ref{Name: name}, defs: defs, run: inner.run}
}

// sortedKinds is used by renderers to list Shape arms deterministically.
func sortedKinds(u Union) []Variant {
	arms := append([]Variant(nil), u.Arms...)
	sort.Slice(arms, func(i, j int) bool { return arms[i].Tag < arms[j].Tag })
	return arms
}
