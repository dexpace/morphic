// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"cmp"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// ParamNamed returns the parameter of op that key names, as a link's parameters
// key and a replacement name a parameter: `<in>.<name>`, or a name alone. It
// also returns how many parameters key matched, so a caller can tell none from
// several; the ID is only set when that is one.
func ParamNamed(op *ir.Operation, key string) (ir.ParamID, int) {
	if location, name, ok := strings.Cut(key, "."); ok && paramLocation(location) {
		if found := BoundParams(op, ir.HTTPLocation(location), name); len(found) == 1 {
			return found[0], 1
		}
	}
	found := BoundParams(op, "", key)
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

// BoundParams returns the parameters op's HTTP bindings place at location
// under the wire name name, at any location but the host for an empty one. A
// header's name matches in any case, as HTTP compares header names.
func BoundParams(op *ir.Operation, location ir.HTTPLocation, name string) []ir.ParamID {
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
