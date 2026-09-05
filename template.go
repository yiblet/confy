package confy

import (
	"strings"
)

// Template renders a YAML skeleton of the schema: every leaf present with
// its default or a <type> placeholder, docs as comments, union arms grouped
// under "when" comments, one example element per list, one example key per
// dict, and recursive definitions expanded once.
func Template(s Schema) string {
	var b strings.Builder
	r := &tmpl{b: &b, defs: s.Defs, expanding: map[string]bool{}}
	r.node(s.Root, 0, "")
	return b.String()
}

type tmpl struct {
	b         *strings.Builder
	defs      map[string]Node
	expanding map[string]bool
}

func (r *tmpl) line(indent int, s string) {
	r.b.WriteString(strings.Repeat("  ", indent))
	r.b.WriteString(s)
	r.b.WriteByte('\n')
}

func (r *tmpl) comment(indent int, m Meta) {
	if m.Doc != "" {
		for _, l := range strings.Split(m.Doc, "\n") {
			r.line(indent, "# "+l)
		}
	}
	if m.Deprecated != "" {
		r.line(indent, "# deprecated: "+m.Deprecated)
	}
}

// scalarText is what a leaf looks like inline.
func scalarText(l Leaf) string {
	if l.Secret {
		return "<secret>"
	}
	if l.Default != nil {
		return *l.Default
	}
	return "<" + l.Type + ">"
}

// node renders n at indent. keyLine is the "key:" prefix that a mapping
// parent already wrote for this node, or "" at the root / inside sequences.
func (r *tmpl) node(n Node, indent int, keyLine string) {
	n = r.deref(n, indent, keyLine)
	if n == nil {
		return
	}
	switch x := n.(type) {
	case Empty:
		if keyLine != "" {
			r.line(indent, keyLine+" {}")
		}
	case Leaf:
		if keyLine != "" {
			r.line(indent, keyLine+" "+scalarText(x))
		} else {
			r.line(indent, scalarText(x))
		}
	case Object:
		if keyLine != "" {
			r.line(indent, keyLine)
			indent++
		}
		r.fields(x, indent)
	case Array:
		if keyLine != "" {
			r.line(indent, keyLine)
			indent++
		}
		r.seqItem(x.Elem, indent)
	case Table:
		if keyLine != "" {
			r.line(indent, keyLine)
			indent++
		}
		r.comment(indent, metaOf(x.Elem))
		r.node(x.Elem, indent, "<key>:")
	case Union:
		if x.Discriminant.Kind == ByKind {
			r.line(indent, "# one of the following forms:")
			for _, a := range x.Arms {
				r.line(indent, "# — as a "+a.Tag+":")
				r.node(a.Node, indent, keyLine)
			}
			return
		}
		if keyLine != "" {
			r.line(indent, keyLine)
			indent++
		}
		r.fields(Object{Unions: []Union{x}}, indent)
	}
}

func (r *tmpl) deref(n Node, indent int, keyLine string) Node {
	ref, ok := n.(Ref)
	if !ok {
		return n
	}
	if r.expanding[ref.Name] {
		if keyLine != "" {
			r.line(indent, keyLine+" # … recursive: "+ref.Name)
		} else {
			r.line(indent, "# … recursive: "+ref.Name)
		}
		return nil
	}
	d, ok := r.defs[ref.Name]
	if !ok {
		r.line(indent, keyLine+" <"+ref.Name+">")
		return nil
	}
	r.expanding[ref.Name] = true
	r.node(d, indent, keyLine)
	delete(r.expanding, ref.Name)
	return nil
}

func (r *tmpl) fields(o Object, indent int) {
	for _, f := range o.Fields {
		r.comment(indent, metaOf(f.Node))
		r.node(f.Node, indent, f.Key+":")
	}
	for _, u := range o.Unions {
		switch u.Discriminant.Kind {
		case ByTag:
			tags := make([]string, len(u.Arms))
			for i, a := range u.Arms {
				tags[i] = a.Tag
			}
			r.comment(indent, u.Meta)
			r.line(indent, u.Discriminant.Key+": <"+strings.Join(tags, "|")+">")
			for _, a := range u.Arms {
				if _, empty := a.Node.(Empty); empty {
					r.line(indent, "# when "+u.Discriminant.Key+" = "+a.Tag+": (no further keys)")
					continue
				}
				r.line(indent, "# when "+u.Discriminant.Key+" = "+a.Tag+":")
				r.node(a.Node, indent, "")
			}
		case ByKind:
			r.node(u, indent, "")
		}
	}
}

// seqItem renders one example element of a sequence at indent.
func (r *tmpl) seqItem(elem Node, indent int) {
	if ref, ok := elem.(Ref); ok {
		if r.expanding[ref.Name] {
			r.line(indent, "- # … recursive: "+ref.Name)
			return
		}
		d, ok := r.defs[ref.Name]
		if !ok {
			r.line(indent, "- <"+ref.Name+">")
			return
		}
		r.expanding[ref.Name] = true
		defer delete(r.expanding, ref.Name)
		elem = d
	}
	switch x := elem.(type) {
	case Leaf:
		r.comment(indent, x.Meta)
		r.line(indent, "- "+scalarText(x))
	case Object:
		// First field on the dash line, rest indented under it.
		var sub strings.Builder
		inner := &tmpl{b: &sub, defs: r.defs, expanding: r.expanding}
		inner.fields(x, 0)
		lines := strings.Split(strings.TrimRight(sub.String(), "\n"), "\n")
		for i, l := range lines {
			if i == 0 {
				r.line(indent, "- "+l)
			} else {
				r.line(indent+1, l)
			}
		}
	default:
		r.line(indent, "-")
		r.node(elem, indent+1, "")
	}
}
