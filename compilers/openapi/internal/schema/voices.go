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

// foldEntry returns the one entry a position keeps keyword in once schema, a
// schema of loweringContext, folds, and false for a keyword no fold reads.
// Keywords one entry holds fold as one, so a branch's `then` and another's `if`
// cannot overwrite each other, and a draft's spelling folds as its twin does
// (TwinKeyword).
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
		// A collection bound ir.Constraints cannot state keeps its branch whole.
		return keyword, referenceHolds(schema, keyword)
	}
	twin := TwinKeyword(loweringContext, schema, keyword)
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

// allOfDecides reports whether schema's allOf is what its position lowers: one
// the election of competing keywords made the value, or one refining, whose
// rival there, an enum or a const beside it, is one of its conjuncts. An allOf
// that value set beats otherwise stays verbatim
// (docs/compilers/openapi.md#competing-keywords). A union beside the composing
// allOf or on one of its branches leaves it deciding, composed into each
// variant the union distributes across, or into the Model keeping the union
// verbatim or composing it as a mixin, and so does a $dynamicRef it keeps from
// expanding.
func allOfDecides(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	if !writesConjuncts(schema) {
		return false
	}
	if dispatchOf(schema).won == "allOf" {
		return true
	}
	return decisionOf(loweringContext, schema).refines
}

// allOfBranchFoldingKey keys what foldingAllOfBranches read of one schema.
type allOfBranchFoldingKey struct {
	schema *statement.Schema
}

// foldingAllOfBranches reports, for each branch of schema's flattened allOf,
// whether it folds: it is inline, it writes something for the position, and
// each such keyword agrees with every other conjunct writing it, schema's own
// keywords among them (agreement). The answer reads the whole set, so no
// conjunct takes precedence and no order decides. A null keyword writes nothing
// (WritesKeywordLeftOut). Every use of schema asks, so it is read once a run.
func foldingAllOfBranches(loweringContext lowering.Ctx, schema *statement.Schema) []bool {
	return allOfBranchFoldingOf(loweringContext, schema).branchFoldsIntoPosition
}

// allOfBranchFolding is what a schema's allOf says for the position holding it:
// whether each branch of the flattened list folds, whether it lends a use what
// s and the folding branches before it do not, an entry none of them writes,
// and whether what it folds writes a relative $ref it reads against another
// base than the position does (readsAtPosition), so nothing it writes folds.
type allOfBranchFolding struct {
	branchFoldsIntoPosition          []bool
	addsKeywordNoEarlierBranchWrites []bool
	rebased                          []bool
}

// allOfBranchFoldingOf reads schema's folding once a run.
func allOfBranchFoldingOf(loweringContext lowering.Ctx, schema *statement.Schema) allOfBranchFolding {
	return lowering.ReadOncePerRun(loweringContext, allOfBranchFoldingKey{schema}, func() allOfBranchFolding { return readAllOfBranchFolding(loweringContext, schema) })
}

// readAllOfBranchFolding reads what allOfBranchFoldingOf remembers.
func readAllOfBranchFolding(loweringContext lowering.Ctx, schema *statement.Schema) allOfBranchFolding {
	a := newAgreement(loweringContext)
	addsKeywordNoEarlierBranchWrites := map[string]bool{}
	for _, entry := range a.write(schema) {
		addsKeywordNoEarlierBranchWrites[entry] = addsKeywordNoEarlierBranchWrites[entry] || !writesFalseFlag(entry, foldValue(schema, entry))
	}
	comp := allOfBranches(loweringContext, schema, "")
	n := len(comp.branches)
	out := allOfBranchFolding{branchFoldsIntoPosition: make([]bool, n), addsKeywordNoEarlierBranchWrites: make([]bool, n), rebased: make([]bool, n)}
	writes := make([][]string, n)
	ids := idWrappers(comp)
	for i, allOfBranch := range comp.branches {
		if stated(allOfBranch.schema.Schema) && allOfBranch.schema.Ref == nil {
			if out.rebased[i] = underID(allOfBranch, allOfBranch.schema.Schema, ids) && foldsReference(loweringContext, allOfBranch.schema.Schema) && !readsAtPosition(loweringContext, allOfBranch); !out.rebased[i] {
				writes[i] = a.write(allOfBranch.schema.Schema)
			}
		}
	}
	for i, entries := range writes {
		out.branchFoldsIntoPosition[i] = len(entries) > 0 && !slices.ContainsFunc(entries, a.apart)
		if !out.branchFoldsIntoPosition[i] {
			continue
		}
		for _, entry := range entries {
			// A false flag lends nothing: it states nothing a later true would not.
			if writesFalseFlag(entry, foldValue(comp.branches[i].schema.Schema, entry)) {
				continue
			}
			out.addsKeywordNoEarlierBranchWrites[i] = out.addsKeywordNoEarlierBranchWrites[i] || !addsKeywordNoEarlierBranchWrites[entry]
			addsKeywordNoEarlierBranchWrites[entry] = true
		}
	}
	return out
}

