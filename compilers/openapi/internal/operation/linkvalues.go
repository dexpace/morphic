// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// exchange is the request and response a link reads values from: a response
// of from, at. lk indexes what the reads name.
type exchange struct {
	lk   *lookups
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
	e, isExpression, err := parseExpression(v.Str)
	switch {
	case err != nil:
		return ir.LinkValue{}, invalid("passes %q, which breaks the runtime expression grammar", v.Str)
	case isExpression:
		return ex.read(e)
	case embedsExpression(v.Str):
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
			if embedsExpression(top.Str) {
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
func (ex exchange) read(e expression) (ir.LinkValue, *linkFailure) {
	switch e.source {
	case exprURL:
		return ir.LinkValue{Kind: ir.LinkURL}, nil
	case exprMethod:
		return ir.LinkValue{Kind: ir.LinkMethod}, nil
	case exprStatusCode:
		return ir.LinkValue{Kind: ir.LinkStatus}, nil
	case exprRequest:
		return ex.request(e)
	default:
		return ex.response(e)
	}
}

// request lowers what e reads of the request: its body or a member of it, or
// a parameter of from.
func (ex exchange) request(e expression) (ir.LinkValue, *linkFailure) {
	if e.part == exprBody {
		segments, failed := bodyPath(ex.lk, ex.from.Request, e.tokens, "request")
		if failed != nil {
			return ir.LinkValue{}, failed
		}
		return ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Segments: segments}}, nil
	}
	found := ex.lk.bound(ex.from).Bound(ir.HTTPLocation(e.part), e.name)
	if len(found) != 1 {
		return ir.LinkValue{}, untyped("reads %s parameter %q, which operation %q does not declare once",
			e.part, e.name, ex.from.ID)
	}
	return ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Param: &found[0]}}, nil
}

// response lowers what e reads of the response: its body or a member of it, or
// one of its headers. A header the response declares twice, in two cases, is
// no one header: either would be a choice the source does not make.
func (ex exchange) response(e expression) (ir.LinkValue, *linkFailure) {
	switch e.part {
	case exprBody:
		segments, failed := bodyPath(ex.lk, ex.at.payload, e.tokens, "response")
		if failed != nil {
			return ir.LinkValue{}, failed
		}
		return ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{Segments: segments}}, nil
	case exprHeader:
		found := ex.lk.header(ex.at, e.name)
		if len(found) != 1 {
			return ir.LinkValue{}, untyped("reads response header %q, which the response does not declare once", e.name)
		}
		return ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{In: "header", Segments: found}}, nil
	default:
		return ir.LinkValue{}, invalid("reads the response's %s parameter %q, which no response carries", e.part, e.name)
	}
}

// bodyPath returns the members tokens walk to from the root of every content
// of p, the body of side, and none for the body whole. Each content must walk
// to the same members: the link reads one value whichever media type arrives.
// A member is one declaration, so two contents that reach the same one first
// walk the same chain from there, and only the first step can tell them apart.
// Each other content's model is flattened for that step, once per Resolve:
// several media types holding distinct models that compose one wide model cost
// that model's width each (#1723).
func bodyPath(lk *lookups, p *ir.Payload, tokens []string, side string) ([]ir.PropID, *linkFailure) {
	if p == nil || len(p.Contents) == 0 {
		return nil, untyped("reads a %s body the document does not describe", side)
	}
	if len(tokens) == 0 {
		return nil, nil
	}
	first := &p.Contents[0]
	segments, ok := lk.walk(first.Type.Target, tokens)
	if !ok {
		return nil, noChain(tokens, side, first.MediaType)
	}
	for i := 1; i < len(p.Contents); i++ {
		ct := &p.Contents[i]
		member, ok := lk.member(ct.Type.Target, tokens[0])
		if !ok {
			return nil, noChain(tokens, side, ct.MediaType)
		}
		if member.ID != segments[0] {
			return nil, untyped("reads /%s, which the %s body's media types hold as different members",
				strings.Join(tokens, "/"), side)
		}
	}
	return segments, nil
}

// noChain is a body read whose tokens walk no chain of members from the root of
// the content of side's body of mediaType.
func noChain(tokens []string, side, mediaType string) *linkFailure {
	return untyped("reads /%s, which the %s body's %s content holds as no chain of members",
		strings.Join(tokens, "/"), side, mediaType)
}

// targetParam returns the parameter of op that a link's parameters key names
// (lowering.ParamIndex.Named), which must name one parameter only.
func targetParam(lk *lookups, op *ir.Operation, key string) (ir.ParamID, *linkFailure) {
	id, matched := lk.bound(op).Named(key)
	switch matched {
	case 1:
		return id, nil
	case 0:
		return "", invalid("names parameter %q, which operation %q does not declare", key, op.ID)
	default:
		return "", invalid("names parameter %q, which operation %q declares in %d locations", key, op.ID, matched)
	}
}
