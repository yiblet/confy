// Package confy reads configuration with readers that are their own
// description.
//
// A Confy[A] is a value with two projections: a static Schema (what it
// consumes: keys, types, defaults, docs, union arms) and a dynamic run
// (input → A plus every issue at once). Every combinator defines both.
// There is no bind, so the schema can always be computed before any input
// exists; that is what makes Docs, Template, JSONSchema and Unused possible
// from one definition.
//
// Method-vs-function placement follows a Go 1.27 rule: a method of Confy[A]
// may not mention Confy[f(A)] for any type expression other than A itself
// (the compiler reports an instantiation cycle), so shape-changing
// combinators (Optional, Seq, List, Table, Switch, Shape, Rec) are free
// functions while Map, With, At and the type-preserving annotations are
// methods.
package confy

import (
	"fmt"
	"time"
)

// Confy reads an A from a Value. It is abstract: build one with the
// constructors and combinators in this package.
type Confy[A any] struct {
	node Node
	defs map[string]Node
	run  func(cursor) (A, Issues)
	enc  func(A) string // set on leaves only; lets Default render
}

// Pure reads nothing and yields a.
func Pure[A any](a A) Confy[A] {
	return Confy[A]{node: Empty{}, run: func(cursor) (A, Issues) { return a, nil }}
}

// With is the product: both readers run at the same cursor, their schemas
// are unioned, their issues are concatenated, and f combines the results.
func (c Confy[A]) With[B, C any](o Confy[B], f func(A, B) C) Confy[C] {
	node := union(c.node, o.node)
	defs := mergeDefs(c.defs, o.defs)
	return Confy[C]{node: node, defs: defs, run: func(cur cursor) (C, Issues) {
		a, e1 := c.run(cur)
		b, e2 := o.run(cur)
		if len(e1)+len(e2) > 0 {
			var z C
			return z, append(e1, e2...)
		}
		return f(a, b), nil
	}}
}

// Map transforms the result; the schema is unchanged.
func (c Confy[A]) Map[B any](f func(A) B) Confy[B] {
	return Confy[B]{node: c.node, defs: c.defs, run: func(cur cursor) (B, Issues) {
		a, e := c.run(cur)
		if len(e) > 0 {
			var z B
			return z, e
		}
		return f(a), nil
	}}
}

// MapErr transforms the result and may fail; a failure is one issue at the
// reader's cursor path. It runs only when the inner reader had no issues.
func (c Confy[A]) MapErr[B any](f func(A) (B, error)) Confy[B] {
	return Confy[B]{node: c.node, defs: c.defs, run: func(cur cursor) (B, Issues) {
		a, e := c.run(cur)
		if len(e) > 0 {
			var z B
			return z, e
		}
		b, err := f(a)
		if err != nil {
			var z B
			return z, issueAt(c.node, cur).issues(err.Error())
		}
		return b, nil
	}}
}

// Check validates the result; an error is one issue at the reader's path.
func (c Confy[A]) Check(f func(A) error) Confy[A] {
	return Confy[A]{node: c.node, defs: c.defs, enc: c.enc, run: func(cur cursor) (A, Issues) {
		a, e := c.run(cur)
		if len(e) > 0 {
			return a, e
		}
		if err := f(a); err != nil {
			return a, issueAt(c.node, cur).issues(err.Error())
		}
		return a, nil
	}}
}

// issueAt is where MapErr and Check report: for the Field(key) shape (an
// Object with exactly one field and no unions) the issue lands on the key
// itself, otherwise on the reader's own cursor.
func issueAt(n Node, cur cursor) cursor {
	if o, ok := n.(Object); ok && len(o.Fields) == 1 && len(o.Unions) == 0 {
		return cur.down(o.Fields[0].Key)
	}
	return cur
}

// Default supplies v when the leaf is absent, and records the rendered
// default in the schema so Docs and Template show it. It is defined only on
// leaves (Scalar, Field, String, Int, …): after Map or With the encoder is
// gone and the default could not be rendered, so Default panics.
func (c Confy[A]) Default(v A) Confy[A] {
	leaf, rebuild, ok := singleLeaf(c.node)
	if !ok || c.enc == nil {
		panic("confy: Default is only defined on a leaf reader (Scalar, Field, String, Int, …)")
	}
	s := c.enc(v)
	leaf.Default = &s
	leaf.Optional = true
	node := rebuild(leaf)
	inner := c
	return Confy[A]{node: node, defs: c.defs, enc: c.enc, run: func(cur cursor) (A, Issues) {
		if !present(node, cur.v, inner.defs) {
			return v, nil
		}
		return inner.run(cur)
	}}
}

