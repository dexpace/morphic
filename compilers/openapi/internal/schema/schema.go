package schema

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// LowerComponentSchemas interns every named component schema in source order.
// It is the entry Compile's run() calls before any operation lowering so that
// $refs resolve to already-registered IDs.
//
// ctx bounds the walk in time: a document declares as many components as it
// likes, so this loop is one of the two places a compile does work proportional
// to nothing the compiler chose. Cancellation stops it between components and
// returns what was lowered so far; the caller — run — sees ctx.Err() at the
// phase boundary immediately after and refuses the document there, so a partial
// registry never becomes a Document.
func LowerComponentSchemas(ctx context.Context, c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex) []ir.Diagnostic {
	comps := c.Doc.Components
	if comps == nil {
		return nil
	}
	schemas := comps.GetSchemas()
	if schemas == nil {
		return nil
	}
	var diags []ir.Diagnostic
	// The declared-name index the $ref and discriminator-mapping resolutions read
	// is derived at entry (lowering.New), so a component declared later in the
	// document is already a valid target here regardless of source order.
	for name, js := range schemas.All() {
		if ctx.Err() != nil {
			return diags
		}
		diags = append(diags, lowerComponentSchema(c, ts, anchors, js, ids.Ptr("components", "schemas", name), name)...)
	}
	return diags
}

// lowerComponentSchema lowers one named component schema and guarantees a node
// is registered at the component's own TypeID even when its body reduces to a
// shared primitive/any or aliases another type. Without this, a component like
// `MyId: {type: string, format: uuid}` would leave nothing at its component
// pointer and every $ref to it would dangle (invariants 1 and 2).
func lowerComponentSchema(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer, name string) []ir.Diagnostic {
	s := annotation.At(js)
	ref, diags := Ref(c, ts, anchors, TopLevelDepth, js, pointer, name)
	// A body that interned the component's own node at its component ID needs no
	// alias, and its annotations were attached where it was lowered.
	if _, owned := ts.Lookup(string(pointer)); owned {
		return diags
	}
	var kept ir.Unmodeled
	cons, consDiags := schemaConstraints(c, &kept, s.Node, pointer)
	diags = append(diags, consDiags...)
	internAlias(c, ts, pointer, name, ref, cons, kept)
	// This alias is the first node the pointer owns, so the annotations
	// schemaBody had nowhere to put now have a home.
	if s.Node != nil {
		diags = append(diags, attachDeclaredAnnotations(c, ts, anchors, s.Node, pointer)...)
	}
	return diags
}

// recordDeclarationResidue keeps, on the node pointer owns, every keyword the
// declaration wrote that only a use site can hold. ir-design §14 applies them
// to referencing properties, which the property path already does; this covers
// a declaration nothing references (GitHub #138).
//
// It runs only at an annotation.HomeOwnNode position, which has no carrier.
// Every annotation.HomeCarrier position already keeps them in its own field:
// `default` in a default field, readOnly/writeOnly in ir.Property.Visibility
// or, at a parameter, verbatim on the ir.Parameter (preserveParamVisibility).
// A pointer with no node wrote none (declaresPositionScoped hoists one for any
// it wrote) or is mid-build, and its builder records them.
func recordDeclarationResidue(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, home annotation.Home) []ir.Diagnostic {
	// A $ref aimed at a carrier's own schema reaches here as HomeOwnNode. When the
	// declaration then rebuilds that node, what this records goes with it but the
	// report stays (GitHub #750).
	if home != annotation.HomeOwnNode || s == nil {
		return nil
	}
	td, ok := ts.NodeAt(string(pointer))
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
		kept, keptDiags := PreserveSchemaKeyword(c, &common.Unmodeled, s, keyword, ir.ReasonNoIRHome, pointer+ids.Ptr(keyword))
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

// schemaConstraints reads the value constraints of a schema and stamps each
// constraint diagnostic with pointer's provenance. It returns nil constraints
// when the schema writes none. It is the shared path for every alias a body
// reduces to, a named component (lowerComponentSchema) or a $ref-hoisted
// sub-schema (hoistSubSchema), so a scalar aliasing a shared primitive keeps
// its constraints, including a bound written beside a $ref.
//
// p is the Unmodeled map of the carrier the constraints land on. A co-declared
// numeric bound with no field in ir.Constraints is kept there, beside the
// constraints it did not reach (GitHub #286).
func schemaConstraints(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) (*ir.Constraints, []ir.Diagnostic) {
	if s == nil {
		return nil, nil
	}
	cons, kept, diags := annotation.Constraints(s, c.ExclusiveBoundIsBoolean(), pointer, c.ProvenanceAt)
	*p = annotation.MergeUnmodeled(*p, kept)
	return cons, StampConstraintDiags(c, diags, pointer)
}

// StampConstraintDiags gives every constraint diagnostic the provenance of the
// pointer that read the schema, which is what makes two reads of one sub-schema
// — its owning property and a $ref that hoists it — identical and so deduped by
// Diags.Append rather than reported twice.
func StampConstraintDiags(c lowering.Ctx, diags []ir.Diagnostic, pointer jsontext.Pointer) []ir.Diagnostic {
	for i := range diags {
		diags[i].Provenance = c.ProvenanceAt(pointer)
	}
	return diags
}

// internAlias interns a named Scalar at pointer whose Base is target, so a
// component (or a sibling-carrying schema) whose body lowered to a shared or
// referenced target still owns a resolvable node at its own TypeID. Any value
// constraints the schema carried are attached so a scalar component never drops
// them, and kept holds what those constraints had no field for — the co-declared
// bound keyword schemaConstraints read alongside them.
func internAlias(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, hint string,
	target ir.TypeRef, constraints *ir.Constraints, kept ir.Unmodeled,
) ir.TypeID {
	return internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := target
		common.Unmodeled = annotation.MergeUnmodeled(common.Unmodeled, kept)
		return &ir.Scalar{TypeCommon: common, Base: &base, Constraints: constraints}
	})
}

// schemaBody lowers a concrete (non-reference) schema body to a TypeRef and
// records the schema's own annotations on whatever node the lowering hoisted at
// pointer. It is shared by Ref and by sub-schema hoisting
// (resolveSchemaRef), which both reach a body only after peeling off any
// leading $ref.
func schemaBody(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, schema *oas3.Schema, pointer jsontext.Pointer, hint string, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
	target, diags := lowerSchemaBody(c, ts, anchors, depth, schema, pointer, hint)
	// No census verdict: a body's census ran inside lower(), against the node the
	// walk actually built, which is the only thing that can answer it.
	ref, homeDiags := homeDeclaration(c, ts, anchors, schema, target, pointer, hint, home, false)
	return ref, append(diags, homeDiags...)
}

// hoistDeclarationHome gives a declaration a node of its own when its lowering
// left it none and it wrote something only a node can hold
// (declaresPositionScoped; GitHub #116), since a shared node must never carry
// one declaration's annotations.
//
// A declaration that already has a home resolves to it, checked after that gate
// so a position declaring nothing still resolves to its target. A $ref naming
// an inline position hoists that home first (resolveSchemaRef); taking the
// body's target would make annotations depend on declaration order (ir-design
// §4.3).
//
// unhomed is the caller's census verdict for a $ref site's keywords, which
// declaresPositionScoped cannot judge.
func hoistDeclarationHome(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, ref ir.TypeRef, pointer jsontext.Pointer, hint string, home annotation.Home, unhomed bool) (ir.TypeRef, []ir.Diagnostic) {
	if home != annotation.HomeOwnNode || s == nil {
		return ref, nil
	}
	if !unhomed && !declaresPositionScoped(s) {
		return ref, nil
	}
	if id, owned := ts.Lookup(string(pointer)); owned {
		return ir.TypeRef{Target: id, Nullable: ref.Nullable}, nil
	}
	var kept ir.Unmodeled
	cons, diags := schemaConstraints(c, &kept, s, pointer)
	id := internAlias(c, ts, pointer, hint, ref, cons, kept)
	return ir.TypeRef{Target: id, Nullable: ref.Nullable}, diags
}

// refSiteHomesNothing reports whether a $ref position written as s holds
// nothing an alias would: no keyword beside the $ref that refSiteRef keeps on
// one (refSiteUnhomedKeywords, declaresUnion), and none hoistDeclarationHome
// homes on one (declaresPositionScoped). Such a position lowers to its
// target's type alone.
func refSiteHomesNothing(s *oas3.Schema) bool {
	return len(refSiteUnhomedKeywords(s, nil)) == 0 && !declaresUnion(s) && !declaresPositionScoped(s)
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

// declaresAnnotations reports whether s carries documentation, deprecation, XML
// hints, vendor extensions, or examples — the five TypeCommon fields
// attachDeclaredAnnotations fills.
func declaresAnnotations(s *oas3.Schema) bool {
	if s.GetTitle() != "" || s.GetDescription() != "" || s.GetExternalDocs() != nil {
		return true
	}
	if annotation.EffectiveDeprecated(s, nil) || s.GetXML() != nil {
		return true
	}
	if ext := s.GetExtensions(); ext != nil && ext.Len() > 0 {
		return true
	}
	return s.GetExample() != nil || len(s.GetExamples()) > 0
}

// valueConstraintKeywords are the keywords annotation.Constraints reads into an
// ir.Constraints, sorted — which is both the order declaredConstraints walks
// them in and what makes the list comparable to the reader it must keep pace
// with.
//
// One list, read by the predicate that hoists a node for them
// (declaresValueConstraints) and by the recorder that keeps the ones no node can
// hold (declaredConstraints), so the two can never disagree about what a schema
// wrote. Collection bounds are absent because they are List-owned and read by
// listConstraints, not here.
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

// declaresValidationOnly reports whether s writes a §4.7 validation-only
// keyword that preserveKeyword keeps verbatim under Unmodeled. A `false`
// unevaluatedProperties is excluded on purpose: fillAdditional lowers it into
// the model's openness, so it is structure rather than a preserved keyword.
func declaresValidationOnly(s *oas3.Schema) bool {
	if s.GetNot() != nil || s.GetContains() != nil || s.GetPropertyNames() != nil {
		return true
	}
	if s.GetIf() != nil || s.GetThen() != nil || s.GetElse() != nil {
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
	return s.GetUnevaluatedItems() != nil || (up != nil && !annotation.IsFalseSchema(up))
}

// lowerSchemaBody lowers the body itself, handling the $dynamicRef expansion and
// the oneOf/anyOf dispatch that precede structural lowering.
func lowerSchemaBody(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, schema *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeRef, []ir.Diagnostic) {
	target, _, expanded, diags := dynamicExpansion(c, anchors, schema, pointer)
	if expanded {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DynamicRefExpanded, pointer+ids.Ptr("$dynamicRef"),
			"$dynamicRef expanded to %q, the one matching $dynamicAnchor in this document", target))
		return ir.TypeRef{Target: target, Nullable: schemaAdmitsNull(schema)}, diags
	}
	if len(schema.GetOneOf()) > 0 || len(schema.GetAnyOf()) > 0 {
		if hasUnionSiblings(schema) {
			id, unionDiags := lowerCoDeclaredUnion(c, ts, anchors, depth, schema, pointer, hint)
			return ir.TypeRef{Target: id, Nullable: schemaAdmitsNull(schema)}, append(diags, unionDiags...)
		}
		ref, unionDiags := lowerOneOfAnyOf(c, ts, anchors, depth, schema, pointer, hint)
		return ref, append(diags, unionDiags...)
	}
	id, bodyDiags := lower(c, ts, anchors, depth, schema, pointer, hint)
	return ir.TypeRef{Target: id, Nullable: schemaAdmitsNull(schema)}, append(diags, bodyDiags...)
}

