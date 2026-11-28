// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// closure names the schema object of a composition whose catch-all keywords
// its model types (closerOf), and the pointer it is written at: the position's
// own, which own marks, or an inline entry's. The zero closure names none.
type closure struct {
	schema *statement.Schema
	at     jsontext.Pointer
	own    bool
}

// entryAt reports whether c names the inline entry written at at.
func (c closure) entryAt(at jsontext.Pointer) bool {
	return c.schema != nil && !c.own && c.at == at
}

// closerOf returns the schema object of comp, the allOf schema writes, whose
// catch-all keywords the composed model types, and the zero closure where none
// does (docs/compilers/openapi.md#additional-properties). additionalProperties
// applies to the members its own object's properties and patternProperties
// leave (JSON Schema 2020-12 §10.3.2.3), so it types the model only where that
// object declares every member the conjunction declares, or none declares any.
// That object is the position, or else the one inline entry writing a catch-all
// keyword, so the model's catch-all has one source. A $ref entry's members
// beside its $ref count; its target's do not.
func closerOf(loweringContext lowering.Ctx, schema *statement.Schema, comp allOfComposition) closure {
	var entry, declarer closure
	entries, declarers := 0, 0
	distributes := unionDeclares(loweringContext, schema)
	for _, schemaEntry := range comp.entries {
		if schemaEntry.schema.Truth != statement.Stated {
			continue
		}
		at := closure{schema: schemaEntry.schema.Schema, at: schemaEntry.pointer}
		if schemaEntry.schema.Ref == nil && writesCatchAll(schemaEntry.schema.Schema) {
			entries++
			entry = at
		}
		if declaresMembers(schemaEntry.schema.Schema) {
			declarers++
			declarer = at
		}
		distributes = distributes || unionDeclares(loweringContext, schemaEntry.schema.Schema)
	}

	switch {
	case writesCatchAll(schema):
		if declarers == 0 {
			return closure{schema: schema, at: comp.pointer, own: true}
		}
		return closure{}
	case entries != 1 || declarers > 1 || (declarers == 1 && declarer != entry) || declaresMembers(schema) || distributes:
		return closure{}
	default:
		return entry
	}
}

// writesCatchAll reports whether schema writes a keyword typing members its
// properties do not name: additionalProperties, or patternProperties naming a
// pattern.
func writesCatchAll(schema *statement.Schema) bool {
	return schema.ObjectKeywords().AdditionalProperties.SchemaValueForm() != statement.Unwritten || len(schema.ObjectKeywords().PatternProperties) > 0
}

// declaresMembers reports whether schema names a member its
// additionalProperties leaves alone: a property, or a pattern.
func declaresMembers(schema *statement.Schema) bool {
	return len(schema.ObjectKeywords().Properties) > 0 || len(schema.ObjectKeywords().PatternProperties) > 0
}

// unionDeclares reports whether an inline branch of the oneOf or anyOf schema
// writes declares a member, which the union distributes into the composed
// model, where an entry's additionalProperties applies to it as well.
func unionDeclares(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	choice := schema.ChoiceKeywords()
	for _, sub := range slices.Concat(choice.OneOf, choice.AnyOf) {
		b := loweringContext.Statements().Subschema(sub)
		if b.Truth == statement.Stated && b.Ref == nil && declaresMembers(b.Schema) {
			return true
		}
	}
	return false
}

// catchAllKeyword reports whether keyword is one the in-place merge takes off
// the entry closing the model (closerOf, fillComposedAdditional).
func catchAllKeyword(keyword string) bool {
	return keyword == "additionalProperties" || keyword == "patternProperties"
}

// fillComposedAdditional fills model's openness and catch-all from the schema
// object comp's closure names (closerOf). With none, the position's
// patternProperties still type the catch-all, as a pattern reads no member
// another object declares, and its additionalProperties is kept (keepUnclosing).
// The additionalProperties of an entry closing the model evaluates every
// member it could apply to, which leaves an unevaluatedProperties: false beside
// the allOf inert (JSON Schema 2020-12 §11.3), as one beside it would be.
func fillComposedAdditional(loweringContext lowering.Ctx, ts *compile.Types, depth int, model *ir.Model, schema *statement.Schema, comp allOfComposition) []ir.Diagnostic {
	if comp.closer.own {
		return fillAdditional(loweringContext, ts, depth, model, schema, comp.pointer)
	}

	closes := annotation.ClosesAfterComposition(schema)
	closure := comp.closer
	if closure.schema == nil {
		diags := keepUnclosing(loweringContext, model, schema, comp.pointer)
		return append(diags, fillCatchAll(loweringContext, ts, depth, model, statement.Subschema{}, schema, comp.pointer, closes)...)
	}
	ap := closure.schema.ObjectKeywords().AdditionalProperties
	return fillCatchAll(loweringContext, ts, depth, model, ap, closure.schema, closure.at, closes && ap.SchemaValueForm() == statement.Unwritten)
}

// keepUnclosing keeps the additionalProperties schema, the position written at
// pointer, writes beside an allOf an entry of which declares members: the
// keyword reads schema's own properties alone, so it applies to those members
// too, and typing it on the model would close or type members it never read,
// where schema refuses an instance setting one the keyword does not admit. The
// model stays open and the keyword is reported.
func keepUnclosing(loweringContext lowering.Ctx, model *ir.Model, schema *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	kept, diags := preserveSchemaKeyword(loweringContext, &model.Unmodeled, schema, "additionalProperties", ir.ReasonDegradedLowering, pointer)
	if !kept {
		return diags
	}
	return append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer, "%s",
		"additionalProperties reads only the properties and patternProperties written beside it, so it also applies "+
			"to the members an allOf entry here declares; the composed model does not take it, and it is kept verbatim "+
			"under Unmodeled"))
}
