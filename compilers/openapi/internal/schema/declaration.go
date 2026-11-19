// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// homeDeclaration gives what schema writes at this position a home and returns the
// reference the position resolves to — an alias over target where the keywords
// need a node of their own, target itself where the position declares none.
//
// It is the one statement of that step, so its two callers, a $ref site
// (refSiteRef) and an allOf branch's $ref (fillRefBranch), keep a declaration
// the same way; a position skipping it would drop what was written at it
// without a word (GitHub #116, #143). The alias's build writes the
// declaration (declarationOn), then fin.
func homeDeclaration(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer, home annotation.Home,
	unhomed bool, fin finisher,
) (ir.TypeRef, []ir.Diagnostic) {
	if !homesDeclaration(loweringContext, schema, target, pointer, home, unhomed || declaresScoped(loweringContext, declarationFoldedBranches(loweringContext, schema, target, pointer))) {
		return target, nil
	}
	var decl finished
	ref, diags := hoistDeclarationHome(loweringContext, ts, schema, target, pointer, decl.on(func(td ir.TypeDef) []ir.Diagnostic {
		return declarationOn(loweringContext, ts, schema, target, pointer, home, td)
	}).then(fin))
	return ref, append(diags, decl.diags...)
}

// declarationFoldedBranches returns the folded branches of the position schema, written at pointer
// and lowered to target, each marked where its entry is kept whole.
func declarationFoldedBranches(loweringContext lowering.Ctx, schema *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer) []annotation.FoldedAllOfBranch {
	return markKeptWhole(loweringContext, schema, pointer, target, positionFoldedBranches(loweringContext, schema, pointer, target))
}

// declarationOn writes the declaration schema, written at pointer and lowered to
// target, on td, in hand, the node standing there: its annotations
// (attachDeclaredAnnotations), and where that node is its home, what binds a
// use, its folded branches' first (recordFoldedBranchResidue, recordResidue). A carrier keeps
// the latter itself.
func declarationOn(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer, home annotation.Home,
	td ir.TypeDef,
) []ir.Diagnostic {
	if !stated(schema) {
		// No schema object, so nothing written for a node to keep.
		return nil
	}
	diags := attachDeclaredAnnotations(loweringContext, ts, schema, pointer, target, td)
	if home != annotation.HomeOwnNode {
		return diags
	}
	diags = append(diags, recordFoldedBranchResidue(loweringContext, td, schema, declarationFoldedBranches(loweringContext, schema, target, pointer))...)
	leaves := leftToWhole(loweringContext, schema, target, pointer)
	return append(diags, recordResidue(loweringContext, td.Common(), schema, pointer, func(keyword string) bool { return slices.Contains(leaves, keyword) })...)
}

// leftToWhole returns the keywords schema, written at pointer and lowered to
// target, writes as the inline principal of its allOf does where it keeps that
// principal whole, or a wrapper around it: the entry holds the one writing, so
// no reader of schema keeps it again, but an extension the promotion policy reads,
// which stays where a promotion finds it
// (docs/compilers/openapi.md#allof-refinement).
func leftToWhole(loweringContext lowering.Ctx, schema *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer) []string {
	if !writesConjuncts(schema) {
		return nil
	}
	refinement, refined := refinementOf(loweringContext, schema, pointer)
	if !refined || refinement.principal == nil || refinement.principal.schema.Ref != nil {
		return nil
	}
	var alike []string
	for _, key := range schema.Keys {
		if keyWrittenAlike(schema, refinement.principal.schema.Schema, key.Name) && !loweringContext.Promoter().Reads(annotation.EntryKey("", key.Name)) {
			alike = append(alike, key.Name)
		}
	}
	if len(alike) == 0 {
		return nil
	}
	for _, keptEntry := range keepsAtOutcome(loweringContext, schema, target, pointer).entries {
		if !keptEntry.keptWhole() || !lowersFrom(refinement.principal, keptEntry.entry) {
			continue
		}
		if _, err := annotation.RawFromNode(keptEntry.entry.schema.Node); err == nil {
			return alike
		}
	}
	return nil
}

// homesDeclaration reports whether the declaration schema, written at pointer and
// lowered to ref, takes a node of its own (hoistDeclarationHome): its lowering
// left it none and it wrote something only a node can hold
// (declaresPositionScoped; GitHub #116), since a shared node must never carry
// one declaration's annotations. One owning its node keeps it, checked after
// that gate so a position declaring nothing resolves to its target. unhomed is
// the census verdict of a position lowered to a reference
// (referenceOutcomeKeywords, or a $ref site), which declaresPositionScoped
// cannot judge.
func homesDeclaration(loweringContext lowering.Ctx, schema *statement.Schema, ref ir.TypeRef, pointer jsontext.Pointer, home annotation.Home, unhomed bool) bool {
	if home != annotation.HomeOwnNode || !stated(schema) {
		return false
	}
	if !unhomed && !declaresPositionScoped(loweringContext, schema) {
		return false
	}
	return !LoweredToOwnNode(loweringContext, pointer, ref)
}

