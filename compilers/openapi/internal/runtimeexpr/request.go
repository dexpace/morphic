// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// shorthandRequest starts the shorthand an extension may write instead of a
// runtime expression naming a value of the request: a dotted chain of names.
const shorthandRequest = "$" + SourceRequest + "."

// RequestValue resolves text, an expression naming a value of op's request: a
// runtime expression reading a parameter or the body, or the shorthand
// `$request.` and a dotted chain of names, the first a parameter's name or,
// where no parameter carries it, a member of the body. It returns the value,
// the type of the parameter or member it names, "" for the body whole, which
// no one declaration types, and why it resolves nothing. Every step reading
// an expression of the request reads it here, so they read it alike.
func RequestValue(w *whole.Document, op *ir.Operation, text string) (*ir.InputPath, ir.TypeID, string) {
	if e, isExpression, err := Parse(text); isExpression && err == nil {
		if e.Source != SourceRequest {
			return nil, "", "names no value of the request"
		}
		if e.Part == PartBody {
			return bodyInput(w, op, e.Tokens)
		}
		found := w.Params(op).Bound(ir.HTTPLocation(e.Part), e.Name)
		if len(found) != 1 {
			return nil, "", "reads " + e.Part + " parameter " + strconv.Quote(e.Name) +
				", which the operation does not declare once"
		}
		return paramInput(w, op, found[0], nil)
	}
	rest, ok := strings.CutPrefix(text, shorthandRequest)
	names := strings.Split(rest, ".")
	if !ok || slices.Contains(names, "") {
		return nil, "", "is no expression naming a value of the request"
	}
	switch found := w.Params(op).Bound("", names[0]); len(found) {
	case 0:
		return bodyInput(w, op, names)
	case 1:
		return paramInput(w, op, found[0], names[1:])
	default:
		return nil, "", "names parameter " + strconv.Quote(names[0]) +
			", which the operation declares in more than one location"
	}
}

// bodyInput is the member of op's request body tokens walk to, the body whole
// for none, and its type.
func bodyInput(w *whole.Document, op *ir.Operation, tokens []string) (*ir.InputPath, ir.TypeID, string) {
	segments, why, ok := BodyPath(w, op.Request, tokens, "request")
	if !ok {
		return nil, "", why
	}
	if len(tokens) == 0 {
		return &ir.InputPath{}, "", ""
	}
	return &ir.InputPath{Segments: segments}, walkedType(w, op.Request.Contents[0].Type.Target, tokens), ""
}

// paramInput is the member of op's parameter id that names walk to, the
// parameter whole for none, and its type.
func paramInput(w *whole.Document, op *ir.Operation, id ir.ParamID, names []string) (*ir.InputPath, ir.TypeID, string) {
	i := slices.IndexFunc(op.Params, func(p ir.Parameter) bool { return p.ID == id })
	if i < 0 {
		// A binding names only a parameter its operation holds.
		return nil, "", "names parameter " + string(id) + ", which a binding names and the operation does not hold"
	}
	root := op.Params[i].Type.Target
	in := &ir.InputPath{Param: &id}
	if len(names) == 0 {
		return in, root, ""
	}
	segments, ok := w.Walk(root, names)
	if !ok {
		return nil, "", "reads " + strings.Join(names, ".") + " of parameter " + strconv.Quote(op.Params[i].Name.Source) +
			", which holds no such chain of members"
	}
	in.Segments = segments
	return in, walkedType(w, root, names), ""
}

// walkedType is the type of the member tokens walk to from root, a walk
// whole.Document.Walk has already taken, so each step reads a kept answer.
// It is "" where a step finds no member, which no such walk leaves.
func walkedType(w *whole.Document, root ir.TypeID, tokens []string) ir.TypeID {
	at := root
	for _, token := range tokens {
		member, ok := w.Member(at, token)
		if !ok {
			return ""
		}
		at = member.Type.Target
	}
	return at
}
