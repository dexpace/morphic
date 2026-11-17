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

// homeDeclaration gives what s writes at this position a home and returns the
// reference the position resolves to — an alias over target where the keywords
// need a node of their own, target itself where the position declares none.
//
// It is the one statement of that step, so every schema position keeps its
// declaration the same way: a $ref site, a lowered body, and an allOf branch's
// $ref, which reached none of it until it was routed here. A position that
// skips it drops what was written at it without a word — which is what both
// GitHub #116 and #143 were.
func homeDeclaration(c lowering.Ctx, ts *compile.Types, s *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer, home annotation.Home, unhomed bool) (ir.TypeRef, []ir.Diagnostic) {
	voices := positionVoices(c, s, pointer, target)
	ref, diags := hoistDeclarationHome(c, ts, s, target, pointer, home, unhomed || declaresScoped(c, voices))
	if stated(s) {
		diags = append(diags, attachDeclaredAnnotations(c, ts, s, pointer, target)...)
	}
	diags = append(diags, recordVoicedResidue(c, ts, voices, pointer, home)...)
	return ref, append(diags, recordDeclarationResidue(c, ts, s, pointer, home)...)
}

// hoistDeclarationHome gives a declaration a node of its own when its lowering
// left it none and it wrote something only a node can hold
// (declaresPositionScoped; GitHub #116), since a shared node must never carry
// one declaration's annotations.
//
// One owning its node keeps it, checked after that gate so a position declaring
// nothing resolves to its target. Any other interns its alias, which the first
// lowering of the position builds, so no home turns on declaration order
// (docs/ir/models.md#composition-beside-a-union).
//
// unhomed is the census verdict of a position lowered to a reference
// (referenceOutcomeKeywords, or a $ref site), which declaresPositionScoped
// cannot judge.
func hoistDeclarationHome(c lowering.Ctx, ts *compile.Types, s *statement.Schema, ref ir.TypeRef, pointer jsontext.Pointer, home annotation.Home, unhomed bool) (ir.TypeRef, []ir.Diagnostic) {
	if home != annotation.HomeOwnNode || !stated(s) {
		return ref, nil
	}
	if !unhomed && !declaresPositionScoped(c, s, pointer) {
		return ref, nil
	}
	if LoweredToOwnNode(c, pointer, ref) {
		return ref, nil
	}
	// The home stands where its target stood, so it keeps what the reference
	// said of null, the marker naming a heuristic that read it in included.
	out := ref
	id, diags := internReferenceAlias(c, ts, s, pointer, ref)
	out.Target = id
	return out, diags
}

// declaresPositionScoped reports whether s writes anything that binds the
// position it is written at rather than the shape it lowers to — an annotation
// attachDeclaredAnnotations records, a bound internReferenceAlias carries, a
// keyword recordDeclarationResidue keeps, or one PreserveUnknownKeywords keeps.
// It is the gate on hoisting an alias, so it must stay in step with what those
// actually keep: a keyword listed here but stored nowhere would hoist an empty
// node, and one stored but missing here would still be dropped. A $dynamicRef
// making pointer a reference site (typed.Bindings.RefSite) is its reference,
// which recordUnexpandedDynamicRef keeps nowhere, as a $ref.
func declaresPositionScoped(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) bool {
	return declaresAnnotations(s) || declaresValueConstraints(s) ||
		hasAnyKey(s, collectionBoundKeywords) ||
		declaresValidationOnly(s) || hasAnyKey(s, residueKeywords) ||
		declaresContentVocabulary(s) || declaresDynamicRef(s) && heldAt(c, pointer).Ref == nil ||
		declaresDialectKeyword(s) ||
		annotation.WritesUnknownKeyword(s) || len(undefinedKeywords(c, s)) > 0
}

// declaresDialectKeyword reports whether s writes one of the dialect keywords
// annotation keeps out of scope, read from its keys as hasAnyKey reads.
func declaresDialectKeyword(s *statement.Schema) bool {
	for keyword := range annotation.DialectKeywords() {
		if hasKey(s, keyword) {
			return true
		}
	}
	return false
}

