// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// InferWalks fills the Pagination of each GET or QUERY of doc the client sends that
// holds none, where a query parameter names where its walk continues and its
// success responses say where a page holds its items, and a cursor walk's next
// cursor (docs/compilers/openapi.md#pagination-policy), marking it inferred on
// the Pagination and in the operation's Provenance. An operation whose
// parameters name a walk it cannot settle keeps none, reported as an info
// diagnostic. It neither reads nor reports an operation in stated, whose
// extensions speak of a pagination. w is the document, which is whole; c is
// the source's context.
func InferWalks(c lowering.Ctx, w *whole.Document, stated map[ir.OpID]bool) []ir.Diagnostic {
	if !c.PaginationInference.Honours() {
		return nil
	}
	r := newReader(w)
	var diags []ir.Diagnostic
	for _, op := range w.Ops {
		if stated[op.ID] {
			continue
		}
		if why, undecided := r.infer(op); undecided {
			diags = append(diags, undecidedAt(op, why))
		}
	}
	return diags
}

// infer fills op's Pagination where the names it uses settle one. It reports
// undecided, with why, where its query parameters name an input and the names
// settle none; an operation holding a Pagination, or one the API sends, is left
// alone.
func (r *reader) infer(op *ir.Operation) (why string, undecided bool) {
	if op.Pagination != nil || inbound(op) {
		return "", false
	}
	in, why, named := requestInputs(r.w, op)
	if !named {
		return "", false
	}
	if why != "" {
		return why, true
	}
	pg, why := r.responsePage(op, in)
	if why != "" {
		return why, true
	}
	op.Pagination = build(in, pg)
	op.Provenance.Inferred = ir.JoinInferred(op.Provenance.Inferred, lowering.PaginationInferenceHeuristic)
	return "", false
}

// inbound reports whether the API sends op rather than the client: a webhook
// or a callback, whose receiver walks no pages.
func inbound(op *ir.Operation) bool {
	return slices.ContainsFunc(op.Bindings.HTTP, func(b ir.HTTPBinding) bool { return b.Inbound != "" })
}

// methodQuery is the QUERY method OpenAPI 3.2 binds a query operation to,
// which net/http names no constant for.
const methodQuery = "QUERY"

// build is the Pagination a walk sending in reads off pg. A walk following a
// next cursor ends at the flag its page states (ir.PageStopNoMore), the page's
// own statement of where it ends, and otherwise where the cursor is empty, null
// or absent (ir.PageStopNoNext), as an empty cursor names no page; one counting
// pages or items lists no stop, a page holding none ending it whatever Stop
// lists.
func build(in inputs, pg page) *ir.Pagination {
	out := &ir.Pagination{
		Strategy:    in.strategy,
		Inferred:    true,
		InputCursor: in.cursor.input(),
		Items:       &ir.PropPath{Segments: slices.Clone(pg.items)},
		TotalCount:  pathTo(pg.totalCount),
		TotalPages:  pathTo(pg.totalPages),
	}
	if in.limit != nil {
		out.InputLimit = in.limit.input()
	}
	switch {
	case pg.hasMore != nil:
		out.NextCursor, out.HasMore = pathTo(pg.next), pathTo(pg.hasMore)
		out.Stop = []ir.PageStop{ir.PageStopNoMore}
	case pg.next != nil:
		out.NextCursor = pathTo(pg.next)
		out.Stop = []ir.PageStop{ir.PageStopNoNext}
	}
	return out
}

// pathTo is the path through segments, or nil for none.
func pathTo(segments []ir.PropID) *ir.PropPath {
	if segments == nil {
		return nil
	}
	return &ir.PropPath{Segments: slices.Clone(segments)}
}

// undecidedAt reports why op gets no Pagination, at op: the declaration its
// Provenance names, which every mount of it shares, so a path item mounted
// twice is reported once.
func undecidedAt(op *ir.Operation, why string) ir.Diagnostic {
	at := op.Provenance
	at.Inferred = ""
	return diag.Newf(ir.SeverityInfo, diag.UndecidedPagination, at, "no pagination is inferred: %s", why)
}

// maxListed bounds how many names a finding lists, so one naming hundreds of
// parameters or members says what it means in one line.
const maxListed = 3

// listed joins names, two or more, into a list sorted so it reads the same
// whatever order the document declares them in (joined).
func listed(names []string) string {
	slices.Sort(names)
	return joined(names)
}

// joined joins names, two or more, in their order, naming the first maxListed
// and how many more there are.
func joined(names []string) string {
	if more := len(names) - maxListed; more > 0 {
		names = append(names[:maxListed:maxListed], strconv.Itoa(more)+" more")
	}
	last := len(names) - 1
	return strings.Join(names[:last], ", ") + " and " + names[last]
}
