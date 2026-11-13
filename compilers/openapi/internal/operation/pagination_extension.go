// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

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

// PromotePagination fills the pagination each operation of svc declares through
// the extensions the promotion policy maps to it, reg being whole, where the
// operation holds none (docs/compilers/openapi.md#extension-pagination). A
// pagination is filled whole or not at all: one whose values do not all
// resolve, or which states no way to reach the next page, is reported at the
// operation and leaves the field unset. c is the source's context.
func PromotePagination(c lowering.Ctx, svc *ir.Service, reg ir.TypeRegistry) []ir.Diagnostic {
	p := c.Promoter()
	pg := pager{c: c, p: p, lk: newLookups(reg), flat: map[ir.TypeID][]*ir.Property{}}
	if !pg.maps() {
		return nil
	}
	var diags []ir.Diagnostic
	for _, op := range lowering.Operations(svc) {
		if op.Pagination == nil {
			diags = append(diags, pg.operation(op)...)
		}
	}
	return diags
}

// pager is what one PromotePagination reads its operations against. flat holds
// each model's members in layout order, read once for every operation that
// walks through it, and reach the models reaching each marking key
// (pager.reaching).
type pager struct {
	c     lowering.Ctx
	p     lowering.Promoter
	lk    *lookups
	flat  map[ir.TypeID][]*ir.Property
	reach map[string]map[ir.TypeID]bool
}

// maps reports whether the policy reads any pagination target.
func (pg *pager) maps() bool {
	return slices.ContainsFunc(slices.Concat(requestTargets(), responseTargets()), pg.p.Maps)
}

// pageFields is what an operation's extensions state of its pagination, each
// field by the target naming it, and the nodes whose keys stated them.
type pageFields struct {
	inputs  map[lowering.ExtensionTarget]*ir.InputPath
	outputs map[lowering.ExtensionTarget]*ir.PropPath
	read    []*ir.Provenance
}

// operation fills op's pagination from what its extensions state, if they
// state any.
func (pg *pager) operation(op *ir.Operation) []ir.Diagnostic {
	fields := pageFields{inputs: map[lowering.ExtensionTarget]*ir.InputPath{},
		outputs: map[lowering.ExtensionTarget]*ir.PropPath{}}
	var diags []ir.Diagnostic
	for _, target := range requestTargets() {
		in, read, failed, readDiags := pg.input(op, target)
		diags = append(diags, readDiags...)
		if failed {
			return diags
		}
		if in != nil {
			fields.inputs[target], fields.read = in, append(fields.read, read...)
		}
	}
	for _, target := range responseTargets() {
		out, read, failed, readDiags := pg.output(op, target)
		diags = append(diags, readDiags...)
		if failed {
			return diags
		}
		if out != nil {
			fields.outputs[target], fields.read = out, append(fields.read, read...)
		}
	}
	if len(fields.inputs) == 0 && len(fields.outputs) == 0 {
		return diags
	}
	page, reason := assemble(fields)
	if page == nil {
		return append(diags, pg.report(op, "its extensions state a pagination that %s, so it fills none", reason))
	}
	op.Pagination = page
	for _, prov := range fields.read {
		prov.Inferred = ir.JoinInferred(prov.Inferred, lowering.ExtensionPromotionHeuristic)
	}
	return diags
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
	page := &ir.Pagination{
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
		page.Strategy, page.InputCursor = strategyOf(continues[0]), f.inputs[continues[0]]
	}
	return page, ""
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
