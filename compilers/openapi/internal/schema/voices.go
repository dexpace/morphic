// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"encoding/json/jsontext"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// foldKeywords are the keywords an inline allOf entry writes for the position
// holding it, each read as if written beside the allOf
// (docs/compilers/openapi.md#allof-inline-residue). foldEntry adds the bounds,
// the extensions and the keywords that fold together. No unevaluated keyword
// folds: each applies to what its own schema evaluates, so on an entry it holds
// the instance to what that entry alone evaluates, and the entry kept whole
// is its record.
var foldKeywords = []string{
	"title", "description", "externalDocs", "deprecated", "xml", "default", "readOnly", "writeOnly",
	"nullable", "not", "dependentSchemas", "dependentRequired", "propertyNames",
}

// foldEntry returns the one entry a position keeps keyword in once s, a schema
// of c, folds, and false for a keyword no fold reads. Keywords one entry holds
// fold as one, so an entry's `then` and another's `if` cannot overwrite each
// other, and a draft's spelling folds as its twin does (TwinKeyword).
func foldEntry(loweringContext lowering.Ctx, schema *statement.Schema, keyword string) (string, bool) {
	switch keyword {
	case "example", "examples":
		return "examples", true
	case "if", "then", "else":
		return "if", true
	case "contains", "minContains", "maxContains":
		return "contains", true
	case "format":
		return keyword, annotation.DeclaresRedaction(schema)
	case "minItems", "maxItems", "uniqueItems":
		// A collection bound ir.Constraints cannot state keeps its entry whole.
		return keyword, referenceHolds(schema, keyword)
	}
	twin := TwinKeyword(loweringContext, schema, keyword)
	fold := strings.HasPrefix(keyword, "x-") || slices.Contains(foldKeywords, twin) ||
		slices.Contains(valueConstraintKeywords, twin)
	return keyword, fold
}

// foldRendering is what a conjunct writes for an entry, rendered to compare with
// another conjunct's. One that cannot be rendered agrees with nothing, which keeps
// its entry; a keyword may render to nothing, which is no failure.
type foldRendering struct {
	raw ir.RawValue
	err error
}

// agrees reports whether r and foldRenderingValue write an entry alike.
func (r foldRendering) agrees(foldRenderingValue foldRendering) bool {
	return r.err == nil && foldRenderingValue.err == nil && bytes.Equal(r.raw, foldRenderingValue.raw)
}

// foldValue renders what schema writes for entry.
func foldValue(schema *statement.Schema, entry string) foldRendering {
	var foldRenderingValue foldRendering
	switch entry {
	case "examples":
		foldRenderingValue.raw, _, foldRenderingValue.err = annotation.ExamplesRaw(schema)
	case "if":
		foldRenderingValue.raw, _, foldRenderingValue.err = annotation.IfThenElseRaw(schema)
	case "contains":
		foldRenderingValue.raw, _, foldRenderingValue.err = annotation.ContainsRaw(schema)
	default:
		foldRenderingValue.raw, foldRenderingValue.err = annotation.RawFromNode(keyNode(schema, entry))
	}
	return foldRenderingValue
}

// allOfMayRefine reports whether schema's conjuncts may refine: it writes them
// (writesConjuncts), an allOf or a reference beside other keys
// (docs/compilers/openapi.md#allof-refinement). A union beside them is one
// more conjunct, and a $dynamicRef the position does not expand masks no kind
// (admissionReader.conjuncts).
func allOfMayRefine(schema *statement.Schema) bool {
	return writesConjuncts(schema)
}

// allOfDecides reports whether s's allOf is what its position lowers: one the
// dispatch of competing keywords made the value, or one refining, whose rival
// there, an enum or a const beside it, is one of its conjuncts. An allOf that
// value set beats otherwise stays verbatim
// (docs/compilers/openapi.md#competing-keywords). A union beside the composing
// allOf or on one of its entries leaves it deciding, composed into each variant
// the union distributes across, or into the Model keeping the union verbatim or
// composing it as a mixin, and so does a $dynamicRef it keeps from expanding.
func allOfDecides(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	if !writesConjuncts(schema) {
		return false
	}
	if dispatchOf(schema).won == "allOf" {
		return true
	}
	return decisionOf(loweringContext, schema).refines
}

// allOfEntryFoldingKey keys what foldingAllOfEntries read of one schema.
type allOfEntryFoldingKey struct {
	schema *statement.Schema
}

