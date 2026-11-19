// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// exchange is the request and response a link reads values from: a response
// of from, at, in the document w.
type exchange struct {
	w    *compileddoc.CompiledDocumentView
	from *ir.Operation
	at   responseView
}

// value lowers one value a link passes as node writes it: a runtime
// expression whole is a value of the exchange, any other value a constant. A
// string an expression is substituted into is a value no ir.LinkValue spells,
// written alone or anywhere within a list or an object: a constant holding one
// would pass on its braces as written.
func (ex exchange) value(node *yaml.Node) (ir.LinkValue, *linkFailure) {
	irValue, err := value.FromNode(node)
	if err != nil {
		return ir.LinkValue{}, untyped("passes a value the IR has no constant for: %v", err)
	}
	if irValue.Kind != ir.ValueString {
		if s, ok := substitutedString(&irValue); ok {
			return ir.LinkValue{}, untyped("passes a value holding %q, a string an expression is substituted into", s)
		}
		return ir.LinkValue{Kind: ir.LinkConstant, Constant: &irValue}, nil
	}
	expression, isExpression, err := runtimeexpr.Parse(irValue.String)
	switch {
	case err != nil:
		return ir.LinkValue{}, invalid("passes %q, which breaks the runtime expression grammar", irValue.String)
	case isExpression:
		return ex.read(expression)
	case runtimeexpr.Embeds(irValue.String):
		return ir.LinkValue{}, untyped("passes %q, a string an expression is substituted into", irValue.String)
	default:
		return ir.LinkValue{Kind: ir.LinkConstant, Constant: &irValue}, nil
	}
}

// substitutedString returns the first string within irValue, in source order,
// that an expression is substituted into, and false for none. It walks with a
// stack of its own rather than recursing, so irValue's depth, which
// value.FromNode bounds, bounds no call stack here.
func substitutedString(irValue *ir.Value) (string, bool) {
	stack := []*ir.Value{irValue}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch top.Kind {
		case ir.ValueString:
			if runtimeexpr.Embeds(top.String) {
				return top.String, true
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
			ir.ValueConstructor, ir.ValueInfinity, ir.ValueNegativeInfinity, ir.ValueNaN:
			// value.FromNode builds none of these holding a string.
		default:
			// Nor a kind ir does not declare.
		}
	}
	return "", false
}

// read lowers the value expression reads of the exchange.
func (ex exchange) read(expression runtimeexpr.Expression) (ir.LinkValue, *linkFailure) {
	switch expression.Source {
	case runtimeexpr.SourceURL:
		return ir.LinkValue{Kind: ir.LinkURL}, nil
	case runtimeexpr.SourceMethod:
		return ir.LinkValue{Kind: ir.LinkMethod}, nil
	case runtimeexpr.SourceStatusCode:
		return ir.LinkValue{Kind: ir.LinkStatus}, nil
	case runtimeexpr.SourceRequest:
		return ex.request(expression)
	default:
		return ex.response(expression)
	}
}

// request lowers what expression reads of the request: its body or a member of
// it, or a parameter of from.
func (ex exchange) request(expression runtimeexpr.Expression) (ir.LinkValue, *linkFailure) {
	if expression.Part == runtimeexpr.PartBody {
		segments, why, ok := runtimeexpr.BodyPath(ex.w, ex.from.Request, expression.Tokens, "request")
		if !ok {
			return ir.LinkValue{}, untyped("%s", why)
		}
		return ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Segments: segments}}, nil
	}
	found := ex.w.Params(ex.from).Bound(ir.HTTPLocation(expression.Part), expression.Name)
	if len(found) != 1 {
		return ir.LinkValue{}, untyped("reads %s parameter %q, which operation %q does not declare once",
			expression.Part, expression.Name, ex.from.ID)
	}
	return ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Param: &found[0]}}, nil
}

// response lowers what expression reads of the response: its body or a member
// of it, or one of its headers. A header the response declares twice, in two
// cases, is no one header: either would be a choice the source does not make.
func (ex exchange) response(expression runtimeexpr.Expression) (ir.LinkValue, *linkFailure) {
	switch expression.Part {
	case runtimeexpr.PartBody:
		segments, why, ok := runtimeexpr.BodyPath(ex.w, ex.at.payload, expression.Tokens, "response")
		if !ok {
			return ir.LinkValue{}, untyped("%s", why)
		}
		return ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{Segments: segments}}, nil
	case runtimeexpr.PartHeader:
		found := ex.w.Header(ex.at.key, ex.at.headers, expression.Name)
		if len(found) != 1 {
			return ir.LinkValue{}, untyped("reads response header %q, which the response does not declare once", expression.Name)
		}
		return ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{In: "header", Segments: found}}, nil
	default:
		return ir.LinkValue{}, invalid("reads the response's %s parameter %q, which no response carries", expression.Part, expression.Name)
	}
}

// targetParam returns the parameter of op that a link's parameters key names
// (compileddoc.ParamIndex.Named), which must name one parameter only.
func targetParam(compiledDocumentView *compileddoc.CompiledDocumentView, op *ir.Operation, key string) (ir.ParameterID, *linkFailure) {
	id, matched := compiledDocumentView.Params(op).Named(key)
	switch matched {
	case 1:
		return id, nil
	case 0:
		return "", invalid("names parameter %q, which operation %q does not declare", key, op.ID)
	default:
		return "", invalid("names parameter %q, which operation %q declares in %d locations", key, op.ID, matched)
	}
}