// hasUnionSiblings reports whether a oneOf/anyOf schema also carries structural
// keywords (a type, properties, allOf, const/enum, additionalProperties, ...) or
// a keyword that narrows an instance without declaring one. When it does, the
// union alone cannot represent the schema, so the sibling body must be lowered
// too rather than dropped (invariant 2, ir-design §4.3).
func hasUnionSiblings(s *oas3.Schema) bool {
	return narrowsInstance(s) || declaresShape(s)
}

// narrowsInstance reports whether s writes a keyword that constrains an instance
// without declaring a shape to build: `required`, `items`, `prefixItems`.
//
// These sit on the narrowing side of declaresShape's line, and that line is about
// what to *build*. Wherever the question is instead whether a lowering can carry
// what the position wrote, the distinction does not apply: an ir.Union has no
// element type, so `{oneOf: [...], items: {...}}` loses the `items` as surely as
// it would lose a co-declared property set.
func narrowsInstance(s *oas3.Schema) bool {
	return len(s.GetRequired()) > 0 || s.GetItems() != nil || len(s.GetPrefixItems()) > 0
}

// declaresShape reports whether a schema declares data shape — a type, a
// property set, a value set, composition, or a catch-all — rather than only
// narrowing what an already-declared shape accepts. The narrowing keywords are
// narrowsInstance's, which is why the callers combine the two (ir-design §4.7).
func declaresShape(s *oas3.Schema) bool {
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return true
	}
	if s.GetConst() != nil || enumWritten(s) || len(s.GetAllOf()) > 0 {
		return true
	}
	if s.GetAdditionalProperties() != nil {
		return true
	}
	if pp := s.GetPatternProperties(); pp != nil && pp.Len() > 0 {
		return true
	}
	return len(effectiveTypes(s)) > 0
}

// lowerBesideUnmodeledUnion lowers the structural body of a schema that
// co-declares oneOf/anyOf and keeps the union verbatim beside it, so neither the
// structural shape nor the union is dropped. reason says which kind of union it
// is and why says what stopped a classified lowering; classifyUnionSiblings
// picks both.
func lowerBesideUnmodeledUnion(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string, reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
	inner, diags := lower(c, ts, anchors, depth, s, pointer, hint)
	owner := inner
	if got, _ := ts.Lookup(string(pointer)); got != inner {
		// The structural body reduced to a shared/aliased target; hoist an alias
		// so the preserved union attaches to a node this pointer owns, never to a
		// shared primitive. The alias carries the position's value constraints for
		// the reason hoistByteScalar records: owning the node is what stops
		// hoistDeclarationHome hoisting the alias that would otherwise carry them.
		// kept travels with them, so the co-declared bound keyword that reaches no
		// Constraints field lands on the same node as the bounds it lost to.
		var kept ir.Unmodeled
		cons, consDiags := schemaConstraints(c, &kept, s, pointer)
		diags = append(diags, consDiags...)
		owner = internAlias(c, ts, pointer, hint, ir.TypeRef{Target: inner}, cons, kept)
	}
	return owner, append(diags, preserveUnionSiblings(c, ts, owner, s, pointer, reason, why)...)
}

// preserveUnionSiblings stores the raw oneOf/anyOf of s under the owning node's
// Unmodeled. A validation-only union joins §4.7's keyword family and is reported
// with it; anything else is a §4.8 degradation and says so.
func preserveUnionSiblings(c lowering.Ctx, ts *compile.Types, id ir.TypeID, s *oas3.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	return append(diags, preserveUnionSiblingsAt(c, &td.Common().Unmodeled, s, pointer, reason, why)...)
}

// preserveUnionSiblingsAt is preserveUnionSiblings' body, addressed by the
// Unmodeled map to write into. PreserveRefSiteKeywords calls it directly to
// keep a $ref site's union on a carrier's Unmodeled, where there is no node to
// look up by ID (GitHub #406).
//
// why is the caller's complete sentence, not a clause in a shared frame:
// lowerCoDeclaredUnion's reasons presuppose a structural body at the position,
// and a $ref site has none, so a shared template would contradict itself there.
func preserveUnionSiblingsAt(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	kept, diags := preserveBranchSets(c, p, s, reason, pointer)
	if reason == ir.ReasonValidationOnly || len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"%s; union branches kept verbatim under Unmodeled", why))
}

// preserveBranchSets stores s's declared oneOf/anyOf verbatim under p, in
// keyword order, reporting one it cannot convert. ir.ReasonValidationOnly
// routes each through §4.7's keyword-family reporting (preserveKeyword) and
// returns no keywords. Every other reason goes through plain preserve and
// returns the keywords kept, so a caller with a message of its own knows what
// to name.
//
// preserveUnionSiblingsAt and preserveNullOnlyUnion share this loop: they keep
// the same keywords because the node they attach to has no branch set of its
// own, and differ only in the sentence that explains why.
func preserveBranchSets(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, reason ir.UnmodeledReason, pointer jsontext.Pointer) ([]string, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	var kept []string
	for _, kw := range []string{"oneOf", "anyOf"} {
		raw, err := annotation.RawFromNode(annotation.RawPropertyNode(s, kw))
		if err != nil {
			diags = append(diags, annotation.UnpreservableDiag("openapi:"+kw, c.ProvenanceAt(pointer+ids.Ptr(kw)), err))
			continue
		}
		if reason == ir.ReasonValidationOnly {
			diags = append(diags, preserveKeyword(c, p, "openapi:"+kw, raw,
				pointer, pointer+ids.Ptr(kw), kw)...)
			continue
		}
		preserve(c, p, "openapi:"+kw, raw, reason, pointer+ids.Ptr(kw))
		if len(raw) > 0 {
			kept = append(kept, kw)
		}
	}
	return kept, diags
}

// declaresUnion reports whether s writes oneOf or anyOf at all. It is the one
// predicate every $ref-site union keeper — a component or inline sub-schema
// (refSiteRef), a carrier (PreserveRefSiteKeywords), an allOf branch spelled
// as a $ref (fillAllOf) — gates hoisting a home for the union on, so the
// three cannot drift into checking it three different ways.
func declaresUnion(s *oas3.Schema) bool {
	return len(s.GetOneOf()) > 0 || len(s.GetAnyOf()) > 0
}

// refSiteUnionWhy is preserveUnionSiblingsAt's why at every $ref site: a
// complete sentence in its own right (see preserveUnionSiblingsAt's doc for
// why it cannot share lowerCoDeclaredUnion's framing), because a $ref site's
// own declaration is the reference alone — there is no structural body at
// that position for the union to distribute its composition across.
const refSiteUnionWhy = "oneOf/anyOf beside a $ref conjoin with it rather than composing into a structural body, so there is nothing at this position to distribute the union's composition across"

// falseSchema hoists a boolean `false` schema as a closed empty model (it
// matches nothing), returning the interned ID and the one info diagnostic that
// announces it.
func falseSchema(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		// Kept verbatim beside the approximation, which is what §4.8 asks of
		// every degraded lowering that loses something, and what the composition
		// half of this same rule already did. Without it a `false` schema and a
		// schema that merely wrote `additionalProperties: false` are the same
		// node, so nothing downstream can tell "no instance" from "the empty
		// object" — the diagnostic says which one it was, but a diagnostic is not
		// part of the document.
		//
		// The key names the position rather than a keyword, because a boolean
		// schema writes none. Nothing can collide with it: a schema that is a
		// boolean has no other keywords to preserve.
		preserve(c, &common.Unmodeled, "openapi:schema",
			ir.RawValue("false"), ir.ReasonDegradedLowering, pointer)

		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.FalseSchema, pointer,
			"boolean false schema matches nothing; lowered as a closed empty model "+
				"with the schema kept verbatim under Unmodeled"))
		return &ir.Model{TypeCommon: common, Additional: ir.AdditionalClosed}
	})
	return id, diags
}

// familyOrder is the order lower() tries the keyword families that outrank the
// structural type, and dispatchOf is the sole reader of it. First match wins, so
// a schema declaring more than one of them lowers as the first and passes over
// the rest — which is why the two are derived together rather than separately.
var familyOrder = []string{"const", "enum", "allOf"}

// declaresFamily reports whether s declares the named family. It is the single
// definition of each family's guard: lower() lowers what dispatchOf elects and
// recordSkippedFamilies keeps what the same walk passed over, so the winner and
// the losers can never be read off two tests that disagree.
// A name it does not know declares nothing, rather than falling through to
// another family's guard: a familyOrder entry added without a case here would
// otherwise report whichever guard the default happened to hold, electing that
// name on schemas that never wrote it.
func declaresFamily(s *oas3.Schema, family string) bool {
	switch family {
	case "const":
		return s.GetConst() != nil
	case "enum":
		return enumWritten(s)
	case "allOf":
		return len(s.GetAllOf()) > 0
	default:
		return false
	}
}

// enumWritten reports whether s writes `enum` at all, an empty member list
// included. `enum: []` is legal JSON Schema and fixes the value space to the
// empty set, so it declares one as a populated list does; testing len > 0
// instead would widen the position to whatever its siblings admit, in silence
// (GitHub #278).
//
// Nilness is what the parser keeps: an absent keyword leaves the field nil, an
// empty list leaves it non-nil. A member list the model layer could not parse
// reads as absent, which is a document the loader already refuses.
func enumWritten(s *oas3.Schema) bool {
	return s.GetEnum() != nil
}

// dispatch records how lower() resolved a schema's competing keyword families:
// the one it lowered, and the ones it passed over. won is "" when the schema
// declares none of them and the type set decides the lowering instead.
type dispatch struct {
	won     string
	skipped []string
}

// dispatchOf elects the family lower() lowers and collects the rest. A schema
// declaring none leaves won empty and skipped nil.
//
// familyOrder, declaresFamily and lower()'s switch must name the same families.
// A name declaresFamily does not know is never elected, and a loser reaches
// recordSkippedFamilies whether or not lower() can lower it, but a winner
// lower() has no arm for is neither lowered nor skipped: the switch falls
// through to the type-set arms and drops the keyword in silence (GitHub #35).
// Adding a family means adding all three.
func dispatchOf(s *oas3.Schema) dispatch {
	var d dispatch
	for _, family := range familyOrder {
		if !declaresFamily(s, family) {
			continue
		}
		if d.won == "" {
			d.won = family
			continue
		}
		d.skipped = append(d.skipped, family)
	}
	return d
}