// hoistDeclarationHome interns the alias over ref a declaration homesDeclaration
// gives a node of its own, finished by fin, which the position's first lowering
// builds, so no home turns on declaration order
// (docs/ir/models.md#composition-beside-a-union).
func hoistDeclarationHome(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, ref ir.TypeRef, pointer jsontext.Pointer, fin finisher,
) (ir.TypeRef, []ir.Diagnostic) {
	// The home stands where its target stood, so it keeps what the reference
	// said of null, the marker naming a heuristic that read it in included.
	out := ref
	id, diags := internReferenceAlias(loweringContext, ts, schema, pointer, ref, fin)
	out.Target = id
	return out, diags
}

// hoistUseHome hoists the home of the declaration schema, written at pointer,
// whose body lowered to ref, a use of what it names: over an object alone the
// Model composing it, as internOwnNode builds one, so no Scalar stands over a
// model, a position a carrier holds that a $ref naming it reaches among them
// (usesOneRef); over anything else the alias hoistDeclarationHome builds.
func hoistUseHome(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, ref ir.TypeRef, pointer jsontext.Pointer,
	fin finisher,
) (ir.TypeRef, []ir.Diagnostic) {
	js := loweringContext.Statements().Read(schema)
	if kinds, _ := lowersTo(loweringContext, js); kinds != kindObject || ref.Nullable {
		return hoistDeclarationHome(loweringContext, ts, schema, ref, pointer, fin)
	}
	out := ref
	out.Target = loweringContext.IDs().ForPointer(pointer)
	return out, internOwnNode(loweringContext, ts, js, pointer, ref, fin)
}

// declaresPositionScoped reports whether schema writes anything that binds the
// position it is written at rather than the shape it lowers to — an annotation
// attachDeclaredAnnotations records, a bound internReferenceAlias carries, a
// keyword declarationOn keeps as residue, or one PreserveUnknownKeywords keeps.
// It is the gate on hoisting an alias, so it must stay in step with what those
// actually keep: a keyword listed here but stored nowhere would hoist an empty
// node, and one stored but missing here would still be dropped. A $dynamicRef
// schema is a reference site under (typed.Bindings.RefSite) is its reference, which
// recordUnexpandedDynamicRef keeps nowhere, as a $ref.
func declaresPositionScoped(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	return declaresAnnotations(schema) || declaresValueConstraints(schema) ||
		hasAnyKey(schema, collectionBoundKeywords) ||
		declaresValidationOnly(schema) || hasAnyKey(schema, residueKeywords) ||
		declaresContentVocabulary(schema) || declaresDynamicRef(schema) && schema.Ref == nil ||
		declaresDialectKeyword(schema) ||
		annotation.WritesUnknownKeyword(schema) || len(undefinedKeywords(loweringContext, schema)) > 0
}

// declaresDialectKeyword reports whether schema writes one of the dialect keywords
// annotation keeps out of scope, read from its keys as hasAnyKey reads.
func declaresDialectKeyword(schema *statement.Schema) bool {
	for keyword := range annotation.DialectKeywords() {
		if hasKey(schema, keyword) {
			return true
		}
	}
	return false
}

// declaresAnnotations reports whether schema carries documentation, deprecation, a
// redaction, XML hints, vendor extensions, or examples — the TypeCommon fields
// attachDeclaredAnnotations fills.
func declaresAnnotations(schema *statement.Schema) bool {
	if notes := schema.Notes; notes != nil {
		if notes.Title != "" || notes.Description != "" || notes.ExternalDocs != nil || notes.XML != nil {
			return true
		}
		if notes.Example != nil || len(notes.Examples) > 0 {
			return true
		}
	}
	if annotation.EffectiveDeprecated([]*statement.Schema{schema}) || annotation.DeclaresRedaction(schema) {
		return true
	}
	return len(schema.Extensions) > 0
}

// declaresValidationOnly reports whether schema writes a validation-only keyword
// that annotation.Read keeps verbatim under Unmodeled. A `false`
// unevaluatedProperties counts: only a Model, which owns its pointer and so
// needs no node hoisted, holds it as its openness (closureTyped).
func declaresValidationOnly(schema *statement.Schema) bool {
	if a := schema.Array; a != nil {
		if written(a.Contains) || written(a.UnevaluatedItems) || a.MinContains != nil || a.MaxContains != nil {
			return true
		}
	}
	if object := schema.Object; object != nil && (written(object.PropertyNames) || written(object.UnevaluatedProperties)) {
		return true
	}
	if logic := schema.Logic; logic != nil {
		if written(logic.Not) || written(logic.If) || written(logic.Then) || written(logic.Else) || len(logic.DependentSchemas) > 0 {
			return true
		}
	}
	return hasKey(schema, "dependentRequired") && !annotation.WritesEmptyDependencyMap(schema, "dependentRequired")
}

