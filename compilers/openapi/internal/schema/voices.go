// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"encoding/json/jsontext"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// foldKeywords are the keywords an inline allOf branch writes for the position
// holding it, each read as if written beside the allOf
// (docs/compilers/openapi.md#allof-inline-residue). foldEntry adds the bounds,
// the extensions and the keywords that fold together. No unevaluated keyword
// folds: each applies to what its own schema evaluates, so on a branch it holds
// the instance to what that branch alone evaluates, and the branch kept whole
// is its record.
var foldKeywords = []string{
	"title", "description", "externalDocs", "deprecated", "xml", "default", "readOnly", "writeOnly",
	"nullable", "not", "dependentSchemas", "dependentRequired", "propertyNames",
}

// foldEntry returns the one entry a position keeps keyword in once s, a schema
// of c, folds, and false for a keyword no fold reads. Keywords one entry holds
// fold as one, so a branch's `then` and another's `if` cannot overwrite each
// other, and a draft's spelling folds as its twin does (TwinKeyword).
func foldEntry(c lowering.Ctx, s *statement.Schema, keyword string) (string, bool) {
	switch keyword {
	case "example", "examples":
		return "examples", true
	case "if", "then", "else":
		return "if", true
	case "contains", "minContains", "maxContains":
		return "contains", true
	case "format":
		return keyword, annotation.DeclaresRedaction(s)
	case "minItems", "maxItems", "uniqueItems":
		// A collection bound ir.Constraints cannot state keeps its branch whole.
		return keyword, referenceHolds(s, keyword)
	}
	twin := TwinKeyword(c, s, keyword)
	fold := strings.HasPrefix(keyword, "x-") || slices.Contains(foldKeywords, twin) ||
		slices.Contains(valueConstraintKeywords, twin)
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
func foldValue(s *statement.Schema, entry string) foldRendering {
	var r foldRendering
	switch entry {
	case "examples":
		r.raw, _, r.err = annotation.ExamplesRaw(s)
	case "if":
		r.raw, _, r.err = annotation.IfThenElseRaw(s)
	case "contains":
		r.raw, _, r.err = annotation.ContainsRaw(s)
	default:
		r.raw, r.err = annotation.RawFromNode(keyNode(s, entry))
	}
	return r
}

// allOfMayRefine reports whether s's conjuncts may refine: it writes them
// (writesConjuncts), an allOf or a reference beside other keys
// (docs/compilers/openapi.md#allof-refinement). A union beside them is one
// more conjunct, and a $dynamicRef the position does not expand masks no kind
// (admissionReader.conjuncts).
func allOfMayRefine(s *statement.Schema) bool {
	return writesConjuncts(s)
}

// allOfDecides reports whether s's allOf is what its position lowers: one the
// election of competing keywords made the value, or one refining, whose rival
// there, an enum or a const beside it, is one of its conjuncts. An allOf that
// value set beats otherwise stays verbatim
// (docs/compilers/openapi.md#competing-keywords). A union beside the composing
// allOf or on one of its branches leaves it deciding, composed into each variant
// the union distributes across, or into the Model keeping the union verbatim or
// composing it as a mixin, and so does a $dynamicRef it keeps from expanding.
func allOfDecides(c lowering.Ctx, s *statement.Schema) bool {
	if !writesConjuncts(s) {
		return false
	}
	if dispatchOf(s).won == "allOf" {
		return true
	}
	return decisionOf(c, s).refines
}

// voicesOf keys what voicesAmong read of one schema.
type voicesOf struct {
	schema *statement.Schema
}

// voicesAmong reports, for each branch of s's flattened allOf, whether it folds:
// it is inline, it writes something for the position, and each such keyword
// agrees with every other conjunct writing it, s's own keywords among them
// (agreement). The answer reads the whole set, so no conjunct takes precedence
// and no order decides. A null keyword writes nothing (WritesKeywordLeftOut).
// Every use of s asks, so it is read once a run.
func voicesAmong(c lowering.Ctx, s *statement.Schema) []bool {
	return voicingOf(c, s).speaks
}

// voicing is what a schema's allOf says for the position holding it: whether
// each branch of the flattened list folds, whether it lends a use what s and
// the folding branches before it do not, an entry none of them writes, and
// whether what it folds writes a relative $ref it reads against another base
// than the position does (readsAtPosition), so nothing it writes folds.
type voicing struct {
	speaks  []bool
	lends   []bool
	rebased []bool
}

// voicingOf reads s's voicing once a run.
func voicingOf(c lowering.Ctx, s *statement.Schema) voicing {
	return lowering.Remembered(c, voicesOf{s}, func() voicing { return readVoices(c, s) })
}

// readVoices reads what voicingOf remembers.
func readVoices(c lowering.Ctx, s *statement.Schema) voicing {
	a := newAgreement(c)
	lent := map[string]bool{}
	for _, entry := range a.write(s) {
		lent[entry] = lent[entry] || !writesFalseFlag(entry, foldValue(s, entry))
	}
	comp := allOfBranches(c, s, "")
	n := len(comp.branches)
	out := voicing{speaks: make([]bool, n), lends: make([]bool, n), rebased: make([]bool, n)}
	writes := make([][]string, n)
	ids := idWrappers(comp)
	for i, e := range comp.branches {
		if stated(e.schema.Schema) && e.schema.Ref == nil {
			if out.rebased[i] = underID(e, e.schema.Schema, ids) && foldsReference(c, e.schema.Schema) && !readsAtPosition(c, e); !out.rebased[i] {
				writes[i] = a.write(e.schema.Schema)
			}
		}
	}
	for i, entries := range writes {
		out.speaks[i] = len(entries) > 0 && !slices.ContainsFunc(entries, a.apart)
		if !out.speaks[i] {
			continue
		}
		for _, entry := range entries {
			// A false flag lends nothing: it states nothing a later true would not.
			if writesFalseFlag(entry, foldValue(comp.branches[i].schema.Schema, entry)) {
				continue
			}
			out.lends[i] = out.lends[i] || !lent[entry]
			lent[entry] = true
		}
	}
	return out
}

// idWrappers returns where the flattened wrappers of comp writing an
// identifier are (writesIdentifier), each one that may give what it holds
// another base than the position's.
func idWrappers(comp allOfComposition) map[jsontext.Pointer]bool {
	out := map[jsontext.Pointer]bool{}
	for _, e := range comp.branches {
		if e.wrapper && writesIdentifier(e.schema.Schema) {
			out[e.pointer] = true
		}
	}
	return out
}

// underID reports whether bs, the inline allOf branch e, or a wrapper holding
// it writes an identifier. Only one can give the branch another base than the
// position holding the allOf, so a branch under none reads at the position
// without a base being looked up. The wrappers holding e are the prefixes of
// its pointer, as many as statement.MaxFlattenDepth allows, so each is looked up.
func underID(e allOfBranch, bs *statement.Schema, wrappers map[jsontext.Pointer]bool) bool {
	if writesIdentifier(bs) {
		return true
	}
	p := string(e.pointer)
	for i := strings.LastIndexByte(p, '/'); i > 0; i = strings.LastIndexByte(p[:i], '/') {
		if wrappers[jsontext.Pointer(p[:i])] {
			return true
		}
	}
	return false
}

// writesIdentifier reports whether s writes `$id`, or `id`, draft 4's
// spelling of it. Either may name a resource, and the base a lookup returns
// says whether it does, so one that names none costs that lookup alone.
func writesIdentifier(s *statement.Schema) bool {
	return hasKey(s, "$id") || hasKey(s, "id")
}

// foldsReference reports whether what bs folds writes a relative $ref, the one
// text a base changes: a branch writing none in it says the same at any base.
func foldsReference(c lowering.Ctx, bs *statement.Schema) bool {
	return slices.ContainsFunc(bs.Keys, func(k statement.Key) bool {
		entry, fold := foldEntry(c, bs, k.Name)
		return fold && annotation.WritesReference(foldValue(bs, entry).raw)
	})
}

// readsAtPosition reports whether the inline allOf branch e reads its $refs
// against the base the position holding the allOf does, so what it writes means
// there what it means where it is written: an identifier at or above it naming
// another URI gives the same spelling another schema. The position is e's site
// less e's path from it, as the allOf may be read where a $ref landed. A base
// the bindings cannot tell is no match.
func readsAtPosition(c lowering.Ctx, e allOfBranch) bool {
	site, placed := siteOf(e.schema.Schema)
	at := bind.Position{Doc: site.Doc, Pointer: jsontext.Pointer(strings.TrimSuffix(string(site.Pointer), string(e.pointer)))}
	own, known := c.Bindings().Table().Base(site)
	position, knownAt := c.Bindings().Table().Base(at)
	return placed && known && knownAt && own == position
}

// disagreedWhy words why a branch that would have folded is kept, as n names
// it: its keywords have homes, and what they say is what is in question.
func disagreedWhy(residue []string, n branchNames) string {
	return "the conjuncts of this allOf disagree on a keyword " + n.this + " writes, so nothing it writes folds; " +
		n.kept + " (" + strings.Join(residue, ", ") + ") is kept verbatim under Unmodeled"
}

// disagreed reports whether residue, what a branch that did not fold keeps of bs,
// holds a keyword folding reads: voicesAmong passes over a branch for writing none
// or for a disagreement, and this tells them apart.
func disagreed(c lowering.Ctx, bs *statement.Schema, residue []string) bool {
	return slices.ContainsFunc(residue, func(keyword string) bool {
		_, fold := foldEntry(c, bs, keyword)
		return fold
	})
}

// folds reports whether a branch that folds consumes keyword: homes says which
// bounds the position has a field for, of which a value bound is read where the
// branch's own types take it too (writerTakes), and a `nullable` folds where
// the policy reads it (nullableRead) and foldsNullable says so.
func folds(c lowering.Ctx, bs *statement.Schema, keyword string, homes boundHomes) bool {
	if keyword == "nullable" {
		return c.NullableInference.Honours() && nullableRead(c, bs) && foldsNullable(bs)
	}
	if slices.Contains(valueConstraintKeywords, keyword) {
		return taken(writerTakes(homes.value, bs), keyword)
	}
	_, fold := foldEntry(c, bs, keyword)
	if slices.Contains(collectionBoundKeywords, keyword) {
		return fold && homes.collection
	}
	return fold
}

// foldsNullable reports whether `nullable` on the branch bs speaks for the whole
// composition: a branch writing properties declares a shape of its own, which
// its `nullable` reads as describing, so it stays conjoined with the rest.
func foldsNullable(bs *statement.Schema) bool {
	return !hasKey(bs, "properties")
}

// positionVoices returns the voices of s, written at pointer, as a reader of
// what the position holds takes them. A refinement's inline principal is one
// only where it reduced to a shared node, target being what the position lowered
// to: a branch owning that node keeps what it writes there.
func positionVoices(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef) []annotation.Voice {
	if !allOfDecides(c, s) {
		return nil
	}
	comp := allOfBranches(c, s, pointer)
	var out []annotation.Voice
	for i, speaks := range voicesAmong(c, s) {
		e := comp.branches[i]
		if !speaks || LoweredToOwnNode(c, e.pointer, target) {
			continue
		}
		out = append(out, annotation.Voice{Node: e.schema.Schema, At: e.pointer})
	}
	return out
}

// lendingBranches returns the branches of s that fold, each at its pointer below
// s, for a use
// reading what s lends it, but those writing only what s or such a branch before
// them writes: the writers of an entry a use reads agree (voicesAmong), so a
// reader taking the nearest takes the same value from any of them, and a use of s
// pays for each entry once, not for each branch repeating it. Bounds, which agree
// by meeting, are no such entry: a use never reads them through its $ref.
func lendingBranches(c lowering.Ctx, s *statement.Schema) []allOfBranch {
	if !allOfDecides(c, s) {
		return nil
	}
	comp := allOfBranches(c, s, "")
	var out []allOfBranch
	for i, lends := range voicingOf(c, s).lends {
		if lends {
			out = append(out, comp.branches[i])
		}
	}
	return out
}

// declaresScoped reports whether a voice writes what only the position can hold,
// as declaresPositionScoped asks of the schema written there.
func declaresScoped(c lowering.Ctx, voices []annotation.Voice) bool {
	return slices.ContainsFunc(voices, func(v annotation.Voice) bool { return declaresPositionScoped(c, v.Node) })
}

// recordVoicedResidue keeps on td, in hand, what binds a use that voices write
// (recordResidue), each reported at the keyword it wrote, in ResidueWriters'
// order: an entry several write is the nearest voice's kept apart, and the
// schema written at the position records after them.
func recordVoicedResidue(c lowering.Ctx, td ir.TypeDef, voices []annotation.Voice) []ir.Diagnostic {
	diags := make([]ir.Diagnostic, 0, len(voices))
	for _, v := range ResidueWriters(voices) {
		diags = append(diags, recordResidue(c, td.Common(), v.Node, v.At)...)
	}
	return diags
}

// ResidueWriters returns voices in the order a keeper overwriting what each
// writes takes them: those kept whole, then the rest, each the last first. The
// nearest voice kept apart holds a keyword several write, and one kept whole,
// whose branch holds it (annotation.Voice.KeptWhole), only one no other writes,
// which that branch then holds alone (dropFoldedIntoWhole).
func ResidueWriters(voices []annotation.Voice) []annotation.Voice {
	out := make([]annotation.Voice, 0, len(voices))
	for _, whole := range []bool{true, false} {
		for _, v := range slices.Backward(voices) {
			if v.KeptWhole == whole {
				out = append(out, v)
			}
		}
	}
	return out
}
