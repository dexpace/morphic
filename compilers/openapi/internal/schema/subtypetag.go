// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// deferSubtypeTag queues the discriminator-inference policy's reading of the
// tag the subtype id, written at pointer, carries where a discriminator routes
// it by its name (tag.byName) and the policy is on: the strings it admits for
// the tag property may rule the name out
// (docs/compilers/openapi.md#discriminator-inference). They are read off the
// types of its members, which only a registry every node is interned in
// answers in any declaration order, so it is a settlement.
func deferSubtypeTag(c lowering.Ctx, ts *compile.Types, tag subtypeTag, id ir.TypeID, pointer jsontext.Pointer) {
	if tag.byName == nil || !c.DiscriminatorInference.Honours() {
		return
	}
	ts.Defer(subtypeTagReading{c: c, id: id, pointer: pointer, property: tag.byName.GetPropertyName(), name: tag.value})
}

// subtypeTagReading settles the tag of the subtype id under the
// discriminator-inference policy. What it reads, a member's declarations and
// their types, no other settlement changes, as unionTags says, so the settling
// order changes no answer. One is deferred per subtype, so N subtypes sharing a
// tag enum of M strings read it N times (#1008).
type subtypeTagReading struct {
	c        lowering.Ctx
	id       ir.TypeID
	pointer  jsontext.Pointer // where the subtype is written
	property string           // the tag property's wire name
	name     string           // the component name the discriminator routes it by
}

// Settle reads the strings the subtype admits for its tag and, where they rule
// its name out, tags it by the one it admits, or by none where it admits
// several or none, as no one tag then routes to it. Either is marked and
// reported.
func (t subtypeTagReading) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	m, ok := reg[t.id].(*ir.Model)
	if !ok || m == nil || m.DiscriminatorValue != t.name {
		return []ir.Diagnostic{t.c.DiagAt(ir.SeverityError, diag.InternalInvariant, t.pointer,
			"internal: subtype %q is not registered as the model its lowering tagged %q, "+
				"so the discriminator inference deferred for it is not taken", t.id, t.name)}
	}
	tags, closed := admittedTags(reg, t.id, t.property)
	if !closed || slices.Contains(tags, t.name) {
		return nil
	}
	m.Provenance.Inferred = ir.JoinInferred(m.Provenance.Inferred, lowering.DiscriminatorInferenceHeuristic)
	outcome := "no one tag routes to it, so it carries none"
	if len(tags) == 1 {
		m.DiscriminatorValue = tags[0]
		outcome = "it carries " + strconv.Quote(tags[0]) + " instead"
	} else {
		m.DiscriminatorValue = ""
	}
	return []ir.Diagnostic{t.c.DiagAt(ir.SeverityInfo, diag.InferredDiscriminator, t.pointer,
		"the discriminator routes this schema by its name, %q, a tag its %q rejects, admitting %s; %s, marked inferred",
		t.name, t.property, admittedList(tags), outcome)}
}

// admittedTags returns the strings an instance of the model id may hold its
// member property to, sorted, and false where it may hold any: those every
// declaration of the member admits (memberStrings). A member the model does
// not declare, or none of whose declarations bounds it (typeStrings), admits
// any. What a union the model composes admits is not read: each subtype
// composing one wide union would flatten every variant again (#1712).
func admittedTags(reg ir.TypeRegistry, id ir.TypeID, property string) ([]string, bool) {
	p, ok := flatMember(ir.FlattenedProperties(reg, id), property)
	if !ok {
		return nil, false
	}
	return memberStrings(reg, p)
}

// flatMember returns the member of flat whose wire name is property.
func flatMember(flat ir.Flattening, property string) (ir.FlatProperty, bool) {
	i := slices.IndexFunc(flat.Properties, func(p ir.FlatProperty) bool {
		return p.Property.EffectiveWireName() == property
	})
	if i < 0 {
		return ir.FlatProperty{}, false
	}
	return flat.Properties[i], true
}

// memberStrings returns the strings every declaration of p admits, sorted, as
// an instance meets them all, and false where none bounds them (typeStrings).
// A declaration admitting null as well bounds them no less: null is no tag.
func memberStrings(reg ir.TypeRegistry, p ir.FlatProperty) ([]string, bool) {
	tags, closed := typeStrings(reg, p.Property.Type.Target)
	for _, d := range p.Overridden {
		if other, ok := typeStrings(reg, d.Property.Type.Target); ok {
			tags, closed = meet(tags, closed, other), true
		}
	}
	return tags, closed
}

// typeStrings returns the strings the type id admits, read through alias
// scalars, sorted: a string Literal's one, or the members of a closed Enum
// every member of which is a string. Any other type bounds no string, and
// returns false: a union of string values too, which pass.ConstUnions only
// later rewrites as such an Enum (#1715). pinnedValue is its reading of one
// string.
func typeStrings(reg ir.TypeRegistry, id ir.TypeID) ([]string, bool) {
	switch n := reg[throughAliases(reg, id)].(type) {
	case *ir.Literal:
		if n == nil || n.Value.Kind != ir.ValueString {
			return nil, false
		}
		return []string{n.Value.Str}, true
	case *ir.Enum:
		if n == nil || !n.Closed || n.Flags {
			return nil, false
		}
		tags := make([]string, 0, len(n.Members))
		for _, member := range n.Members {
			if member.Value.Kind != ir.ValueString {
				return nil, false
			}
			tags = append(tags, member.Value.Str)
		}
		slices.Sort(tags)
		return slices.Compact(tags), true
	default:
		return nil, false
	}
}

// meet returns the strings both tags and other hold, both sorted, where closed
// says tags bounds them; other alone where it does not.
func meet(tags []string, closed bool, other []string) []string {
	if !closed {
		return other
	}
	return slices.DeleteFunc(slices.Clone(tags), func(tag string) bool {
		_, found := slices.BinarySearch(other, tag)
		return !found
	})
}

// admittedList names the first maxReportedTags of the sorted tags, and how
// many more there are, or says none is admitted.
func admittedList(tags []string) string {
	if len(tags) == 0 {
		return "no string"
	}
	named := make([]string, 0, min(len(tags), maxReportedTags)+1)
	for _, tag := range tags[:min(len(tags), maxReportedTags)] {
		named = append(named, strconv.Quote(tag))
	}
	if more := len(tags) - maxReportedTags; more > 0 {
		named = append(named, strconv.Itoa(more)+" more")
	}
	return "only " + strings.Join(named, ", ")
}