// written reports whether sub is a schema written at its position, which a
// value written as no schema is not (statement.Unwritten).
func written(sub statement.Subschema) bool {
	return sub.SchemaValueForm() != statement.Unwritten
}

// stated reports whether schema is a schema object, which alone has keys and slots
// to read: a nil schema, a boolean schema, a value written as no schema and a
// mapping the parser read as a boolean are none (statement.SchemaValueForm).
func stated(schema *statement.Schema) bool {
	return schema != nil && schema.Truth == statement.Stated
}

// closureTyped reports whether td, the node schema lowered to, holds schema's
// `unevaluatedProperties: false` as a model's openness, so no entry keeps it:
// the keyword is live (annotation.ClosesAfterComposition), and td is a Model
// closed after composition, or a Union whose variants schema builds itself
// (buildsVariants), one of them such a Model. A `false` allOf branch that
// closed the model outright leaves it holding the keyword no longer.
func closureTyped(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, td ir.TypeDef) bool {
	if !annotation.ClosesAfterComposition(schema) {
		return false
	}
	switch n := td.(type) {
	case *ir.Model:
		return closedAfterComposition(n)
	case *ir.Union:
		return buildsVariants(loweringContext, schema) && slices.ContainsFunc(variantNodes(ts, n), closedAfterComposition)
	default:
		return false
	}
}

// variantNodes returns the registered nodes of union's variants.
func variantNodes(ts *compile.Types, union *ir.Union) []ir.TypeDef {
	nodes := make([]ir.TypeDef, 0, len(union.Variants))
	for _, variant := range union.Variants {
		if td, ok := ts.Node(variant.Type.Target); ok {
			nodes = append(nodes, td)
		}
	}
	return nodes
}

// principalDiscriminatorKeys returns what the discriminator the inline
// principal of schema's allOf writes declares that reaches no IR field, where the
// principal's node stands at schema's position, written at pointer and lowered to
// target (standsAtPosition): that node holds the discriminator as one written
// at the position, so it keeps the object's keys as the position's own
// (annotation.DiscriminatorKeys), which no folded branch's read keeps.
func principalDiscriminatorKeys(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef,
) (ir.Unmodeled, []ir.Diagnostic) {
	refinement, refines := refinementOf(loweringContext, schema, pointer)
	if !refines || !standsAtPosition(loweringContext, refinement, target, pointer) {
		return nil, nil
	}
	return annotation.DiscriminatorKeys(refinement.principal.schema.Schema, refinement.principal.pointer, loweringContext.Place())
}

// closedAfterComposition reports whether td is a Model closed once composition
// is resolved.
func closedAfterComposition(td ir.TypeDef) bool {
	model, ok := td.(*ir.Model)
	return ok && model.Additional == ir.AdditionalClosedAfterComposition
}

