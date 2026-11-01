// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// censusKeywords are the keywords whose IR home depends on what the position
// lowered to: a Model's properties, a Model's or MapT's catch-all, a List's
// element type and bounds, a Literal's value, an Enum's members, a Model's or
// Union's discriminator. keywordHome asks the built node which a position kept.
//
// Annotations, validation-only and use-site keywords, content vocabulary and
// value constraints have their own keepers.
//
// The list and keywordHome's switch must agree. A keyword listed without an
// arm is homeless and preserved everywhere, noisy but lossless; one in neither
// is dropped in silence (GitHub #268, #283).
// oneOf/anyOf/allOf stay out; see refSiteUnhomedKeywords (GitHub #406).
var censusKeywords = []string{
	"additionalProperties", "const", "discriminator", "enum", "format", "items",
	"maxItems", "minItems", "patternProperties", "prefixItems", "properties",
	"required", "type", "uniqueItems",
}

// keywordHome reports whether td, the node this position's declaration
// lowered to, has the field the named keyword lowers into. It asks the node
// rather than re-deriving lower()'s dispatch, so the two cannot drift apart.
//
// A nil td is a position lowered to a reference (referenceOutcomeKeywords, or a
// $ref site). Its alias carries only the position's constraints and
// annotations, and the target's fields are the referent's, so a `format` beside
// a $ref must not read the referent's Encoding as its home. Every keyword is
// homeless there but a null-only `type` and a redaction `format`, which
// annotation readers hold.
func keywordHome(td ir.TypeDef, s *oas3.Schema, keyword string) bool {
	switch keyword {
	case "properties", "patternProperties", "additionalProperties":
		// A MapT homes all three: its Value is the catch-all, and it is a map
		// only because the other two declared no member (ir.AsMap).
		return isKind(td, ir.KindModel) || isKind(td, ir.KindMap)
	case "required":
		// A MapT has no key to require, so it homes only an empty list. A name
		// lands on a property or in Model.Requires (requireMembers), either of
		// which keeps the node a Model, so a MapT never meets a longer one.
		return isKind(td, ir.KindModel) || (isKind(td, ir.KindMap) && len(s.GetRequired()) == 0)
	case "items", "prefixItems":
		return isKind(td, ir.KindList) || isKind(td, ir.KindTuple)
	case "maxItems", "minItems", "uniqueItems":
		// Either collection node, since collectionConstraints fills both. That is
		// also why valueConstraintKeywords leaves them out, which puts them in this
		// census rather than in declaredConstraints.
		return isKind(td, ir.KindList) || isKind(td, ir.KindTuple)
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
	case "discriminator":
		return discriminatorHome(td)
	default:
		return false
	}
}

