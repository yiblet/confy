package confy

import (
	"fmt"
	"reflect"
)

// Schema is the static projection of a Confy: what it consumes, as data.
type Schema struct {
	Root Node
	Defs map[string]Node // named nodes introduced by Rec
}

// Node is a schema node. The set is sealed.
type Node interface{ node() }

// Meta holds annotations that any node may carry.
type Meta struct {
	Doc        string
	Secret     bool
	Deprecated string // non-empty means deprecated, with the reason
}

// Empty is the schema of Pure: it reads nothing.
type Empty struct{ Meta }

// Leaf is a scalar read with a codec. Default is the rendered default, if
// any. Optional is set by Optional or Default.
type Leaf struct {
	Meta
	Type     string
	Default  *string
	Optional bool
}

// Object is a mapping level: declared fields plus any unions whose tag and
// arms live at this level.
type Object struct {
	Meta
	Fields []Prop
	Unions []Union
}

// Prop is one key of an Object.
type Prop struct {
	Key      string
	Node     Node
	Optional bool
}

// Array is a sequence whose every element is read by Elem (key[*]).
type Array struct {
	Meta
	Elem     Node
	Optional bool
}

// Table is a mapping with free keys whose every value is read by Elem.
type Table struct {
	Meta
	Elem     Node
	Optional bool
}

// Union is a named selection: by the value of a tag field (Switch) or by the
// kind of the node (Shape). Its schema is the union of its arms.
type Union struct {
	Meta
	Discriminant Discriminant
	Arms         []Variant
	Optional     bool
}

// Variant is one named alternative of a Union.
type Variant struct {
	Tag  string
	Node Node
}

// DiscriminantKind says what a Union switches on.
type DiscriminantKind int

const (
	ByTag  DiscriminantKind = iota // the string value at Discriminant.Key
	ByKind                         // the Kind of the node; tags are Kind names
)

// Discriminant identifies how a Union picks its arm.
type Discriminant struct {
	Kind DiscriminantKind
	Key  string // for ByTag
}

// Ref names a node defined in Schema.Defs (introduced by Rec).
type Ref struct {
	Meta
	Name string
}

func (Empty) node()  {}
func (Leaf) node()   {}
func (Object) node() {}
func (Array) node()  {}
func (Table) node()  {}
func (Union) node()  {}
func (Ref) node()    {}

// union is the schema half of With: the two operands' reads, combined.
// Conflicts are programmer errors and panic at construction time.
func union(a, b Node) Node {
	switch x := a.(type) {
	case Empty:
		return b
	case Object:
		switch y := b.(type) {
		case Empty:
			return a
		case Object:
			return mergeObjects(x, y)
		case Union:
			return mergeObjects(x, Object{Unions: []Union{y}})
		}
	case Union:
		switch y := b.(type) {
		case Empty:
			return a
		case Object:
			return mergeObjects(Object{Unions: []Union{x}}, y)
		case Union:
			return Object{Unions: []Union{x, y}}
		}
	case Array:
		switch y := b.(type) {
		case Empty:
			return a
		case Array:
			return Array{Meta: x.Meta, Elem: union(x.Elem, y.Elem), Optional: x.Optional && y.Optional}
		}
	case Table:
		switch y := b.(type) {
		case Empty:
			return a
		case Table:
			return Table{Meta: x.Meta, Elem: union(x.Elem, y.Elem), Optional: x.Optional && y.Optional}
		}
	case Ref:
		switch y := b.(type) {
		case Empty:
			return a
		case Ref:
			if x.Name == y.Name {
				return a
			}
		}
	case Leaf:
		if _, ok := b.(Empty); ok {
			return a
		}
		panic(fmt.Sprintf("confy: two readers claim the same scalar position (%s and %s)", describe(a), describe(b)))
	}
	panic(fmt.Sprintf("confy: schema shape conflict: %s vs %s", describe(a), describe(b)))
}

func mergeObjects(a, b Object) Object {
	out := Object{Meta: a.Meta, Fields: append([]Prop(nil), a.Fields...)}
	for _, f := range b.Fields {
		merged := false
		for i, g := range out.Fields {
			if g.Key == f.Key {
				out.Fields[i] = Prop{Key: f.Key, Node: union(g.Node, f.Node), Optional: g.Optional && f.Optional}
				merged = true
				break
			}
		}
		if !merged {
			out.Fields = append(out.Fields, f)
		}
	}
	out.Unions = append(append([]Union(nil), a.Unions...), b.Unions...)
	if out.Meta.Doc == "" {
		out.Meta.Doc = b.Meta.Doc
	}
	return out
}

func mergeDefs(a, b map[string]Node) map[string]Node {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make(map[string]Node, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if prev, ok := out[k]; ok && !reflect.DeepEqual(prev, v) {
			panic(fmt.Sprintf("confy: Rec %q defined twice with different shapes", k))
		}
		out[k] = v
	}
	return out
}