// foldingAllOfEntries reports, for each entry of s's flattened allOf, whether
// it folds: it is inline, it writes something for the position, and each such
// keyword agrees with every other conjunct writing it, s's own keywords among
// them (agreement). The answer reads the whole set, so no conjunct takes
// precedence and no order decides. A null keyword writes nothing
// (WritesKeywordLeftOut). Every use of s asks, so it is read once a run.
func foldingAllOfEntries(loweringContext lowering.Ctx, schema *statement.Schema) []bool {
	return allOfEntryFoldingOf(loweringContext, schema).entryFoldsIntoPosition
}

// allOfEntryFolding is what a schema's allOf says for the position holding it:
// whether each entry of the flattened list folds, whether it lends a use what s
// and the folding entries before it do not, a keyword none of them writes, and
// whether what it folds writes a relative $ref it reads against another base
// than the position does (readsAtPosition), so nothing it writes folds.
type allOfEntryFolding struct {
	entryFoldsIntoPosition          []bool
	addsKeywordNoEarlierEntryWrites []bool
	rebased                         []bool
}

// allOfEntryFoldingOf reads schema's folding once a run.
func allOfEntryFoldingOf(loweringContext lowering.Ctx, schema *statement.Schema) allOfEntryFolding {
	return lowering.ReadOncePerRun(loweringContext, allOfEntryFoldingKey{schema}, func() allOfEntryFolding { return readAllOfEntryFolding(loweringContext, schema) })
}

// readAllOfEntryFolding reads what allOfEntryFoldingOf remembers.
func readAllOfEntryFolding(loweringContext lowering.Ctx, schema *statement.Schema) allOfEntryFolding {
	a := newAgreement(loweringContext)
	addsKeywordNoEarlierEntryWrites := map[string]bool{}
	for _, entry := range a.write(schema) {
		addsKeywordNoEarlierEntryWrites[entry] = addsKeywordNoEarlierEntryWrites[entry] || !writesFalseFlag(entry, foldValue(schema, entry))
	}
	comp := allOfEntries(loweringContext, schema, "")
	n := len(comp.entries)
	out := allOfEntryFolding{entryFoldsIntoPosition: make([]bool, n), addsKeywordNoEarlierEntryWrites: make([]bool, n), rebased: make([]bool, n)}
	writes := make([][]string, n)
	ids := idWrappers(comp)
	for i, entry := range comp.entries {
		if stated(entry.schema.Schema) && entry.schema.Ref == nil {
			if out.rebased[i] = underID(entry, entry.schema.Schema, ids) && foldsReference(loweringContext, entry.schema.Schema) && !readsAtPosition(loweringContext, entry); !out.rebased[i] {
				writes[i] = a.write(entry.schema.Schema)
			}
		}
	}
	for i, entries := range writes {
		out.entryFoldsIntoPosition[i] = len(entries) > 0 && !slices.ContainsFunc(entries, a.apart)
		if !out.entryFoldsIntoPosition[i] {
			continue
		}
		for _, entry := range entries {
			// A false flag lends nothing: it states nothing a later true would not.
			if writesFalseFlag(entry, foldValue(comp.entries[i].schema.Schema, entry)) {
				continue
			}
			out.addsKeywordNoEarlierEntryWrites[i] = out.addsKeywordNoEarlierEntryWrites[i] || !addsKeywordNoEarlierEntryWrites[entry]
			addsKeywordNoEarlierEntryWrites[entry] = true
		}
	}
	return out
}

// idWrappers returns where the flattened wrappers of comp writing an
// identifier are (writesIdentifier), each one that may give what it holds
// another base than the position's.
func idWrappers(comp allOfComposition) map[jsontext.Pointer]bool {
	out := map[jsontext.Pointer]bool{}
	for _, entry := range comp.entries {
		if entry.wrapper && writesIdentifier(entry.schema.Schema) {
			out[entry.pointer] = true
		}
	}
	return out
}

// underID reports whether bs, the inline allOf entry e, or a wrapper holding
// it writes an identifier. Only one can give the entry another base than the
// position holding the allOf, so an entry under none reads at the position
// without a base being looked up. The wrappers holding e are the prefixes of
// its pointer, as many as statement.MaxFlattenDepth allows, so each is looked up.
func underID(allOfEntryValue allOfEntry, bs *statement.Schema, wrappers map[jsontext.Pointer]bool) bool {
	if writesIdentifier(bs) {
		return true
	}
	p := string(allOfEntryValue.pointer)
	for i := strings.LastIndexByte(p, '/'); i > 0; i = strings.LastIndexByte(p[:i], '/') {
		if wrappers[jsontext.Pointer(p[:i])] {
			return true
		}
	}
	return false
}

