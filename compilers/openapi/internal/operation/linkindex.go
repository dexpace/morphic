// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"cmp"
	"slices"
	"strings"

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
	params  map[ir.OpID]boundIndex
	headers map[responseKey]map[string][]ir.PropID
}

// newLookups returns lookups over reg that have indexed nothing yet.
func newLookups(reg ir.TypeRegistry) *lookups {
	return &lookups{
		reg:     reg,
		members: map[ir.TypeID]map[string]*ir.Property{},
		params:  map[ir.OpID]boundIndex{},
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

// bound returns the parameters op's HTTP bindings place at location under the
// wire name name, at any location but the host for an empty one. A header's
// name matches in any case, as HTTP compares header names.
func (lk *lookups) bound(op *ir.Operation, location ir.HTTPLocation, name string) []ir.ParamID {
	ix, indexed := lk.params[op.ID]
	if !indexed {
		ix = indexBindings(op)
		lk.params[op.ID] = ix
	}
	return ix.find(location, name)
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

// boundIndex is an operation's parameters by the location and the wire name
// its HTTP bindings place each at, in binding order. The host's bindings, which
// no link names, are left out.
type boundIndex struct {
	at map[boundKey][]ir.ParamID
	// locations are those a binding names, in the order bindings first name
	// them, which a lookup at no location reads each of.
	locations []ir.HTTPLocation
}

// boundKey is where a binding places a parameter: its location, and its wire
// name, in lower case for a header, as HTTP compares header names.
type boundKey struct {
	location ir.HTTPLocation
	name     string
}

// keyOf returns the boundKey of name at location.
func keyOf(location ir.HTTPLocation, name string) boundKey {
	if location == ir.HTTPLocationHeader {
		name = strings.ToLower(name)
	}
	return boundKey{location: location, name: name}
}

// indexBindings indexes op's HTTP bindings. A binding naming no wire name
// places its parameter under the parameter's own name.
func indexBindings(op *ir.Operation) boundIndex {
	names := make(map[ir.ParamID]string, len(op.Params))
	for i := range op.Params {
		names[op.Params[i].ID] = op.Params[i].Name.Source
	}
	ix := boundIndex{at: map[boundKey][]ir.ParamID{}}
	for _, b := range op.Bindings.HTTP {
		for _, pb := range b.ParamBindings {
			if pb.Location == ir.HTTPLocationHost {
				continue
			}
			if !slices.Contains(ix.locations, pb.Location) {
				ix.locations = append(ix.locations, pb.Location)
			}
			key := keyOf(pb.Location, cmp.Or(pb.WireName, names[pb.Param]))
			ix.at[key] = append(ix.at[key], pb.Param)
		}
	}
	return ix
}

// find returns the parameters bound at location under name, or at any location
// for an empty one.
func (ix boundIndex) find(location ir.HTTPLocation, name string) []ir.ParamID {
	if location != "" {
		return ix.at[keyOf(location, name)]
	}
	var found []ir.ParamID
	for _, l := range ix.locations {
		found = append(found, ix.at[keyOf(l, name)]...)
	}
	return found
}