func describe(n Node) string {
	switch x := n.(type) {
	case Empty:
		return "empty"
	case Leaf:
		return "leaf " + x.Type
	case Object:
		keys := make([]string, len(x.Fields))
		for i, f := range x.Fields {
			keys[i] = f.Key
		}
		return fmt.Sprintf("object%v", keys)
	case Array:
		return "array"
	case Table:
		return "dict"
	case Union:
		return "union"
	case Ref:
		return "ref " + x.Name
	}
	return fmt.Sprintf("%T", n)
}

// present is the rule Optional and Default use to decide whether a reader's
// input exists at a cursor.
func present(n Node, v Value, defs map[string]Node) bool {
	switch x := n.(type) {
	case Empty:
		return true
	case Leaf, Array, Table:
		return v.Kind() != KindAbsent
	case Ref:
		if d, ok := defs[x.Name]; ok {
			return present(d, v, defs)
		}
		return v.Kind() != KindAbsent
	case Union:
		if x.Discriminant.Kind == ByKind {
			return v.Kind() != KindAbsent
		}
		return v.Get(x.Discriminant.Key).Kind() != KindAbsent
	case Object:
		if v.Kind() != KindMapping {
			return false
		}
		for _, f := range x.Fields {
			if v.Get(f.Key).Kind() != KindAbsent {
				return true
			}
		}
		for _, u := range x.Unions {
			if present(u, v, defs) {
				return true
			}
		}
		return false
	}
	return false
}

// markOptional records optionality in the schema. Object fields are marked
// individually: the run-time rule is all-or-nothing on the group, which is
// strictly tighter than what the schema shows.
func markOptional(n Node) Node {
	switch x := n.(type) {
	case Leaf:
		x.Optional = true
		return x
	case Array:
		x.Optional = true
		return x
	case Table:
		x.Optional = true
		return x
	case Union:
		x.Optional = true
		return x
	case Object:
		out := Object{Meta: x.Meta, Fields: make([]Prop, len(x.Fields)), Unions: make([]Union, len(x.Unions))}
		for i, f := range x.Fields {
			out.Fields[i] = Prop{Key: f.Key, Node: f.Node, Optional: true}
		}
		for i, u := range x.Unions {
			u.Optional = true
			out.Unions[i] = u
		}
		return out
	}
	return n
}

// annotate applies f to the node's Meta. For the common Field(key) shape —
// an Object with exactly one field and no unions — the annotation lands on
// the field's node, so String("host").Doc("…") documents the leaf.
func annotate(n Node, f func(*Meta)) Node {
	if o, ok := n.(Object); ok && len(o.Fields) == 1 && len(o.Unions) == 0 {
		fld := o.Fields[0]
		fld.Node = annotate(fld.Node, f)
		return Object{Meta: o.Meta, Fields: []Prop{fld}}
	}
	switch x := n.(type) {
	case Empty:
		f(&x.Meta)
		return x
	case Leaf:
		f(&x.Meta)
		return x
	case Object:
		f(&x.Meta)
		return x
	case Array:
		f(&x.Meta)
		return x
	case Table:
		f(&x.Meta)
		return x
	case Union:
		f(&x.Meta)
		return x
	case Ref:
		f(&x.Meta)
		return x
	}
	return n
}

func metaOf(n Node) Meta {
	switch x := n.(type) {
	case Empty:
		return x.Meta
	case Leaf:
		return x.Meta
	case Object:
		return x.Meta
	case Array:
		return x.Meta
	case Table:
		return x.Meta
	case Union:
		return x.Meta
	case Ref:
		return x.Meta
	}
	return Meta{}
}

// singleLeaf returns the Leaf when n is a Leaf or a one-field Object whose
// field is a Leaf (the Field(key) shape), plus a setter that rebuilds n.
func singleLeaf(n Node) (Leaf, func(Leaf) Node, bool) {
	switch x := n.(type) {
	case Leaf:
		return x, func(l Leaf) Node { return l }, true
	case Object:
		if len(x.Fields) == 1 && len(x.Unions) == 0 {
			if l, ok := x.Fields[0].Node.(Leaf); ok {
				return l, func(l Leaf) Node {
					return Object{Meta: x.Meta, Fields: []Prop{{Key: x.Fields[0].Key, Node: l, Optional: l.Optional}}}
				}, true
			}
		}
	}
	return Leaf{}, nil, false
}

// resolve follows Ref nodes through defs.
func resolve(n Node, defs map[string]Node) Node {
	for i := 0; i < 64; i++ {
		r, ok := n.(Ref)
		if !ok {
			return n
		}
		d, ok := defs[r.Name]
		if !ok {
			return n
		}
		n = d
	}
	return n
}
