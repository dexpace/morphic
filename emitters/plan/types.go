// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// TypePlan holds what the plan derives of one type
// (docs/emitters/plan/types.md). It picks no target type and cases nothing,
// and it copies no fact the type states outright: the IR keeps Base,
// Implements and Mixins distinct beside the flattening held here. Each field
// is exposed by an SDK and by the docs, which must agree on it.
type TypePlan struct {
	// Deprecated maps each flattened member a declaration deprecates, by the
	// declaration it takes, to that deprecation: an overridden declaration's
	// where the taken one states none.
	Deprecated map[ir.PropertyID]*ir.Deprecation `json:"deprecated,omitempty"`
	// Discrim is the discriminator of a discriminated union or of the model
	// holding one, every route written out as a type ID; nil for any other
	// type, a subtype among them.
	Discrim *DiscrimFacts `json:"discrim,omitzero"`
	// Union is a union's wire shape, read off its Exclusive, WireTagged and
	// Discriminator together; "" = not a union.
	Union UnionShape `json:"union,omitempty"`
	// Alias is the type every reference to this one is emitted as, this one
	// emitting nothing: the end of a decorator's Base chain
	// (docs/emitters/refine/decorators.md); "" = itself.
	Alias ir.TypeID `json:"alias,omitempty"`
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
	// Unflattened are a model's composition entries that name no model, which
	// Flattened cannot hold, in walk order.
	Unflattened []ir.TypeID `json:"unflattened,omitempty"`
	// Sensitive reports that the type, or one its Base chain reaches, is
	// Sensitive.
	Sensitive bool `json:"sensitive,omitzero"`
	// Open reports a union, polymorphic base or enum that keeps a value no
	// variant or member recognizes rather than refusing it: each the IR
	// declares open, and each [Policy.OpenUnions] or [Policy.OpenEnums] opens
	// (docs/emitters/plan/policy.md). A model narrowing a union is not read
	// yet, landing with TypePlan.Composed, and an Open the policy sets is not
	// yet marked Inferred, landing with TypePlan.Inferred.
	Open bool `json:"open,omitzero"`
	// Strict reports a one_of union that refuses a value two of its variants
	// admit unless its constraints single one out, as [Policy.StrictExclusive]
	// asks.
	Strict bool `json:"strict,omitzero"`
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

// typePlans returns the plan of every type in types under pp, facts being
// their document's, and the findings planning them reports, in type-ID order.
// An entry that holds no type, a nil one, plans as the zero TypePlan. No
// flattening is bounded: each model holds its own, so the plan grows with the
// square of a composition's depth, and so do the routes of bases nested along
// it (GitHub #2206).
func typePlans(
	types ir.TypeRegistry, facts *irfacts.DocumentFacts, pp Policy,
) (map[ir.TypeID]TypePlan, []ir.Diagnostic) {
	if len(types) == 0 {
		return nil, nil
	}
	plans := make(map[ir.TypeID]TypePlan, len(types))
	chains := newBaseChains(types)
	var diags []ir.Diagnostic
	for _, id := range slices.Sorted(maps.Keys(types)) {
		typePlan, found := planType(types, facts, chains, id, pp)
		plans[id] = typePlan
		diags = append(diags, found...)
	}
	return plans, diags
}

// planType returns the plan of the type types holds at id under pp, the zero
// TypePlan for a nil one, and what planning it finds.
func planType(
	types ir.TypeRegistry, facts *irfacts.DocumentFacts, chains *baseChains, id ir.TypeID, pp Policy,
) (TypePlan, []ir.Diagnostic) {
	td := types[id]
	if ir.IsNilTypeDef(td) {
		return TypePlan{}, nil
	}
	typePlan := TypePlan{Sensitive: chains.sensitive(id), Alias: chains.alias(id)}
	var diags []ir.Diagnostic
	switch t := td.(type) {
	case *ir.Model:
		flat := ir.FlattenedProperties(types, id)
		typePlan.flatten(flat)
		typePlan.Discrim, diags = hierarchyDiscrim(types, facts, t, flat)
		// A model narrowing a union is open where its union is: not read yet,
		// landing with Composed.
		typePlan.Open = baseOpen(t, pp)
	case *ir.Union:
		typePlan.Union = unionShape(t)
		typePlan.Discrim, diags = unionDiscrim(types, t)
		typePlan.Open = unionOpen(t, pp)
		typePlan.Strict = typePlan.Union == UnionOneOf && pp.StrictExclusive
	case *ir.Enum:
		typePlan.Open = enumOpen(t, pp)
	default:
		// No other kind composes members, has a wire shape to classify or
		// keeps a value it does not list.
	}
	return typePlan, diags
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

// unionOpen reports whether union keeps a variant it does not list
// (docs/emitters/plan/policy.md#a-union-keeps-a-variant-it-does-not-know):
// where the IR declares it open, and under OpenUnions where no request alone
// carries it.
func unionOpen(union *ir.Union, pp Policy) bool {
	return !union.Closed || pp.OpenUnions && !union.InputOnly
}

// baseOpen reports whether model, a polymorphic base where it holds a
// Discriminator or is Abstract, keeps a subtype it does not know: only under
// OpenUnions, where no request alone carries it, since the IR declares no base
// open (docs/emitters/plan/policy.md#a-union-keeps-a-variant-it-does-not-know).
func baseOpen(model *ir.Model, pp Policy) bool {
	polymorphic := model.Discriminator != nil || model.Abstract
	return polymorphic && pp.OpenUnions && !model.InputOnly
}

// enumOpen reports whether enum keeps a value it does not list
// (docs/emitters/plan/policy.md#an-enum-keeps-a-value-it-does-not-list): where
// the IR declares it open, and under OpenEnums unless it declares what such a
// value decodes to, a FallbackMember, or lists no member, admitting no value
// (docs/emitters/refine/enums.md#an-enum-with-no-members).
func enumOpen(enum *ir.Enum, pp Policy) bool {
	return !enum.Closed || pp.OpenEnums && enum.FallbackMember == "" && len(enum.Members) > 0
}
