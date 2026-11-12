// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// lookups indexes what resolving a service's links reads again and again: a
// model's members by wire name, an operation's parameters by where its
// bindings place them, and a response's headers by name. Each list is indexed
// the first time a value reads it and kept for the rest of Resolve, so a value
// costs a map read, and resolving grows with the links and what they read
// rather than with the product of the two.
type lookups struct {
	reg     ir.TypeRegistry
	members map[ir.TypeID]map[string]*ir.Property
	params  map[ir.OpID]lowering.ParamIndex
	headers map[responseKey]map[string][]ir.PropID
}

// newLookups returns lookups over reg that have indexed nothing yet.
func newLookups(reg ir.TypeRegistry) *lookups {
	return &lookups{
		reg:     reg,
		members: map[ir.TypeID]map[string]*ir.Property{},
		params:  map[ir.OpID]lowering.ParamIndex{},
		headers: map[responseKey]map[string][]ir.PropID{},
	}
}

// member returns the declaration the flattening of model takes for the wire
// name name, and false where model is no model or declares no such member.
func (lk *lookups) member(model ir.TypeID, name string) (*ir.Property, bool) {
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

// walk returns the property each token names, from the model root through the
// type of each member it reaches, and false where a step reaches no model, or
// a model declaring no member of that name.
func (lk *lookups) walk(root ir.TypeID, tokens []string) ([]ir.PropID, bool) {
	segments := make([]ir.PropID, 0, len(tokens))
	at := root
	for _, token := range tokens {
		member, ok := lk.member(at, token)
		if !ok {
			return nil, false
		}
		segments = append(segments, member.ID)
		at = member.Type.Target
	}
	return segments, true
}

// paramIndex returns op's parameters by where its HTTP bindings place them.
func (lk *lookups) paramIndex(op *ir.Operation) lowering.ParamIndex {
	ix, indexed := lk.params[op.ID]
	if !indexed {
		ix = lowering.IndexParams(op)
		lk.params[op.ID] = ix
	}
	return ix
}

// header returns the headers the response at declares under name, its case
// aside, as HTTP compares header names.
func (lk *lookups) header(at responseView, name string) []ir.PropID {
	byName, indexed := lk.headers[at.key]
	if !indexed {
		byName = make(map[string][]ir.PropID, len(at.headers))
		for i := range at.headers {
			p := &at.headers[i].Property
			key := strings.ToLower(p.EffectiveWireName())
			byName[key] = append(byName[key], p.ID)
		}
		lk.headers[at.key] = byName
	}
	return byName[strings.ToLower(name)]
}
