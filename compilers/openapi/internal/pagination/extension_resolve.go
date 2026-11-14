// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"iter"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// The shorthand a pagination expression may take instead of a runtime
// expression: a dotted chain of names after the side it reads, which
// runtimeexpr.RequestValue reads for the request.
const (
	shorthandResponse = "$" + runtimeexpr.SourceResponse + "."
)

// resolved is what one source of a pagination target names on an operation:
// the value, the nodes whose keys named it, where that is written, and whether
// the source's key is written where it is read, whatever it states
// (lowering.PageStatement.Written).
type resolved[T any] struct {
	value   T
	read    []*ir.Provenance
	at      ir.Provenance
	stated  bool
	written bool
}

// firstResolved returns what the first of target's sources to state anything on
// op resolves to, written where any source's key is, and true where a source
// stated a value that does not resolve, which is reported and fills no
// pagination. A later source naming another value is reported and loses, as it
// does for every target.
func firstResolved[T any](pg *pager, op *ir.Operation, target lowering.ExtensionTarget,
	read func(lowering.ExtensionSource) (resolved[T], string, []ir.Diagnostic), same func(T, T) bool,
) (resolved[T], bool, []ir.Diagnostic) {
	var kept resolved[T]
	var first lowering.ExtensionSource
	var diags []ir.Diagnostic
	written := false
	for _, src := range pg.p.Sources(target) {
		r, failure, readDiags := read(src)
		diags = append(diags, readDiags...)
		written = written || r.written
		switch {
		case failure != "":
			return resolved[T]{written: true}, true, append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, r.at,
				"extension %q %s, so operation %q takes no pagination from its extensions", src, failure, op.ID))
		case !r.stated:
		case !kept.stated:
			kept, first = r, src
		case !same(kept.value, r.value):
			diags = append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, r.at,
				"extension %q states another %s than %q, which the policy reads first, and does not fill it",
				src, target, first))
		}
	}
	kept.written = written
	return kept, false, diags
}

// input resolves what op's extensions state of target, a value of its request.
func (pg *pager) input(op *ir.Operation, target lowering.ExtensionTarget) (resolved[*ir.InputPath], bool, []ir.Diagnostic) {
	return firstResolved(pg, op, target, func(src lowering.ExtensionSource) (resolved[*ir.InputPath], string, []ir.Diagnostic) {
		if len(src.Values) > 0 {
			return pg.markedInput(op, target, src)
		}
		st, diags := lowering.PageStatementOf(pg.p, target, src, op.Unmodeled, pg.root)
		r := resolved[*ir.InputPath]{at: st.At, stated: st.Stated, written: st.Written,
			read: []*ir.Provenance{&op.Provenance}}
		if !st.Stated {
			return r, "", diags
		}
		in, _, failure := runtimeexpr.RequestValue(pg.w, op, st.Expression)
		r.value = in
		return r, failure, diags
	}, sameInput)
}

// output resolves what op's extensions state of target, a value of a page.
func (pg *pager) output(op *ir.Operation, target lowering.ExtensionTarget) (resolved[*ir.PropPath], bool, []ir.Diagnostic) {
	return firstResolved(pg, op, target, func(src lowering.ExtensionSource) (resolved[*ir.PropPath], string, []ir.Diagnostic) {
		if len(src.Values) > 0 {
			return pg.markedOutput(op, target, src)
		}
		st, diags := lowering.PageStatementOf(pg.p, target, src, op.Unmodeled, pg.root)
		r := resolved[*ir.PropPath]{at: st.At, stated: st.Stated, written: st.Written,
			read: []*ir.Provenance{&op.Provenance}}
		if !st.Stated {
			return r, "", diags
		}
		out, failure := pg.responseValue(op, st.Expression)
		r.value = out
		return r, failure, diags
	}, sameOutput)
}

// sameInput reports whether two inputs name one value.
func sameInput(a, b *ir.InputPath) bool {
	return (a.Param == nil) == (b.Param == nil) && (a.Param == nil || *a.Param == *b.Param) &&
		slices.Equal(a.Segments, b.Segments)
}

// sameOutput reports whether two page values name one value.
func sameOutput(a, b *ir.PropPath) bool {
	return a.In == b.In && slices.Equal(a.Segments, b.Segments)
}