// lower interns the inline schema at pointer and returns its TypeID. const,
// enum and allOf composition take precedence over the structural type, which
// is otherwise dispatched on the effective (null-stripped) type set.
//
// The families are conjoined where a schema writes more than one, so electing
// a winner is a §4.8 degradation: the ones passed over are kept verbatim beside
// it (recordSkippedFamilies). A keyword the elected lowering never reads, such
// as `type: string` beside an allOf, goes through preserveUnhomedKeywords, which
// asks the built node whether it has a field for it.
func lower(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	d := dispatchOf(s)
	unhomed := func(id ir.TypeID, diags []ir.Diagnostic) (ir.TypeID, []ir.Diagnostic) {
		owner, ownDiags := preserveUnhomedKeywords(c, ts, s, pointer, hint, id, d)
		return owner, append(diags, ownDiags...)
	}
	switch d.won {
	case "const":
		return unhomed(hoistLiteral(c, ts, s.GetConst(), pointer, hint))
	case "enum":
		return unhomed(lowerEnum(c, ts, s, pointer, hint))
	case "allOf":
		return unhomed(lowerAllOf(c, ts, anchors, depth, s, pointer, hint))
	}
	// won is "" — the schema declares no family, so the type set decides.
	types := effectiveTypes(s)
	switch {
	case len(types) > 1:
		// A multi-typed schema lowers one variant per declared type, each from
		// this same schema, so an applicator with no home in one variant has one
		// in another: `{type: [string, object], properties: {...}}` puts the
		// property set on the object variant. The homes here are the variants',
		// not the Union's, so the applicator check does not apply.
		return lowerUnion(c, ts, anchors, depth, s, pointer, hint, types)
	case len(types) == 1:
		return unhomed(lowerTyped(c, ts, anchors, depth, s, pointer, hint, types[0]))
	default:
		return unhomed(lowerUntyped(c, ts, anchors, depth, s, pointer, hint))
	}
}

// censusKeywords are the keywords whose IR home depends on what the position
// lowered to: a Model carries properties, a List an element type and the
// collection bounds, a Literal a value, an Enum a member set. keywordHome asks
// the built node which a position kept.
//
// Annotations, validation-only keywords, content vocabulary, use-site keywords
// and value constraints have their own keepers.
//
// The list and keywordHome's switch must name the same set. A keyword listed
// without an arm is reported homeless and preserved everywhere, noisy but
// lossless; one in neither is dropped in silence (GitHub #268, #283).
// oneOf/anyOf/allOf stay out; see refSiteUnhomedKeywords (GitHub #406).
var censusKeywords = []string{
	"additionalProperties", "const", "enum", "format", "items", "maxItems",
	"minItems", "patternProperties", "prefixItems", "properties", "required",
	"type", "uniqueItems",
}

// keywordHome reports whether td, the node this position's own declaration
// lowered to, has the field the named keyword lowers into. It asks the node
// rather than re-deriving lower()'s dispatch, so the two cannot drift apart
// (see recordUnplacedContent).
//
// A nil td is a $ref site, whose declaration becomes an alias over the target.
// The alias carries only the position's constraints and annotations, and the
// target's fields belong to the referent's declaration, so a `format` beside a
// $ref must not read the referent's Encoding as its home. So every keyword is
// homeless at a $ref site except a null-only `type`.
func keywordHome(td ir.TypeDef, s *oas3.Schema, keyword string) bool {
	switch keyword {
	case "properties", "patternProperties", "additionalProperties", "required":
		return isKind(td, ir.KindModel)
	case "items", "prefixItems":
		return isKind(td, ir.KindList) || isKind(td, ir.KindTuple)
	case "maxItems", "minItems", "uniqueItems":
		// Only a List. listConstraints is their sole reader and lowerArray its
		// sole caller, while ir.Tuple has no Constraints field at all — so a
		// collection bound beside prefixItems reaches as little as one written on
		// an object does. Being List-owned is also why valueConstraintKeywords
		// leaves them out, which is what puts them in this census rather than in
		// declaredConstraints.
		return isKind(td, ir.KindList)
	case "const":
		// Any counts as well as Literal. hoistLiteral degrades a value ir.Value
		// cannot represent to the top type and reports it, so the const was read
		// — badly, and already announced — rather than left unread. Claiming it
		// here would report one keyword twice, the second time as an error, since
		// a value no converter can read is one no preserver can read either.
		return isKind(td, ir.KindLiteral) || isKind(td, ir.KindAny)
	case "enum":
		// Any counts here for the reason it counts for const: lowerEnum degrades a
		// member set past the caller's budget to the top type and reports it, so
		// the enum was read and announced rather than left unread. Claiming it
		// again would also undo the budget — the census preserves a homeless
		// keyword verbatim, which would put every member back into the IR as raw
		// bytes and leave only the per-member amplification bounded (GitHub #75).
		return isKind(td, ir.KindEnum) || isKind(td, ir.KindUnion) || isKind(td, ir.KindAny)
	case "format":
		return formatHome(td, s)
	case "type":
		return typeHome(td, s)
	default:
		return false
	}
}

// isKind reports whether td is a live node of kind k. A nil td — the $ref site's
// alias, which is no node of the walk's making — is of no kind at all.
func isKind(td ir.TypeDef, k ir.TypeKind) bool {
	return !ir.IsNilTypeDef(td) && td.Kind() == k
}

// formatHome reports whether the position's `format` reached a field. A Scalar
// hoisted for it carries it in Encoding; a (type, format) pairing formatTable
// knows selects a primitive, which carries the pairing in the primitive kind
// itself. With no type declared there was no pairing to make and nothing read it,
// and a node of any other kind has no Encoding field at all.
func formatHome(td ir.TypeDef, s *oas3.Schema) bool {
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Encoding != nil
	case *ir.Primitive:
		return len(effectiveTypes(s)) > 0
	default:
		return false
	}
}

// typeHome reports whether every shape s's declared type set names is a shape td
// can be. One that td cannot be was read by nothing: the walk built a different
// shape, so `type: string` beside an allOf that composed a Model states something
// the IR no longer holds.
//
// A type set that reduces to nothing — `type: "null"` on its own — is carried by
// TypeRef.Nullable rather than by a node, so it is homed wherever it is written.
func typeHome(td ir.TypeDef, s *oas3.Schema) bool {
	for _, st := range effectiveTypes(s) {
		if !typeShapedBy(td, st) {
			return false
		}
	}
	return true
}

// typeShapedBy reports whether td is a node kind that carries the shape st names.
//
// An Enum carries a scalar type in ValueType, which enumValueType fills from
// exactly that type set. A Literal carries only its value, so a `type` written
// beside a const reached no field at all — including the case where the two
// agree, since the IR then holds the value and nothing about what was declared
// about it. Any, External and the $ref site's nil carry no shape.
func typeShapedBy(td ir.TypeDef, st oas3.SchemaType) bool {
	switch td.(type) {
	case *ir.Model:
		return st == oas3.SchemaTypeObject
	case *ir.List, *ir.Tuple:
		return st == oas3.SchemaTypeArray
	case *ir.Primitive, *ir.Scalar, *ir.Enum:
		return st != oas3.SchemaTypeObject && st != oas3.SchemaTypeArray
	case *ir.Any:
		// The top type admits every shape, so a declared type says nothing it
		// contradicts. A position only reaches Any with a type declared by being
		// degraded there — an unrepresentable const, an enum past its budget — and
		// that degradation is already reported, so restating the type beside it
		// would announce the same collapse twice.
		return true
	default:
		return false
	}
}

// constraintsHome reports whether the value constraints a position declared
// reached td's Constraints field. A position that owns its node hoists no alias
// over it (hoistDeclarationHome), so a constraint that missed the field reaches
// none anywhere.
//
// Having the field is not filling it, so a Scalar or Model is asked whether it
// is non-nil; lowerModel and lowerAllOf fill a Model's the same way (GitHub
// #407). A List is absent because lowerArray fills its Constraints from
// listConstraints, whose collection bounds valueConstraintKeywords excludes, so
// no value constraint reaches it however full the field looks.
func constraintsHome(td ir.TypeDef) bool {
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Constraints != nil
	case *ir.Model:
		return n.Constraints != nil
	default:
		// An Enum, Literal, Union, Tuple or Primitive has no Constraints field at
		// all, so a bound written beside one was read by nothing.
		return false
	}
}

// unhomedKeywords returns the census keywords s declares that td has nowhere to
// carry, in censusKeywords order. It reads the raw nodes for the reason
// declaresValueConstraints does: a keyword the model layer failed to parse is
// still plainly written.
//
// handled names keywords another reader at this position already accounts for:
// the families lower()'s election passed over go to recordSkippedFamilies, which
// knows why they lost, and an allOf branch's `required` is consumed by
// applyCompositionRequired. Recording them here too would report one keyword
// twice.
func unhomedKeywords(s *oas3.Schema, td ir.TypeDef, handled []string) []string {
	out := make([]string, 0, len(censusKeywords))
	for _, keyword := range censusKeywords {
		if annotation.RawPropertyNode(s, keyword) == nil || keywordHome(td, s, keyword) {
			continue
		}
		if slices.Contains(handled, keyword) {
			continue
		}
		out = append(out, keyword)
	}
	return out
}

// refSiteUnhomedKeywords is unhomedKeywords for a $ref site (td is always nil),
// plus allOf. handled passes through so an allOf branch spelled as a $ref still
// routes its own `required` to applyCompositionRequired.
//
// allOf is as homeless here as every census keyword, because none of the
// $ref-site callers goes through dispatchOf, so it never wins an election.
// Adding it to censusKeywords instead would also hit ordinarily composed
// schemas, since keywordHome has no arm saying a Model built from a winning
// allOf carries it.
//
// oneOf/anyOf stay out: callers check declaresUnion and call the union keepers
// directly (GitHub #406).
func refSiteUnhomedKeywords(s *oas3.Schema, handled []string) []string {
	unhomed := unhomedKeywords(s, nil, handled)
	if len(s.GetAllOf()) > 0 {
		unhomed = append(unhomed, "allOf")
	}
	return unhomed
}

// preserveUnhomedKeywords keeps verbatim the census keywords and value
// constraints a position declared that its lowered node cannot carry, and the
// families lower()'s election skipped, returning the position's resulting ID
// (§4.8). That covers a contradictory schema's dropped half and an untyped
// applicator such as `items`.
//
// A lowering that reduced to a shared node gets an alias first, with the
// position's constraints, so a shared node never carries one declaration's
// keywords. A node the position owns gets none, so a bound beside an Enum is
// kept verbatim.
//
// A multipart body still mints encoding keys for `properties` the node lacks,
// so pass.Validate reports ir/encoding-key-unknown-property.
func preserveUnhomedKeywords(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, hint string, id ir.TypeID, d dispatch) (ir.TypeID, []ir.Diagnostic) {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return id, diags
	}
	owns, _ := ts.Lookup(string(pointer))
	unhomed := unhomedKeywords(s, td, d.skipped)
	if owns == id && !constraintsHome(td) {
		// No alias will be hoisted over a node the position already owns, so the
		// bounds it wrote reach no Constraints field anywhere.
		unhomed = append(unhomed, declaredConstraints(s)...)
	}
	if len(unhomed) == 0 && len(d.skipped) == 0 {
		return id, diags
	}
	owner := id
	if owns != id {
		// Once the alias owns the pointer, hoistDeclarationHome attaches no
		// constraints later, so the alias carries the position's.
		var kept ir.Unmodeled
		cons, consDiags := schemaConstraints(c, &kept, s, pointer)
		diags = append(diags, consDiags...)
		owner = internAlias(c, ts, pointer, hint, ir.TypeRef{Target: id}, cons, kept)
	}
	diags = append(diags, recordUnhomedKeywords(c, ts, owner, s, unhomed, nodeShape(td.Kind()), pointer)...)
	return owner, append(diags, recordSkippedFamilies(c, ts, owner, s, d, pointer)...)
}