// declaresAnnotations reports whether s carries documentation, deprecation, a
// redaction, XML hints, vendor extensions, or examples — the TypeCommon fields
// attachDeclaredAnnotations fills.
func declaresAnnotations(s *statement.Schema) bool {
	if n := s.Notes; n != nil {
		if n.Title != "" || n.Description != "" || n.ExternalDocs != nil || n.XML != nil {
			return true
		}
		if n.Example != nil || len(n.Examples) > 0 {
			return true
		}
	}
	if annotation.EffectiveDeprecated([]*statement.Schema{s}) || annotation.DeclaresRedaction(s) {
		return true
	}
	return len(s.Extensions) > 0
}

// declaresValidationOnly reports whether s writes a validation-only keyword
// that annotation.Read keeps verbatim under Unmodeled. A `false`
// unevaluatedProperties counts: only a Model, which owns its pointer and so
// needs no node hoisted, holds it as its openness (closureTyped).
func declaresValidationOnly(s *statement.Schema) bool {
	if a := s.Array; a != nil {
		if written(a.Contains) || written(a.UnevaluatedItems) || a.MinContains != nil || a.MaxContains != nil {
			return true
		}
	}
	if o := s.Object; o != nil && (written(o.PropertyNames) || written(o.UnevaluatedProperties)) {
		return true
	}
	if l := s.Logic; l != nil {
		if written(l.Not) || written(l.If) || written(l.Then) || written(l.Else) || len(l.DependentSchemas) > 0 {
			return true
		}
	}
	return hasKey(s, "dependentRequired") && !annotation.WritesEmptyDependencyMap(s, "dependentRequired")
}

// written reports whether sub is a schema written at its position, which a
// value written as no schema is not (statement.Unwritten).
func written(sub statement.Sub) bool {
	return sub.Truth() != statement.Unwritten
}

// stated reports whether s is a schema object, which alone has keys and slots
// to read: a nil s, a boolean schema, a value written as no schema and a
// mapping the parser read as a boolean are none (statement.Truth).
func stated(s *statement.Schema) bool {
	return s != nil && s.Truth == statement.Stated
}

// closureTyped reports whether td, the node s lowered to, holds s's
// `unevaluatedProperties: false` as a model's openness, so no entry keeps it:
// the keyword is live (annotation.ClosesAfterComposition), and td is a Model
// closed after composition, or a Union whose variants s builds itself
// (buildsVariants), one of them such a Model. A `false` allOf branch that
// closed the model outright leaves it holding the keyword no longer.
func closureTyped(c lowering.Ctx, ts *compile.Types, s *statement.Schema, td ir.TypeDef) bool {
	if !annotation.ClosesAfterComposition(s) {
		return false
	}
	switch n := td.(type) {
	case *ir.Model:
		return closedAfterComposition(n)
	case *ir.Union:
		return buildsVariants(c, s) && slices.ContainsFunc(variantNodes(ts, n), closedAfterComposition)
	default:
		return false
	}
}

// closedAfterComposition reports whether td is a Model closed once composition
// is resolved.
func closedAfterComposition(td ir.TypeDef) bool {
	m, ok := td.(*ir.Model)
	return ok && m.Additional == ir.AdditionalClosedAfterComposition
}