// responseValue resolves text, an expression naming a value every success
// response of op carries: a runtime expression reading the body or a header,
// or the shorthand `$response.` and a dotted chain of the body's members. It
// returns why it resolves nothing.
func (pg *pager) responseValue(op *ir.Operation, text string) (*ir.PropPath, string) {
	if e, isExpression, err := runtimeexpr.Parse(text); isExpression && err == nil {
		switch {
		case e.Source != runtimeexpr.SourceResponse:
			return nil, "names no value of a page"
		case e.Part == runtimeexpr.PartBody:
			return pg.bodyOutput(op, e.Tokens)
		case e.Part == runtimeexpr.PartHeader:
			return headerOutput(pg.w, op, e.Name)
		default:
			return nil, "reads a response's " + e.Part + " parameter, which no response carries"
		}
	}
	rest, ok := strings.CutPrefix(text, shorthandResponse)
	names := strings.Split(rest, ".")
	if !ok || slices.Contains(names, "") {
		return nil, "is no expression naming a value of a page"
	}
	return pg.bodyOutput(op, names)
}

// bodyOutput is the member tokens walk to in the page body of every success
// response of op declaring one (pageBody), which must be the same member in
// each.
func (pg *pager) bodyOutput(op *ir.Operation, tokens []string) (*ir.PropPath, string) {
	var out *ir.PropPath
	for i := range op.Responses {
		body := pageBody(op.Responses[i].Payload)
		if body == nil {
			continue
		}
		segments, why, ok := runtimeexpr.BodyPath(pg.w, body, tokens, "response")
		switch {
		case !ok:
			return nil, why
		case out == nil:
			out = &ir.PropPath{Segments: segments}
		case !slices.Equal(out.Segments, segments):
			return nil, "reads " + strings.Join(tokens, ".") + ", which the success responses hold as different members"
		}
	}
	if out == nil {
		return nil, "reads a page body, and no success response declares one"
	}
	return out, ""
}

// headerOutput is the header name, which every success response of op must
// declare once: what an extension's expression reads, and what the inference
// reads a next cursor from (headerCursor). w is the document.
func headerOutput(w *whole.Document, op *ir.Operation, name string) (*ir.PropPath, string) {
	if len(op.Responses) == 0 {
		return nil, "reads response header " + strconv.Quote(name) + ", and the operation declares no success response"
	}
	var out *ir.PropPath
	for i := range op.Responses {
		found := w.Header(whole.ResponseKey{Op: op.ID, List: whole.ListResponses, Index: i}, op.Responses[i].Headers, name)
		switch {
		case len(found) != 1:
			return nil, "reads response header " + strconv.Quote(name) + ", which a success response does not declare once"
		case out == nil:
			out = &ir.PropPath{In: "header", Segments: found}
		case out.Segments[0] != found[0]:
			return nil, "reads response header " + strconv.Quote(name) + ", which the success responses declare apart"
		}
	}
	return out, ""
}

// pageContents returns the contents of op's success responses that can be a
// page, response by response (pageBody): what a walk reads its page from,
// whether a promotion states it or an inference reads it.
func pageContents(op *ir.Operation) iter.Seq[*ir.Content] {
	return func(yield func(*ir.Content) bool) {
		for i := range op.Responses {
			body := pageBody(op.Responses[i].Payload)
			if body == nil {
				continue
			}
			for j := range body.Contents {
				if !yield(&body.Contents[j]) {
					return
				}
			}
		}
	}
}

// pageBody is what of a response's body can be a page: its contents that do
// not stream, a stream being a sequence of frames and no one document a pager
// reads (docs/ir/payloads.md#a-content-that-streams). It is nil where body is
// or every content streams, such a response holding no page, as one declaring
// no body holds none.
func pageBody(body *ir.Payload) *ir.Payload {
	if body == nil {
		return nil
	}
	streaming := 0
	for i := range body.Contents {
		if body.Contents[i].Stream != nil {
			streaming++
		}
	}
	switch streaming {
	case 0:
		return body
	case len(body.Contents):
		return nil
	}
	kept := *body
	kept.Contents = make([]ir.Content, 0, len(body.Contents)-streaming)
	for i := range body.Contents {
		if body.Contents[i].Stream == nil {
			kept.Contents = append(kept.Contents, body.Contents[i])
		}
	}
	return &kept
}
