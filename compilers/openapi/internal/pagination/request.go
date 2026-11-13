// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"fmt"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// role is what a query parameter's name says it sends a page walk: where the
// walk continues under a strategy, or the page size.
type role struct {
	strategy ir.PageStrategy
	limit    bool
}

// roleOf returns the role a canonical name gives a parameter, the zero role
// for a name giving none. It is the vocabulary
// docs/compilers/openapi.md#pagination-policy states, compared by canonical
// words, so `pageSize` and `page_size` are one name.
func roleOf(canonical string) role {
	switch canonical {
	case "page", "page_number":
		return role{strategy: ir.PageStrategyPage}
	case "offset":
		return role{strategy: ir.PageStrategyOffset}
	case "cursor":
		return role{strategy: ir.PageStrategyCursor}
	case "page_token", "next_token", "continuation_token":
		return role{strategy: ir.PageStrategyToken}
	case "per_page", "page_size", "limit":
		return role{limit: true}
	default:
		return role{}
	}
}

// inputs are the query parameters a walk sends: cursor carries where it
// continues under strategy, prim being its primitive, and limit is the page
// size, nil where none is named.
type inputs struct {
	strategy ir.PageStrategy
	cursor   *ir.Parameter
	prim     ir.PrimKind
	limit    *ir.Parameter
}

// readsNextCursor reports whether the walk sends a value a page hands back,
// rather than one it counts.
func (in inputs) readsNextCursor() bool {
	return in.strategy == ir.PageStrategyCursor || in.strategy == ir.PageStrategyToken
}

// requestInputs returns the inputs op's query parameters name. named reports
// whether one names where a walk continues; where one does and they give no
// walk, why says what stops it: two naming a continuation or a page size, or
// one of a type a walk cannot send.
func requestInputs(types ir.TypeRegistry, op *ir.Operation) (in inputs, why string, named bool) {
	var continuations, limits []*ir.Parameter
	for i := range op.Params {
		p := &op.Params[i]
		switch r := roleOf(p.Name.Canonical); {
		case r == (role{}) || !boundInQuery(op, p.ID):
		case r.limit:
			limits = append(limits, p)
		default:
			continuations = append(continuations, p)
		}
	}
	switch {
	case len(continuations) == 0:
		return inputs{}, "", false
	case len(continuations) > 1:
		return inputs{}, fmt.Sprintf("query parameters %s each name where a walk continues", quoted(continuations)), true
	case len(limits) > 1:
		return inputs{}, fmt.Sprintf("query parameters %s each name the page size", quoted(limits)), true
	}
	cursor := continuations[0]
	prim, ok := sendable(types, cursor.Type.Target)
	if !ok {
		return inputs{}, notSendable(cursor, "where a walk continues"), true
	}
	in = inputs{strategy: roleOf(cursor.Name.Canonical).strategy, cursor: cursor, prim: prim}
	if len(limits) == 1 {
		if _, ok := sendable(types, limits[0].Type.Target); !ok {
			return inputs{}, notSendable(limits[0], "the page size"), true
		}
		in.limit = limits[0]
	}
	return in, "", true
}

// boundInQuery reports whether an HTTP binding of op places the parameter id
// in the query.
func boundInQuery(op *ir.Operation, id ir.ParamID) bool {
	for _, b := range op.Bindings.HTTP {
		for _, pb := range b.ParamBindings {
			if pb.Param == id && pb.Location == ir.HTTPLocationQuery {
				return true
			}
		}
	}
	return false
}

// sendable returns the primitive the type id stands for, read through alias
// scalars, when a walk can send it or count with it (sendableAs).
func sendable(types ir.TypeRegistry, id ir.TypeID) (ir.PrimKind, bool) {
	return sendableAs(resolve(types, id))
}

// sendableAs returns td's primitive when a walk can send it or count with it:
// a number of any kind, which a value of it holding no integer leaves none to
// count with, or a string, which carries a number as its decimal text.
func sendableAs(td ir.TypeDef) (ir.PrimKind, bool) {
	p, ok := td.(*ir.Primitive)
	if !ok {
		return "", false
	}
	k := p.Prim
	if k == ir.PrimString || k == ir.PrimNumber || k.StrictlyWithin(ir.PrimNumber) {
		return k, true
	}
	return "", false
}

// notSendable says that p names what a walk sends as what, and is of a type it
// cannot send.
func notSendable(p *ir.Parameter, what string) string {
	return fmt.Sprintf("query parameter %q names %s, and its type is no number or string", p.Name.Source, what)
}

// quoted lists the parameters' names, two or more, as listed does.
func quoted(params []*ir.Parameter) string {
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = strconv.Quote(p.Name.Source)
	}
	return listed(names)
}