// idWrappers returns where the flattened wrappers of comp writing an
// identifier are (writesIdentifier), each one that may give what it holds
// another base than the position's.
func idWrappers(comp allOfComposition) map[jsontext.Pointer]bool {
	out := map[jsontext.Pointer]bool{}
	for _, allOfBranch := range comp.branches {
		if allOfBranch.wrapper && writesIdentifier(allOfBranch.schema.Schema) {
			out[allOfBranch.pointer] = true
		}
	}
	return out
}

// underID reports whether bs, the inline allOf branch allOfBranchValue, or a
// wrapper holding it writes an identifier. Only one can give the branch another
// base than the position holding the allOf, so a branch under none reads at the
// position without a base being looked up. The wrappers holding
// allOfBranchValue are the prefixes of its pointer, as many as
// statement.MaxFlattenDepth allows, so each is looked up.
func underID(allOfBranchValue allOfBranch, bs *statement.Schema, wrappers map[jsontext.Pointer]bool) bool {
	if writesIdentifier(bs) {
		return true
	}
	p := string(allOfBranchValue.pointer)
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
// text a base changes: a branch writing none in it says the same at any base.
func foldsReference(loweringContext lowering.Ctx, bs *statement.Schema) bool {
	return slices.ContainsFunc(bs.Keys, func(key statement.Key) bool {
		entry, fold := foldEntry(loweringContext, bs, key.Name)
		return fold && annotation.WritesReference(foldValue(bs, entry).raw)
	})
}

// readsAtPosition reports whether the inline allOf branch allOfBranchValue
// reads its $refs against the base the position holding the allOf does, so what
// it writes means there what it means where it is written: an identifier at or
// above it naming another URI gives the same spelling another schema. The
// position is allOfBranchValue's site less allOfBranchValue's path from it, as
// the allOf may be read where a $ref landed. A base the bindings cannot tell is
// no match.
func readsAtPosition(loweringContext lowering.Ctx, allOfBranchValue allOfBranch) bool {
	site, placed := siteOf(allOfBranchValue.schema.Schema)
	at := reftable.DocumentPointer{Doc: site.Doc, Pointer: jsontext.Pointer(strings.TrimSuffix(string(site.Pointer), string(allOfBranchValue.pointer)))}
	own, known := loweringContext.Bindings().Table().Base(site)
	position, knownAt := loweringContext.Bindings().Table().Base(at)
	return placed && known && knownAt && own == position
}

// disagreedWhy words why a branch that would have folded is kept, as
// branchNamesValue names it: its keywords have homes, and what they say is what
// is in question.
func disagreedWhy(residue []string, branchNamesValue branchNames) string {
	return "the conjuncts of this allOf disagree on a keyword " + branchNamesValue.this + " writes, so nothing it writes folds; " +
		branchNamesValue.kept + " (" + strings.Join(residue, ", ") + ") is kept verbatim under Unmodeled"
}

// disagreed reports whether residue, what a branch that did not fold keeps of
// bs, holds a keyword folding reads: foldingAllOfBranches passes over a branch
// for writing none or for a disagreement, and this tells them apart.
func disagreed(loweringContext lowering.Ctx, bs *statement.Schema, residue []string) bool {
	return slices.ContainsFunc(residue, func(keyword string) bool {
		_, fold := foldEntry(loweringContext, bs, keyword)
		return fold
	})
}

// folds reports whether a branch that folds consumes keyword: homes says which
// bounds the position has a field for, of which a value bound is read where the
// branch's own types take it too (writerTakes), and a `nullable` folds where
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

// foldsNullable reports whether `nullable` on the branch bs speaks for the whole
// composition: a branch writing properties declares a shape of its own, which
// its `nullable` reads as describing, so it stays conjoined with the rest.
func foldsNullable(bs *statement.Schema) bool {
	return !hasKey(bs, "properties")
}

// positionFoldedBranches returns the folded branches of schema, written at
// pointer, as a reader of what the position holds takes them, target being what
// the position lowered to. A branch whose own node that is, the alias a $ref
// principal writing keywords beside it stands at its entry, keeps what it
// writes there.
func positionFoldedBranches(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef) []annotation.FoldedAllOfBranch {
	if !allOfDecides(loweringContext, schema) {
		return nil
	}
	comp := allOfBranches(loweringContext, schema, pointer)
	var out []annotation.FoldedAllOfBranch
	for i, speaks := range foldingAllOfBranches(loweringContext, schema) {
		allOfBranch := comp.branches[i]
		if !speaks || LoweredToOwnNode(loweringContext, allOfBranch.pointer, target) {
			continue
		}
		foldedAllOfBranch := foldedBranchAt(loweringContext, allOfBranch.schema.Schema, allOfBranch.pointer)
		// The entry keeps a `dependencies` that does not fold (entryKeeps).
		foldedAllOfBranch.Dependencies = foldedAllOfBranch.Dependencies && !foldsNoDependencies(loweringContext, schema, pointer)
		out = append(out, foldedAllOfBranch)
	}
	return out
}

// foldedBranchAt returns the folded branch of node, written at at, which reads
// its `dependencies` in the dialect node is written in, as a fold of it does
// (TwinKeyword): an allOf entry naming a draft by its own `$schema` keeps the
// draft's keyword beside a position read in 2020-12, as the same schema written
// flat in the draft keeps it (docs/compilers/openapi.md#dialect-keywords).
func foldedBranchAt(loweringContext lowering.Ctx, node *statement.Schema, at jsontext.Pointer) annotation.FoldedAllOfBranch {
	return annotation.FoldedAllOfBranch{Node: node, At: at, Dependencies: rulesOf(loweringContext, node).Defines("dependencies")}
}

// fallbackAllOfBranches returns the branches of schema that fold, each at its
// pointer below schema, for a use reading what schema addsKeyword it, but those
// writing only what schema or such a branch before them writes: the writers of
// an entry a use reads agree (foldingAllOfBranches), so a reader taking the
// nearest takes the same value from any of them, and a use of schema pays for
// each entry once, not for each branch repeating it. Bounds, which agree by
// meeting, are no such entry: a use never reads them through its $ref.
func fallbackAllOfBranches(loweringContext lowering.Ctx, schema *statement.Schema) []allOfBranch {
	if !allOfDecides(loweringContext, schema) {
		return nil
	}
	comp := allOfBranches(loweringContext, schema, "")
	var out []allOfBranch
	for i, addsKeyword := range allOfBranchFoldingOf(loweringContext, schema).addsKeywordNoEarlierBranchWrites {
		if addsKeyword {
			out = append(out, comp.branches[i])
		}
	}
	return out
}

// declaresScoped reports whether a folded branch writes what only the position
// can hold, as declaresPositionScoped asks of the schema written there.
func declaresScoped(loweringContext lowering.Ctx, foldedBranches []annotation.FoldedAllOfBranch) bool {
	return slices.ContainsFunc(foldedBranches, func(foldedAllOfBranch annotation.FoldedAllOfBranch) bool {
		return declaresPositionScoped(loweringContext, foldedAllOfBranch.Node)
	})
}

// recordFoldedBranchResidue keeps on td, in hand, what binds a use that
// foldedBranches write (recordResidue), each reported at the keyword it wrote,
// in FoldedBranchesInOverwriteOrder' order: an entry several write is the
// nearest folded branch's kept apart, and the schema written at the position
// records after them.
func recordFoldedBranchResidue(loweringContext lowering.Ctx, td ir.TypeDef, schema *statement.Schema, foldedBranches []annotation.FoldedAllOfBranch) []ir.Diagnostic {
	diags := make([]ir.Diagnostic, 0, len(foldedBranches))
	for _, foldedAllOfBranch := range FoldedBranchesInOverwriteOrder(foldedBranches) {
		// What schema writes too is schema's to keep and report: one writing,
		// as the foldedBranches agree.
		diags = append(diags, recordResidue(loweringContext, td.Common(), foldedAllOfBranch.Node, foldedAllOfBranch.At, func(keyword string) bool { return hasKey(schema, keyword) })...)
	}
	return diags
}

// FoldedBranchesInOverwriteOrder returns foldedBranches in the order a keeper
// overwriting what each writes takes them: those kept whole, then the rest,
// each the last first. The nearest folded branch kept apart holds a keyword
// several write, and one kept whole, whose branch holds it
// (annotation.FoldedAllOfBranch.KeptWhole), only one no other writes, which
// that branch then holds alone (dropFoldedIntoWhole).
func FoldedBranchesInOverwriteOrder(foldedBranches []annotation.FoldedAllOfBranch) []annotation.FoldedAllOfBranch {
	out := make([]annotation.FoldedAllOfBranch, 0, len(foldedBranches))
	for _, whole := range []bool{true, false} {
		for _, foldedAllOfBranch := range slices.Backward(foldedBranches) {
			if foldedAllOfBranch.KeptWhole == whole {
				out = append(out, foldedAllOfBranch)
			}
		}
	}
	return out
}
