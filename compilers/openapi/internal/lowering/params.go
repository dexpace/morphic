// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"cmp"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// ParamIndex is an operation's parameters by the location and the wire name
// its HTTP bindings place each at, in binding order: what a link's values and
// parameters keys and a replacement name read. The host's bindings, which no
// such name reaches, are left out. Indexed once, a name costs a map read
// however many names an operation's links or replacements hold.
type ParamIndex struct {
	at map[paramKey][]ir.ParamID
	// locations are those a binding names, in the order bindings first name
	// them, which a lookup at no location reads each of.
	locations []ir.HTTPLocation
}

// paramKey is where a binding places a parameter: its location, and its wire
// name, in lower case for a header, as HTTP compares header names.
type paramKey struct {
	location ir.HTTPLocation
	name     string
}

// keyOf returns the paramKey of name at location.
func keyOf(location ir.HTTPLocation, name string) paramKey {
	if location == ir.HTTPLocationHeader {
		name = strings.ToLower(name)
	}
	return paramKey{location: location, name: name}
}

// IndexParams indexes op's HTTP bindings. A binding naming no wire name places
// its parameter under the parameter's own name, and a parameter bound twice
// at one place is indexed there once.
func IndexParams(op *ir.Operation) ParamIndex {
	names := make(map[ir.ParamID]string, len(op.Params))
	for i := range op.Params {
		names[op.Params[i].ID] = op.Params[i].Name.Source
	}
	ix := ParamIndex{at: map[paramKey][]ir.ParamID{}}
	for _, b := range op.Bindings.HTTP {
		for _, pb := range b.ParamBindings {
			if pb.Location == ir.HTTPLocationHost {
				continue
			}
			if !slices.Contains(ix.locations, pb.Location) {
				ix.locations = append(ix.locations, pb.Location)
			}
			key := keyOf(pb.Location, cmp.Or(pb.WireName, names[pb.Param]))
			if !slices.Contains(ix.at[key], pb.Param) {
				ix.at[key] = append(ix.at[key], pb.Param)
			}
		}
	}
	return ix
}

// Bound returns the parameters bound at location under name, or at any
// location but the host for an empty one. A header's name matches in any
// case, as HTTP compares header names.
func (ix ParamIndex) Bound(location ir.HTTPLocation, name string) []ir.ParamID {
	if location != "" {
		return ix.at[keyOf(location, name)]
	}
	var found []ir.ParamID
	for _, l := range ix.locations {
		for _, id := range ix.at[keyOf(l, name)] {
			if !slices.Contains(found, id) {
				found = append(found, id)
			}
		}
	}
	return found
}

// Named returns the parameter key names, as a link's parameters key and a
// replacement name a parameter: `<in>.<name>`, or a name alone. It also
// returns how many parameters key matched, so a caller can tell none from
// several; the ID is only set when that is one.
func (ix ParamIndex) Named(key string) (ir.ParamID, int) {
	if location, name, ok := strings.Cut(key, "."); ok && paramLocation(location) {
		if found := ix.Bound(ir.HTTPLocation(location), name); len(found) == 1 {
			return found[0], 1
		}
	}
	found := ix.Bound("", key)
	if len(found) == 1 {
		return found[0], 1
	}
	return "", len(found)
}

// paramLocation reports whether location is one a parameter's `in` names.
func paramLocation(location string) bool {
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
