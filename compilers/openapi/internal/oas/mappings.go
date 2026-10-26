package oas

import (
	"context"
	"encoding/json/jsontext"
	"slices"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// CheckFormat holds every mapping of the tree root to the Format section's
// rule that a key is a string, at the minor declared names, as Check holds
// objects. It reports each key that is no scalar, which the parser reads as
// "" (docs/parser-library.md#non-scalar-key), and each `<<` naming no
// mapping, which merges nothing (docs/parser-library.md#merge-keys). A rule
// judging a key's spelling still judges that "" (GitHub #1108).
//
// It reads the tree as written, values and extensions included, so a node an
// alias mounts twice draws one finding, placed at its pointer as the parser
// reads the tree. It reads Locate, Budget, ReadAt and Root of opts.
func CheckFormat(ctx context.Context, root *yaml.Node, declared string, opts Options) []ir.Diagnostic {
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
	// Placing a finding costs a pointer per node, and most documents break
	// neither rule, so a first walk only looks, and stops at the first break.
	look := nodeview.Walk{Nodes: budget, Visit: lookForBreak}.Run(ctx, nodeview.DocumentRoot(root))
	if !look.Stopped && !look.Cut {
		return nil
	}
	out := nodeview.Walk{Nodes: budget, Visit: f.visit}.Run(ctx, nodeview.DocumentRoot(root))
	if ctx.Err() != nil {
		return nil
	}
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

// lookForBreak stops a walk at the first mapping that breaks either rule
// CheckFormat holds it to.
func lookForBreak(n *yaml.Node, _ int) nodeview.Step {
	for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if nodeview.IsMergeKey(key) {
			if slices.ContainsFunc(mergeSources(val), notMapping) {
				return nodeview.Stop
			}
		} else if !scalarKey(key) {
			return nodeview.Stop
		}
	}
	return nodeview.Descend
}

// scalarKey reports whether key, followed through its alias, is a scalar, as
// the Format section requires a key to be.
func scalarKey(key *yaml.Node) bool {
	k := nodeview.Deref(key)
	return k == nil || k.Kind == yaml.ScalarNode
}

// mergeSources returns what the `<<` value val names: each element of a list
// written or aliased there, or val itself.
func mergeSources(val *yaml.Node) []*yaml.Node {
	if v := nodeview.Deref(val); v != nil && v.Kind == yaml.SequenceNode {
		return v.Content
	}
	return []*yaml.Node{val}
}

// notMapping reports whether src, followed through its alias, is no mapping,
// so a `<<` naming it merges nothing.
func notMapping(src *yaml.Node) bool {
	s := nodeview.Deref(src)
	return s == nil || s.Kind != yaml.MappingNode
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
		if !scalarKey(key) {
			f.report(ir.SeverityError, classType, ptr.AppendToken(name),
				"a mapping key must be a string in OpenAPI %s (Format), and this one is %s; it is read as the empty string",
				f.minor, kindName(nodeview.Deref(key)))
		}
	}
}

// merge checks the value of a `<<` key in the mapping at ptr, which the
// parser reads the members of as that mapping's own: a source, or a list of
// them. A list written in place is noted, so visit places its elements at ptr.
func (f *formatCheck) merge(val *yaml.Node, ptr jsontext.Pointer) {
	f.at[val] = ptr
	for _, src := range mergeSources(val) {
		if notMapping(src) {
			f.report(ir.SeverityError, classType, ptr,
				"a `<<` merge key must name a mapping or a list of mappings, and this one names %s; it merges nothing",
				kindName(nodeview.Deref(src)))
		}
	}
	if v := nodeview.Deref(val); v != nil && v == val && v.Kind == yaml.SequenceNode {
		f.merged[val] = true
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
