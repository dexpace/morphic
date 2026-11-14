// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/ir"
)

// The shorthand a pagination expression may take instead of a runtime
// expression: a dotted chain of names after the side it reads.
const (
	shorthandRequest  = "$" + runtimeexpr.SourceRequest + "."
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
		st, diags := lowering.PageStatementOf(pg.p, target, src, op.Unmodeled)
		r := resolved[*ir.InputPath]{at: st.At, stated: st.Stated, written: st.Written,
			read: []*ir.Provenance{&op.Provenance}}
		if !st.Stated {
			return r, "", diags
		}
		in, failure := pg.requestValue(op, st.Expression)
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
		st, diags := lowering.PageStatementOf(pg.p, target, src, op.Unmodeled)
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

// requestValue resolves text, an expression naming a value of op's request:
// a runtime expression reading a parameter or the body, or the shorthand
// `$request.` and a dotted chain of names, the first a parameter's name or,
// where no parameter carries it, a member of the body. It returns why it
// resolves nothing.
func (pg *pager) requestValue(op *ir.Operation, text string) (*ir.InputPath, string) {
	if e, isExpression, err := runtimeexpr.Parse(text); isExpression && err == nil {
		if e.Source != runtimeexpr.SourceRequest {
			return nil, "names no value of the request"
		}
		if e.Part == runtimeexpr.PartBody {
			return pg.bodyInput(op, e.Tokens)
		}
		found := pg.lk.ParamIndex(op).Bound(ir.HTTPLocation(e.Part), e.Name)
		if len(found) != 1 {
			return nil, "reads " + e.Part + " parameter " + strconv.Quote(e.Name) + ", which the operation does not declare once"
		}
		return &ir.InputPath{Param: &found[0]}, ""
	}
	rest, ok := strings.CutPrefix(text, shorthandRequest)
	names := strings.Split(rest, ".")
	if !ok || slices.Contains(names, "") {
		return nil, "is no expression naming a value of the request"
	}
	switch found := pg.lk.ParamIndex(op).Bound("", names[0]); len(found) {
	case 0:
		return pg.bodyInput(op, names)
	case 1:
		return pg.paramInput(op, found[0], names[1:])
	default:
		return nil, "names parameter " + strconv.Quote(names[0]) + ", which the operation declares in more than one location"
	}
}

// bodyInput is the member of op's request body tokens walk to, the body whole
// for none.
func (pg *pager) bodyInput(op *ir.Operation, tokens []string) (*ir.InputPath, string) {
	segments, why, ok := runtimeexpr.BodyPath(pg.lk, op.Request, tokens, "request")
	if !ok {
		return nil, why
	}
	return &ir.InputPath{Segments: segments}, ""
}

// paramInput is the member of op's parameter id that names walk to, the
// parameter whole for none.
func (pg *pager) paramInput(op *ir.Operation, id ir.ParamID, names []string) (*ir.InputPath, string) {
	in := &ir.InputPath{Param: &id}
	if len(names) == 0 {
		return in, ""
	}
	i := slices.IndexFunc(op.Params, func(p ir.Parameter) bool { return p.ID == id })
	if i < 0 {
		// A binding names only a parameter its operation holds.
		return nil, "names parameter " + string(id) + ", which a binding names and the operation does not hold"
	}
	segments, ok := pg.lk.Walk(op.Params[i].Type.Target, names)
	if !ok {
		return nil, "reads " + strings.Join(names, ".") + " of parameter " + strconv.Quote(op.Params[i].Name.Source) +
			", which holds no such chain of members"
	}
	in.Segments = segments
	return in, ""
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
			return pg.headerOutput(op, e.Name)
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
		segments, why, ok := runtimeexpr.BodyPath(pg.lk, body, tokens, "response")
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
// declare once.
func (pg *pager) headerOutput(op *ir.Operation, name string) (*ir.PropPath, string) {
	if len(op.Responses) == 0 {
		return nil, "reads response header " + strconv.Quote(name) + ", and the operation declares no success response"
	}
	var out *ir.PropPath
	for i := range op.Responses {
		found := pg.lk.Header(runtimeexpr.ResponseKey{Op: op.ID, Index: i}, op.Responses[i].Headers, name)
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
