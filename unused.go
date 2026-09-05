package confy

import "fmt"

// Unused lists every path in v that no reader claims. It is a co-walk of
// the value and the schema: at a mapping level the claimed keys are the
// declared fields, each union's tag, and the fields of the arm the tag
// selects (or of every arm when the tag is absent or unrecognised — the sound
// over-approximation). A Table claims all keys; an Array recurses into every
// element; a Ref resolves through Defs.
func (c Confy[A]) Unused(v Value) []Path {
	var out []Path
	walkUnused(c.node, cursor{v: v}, c.defs, &out)
	return out
}

// BuildOption adjusts Build. Options are values, so a service can define
// its policy once and pass it everywhere.
type BuildOption func(*buildOptions)

type buildOptions struct{ allowUnused func(Path) bool }

// AllowUnused makes Build ignore every key in the input that no reader
// claims. Without it, a misspelled or unexpected key is an error.
func AllowUnused() BuildOption { return AllowUnusedMatching(func(Path) bool { return true }) }

// AllowUnusedMatching makes Build ignore unclaimed keys whose path satisfies
// match, and still report the rest. Use it for extension keys a format
// permits anywhere, such as compose's "x-" prefix:
//
//	confy.AllowUnusedMatching(func(p confy.Path) bool {
//	    return strings.HasPrefix(p[len(p)-1].Key, "x-")
//	})
func AllowUnusedMatching(match func(Path) bool) BuildOption {
	return func(o *buildOptions) { o.allowUnused = match }
}

// Build reads an A from v and reports every issue in one pass: type,
// presence and validation issues, and by default every key in v that no
// reader claims, so a misspelled or unexpected key is an error rather than a
// silent default. Keys beneath a node already refused for its kind are not
// reported again. The error, when non-nil, is an Issues.
func (c Confy[A]) Build(v Value, opts ...BuildOption) (A, error) {
	var o buildOptions
	for _, opt := range opts {
		opt(&o)
	}
	a, e := c.run(cursor{v: v})
unused:
	for _, p := range c.Unused(v) {
		if o.allowUnused != nil && o.allowUnused(p) {
			continue
		}
		// A subtree refused for its kind is one error, not one per key.
		for _, iss := range e {
			if iss.rejected && p.HasPrefix(iss.Path) {
				continue unused
			}
		}
		e = append(e, Issue{Path: p, Msg: "unused key"})
	}
	return a, e.err()
}

func walkUnused(n Node, cur cursor, defs map[string]Node, out *[]Path) {
	n = resolve(n, defs)
	switch x := n.(type) {
	case Empty:
		if cur.v.Kind() == KindMapping {
			for _, k := range cur.v.Keys() {
				*out = append(*out, cur.path.key(k))
			}
		}
	case Leaf:
		// A leaf claims whatever sits here; a wrong kind is the reader's job.
	case Array:
		if cur.v.Kind() == KindSequence {
			for i := 0; i < cur.v.Len(); i++ {
				walkUnused(x.Elem, cur.at(i), defs, out)
			}
		}
	case Table:
		if cur.v.Kind() == KindMapping {
			for _, k := range cur.v.Keys() {
				walkUnused(x.Elem, cur.down(k), defs, out)
			}
		}
	case Union, Object:
		walkLevel(n, cur, defs, out)
	}
}

// walkLevel handles one mapping level: an Object, or a Union standing alone.
func walkLevel(n Node, cur cursor, defs map[string]Node, out *[]Path) {
	claimed := map[string][]Node{}
	dictAll := false
	var collect func(Node)
	collect = func(n Node) {
		n = resolve(n, defs)
		switch x := n.(type) {
		case Object:
			for _, f := range x.Fields {
				claimed[f.Key] = append(claimed[f.Key], f.Node)
			}
			for _, u := range x.Unions {
				collect(u)
			}
		case Union:
			switch x.Discriminant.Kind {
			case ByTag:
				claimed[x.Discriminant.Key] = append(claimed[x.Discriminant.Key], Leaf{Type: "string"})
				tagV := cur.v.Get(x.Discriminant.Key)
				var picked *Variant
				if tagV.Kind() == KindScalar {
					tag := fmt.Sprint(tagV.Scalar())
					for i := range x.Arms {
						if x.Arms[i].Tag == tag {
							picked = &x.Arms[i]
						}
					}
				}
				if picked != nil {
					collect(picked.Node)
				} else {
					for _, a := range x.Arms {
						collect(a.Node)
					}
				}
			case ByKind:
				for _, a := range x.Arms {
					if a.Tag == cur.v.Kind().String() {
						collect(a.Node)
					}
				}
			}
		case Table:
			dictAll = true
			for _, k := range cur.v.Keys() {
				claimed[k] = append(claimed[k], x.Elem)
			}
		case Leaf, Empty, Array:
			// Nothing claimed at this mapping level.
		}
	}
	collect(n)

	if u, ok := n.(Union); ok && u.Discriminant.Kind == ByKind && cur.v.Kind() != KindMapping {
		// A Shape at a scalar or sequence: delegate to the matching arm.
		for _, a := range u.Arms {
			if a.Tag == cur.v.Kind().String() {
				walkUnused(a.Node, cur, defs, out)
			}
		}
		return
	}
	if cur.v.Kind() != KindMapping {
		return
	}
	for _, k := range cur.v.Keys() {
		nodes, ok := claimed[k]
		if !ok && !dictAll {
			*out = append(*out, cur.path.key(k))
			continue
		}
		for _, sub := range nodes {
			walkUnused(sub, cur.down(k), defs, out)
		}
	}
}
