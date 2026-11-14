// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr

import (
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// ResponseKey names one response of a service, which Lookups index the headers
// of: the operation carrying it, which of that operation's lists of responses
// it sits in, and where in the list. List is numbered as the caller likes, and
// two responses of an operation differ in List or in Index.
type ResponseKey struct {
	Op    ir.OpID
	List  int
	Index int
}

// Lookups index what resolving expressions reads again and again: a model's
// members by wire name, an operation's parameters by where its bindings place
// them, and a response's headers by name. Each list is indexed the first time a
// value reads it and kept, so a value costs a map read, and resolving grows
// with the expressions and what they read rather than with the product of the
// two.
type Lookups struct {
	reg     ir.TypeRegistry
	members map[ir.TypeID]map[string]*ir.Property
	params  map[ir.OpID]lowering.ParamIndex
	headers map[ResponseKey]map[string][]ir.PropID
}

// NewLookups returns Lookups over reg that have indexed nothing yet.
func NewLookups(reg ir.TypeRegistry) *Lookups {
	return &Lookups{
		reg:     reg,
		members: map[ir.TypeID]map[string]*ir.Property{},
		params:  map[ir.OpID]lowering.ParamIndex{},
		headers: map[ResponseKey]map[string][]ir.PropID{},
	}
}

// Member returns the declaration the flattening of model takes for the wire
// name name, and false where model is no model or declares no such member.
func (lk *Lookups) Member(model ir.TypeID, name string) (*ir.Property, bool) {
	byName, indexed := lk.members[model]
	if !indexed {
		byName = membersByWireName(lk.reg, model)
		lk.members[model] = byName
	}
	p, ok := byName[name]
	return p, ok
}

// membersByWireName indexes the members of the model reg holds at id by wire
// name: the declaration its flattening takes for each, which holds one per
// wire name. It returns nil for any type but a model.
func membersByWireName(reg ir.TypeRegistry, id ir.TypeID) map[string]*ir.Property {
	if _, ok := reg[id].(*ir.Model); !ok {
		return nil
	}
	flat := ir.FlattenedProperties(reg, id)
	byName := make(map[string]*ir.Property, len(flat.Properties))
	for i := range flat.Properties {
		p := flat.Properties[i].Property
		byName[p.EffectiveWireName()] = p
	}
	return byName
}

// Walk returns the property each token names, from the model root through the
// type of each member it reaches, and false where a step reaches no model, or
// a model declaring no member of that name.
func (lk *Lookups) Walk(root ir.TypeID, tokens []string) ([]ir.PropID, bool) {
	segments := make([]ir.PropID, 0, len(tokens))
	at := root
	for _, token := range tokens {
		member, ok := lk.Member(at, token)
		if !ok {
			return nil, false
		}
		segments = append(segments, member.ID)
		at = member.Type.Target
	}
	return segments, true
}

// ParamIndex returns op's parameters by where its HTTP bindings place them.
func (lk *Lookups) ParamIndex(op *ir.Operation) lowering.ParamIndex {
	ix, indexed := lk.params[op.ID]
	if !indexed {
		ix = lowering.IndexParams(op)
		lk.params[op.ID] = ix
	}
	return ix
}

// Header returns the headers among headers that declare name, its case aside,
// as HTTP compares header names. headers are those of the response at names:
// the first read of a response indexes them and a later read takes that index,
// so reads of one key pass the same headers.
func (lk *Lookups) Header(at ResponseKey, headers []ir.Header, name string) []ir.PropID {
	byName, indexed := lk.headers[at]
	if !indexed {
		byName = make(map[string][]ir.PropID, len(headers))
		for i := range headers {
			p := &headers[i].Property
			key := strings.ToLower(p.EffectiveWireName())
			byName[key] = append(byName[key], p.ID)
		}
		lk.headers[at] = byName
	}
	return byName[strings.ToLower(name)]
}
