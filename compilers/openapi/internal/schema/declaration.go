// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
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
func homeDeclaration(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, target ir.TypeRef, pointer jsontext.Pointer, home annotation.Home, unhomed bool) (ir.TypeRef, []ir.Diagnostic) {
	ref, diags := hoistDeclarationHome(c, ts, s, target, pointer, home, unhomed)
	if s != nil {
		diags = append(diags, attachDeclaredAnnotations(c, ts, s, pointer)...)
	}
	return ref, append(diags, recordDeclarationResidue(c, ts, s, pointer, home)...)
}

// hoistDeclarationHome gives a declaration a node of its own when its lowering
// left it none and it wrote something only a node can hold
// (declaresPositionScoped; GitHub #116), since a shared node must never carry
// one declaration's annotations.
//
// A declaration already owning a node at pointer resolves to it, checked after
// that gate so a position declaring nothing still resolves to its target. A
// $ref naming the position only promises one (resolvePointer), so no home
// depends on declaration order (docs/ir/models.md#composition-beside-a-union).
//
// unhomed is the census verdict of a $ref site or a collapsed union, which
// declaresPositionScoped cannot judge.
func hoistDeclarationHome(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, ref ir.TypeRef, pointer jsontext.Pointer, home annotation.Home, unhomed bool) (ir.TypeRef, []ir.Diagnostic) {
	if home != annotation.HomeOwnNode || s == nil {
		return ref, nil
	}
	if !unhomed && !declaresPositionScoped(s) {
		return ref, nil
	}
	// The home stands where its target stood, so it keeps what the reference
	// said of null, the marker naming a heuristic that read it in included.
	out := ref
	if id, owned := ts.Lookup(c.IDs().Coordinate(pointer)); owned {
		out.Target = id
		return out, nil
	}
	id, diags := internAlias(c, ts, s, pointer, ref)
	out.Target = id
	return out, diags
}

// declaresPositionScoped reports whether s writes anything that binds the
// position it is written at rather than the shape it lowers to — an annotation
// attachDeclaredAnnotations records, a value constraint internAlias carries, or
// a keyword recordDeclarationResidue keeps. It is the gate on hoisting an alias,
// so it must stay in step with what those actually keep: a keyword listed here
// but stored nowhere would hoist an empty node, and one stored but missing here
// would still be dropped.
func declaresPositionScoped(s *oas3.Schema) bool {
	return declaresAnnotations(s) || declaresValueConstraints(s) ||
		declaresValidationOnly(s) || annotation.DeclaresAny(s, residueKeywords) ||
		declaresContentVocabulary(s) || declaresDynamicRef(s) ||
		annotation.DeclaresAny(s, annotation.DialectKeywords)
}

// declaresAnnotations reports whether s carries documentation, deprecation, a
// redaction, XML hints, vendor extensions, or examples — the TypeCommon fields
// attachDeclaredAnnotations fills.
func declaresAnnotations(s *oas3.Schema) bool {
	if s.GetTitle() != "" || s.GetDescription() != "" || s.GetExternalDocs() != nil {
		return true
	}
	if annotation.EffectiveDeprecated(s, nil) || annotation.DeclaresRedaction(s) || s.GetXML() != nil {
		return true
	}
	if ext := s.GetExtensions(); ext != nil && ext.Len() > 0 {
		return true
	}
	return s.GetExample() != nil || len(s.GetExamples()) > 0
}

// declaresValidationOnly reports whether s writes a validation-only keyword
// that annotation.Read keeps verbatim under Unmodeled. A `false`
// unevaluatedProperties is excluded on purpose: fillAdditional lowers it into
// the model's openness, so it is structure rather than a preserved keyword.
func declaresValidationOnly(s *oas3.Schema) bool {
	if annotation.Written(s.GetNot()) || annotation.Written(s.GetContains()) ||
		annotation.Written(s.GetPropertyNames()) {
		return true
	}
	if annotation.Written(s.GetIf()) || annotation.Written(s.GetThen()) || annotation.Written(s.GetElse()) {
		return true
	}
	if s.GetMinContains() != nil || s.GetMaxContains() != nil {
		return true
	}
	if ds := s.GetDependentSchemas(); ds != nil && ds.Len() > 0 {
		return true
	}
	if annotation.RawPropertyNode(s, "dependentRequired") != nil {
		return true
	}
	up := s.GetUnevaluatedProperties()
	return annotation.Written(s.GetUnevaluatedItems()) || (annotation.Written(up) && !annotation.IsFalseSchema(up))
}

// attachDeclaredAnnotations records every annotation s declares on the type
// node pointer owns, the one structural home they have (ir.TypeCommon).
//
// It is the sole reader of declaration-scoped annotations and runs above
// lower()'s dispatch: every declaration reaches it through homeDeclaration or
// an alias fallback (lowerComponentSchema, hoistSubSchema), so a new lowering
// destination cannot forget to read them (GitHub #114).
//
// A schema whose body reduced to a shared primitive owns no node; its
// annotations stay with the declaring property (FillPropertyDetail). Ownership
// is checked before conversion because callers cover a pointer in either order,
// and only the one that finds a node may emit conversion diagnostics.
func attachDeclaredAnnotations(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	// No node here means a position that reduced to a shared primitive, or one
	// still being built further up this walk, whose builder attaches the same
	// declaration once its build returns (GitHub #749).
	td, ok := ts.NodeAt(c.IDs().Coordinate(pointer))
	if !ok {
		return nil
	}
	a, diags := annotation.Read(annotation.Site{Kind: annotation.Declaration, Node: s}, pointer, c.ProvenanceAt)

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
	diags = append(diags, c.PromoteDeprecation(common.Unmodeled, common.Deprecation, &common.Provenance)...)
	// The enum-openness promotion is applied here rather than where the Enum is
	// built, for the same reason the deprecation one is: a promotion reads the
	// preserved Unmodeled entries, and this is the point at which the
	// declaration's extensions have reached the node's map.
	if enum, isEnum := td.(*ir.Enum); isEnum {
		c.PromoteEnumOpenness(common.Unmodeled, enum, &common.Provenance)
	}
	if len(a.Examples) > 0 {
		common.Examples = a.Examples
	}
	diags = append(diags, recordUnplacedContent(c, &common.Unmodeled, s, td, pointer)...)
	diags = append(diags, recordUnexpandedDynamicRef(c, &common.Unmodeled, s, pointer)...)
	return append(diags, PreserveUnknownKeywords(c, &common.Unmodeled, s, pointer)...)
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
func recordDeclarationResidue(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, home annotation.Home) []ir.Diagnostic {
	// A $ref never lowers a carrier's own schema, which its declaration lowers as
	// HomeCarrier, so a position reaches here as HomeOwnNode only where nothing
	// carries it, and the record here is its only one (GitHub #750).
	if home != annotation.HomeOwnNode || s == nil {
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
// (declaresPositionScoped, via annotation.DeclaresAny) and the recorder that
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
// declaration's own node.
func recordResidue(c lowering.Ctx, common *ir.TypeCommon, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range residueKeywords {
		kept, keptDiags := annotation.PreserveSchemaKeyword(&common.Unmodeled, s, keyword, ir.ReasonNoIRHome, pointer, c.ProvenanceAt)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(keyword),
			"%s on a type declaration binds a use of the type, not the type; it is kept "+
				"verbatim under Unmodeled and applied to a referencing property, header or "+
				"parameter wherever that carrier has a field for it", keyword))
	}
	return diags
}