// Doc attaches documentation to the reader's schema node (for the Field(key)
// shape, to the field's leaf).
func (c Confy[A]) Doc(s string) Confy[A] {
	c.node = annotate(c.node, func(m *Meta) { m.Doc = s })
	return c
}

// Secret marks the value sensitive: issues never print it, Template prints
// a placeholder instead of the default, and Docs marks it [secret].
func (c Confy[A]) Secret() Confy[A] {
	inner := c
	c.node = annotate(c.node, func(m *Meta) { m.Secret = true })
	c.run = func(cur cursor) (A, Issues) {
		a, e := inner.run(cur)
		for i := range e {
			e[i].Secret = true
		}
		return a, e
	}
	return c
}

// Deprecated marks the reader deprecated with a reason; Docs and JSONSchema
// show it.
func (c Confy[A]) Deprecated(reason string) Confy[A] {
	if reason == "" {
		reason = "deprecated"
	}
	c.node = annotate(c.node, func(m *Meta) { m.Deprecated = reason })
	return c
}

// Struct starts a product for struct type T: Pure of the zero value, to be
// filled with At.
func Struct[T any]() Confy[T] {
	var z T
	return Pure(z)
}

// At fills one field of a struct product: sel picks the field, f reads it.
//
//	Struct[Server]().
//	    At(func(s *Server) *string { return &s.Host }, String("host")).
//	    At(func(s *Server) *int    { return &s.Port }, Int("port").Default(8080))
func (c Confy[T]) At[F any](sel func(*T) *F, f Confy[F]) Confy[T] {
	return c.With(f, func(t T, v F) T {
		*sel(&t) = v
		return t
	})
}

// Under reindexes: the inner reader runs at cursor.key and its schema is
// placed under key.
func Under[A any](key string, c Confy[A]) Confy[A] {
	return Confy[A]{
		node: Object{Fields: []Prop{{Key: key, Node: c.node, Optional: isOptional(c.node)}}},
		defs: c.defs,
		enc:  c.enc,
		run: func(cur cursor) (A, Issues) {
			return c.run(cur.down(key))
		},
	}
}

// Under is Under(key, c) as a method.
func (c Confy[A]) Under(key string) Confy[A] { return Under(key, c) }

// Scalar reads the cursor itself as a scalar with codec. Use it as the
// element of Seq/Dict or as the scalar arm of Shape.
func Scalar[A any](codec Codec[A]) Confy[A] {
	return Confy[A]{
		node: Leaf{Type: codec.Name},
		enc:  codec.Encode,
		run: func(cur cursor) (A, Issues) {
			var z A
			switch cur.v.Kind() {
			case KindAbsent:
				return z, cur.issues("missing")
			case KindScalar:
				a, err := codec.Decode(cur.v.Scalar())
				if err != nil {
					iss := cur.issue(fmt.Sprintf("expected %s, got %q: %v", codec.Name, fmt.Sprint(cur.v.Scalar()), err))
					iss.safe = "expected " + codec.Name + ": invalid value"
					return z, Issues{iss}
				}
				return a, nil
			default:
				return z, cur.rejected(fmt.Sprintf("expected %s, got %s", codec.Name, cur.v.Kind()))
			}
		},
	}
}

// Field reads key as a scalar with codec.
func Field[A any](key string, codec Codec[A]) Confy[A] { return Under(key, Scalar(codec)) }

// String reads key as a string.
func String(key string) Confy[string] { return Field(key, StringCodec) }

// Int reads key as an int.
func Int(key string) Confy[int] { return Field(key, IntCodec) }

// Int64 reads key as an int64.
func Int64(key string) Confy[int64] { return Field(key, Int64Codec) }

// Float reads key as a float64.
func Float(key string) Confy[float64] { return Field(key, FloatCodec) }

// Bool reads key as a bool.
func Bool(key string) Confy[bool] { return Field(key, BoolCodec) }

// Duration reads key as a time.Duration.
func Duration(key string) Confy[time.Duration] { return Field(key, DurationCodec) }

// Schema is the static projection.
func (c Confy[A]) Schema() Schema { return Schema{Root: c.node, Defs: c.defs} }

func isOptional(n Node) bool {
	switch x := n.(type) {
	case Leaf:
		return x.Optional
	case Array:
		return x.Optional
	case Table:
		return x.Optional
	case Union:
		return x.Optional
	case Empty:
		return true
	case Object:
		if len(x.Fields) == 0 && len(x.Unions) == 0 {
			return true
		}
		for _, f := range x.Fields {
			if !f.Optional {
				return false
			}
		}
		for _, u := range x.Unions {
			if !u.Optional {
				return false
			}
		}
		return true
	}
	return false
}