// attachDeclaredAnnotations records every annotation schema declares on td, in
// hand, the type node pointer owns, the one structural home they have
// (ir.TypeCommon).
//
// It reads every declaration's annotations, above lower()'s dispatch: each
// reaches it through the finishing of the node its position builds
// (declarationOn) or an alias fallback, so a new lowering destination cannot
// forget to read them (GitHub #114).
//
// A schema whose body reduced to a shared primitive owns no node; its
// annotations stay with the declaring property (FillPropertyDetail). Only the
// build of the node converts them, so its conversion diagnostics are drawn
// once. target says what speaks there (nodeUse).
func attachDeclaredAnnotations(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef,
	td ir.TypeDef,
) []ir.Diagnostic {
	use := nodeUse(loweringContext, schema, pointer, target)
	site := use.SiteAt(loweringContext, schema, pointer)
	site.Closure = closureTyped(loweringContext, ts, schema, td)
	site.Leaves = leftToWhole(loweringContext, schema, target, pointer)
	a, diags := annotation.Read(site, pointer, loweringContext.Place())
	held, heldDiags := principalDiscriminatorKeys(loweringContext, schema, pointer, target)
	a.Unmodeled = annotation.MergeUnmodeled(a.Unmodeled, held)
	diags = append(diags, heldDiags...)

	common := td.Common()
	common.Docs = a.Docs
	if a.Deprecated {
		common.Deprecation = &ir.Deprecation{}
	}
	if a.Sensitive {
		common.Sensitive = true
	}
	if a.XML != nil {
		common.XML = a.XML
	}
	common.Unmodeled = annotation.MergeUnmodeled(common.Unmodeled, a.Unmodeled)
	// A discriminator the census kept whole holds the extensions just read out
	// of it, which the node keeps no second time (GitHub #849).
	promotion.DropContained(loweringContext.Promoter(), &common.Unmodeled, "openapi:discriminator")
	dropFoldedIntoWhole(loweringContext, &common.Unmodeled)
	from := use.fallbackKeys(loweringContext)
	diags = append(diags, loweringContext.Promoter().Deprecation(common.Unmodeled, from, &common.Deprecation, &common.Provenance)...)
	diags = append(diags, loweringContext.Promoter().Redaction(common.Unmodeled, from, &common.Sensitive, &common.Provenance)...)
	// The openness promotions are applied here rather than where the Enum or
	// Union is built, for the same reason the deprecation one is: a promotion
	// reads the preserved Unmodeled entries, and this is the point at which the
	// declaration's extensions have reached the node's map.
	switch n := td.(type) {
	case *ir.Enum:
		diags = append(diags, promotion.EnumOpenness(loweringContext.Promoter(), common.Unmodeled, &n.Closed, &common.Provenance)...)
	case *ir.Union:
		// A target names what the document writes, so the union a mixed enum
		// lowers to reads the enum's, as the enum would, and only that one: one
		// fact read through two targets would let either override the other.
		openness := promotion.UnionOpenness
		if dispatchOf(schema).won == "enum" {
			openness = promotion.EnumOpenness
		}
		diags = append(diags, openness(loweringContext.Promoter(), common.Unmodeled, &n.Closed, &common.Provenance)...)
	default:
		// Openness is a fact of an enum's or a union's value space alone.
	}
	if len(a.Examples) > 0 {
		common.Examples = a.Examples
	}
	diags = append(diags, recordUnplacedContent(loweringContext, &common.Unmodeled, schema, td, pointer)...)
	diags = append(diags, recordUnexpandedDynamicRef(loweringContext, &common.Unmodeled, schema, pointer)...)
	return append(diags, PreserveUnknownKeywords(loweringContext, &common.Unmodeled, schema, pointer, td, site.Leaves...)...)
}

// residueKeywords are the keywords a schema can write that bind a *use* of the
// type rather than the type itself, so no type node has a field to hold them:
// `default` (Property/Parameter.Default is its only home) and
// readOnly/writeOnly (Property.Visibility is theirs).
//
// One list, read by both the predicate that hoists a node for them
// (declaresPositionScoped, via hasAnyKey) and the recorder that
// fills it (recordResidue), so the two can never drift into either half of
// declaresPositionScoped's trap.
var residueKeywords = []string{"default", "readOnly", "writeOnly"}

// ResidueKeywords returns that list, for the carrier lowerings outside this
// package that preserve the same set at their own positions.
//
// It hands back a copy rather than the slice. Being one list is the whole point
// — a keyword added here has to reach every position that preserves one — and an
// exported slice is a mutable global: any importer could rewrite what every
// schema position in the process preserves, silently and for good.
func ResidueKeywords() []string { return slices.Clone(residueKeywords) }

// recordResidue keeps each declared residue keyword verbatim in
// common.Unmodeled and reports it at the keyword's own pointer.
//
// The message says the keyword is applied wherever a carrier has a field for
// it: `default` at every carrier, readOnly/writeOnly at a property or header
// but not at a parameter, which has no Visibility field. So a $ref'd
// declaration's readOnly reaches a referencing parameter only here, on the
// declaration's own node. A default no ir.Value holds reaches no carrier
// (residueReach).
func recordResidue(loweringContext lowering.Ctx, common *ir.TypeCommon, schema *statement.Schema, pointer jsontext.Pointer, left func(string) bool,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range residueKeywords {
		if left(keyword) {
			continue
		}
		kept, keptDiags := annotation.PreserveSchemaKeyword(&common.Unmodeled, schema, keyword, ir.ReasonNoIRHome, pointer, loweringContext.Place())
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(keyword),
			"%s on a type declaration binds a use of the type, not the type; it is kept "+
				"verbatim under Unmodeled and %s", keyword, residueReach(schema, keyword)))
	}
	return diags
}

// residueReach says where the residue keyword schema writes reaches: each
// referencing carrier with a field for it, but for a default no ir.Value
// holds, which no carrier takes (Use.Default, which reports it).
func residueReach(schema *statement.Schema, keyword string) string {
	if keyword == "default" {
		if _, err := value.FromNode(schema.Default()); err != nil {
			return "applied to no carrier, as no IR value holds it"
		}
	}
	return "applied to a referencing property, header or parameter wherever that carrier has a field for it"
}
