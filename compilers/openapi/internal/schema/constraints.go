// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// ConstraintsOf returns the value constraints s writes, or nil where it writes
// none, for the node or carrier at pointer to hold, so a node a position owns,
// a property and a parameter read them alike.
//
// p is the Unmodeled map of the node or carrier the constraints land on. A 3.0
// exclusive modifier with no bound beside it to modify, which no ir.Constraints
// field holds, is kept there, beside the constraints it did not reach (GitHub
// #286).
func ConstraintsOf(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) (*ir.Constraints, []ir.Diagnostic) {
	if s == nil {
		return nil, nil
	}
	cons, kept, diags := annotation.Constraints(s, c.Is30(), pointer, c.ProvenanceAt)
	*p = annotation.MergeUnmodeled(*p, kept)
	return cons, stampConstraintDiags(c, diags, pointer)
}

// stampConstraintDiags gives every constraint diagnostic the provenance of the
// pointer that read the schema, which is what makes two reads of one sub-schema
// — its owning property and a $ref that hoists it — identical and so deduped by
// Diags.Append rather than reported twice.
func stampConstraintDiags(c lowering.Ctx, diags []ir.Diagnostic, pointer jsontext.Pointer) []ir.Diagnostic {
	for i := range diags {
		diags[i].Provenance = c.ProvenanceAt(pointer)
	}
	return diags
}

// CarrierConstraintsOf is ConstraintsOf for the carrier of a position of type t.
// A position owning its node leaves the collection bounds to it, one home per
// declaration, so the carrier reads the value bounds alone. One owning none can
// have written a collection bound only beside a reference, since the census
// hoists an alias to keep any other, so the carrier reads it there
// (referenceConstraintsOf). PreserveRefSiteKeywords draws the same line.
func CarrierConstraintsOf(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, s *oas3.Schema, t ir.TypeRef, pointer jsontext.Pointer) (*ir.Constraints, []ir.Diagnostic) {
	if LoweredToOwnNode(c, ts, pointer, t) {
		return ConstraintsOf(c, p, s, pointer)
	}
	return referenceConstraintsOf(c, p, s, pointer)
}

// referenceConstraintsOf is ConstraintsOf for a position that lowered to a
// reference, a $ref site, a collapsed union or an expanded $dynamicRef: no node
// of its own holds the collection bounds it wrote, so they join its value bounds
// on the alias or carrier it owns (docs/compilers/openapi.md#constraints). An
// allOf over a $ref is no reference here: it lowers to a Model, which keeps one
// verbatim (GitHub #1269).
func referenceConstraintsOf(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) (*ir.Constraints, []ir.Diagnostic) {
	cons, diags := ConstraintsOf(c, p, s, pointer)
	if s == nil || !slices.ContainsFunc(collectionBoundKeywords, func(k string) bool { return referenceHolds(s, k) }) {
		return cons, diags
	}
	if cons == nil {
		cons = &ir.Constraints{}
	}
	if referenceHolds(s, "minItems") {
		cons.MinItems = s.MinItems
	}
	if referenceHolds(s, "maxItems") {
		cons.MaxItems = s.MaxItems
	}
	cons.UniqueItems = referenceHolds(s, "uniqueItems")
	return cons, diags
}

// referenceHolds reports whether referenceConstraintsOf fills the collection
// bound keyword from s, which is the census's whole test at a reference, so a
// bound ir.Constraints cannot state stays verbatim: a count the parser misread
// (annotation.CountIsExact), and a false uniqueItems, which a plain bool cannot
// tell from unset and which would stop an alias being a decorator.
func referenceHolds(s *oas3.Schema, keyword string) bool {
	switch keyword {
	case "minItems":
		return annotation.CountIsExact(s, keyword, s.MinItems)
	case "maxItems":
		return annotation.CountIsExact(s, keyword, s.MaxItems)
	case "uniqueItems":
		return s.GetUniqueItems()
	default:
		return false
	}
}

// valueConstraintKeywords are the keywords annotation.Constraints reads into an
// ir.Constraints, sorted — which is both the order declaredConstraints walks
// them in and what makes the list comparable to the reader it must keep pace
// with.
//
// One list, read by the predicate that hoists a node for them
// (declaresValueConstraints) and by the recorder that keeps the ones no node can
// hold (declaredConstraints), so the two can never disagree about what a schema
// wrote. Collection bounds are absent because they belong to the collection
// nodes, or to what a reference owns, and are read by collectionConstraints or
// referenceConstraintsOf, not here.
var valueConstraintKeywords = []string{
	"exclusiveMaximum", "exclusiveMinimum", "maxLength", "maxProperties",
	"maximum", "minLength", "minProperties", "minimum", "multipleOf", "pattern",
}

// collectionBoundKeywords are the keywords collectionConstraints reads.
var collectionBoundKeywords = []string{"maxItems", "minItems", "uniqueItems"}

// declaresValueConstraints reports whether s sets any keyword
// annotation.Constraints reads. It does not call it: that reports a malformed
// bound, and a predicate must not emit diagnostics. The keywords are detected on
// their raw nodes for the same reason numericBounds reads them there — a
// magnitude beyond float64 leaves the model field nil while the keyword is
// plainly written.
func declaresValueConstraints(s *oas3.Schema) bool {
	return annotation.DeclaresAny(s, valueConstraintKeywords)
}

// declaredConstraints returns the value-constraint keywords s writes, in
// valueConstraintKeywords order.
func declaredConstraints(s *oas3.Schema) []string {
	out := make([]string, 0, len(valueConstraintKeywords))
	for _, keyword := range valueConstraintKeywords {
		if annotation.RawPropertyNode(s, keyword) != nil {
			out = append(out, keyword)
		}
	}
	return out
}
