// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

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

// valueConstraintKeywords are the keywords annotation.Constraints reads into an
// ir.Constraints, sorted — which is both the order declaredConstraints walks
// them in and what makes the list comparable to the reader it must keep pace
// with.
//
// One list, read by the predicate that hoists a node for them
// (declaresValueConstraints) and by the recorder that keeps the ones no node can
// hold (declaredConstraints), so the two can never disagree about what a schema
// wrote. Collection bounds are absent because they belong to the collection
// nodes and are read by collectionConstraints, not here.
var valueConstraintKeywords = []string{
	"exclusiveMaximum", "exclusiveMinimum", "maxLength", "maxProperties",
	"maximum", "minLength", "minProperties", "minimum", "multipleOf", "pattern",
}

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
