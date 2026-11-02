// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"cmp"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// exchange is the request and response a link reads values from: a response
// of from, at.
type exchange struct {
	reg  ir.TypeRegistry
	from *ir.Operation
	at   responseView
}

// value lowers one value a link passes as node writes it: a runtime
// expression whole is a value of the exchange, any other value a constant. A
// string an expression is substituted into is a value no ir.LinkValue spells.
func (ex exchange) value(node *yaml.Node) (ir.LinkValue, *linkFailure) {
	v, err := value.FromNode(node)
	if err != nil {
		return ir.LinkValue{}, untyped("passes a value the IR has no constant for: %v", err)
	}
	if v.Kind != ir.ValueString {
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
		segments, failed := bodyPath(ex.reg, ex.from.Request, e.tokens, "request")
		if failed != nil {
			return ir.LinkValue{}, failed
		}
		return ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Segments: segments}}, nil
	}
	found := boundParams(ex.from, ir.HTTPLocation(e.part), e.name)
	if len(found) != 1 {
		return ir.LinkValue{}, untyped("reads %s parameter %q, which operation %q does not declare once",
			e.part, e.name, ex.from.ID)
	}
	return ir.LinkValue{Kind: ir.LinkRequest, Request: &ir.InputPath{Param: &found[0]}}, nil
}

// response lowers what e reads of the response: its body or a member of it, or
// one of its headers.
func (ex exchange) response(e expression) (ir.LinkValue, *linkFailure) {
	switch e.part {
	case exprBody:
		segments, failed := bodyPath(ex.reg, ex.at.payload, e.tokens, "response")
		if failed != nil {
			return ir.LinkValue{}, failed
		}
		return ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{Segments: segments}}, nil
	case exprHeader:
		for i := range ex.at.headers {
			if h := &ex.at.headers[i].Property; strings.EqualFold(h.EffectiveWireName(), e.name) {
				return ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{In: "header", Segments: []ir.PropID{h.ID}}}, nil
			}
		}
		return ir.LinkValue{}, untyped("reads response header %q, which the response does not declare", e.name)
	default:
		return ir.LinkValue{}, invalid("reads the response's %s parameter %q, which no response carries", e.part, e.name)
	}
}

// bodyPath returns the members tokens walk to from the root of every content
// of p, the body of side, and none for the body whole. Each content must walk
// to the same members: the link reads one value whichever media type arrives.
func bodyPath(reg ir.TypeRegistry, p *ir.Payload, tokens []string, side string) ([]ir.PropID, *linkFailure) {
	if p == nil || len(p.Contents) == 0 {
		return nil, untyped("reads a %s body the document does not describe", side)
	}
	if len(tokens) == 0 {
		return nil, nil
	}
	var first []ir.PropID
	for i := range p.Contents {
		ct := &p.Contents[i]
		segments, ok := walkMembers(reg, ct.Type.Target, tokens)
		if !ok {
			return nil, untyped("reads /%s, which the %s body's %s content holds as no chain of members",
				strings.Join(tokens, "/"), side, ct.MediaType)
		}
		if i > 0 && !slices.Equal(segments, first) {
			return nil, untyped("reads /%s, which the %s body's media types hold as different members",
				strings.Join(tokens, "/"), side)
		}
		first = segments
	}
	return first, nil
}

// walkMembers returns the property each token names, from the model root
// through the type of each member it reaches, and false where a step reaches
// no model, or a model declaring no member of that name.
func walkMembers(reg ir.TypeRegistry, root ir.TypeID, tokens []string) ([]ir.PropID, bool) {
	segments := make([]ir.PropID, 0, len(tokens))
	at := root
	for _, token := range tokens {
		if _, ok := reg[at].(*ir.Model); !ok {
			return nil, false
		}
		member := memberNamed(ir.FlattenedProperties(reg, at), token)
		if member == nil {
			return nil, false
		}
		segments = append(segments, member.ID)
		at = member.Type.Target
	}
	return segments, true
}

// memberNamed returns the declaration flat takes for the wire name name, nil
// for none.
func memberNamed(flat ir.Flattening, name string) *ir.Property {
	for i := range flat.Properties {
		if p := flat.Properties[i].Property; p.EffectiveWireName() == name {
			return p
		}
	}
	return nil
}

// targetParam returns the parameter of op that a link's parameters key names:
// `<location>.<name>`, or a name alone, which must name one parameter only.
func targetParam(op *ir.Operation, key string) (ir.ParamID, *linkFailure) {
	if location, name, ok := strings.Cut(key, "."); ok && isParamLocation(location) {
		if found := boundParams(op, ir.HTTPLocation(location), name); len(found) == 1 {
			return found[0], nil
		}
	}
	found := boundParams(op, "", key)
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", invalid("names parameter %q, which operation %q does not declare", key, op.ID)
	default:
		return "", invalid("names parameter %q, which operation %q declares in %d locations", key, op.ID, len(found))
	}
}

// isParamLocation reports whether location is one a parameter's `in` names,
// which a link's parameters key qualifies a name with.
func isParamLocation(location string) bool {
	switch ir.HTTPLocation(location) {
	case ir.HTTPLocationPath, ir.HTTPLocationQuery, ir.HTTPLocationQuerystring, ir.HTTPLocationHeader,
		ir.HTTPLocationCookie:
		return true
	case ir.HTTPLocationBody, ir.HTTPLocationBodyProperty, ir.HTTPLocationHost:
		return false
	default:
		return false
	}
}

// boundParams returns the parameters op's HTTP bindings place at location
// under the wire name name, at any location but the host for an empty one. A
// header's name matches in any case, as HTTP compares header names.
func boundParams(op *ir.Operation, location ir.HTTPLocation, name string) []ir.ParamID {
	names := make(map[ir.ParamID]string, len(op.Params))
	for i := range op.Params {
		names[op.Params[i].ID] = op.Params[i].Name.Source
	}
	var found []ir.ParamID
	for _, b := range op.Bindings.HTTP {
		for _, pb := range b.ParamBindings {
			if pb.Location == ir.HTTPLocationHost || location != "" && pb.Location != location {
				continue
			}
			wire := cmp.Or(pb.WireName, names[pb.Param])
			matches := wire == name || pb.Location == ir.HTTPLocationHeader && strings.EqualFold(wire, name)
			if matches && !slices.Contains(found, pb.Param) {
				found = append(found, pb.Param)
			}
		}
	}
	return found
}
