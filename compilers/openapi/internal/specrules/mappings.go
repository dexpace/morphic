// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules

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
// objects. It reports each key that is no scalar, whose entry every reader
// leaves out (docs/parser-library.md#non-scalar-key), at the key's line and
// column, and each `<<` naming no mapping (docs/parser-library.md#merge-keys).
//
// It reads the tree as written, values and extensions included, so a node an
// alias mounts twice draws one finding, placed at its pointer as the parser
// reads the tree. It reads Locate, Budget, ReadAt and Root of opts.
func CheckFormat(ctx context.Context, root *yaml.Node, declared string, opts Options) []ir.Diagnostic {
	minor, ok := ParseMinor(declared)
	if !ok {
		return nil
	}
	formatCheckValue := formatCheck{minor: minor, at: map[*yaml.Node]jsontext.Pointer{}, merged: map[*yaml.Node]bool{},
		locate: opts.Locate, declared: declared, readAt: opts.ReadAt, rootAt: opts.Root, said: map[ir.DiagnosticKey]saying{}}
	if formatCheckValue.locate == nil {
		formatCheckValue.locate = func(jsontext.Pointer) ir.Provenance { return ir.Provenance{} }
	}
	// Placing a finding costs a pointer per node, and most documents break
	// neither rule, so a first walk only looks, and stops at the first break.
	if !BreaksFormat(ctx, root, opts.Budget.Cap()) {
		return nil
	}
	out := nodeview.Walk{Nodes: opts.Budget.Cap(), Visit: formatCheckValue.visit}.Run(ctx, nodeview.DocumentRoot(root))
	if ctx.Err() != nil {
		return nil
	}
	if out.Cut {
		formatCheckValue.diags = append(formatCheckValue.diags, diag.Newf(ir.SeverityError, diag.BudgetExceeded, formatCheckValue.locate(""),
			"the OpenAPI %s check of the document's keys stopped at its work budget; what lies past it is unchecked", minor))
	}
	return formatCheckValue.diags
}

// BreaksFormat reports whether some mapping of the tree root breaks a rule
// CheckFormat holds it to, a key that is no scalar or a `<<` naming no
// mapping, in a walk of at most nodes nodes, which it stops at the first: one
// its bound or ctx cuts short says it may. CheckFormat then spells a pointer
// for every node of the tree, so the load phase asks it of a document whose
// pointers it would total by what a compile names
// (docs/compilers/openapi.md#source-budgets).
func BreaksFormat(ctx context.Context, root *yaml.Node, nodes int) bool {
	look := nodeview.Walk{Nodes: nodes, Visit: lookForBreak}.Run(ctx, nodeview.DocumentRoot(root))
	return look.Stopped || look.Cut
}

// formatCheck is one CheckFormat in progress: the pointer each node to visit is
// at, and the lists a `<<` names, whose mappings merge where it is written.
type formatCheck struct {
	minor  Minor
	at     map[*yaml.Node]jsontext.Pointer
	merged map[*yaml.Node]bool
	reporter
}

// visit checks node, a mapping's pairs or a merged list's elements, and notes
// where each of its children is.
func (f *formatCheck) visit(node *yaml.Node, _ int) nodeview.Step {
	ptr := f.at[node]
	delete(f.at, node)
	switch node.Kind {
	case yaml.MappingNode:
		f.mapping(node, ptr)
	case yaml.SequenceNode:
		for i, item := range node.Content {
			f.at[item] = ptr.AppendToken(strconv.Itoa(i))
			if f.merged[node] {
				f.at[item] = ptr
			}
		}
		delete(f.merged, node)
	default:
	}
	return nodeview.Descend
}

