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
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// foldKeywords are the keywords an inline allOf branch writes for the position
// holding it, each read as if written beside the allOf
// (docs/compilers/openapi.md#allof-inline-residue). foldEntry adds the value
// constraints, the extensions and the keywords that fold together. A collection
// bound folds nowhere yet, so a branch writing one stays whole whatever the
// position lowers to, a List with the field included (GitHub #1312).
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
// agrees with every other conjunct writing it, s's own keywords among them
// (agreement). The answer reads the whole set, so no conjunct takes precedence
// and no order decides. A null keyword writes nothing (WritesKeywordLeftOut).
// Every use of s asks, so it is read once a run.
func voicesAmong(c lowering.Ctx, s *oas3.Schema) []bool {
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
func voicingOf(c lowering.Ctx, s *oas3.Schema) voicing {
	return lowering.Remembered(c, voicesOf{s}, func() voicing { return readVoices(c, s) })
}

// readVoices reads what voicingOf remembers.
func readVoices(c lowering.Ctx, s *oas3.Schema) voicing {
	a := newAgreement(c)
	lent := map[string]bool{}
	for _, entry := range a.write(s) {
		lent[entry] = true
	}
	comp := allOfBranches(s, "")
	n := len(comp.branches)
	out := voicing{speaks: make([]bool, n), lends: make([]bool, n), rebased: make([]bool, n)}
	writes := make([][]string, n)
	ids := idWrappers(comp)
	for i, e := range comp.branches {
		if bs := e.schema.GetSchema(); bs != nil && isInlineBranch(e.schema) {
			if out.rebased[i] = underID(e, bs, ids) && foldsReference(bs) && !readsAtPosition(c, e); !out.rebased[i] {
				writes[i] = a.write(bs)
			}
		}
	}
	for i, entries := range writes {
		out.speaks[i] = len(entries) > 0 && !slices.ContainsFunc(entries, a.apart)
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

// idWrappers returns where the flattened wrappers of comp writing an
// identifier are (writesIdentifier), each one that may give what it holds
// another base than the position's.
func idWrappers(comp allOfComposition) map[jsontext.Pointer]bool {
	out := map[jsontext.Pointer]bool{}
	for _, e := range comp.branches {
		if e.wrapper && writesIdentifier(e.schema.GetSchema()) {
			out[e.pointer] = true
		}
	}
	return out
}

// underID reports whether bs, the inline allOf branch e, or a wrapper holding
// it writes an identifier. Only one can give the branch another base than the
// position holding the allOf, so a branch under none reads at the position
// without a base being looked up. The wrappers holding e are the prefixes of
// its pointer, as many as MaxAllOfFlattenDepth allows, so each is looked up.
func underID(e allOfBranch, bs *oas3.Schema, wrappers map[jsontext.Pointer]bool) bool {
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
func writesIdentifier(s *oas3.Schema) bool {
	return annotation.RawPropertyNode(s, "$id") != nil || annotation.RawPropertyNode(s, "id") != nil
}

// foldsReference reports whether what bs folds writes a relative $ref, the one
// text a base changes: a branch writing none in it says the same at any base.
func foldsReference(bs *oas3.Schema) bool {
	return slices.ContainsFunc(rawMappingKeys(bs.GetRootNode()), func(keyword string) bool {
		entry, fold := foldEntry(bs, keyword)
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
	site, placed := c.Bindings().Site(e.schema)
	at := bind.Position{Doc: site.Doc, Pointer: jsontext.Pointer(strings.TrimSuffix(string(site.Pointer), string(e.pointer)))}
	own, known := c.Bindings().Table().Base(site)
	position, knownAt := c.Bindings().Table().Base(at)
	return placed && known && knownAt && own == position
}

// unfoldedWhy words why a branch that writes what folds kept it, residue being
// what it keeps, and is "" where the branch spoke or writes nothing folding
// reads: what it folds writes a relative $ref it reads against another base
// than the position, or the conjuncts disagree on an entry it writes.
func unfoldedWhy(speaks, rebased bool, bs *oas3.Schema, residue []string) string {
	switch {
	case speaks || !disagreed(bs, residue):
		return ""
	case rebased:
		return "this inline allOf branch reads its $refs against another base than the position does, as an " +
			"identifier at or above it names another URI, so nothing it writes folds; the branch (" +
			strings.Join(residue, ", ") + ") is kept verbatim under Unmodeled"
	default:
		return disagreedWhy(residue)
	}
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
// them writes: the writers of an entry a use reads agree (voicesAmong), so a
// reader taking the nearest takes the same value from any of them, and a use of s
// pays for each entry once, not for each branch repeating it. Value bounds, which
// agree by meeting, are no such entry: a use never reads them through its $ref.
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
