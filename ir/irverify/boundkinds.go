// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"github.com/dexpace/morphic/ir"
)

// checkBoundKinds asserts each structural node holds only the bounds of its kind
// (docs/ir/constraints.md#constraints): a Model or a MapT the property counts,
// a List or a Tuple the item counts and uniqueness. Any other bound applies to
// instances no such node admits, so it bounds nothing there, and a consumer
// reading it would hold a value to a rule its source does not state. A Scalar's,
// a Union's and a carrier's bounds turn on an encoding, variants or what is
// named, which only a compiler judges, so none is read here.
func checkBoundKinds(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		kind, foreign := foreignBounds(td)
		if !foreign {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/bound-of-another-kind",
			Message: kind + " holds a bound that applies to instances of another kind",
			Path:    typePath(id) + ".Constraints",
		})
	}
	return vs
}

// foreignBounds reports whether td, a Model, a MapT, a List or a Tuple, holds a
// bound its kind does not take, and the kind's name. Any other node, a nil one
// among them, which checkRegistryKeys reports, is not asked.
func foreignBounds(td ir.TypeDef) (string, bool) {
	switch n := td.(type) {
	case *ir.Model:
		return "model", n != nil && beyondProperties(n.Constraints)
	case *ir.MapT:
		return "map", n != nil && beyondProperties(n.Constraints)
	case *ir.List:
		return "list", n != nil && beyondItems(n.Constraints)
	case *ir.Tuple:
		return "tuple", n != nil && beyondItems(n.Constraints)
	default:
		return "", false
	}
}

// beyondProperties reports whether constraints holds a bound other than the
// property counts.
func beyondProperties(constraints *ir.Constraints) bool {
	if constraints == nil {
		return false
	}
	rest := *constraints
	rest.MinProperties, rest.MaxProperties = nil, nil
	return rest != ir.Constraints{}
}

// beyondItems reports whether constraints holds a bound other than the item
// counts and uniqueness. A false UniqueItems states nothing, as it is the
// field's zero.
func beyondItems(constraints *ir.Constraints) bool {
	if constraints == nil {
		return false
	}
	rest := *constraints
	rest.MinItems, rest.MaxItems, rest.UniqueItems = nil, nil, false
	return rest != ir.Constraints{}
}
