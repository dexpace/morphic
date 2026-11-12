// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// maxTagAliasHops bounds how many alias scalars pinnedValue reads a tag
// property's type through (styleguide bounded-everything rule), as
// maxBodyAliasHops bounds a body's. A longer chain, or a loop, pins nothing.
const maxTagAliasHops = 64

// maxReportedTags bounds how many variants the inferred-discriminator report
// names, so a union of hundreds says what it means in one line.
const maxReportedTags = 3

// deferUnionTags queues the discriminator-inference policy's reading of the
// union id, which s writes at pointer, where the policy is on: for a
// discriminator declaring no mapping, or for none at all
// (docs/compilers/openapi.md#discriminator-inference). It reads what each
// variant's type says of its tag, which only a registry every node is interned
// in answers in any declaration order, so it is a settlement.
func deferUnionTags(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, id ir.TypeID, pointer jsontext.Pointer) {
	if !c.DiscriminatorInference.Honours() {
		return
	}
	d := s.GetDiscriminator()
	if d.GetMapping().Len() > 0 {
		return // the document maps every tag it routes
	}
	tags := unionTags{c: c, id: id, pointer: pointer}
	if d != nil {
		tags.at = pointer + ids.Ptr("discriminator")
		tags.implicit = implicitTags(c, s)
	}
	ts.Defer(tags)
}

// implicitTags returns, per variant of the union s writes, the tag OpenAPI's
// implicit mapping routes it by, the name of the component its $ref branch
// names, and "" for a variant it cannot route: one written inline, or a $ref
// to any other position. A null branch has no variant.
func implicitTags(c lowering.Ctx, s *oas3.Schema) []string {
	scope := c.RefScope()
	branches, _, _ := unionBranches(s)
	tags := make([]string, 0, len(branches))
	for _, b := range branches {
		if isNullSchema(b) {
			continue
		}
		name, _, _ := componentNamed(scope, b)
		tags = append(tags, name)
	}
	return tags
}

// unionTags settles the tag of the union id under the discriminator-inference
// policy, from the value each variant pins its tag property to: a mapping for
// the discriminator written at at, which declares none, or, where at is empty,
// a discriminator for a union declaring none. What it finds, a member's
// declarations and their types, no other settlement changes: one swapping a
// model for its map swaps one with no member, which pins nothing either way,
// so the settling order changes no answer.
type unionTags struct {
	c        lowering.Ctx
	id       ir.TypeID
	pointer  jsontext.Pointer // where the union is written
	at       jsontext.Pointer // the discriminator's, "" for none
	implicit []string         // implicitTags, per variant; nil for no discriminator
}

// Settle reads the union's tags, and writes them where the policy reads one.
func (t unionTags) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	u, ok := reg[t.id].(*ir.Union)
	if !ok || u == nil {
		return t.invariant("is not registered as one")
	}
	if t.at != "" {
		return t.mapDeclared(reg, u)
	}
	return t.inferUndeclared(reg, u)
}

// invariant reports a union the settlement finds otherwise than its lowering
// left it, a compiler bug: the reading is not taken.
func (t unionTags) invariant(why string) []ir.Diagnostic {
	return []ir.Diagnostic{t.c.DiagAt(ir.SeverityError, diag.InternalInvariant, t.pointer,
		"internal: union %q %s, so the discriminator inference deferred for it is not taken", t.id, why)}
}

// mapDeclared maps the declared discriminator of u by the value each variant
// pins its tag property to, where every variant pins a distinct one and the
// implicit mapping routes some variant by another: the component name it
// routes by is a tag that variant rejects. Where each pinned value is that
// name, the implicit mapping already says it.
func (t unionTags) mapDeclared(reg ir.TypeRegistry, u *ir.Union) []ir.Diagnostic {
	d := u.Discriminator
	if d == nil {
		return t.invariant("holds no discriminator, though its source declares one")
	}
	if len(t.implicit) != len(u.Variants) {
		return t.invariant("has a variant no branch stands for")
	}
	tags, ok := pinnedTags(reg, membersOf(reg, u.Variants), d.PropertyName, false /* required */)
	if !ok || slices.Equal(tags, t.implicit) {
		return nil
	}
	d.Mapping = tagMapping(tags, u.Variants)
	u.Provenance.Inferred = ir.JoinInferred(u.Provenance.Inferred, lowering.DiscriminatorInferenceHeuristic)
	return []ir.Diagnostic{t.c.DiagAt(ir.SeverityInfo, diag.InferredDiscriminator, t.at,
		"the discriminator declares no mapping, and OpenAPI's implicit mapping routes each variant by the "+
			"name of the component it names, a tag a variant pinning %q to another value rejects (%s); "+
			"mapped by the pinned values, marked inferred",
		d.PropertyName, disagreements(tags, t.implicit))}
}

