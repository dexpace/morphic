// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"encoding/json/jsontext"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// foldKeywords are the keywords an inline allOf branch writes for the position
// holding it, each read as if written beside the allOf
// (docs/compilers/openapi.md#allof-inline-residue). foldEntry adds the value
// constraints, the extensions and the keywords that fold together.
var foldKeywords = []string{
	"title", "description", "externalDocs", "deprecated", "xml", "default", "readOnly", "writeOnly",
	"nullable", "not", "dependentSchemas", "dependentRequired", "propertyNames",
}

// foldEntry returns the one entry a position keeps keyword in once s folds, and
// false for a keyword no fold reads. Keywords one entry holds fold as one, so a
// branch's `then` and another's `if` cannot overwrite each other.
func foldEntry(s *oas3.Schema, keyword string) (string, bool) {
	switch keyword {
	case "example", "examples":
		return "examples", true
	case "if", "then", "else":
		return "if", true
	case "contains", "minContains", "maxContains":
		return "contains", true
	case "unevaluatedItems":
		return "unevaluated", true
	case "unevaluatedProperties":
		// `false` is the model's openness, which no annotation reader keeps
		// (fillAdditional).
		return "unevaluated", !annotation.IsFalseSchema(s.GetUnevaluatedProperties())
	case "format":
		return keyword, annotation.DeclaresRedaction(s)
	}
	fold := strings.HasPrefix(keyword, "x-") || slices.Contains(foldKeywords, keyword) ||
		slices.Contains(valueConstraintKeywords, keyword)
	return keyword, fold
}

// foldRendering is what a conjunct writes for an entry, rendered to compare with
// another conjunct's. One that cannot be rendered agrees with nothing, which keeps
// its branch; a keyword may render to nothing, which is no failure.
type foldRendering struct {
	raw ir.RawValue
	err error
}

// agrees reports whether r and o write an entry alike.
func (r foldRendering) agrees(o foldRendering) bool {
	return r.err == nil && o.err == nil && bytes.Equal(r.raw, o.raw)
}

// foldValue renders what s writes for entry.
func foldValue(s *oas3.Schema, entry string) foldRendering {
	var r foldRendering
	switch entry {
	case "examples":
		r.raw, _, r.err = annotation.ExamplesRaw(s)
	case "if":
		r.raw, _, r.err = annotation.IfThenElseRaw(s)
	case "contains":
		r.raw, _, r.err = annotation.ContainsRaw(s)
	case "unevaluated":
		r.raw, _, r.err = annotation.UnevaluatedRaw(s)
	default:
		r.raw, r.err = annotation.RawFromNode(annotation.RawPropertyNode(s, entry))
	}
	return r
}

// allOfDecides reports whether s's allOf is what its position lowers: not a
// family the election passed over, which stays verbatim (GitHub #1309), nor
// keywords beside a $ref, a $dynamicRef or a union, whose branches keep what they
// write on each variant (docs/compilers/openapi.md#union-beside-structure).
func allOfDecides(s *oas3.Schema) bool {
	return len(s.GetAllOf()) > 0 && s.Ref == nil && dispatchOf(s).won == "allOf" &&
		!declaresUnion(s) && !declaresDynamicRef(s)
}

// voicesOf keys what voicesAmong read of one schema.
type voicesOf struct {
	schema *oas3.Schema
}

// voicesAmong reports, for each branch of s's flattened allOf, whether it folds:
// it is inline, it writes something for the position, and each such keyword
// agrees with every other conjunct writing it, s's own keywords among them. The
// answer reads the whole set, so no conjunct takes precedence and no order
// decides. Two bounds that differ disagree: nothing here intersects them (GitHub
// #10). A null keyword writes nothing (WritesKeywordLeftOut). Every use of s
// asks, so it is read once a run.
func voicesAmong(c lowering.Ctx, s *oas3.Schema) []bool {
	return voicingOf(c, s).speaks
}

// voicing is what a schema's allOf says for the position holding it: whether
// each branch of the flattened list folds, and whether it lends a use what s and
// the folding branches before it do not, an entry none of them writes.
type voicing struct {
	speaks []bool
	lends  []bool
}

// voicingOf reads s's voicing once a run.
func voicingOf(c lowering.Ctx, s *oas3.Schema) voicing {
	return lowering.Remembered(c, voicesOf{s}, func() voicing { return readVoices(s) })
}

