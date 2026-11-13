// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// The shorthand a pagination expression may take instead of a runtime
// expression: a dotted chain of names after the side it reads.
const (
	shorthandRequest  = "$" + exprRequest + "."
	shorthandResponse = "$" + exprResponse + "."
)

// resolved is what one source of a pagination target names on an operation:
// the value, the nodes whose keys named it, and where that is written.
type resolved[T any] struct {
	value  T
	read   []*ir.Provenance
	at     ir.Provenance
	stated bool
}

// firstResolved returns what the first of target's sources to state anything on
// op resolves to, and true where a source stated a value that does not
// resolve, which is reported and fills no pagination. A later source naming
// another value is reported and loses, as it does for every target.
func firstResolved[T any](pg *pager, op *ir.Operation, target lowering.ExtensionTarget,
	resolve func(lowering.ExtensionSource) (resolved[T], string, []ir.Diagnostic), same func(T, T) bool,
) (T, []*ir.Provenance, bool, []ir.Diagnostic) {
	var kept resolved[T]
	var first lowering.ExtensionSource
	var diags []ir.Diagnostic
	for _, src := range pg.p.Sources(target) {
		r, failure, readDiags := resolve(src)
		diags = append(diags, readDiags...)
		switch {
		case failure != "":
			return kept.value, nil, true, append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, r.at,
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
	return kept.value, kept.read, false, diags
}

// input resolves what op's extensions state of target, a value of its request.
func (pg *pager) input(op *ir.Operation, target lowering.ExtensionTarget) (*ir.InputPath, []*ir.Provenance, bool, []ir.Diagnostic) {
	return firstResolved(pg, op, target, func(src lowering.ExtensionSource) (resolved[*ir.InputPath], string, []ir.Diagnostic) {
		if len(src.Values) > 0 {
			return pg.markedInput(op, target, src)
		}
		st, diags := lowering.PageStatementOf(pg.p, target, src, op.Unmodeled)
		r := resolved[*ir.InputPath]{at: st.At, stated: st.Stated, read: []*ir.Provenance{&op.Provenance}}
		if !st.Stated {
			return r, "", diags
		}
		in, failure := pg.requestValue(op, st.Expression)
		r.value = in
		return r, failure, diags
	}, sameInput)
}

// output resolves what op's extensions state of target, a value of a page.
func (pg *pager) output(op *ir.Operation, target lowering.ExtensionTarget) (*ir.PropPath, []*ir.Provenance, bool, []ir.Diagnostic) {
	return firstResolved(pg, op, target, func(src lowering.ExtensionSource) (resolved[*ir.PropPath], string, []ir.Diagnostic) {
		if len(src.Values) > 0 {
			return pg.markedOutput(op, target, src)
		}
		st, diags := lowering.PageStatementOf(pg.p, target, src, op.Unmodeled)
		r := resolved[*ir.PropPath]{at: st.At, stated: st.Stated, read: []*ir.Provenance{&op.Provenance}}
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
	if e, isExpression, err := parseExpression(text); isExpression && err == nil {
		if e.source != exprRequest {
			return nil, "names no value of the request"
		}
		if e.part == exprBody {
			return pg.bodyInput(op, e.tokens)
		}
		found := pg.lk.paramIndex(op).Bound(ir.HTTPLocation(e.part), e.name)
		if len(found) != 1 {
			return nil, "reads " + e.part + " parameter " + strconv.Quote(e.name) + ", which the operation does not declare once"
		}
		return &ir.InputPath{Param: &found[0]}, ""
	}
	rest, ok := strings.CutPrefix(text, shorthandRequest)
	names := strings.Split(rest, ".")
	if !ok || slices.Contains(names, "") {
		return nil, "is no expression naming a value of the request"
	}
	switch found := pg.lk.paramIndex(op).Bound("", names[0]); len(found) {
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
	segments, failed := bodyPath(pg.lk, op.Request, tokens, "request")
	if failed != nil {
		return nil, failed.reason
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
	segments, ok := pg.lk.walk(op.Params[i].Type.Target, names)
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
	if e, isExpression, err := parseExpression(text); isExpression && err == nil {
		switch {
		case e.source != exprResponse:
			return nil, "names no value of a page"
		case e.part == exprBody:
			return pg.bodyOutput(op, e.tokens)
		case e.part == exprHeader:
			return pg.headerOutput(op, e.name)
		default:
			return nil, "reads a response's " + e.part + " parameter, which no response carries"
		}
	}
	rest, ok := strings.CutPrefix(text, shorthandResponse)
	names := strings.Split(rest, ".")
	if !ok || slices.Contains(names, "") {
		return nil, "is no expression naming a value of a page"
	}
	return pg.bodyOutput(op, names)
}

// bodyOutput is the member tokens walk to in the body of every success
// response of op declaring one, which must be the same member in each.
func (pg *pager) bodyOutput(op *ir.Operation, tokens []string) (*ir.PropPath, string) {
	var out *ir.PropPath
	for i := range op.Responses {
		r := &op.Responses[i]
		if r.Payload == nil {
			continue
		}
		segments, failed := bodyPath(pg.lk, r.Payload, tokens, "response")
		switch {
		case failed != nil:
			return nil, failed.reason
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
		found := pg.lk.header(responseSlot{list: slotResponses, index: i}.in(op), name)
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
