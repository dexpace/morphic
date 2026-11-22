// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// notesOnCarrier reports whether the carrier holding the position schema is
// written at, pointer, holds the notes schema writes, its documentation,
// deprecation and examples, though typeRef names a node the position owns: a
// carrier holds the position (carriedAt), typeRef names the node the position's
// own lowering builds there, and schema names no $ref, its statement listing
// none among its conjuncts (allOfRefEntries). Node and carrier both ask it, so
// the notes have one home (docs/ir/annotations.md#one-structural-home-per-declaration).
//
// A position naming a $ref keeps them on the node it builds, read off what it
// writes alone, so every spelling of one use reads alike
// (docs/compilers/openapi.md#ref-use-site).
func notesOnCarrier(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer, typeRef ir.TypeRef) bool {
	if !stated(schema) || !LoweredToOwnNode(loweringContext, pointer, typeRef) || !carriedAt(loweringContext, schema) {
		return false
	}
	for range allOfRefEntries(loweringContext, schema) {
		return false
	}
	return true
}

// CarriedAnnotations returns what a carrier holds of the annotations schema,
// written at pointer and lowered to typeRef, declares, read through use, and
// whether the position owns the node typeRef names. Where it owns none, the
// carrier holds them all. Where it owns one, the node keeps everything but the
// notes, which are the carrier's where notesOnCarrier says so and the node's
// otherwise.
//
// It is the one reading a property, a header and a parameter share, so the
// three cannot differ in what they hold.
func CarriedAnnotations(loweringContext lowering.Ctx, schema *statement.Schema, use Use, typeRef ir.TypeRef, pointer jsontext.Pointer,
) (annotation.Set, bool, []ir.Diagnostic) {
	if !LoweredToOwnNode(loweringContext, pointer, typeRef) {
		a, diags := annotation.Read(use.SiteAt(loweringContext, schema, pointer), pointer, loweringContext.Place())
		return a, false, diags
	}
	if !notesOnCarrier(loweringContext, schema, pointer, typeRef) {
		return annotation.Set{}, true, nil
	}
	// The node's build read the same declaration and drew its findings, so this
	// reading draws none. Naming no $ref, the position falls back to nothing, so
	// use speaks as the node's use does (nodeUse).
	a, _ := annotation.Read(use.SiteAt(loweringContext, schema, pointer), pointer, loweringContext.Place())
	return annotation.Set{Docs: a.Docs, Deprecated: a.Deprecated, Examples: a.Examples}, true, nil
}

// carriedNotesOf keys what carriedNoteKeys reads of one schema.
type carriedNotesOf struct {
	schema *statement.Schema
}

// carriedNoteKeys returns what the mapped keys the node at pointer keeps of
// schema, which lowered to typeRef, lend the carrier holding its notes: a
// deprecation a key states, and its members, read as the keys of an object the
// carrier carries are (promotion.Promoter.WithOwnCarriedState). The node keeps
// its redaction, so a key asking for one lends nothing here. A replacement is
// read later off the deprecated node keeping its key, so one a mapped key names
// there fills no ReplacedBy: the key stays on the node, which holds no
// deprecation (docs/compilers/openapi.md#docs-and-deprecation).
func carriedNoteKeys(loweringContext lowering.Ctx, schema *statement.Schema, typeRef ir.TypeRef, pointer jsontext.Pointer) promotion.Referent {
	lent := loweringContext.Promoter().Referent(carriedNotesOf{schema}, true, func() ir.Unmodeled {
		a, _ := annotation.Read(nodeUse(loweringContext, schema, pointer, typeRef).SiteAt(loweringContext, schema, pointer), pointer, loweringContext.Place())
		return a.Unmodeled
	})
	lent.Redacted = false
	return lent
}

// placeNotes writes the notes a holds on common, the node schema, written at
// pointer and lowered to target, finishes, unless its carrier holds them
// (notesOnCarrier). It returns where the promotion writes the node's
// deprecation: common's own, or one nothing keeps where the carrier holds it,
// so the promotion still reports a key it misreads, once, at the node keeping
// the key, while the carrier reads the key in silence (carriedNoteKeys).
//
// A $ref naming the position from one no carrier holds reaches this node, so
// it reads none of the notes, which the carrier alone holds.
func placeNotes(loweringContext lowering.Ctx, common *ir.TypeCommon, a annotation.Set, schema *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef,
) (**ir.Deprecation, *ir.Provenance) {
	var withheld *ir.Deprecation
	if a.Deprecated {
		withheld = &ir.Deprecation{}
	}
	if notesOnCarrier(loweringContext, schema, pointer, target) {
		return &withheld, &ir.Provenance{}
	}
	common.Docs = a.Docs
	if withheld != nil {
		common.Deprecation = withheld
	}
	if len(a.Examples) > 0 {
		common.Examples = a.Examples
	}
	return &common.Deprecation, &common.Provenance
}