// readVoices reads what voicingOf remembers, rendering each conjunct's value
// once: the first writer's is compared with every later one's.
func readVoices(s *oas3.Schema) voicing {
	first := map[string]foldRendering{}
	split := map[string]bool{}
	write := func(conjunct *oas3.Schema) []string {
		var entries []string
		for _, keyword := range rawMappingKeys(conjunct.GetRootNode()) {
			entry, fold := foldEntry(conjunct, keyword)
			if !fold || annotation.WritesKeywordLeftOut(conjunct, keyword) {
				continue
			}
			entries = append(entries, entry)
			rendered := foldValue(conjunct, entry)
			if held, ok := first[entry]; !ok {
				first[entry] = rendered
			} else if !held.agrees(rendered) {
				split[entry] = true
			}
		}
		return entries
	}
	lent := map[string]bool{}
	for _, entry := range write(s) {
		lent[entry] = true
	}
	comp := allOfBranches(s, "")
	writes := make([][]string, len(comp.branches))
	for i, e := range comp.branches {
		if bs := e.schema.GetSchema(); bs != nil && isInlineBranch(e.schema) {
			writes[i] = write(bs)
		}
	}
	out := voicing{speaks: make([]bool, len(comp.branches)), lends: make([]bool, len(comp.branches))}
	for i, entries := range writes {
		out.speaks[i] = len(entries) > 0 && !slices.ContainsFunc(entries, func(entry string) bool { return split[entry] })
		if !out.speaks[i] {
			continue
		}
		for _, entry := range entries {
			out.lends[i] = out.lends[i] || !lent[entry]
			lent[entry] = true
		}
	}
	return out
}

// disagreedWhy words why a branch that would have folded is kept: its keywords
// have homes, and what they say is what is in question.
func disagreedWhy(residue []string) string {
	return "the conjuncts of this allOf disagree on a keyword this inline branch writes, so nothing it writes folds; " +
		"the branch (" + strings.Join(residue, ", ") + ") is kept verbatim under Unmodeled"
}

// disagreed reports whether residue, what a branch that did not fold keeps of bs,
// holds a keyword folding reads: voicesAmong passes over a branch for writing none
// or for a disagreement, and this tells them apart.
func disagreed(bs *oas3.Schema, residue []string) bool {
	return slices.ContainsFunc(residue, func(keyword string) bool {
		_, fold := foldEntry(bs, keyword)
		return fold
	})
}

// folds reports whether a branch that folds consumes keyword: bounds says the
// position has a field for value bounds, and a `nullable` folds where
// foldsNullable says so.
func folds(c lowering.Ctx, bs *oas3.Schema, keyword string, bounds bool) bool {
	if keyword == "nullable" {
		return c.NullableWidening.Honours() && foldsNullable(bs)
	}
	if slices.Contains(valueConstraintKeywords, keyword) {
		return bounds
	}
	_, fold := foldEntry(bs, keyword)
	return fold
}

// foldsNullable reports whether `nullable` on the branch bs speaks for the whole
// composition: a branch writing properties declares a shape of its own, which
// its `nullable` reads as describing, so it stays conjoined with the rest.
func foldsNullable(bs *oas3.Schema) bool {
	return annotation.RawPropertyNode(bs, "properties") == nil
}

// positionVoices returns the voices of s, written at pointer, as a reader of
// what the position holds takes them. A refinement's inline principal is one
// only where it reduced to a shared node, target being what the position lowered
// to: a branch owning that node keeps what it writes there.
func positionVoices(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, target ir.TypeRef) []annotation.Voice {
	if !allOfDecides(s) {
		return nil
	}
	comp := allOfBranches(s, pointer)
	var out []annotation.Voice
	for i, speaks := range voicesAmong(c, s) {
		e := comp.branches[i]
		if !speaks || LoweredToOwnNode(c, ts, e.pointer, target) {
			continue
		}
		out = append(out, annotation.Voice{Node: e.schema.GetSchema(), At: e.pointer})
	}
	return out
}

// lendingBranches returns the schemas of the branches of s that fold, for a use
// reading what s lends it, but those writing only what s or such a branch before
// them writes: every writer of an entry agrees (voicesAmong), so a reader taking
// the nearest takes the same value from any of them, and a use of s pays for each
// entry once, not for each branch repeating it.
func lendingBranches(c lowering.Ctx, s *oas3.Schema) []*oas3.Schema {
	if !allOfDecides(s) {
		return nil
	}
	comp := allOfBranches(s, "")
	var out []*oas3.Schema
	for i, lends := range voicingOf(c, s).lends {
		if lends {
			out = append(out, comp.branches[i].schema.GetSchema())
		}
	}
	return out
}

// declaresScoped reports whether a voice writes what only the position can hold,
// as declaresPositionScoped asks of the schema written there.
func declaresScoped(c lowering.Ctx, voices []annotation.Voice) bool {
	return slices.ContainsFunc(voices, func(v annotation.Voice) bool { return declaresPositionScoped(c, v.Node) })
}

// recordVoicedResidue is recordDeclarationResidue for what voices write, each
// reported at the keyword it wrote, the last first: an entry several write is the
// nearest's, and the schema written at the position records after them.
func recordVoicedResidue(c lowering.Ctx, ts *compile.Types, voices []annotation.Voice, pointer jsontext.Pointer, home annotation.Home) []ir.Diagnostic {
	if home != annotation.HomeOwnNode {
		return nil
	}
	td, ok := ts.NodeAt(c.IDs().Coordinate(pointer))
	if !ok {
		return nil
	}
	var diags []ir.Diagnostic
	for _, v := range slices.Backward(voices) {
		diags = append(diags, recordResidue(c, td.Common(), v.Node, v.At)...)
	}
	return diags
}
