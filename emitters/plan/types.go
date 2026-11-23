// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import "github.com/dexpace/morphic/ir"

// TypePlan holds what the plan derives of one type
// (docs/emitters/plan/types.md). It picks no target type and cases nothing,
// and it copies no fact the type states outright: the IR keeps Base,
// Implements and Mixins distinct beside the flattening held here. Each field
// is exposed by an SDK and by the docs, which must agree on it.
type TypePlan struct {
	// Flattened are a model's members across its composition, each the
	// declaration it takes, in layout order (ir.FlattenedProperties).
	Flattened []ir.PropertyID `json:"flattened,omitempty"`
	// Required are the flattened members an instance must carry: each one a
	// declaration marks Required or a Requires along the composition names.
	Required []ir.PropertyID `json:"required,omitempty"`
	// Undeclared are the names a Requires lists that no flattened member
	// declares, in order: members only a catch-all governs.
	Undeclared []string `json:"undeclared,omitempty"`
	// Redacted are the flattened members a value is redacted at: each with a
	// declaration that is Secret or of a sensitive type.
	Redacted []ir.PropertyID `json:"redacted,omitempty"`
	// Deprecated maps each flattened member a declaration deprecates, by the
	// declaration it takes, to that deprecation: an overridden declaration's
	// where the taken one states none.
	Deprecated map[ir.PropertyID]*ir.Deprecation `json:"deprecated,omitempty"`
	// Sensitive reports that the type, or one its Base chain reaches, is
	// Sensitive.
	Sensitive bool `json:"sensitive,omitzero"`
	// Unflattened are a model's composition entries that name no model, which
	// Flattened cannot hold, in walk order.
	Unflattened []ir.TypeID `json:"unflattened,omitempty"`
	// Union is a union's wire shape, read off its Exclusive, WireTagged and
	// Discriminator together; "" = not a union.
	Union UnionShape `json:"union,omitempty"`
	// Strict reports a one_of union that refuses a value two of its variants
	// admit unless its constraints single one out, as [Policy.StrictExclusive]
	// asks.
	Strict bool `json:"strict,omitzero"`
	// Alias is the type every reference to this one is emitted as, this one
	// emitting nothing: the end of a decorator's Base chain
	// (docs/emitters/refine/decorators.md); "" = itself.
	Alias ir.TypeID `json:"alias,omitempty"`
}

// UnionShape is the wire shape a union's serializer reads
// (docs/ir/unions.md#tag-modes).
type UnionShape string

// The four wire shapes of a union.
const (
	// UnionOneOf is an untagged union exactly one of whose variants holds a
	// value.
	UnionOneOf UnionShape = "one_of"
	// UnionAnyOf is an untagged union one or more of whose variants hold a
	// value.
	UnionAnyOf UnionShape = "any_of"
	// UnionDiscriminated is a union whose payload carries a tag its
	// discriminator locates.
	UnionDiscriminated UnionShape = "discriminated"
	// UnionKeyTagged is a union whose payload is a single-key object, the key
	// naming the variant.
	UnionKeyTagged UnionShape = "key_tagged"
)

// typePlans returns the plan of every type in types under pp. An entry that
// holds no type, a nil one, plans as the zero TypePlan.
func typePlans(types ir.TypeRegistry, pp Policy) map[ir.TypeID]TypePlan {
	if len(types) == 0 {
		return nil
	}
	plans := make(map[ir.TypeID]TypePlan, len(types))
	chains := newBaseChains(types)
	for id, td := range types {
		if ir.IsNilTypeDef(td) {
			plans[id] = TypePlan{}
			continue
		}
		typePlan := TypePlan{Sensitive: chains.sensitive(id), Alias: chains.alias(id)}
		switch t := td.(type) {
		case *ir.Model:
			typePlan.flatten(ir.FlattenedProperties(types, id))
		case *ir.Union:
			typePlan.Union = unionShape(t)
			typePlan.Strict = typePlan.Union == UnionOneOf && pp.StrictExclusive
		default:
			// No other kind composes members or has a wire shape to classify.
		}
		plans[id] = typePlan
	}
	return plans
}

// flatten fills the fields read off flat, the flattening of p's model: its
// members, and which of them are required, redacted and deprecated.
func (p *TypePlan) flatten(flat ir.Flattening) {
	p.Undeclared = flat.Undeclared
	p.Unflattened = flat.Unflattened
	for i := range flat.Properties {
		member := &flat.Properties[i]
		id := member.Property.ID
		p.Flattened = append(p.Flattened, id)
		if member.Required {
			p.Required = append(p.Required, id)
		}
		if member.Secret {
			p.Redacted = append(p.Redacted, id)
		}
		if member.Deprecation == nil {
			continue
		}
		if p.Deprecated == nil {
			p.Deprecated = map[ir.PropertyID]*ir.Deprecation{}
		}
		p.Deprecated[id] = member.Deprecation
	}
}

// unionShape classifies union's wire shape by the tag-mode conventions
// (docs/ir/unions.md#tag-modes): a payload naming its variant is discriminated
// where a discriminator locates the tag and key-tagged where none does, and
// any other union is untagged, whatever discriminator it carries.
func unionShape(union *ir.Union) UnionShape {
	switch {
	case union.WireTagged && union.Discriminator != nil:
		return UnionDiscriminated
	case union.WireTagged:
		return UnionKeyTagged
	case union.Exclusive:
		return UnionOneOf
	default:
		return UnionAnyOf
	}
}
