// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package whole

import "github.com/dexpace/morphic/ir"

// operations returns every operation svc holds, those of nested groups
// included to ir.MaxGroupDepth, as pointers into the groups holding them: what
// a step reading the document once it is whole writes an operation through.
// New is its one caller, so no step lists the operations for itself.
func operations(svc *ir.Service) []*ir.Operation {
	type level struct {
		groups []ir.OperationGroup
		depth  int
	}
	var ops []*ir.Operation
	stack := []level{{groups: svc.Groups}}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top.depth > ir.MaxGroupDepth {
			continue
		}
		for gi := range top.groups {
			g := &top.groups[gi]
			for oi := range g.Operations {
				ops = append(ops, &g.Operations[oi])
			}
			stack = append(stack, level{groups: g.Groups, depth: top.depth + 1})
		}
	}
	return ops
}