// discriminatorHome reports whether td holds the discriminator its position
// declared. Only a Model and a Union have the field, and having it is not
// filling it: the Union a mixed-type enum lowers to carries none. One kept for
// want of a home keeps its extensions a second time, under
// openapi:discriminator/x-* (GitHub #849).
func discriminatorHome(td ir.TypeDef) bool {
	switch n := td.(type) {
	case *ir.Model:
		return n.Discriminator != nil
	case *ir.Union:
		return n.Discriminator != nil
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
// hoisted for it carries it in Encoding; a (type, format) pairing
// spelling.Lookup knows selects a primitive, which carries the pairing in the
// primitive kind itself. With no type declared there was no pairing to make
// and nothing read it, and a node of any other kind has no Encoding field at
// all.
func formatHome(td ir.TypeDef, s *oas3.Schema) bool {
	// The redaction format names no shape, so no node kind is its home: the
	// annotation readers hold it, as Sensitive on the node the position owns or
	// Secret on its carrier, and verbatim at a parameter, which has no field.
	if annotation.DeclaresRedaction(s) {
		return true
	}
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
// about it. Any admits every shape, so it contradicts none; External and
// keywordHome's nil td carry no shape.
func typeShapedBy(td ir.TypeDef, st oas3.SchemaType) bool {
	switch td.(type) {
	case *ir.Model, *ir.MapT:
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
// Having the field is not filling it, so a Scalar, Model or MapT is asked
// whether it is non-nil; lowerModel and lowerAllOf fill a Model's, and so its
// MapT's, the same way (GitHub #407). A List or Tuple is absent because
// lowerArray and buildTuple fill theirs from collectionConstraints, whose
// collection bounds valueConstraintKeywords excludes, so no value constraint
// reaches either however full the field looks.
func constraintsHome(td ir.TypeDef) bool {
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Constraints != nil
	case *ir.Model:
		return n.Constraints != nil
	case *ir.MapT:
		return n.Constraints != nil
	default:
		// Every other kind has no Constraints field, or holds only collection
		// bounds in it as a List and a Tuple do, so a value bound written beside
		// one was read by nothing.
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
// requireMembers. Recording them here too would report one keyword twice.
func unhomedKeywords(s *oas3.Schema, td ir.TypeDef, handled []string) []string {
	return unhomedAcross(s, []ir.TypeDef{td}, handled)
}

// unhomedAcross is unhomedKeywords for what any of nodes can carry.
func unhomedAcross(s *oas3.Schema, nodes []ir.TypeDef, handled []string) []string {
	out := make([]string, 0, len(censusKeywords))
	for _, keyword := range censusKeywords {
		if annotation.RawPropertyNode(s, keyword) == nil || slices.Contains(handled, keyword) {
			continue
		}
		if !slices.ContainsFunc(nodes, func(td ir.TypeDef) bool { return keywordHome(td, s, keyword) }) {
			out = append(out, keyword)
		}
	}
	return out
}

// refSiteUnhomedKeywords is unhomedKeywords for a $ref site (td is always nil),
// plus allOf. handled passes through so an allOf branch spelled as a $ref still
// routes its own `required` to requireMembers.
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

// What a position lowering to a reference lowered to, for the diagnostic
// recordUnhomedAt reports.
const (
	collapsedShape = "a nullable reference to its one non-null branch"
	expandedShape  = "a reference to the type its $dynamicRef expands to"
)

// referenceOutcomeKeywords returns the census keywords s writes when it lowers
// to a reference to a node that is not its own, and what it lowered to: a
// collapsed union (lowerOneOfAnyOf), or an expanded $dynamicRef, which a
// lowering of s decides first. It is nil for any other outcome. Every keyword
// is homeless there, as the target belongs to its declaration as a $ref site's
// does to the referent (keywordHome's nil td). Those two write few: neither
// expands or collapses beside a shape (GitHub #595).
func referenceOutcomeKeywords(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) ([]string, string) {
	if _, _, expanded, _ := dynamicExpansion(c, s, pointer); expanded {
		return unhomedKeywords(s, nil, nil), expandedShape
	}
	if hasUnionSiblings(s) {
		return nil, ""
	}
	if _, _, collapses := nullUnionCollapse(s, ""); !collapses {
		return nil, ""
	}
	return unhomedKeywords(s, nil, nil), collapsedShape
}

// preserveUnhomedKeywords keeps verbatim the census keywords and value
// constraints a position declared that its lowered node cannot carry, and the
// families lower()'s election skipped, returning the position's resulting ID.
// docs/compilers/openapi.md#shape-applicator covers a contradictory schema's
// dropped half and an untyped applicator such as `items`.
//
// A lowering that reduced to a shared node gets an alias first, with the
// position's constraints, so a shared node never carries one declaration's
// keywords. A node the position owns gets none, so a bound beside an Enum is
// kept verbatim.
//
// A multipart body still mints encoding keys for `properties` the node lacks,
// so pass.Validate reports ir/encoding-key-unknown-property.
func preserveUnhomedKeywords(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, id ir.TypeID, d dispatch) (ir.TypeID, []ir.Diagnostic) {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return id, diags
	}
	owns, _ := ts.Lookup(c.IDs().Coordinate(pointer))
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
		var aliasDiags []ir.Diagnostic
		owner, aliasDiags = internAlias(c, ts, s, pointer, ir.TypeRef{Target: id})
		diags = append(diags, aliasDiags...)
	}
	diags = append(diags, recordUnhomedKeywords(c, ts, owner, s, unhomed, nodeShape(td.Kind()), pointer)...)
	return owner, append(diags, recordSkippedFamilies(c, ts, owner, s, d, pointer)...)
}

// refSiteShape describes what a $ref position's own declaration lowers to, for
// the diagnostic recordUnhomedAt reports.
const refSiteShape = "an alias over its $ref target"

// nodeShape describes a node of kind k, for that same diagnostic.
func nodeShape(kind ir.TypeKind) string { return fmt.Sprintf("a node of kind %q", kind) }

// distributedShape describes the Union a body distributed across its variants
// lowers to, for that same diagnostic.
const distributedShape = "a union of models conjoining its body with each branch"

// preserveVariantsUnhomed keeps verbatim on the Union at id what s declares that
// no variant carries, for a Union each variant of which s itself builds: a type
// array's (lowerUnion) and a distributed composition's (composedVariant). The
// Union has no field for what they consume, so the census asks them; handled is
// as unhomedKeywords takes it. A constraint reaches each variant that has the
// field, so it is kept only where none does.
func preserveVariantsUnhomed(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, id ir.TypeID, shape string, handled []string, pointer jsontext.Pointer) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	u, isUnion := td.(*ir.Union)
	if !isUnion {
		return append(diags, c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
			"internal: type %q is not the union its variants were built for", id))
	}
	nodes := make([]ir.TypeDef, 0, len(u.Variants))
	for _, v := range u.Variants {
		node, held, nodeDiags := registeredNode(c, ts, v.Type.Target, pointer)
		diags = append(diags, nodeDiags...)
		if held {
			nodes = append(nodes, node)
		}
	}
	unhomed := unhomedAcross(s, nodes, handled)
	if !slices.ContainsFunc(nodes, constraintsHome) {
		unhomed = append(unhomed, declaredConstraints(s)...)
	}
	return append(diags, recordUnhomedKeywords(c, ts, id, s, unhomed, shape, pointer)...)
}

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
		ok, keptDiags := annotation.PreserveSchemaKeyword(p, s, keyword,
			ir.ReasonDegradedLowering, pointer, c.ProvenanceAt)
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
