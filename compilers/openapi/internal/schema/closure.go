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

// closerOf returns the schema object of comp, the allOf s writes, whose
// catch-all keywords the composed model types, and the zero closure where none
// does (docs/compilers/openapi.md#additional-properties). additionalProperties
// applies to the members its own object's properties and patternProperties
// leave (JSON Schema 2020-12 §10.3.2.3), so it types the model only where that
// object declares every member the conjunction declares, or none declares any.
// That object is the position, or else the one inline entry writing a catch-all
// keyword, so the model's catch-all has one source. A $ref entry's members
// beside its $ref count; its target's do not.
func closerOf(c lowering.Ctx, s *statement.Schema, comp allOfComposition) closure {
	var entry, declarer closure
	entries, declarers := 0, 0
	distributes := unionDeclares(c, s)
	for _, e := range comp.entries {
		if e.schema.Truth != statement.Stated {
			continue
		}
		at := closure{schema: e.schema.Schema, at: e.pointer}
		if e.schema.Ref == nil && writesCatchAll(e.schema.Schema) {
			entries++
			entry = at
		}
		if declaresMembers(e.schema.Schema) {
			declarers++
			declarer = at
		}
		distributes = distributes || unionDeclares(c, e.schema.Schema)
	}

	switch {
	case writesCatchAll(s):
		if declarers == 0 {
			return closure{schema: s, at: comp.pointer, own: true}
		}
		return closure{}
	case entries != 1 || declarers > 1 || (declarers == 1 && declarer != entry) || declaresMembers(s) || distributes:
		return closure{}
	default:
		return entry
	}
}

// writesCatchAll reports whether s writes a keyword typing members its
// properties do not name: additionalProperties, or patternProperties naming a
// pattern.
func writesCatchAll(s *statement.Schema) bool {
	return s.ObjectKeywords().AdditionalProperties.Truth() != statement.Unwritten || len(s.ObjectKeywords().PatternProperties) > 0
}

// declaresMembers reports whether s names a member its additionalProperties
// leaves alone: a property, or a pattern.
func declaresMembers(s *statement.Schema) bool {
	return len(s.ObjectKeywords().Properties) > 0 || len(s.ObjectKeywords().PatternProperties) > 0
}

// unionDeclares reports whether an inline branch of the oneOf or anyOf s
// writes declares a member, which the union distributes into the composed
// model, where an entry's additionalProperties applies to it as well.
func unionDeclares(c lowering.Ctx, s *statement.Schema) bool {
	choice := s.ChoiceKeywords()
	for _, sub := range slices.Concat(choice.OneOf, choice.AnyOf) {
		b := c.Statements().Sub(sub)
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

// fillComposedAdditional fills m's openness and catch-all from the schema
// object comp's closure names (closerOf). With none, the position's
// patternProperties still type the catch-all, as a pattern reads no member
// another object declares, and its additionalProperties is kept (keepUnclosing).
// The additionalProperties of an entry closing the model evaluates every
// member it could apply to, which leaves an unevaluatedProperties: false beside
// the allOf inert (JSON Schema 2020-12 §11.3), as one beside it would be.
func fillComposedAdditional(c lowering.Ctx, ts *compile.Types, depth int, m *ir.Model, s *statement.Schema, comp allOfComposition) []ir.Diagnostic {
	if comp.closer.own {
		return fillAdditional(c, ts, depth, m, s, comp.pointer)
	}

	closes := annotation.ClosesAfterComposition(s)
	e := comp.closer
	if e.schema == nil {
		diags := keepUnclosing(c, m, s, comp.pointer)
		return append(diags, fillCatchAll(c, ts, depth, m, statement.Sub{}, s, comp.pointer, closes)...)
	}
	ap := e.schema.ObjectKeywords().AdditionalProperties
	return fillCatchAll(c, ts, depth, m, ap, e.schema, e.at, closes && ap.Truth() == statement.Unwritten)
}

// keepUnclosing keeps the additionalProperties s, the position written at
// pointer, writes beside an allOf an entry of which declares members: the
// keyword reads s's own properties alone, so it applies to those members too,
// and typing it on the model would close or type members it never read, where
// s refuses an instance setting one the keyword does not admit. The model stays
// open and the keyword is reported.
func keepUnclosing(c lowering.Ctx, m *ir.Model, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	kept, diags := annotation.PreserveSchemaKeyword(&m.Unmodeled, s, "additionalProperties", ir.ReasonDegradedLowering, pointer, c.Place())
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer, "%s",
		"additionalProperties reads only the properties and patternProperties written beside it, so it also applies "+
			"to the members an allOf entry here declares; the composed model does not take it, and it is kept verbatim "+
			"under Unmodeled"))
}
