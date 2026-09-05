package confy

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

// docEntry is one line of Docs: a leaf path with its type and requirement.
type docEntry struct {
	path, typ, req, when string
	meta                 Meta
}

// Docs renders a flag.PrintDefaults-style listing of every leaf the schema
// reads, in declaration order, with defaults, union conditions, and
// annotations. Recursive definitions are listed once under "definitions".
func Docs(s Schema) string {
	var entries []docEntry
	visited := map[string]bool{}
	collectDocs(s.Root, s.Defs, "", "", &entries, visited)

	var b strings.Builder
	writeEntries(&b, entries)

	// Definitions referenced by Ref, each expanded once relative to itself.
	names := sortedDefNames(s.Defs)
	first := true
	for _, name := range names {
		if !visited[name] {
			continue
		}
		if first {
			b.WriteString("\ndefinitions:\n")
			first = false
		}
		var sub []docEntry
		collectDocs(s.Defs[name], s.Defs, name+":", "", &sub, map[string]bool{name: true})
		writeEntries(&b, sub)
	}
	return b.String()
}

// writeEntries aligns the entries as one tabwriter block, then interleaves
// each entry's doc lines (a line without tabs would otherwise end the
// alignment block).
func writeEntries(w *strings.Builder, entries []docEntry) {
	var block strings.Builder
	tw := tabwriter.NewWriter(&block, 2, 4, 2, ' ', 0)
	for _, e := range entries {
		line := e.path + "\t" + e.typ + "\t" + e.req
		if e.when != "" {
			line += "\t" + e.when
		}
		var marks []string
		if e.meta.Secret {
			marks = append(marks, "[secret]")
		}
		if e.meta.Deprecated != "" {
			marks = append(marks, "[deprecated: "+e.meta.Deprecated+"]")
		}
		if len(marks) > 0 {
			line += "\t" + strings.Join(marks, " ")
		}
		fmt.Fprintln(tw, line)
	}
	tw.Flush()
	lines := strings.Split(strings.TrimRight(block.String(), "\n"), "\n")
	for i, e := range entries {
		if i < len(lines) {
			w.WriteString(strings.TrimRight(lines[i], " ") + "\n")
		}
		if e.meta.Doc != "" {
			for _, l := range strings.Split(e.meta.Doc, "\n") {
				w.WriteString("    " + l + "\n")
			}
		}
	}
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	if strings.HasSuffix(prefix, ":") {
		return prefix + key
	}
	return prefix + "." + key
}

func collectDocs(n Node, defs map[string]Node, prefix, when string, out *[]docEntry, visited map[string]bool) {
	switch x := n.(type) {
	case Empty:
	case Leaf:
		req := "required"
		if x.Default != nil {
			if x.Secret {
				req = "default <secret>"
			} else {
				req = "default " + *x.Default
			}
		} else if x.Optional {
			req = "optional"
		}
		p := prefix
		if p == "" {
			p = "(value)"
		}
		*out = append(*out, docEntry{path: p, typ: x.Type, req: req, when: when, meta: x.Meta})
	case Object:
		for _, f := range x.Fields {
			node := f.Node
			cond := when
			if l, ok := node.(Leaf); ok {
				if f.Optional && !l.Optional {
					l.Optional = true
					node = l
				}
			} else if f.Optional && !isOptionalNode(node) {
				// An optional group: what is inside is required only once
				// the group is present.
				cond = andWhen(when, "if "+joinPath(prefix, f.Key)+" is set")
			}
			collectDocs(node, defs, joinPath(prefix, f.Key), cond, out, visited)
		}
		for _, u := range x.Unions {
			collectDocs(u, defs, prefix, when, out, visited)
		}
	case Array:
		cond := when
		if x.Optional {
			cond = andWhen(when, "if "+orValue(prefix)+" is set")
		}
		collectDocs(x.Elem, defs, prefix+"[]", cond, out, visited)
	case Table:
		cond := when
		if x.Optional {
			cond = andWhen(when, "if "+orValue(prefix)+" is set")
		}
		collectDocs(x.Elem, defs, joinPath(prefix, "<key>"), cond, out, visited)
	case Union:
		switch x.Discriminant.Kind {
		case ByTag:
			tags := make([]string, len(x.Arms))
			for i, a := range x.Arms {
				tags[i] = a.Tag
			}
			tagPath := joinPath(prefix, x.Discriminant.Key)
			req := "required"
			if x.Optional {
				req = "optional"
			}
			*out = append(*out, docEntry{path: tagPath, typ: "one of: " + strings.Join(tags, ", "), req: req, when: when, meta: x.Meta})
			for _, a := range x.Arms {
				cond := "when " + tagPath + "=" + a.Tag
				if when != "" {
					cond = when + ", " + cond
				}
				collectDocs(a.Node, defs, prefix, cond, out, visited)
			}
		case ByKind:
			start := len(*out)
			for _, a := range x.Arms {
				collectDocs(a.Node, defs, prefix, andWhen(when, "when "+orValue(prefix)+" is a "+a.Tag), out, visited)
			}
			// The union's own doc goes on its first entry.
			if x.Meta.Doc != "" && len(*out) > start && (*out)[start].meta.Doc == "" {
				(*out)[start].meta.Doc = x.Meta.Doc
			}
		}
	case Ref:
		visited[x.Name] = true
		p := prefix
		if p == "" {
			p = "(value)"
		}
		*out = append(*out, docEntry{path: p, typ: "→ " + x.Name, req: "", when: when, meta: x.Meta})
	}
}

func andWhen(when, cond string) string {
	if when == "" {
		return cond
	}
	return when + ", " + cond
}

// isOptionalNode reports whether the node already records its own
// optionality (so the enclosing field need not add a condition).
func isOptionalNode(n Node) bool {
	switch x := n.(type) {
	case Leaf:
		return x.Optional
	case Array:
		return x.Optional
	case Table:
		return x.Optional
	case Union:
		return x.Optional
	}
	return false
}

func orValue(prefix string) string {
	if prefix == "" {
		return "(value)"
	}
	return prefix
}

func sortedDefNames(defs map[string]Node) []string {
	names := make([]string, 0, len(defs))
	for k := range defs {
		names = append(names, k)
	}
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