// recordSkippedFamilies keeps verbatim the keyword families lower()'s election
// passed over, and reports them once.
//
// JSON Schema conjoins keywords, so `{allOf: [{$ref: Base}], enum: [a, b]}` is a
// narrowing of Base, not a malformed document. The IR has no intersection
// combinator (ir-design §15), so only one family can be the value; keeping the
// loser beside it is what §4.8 asks of an unrepresentable conjunction.
//
// It reports the keywords it stored, never the ones it was handed, so the
// message cannot claim one that failed to convert.
func recordSkippedFamilies(c lowering.Ctx, ts *compile.Types, owner ir.TypeID, s *oas3.Schema, d dispatch, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(d.skipped) == 0 {
		return nil
	}
	td, ok, diags := registeredNode(c, ts, owner, pointer)
	if !ok {
		return diags
	}
	common := td.Common()
	kept := make([]string, 0, len(d.skipped))
	for _, keyword := range d.skipped {
		stored, storedDiags := PreserveSchemaKeyword(c, &common.Unmodeled, s, keyword,
			ir.ReasonDegradedLowering, pointer+ids.Ptr(keyword))
		diags = append(diags, storedDiags...)
		if stored {
			kept = append(kept, keyword)
		}
	}
	if len(kept) == 0 {
		return diags
	}
	skipped := strings.Join(kept, " and ")
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"this position declares %s beside its %s, and JSON Schema conjoins them where only "+
			"one can be the value; it lowered as the %s, with %s kept verbatim under Unmodeled",
		skipped, d.won, d.won, skipped))
}

// refSiteShape describes what a $ref position's own declaration lowers to, for
// the diagnostic recordUnhomedAt reports.
const refSiteShape = "an alias over its $ref target"

// nodeShape describes a node of kind k, for that same diagnostic.
func nodeShape(kind ir.TypeKind) string { return fmt.Sprintf("a node of kind %q", kind) }

// recordUnhomedKeywords stores each unhomed keyword on the node owner names.
// shape describes what the position lowered to, so a reader is told which half
// of a contradictory schema the IR describes.
func recordUnhomedKeywords(c lowering.Ctx, ts *compile.Types, owner ir.TypeID, s *oas3.Schema, unhomed []string, shape string, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(unhomed) == 0 {
		return nil
	}
	td, ok, diags := registeredNode(c, ts, owner, pointer)
	if !ok {
		return diags
	}
	return append(diags, recordUnhomedAt(c, &td.Common().Unmodeled, s, unhomed, shape, pointer)...)
}

// recordUnhomedAt keeps each unhomed keyword verbatim in p and reports them once.
//
// It names the keywords it actually stored, never the ones it was handed, so the
// message cannot claim one that failed to convert and was reported unpreservable
// instead (GitHub #144).
func recordUnhomedAt(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, unhomed []string, shape string, pointer jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	kept := make([]string, 0, len(unhomed))
	for _, keyword := range unhomed {
		ok, keptDiags := PreserveSchemaKeyword(c, p, s, keyword,
			ir.ReasonDegradedLowering, pointer+ids.Ptr(keyword))
		diags = append(diags, keptDiags...)
		if ok {
			kept = append(kept, keyword)
		}
	}
	if len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"this position lowered to %s, which has no home for %s declared beside it; kept verbatim under Unmodeled",
		shape, strings.Join(kept, ", ")))
}

// lowerTyped dispatches a single-typed schema to its structural or scalar form.
func lowerTyped(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string, st oas3.SchemaType) (ir.TypeID, []ir.Diagnostic) {
	switch st {
	case oas3.SchemaTypeObject:
		return lowerModel(c, ts, anchors, depth, s, pointer, hint)
	case oas3.SchemaTypeArray:
		return lowerArray(c, ts, anchors, depth, s, pointer, hint)
	default:
		return scalarTypeID(c, ts, anchors, depth, s, st, pointer, hint)
	}
}

// lowerUntyped handles a schema with no declared type: a property set makes it a
// model; enum/const and composition are lowered by later passes; anything else
// is schemaless.
func lowerUntyped(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return lowerModel(c, ts, anchors, depth, s, pointer, hint)
	}
	return ts.PrimID(ir.PrimAny), nil
}

// lowerUnion hoists a multi-typed schema (e.g. type: [string, integer]) as an
// exclusive, untagged union with one variant per declared type.
func lowerUnion(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string, types []oas3.SchemaType) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		variants := make([]ir.Variant, 0, len(types))
		for i, st := range types {
			vptr := pointer + ids.Ptr("type", strconv.Itoa(i))
			vid, vDiags := lowerTyped(c, ts, anchors, depth, s, vptr, hint, st)
			diags = append(diags, vDiags...)
			variants = append(variants, ir.Variant{
				Name: compile.NamingHint(string(st)),
				Type: ir.TypeRef{Target: vid},
			})
		}
		return &ir.Union{
			TypeCommon: common,
			Variants:   variants,
			Exclusive:  true,
		}
	})
	return id, diags
}

// lowerModel hoists an object schema as a Model. This task lowers only the
// property shape (name, type, required) and the property-set cardinality; a
// later pass fills the rest. It reads no annotations: attachDeclaredAnnotations
// does that for every destination.
//
// Cardinality is read here rather than on lowerComponentSchema's internAlias
// fallback because an object-shaped schema owns its node before that fallback
// would run, so the fallback never fires for one (GitHub #129).
func lowerModel(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		cons, consDiags := schemaConstraints(c, &common.Unmodeled, s, pointer)
		diags = append(diags, consDiags...)
		m := &ir.Model{TypeCommon: common, Constraints: cons}
		diags = append(diags, fillModelProperties(c, ts, anchors, depth, m, s, pointer)...)
		diags = append(diags, fillAdditional(c, ts, anchors, depth, m, s, pointer, hint)...)
		d, discDiags := lowerDiscriminator(c, ts, anchors, depth, s, m, nil, pointer)
		diags = append(diags, discDiags...)
		if d != nil {
			m.Discriminator = d
		}
		return m
	})
	return id, diags
}

// fillModelProperties lowers a model's own properties in source order, each with
// its full property-level detail (constraints, visibility, defaults, docs, ...).
func fillModelProperties(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, m *ir.Model, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	props := s.GetProperties()
	if props == nil {
		return nil
	}
	var diags []ir.Diagnostic
	required := requiredSet(s.GetRequired())
	byWire := merge.WireNameIndex(m.Properties)
	for name, js := range props.All() {
		ppointer := pointer + ids.Ptr("properties", name)
		ref, refDiags := CarriedRef(c, ts, anchors, depth, js, ppointer, name)
		diags = append(diags, refDiags...)
		p := ir.Property{
			ID:         ids.Prop(ppointer),
			Name:       compile.NamingFor(name),
			WireName:   name,
			Type:       ref,
			Required:   required[name],
			Provenance: c.ProvenanceAt(ppointer),
		}
		diags = append(diags, FillPropertyDetail(c, ts, anchors, &p, js, ppointer)...)
		var mergeDiags []ir.Diagnostic
		mg := merger(c, ts, &mergeDiags)
		mg.MergeProperty(m, byWire, p, redeclarationSource(js))
		diags = append(diags, mergeDiags...)
	}
	return diags
}

// redeclarationSource renders the schema written at a property position
// verbatim, as the merge keeps it when the position redeclares a field and
// not all of the redeclaration folds onto the first declaration. It is a
// function rather than the bytes because nearly every property is declared
// once, and rendering a node nobody will keep is the cost the merge asks for
// only when it has something to keep.
//
// A boolean schema has no node of its own to render, so its value is spelled
// out; a `$ref` is rendered as the `$ref` the position wrote, not the target.
func redeclarationSource(js *oas3.JSONSchema[oas3.Referenceable]) func() (ir.RawValue, error) {
	return func() (ir.RawValue, error) {
		if b := js.GetBool(); b != nil {
			return ir.RawValue(strconv.FormatBool(*b)), nil
		}
		return annotation.RawFromNode(js.GetSchema().GetRootNode())
	}
}

// FillPropertyDetail enriches a property from its schema: the property-scoped
// facts a type node has no field for (default, visibility, secrecy,
// constraints), then the declaration's annotations. Annotations present at a
// $ref use-site override the target's (ir-design §14).
//
// Constraints stay unconditional: ir.Property is the only home every property
// has, while the node its schema hoists may carry none (a byte or unknown-format
// Scalar, or no node at all). Restating them where the node also carries them
// is the safe half of that trade.
func FillPropertyDetail(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, p *ir.Property, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) []ir.Diagnostic {
	ref := js.GetSchema()
	if ref == nil {
		return nil
	}
	tgt := resolve.TargetSchema(js, ref)
	diags := fillPropertyDefault(c, p, ref, tgt, pointer)
	if ref.GetFormat() == "password" {
		p.Secret = true
	}
	diags = append(diags, fillPropertyVisibility(c, p, ref, tgt, pointer)...)
	diags = append(diags, fillPropertyConstraints(c, p, ref, pointer)...)
	diags = append(diags, fillPropertyAnnotations(c, ts, anchors, p, ref, tgt, pointer)...)
	return append(diags, PreserveRefSiteKeywords(c, ts, &p.Unmodeled, js, p.Type, pointer)...)
}

// fillPropertyVisibility lowers readOnly/writeOnly onto the property and reports
// the pairing that leaves it visible nowhere.
//
// The report is made here rather than inside the reader for the reason
// merge.reconcileProperty reports its own disjoint intersection from outside
// mergeVisibility: provenance is built in one place (lowering.Ctx), and this is
// the caller that knows the position being filled. It is the same finding under
// the same code as the allOf spelling, so a consumer filtering on
// diag.DisjointVisibility sees both.
func fillPropertyVisibility(c lowering.Ctx, p *ir.Property, ref, tgt *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	visibility, disjoint := annotation.EffectiveVisibility(ref, tgt)
	p.Visibility = visibility
	if !disjoint {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DisjointVisibility, pointer,
		"readOnly and writeOnly are both in force for field %q; they admit disjoint lifecycles, "+
			"so the field is visible in none", p.WireName)}
}

// fillPropertyAnnotations records the annotations the property's schema
// declares on the property, but only when that schema lowered to no node of its
// own. A shared primitive must never carry one declaration's annotations, and a
// node keeps them itself (attachDeclaredAnnotations), so the two homes never
// double.
//
// A node another $ref hoisted at the property's pointer is not that node. It
// carries what the schema declares and so does the property: two IR entities
// reflecting one source schema, whichever is reached first (GitHub #116).
//
// A $ref position never hoists a node, so annotations written beside a
// property's $ref land here (GitHub #114).
func fillPropertyAnnotations(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, p *ir.Property, ref, tgt *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if LoweredToOwnNode(ts, pointer, p.Type) {
		return nil
	}
	a, diags := annotation.Read(annotation.Site{Kind: annotation.Reference, Node: ref, Referent: tgt}, pointer, c.ProvenanceAt, c.Is32())

	p.Docs = a.Docs
	if a.Deprecated {
		p.Deprecation = &ir.Deprecation{}
	}
	if a.XML != nil {
		p.XML = a.XML
	}
	if len(a.Examples) > 0 {
		p.Examples = a.Examples
	}
	p.Unmodeled = annotation.MergeUnmodeled(p.Unmodeled, a.Unmodeled)
	diags = append(diags, c.PromoteDeprecation(p.Unmodeled, p.Deprecation, &p.Provenance)...)
	// nil node: this arm runs only when the schema lowered to no node of its own,
	// so nothing here can be carrying an Encoding.
	diags = append(diags, recordUnplacedContent(c, &p.Unmodeled, ref, nil, pointer)...)
	diags = append(diags, recordUnexpandedDynamicRef(c, anchors, &p.Unmodeled, ref, pointer)...)
	return append(diags, PreserveUnknownKeywords(c, &p.Unmodeled, ref, pointer)...)
}