// writesIdentifier reports whether schema writes `$id`, or `id`, draft 4's
// spelling of it. Either may name a resource, and the base a lookup returns
// says whether it does, so one that names none costs that lookup alone.
func writesIdentifier(schema *statement.Schema) bool {
	return hasKey(schema, "$id") || hasKey(schema, "id")
}

// foldsReference reports whether what bs folds writes a relative $ref, the one
// text a base changes: an entry writing none in it says the same at any base.
func foldsReference(loweringContext lowering.Ctx, bs *statement.Schema) bool {
	return slices.ContainsFunc(bs.Keys, func(key statement.Key) bool {
		entry, fold := foldEntry(loweringContext, bs, key.Name)
		return fold && annotation.WritesReference(foldValue(bs, entry).raw)
	})
}

// readsAtPosition reports whether the inline allOf entry e reads its $refs
// against the base the position holding the allOf does, so what it writes means
// there what it means where it is written: an identifier at or above it naming
// another URI gives the same spelling another schema. The position is e's site
// less e's path from it, as the allOf may be read where a $ref landed. A base
// the bindings cannot tell is no match.
func readsAtPosition(loweringContext lowering.Ctx, allOfEntryValue allOfEntry) bool {
	site, placed := siteOf(allOfEntryValue.schema.Schema)
	at := reftable.DocumentPointer{Doc: site.Doc, Pointer: jsontext.Pointer(strings.TrimSuffix(string(site.Pointer), string(allOfEntryValue.pointer)))}
	own, known := loweringContext.Bindings().Table().Base(site)
	position, knownAt := loweringContext.Bindings().Table().Base(at)
	return placed && known && knownAt && own == position
}

// disagreedWhy words why an entry that would have folded is kept, as n names
// it: its keywords have homes, and what they say is what is in question.
func disagreedWhy(residue []string, entryNamesValue entryNames) string {
	return "the conjuncts of this allOf disagree on a keyword " + entryNamesValue.this + " writes, so nothing it writes folds; " +
		entryNamesValue.kept + " (" + strings.Join(residue, ", ") + ") is kept verbatim under Unmodeled"
}

// disagreed reports whether residue, what an entry that did not fold keeps of
// bs, holds a keyword folding reads: foldingAllOfEntries passes over an entry
// for writing none or for a disagreement, and this tells them apart.
func disagreed(loweringContext lowering.Ctx, bs *statement.Schema, residue []string) bool {
	return slices.ContainsFunc(residue, func(keyword string) bool {
		_, fold := foldEntry(loweringContext, bs, keyword)
		return fold
	})
}

// folds reports whether an entry that folds consumes keyword: homes says which
// bounds the position has a field for, of which a value bound is read where the
// entry's own types take it too (writerTakes), and a `nullable` folds where
// the policy reads it (nullableRead) and foldsNullable says so.
func folds(loweringContext lowering.Ctx, bs *statement.Schema, keyword string, homes boundHomes) bool {
	if keyword == "nullable" {
		return loweringContext.NullableInference.Honours() && nullableRead(loweringContext, bs) && foldsNullable(bs)
	}
	if slices.Contains(valueConstraintKeywords, keyword) {
		return taken(writerTakes(homes.value, bs), keyword)
	}
	_, fold := foldEntry(loweringContext, bs, keyword)
	if slices.Contains(collectionBoundKeywords, keyword) {
		return fold && homes.collection
	}
	return fold
}

// foldsNullable reports whether `nullable` on the entry bs speaks for the whole
// composition: an entry writing properties declares a shape of its own, which
// its `nullable` reads as describing, so it stays conjoined with the rest.
func foldsNullable(bs *statement.Schema) bool {
	return !hasKey(bs, "properties")
}

// positionFoldedEntries returns the voices of s, written at pointer, as a
// reader of what the position holds takes them, target being what the position
// lowered to. An entry whose own node that is, the alias a $ref principal
// writing keywords beside it stands at its entry, keeps what it writes there.
func positionFoldedEntries(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef) []annotation.FoldedAllOfEntry {
	if !allOfDecides(loweringContext, schema) {
		return nil
	}
	comp := allOfEntries(loweringContext, schema, pointer)
	var out []annotation.FoldedAllOfEntry
	for i, speaks := range foldingAllOfEntries(loweringContext, schema) {
		entry := comp.entries[i]
		if !speaks || LoweredToOwnNode(loweringContext, entry.pointer, target) {
			continue
		}
		foldedAllOfEntry := foldedEntryAt(loweringContext, entry.schema.Schema, entry.pointer)
		// The entry keeps a `dependencies` that does not fold (entryKeeps).
		foldedAllOfEntry.Dependencies = foldedAllOfEntry.Dependencies && !foldsNoDependencies(loweringContext, schema, pointer)
		out = append(out, foldedAllOfEntry)
	}
	return out
}