// lookForBreak stops a walk at the first mapping that breaks either rule
// CheckFormat holds it to.
func lookForBreak(node *yaml.Node, _ int) nodeview.Step {
	for i := 0; node.Kind == yaml.MappingNode && i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		if nodeview.IsMergeKey(key) {
			if slices.ContainsFunc(nodeview.MergeSources(val), nodeview.NotMapping) {
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
	node := nodeview.Deref(key)
	return node == nil || node.Kind == yaml.ScalarNode
}

// mapping checks the pairs mapping node, at ptr, writes itself.
func (f *formatCheck) mapping(node *yaml.Node, ptr jsontext.Pointer) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		if nodeview.IsMergeKey(key) {
			f.merge(val, ptr)
			continue
		}
		name, own := nodeview.KeyOf(key)
		if own {
			f.at[key], f.at[val] = ptr.AppendToken(name), ptr.AppendToken(name)
			continue
		}
		// The entry is left out, so it has no pointer of its own: two such keys of
		// one mapping are told apart by where each is written.
		f.at[key], f.at[val] = ptr, ptr
		diagnostic := diag.Newf(ir.SeverityError, diag.Validation+"/"+classType, f.locate(ptr),
			"a mapping key must be a string in OpenAPI %s (Format), and this one is %s; the entry is read as left out",
			f.minor, kindName(nodeview.Deref(key)))
		diagnostic.Provenance.Position = ir.Position{Line: key.Line, Column: key.Column}
		f.add(diagnostic, ptr)
	}
}

// merge checks the value of a `<<` key in the mapping at ptr, which the
// parser reads the members of as that mapping's own: a source, or a list of
// them. A list written in place is noted, so visit places its elements at ptr.
func (f *formatCheck) merge(val *yaml.Node, ptr jsontext.Pointer) {
	f.at[val] = ptr
	for _, src := range nodeview.MergeSources(val) {
		if nodeview.NotMapping(src) {
			f.report(ir.SeverityError, classType, ptr,
				"a `<<` merge key must name a mapping or a list of mappings, and this one names %s; it merges nothing",
				kindName(nodeview.Deref(src)))
		}
	}
	if node := nodeview.Deref(val); node != nil && node == val && node.Kind == yaml.SequenceNode {
		f.merged[val] = true
	}
}

// kindName names the kind of a node with its article.
func kindName(node *yaml.Node) string {
	switch {
	case node == nil:
		return "nothing"
	case node.Kind == yaml.MappingNode:
		return "a mapping"
	case node.Kind == yaml.SequenceNode:
		return "a list"
	default:
		return "a scalar"
	}
}

// CheckRepeatedKeys holds every mapping of the tree root, as written, values
// and extensions included, to the Format section's rule that a key is written
// once, at the minor declared names: each key a mapping writes again is a
// warning at the repeat, its pointer and the key's line and column, since only
// the last copy is read. It reads Locate and Budget of opts; a walk its budget
// cuts says nothing of what lies past it.
func CheckRepeatedKeys(ctx context.Context, root *yaml.Node, declared string, opts Options) []ir.Diagnostic {
	minor, ok := ParseMinor(declared)
	if !ok {
		return nil
	}
	locate := opts.Locate
	if locate == nil {
		locate = func(jsontext.Pointer) ir.Provenance { return ir.Provenance{} }
	}
	at := map[*yaml.Node]jsontext.Pointer{}
	var diags []ir.Diagnostic
	nodeview.Walk{Nodes: opts.Budget.Cap(), Visit: func(node *yaml.Node, _ int) nodeview.Step {
		ptr := at[node]
		delete(at, node)
		switch node.Kind {
		case yaml.MappingNode:
			diags = append(diags, repeatedIn(node, ptr, at, locate, minor)...)
		case yaml.SequenceNode:
			for i, item := range node.Content {
				at[item] = itemPointer(ptr, i)
			}
		default:
		}
		return nodeview.Descend
	}}.Run(ctx, nodeview.DocumentRoot(root))
	if ctx.Err() != nil {
		return nil
	}
	return diags
}

// repeatedIn notes where each child of the mapping node at ptr is, and reports
// each key node writes a second time, at that copy.
func repeatedIn(node *yaml.Node, ptr jsontext.Pointer, at map[*yaml.Node]jsontext.Pointer, locate Locator, minor Minor,
) []ir.Diagnostic {
	copies := map[string]int{}
	var diags []ir.Diagnostic
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, own := nodeview.KeyOf(node.Content[i])
		if !own {
			continue
		}
		at[node.Content[i+1]] = ptr.AppendToken(key)
		if copies[key]++; copies[key] != 2 {
			continue
		}
		diagnostic := diag.Newf(ir.SeverityWarning, diag.Validation+"/"+classDuplicate, locate(ptr.AppendToken(key)),
			"a mapping writes the key %q more than once, and only the last copy is read in OpenAPI %s (Format)", key, minor)
		diagnostic.Provenance.Position = ir.Position{Line: node.Content[i].Line, Column: node.Content[i].Column}
		diags = append(diags, diagnostic)
	}
	return diags
}