// LoweredToOwnNode reports whether the declaration at pointer lowered to a type
// node of its own — the node attachDeclaredAnnotations then fills, leaving its
// carrier nothing to hold.
//
// It asks whether the node interned at pointer is the one the declaration
// lowered to, not merely whether a node is interned there. A $ref naming an
// inline position hoists that position's home for its own use, in either
// declaration order, and a carrier reading the registry alone would keep its
// schema's annotations only when it happened to lower first.
func LoweredToOwnNode(ts *compile.Types, pointer jsontext.Pointer, t ir.TypeRef) bool {
	id, owned := ts.Lookup(string(pointer))
	return owned && id == t.Target
}

// fillPropertyDefault sets the property default, preferring the use-site node
// over the $ref target's; an unconvertible node yields a diagnostic.
func fillPropertyDefault(c lowering.Ctx, p *ir.Property, ref, tgt *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	node := ref.GetDefault()
	if node == nil && tgt != nil {
		node = tgt.GetDefault()
	}
	if node == nil {
		return nil
	}
	v, err := value.FromNode(node)
	if err != nil {
		return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
			"default: %s", err.Error())}
	}
	p.Default = &v
	return nil
}

// fillPropertyConstraints attaches the property's scalar constraints, and the
// co-declared bound keyword that reached none of them, to the property itself.
// ir.Property is the carrier at this position: a property's schema is read
// through CarriedRef, so it hoists no node of its own to hold either.
//
// It reads ref alone and never the $ref target, which is why no tgt reaches it:
// bounds conjoin rather than override, so a referent's bound merged here under
// use-site precedence would publish the wider of the two as the whole truth. It
// stays on the node the reference points at instead (ir-design §12.2).
func fillPropertyConstraints(c lowering.Ctx, p *ir.Property, ref *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	cons, diags := schemaConstraints(c, &p.Unmodeled, ref, pointer)
	if cons != nil {
		p.Constraints = cons
	}
	return diags
}

// attachDeclaredAnnotations records every annotation s declares on the type
// node pointer owns, the one structural home they have (ir.TypeCommon).
//
// It is the sole reader of declaration-scoped annotations and runs above
// lower()'s dispatch: every declaration reaches it through schemaBody or an
// alias fallback, so a new lowering destination cannot forget to read them
// (GitHub #114).
//
// A schema whose body reduced to a shared primitive owns no node; its
// annotations stay with the declaring property (FillPropertyDetail). Ownership
// is checked before conversion because callers cover a pointer in either order,
// and only the one that finds a node may emit conversion diagnostics.
func attachDeclaredAnnotations(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	// No node here means a position that reduced to a shared primitive, or one
	// still being built further up this walk, whose builder attaches the same
	// declaration once its build returns (GitHub #749).
	td, ok := ts.NodeAt(string(pointer))
	if !ok {
		return nil
	}
	a, diags := annotation.Read(annotation.Site{Kind: annotation.Declaration, Node: s}, pointer, c.ProvenanceAt, c.Is32())

	common := td.Common()
	common.Docs = a.Docs
	if a.Deprecated {
		common.Deprecation = &ir.Deprecation{}
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
	diags = append(diags, recordUnexpandedDynamicRef(c, anchors, &common.Unmodeled, s, pointer)...)
	return append(diags, PreserveUnknownKeywords(c, &common.Unmodeled, s, pointer)...)
}

// fillAdditional lowers additionalProperties, patternProperties, and
// unevaluatedProperties into the model's openness and catch-all shape.
func fillAdditional(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, m *ir.Model, s *oas3.Schema, pointer jsontext.Pointer, hint string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ap := s.GetAdditionalProperties()
	switch {
	case annotation.IsFalseSchema(ap):
		m.Additional = ir.AdditionalClosed
	case ap != nil && !ap.IsBool():
		ref, refDiags := Ref(c, ts, anchors, depth, ap, pointer+ids.Ptr("additionalProperties"), compile.SubHint(hint, "value"))
		diags = append(diags, refDiags...)
		m.AdditionalProps = &ir.AdditionalProps{Value: ref}
	}
	patterns, patternDiags := patternProps(c, ts, anchors, depth, s, pointer, hint)
	diags = append(diags, patternDiags...)
	if len(patterns) > 0 {
		if m.AdditionalProps == nil {
			m.AdditionalProps = &ir.AdditionalProps{Value: ts.PrimRef(ir.PrimAny)}
		}
		m.AdditionalProps.Patterns = patterns
	}
	if annotation.IsFalseSchema(s.GetUnevaluatedProperties()) {
		m.Additional = ir.AdditionalClosedAfterComposition
	}
	return diags
}

// patternProps lowers patternProperties into pattern/value bindings in source
// order.
func patternProps(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) ([]ir.PatternProps, []ir.Diagnostic) {
	pp := s.GetPatternProperties()
	if pp == nil || pp.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make([]ir.PatternProps, 0, pp.Len())
	for pattern, js := range pp.All() {
		ref, refDiags := Ref(c, ts, anchors, depth, js, pointer+ids.Ptr("patternProperties", pattern), compile.SubHint(hint, "pattern"))
		diags = append(diags, refDiags...)
		out = append(out, ir.PatternProps{Pattern: pattern, Value: ref})
	}
	return out, diags
}

// buildTuple lowers prefixItems into a Tuple. A trailing `items` schema makes
// the source an *open* tuple — a fixed positional head plus a homogeneous tail
// — and the IR has no combinator for that: Tuple is fixed-arity, List is
// homogeneous, and there is no node that is both. The head lowers to a Tuple,
// which is the documented weaker shape, and the tail is kept beside it so the
// arity the Tuple now asserts falsely stays recoverable (ir-design §4.8).
func buildTuple(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer, hint string, prefix []*oas3.JSONSchema[oas3.Referenceable]) (ir.TypeDef, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	elems := make([]ir.TypeRef, 0, len(prefix))
	for i, ps := range prefix {
		ref, refDiags := Ref(c, ts, anchors, depth, ps, pointer+ids.Ptr("prefixItems", strconv.Itoa(i)), compile.SubHint(hint, strconv.Itoa(i)))
		diags = append(diags, refDiags...)
		elems = append(elems, ref)
	}
	t := &ir.Tuple{TypeCommon: common, Elems: elems}
	if s.GetItems() == nil {
		return t, diags
	}
	kept, keptDiags := PreserveNode(c, &t.Unmodeled, "openapi:items-after-prefix", annotation.RawPropertyNode(s, "items"), ir.ReasonDegradedLowering, pointer+ids.Ptr("items"))
	diags = append(diags, keptDiags...)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
			"items after prefixItems is an open tuple; lowered as a fixed-arity Tuple with the tail kept under Unmodeled"))
	}
	return t, diags
}

// scalarTypeID maps a scalar (type, format) pair to a TypeID via formatTable: a
// known pairing interns the shared primitive; byte, an unknown format, and the
// 2020-12 content vocabulary each hoist a named Scalar wrapping the base
// primitive with an Encoding, so what the position wrote never leaks onto the
// shared primitive every other declaration of that type also resolves to.
func scalarTypeID(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, st oas3.SchemaType, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	format := s.GetFormat()
	if st == oas3.SchemaTypeString && format == "byte" {
		return hoistByteScalar(c, ts, anchors, depth, s, pointer, hint)
	}
	key := string(st)
	if format != "" {
		key += "/" + format
	}
	prim, known := formatTable[key]
	if !known {
		return hoistFormatScalar(c, ts, anchors, depth, s, baseForType(st), format, pointer, hint)
	}
	if !declaresContentVocabulary(s) {
		return ts.PrimID(prim), nil
	}
	return hoistContentScalar(c, ts, anchors, depth, s, prim, pointer, hint)
}

// hoistByteScalar hoists a base64-encoded byte scalar (string+byte).
//
// Every hoister here carries the position's value constraints itself, because
// owning a node is what stops hoistDeclarationHome hoisting the alias that would
// otherwise carry them: that fallback resolves to whatever node the pointer
// already owns and returns early. A scalar that hoisted because it wrote a
// format must not lose the bounds it wrote beside it (invariant 2).
func hoistByteScalar(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := ts.PrimRef(ir.PrimBytes)
		wire := ts.PrimRef(ir.PrimString)
		enc, encDiags := scalarEncoding(c, ts, anchors, depth, s, "base64", &common, pointer, hint)
		diags = append(diags, encDiags...)
		enc.WireType = &wire
		cons, consDiags := schemaConstraints(c, &common.Unmodeled, s, pointer)
		diags = append(diags, consDiags...)
		return &ir.Scalar{
			TypeCommon:  common,
			Base:        &base,
			Encoding:    enc,
			Constraints: cons,
		}
	})
	return id, diags
}

// hoistFormatScalar hoists a scalar over base carrying an unknown format as its
// encoding name, preserving the format losslessly.
func hoistFormatScalar(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, base ir.PrimKind, format string, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		baseRef := ts.PrimRef(base)
		enc, encDiags := scalarEncoding(c, ts, anchors, depth, s, format, &common, pointer, hint)
		diags = append(diags, encDiags...)
		cons, consDiags := schemaConstraints(c, &common.Unmodeled, s, pointer)
		diags = append(diags, consDiags...)
		return &ir.Scalar{
			TypeCommon:  common,
			Base:        &baseRef,
			Encoding:    enc,
			Constraints: cons,
		}
	})
	return id, diags
}

// hoistContentScalar hoists a scalar over the shared primitive a known
// (type, format) pair maps to, giving the content vocabulary written here a node
// of its own to sit on. It carries the position's value constraints for the
// reason hoistByteScalar records.
func hoistContentScalar(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, prim ir.PrimKind, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := ts.PrimRef(prim)
		enc, encDiags := scalarEncoding(c, ts, anchors, depth, s, "", &common, pointer, hint)
		diags = append(diags, encDiags...)
		cons, consDiags := schemaConstraints(c, &common.Unmodeled, s, pointer)
		diags = append(diags, consDiags...)
		return &ir.Scalar{
			TypeCommon:  common,
			Base:        &base,
			Encoding:    enc,
			Constraints: cons,
		}
	})
	return id, diags
}

// scalarEncoding builds the Encoding a scalar position declares: the whole
// 2020-12 content vocabulary over the OpenAPI `format` spelling of the encoding
// name. formatName is the encoding name the format contributes — "base64" for
// format: byte, an unrecognized format verbatim, "" when the pairing is already
// captured by the primitive kind.
func scalarEncoding(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema,
	formatName string, common *ir.TypeCommon, pointer jsontext.Pointer, hint string,
) (*ir.Encoding, []ir.Diagnostic) {
	name, diags := encodingName(c, s, formatName, common, pointer)
	content, contentDiags := contentSchemaRef(c, ts, anchors, depth, s, pointer, hint)
	enc := &ir.Encoding{Name: name, MediaType: s.GetContentMediaType(), Schema: content}
	return enc, append(diags, contentDiags...)
}

