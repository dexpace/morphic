// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// service fills the replacements of every operation svc holds, nested groups
// included to ir.MaxGroupDepth, of its parameters and of its responses' headers.
func (r *resolver) service(svc *ir.Service) []ir.Diagnostic {
	ops := operations(svc)
	byName := map[string][]ir.OpID{}
	for _, op := range ops {
		if name := op.Name.Source; name != "" {
			byName[name] = append(byName[name], op.ID)
		}
	}
	operation := func(name string) (ir.EntityRef, bool) {
		if found := byName[name]; len(found) == 1 {
			return ir.EntityRef{Operation: found[0]}, true
		}
		return ir.EntityRef{}, false
	}
	var diags []ir.Diagnostic
	for _, op := range ops {
		diags = append(diags, r.fill(op.Unmodeled, op.Deprecation, &op.Provenance, ir.EntityRef{Operation: op.ID},
			"operation", operation)...)
		diags = append(diags, r.params(op)...)
		for i := range op.Responses {
			diags = append(diags, r.headers(op.Responses[i].Headers)...)
		}
		for i := range op.Errors {
			diags = append(diags, r.headers(op.Errors[i].Headers)...)
		}
		if op.Default != nil {
			diags = append(diags, r.headers(op.Default.Headers)...)
		}
	}
	return diags
}

// params fills each parameter's replacements, among op's parameters.
func (r *resolver) params(op *ir.Operation) []ir.Diagnostic {
	ix := lowering.IndexParams(op)
	param := func(name string) (ir.EntityRef, bool) {
		id, matched := ix.Named(name)
		return ir.EntityRef{Param: id}, matched == 1
	}
	diags := make([]ir.Diagnostic, 0, len(op.Params))
	for i := range op.Params {
		p := &op.Params[i]
		diags = append(diags, r.fill(p.Unmodeled, p.Deprecation, &p.Provenance, ir.EntityRef{Param: p.ID}, "parameter", param)...)
	}
	return diags
}

// operations returns every operation svc holds, nested groups included, to
// ir.MaxGroupDepth.
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
