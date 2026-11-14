// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// requestTargets are the pagination targets naming a value the request carries.
func requestTargets() []lowering.ExtensionTarget {
	return []lowering.ExtensionTarget{
		lowering.TargetPaginationCursor, lowering.TargetPaginationOffset, lowering.TargetPaginationPage,
		lowering.TargetPaginationLimit,
	}
}

// responseTargets are the pagination targets naming a value a page carries.
func responseTargets() []lowering.ExtensionTarget {
	return []lowering.ExtensionTarget{
		lowering.TargetPaginationItems, lowering.TargetPaginationNextCursor, lowering.TargetPaginationNextLink,
		lowering.TargetPaginationTotalCount, lowering.TargetPaginationTotalPages,
	}
}

// Promote fills the pagination each operation of w, the document, declares
// through the extensions the promotion policy maps to it, where the operation
// holds none (docs/compilers/openapi.md#extension-pagination). A
// pagination is filled whole or not at all: one whose values do not all
// resolve, or which states no way to reach the next page, is reported at the
// operation and leaves the field unset. c is the source's context.
//
// stated holds each operation it reads whose extensions speak of a pagination,
// filled or not: those a step inferring one leaves alone, by that rule's
// precedence, without reading an extension again.
func Promote(c lowering.Ctx, w *lowering.Whole) (stated map[ir.OpID]bool, diags []ir.Diagnostic) {
	if w == nil {
		return nil, nil
	}
	pg := pager{c: c, p: c.Promoter(), w: w}
	if !pg.maps() {
		return nil, nil
	}
	stated = map[ir.OpID]bool{}
	for _, op := range w.Ops {
		if op.Pagination != nil {
			continue
		}
		spoke, opDiags := pg.operation(op)
		diags = append(diags, opDiags...)
		if spoke {
			stated[op.ID] = true
		}
	}
	return stated, diags
}

// pager is what one Promote reads its operations against: the document, w,
// and reach, the models reaching each marking key (pager.reaching).
type pager struct {
	c     lowering.Ctx
	p     lowering.Promoter
	w     *lowering.Whole
	reach map[string]map[ir.TypeID]bool
}

// maps reports whether the policy reads any pagination target.
func (pg *pager) maps() bool {
	return slices.ContainsFunc(slices.Concat(requestTargets(), responseTargets()), pg.p.Maps)
}

// pageFields is what an operation's extensions state of its pagination, each
// field by the target naming it, the nodes whose keys stated them, and whether
// a source's key is written where it is read, whatever it states.
type pageFields struct {
	inputs  map[lowering.ExtensionTarget]*ir.InputPath
	outputs map[lowering.ExtensionTarget]*ir.PropPath
	read    []*ir.Provenance
	written bool
}

// operation fills op's pagination from what its extensions state, if they
// state any, and reports whether they speak of it at all: whether a source's
// key is written where it is read, whatever its value, filling a pagination,
// failing to or stating nothing.
func (pg *pager) operation(op *ir.Operation) (bool, []ir.Diagnostic) {
	fields := pageFields{inputs: map[lowering.ExtensionTarget]*ir.InputPath{},
		outputs: map[lowering.ExtensionTarget]*ir.PropPath{}}
	var diags []ir.Diagnostic
	for _, target := range requestTargets() {
		in, failed, readDiags := pg.input(op, target)
		diags = append(diags, readDiags...)
		fields.written = fields.written || in.written
		if failed {
			return fields.written, diags
		}
		if in.value != nil {
			fields.inputs[target], fields.read = in.value, append(fields.read, in.read...)
		}
	}
	for _, target := range responseTargets() {
		out, failed, readDiags := pg.output(op, target)
		diags = append(diags, readDiags...)
		fields.written = fields.written || out.written
		if failed {
			return fields.written, diags
		}
		if out.value != nil {
			fields.outputs[target], fields.read = out.value, append(fields.read, out.read...)
		}
	}
	if len(fields.inputs) == 0 && len(fields.outputs) == 0 {
		return fields.written, diags
	}
	walk, reason := assemble(fields)
	if walk == nil {
		diags = append(diags, pg.report(op, "the pagination the policy reads from its extensions %s, so it fills none", reason))
		return fields.written, diags
	}
	op.Pagination = walk
	for _, prov := range fields.read {
		prov.Inferred = ir.JoinInferred(prov.Inferred, lowering.ExtensionPromotionHeuristic)
	}
	return fields.written, diags
}

// report is a finding at op about what its extensions state of its pagination.
func (pg *pager) report(op *ir.Operation, format string, args ...any) ir.Diagnostic {
	return diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, op.Provenance, "operation %q: "+format,
		append([]any{op.ID}, args...)...)
}

// assemble builds the pagination fields state, and returns why it builds none:
// more than one input continuing the walk, a cursor beside no next cursor or a
// next cursor beside no cursor, an offset or a page number beside no items,
// which a pager counts to advance an offset and to end either walk
// (docs/emitters/operations/pagination.md), or nothing reaching the next page
// at all.
func assemble(f pageFields) (*ir.Pagination, string) {
	var continues []lowering.ExtensionTarget
	for _, t := range []lowering.ExtensionTarget{lowering.TargetPaginationCursor, lowering.TargetPaginationOffset,
		lowering.TargetPaginationPage} {
		if f.inputs[t] != nil {
			continues = append(continues, t)
		}
	}
	_, cursor := f.inputs[lowering.TargetPaginationCursor]
	_, next := f.outputs[lowering.TargetPaginationNextCursor]
	_, link := f.outputs[lowering.TargetPaginationNextLink]
	_, items := f.outputs[lowering.TargetPaginationItems]
	switch {
	case len(continues) > 1:
		return nil, "names more than one input continuing it (" + joinTargets(continues) + ")"
	case cursor && !next:
		return nil, "sends a cursor and names no next cursor to send"
	case !cursor && next:
		return nil, "names a next cursor and no input to send it in"
	case len(continues) == 1 && !cursor && !items:
		return nil, "counts its walk by " + string(continues[0]) + " and names no items to count"
	case len(continues) == 0 && !link:
		return nil, "names neither an input continuing it nor a next link"
	}
	walk := &ir.Pagination{
		Strategy:   ir.PageStrategyNextLink,
		Inferred:   true,
		InputLimit: f.inputs[lowering.TargetPaginationLimit],
		Items:      f.outputs[lowering.TargetPaginationItems],
		NextCursor: f.outputs[lowering.TargetPaginationNextCursor],
		NextLink:   f.outputs[lowering.TargetPaginationNextLink],
		TotalCount: f.outputs[lowering.TargetPaginationTotalCount],
		TotalPages: f.outputs[lowering.TargetPaginationTotalPages],
	}
	if len(continues) == 1 {
		walk.Strategy, walk.InputCursor = strategyOf(continues[0]), f.inputs[continues[0]]
	}
	return walk, ""
}

// strategyOf is the strategy of a walk an input of target continues.
func strategyOf(target lowering.ExtensionTarget) ir.PageStrategy {
	switch target {
	case lowering.TargetPaginationOffset:
		return ir.PageStrategyOffset
	case lowering.TargetPaginationPage:
		return ir.PageStrategyPage
	default:
		return ir.PageStrategyCursor
	}
}

// joinTargets spells targets for a report.
func joinTargets(targets []lowering.ExtensionTarget) string {
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = string(t)
	}
	return strings.Join(names, ", ")
}