// encodingName elects the one name ir.Encoding holds from the two keywords that
// can name an encoding at a scalar position.
//
// contentEncoding wins: it is the standard keyword, where a format the IR could
// not place is only parked there. Encoding holds one name, so a format that
// named a *different* encoding is kept verbatim on the node rather than
// overwritten away.
func encodingName(c lowering.Ctx, s *oas3.Schema, formatName string, common *ir.TypeCommon, pointer jsontext.Pointer) (string, []ir.Diagnostic) {
	content := s.GetContentEncoding()
	if content == "" || content == formatName {
		return formatName, nil
	}
	if formatName == "" {
		return content, nil
	}
	at := pointer + ids.Ptr("format")
	kept, diags := PreserveSchemaKeyword(c, &common.Unmodeled, s, "format", ir.ReasonNoIRHome, at)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"format and contentEncoding both name an encoding and ir.Encoding holds one; "+
				"contentEncoding %q is lowered and format is kept verbatim under Unmodeled", content))
	}
	return content, diags
}

// contentSchemaRef lowers contentSchema — the shape the encoded value has once
// decoded — to Encoding.Schema. Its value is a schema, so it lowers like every
// other sub-schema position (fillAdditional, patternProps): hoisted at its own
// pointer and referenced by ID, never carried beside the encoding as a raw blob
// a consumer would have to re-parse.
//
// The pointer it hoists at is the one the source wrote it at, which only this
// declaration can name, so the node needs no namespace of its own (§4.3).
func contentSchemaRef(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (*ir.TypeRef, []ir.Diagnostic) {
	cs := s.GetContentSchema()
	if cs == nil {
		return nil, nil
	}
	ref, diags := Ref(c, ts, anchors, depth, cs, pointer+ids.Ptr("contentSchema"), compile.SubHint(hint, "content"))
	return &ref, diags
}

// contentKeywords are the 2020-12 content-vocabulary keywords, all three of
// which lower into ir.Encoding: contentEncoding names Encoding.Name,
// contentMediaType names Encoding.MediaType, and contentSchema names
// Encoding.Schema (ir/constraints.go, ir-design §5.3). One list because they
// share one home — a position that reached no Encoding keeps all three, and one
// that reached an Encoding keeps none.
var contentKeywords = []string{"contentEncoding", "contentMediaType", "contentSchema"}

// declaresContentVocabulary reports whether s writes any content-vocabulary
// keyword, so a position that wrote one owns a node to keep it on.
func declaresContentVocabulary(s *oas3.Schema) bool {
	return s.GetContentEncoding() != "" || s.GetContentMediaType() != "" ||
		s.GetContentSchema() != nil
}

// recordUnplacedContent keeps each content keyword verbatim on p, for a position
// whose lowering produced no ir.Encoding to hold it.
//
// A string position lowers them (scalarEncoding). An object, an array, a union,
// an alias over a $ref, or a schema with no declared type at all has no Encoding
// field — yet 2020-12 §8.3 gives these keywords meaning on whatever instance
// turns out to be a string, so they are kept rather than dropped. It asks the
// node the position actually lowered to instead of re-deriving lower()'s
// dispatch, so the two cannot drift apart.
func recordUnplacedContent(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, td ir.TypeDef, pointer jsontext.Pointer) []ir.Diagnostic {
	if !declaresContentVocabulary(s) || scalarHasEncoding(td) {
		return nil
	}
	var diags []ir.Diagnostic
	for _, keyword := range contentKeywords {
		kept, keptDiags := PreserveSchemaKeyword(c, p, s, keyword, ir.ReasonNoIRHome, pointer+ids.Ptr(keyword))
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(keyword),
			"%s is content-vocabulary data the IR holds in ir.Encoding, and this position "+
				"lowered to a shape with no Encoding field; kept verbatim under Unmodeled", keyword))
	}
	return diags
}

// scalarHasEncoding reports whether td is a Scalar already carrying the
// position's Encoding. A nil td is the property-carrier case — no node, so no
// Encoding.
func scalarHasEncoding(td ir.TypeDef) bool {
	sc, ok := td.(*ir.Scalar)
	return ok && sc.Encoding != nil
}

// maxDynamicAnchorDepth bounds the raw-source walk that indexes $dynamicAnchor
// declarations (styleguide bounded-recursion rule). Document trees nest far
// shallower — a schema under a media type under a response is about ten levels —
// so the cap only ever fires on a pathological structure.
const maxDynamicAnchorDepth = 512

// maxDynamicAnchorNodes bounds how many nodes that walk visits in total. Depth
// stopped bounding it once the walk began following YAML aliases: an alias graph
// is a DAG, so a shallow document can present far more paths than it has nodes.
// Both bounds report the same incomplete-index warning when they fire.
const maxDynamicAnchorNodes = 1 << 20

// dynamicExpansion resolves the $dynamicRef s writes at pointer to the type it
// names, or reports why it is irreducible. Expansion and preservation both
// decide through it.
//
// Dynamic scope is static per document (ir-design §4.7), so a $dynamicAnchor
// declared once is the only match, provided the document is one schema
// resource: an $id on the path to either end makes the reference irreducible
// (dynamicChainVerdict).
//
// Only an anchor on a top-level component schema resolves, since only its
// TypeID is stable whatever lowers first (ir-design §4.3). A $dynamicRef
// co-declared with a $ref, oneOf/anyOf or a shape is irreducible too, as is
// one another document holds.
func dynamicExpansion(c lowering.Ctx, anchors *AnchorIndex, s *oas3.Schema, pointer jsontext.Pointer) (target ir.TypeID, why string, ok bool, diags []ir.Diagnostic) {
	name, why, ok := dynamicRefName(s)
	if !ok {
		return "", why, false, nil
	}
	if c.RefScope().Foreign {
		// Its fragment names that document's anchor, and the index holds the
		// source's alone: expanding it read the source's anchor of that name
		// (GitHub #762).
		return "", "it is written in another document, whose $dynamicAnchor it names and which is not lowered", false, nil
	}
	at, why, ok, diags := soleAnchorSite(c, anchors, name)
	if !ok {
		return "", why, false, diags
	}
	id, resolved, handled := c.RefScope().ComponentRef(at)
	if !handled || !resolved {
		return "", unnamedAnchorSiteWhy(name, at), false, diags
	}
	chainWhy, chainOK, chainDiags := dynamicChainVerdict(c, anchors, at, pointer)
	diags = append(diags, chainDiags...)
	if !chainOK {
		return "", chainWhy, false, diags
	}
	return id, "", true, diags
}

// unnamedAnchorSiteWhy words why the position declaring an anchor is not a
// target the IR can name. Two shapes reach it: a pointer deeper than a
// top-level component schema, which has no stable TypeID, and a component
// schema keyed "", which earns none because an empty name is not a name. The
// second must not be worded "rather than on a component schema", which the
// document contradicts. The verdict is the same; only the reason differs.
func unnamedAnchorSiteWhy(name string, at jsontext.Pointer) string {
	if ids.ComponentSchemaNamedEmpty(at) {
		return fmt.Sprintf(`$dynamicAnchor %q is declared on the component schema keyed "" at %q, `+
			"and an empty name earns no named type to expand to", name, at)
	}
	return fmt.Sprintf("$dynamicAnchor %q is declared at %q rather than on a component schema", name, at)
}

// dynamicRefName returns the $dynamicAnchor name the $dynamicRef s writes
// addresses, or the reason it addresses none. It stops short of the index, so
// the chain walk and the lowering path ask one function what a schema requests.
func dynamicRefName(s *oas3.Schema) (name, why string, ok bool) {
	node := annotation.RawPropertyNode(s, "$dynamicRef")
	if node == nil {
		return "", "no $dynamicRef is written here", false
	}
	if dynamicRefSiblings(s) {
		return "", "it is co-declared with another applicator, which the IR cannot intersect with it", false
	}
	if node.Kind != yaml.ScalarNode {
		return "", "its value is not a reference string", false
	}
	return dynamicFragment(node.Value)
}

// dynamicRefSiblings reports whether s writes anything beside its $dynamicRef
// that the expansion would have to intersect with. JSON Schema conjoins
// keywords and the IR has no node that intersects a reference with a shape, so
// any of these makes the reference irreducible; expanding regardless would
// assert the target's shape and drop the sibling in silence.
//
// It is broader than declaresShape by narrowsInstance's keywords, for the reason
// that predicate records: an expansion takes the target whole, so a keyword it
// cannot carry is lost whether or not it declares a shape by itself.
func dynamicRefSiblings(s *oas3.Schema) bool {
	if s.Ref != nil || len(s.GetOneOf()) > 0 || len(s.GetAnyOf()) > 0 {
		return true
	}
	return narrowsInstance(s) || declaresShape(s)
}

// soleAnchorSite returns the one pointer declaring the named $dynamicAnchor, or
// the reason no single declaration answers for it. Two declarations put the
// target back under the evaluation path's control, which no static lowering can
// resolve (ir-design §4.7).
//
// The index is a parameter because the memo is state the caller owns. Its
// diagnostics come back on every path, including the two answering "no single
// declaration": the walk bound they report is a fact about the document, not
// about this lookup's verdict, so a failed lookup must not swallow it.
func soleAnchorSite(c lowering.Ctx, anchors *AnchorIndex, name string) (at jsontext.Pointer, why string, ok bool, diags []ir.Diagnostic) {
	sites, diags := anchors.sites(c, name)
	if len(sites) == 0 {
		return "", fmt.Sprintf("no $dynamicAnchor %q is declared in this document", name), false, diags
	}
	if len(sites) > 1 {
		return "", fmt.Sprintf("$dynamicAnchor %q is declared %d times, so the target depends on the evaluation path",
			name, len(sites)), false, diags
	}
	return sites[0], "", true, diags
}

// dynamicChainVerdict reports whether from may take the expansion to the anchor
// declared at at, following the target's own expansions in turn. It refuses in
// two cases. A chain returning to from would make the position's own type its
// base, a loop no emitter can resolve; every member of such a cycle reaches this
// verdict independently, so the whole cycle is preserved. An $id on the path to
// from or to any link puts the ends in different schema resources.
//
// The loop is bounded: cur only takes anchor-index values, and each turn
// returns or adds one to seen.
func dynamicChainVerdict(c lowering.Ctx, anchors *AnchorIndex, at, from jsontext.Pointer) (why string, ok bool, diags []ir.Diagnostic) {
	if declaresResourceIDAbove(c, from) {
		return resourceBoundaryWhy(from), false, nil
	}
	seen := map[jsontext.Pointer]bool{}
	for cur := at; !seen[cur]; {
		if cur == from {
			return fmt.Sprintf("expanding it closes a cycle of $dynamicRef expansions back onto %q, "+
				"leaving a type whose own base chain never terminates", from), false, diags
		}
		if declaresResourceIDAbove(c, cur) {
			return resourceBoundaryWhy(cur), false, diags
		}
		seen[cur] = true
		next, hops, hopDiags := dynamicHop(c, anchors, cur)
		diags = append(diags, hopDiags...)
		if !hops {
			break
		}
		cur = next
	}
	return "", true, diags
}

// resourceBoundaryWhy words the one irreducible case that is about resources
// rather than shapes, for either end of a chain.
func resourceBoundaryWhy(at jsontext.Pointer) string {
	return fmt.Sprintf("an $id at or above %q starts a schema resource of its own, "+
		"and the IR resolves no resource base URIs", at)
}

