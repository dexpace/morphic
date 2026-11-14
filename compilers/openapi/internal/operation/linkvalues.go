// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// exchange is the request and response a link reads values from: a response
// of from, at, in the document w.
type exchange struct {
	w    *lowering.Whole
	from *ir.Operation
	at   responseView
}

// value lowers one value a link passes as node writes it: a runtime
// expression whole is a value of the exchange, any other value a constant. A
// string an expression is substituted into is a value no ir.LinkValue spells,
// written alone or anywhere within a list or an object: a constant holding one
// would pass on its braces as written.
func (ex exchange) value(node *yaml.Node) (ir.LinkValue, *linkFailure) {
	v, err := value.FromNode(node)
	if err != nil {
		return ir.LinkValue{}, untyped("passes a value the IR has no constant for: %v", err)
	}
	if v.Kind != ir.ValueString {
		if s, ok := substitutedString(&v); ok {
			return ir.LinkValue{}, untyped("passes a value holding %q, a string an expression is substituted into", s)
		}
		return ir.LinkValue{Kind: ir.LinkConstant, Constant: &v}, nil
	}
	e, isExpression, err := runtimeexpr.Parse(v.Str)
	switch {
	case err != nil:
		return ir.LinkValue{}, invalid("passes %q, which breaks the runtime expression grammar", v.Str)
	case isExpression:
		return ex.read(e)
	case runtimeexpr.Embeds(v.Str):
		return ir.LinkValue{}, untyped("passes %q, a string an expression is substituted into", v.Str)
	default:
		return ir.LinkValue{Kind: ir.LinkConstant, Constant: &v}, nil
	}
}

// substitutedString returns the first string within v, in source order, that
// an expression is substituted into, and false for none. It walks with a
// stack of its own rather than recursing, so v's depth, which value.FromNode
// bounds, bounds no call stack here.
func substitutedString(v *ir.Value) (string, bool) {
	stack := []*ir.Value{v}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch top.Kind {
		case ir.ValueString:
			if runtimeexpr.Embeds(top.Str) {
				return top.Str, true
			}
		case ir.ValueList:
			for i := len(top.List) - 1; i >= 0; i-- {
				stack = append(stack, &top.List[i])
			}
		case ir.ValueObject:
			for i := len(top.Object) - 1; i >= 0; i-- {
				stack = append(stack, &top.Object[i].Value)
			}
		case ir.ValueNull, ir.ValueBool, ir.ValueNumber, ir.ValueBytes, ir.ValueSymbol, ir.ValueRefKind,
			ir.ValueCtor, ir.ValueInf, ir.ValueNegInf, ir.ValueNaN:
			// value.FromNode builds none of these holding a string.
		default:
			// Nor a kind ir does not declare.
		}
	}
	return "", false
}

// read lowers the value e reads of the exchange.
func (ex exchange) read(e runtimeexpr.Expression) (ir.LinkValue, *linkFailure) {
	switch e.Source {
	case runtimeexpr.SourceURL:
		return ir.LinkValue{Kind: ir.LinkURL}, nil
	case runtimeexpr.SourceMethod:
		return ir.LinkValue{Kind: ir.LinkMethod}, nil
	case runtimeexpr.SourceStatusCode:
		return ir.LinkValue{Kind: ir.LinkStatus}, nil
	case runtimeexpr.SourceRequest:
		return ex.request(e)
	default:
		return ex.response(e)
	}
}

// request lowers what e reads of the request: its body or a member of it, or
// a parameter of from.
func (ex exchange) request(e runtimeexpr.Expression) (ir.LinkValue, *linkFailure) {
	if e.Part == runtimeexpr.PartBody {
		segments, why, ok := runtimeexpr.BodyPath(ex.w, ex.from.Request, e.Tokens, "request")
		if !ok {
			return ir.LinkValue{}, untyped("%s", why)
		}
		return ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Segments: segments}}, nil
	}
	found := ex.w.Params(ex.from).Bound(ir.HTTPLocation(e.Part), e.Name)
	if len(found) != 1 {
		return ir.LinkValue{}, untyped("reads %s parameter %q, which operation %q does not declare once",
			e.Part, e.Name, ex.from.ID)
	}
	return ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Param: &found[0]}}, nil
}

// response lowers what e reads of the response: its body or a member of it, or
// one of its headers. A header the response declares twice, in two cases, is
// no one header: either would be a choice the source does not make.
func (ex exchange) response(e runtimeexpr.Expression) (ir.LinkValue, *linkFailure) {
	switch e.Part {
	case runtimeexpr.PartBody:
		segments, why, ok := runtimeexpr.BodyPath(ex.w, ex.at.payload, e.Tokens, "response")
		if !ok {
			return ir.LinkValue{}, untyped("%s", why)
		}
		return ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{Segments: segments}}, nil
	case runtimeexpr.PartHeader:
		found := ex.w.Header(ex.at.key, ex.at.headers, e.Name)
		if len(found) != 1 {
			return ir.LinkValue{}, untyped("reads response header %q, which the response does not declare once", e.Name)
		}
		return ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{In: "header", Segments: found}}, nil
	default:
		return ir.LinkValue{}, invalid("reads the response's %s parameter %q, which no response carries", e.Part, e.Name)
	}
}

// targetParam returns the parameter of op that a link's parameters key names
// (lowering.ParamIndex.Named), which must name one parameter only.
func targetParam(w *lowering.Whole, op *ir.Operation, key string) (ir.ParamID, *linkFailure) {
	id, matched := w.Params(op).Named(key)
	switch matched {
	case 1:
		return id, nil
	case 0:
		return "", invalid("names parameter %q, which operation %q does not declare", key, op.ID)
	default:
		return "", invalid("names parameter %q, which operation %q declares in %d locations", key, op.ID, matched)
	}
}
