// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import "github.com/dexpace/morphic/ir"

// baseChains reads what a type takes from the types its Base chain reaches,
// each chain once however many types lead into it: a walk settles every type
// it passed, so the walks of one registry together take a step per type and
// one per question.
type baseChains struct {
	types ir.TypeRegistry
	// sensitivity holds, for each type with a Base a walk has passed, whether
	// its chain reaches a Sensitive type: false while the type is on the chain
	// being read, so a chain returning to it ends there.
	sensitivity map[ir.TypeID]bool
	// aliases holds, for each decorator a walk has passed, the type its chain
	// ends at, "" where it ends at none.
	aliases map[ir.TypeID]ir.TypeID
	// passed are the decorators on the chain being read, emptied after each
	// walk.
	passed map[ir.TypeID]struct{}
	// steps counts the turns the walks have taken, each reading one type: what
	// they cost.
	steps int
}

// newBaseChains returns the chains of types, none read yet.
func newBaseChains(types ir.TypeRegistry) *baseChains {
	return &baseChains{
		types:       types,
		sensitivity: map[ir.TypeID]bool{},
		aliases:     map[ir.TypeID]ir.TypeID{},
		passed:      map[ir.TypeID]struct{}{},
	}
}

// sensitive reports whether id names a type that is Sensitive, or whose Base
// chain reaches one, as ir.FlattenedProperties reads a member's type. A chain
// is followed through models and scalars, the kinds with a Base, and ends at a
// type the registry does not hold.
func (c *baseChains) sensitive(id ir.TypeID) bool {
	var chain []ir.TypeID
	found := false
	// Each turn ends the walk or marks a type not marked before, so the walk
	// takes no more turns than the registry holds types, and one.
	for {
		c.steps++
		if known, read := c.sensitivity[id]; read {
			found = known
			break
		}
		td := c.types[id]
		if ir.IsNilTypeDef(td) {
			break
		}
		if td.Common().Sensitive {
			found = true
			break
		}
		base := baseOf(td)
		if base == nil {
			break
		}
		c.sensitivity[id] = false
		chain = append(chain, id)
		id = base.Target
	}
	for _, passed := range chain {
		c.sensitivity[passed] = found
	}
	return found
}

// baseOf returns the Base of a model or a scalar, nil where it has none, and
// nil for every other kind, none of which has one.
func baseOf(td ir.TypeDef) *ir.TypeRef {
	switch t := td.(type) {
	case *ir.Model:
		return t.Base
	case *ir.Scalar:
		return t.Base
	default:
		return nil
	}
}

// alias returns the type every reference to id is emitted as
// (docs/emitters/refine/decorators.md): for a decorator, the first type along
// its Base chain that is not one. It is "" for a type that is no decorator,
// and for a decorator collapsing nowhere: one whose chain returns to a
// decorator it passed, or ends at a type the registry does not hold.
func (c *baseChains) alias(id ir.TypeID) ir.TypeID {
	if settled, read := c.aliases[id]; read {
		return settled
	}
	var chain []ir.TypeID
	var end ir.TypeID
	// Each turn ends the walk or passes a decorator not passed before, so the
	// walk takes no more turns than the registry holds types, and one.
	for at := id; ; {
		c.steps++
		if settled, read := c.aliases[at]; read {
			end = settled
			break
		}
		if _, returned := c.passed[at]; returned {
			break
		}
		td := c.types[at]
		base, decorator := ir.Decorated(td)
		if !decorator {
			if !ir.IsNilTypeDef(td) {
				end = at
			}
			break
		}
		c.passed[at] = struct{}{}
		chain = append(chain, at)
		at = base.Target
	}
	clear(c.passed)
	if len(chain) == 0 {
		return ""
	}
	for _, decorator := range chain {
		c.aliases[decorator] = end
	}
	return end
}