// dynamicHop returns the anchor declaration the schema at a component pointer
// would itself expand to, when it writes an expandable $dynamicRef of its own.
// Anything else ends the chain: a position that lowers to a shape rather than to
// another reference cannot extend a cycle of references.
//
// The diagnostics are the index's, so the paths that return before consulting
// it carry none. Once it is consulted they come back even if the chain ends
// there: whether the hop happened and whether the index was complete are
// independent answers.
func dynamicHop(c lowering.Ctx, anchors *AnchorIndex, at jsontext.Pointer) (jsontext.Pointer, bool, []ir.Diagnostic) {
	s := componentSchemaAt(c, at)
	if s == nil {
		return "", false, nil
	}
	name, _, ok := dynamicRefName(s)
	if !ok {
		return "", false, nil
	}
	next, nextDiags := anchors.sites(c, name)
	if len(next) != 1 {
		return "", false, nextDiags
	}
	return next[0], true, nextDiags
}

// componentSchemaAt returns the schema body of the top-level component pointer
// addresses, and nil for any other pointer. Every accessor on the way is
// nil-safe and annotation.At reads a missing entry as "no body written", so the
// one guard is what distinguishes a component pointer from a deeper one.
func componentSchemaAt(c lowering.Ctx, pointer jsontext.Pointer) *oas3.Schema {
	name, ok := ids.ComponentSchemaName(pointer)
	if !ok {
		return nil
	}
	js, _ := c.Doc.GetComponents().GetSchemas().Get(name)
	return annotation.At(js).Node
}

// declaresResourceIDAbove reports whether any mapping on the path from the root
// down to pointer writes $id, which §8.2.1 makes a schema resource's root.
//
// It reads every step, so a property literally named "$id" reads as a boundary
// that is not there. That errs safely: a false boundary costs an expansion that
// would have been safe, where a missed one mints an inexpressible reference.
//
// The view is built per call; sharing one is a cost question (GitHub #338).
// Known gap: a path node whose merge chain exceeds MergeDepthLimit expands to
// nothing, so an $id there is missed, the unsafe direction (GitHub #401).
func declaresResourceIDAbove(c lowering.Ctx, pointer jsontext.Pointer) bool {
	view := nodeview.New()
	root := nodeview.DocumentRoot(nodeview.Deref(c.Doc.GetRootNode()))
	// An incomplete walk needs no arm: path holds the nodes reached, and an $id
	// above a pointer that falls off the tree still binds.
	path, _ := view.DocumentPath(root, pointer)
	for _, n := range path {
		if view.ChildByToken(n, "$id") != nil {
			return true
		}
	}
	return false
}

// dynamicFragment returns the anchor name a $dynamicRef addresses, or the
// reason it addresses none. Only the same-document `#name` spelling resolves; a
// URI part or a `#/…` pointer addresses no anchor.
//
// The name is the fragment percent-decoded once (RFC 3986): `#my%2Danchor` and
// `#my-anchor` name one anchor, and `#my%252Danchor` names `my%2Danchor`
// (GitHub #233). PathUnescape keeps `+` literal, as a fragment needs, and a
// fragment it cannot decode is refused, not matched raw.
//
// The $dynamicAnchor side is not decoded: JSON Schema §8.2.2 anchors admit no
// `%`, so decoding could only merge distinct names such as `a-b` and `a%2Db`,
// and the declaration count per name decides whether a reference expands.
func dynamicFragment(ref string) (name, why string, ok bool) {
	fragment, found := strings.CutPrefix(ref, "#")
	if !found || fragment == "" || strings.Contains(fragment, "/") {
		return "", fmt.Sprintf("%q is not a plain same-document fragment", ref), false
	}
	name, err := url.PathUnescape(fragment)
	if err != nil {
		return "", fmt.Sprintf("%q is not valid percent-encoded text: %s", ref, err.Error()), false
	}
	return name, "", true
}

// recordUnexpandedDynamicRef keeps a $dynamicRef verbatim on p when it was not
// expanded, so the reference survives the position lowering without it
// (ir-design §4.7's irreducible half). An expanded one is already the position's
// type and must not also be preserved — that would tell a consumer the compiler
// ignored it.
func recordUnexpandedDynamicRef(c lowering.Ctx, anchors *AnchorIndex, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	_, why, expanded, diags := dynamicExpansion(c, anchors, s, pointer)
	if expanded {
		return diags
	}
	at := pointer + ids.Ptr("$dynamicRef")
	kept, keptDiags := PreserveSchemaKeyword(c, p, s, "$dynamicRef", ir.ReasonDegradedLowering, at)
	diags = append(diags, keptDiags...)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"$dynamicRef was not expanded because %s; it is kept verbatim under Unmodeled", why))
	}
	return diags
}

// declaresDynamicRef reports whether s writes a $dynamicRef, so a position that
// wrote one owns a node to keep an unexpanded reference on.
func declaresDynamicRef(s *oas3.Schema) bool {
	return annotation.RawPropertyNode(s, "$dynamicRef") != nil
}

// AnchorIndex memoizes the document's $dynamicAnchor index.
//
// It is the one thing this compiler shares and mutates besides the interning
// table, an exception micro-compiler-design §4 did not expect. It is a memo, not
// an accumulator: the value it caches is a pure function of the document. It
// stays out of the immutable context (§4.1) because building it emits a
// diagnostic, which would report on documents that never write $dynamicRef, and
// the walk is a poor trade for a keyword almost no document uses.
type AnchorIndex struct {
	byName map[string][]jsontext.Pointer
}

// sites returns the pointers declaring the named $dynamicAnchor, building the
// index on first use and announcing once when a bound stopped the walk short.
//
// A truncated index can only undercount, and every verdict resting on it reads
// an undercount as "declared exactly once" — the one count that expands. So the
// warning is what tells a reader that an expansion reported below was decided
// against an index nothing verified, which is exactly what diag.CycleScanFailed
// says for the pre-parse scan.
func (a *AnchorIndex) sites(c lowering.Ctx, name string) ([]jsontext.Pointer, []ir.Diagnostic) {
	if a.byName != nil {
		return a.byName[name], nil
	}
	index, complete := dynamicAnchors(c.Doc.GetRootNode())
	a.byName = index
	if complete {
		return a.byName[name], nil
	}
	return a.byName[name], []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "",
		"the $dynamicAnchor index stopped at its walk bounds (%d levels, %d nodes); "+
			"a $dynamicRef expanded below is not verified to name the document's only anchor of its name",
		maxDynamicAnchorDepth, maxDynamicAnchorNodes)}
}

// dynamicAnchors indexes every $dynamicAnchor in the raw source by name, mapping
// it to the JSON pointers that declare it, in document order, and reports
// whether the walk ran to completion. It reads the raw tree because oas3.Schema
// has no field for the keyword at v1.24.0 — the reason nothing expanded a
// $dynamicRef before.
func dynamicAnchors(root *yaml.Node) (map[string][]jsontext.Pointer, bool) {
	w := newAnchorWalk(maxDynamicAnchorNodes)
	w.walk(root, "", 0)
	return w.out, !w.truncated && !w.view.Exhausted()
}

// anchorWalk is the state of one $dynamicAnchor index build: the source view,
// the index under construction, and what is left of the visit budget.
//
// It reads mappings through a nodeview.View, so a `<<` merge key and a YAML alias
// contribute the anchors they carry to every position that pulls them in —
// which is what the parser downstream sees, and what makes "declared exactly
// once" a count of what the document declares rather than of what it spells
// out. The count decides whether a $dynamicRef expands, so an anchor reached
// only through an alias must not be invisible to it.
type anchorWalk struct {
	view      *nodeview.View
	out       map[string][]jsontext.Pointer
	budget    int
	truncated bool
}

// newAnchorWalk returns a walk that will visit at most budget nodes.
func newAnchorWalk(budget int) *anchorWalk {
	return &anchorWalk{view: nodeview.New(), out: map[string][]jsontext.Pointer{}, budget: budget}
}

// walk indexes the anchors n declares, under the pointer of the mapping
// declaring each.
func (w *anchorWalk) walk(n *yaml.Node, pointer jsontext.Pointer, depth int) {
	n = nodeview.Deref(n)
	if n == nil || !w.charge(depth) {
		return
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			w.walk(c, pointer, depth+1)
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			w.walk(c, pointer+ids.Ptr(strconv.Itoa(i)), depth+1)
		}
	case yaml.MappingNode:
		w.walkMapping(n, pointer, depth)
	default:
		// A scalar declares no anchor and has no children; an alias survived
		// nodeview.Deref only by dangling, so it has nothing to stand in for.
	}
}

// walkMapping reads one mapping's effective pairs: a $dynamicAnchor names this
// mapping, and every other value is walked in turn.
func (w *anchorWalk) walkMapping(n *yaml.Node, pointer jsontext.Pointer, depth int) {
	for _, p := range w.view.MappingPairs(n) {
		if p.Key == "$dynamicAnchor" {
			w.record(p.Val, pointer)
			continue
		}
		w.walk(p.Val, pointer+ids.Ptr(p.Key), depth+1)
	}
}

// record indexes the anchor val names at pointer. A value that is not a
// non-empty scalar names nothing a $dynamicRef could spell.
func (w *anchorWalk) record(val *yaml.Node, pointer jsontext.Pointer) {
	if val == nil || val.Kind != yaml.ScalarNode || val.Value == "" {
		return
	}
	w.out[val.Value] = append(w.out[val.Value], pointer)
}

// charge reports whether the walk may read one more node, recording the refusal
// when it may not so the caller of dynamicAnchors learns the index is partial.
func (w *anchorWalk) charge(depth int) bool {
	if depth > maxDynamicAnchorDepth || w.budget == 0 {
		w.truncated = true
		return false
	}
	w.budget--
	return true
}

// formatTable maps a scalar "type" or "type/format" key to its IR primitive.
// Keys absent here (byte, and any unknown format) hoist a Scalar instead.
var formatTable = map[string]ir.PrimKind{
	"string":           ir.PrimString,
	"string/date":      ir.PrimDate,
	"string/time":      ir.PrimTime,
	"string/duration":  ir.PrimDuration,
	"string/uuid":      ir.PrimUUID,
	"string/uri":       ir.PrimURL,
	"string/date-time": ir.PrimDatetimeOffset,
	"string/binary":    ir.PrimBytes,
	"string/password":  ir.PrimString,
	"integer":          ir.PrimInteger,
	"integer/int32":    ir.PrimInt32,
	"integer/int64":    ir.PrimInt64,
	"number":           ir.PrimNumber,
	"number/float":     ir.PrimFloat32,
	"number/double":    ir.PrimFloat64,
	"number/decimal":   ir.PrimDecimal,
	"boolean":          ir.PrimBool,
}

// baseForType returns the base primitive for an unknown-format scalar of type st.
func baseForType(st oas3.SchemaType) ir.PrimKind {
	switch st {
	case oas3.SchemaTypeInteger:
		return ir.PrimInteger
	case oas3.SchemaTypeNumber:
		return ir.PrimNumber
	case oas3.SchemaTypeBoolean:
		return ir.PrimBool
	default:
		return ir.PrimString
	}
}

// effectiveTypes returns a schema's declared types with the JSON Schema "null"
// member removed; nullability is normalized onto the TypeRef, not the type set.
func effectiveTypes(s *oas3.Schema) []oas3.SchemaType {
	types := s.GetType()
	out := make([]oas3.SchemaType, 0, len(types))
	for _, t := range types {
		if t == oas3.SchemaTypeNull {
			continue
		}
		out = append(out, t)
	}
	return out
}

// nullVerdict is what one keyword family says about the null value. JSON Schema
// conjoins keywords, so the families are read together rather than first-match:
// a schema admits null when one family puts it in the value space and no other
// takes it out.
type nullVerdict int