// inferUndeclared gives u, a closed union of two or more variants declaring no
// discriminator, the one its variants carry in all but name: the first property
// of the first variant, in layout order, that every variant requires and pins
// to a distinct value. A payload then names its variant by that value, as
// validation would find it. An open union may meet a variant carrying any tag.
// The other variants are flattened once, and only where the first pins one,
// each keyed by wire name, so a candidate costs the same however many members
// a variant has.
func (t unionTags) inferUndeclared(reg ir.TypeRegistry, u *ir.Union) []ir.Diagnostic {
	if u.Discriminator != nil {
		return t.invariant("holds a discriminator, though its source declares none")
	}
	if !u.Closed || len(u.Variants) < 2 ||
		slices.ContainsFunc(u.Variants, func(v ir.Variant) bool { return v.Type.Nullable }) {
		return nil
	}
	var members []map[string]ir.FlatProperty
	for _, p := range ir.FlattenedProperties(reg, u.Variants[0].Type.Target).Properties {
		if _, ok := pinnedMember(reg, p, true /* required */); !ok {
			continue
		}
		if members == nil {
			members = membersOf(reg, u.Variants)
		}
		name := p.Property.EffectiveWireName()
		tags, ok := pinnedTags(reg, members, name, true /* required */)
		if !ok {
			continue
		}
		u.Discriminator = &ir.Discriminator{PropertyName: name, Mapping: tagMapping(tags, u.Variants), Inferred: true}
		u.WireTagged = true
		u.Provenance.Inferred = ir.JoinInferred(u.Provenance.Inferred, lowering.DiscriminatorInferenceHeuristic)
		return nil
	}
	return nil
}

// membersOf returns the members of each variant across its composition
// (ir.FlattenedProperties), keyed by wire name, in variant order.
func membersOf(reg ir.TypeRegistry, variants []ir.Variant) []map[string]ir.FlatProperty {
	members := make([]map[string]ir.FlatProperty, len(variants))
	for i, v := range variants {
		props := ir.FlattenedProperties(reg, v.Type.Target).Properties
		byName := make(map[string]ir.FlatProperty, len(props))
		for _, p := range props {
			byName[p.Property.EffectiveWireName()] = p
		}
		members[i] = byName
	}
	return members
}

// pinnedTags returns the tag each variant's member of the property name pins,
// members holding them per variant (membersOf), in variant order, and false
// unless every one pins one and no two pin the same: a mapping routes a tag to
// one variant. Where required is set, each variant must require the property
// too, as a tag no payload need carry names no variant.
func pinnedTags(reg ir.TypeRegistry, members []map[string]ir.FlatProperty, name string, required bool) ([]string, bool) {
	tags := make([]string, 0, len(members))
	seen := make(map[string]bool, len(members))
	for _, byName := range members {
		p, ok := byName[name]
		if !ok {
			return nil, false
		}
		tag, ok := pinnedMember(reg, p, required)
		if !ok || seen[tag] {
			return nil, false
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags, true
}

// pinnedMember returns the string the member p holds its value to, read off
// every declaration it conjoins, the one it takes and those it overrides, as an
// instance meets them all: one is a reference to a string Literal or to a
// closed Enum of one string member, read through alias scalars, none pins
// another, and not every one admits null. So a composed variant's body
// redeclaring its tag as a string (docs/ir/models.md#composition-beside-a-union)
// leaves its branch's pin in force. Where required is set, the member must be
// required.
func pinnedMember(reg ir.TypeRegistry, p ir.FlatProperty, required bool) (string, bool) {
	if required && !p.Required {
		return "", false
	}

	tag, pinned := pinnedValue(reg, p.Property.Type.Target)
	nullable := p.Property.Type.Nullable
	for _, d := range p.Overridden {
		nullable = nullable && d.Property.Type.Nullable
		other, pins := pinnedValue(reg, d.Property.Type.Target)
		if !pins {
			continue
		}
		if pinned && other != tag {
			return "", false // no string meets both
		}
		tag, pinned = other, true
	}

	if !pinned || nullable {
		return "", false
	}
	return tag, true
}

// pinnedValue returns the one string the type id admits, read through alias
// scalars: a Literal's, or the member of a closed Enum of one.
func pinnedValue(reg ir.TypeRegistry, id ir.TypeID) (string, bool) {
	var v ir.Value
	switch n := reg[throughAliases(reg, id)].(type) {
	case *ir.Literal:
		if n == nil {
			return "", false
		}
		v = n.Value
	case *ir.Enum:
		if n == nil || !n.Closed || n.Flags || len(n.Members) != 1 {
			return "", false
		}
		v = n.Members[0].Value
	default:
		return "", false
	}
	return v.Str, v.Kind == ir.ValueString
}

// throughAliases returns the type the alias scalars from id stand over, at
// most maxTagAliasHops of them read.
func throughAliases(reg ir.TypeRegistry, id ir.TypeID) ir.TypeID {
	for range maxTagAliasHops {
		base, alias := ir.AliasBase(reg, id)
		if !alias {
			break
		}
		id = base
	}
	return id
}

// tagMapping maps each tag to the variant pinning it, tags being in variant
// order.
func tagMapping(tags []string, variants []ir.Variant) map[string]ir.TypeID {
	mapping := make(map[string]ir.TypeID, len(tags))
	for i, tag := range tags {
		mapping[tag] = variants[i].Type.Target
	}
	return mapping
}

// disagreements names the first maxReportedTags variants whose pinned tag is
// not the one the implicit mapping routes them by, and how many more there are.
func disagreements(tags, implicit []string) string {
	var named []string
	var more int
	for i, tag := range tags {
		if tag == implicit[i] {
			continue
		}
		if len(named) == maxReportedTags {
			more++
			continue
		}
		route := "names none"
		if implicit[i] != "" {
			route = "routes by " + strconv.Quote(implicit[i])
		}
		named = append(named, strconv.Quote(tag)+" where it "+route)
	}
	if more > 0 {
		named = append(named, strconv.Itoa(more)+" more")
	}
	return strings.Join(named, ", ")
}
