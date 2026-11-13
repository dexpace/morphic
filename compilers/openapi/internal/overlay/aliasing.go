// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"fmt"
	"math"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// aliasing is what the tree's YAML aliases and `<<` merge keys make of an
// overlay. The Overlay Specification applies one to the document's JSON value,
// where an alias is a copy of what it names and a merged member a member, but
// the actions apply to the tree as written: a selector reads neither through,
// and a write inside an anchored node reaches every alias of it. Each is
// reported rather than left silent (GitHub #1834).
type aliasing struct {
	// named holds each node an alias in the tree names, with how many do.
	named map[*yaml.Node]int
	// merges reports that the tree writes a `<<` merge key.
	merges bool
}

// readAliasing reads the aliases and merge keys of the tree under root, and
// reports whether ctx let it finish. It reads the whole tree, whose size the
// node budget and the loader's refusals bound.
func readAliasing(ctx context.Context, root *yaml.Node) (aliasing, bool) {
	var found aliasing
	out := nodeview.Walk{Nodes: math.MaxInt, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		if n.Kind == yaml.AliasNode && n.Alias != nil {
			if found.named == nil {
				found.named = map[*yaml.Node]int{}
			}
			found.named[n.Alias]++
		}
		for i := 0; n.Kind == yaml.MappingNode && i < len(n.Content); i += 2 {
			found.merges = found.merges || nodeview.IsMergeKey(n.Content[i])
		}
		return nodeview.Descend
	}}.Run(ctx, nodeview.DocumentRoot(root))
	return found, !out.Cut
}

// any reports whether the tree writes an alias or a merge key at all.
func (a aliasing) any() bool { return len(a.named) > 0 || a.merges }

// aliasesNotRead is the warning that the source writes aliases or merge keys,
// which no selector of the overlay at reads through.
func aliasesNotRead(at ir.Provenance) ir.Diagnostic {
	return diag.Newf(ir.SeverityWarning, diag.OverlayAction, at,
		"the source writes YAML aliases or `<<` merge keys, which the overlay's selectors do not read through: "+
			"a selector selects what the source writes, not the value an alias or a merge stands for (GitHub #1834)")
}

// writeReachesAliases is the warning that a, writing or removing inside
// anchor, changes what each of the aliases naming it stands for too.
func writeReachesAliases(a action, anchor *yaml.Node, aliases int) ir.Diagnostic {
	named := "1 YAML alias names"
	if aliases != 1 {
		named = fmt.Sprintf("%d YAML aliases name", aliases)
	}
	return diag.Newf(ir.SeverityWarning, diag.OverlayAction, a.overlay,
		"%s changes what the node anchored at %s holds, which %s, so the change reaches each of them too (GitHub #1834)",
		a.label(), where(anchor), named)
}

// mergeKeyWritten is the warning that a wrote a `<<` merge key into an object
// the tree holds, where it merges as written rather than as the members it
// names.
func mergeKeyWritten(a action) ir.Diagnostic {
	return diag.Newf(ir.SeverityWarning, diag.OverlayAction, a.overlay,
		"%s writes a `<<` merge key into an object, where it merges as written: the members it names replace none "+
			"the object holds, as they would in the value the update stands for (GitHub #1834)", a.label())
}