// foldedEntryAt returns the folded entry of node, written at at, which reads
// its `dependencies` in the dialect node is written in, as a fold of it does
// (TwinKeyword): an allOf entry naming a draft by its own `$schema` keeps the
// draft's keyword beside a position read in 2020-12, as the same schema written
// flat in the draft keeps it (docs/compilers/openapi.md#dialect-keywords).
func foldedEntryAt(loweringContext lowering.Ctx, node *statement.Schema, at jsontext.Pointer) annotation.FoldedAllOfEntry {
	return annotation.FoldedAllOfEntry{Node: node, At: at, Dependencies: rulesOf(loweringContext, node).Defines("dependencies")}
}

// lendingEntries returns the entries of s that fold, each at its pointer below
// s, for a use reading what s lends it, but those writing only what s or such
// an entry before them writes: the writers of a keyword a use reads agree
// (foldingAllOfEntries), so a reader taking the nearest takes the same value
// from any of them, and a use of s pays for each keyword once, not for each
// entry repeating it. Bounds, which agree by meeting, are no such keyword: a
// use never reads them through its $ref.
func lendingEntries(loweringContext lowering.Ctx, schema *statement.Schema) []allOfEntry {
	if !allOfDecides(loweringContext, schema) {
		return nil
	}
	comp := allOfEntries(loweringContext, schema, "")
	var out []allOfEntry
	for i, addsKeyword := range allOfEntryFoldingOf(loweringContext, schema).addsKeywordNoEarlierEntryWrites {
		if addsKeyword {
			out = append(out, comp.entries[i])
		}
	}
	return out
}

// declaresScoped reports whether a folded entry writes what only the position
// can hold, as declaresPositionScoped asks of the schema written there.
func declaresScoped(loweringContext lowering.Ctx, foldedEntries []annotation.FoldedAllOfEntry) bool {
	return slices.ContainsFunc(foldedEntries, func(foldedAllOfEntry annotation.FoldedAllOfEntry) bool {
		return declaresPositionScoped(loweringContext, foldedAllOfEntry.Node)
	})
}

// recordFoldedEntryResidue keeps on td, in hand, what binds a use that
// foldedEntries write (recordResidue), each reported at the keyword it wrote,
// in FoldedEntriesInOverwriteOrder' order: an entry several write is the
// nearest folded entry's kept apart, and the schema written at the position
// records after them.
func recordFoldedEntryResidue(loweringContext lowering.Ctx, td ir.TypeDef, schema *statement.Schema, foldedEntries []annotation.FoldedAllOfEntry) []ir.Diagnostic {
	diags := make([]ir.Diagnostic, 0, len(foldedEntries))
	for _, foldedAllOfEntry := range FoldedEntriesInOverwriteOrder(foldedEntries) {
		// What schema writes too is schema's to keep and report: one writing,
		// as the foldedEntries agree.
		diags = append(diags, recordResidue(loweringContext, td.Common(), foldedAllOfEntry.Node, foldedAllOfEntry.At, func(keyword string) bool { return hasKey(schema, keyword) })...)
	}
	return diags
}

// FoldedEntriesInOverwriteOrder returns voices in the order a keeper
// overwriting what each writes takes them: those kept whole, then the rest,
// each the last first. The nearest voice kept apart holds a keyword several
// write, and one kept whole, whose entry holds it (annotation.Voice.KeptWhole),
// only one no other writes, which that entry then holds alone
// (dropFoldedIntoWhole).
func FoldedEntriesInOverwriteOrder(foldedEntries []annotation.FoldedAllOfEntry) []annotation.FoldedAllOfEntry {
	out := make([]annotation.FoldedAllOfEntry, 0, len(foldedEntries))
	for _, whole := range []bool{true, false} {
		for _, foldedAllOfEntry := range slices.Backward(foldedEntries) {
			if foldedAllOfEntry.KeptWhole == whole {
				out = append(out, foldedAllOfEntry)
			}
		}
	}
	return out
}
