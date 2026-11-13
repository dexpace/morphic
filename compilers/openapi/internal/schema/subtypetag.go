// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"strconv"

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
// types of its members and the unions it composes, which only a registry
// every node is interned in answers in any declaration order, so it is a
// settlement.
func deferSubtypeTag(c lowering.Ctx, ts *compile.Types, tag subtypeTag, id ir.TypeID, pointer jsontext.Pointer) {
	if tag.byName == nil || !c.DiscriminatorInference.Honours() {
		return
	}
	ts.Defer(subtypeTagReading{c: c, id: id, pointer: pointer, property: tag.byName.GetPropertyName(), name: tag.value})
}

// subtypeTagReading settles the tag of the subtype id under the
// discriminator-inference policy. What it reads no other settlement changes
// (tagReadings), so the settling order changes no answer. One is deferred per
// subtype, and the types they share are read once a run.
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
	flat := ir.FlattenedProperties(reg, t.id)
	p, declared := flatMember(flat, t.property)
	got := readingsOf(t.c).object(reg, flat, p, declared, t.property)
	if got.admits(t.name) {
		return nil
	}
	tags := got.strings
	m.Provenance.Inferred = ir.JoinInferred(m.Provenance.Inferred, lowering.DiscriminatorInferenceHeuristic)
	outcome := "no tag its instances carry routes to it, so it carries none"
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

// admittedList names the first maxReportedTags of the sorted tags, and how
// many more there are, or says none is tagSet.
func admittedList(tags []string) string {
	if len(tags) == 0 {
		return "no string"
	}
	return "only " + fewQuoted(tags)
}
