// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// requestTargets are the pagination targets naming a value the request carries.
func requestTargets() []promotion.ExtensionTarget {
	return []promotion.ExtensionTarget{
		promotion.TargetPaginationCursor, promotion.TargetPaginationOffset, promotion.TargetPaginationPage,
		promotion.TargetPaginationLimit,
	}
}

// responseTargets are the pagination targets naming a value a page carries.
func responseTargets() []promotion.ExtensionTarget {
	return []promotion.ExtensionTarget{
		promotion.TargetPaginationItems, promotion.TargetPaginationNextCursor, promotion.TargetPaginationNextLink,
		promotion.TargetPaginationTotalCount, promotion.TargetPaginationTotalPages, promotion.TargetPaginationHasMore,
	}
}

// Promote fills the pagination each operation of compiledDocumentView, the
// document, declares through the extensions the promotion policy maps to it,
// where the operation holds none
// (docs/compilers/openapi.md#extension-pagination), whole or not at all: one
// whose values do not all resolve, or which states no way to the next page, is
// reported at the operation. promoter is the run's promoter.
//
// stated holds each operation it reads whose extensions speak of a pagination,
// filled or not: those an inference leaves alone, by that rule's precedence.
// root is the document root's Unmodeled, which an operation's key written
// true reads (promotion.PageStatementOf).
func Promote(promoter promotion.Promoter, compiledDocumentView *compileddoc.CompiledDocumentView, root ir.Unmodeled) (stated map[ir.OperationID]bool, diags []ir.Diagnostic) {
	if root == nil {
		root = ir.Unmodeled{} // the nodes read are operations, whose root states nothing
	}
	pg := pager{p: promoter, w: compiledDocumentView, root: root}
	if !pg.maps() {
		return nil, nil
	}
	stated = map[ir.OperationID]bool{}
	for _, operation := range compiledDocumentView.Operations {
		if operation.Pagination != nil {
			continue
		}
		spoke, operationDiags := pg.operation(operation)
		diags = append(diags, operationDiags...)
		if spoke {
			stated[operation.ID] = true
		}
	}
	return stated, diags
}

// pager is what one Promote reads its operations against: the promoter, p,
// the document, w, its root's Unmodeled, root, and reach, the models reaching
// each marking key (pager.reaching).
type pager struct {
	p     promotion.Promoter
	w     *compileddoc.CompiledDocumentView
	root  ir.Unmodeled
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
	inputs  map[promotion.ExtensionTarget]*ir.InputPath
	outputs map[promotion.ExtensionTarget]*ir.PropertyPath
	read    []*ir.Provenance
	written bool
}

// operation fills operation's pagination from what its extensions state, if they
// state any, and reports whether they speak of it at all: whether a source's
// key is written where it is read, whatever its value, filling a pagination,
// failing to or stating nothing.
func (pg *pager) operation(operation *ir.Operation) (bool, []ir.Diagnostic) {
	fields := pageFields{inputs: map[promotion.ExtensionTarget]*ir.InputPath{},
		outputs: map[promotion.ExtensionTarget]*ir.PropertyPath{}}
	var diags []ir.Diagnostic
	for _, target := range requestTargets() {
		in, failed, readDiags := pg.input(operation, target)
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
		out, failed, readDiags := pg.output(operation, target)
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
		diags = append(diags, pg.report(operation, "the pagination the policy reads from its extensions %s, so it fills none", reason))
		return fields.written, diags
	}
	operation.Pagination = walk
	for _, prov := range fields.read {
		prov.Inferred = ir.JoinInferred(prov.Inferred, promotion.ExtensionPromotionHeuristic)
	}
	return fields.written, diags
}

// report is a finding at operation about what its extensions state of its pagination.
func (pg *pager) report(operation *ir.Operation, format string, args ...any) ir.Diagnostic {
	return diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, operation.Provenance, "operation %q: "+format,
		append([]any{operation.ID}, args...)...)
}

// assemble builds the pagination fields state, and returns why it builds none:
// more than one input continuing the walk, a cursor beside no next cursor or a
// next cursor beside no cursor, an offset or a page number beside no items,
// which a pager counts to advance an offset and to end either walk
// (docs/emitters/operations/pagination.md), or nothing reaching the next page
// at all. A walk naming its flag ends by it alone (ir.PageStopNoMore).
func assemble(pageFieldsValue pageFields) (*ir.Pagination, string) {
	var continues []promotion.ExtensionTarget
	for _, extensionTarget := range []promotion.ExtensionTarget{promotion.TargetPaginationCursor, promotion.TargetPaginationOffset,
		promotion.TargetPaginationPage} {
		if pageFieldsValue.inputs[extensionTarget] != nil {
			continues = append(continues, extensionTarget)
		}
	}
	_, cursor := pageFieldsValue.inputs[promotion.TargetPaginationCursor]
	_, next := pageFieldsValue.outputs[promotion.TargetPaginationNextCursor]
	_, link := pageFieldsValue.outputs[promotion.TargetPaginationNextLink]
	_, items := pageFieldsValue.outputs[promotion.TargetPaginationItems]
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
		InputLimit: pageFieldsValue.inputs[promotion.TargetPaginationLimit],
		Items:      pageFieldsValue.outputs[promotion.TargetPaginationItems],
		NextCursor: pageFieldsValue.outputs[promotion.TargetPaginationNextCursor],
		NextLink:   pageFieldsValue.outputs[promotion.TargetPaginationNextLink],
		TotalCount: pageFieldsValue.outputs[promotion.TargetPaginationTotalCount],
		TotalPages: pageFieldsValue.outputs[promotion.TargetPaginationTotalPages],
		HasMore:    pageFieldsValue.outputs[promotion.TargetPaginationHasMore],
	}
	if walk.HasMore != nil {
		// The source states the flag, which is where the walk ends.
		walk.Stop = []ir.PageStop{ir.PageStopNoMore}
	}
	if len(continues) == 1 {
		walk.Strategy, walk.InputCursor = strategyOf(continues[0]), pageFieldsValue.inputs[continues[0]]
	}
	return walk, ""
}

// strategyOf is the strategy of a walk an input of target continues.
func strategyOf(target promotion.ExtensionTarget) ir.PageStrategy {
	switch target {
	case promotion.TargetPaginationOffset:
		return ir.PageStrategyOffset
	case promotion.TargetPaginationPage:
		return ir.PageStrategyPage
	default:
		return ir.PageStrategyCursor
	}
}

// joinTargets spells targets for a report.
func joinTargets(targets []promotion.ExtensionTarget) string {
	names := make([]string, len(targets))
	for i, extensionTarget := range targets {
		names[i] = string(extensionTarget)
	}
	return strings.Join(names, ", ")
}