// attachDeclaredAnnotations records every annotation s declares on the type
// node pointer owns, the one structural home they have (ir.TypeCommon).
//
// It reads every declaration's annotations, above lower()'s dispatch: each
// reaches it through homeDeclaration or an alias fallback, so a new lowering
// destination cannot forget to read them (GitHub #114).
//
// A schema whose body reduced to a shared primitive owns no node; its
// annotations stay with the declaring property (FillPropertyDetail). Ownership
// is checked before conversion because callers cover a pointer in either order,
// and only the one that finds a node may emit conversion diagnostics. target
// says what speaks there (nodeUse).
func attachDeclaredAnnotations(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef) []ir.Diagnostic {
	// No node here means a position that reduced to a shared primitive, or one
	// still being built further up this walk, whose builder attaches the same
	// declaration once its build returns (GitHub #749).
	td, ok := ts.NodeAt(c.IDs().Coordinate(pointer))
	if !ok {
		return nil
	}
	use := nodeUse(c, s, pointer, target)
	site := use.SiteAt(c, s, pointer)
	site.Closure = closureTyped(c, ts, s, td)
	a, diags := annotation.Read(site, pointer, c.Place())

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
	promotion.DropContained(c.Promoter(), &common.Unmodeled, "openapi:discriminator")
	from := use.lentKeys(c)
	diags = append(diags, c.Promoter().Deprecation(common.Unmodeled, from, &common.Deprecation, &common.Provenance)...)
	diags = append(diags, c.Promoter().Redaction(common.Unmodeled, from, &common.Sensitive, &common.Provenance)...)
	// The openness promotions are applied here rather than where the Enum or
	// Union is built, for the same reason the deprecation one is: a promotion
	// reads the preserved Unmodeled entries, and this is the point at which the
	// declaration's extensions have reached the node's map.
	switch n := td.(type) {
	case *ir.Enum:
		diags = append(diags, promotion.EnumOpenness(c.Promoter(), common.Unmodeled, &n.Closed, &common.Provenance)...)
	case *ir.Union:
		// A target names what the document writes, so the union a mixed enum
		// lowers to reads the enum's, as the enum would, and only that one: one
		// fact read through two targets would let either override the other.
		openness := promotion.UnionOpenness
		if dispatchOf(s).won == "enum" {
			openness = promotion.EnumOpenness
		}
		diags = append(diags, openness(c.Promoter(), common.Unmodeled, &n.Closed, &common.Provenance)...)
	default:
		// Openness is a fact of an enum's or a union's value space alone.
	}
	if len(a.Examples) > 0 {
		common.Examples = a.Examples
	}
	diags = append(diags, recordUnplacedContent(c, &common.Unmodeled, s, td, pointer)...)
	diags = append(diags, recordUnexpandedDynamicRef(c, &common.Unmodeled, s, pointer)...)
	return append(diags, PreserveUnknownKeywords(c, &common.Unmodeled, s, pointer, td)...)
}

// recordDeclarationResidue keeps, on the node pointer owns, every keyword the
// declaration wrote that only a use site can hold. The rule
// docs/compilers/openapi.md#use-bound-annotations applies them to referencing
// properties, which the property path already does; this covers a declaration
// nothing references (GitHub #138).
//
// It runs only at an annotation.HomeOwnNode position, which has no carrier.
// Every annotation.HomeCarrier position already keeps them in its own field:
// `default` in a default field, readOnly/writeOnly in ir.Property.Visibility
// or, at a parameter, verbatim on the ir.Parameter (preserveParamVisibility).
// A pointer with no node wrote none (declaresPositionScoped hoists one for any
// it wrote) or is mid-build, and its builder records them.
func recordDeclarationResidue(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer, home annotation.Home) []ir.Diagnostic {
	// A $ref never lowers a carrier's own schema, which its declaration lowers as
	// HomeCarrier, so a position reaches here as HomeOwnNode only where nothing
	// carries it, and the record here is its only one (GitHub #750).
	if home != annotation.HomeOwnNode || !stated(s) {
		return nil
	}
	td, ok := ts.NodeAt(c.IDs().Coordinate(pointer))
	if !ok {
		return nil
	}
	return recordResidue(c, td.Common(), s, pointer)
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
func recordResidue(c lowering.Ctx, common *ir.TypeCommon, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range residueKeywords {
		kept, keptDiags := annotation.PreserveSchemaKeyword(&common.Unmodeled, s, keyword, ir.ReasonNoIRHome, pointer, c.Place())
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(keyword),
			"%s on a type declaration binds a use of the type, not the type; it is kept "+
				"verbatim under Unmodeled and %s", keyword, residueReach(s, keyword)))
	}
	return diags
}

// residueReach says where the residue keyword s writes reaches: each
// referencing carrier with a field for it, but for a default no ir.Value
// holds, which no carrier takes (Use.Default, which reports it).
func residueReach(s *statement.Schema, keyword string) string {
	if keyword == "default" {
		if _, err := value.FromNode(defaultOf(s)); err != nil {
			return "applied to no carrier, as no IR value holds it"
		}
	}
	return "applied to a referencing property, header or parameter wherever that carrier has a field for it"
}