const (
	// nullSilent is a family the schema does not declare, or one that constrains
	// only non-null instances. It forbids nothing.
	nullSilent nullVerdict = iota
	// nullAdmitted is a family that puts null in the value space.
	nullAdmitted
	// nullForbidden is a family that takes null out of it.
	nullForbidden
)

// maxNullConjuncts bounds the conjunct walk below (styleguide bounded-recursion
// rule). One budget unit is spent per schema visited, so it caps the walk's
// depth as well as its breadth — an `allOf` naming its own schema, or a diamond
// of conjunctions, terminates on it rather than on the shape of the source. A
// conjunction deeper or wider than this reads as silent, which is the answer
// that claims the least.
const maxNullConjuncts = 256

// schemaAdmitsNull reports whether a schema admits the null value. Lowering
// lifts every spelling of that onto the enclosing TypeRef rather than into the
// type node, so this is the one predicate every site computing a Nullable bit
// goes through — a definition site, a union, an allOf conjunct and a $ref use
// site must never disagree about the same schema.
func schemaAdmitsNull(s *oas3.Schema) bool {
	budget := maxNullConjuncts
	return schemaNullVerdict(s, &budget) == nullAdmitted
}

// schemaNullVerdict reads what a whole schema says about null, by conjoining
// what each of its keyword families says (foldNullVerdicts).
//
// 3.0 `nullable: true` is the one keyword that does not conjoin: it widens the
// schema it is written on, which is what it exists to do, so it decides alone.
// The 3.1 spelling is an ordinary `type` member and conjoins like any other —
// which is why `{type: [string, "null"], enum: [red, green]}` does not admit
// null while `{type: string, nullable: true, enum: [red, green]}` does.
func schemaNullVerdict(s *oas3.Schema, budget *int) nullVerdict {
	if s == nil || *budget <= 0 {
		return nullSilent
	}
	*budget--
	if s.Nullable != nil && *s.Nullable {
		return nullAdmitted
	}
	return foldNullVerdicts(typeNullVerdict(s), constNullVerdict(s), enumNullVerdict(s),
		unionNullVerdict(s), allOfNullVerdict(s, budget))
}

// foldNullVerdicts conjoins keyword verdicts: one forbidding family decides the
// schema, otherwise one admitting family does, and a schema no family speaks for
// stays silent — which is not the same answer as forbidding, since a silent
// conjunct must not veto a sibling that admits.
func foldNullVerdicts(verdicts ...nullVerdict) nullVerdict {
	out := nullSilent
	for _, v := range verdicts {
		if v == nullForbidden {
			return nullForbidden
		}
		if v == nullAdmitted {
			out = nullAdmitted
		}
	}
	return out
}

// typeNullVerdict reads the `type` keyword. A schema writing none constrains no
// instance kind at all, so it is silent rather than forbidding.
func typeNullVerdict(s *oas3.Schema) nullVerdict {
	types := s.GetType()
	if len(types) == 0 {
		return nullSilent
	}
	if slices.Contains(types, oas3.SchemaTypeNull) {
		return nullAdmitted
	}
	return nullForbidden
}

// constNullVerdict reads `const`, which fixes the value space to one member.
func constNullVerdict(s *oas3.Schema) nullVerdict {
	node := s.GetConst()
	if node == nil {
		return nullSilent
	}
	if isNullValue(node) {
		return nullAdmitted
	}
	return nullForbidden
}

// enumNullVerdict reads `enum`, which fixes the value space to its members: the
// position admits null exactly when a member is null.
//
// An empty enum is silent rather than forbidding. It lists no member at all, so
// reading it as "no null member" would let a degenerate keyword strip a
// co-declared type array's null; what an empty enum lowers to is GitHub #278's
// question, and this rule leaves it open.
func enumNullVerdict(s *oas3.Schema) nullVerdict {
	nodes := s.GetEnum()
	if len(nodes) == 0 {
		return nullSilent
	}
	if slices.ContainsFunc(nodes, isNullValue) {
		return nullAdmitted
	}
	return nullForbidden
}

// isNullValue reports whether an enum member or const node is the null literal,
// read through the same converter enumMembers drops a member by. Both must
// recognize one spelling: the null this predicate lifts onto a reference is
// exactly the member that lowering strips.
func isNullValue(node values.Value) bool {
	val, err := value.FromNode(node)
	return err == nil && val.Kind == ir.ValueNull
}

// unionNullVerdict reads a oneOf/anyOf null branch, which counts only when the
// union is the type itself. Structural siblings intersect with the union, so
// `{type: object, oneOf: [{type: string}, {type: null}]}` admits neither string
// nor null, and that union is kept verbatim under Unmodeled. An inline
// `type: null` branch also blocks distribution, so no distributed union can
// strip a null branch from under this verdict.
//
// It never forbids: a union with no null branch says nothing about a null that
// a sibling keyword admits, as in `{nullable: true, oneOf: [...]}`.
func unionNullVerdict(s *oas3.Schema) nullVerdict {
	if oneOfAnyOfHasNull(s) && !hasUnionSiblings(s) {
		return nullAdmitted
	}
	return nullSilent
}

// allOfNullVerdict conjoins what the allOf branches say. A conjunction admits
// null when a branch does and none forbids it, which is what makes
// `{allOf: [{$ref: T}]}` answer the same as `{$ref: T}` — the composition
// declares no nullability of its own, and the usage naming it has nowhere else
// to derive the bit from (GitHub #279). Model.Base and Mixins still carry no
// Nullable bit: they name a conjunct, and nullability is a property of the
// usage that names the conjunction.
func allOfNullVerdict(s *oas3.Schema, budget *int) nullVerdict {
	out := nullSilent
	for _, b := range s.GetAllOf() {
		out = foldNullVerdicts(out, conjunctNullVerdict(b, budget))
	}
	return out
}

// conjunctNullVerdict reads one allOf branch. A `false` branch admits no
// instance whatever, null included; a `true` branch constrains nothing.
func conjunctNullVerdict(b *oas3.JSONSchema[oas3.Referenceable], budget *int) nullVerdict {
	if b == nil {
		return nullSilent
	}
	if b.IsBool() {
		if v := b.GetBool(); v != nil && !*v {
			return nullForbidden
		}
		return nullSilent
	}
	s := b.GetSchema()
	if resolve.IsRefSite(b, s) {
		return refNullVerdict(b, budget)
	}
	return schemaNullVerdict(s, budget)
}

// refNullVerdict reads a $ref usage: the reference site or its resolved target
// admitting null is enough, since a site's keywords widen the referent as often
// as they narrow it (3.0 writes `{$ref: T, nullable: true}` for exactly that).
// Only when neither admits does a forbidding side decide.
//
// The ref site must be read at all because a target interned at its own ID — a
// model, a union — discards the TypeRef its definition produced, so the bit
// survives nowhere else.
func refNullVerdict(js *oas3.JSONSchema[oas3.Referenceable], budget *int) nullVerdict {
	site := schemaNullVerdict(js.GetSchema(), budget)
	if site == nullAdmitted {
		return nullAdmitted
	}
	var target nullVerdict
	if resolved := js.GetResolvedSchema(); resolved != nil {
		target = schemaNullVerdict(resolved.GetSchema(), budget)
	}
	if target == nullAdmitted {
		return nullAdmitted
	}
	return foldNullVerdicts(site, target)
}

// nullUnionCollapse detects a oneOf/anyOf with exactly one non-null branch
// beside `type: null` branches, and returns that branch's schema, pointer and
// hint so it lowers as nullable X, not a union node (ir-design §3.3).
//
// The hint is the branch's own (branchHint), which an outside $ref naming the
// same pointer also derives. Whichever lowers first interns the node, so any
// other hint would make the document depend on declaration order (GitHub #281).
//
// A schema declaring both combinators collapses neither: the co-declared anyOf
// conjoins, so the position is not nullable X. The Union keeps the unused
// combinator (preserveUnusedCombinator), which X's shared node cannot.
func nullUnionCollapse(s *oas3.Schema, pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], jsontext.Pointer, string, bool) {
	if len(s.GetOneOf()) > 0 && len(s.GetAnyOf()) > 0 {
		return nil, "", "", false
	}
	variants, key, _ := unionBranches(s)
	var nonNull *oas3.JSONSchema[oas3.Referenceable]
	nonNullIdx, nonNullCount, nullCount := -1, 0, 0
	for i, v := range variants {
		if isNullSchema(v) {
			nullCount++
			continue
		}
		nonNull, nonNullIdx = v, i
		nonNullCount++
	}
	if nullCount == 0 || nonNullCount != 1 {
		return nil, "", "", false
	}
	return nonNull, pointer + ids.Ptr(key, strconv.Itoa(nonNullIdx)), branchHint(nonNull, nonNullIdx), true
}

// isNullSchema reports whether a variant schema is the bare null-typed schema.
func isNullSchema(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	if js == nil || !js.IsSchema() {
		return false
	}
	s := js.GetSchema()
	if s == nil {
		return false
	}
	types := s.GetType()
	return len(types) == 1 && types[0] == oas3.SchemaTypeNull
}

// allNullUnion reports whether every branch of the oneOf/anyOf combinator
// unionBranches elects is a bare `type: null` schema. nullUnionCollapse has no
// non-null branch to collapse a set like this onto, so without this check
// lowerOneOfAnyOf's buildUnion fallback strips every branch as a null marker
// and interns a Union with none left — the empty value space irverify's
// ir/union-no-variants rejects (GitHub #416). The branch set still admits
// exactly one value, the same one a bare `{type: null}` schema at this
// position admits, so lowerOneOfAnyOf lowers it the same way instead.
func allNullUnion(s *oas3.Schema) bool {
	variants, _, _ := unionBranches(s)
	if len(variants) == 0 {
		return false
	}
	for _, v := range variants {
		if !isNullSchema(v) {
			return false
		}
	}
	return true
}

// listConstraints reads a list schema's collection constraints. Only the safe
// integer/bool bounds are read here; numeric-value bounds go through raw nodes
// elsewhere to avoid the float64 trap.
func listConstraints(s *oas3.Schema) *ir.Constraints {
	if s.MinItems == nil && s.MaxItems == nil && s.UniqueItems == nil {
		return nil
	}
	c := &ir.Constraints{MinItems: s.MinItems, MaxItems: s.MaxItems}
	if s.UniqueItems != nil {
		c.UniqueItems = *s.UniqueItems
	}
	return c
}

// requiredSet builds a lookup of a model's required property names.
func requiredSet(required []string) map[string]bool {
	if len(required) == 0 {
		return nil
	}
	set := make(map[string]bool, len(required))
	for _, r := range required {
		set[r] = true
	}
	return set
}

// merger builds the allOf property reconciler over an explicit diagnostic sink.
//
// Merger.Report records rather than returns, which is the one place this package
// still hands diagnostics to a callback. diags is the caller's own slice, so what
// the reconciler reports travels out the way everything else does; the callback
// is the merge package's contract, not accumulation surviving here. It is two
// closures over state already at hand, so building it per use costs nothing
// worth measuring.
func merger(c lowering.Ctx, ts *compile.Types, diags *[]ir.Diagnostic) merge.Merger {
	return merge.Merger{
		Resolve: ts.Node,
		Report: func(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any) {
			*diags = append(*diags, c.DiagAt(sev, code, pointer, format, args...))
		},
	}
}
