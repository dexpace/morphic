package oas

import (
	"encoding/json/jsontext"
	"strconv"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// CheckFormat holds every mapping of the tree root to the Format section's
// rule that a key is a string, at the minor declared names, as Check holds
// objects. It reports each key that is no scalar, which the parser reads as
// the empty string (docs/parser-library.md#non-scalar-key), and each `<<`
// naming something that is not a mapping, from which the parser merges
// nothing (docs/parser-library.md#merge-keys).
//
// It reads the tree as written, values and extensions included, so a node an
// alias mounts twice draws one finding, placed at its pointer as the parser
// reads the tree. It reads Locate, Budget, ReadAt and Root of opts.
func CheckFormat(root *yaml.Node, declared string, opts Options) []ir.Diagnostic {
	minor, ok := ParseMinor(declared)
	if !ok {
		return nil
	}
	f := formatCheck{minor: minor, at: map[*yaml.Node]jsontext.Pointer{}, merged: map[*yaml.Node]bool{},
		locate: opts.Locate, declared: declared, readAt: opts.ReadAt, rootAt: opts.Root, said: map[ir.Diagnostic]bool{}}
	if f.locate == nil {
		f.locate = func(jsontext.Pointer) ir.Provenance { return ir.Provenance{} }
	}
	budget := opts.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	out := nodeview.Walk{Nodes: budget, Visit: f.visit}.Run(nodeview.DocumentRoot(root))
	if out.Cut {
		f.diags = append(f.diags, diag.Newf(ir.SeverityError, diag.BudgetExceeded, f.locate(""),
			"the OpenAPI %s check of the document's keys stopped at its work budget; what lies past it is unchecked", minor))
	}
	return f.diags
}

// formatCheck is one CheckFormat in progress: the pointer each node to visit is
// at, and the lists a `<<` names, whose mappings merge where it is written.
type formatCheck struct {
	minor  Minor
	at     map[*yaml.Node]jsontext.Pointer
	merged map[*yaml.Node]bool
	reporter
}

// visit checks n, a mapping's pairs or a merged list's elements, and notes
// where each of its children is.
func (f *formatCheck) visit(n *yaml.Node, _ int) nodeview.Step {
	ptr := f.at[n]
	delete(f.at, n)
	switch n.Kind {
	case yaml.MappingNode:
		f.mapping(n, ptr)
	case yaml.SequenceNode:
		for i, item := range n.Content {
			f.at[item] = ptr.AppendToken(strconv.Itoa(i))
			if f.merged[n] {
				f.at[item] = ptr
			}
		}
		delete(f.merged, n)
	default:
	}
	return nodeview.Descend
}

// mapping checks the pairs mapping n, at ptr, writes itself.
func (f *formatCheck) mapping(n *yaml.Node, ptr jsontext.Pointer) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if nodeview.IsMergeKey(key) {
			f.merge(val, ptr)
			continue
		}
		name, _ := nodeview.KeyOf(key)
		f.at[key], f.at[val] = ptr.AppendToken(name), ptr.AppendToken(name)
		if k := nodeview.Deref(key); k != nil && k.Kind != yaml.ScalarNode {
			f.report(ir.SeverityError, classType, ptr.AppendToken(name),
				"a mapping key must be a string in OpenAPI %s (Format), and this one is %s; it is read as the empty string",
				f.minor, kindName(k))
		}
	}
}

// merge checks the value of a `<<` key in the mapping at ptr, which the
// parser reads the members of as that mapping's own: a source, or a list of
// them. A list written in place is noted, so visit places its elements at ptr.
func (f *formatCheck) merge(val *yaml.Node, ptr jsontext.Pointer) {
	f.at[val] = ptr
	v := nodeview.Deref(val)
	if v == nil || v.Kind != yaml.SequenceNode {
		f.source(val, ptr)
		return
	}
	for _, item := range v.Content {
		f.source(item, ptr)
	}
	if v == val {
		f.merged[val] = true
	}
}

// source checks one `<<` merge source at ptr: a mapping, or an alias to one.
func (f *formatCheck) source(src *yaml.Node, ptr jsontext.Pointer) {
	if s := nodeview.Deref(src); s == nil || s.Kind != yaml.MappingNode {
		f.report(ir.SeverityError, classType, ptr,
			"a `<<` merge key must name a mapping or a list of mappings, and this one names %s; it merges nothing",
			kindName(s))
	}
}

// kindName names the kind of a node with its article.
func kindName(n *yaml.Node) string {
	switch {
	case n == nil:
		return "nothing"
	case n.Kind == yaml.MappingNode:
		return "a mapping"
	case n.Kind == yaml.SequenceNode:
		return "a list"
	default:
		return "a scalar"
	}
}
