// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"strings"

	"github.com/dexpace/morphic/ir"
)

// The pagination an extension declares, one target a field of ir.Pagination.
// A source listing no Values holds a runtime expression naming the value, as
// a link's parameters do; one listing Values marks the parameter, member or
// type it sits on as that value (docs/compilers/openapi.md#extension-pagination).
const (
	// TargetPaginationCursor is the input a cursor walk sends the next cursor
	// in, ir.Pagination.InputCursor of strategy cursor.
	TargetPaginationCursor ExtensionTarget = "pagination.cursor"
	// TargetPaginationOffset is the input an offset walk sends its item offset
	// in, InputCursor of strategy offset.
	TargetPaginationOffset ExtensionTarget = "pagination.offset"
	// TargetPaginationPage is the input a page walk sends its page number in,
	// InputCursor of strategy page.
	TargetPaginationPage ExtensionTarget = "pagination.page"
	// TargetPaginationLimit is the page-size input, InputLimit.
	TargetPaginationLimit ExtensionTarget = "pagination.limit"
	// TargetPaginationItems is where a page's items sit, Items.
	TargetPaginationItems ExtensionTarget = "pagination.items"
	// TargetPaginationNextCursor is the next cursor in a page, NextCursor.
	TargetPaginationNextCursor ExtensionTarget = "pagination.nextCursor"
	// TargetPaginationNextLink is the next page's link in a page, NextLink, a
	// walk of strategy next_link where no input continues it.
	TargetPaginationNextLink ExtensionTarget = "pagination.nextLink"
	// TargetPaginationTotalCount is the count of every item, TotalCount.
	TargetPaginationTotalCount ExtensionTarget = "pagination.totalCount"
	// TargetPaginationTotalPages is the count of every page, TotalPages.
	TargetPaginationTotalPages ExtensionTarget = "pagination.totalPages"
)

// paginationTargetPrefix starts the name of every pagination target.
const paginationTargetPrefix = "pagination."

// isPaginationTarget reports whether target names a field of ir.Pagination.
func isPaginationTarget(target ExtensionTarget) bool {
	return strings.HasPrefix(string(target), paginationTargetPrefix)
}

// PageStatement is what one source of a pagination target says on one node.
type PageStatement struct {
	// Expression is the runtime expression the source's value spells, empty
	// for a source that marks.
	Expression string
	// At is where the source's value is written, which a report names.
	At ir.Provenance
	// Stated reports whether the source says anything on the node: an
	// expression, or that the node is the value its target names.
	Stated bool
}

// Sources returns what the policy reads target from, in order of precedence.
// The list is the policy's own: a caller reads it and writes nothing to it.
func (p Promoter) Sources(target ExtensionTarget) []ExtensionSource {
	return p.targets[target]
}

// PageStatementOf reads what src, a source p reads the pagination target
// target from, says on the node keeping unmodeled: the expression its value
// spells where it lists no Values, and whether its value is one of them where
// it does. A value of neither form is reported as every target reports a
// misfit.
func PageStatementOf(p Promoter, target ExtensionTarget, src ExtensionSource, unmodeled ir.Unmodeled) (PageStatement, []ir.Diagnostic) {
	one := []ExtensionSource{src}
	if len(src.Values) > 0 {
		_, at, marked, diags := firstStatedAt(unmodeled, one, reading[bool]{
			target: target,
			read: func(src ExtensionSource, raw ir.RawValue) (bool, statement) {
				return listed(p.targets, target, src, raw)
			},
		})
		return PageStatement{At: at, Stated: marked}, diags
	}
	expression, at, found, diags := firstStatedAt(unmodeled, one, textReading(target))
	return PageStatement{Expression: expression, At: at, Stated: found}, diags
}
