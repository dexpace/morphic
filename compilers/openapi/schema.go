package openapi

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/speakeasy-api/openapi/extensions"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// lowerComponentSchemas interns every named component schema in source order.
// It is the entry Compile's run() calls before any operation lowering so that
// $refs resolve to already-registered IDs.
func (l *lowerer) lowerComponentSchemas() {
	comps := l.doc.Components
	if comps == nil {
		return
	}
	schemas := comps.GetSchemas()
	if schemas == nil {
		return
	}
	// Record every declared name before lowering any schema so a $ref or
	// discriminator mapping resolved mid-lowering sees forward-declared
	// components as valid targets regardless of source order.
	l.schemas = make(map[string]bool, schemas.Len())
	for name := range schemas.All() {
		l.schemas[name] = true
	}
	for name, js := range schemas.All() {
		l.lowerComponentSchema(js, ptr("components", "schemas", name), name)
	}
}

// lowerComponentSchema lowers one named component schema and guarantees a node
// is registered at the component's own TypeID even when its body reduces to a
// shared primitive/any or aliases another type. Without this, a component like
// `MyId: {type: string, format: uuid}` would leave nothing at its component
// pointer and every $ref to it would dangle (invariants 1 and 2).
func (l *lowerer) lowerComponentSchema(js *oas3.JSONSchema[oas3.Referenceable], pointer, name string) {
	s := siteAt(js)
	ref := l.schemaRef(js, pointer, name)
	if _, owned := l.byPointer[pointer]; owned {
		return // schemaRef interned the component's own node at its component ID
	}
	l.internAlias(pointer, name, ref, l.schemaConstraints(s.Node, pointer))
	// This alias is the first node the pointer owns, so the annotations
	// schemaBody had nowhere to put now have a home.
	if s.Node != nil {
		l.attachDeclaredAnnotations(s.Node, pointer)
	}
}

// schemaConstraints reads the value constraints of a schema and stamps each
// constraint diagnostic with pointer's provenance. It returns nil only when
// there is no schema to read. It is the shared path for every alias a body
// reduces to — a named component (lowerComponentSchema) and a $ref-hoisted
// internal sub-schema (hoistSubSchema) — so a scalar that aliases a shared
// primitive never drops the constraints it carried, including a bound written
// beside a $ref, which constrains the position it is written at.
func (l *lowerer) schemaConstraints(s *oas3.Schema, pointer string) *ir.Constraints {
	if s == nil {
		return nil
	}
	c, diags := constraintsFromSchema(s, l.exclusiveBoundIsBoolean())
	l.appendConstraintDiags(diags, pointer)
	return c
}

// internAlias interns a named Scalar at pointer whose Base is target, so a
// component (or a sibling-carrying schema) whose body lowered to a shared or
// referenced target still owns a resolvable node at its own TypeID. Any value
// constraints the schema carried are attached so a scalar component never drops
// them.
func (l *lowerer) internAlias(pointer, hint string, target ir.TypeRef, constraints *ir.Constraints) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := target
		return &ir.Scalar{TypeCommon: common, Base: &base, Constraints: constraints}
	})
}

// schemaBody lowers a concrete (non-reference) schema body to a TypeRef and
// records the schema's own annotations on whatever node the lowering hoisted at
// pointer. It is shared by schemaRef and by sub-schema hoisting
// (resolveSchemaRef), which both reach a body only after peeling off any
// leading $ref.
func (l *lowerer) schemaBody(schema *oas3.Schema, pointer, hint string) ir.TypeRef {
	ref := l.lowerSchemaBody(schema, pointer, hint)
	l.attachDeclaredAnnotations(schema, pointer)
	return ref
}

// lowerSchemaBody lowers the body itself, handling the oneOf/anyOf dispatch that
// precedes structural lowering.
func (l *lowerer) lowerSchemaBody(schema *oas3.Schema, pointer, hint string) ir.TypeRef {
	if len(schema.GetOneOf()) > 0 || len(schema.GetAnyOf()) > 0 {
		if hasUnionSiblings(schema) {
			return ir.TypeRef{
				Target:   l.lowerWithUnionSiblings(schema, pointer, hint),
				Nullable: schemaAdmitsNull(schema),
			}
		}
		return l.lowerOneOfAnyOf(schema, pointer, hint)
	}
	return ir.TypeRef{Target: l.lower(schema, pointer, hint), Nullable: schemaAdmitsNull(schema)}
}

// hasUnionSiblings reports whether a oneOf/anyOf schema also carries structural
// keywords (a type, properties, allOf, const/enum, additionalProperties, ...).
// When it does, the union alone cannot represent the schema, so the structural
// body must be lowered too rather than dropped (invariant 2, ir-design §4.3).
func hasUnionSiblings(s *oas3.Schema) bool {
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return true
	}
	if len(s.GetRequired()) > 0 || len(s.GetAllOf()) > 0 {
		return true
	}
	if s.GetConst() != nil || len(s.GetEnum()) > 0 {
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

// lowerWithUnionSiblings lowers the structural body of a schema that co-declares
// oneOf/anyOf, then preserves the union branches verbatim under the resulting
// node's Extensions with an info diagnostic — so neither the structural shape
// nor the union is dropped (ir-design §4.7-style preservation).
func (l *lowerer) lowerWithUnionSiblings(s *oas3.Schema, pointer, hint string) ir.TypeID {
	inner := l.lower(s, pointer, hint)
	owner := inner
	if l.byPointer[pointer] != inner {
		// The structural body reduced to a shared/aliased target; hoist an alias
		// so the preserved union attaches to a node this pointer owns, never to a
		// shared primitive.
		owner = l.internAlias(pointer, hint, ir.TypeRef{Target: inner}, nil)
	}
	l.preserveUnionSiblings(owner, s, pointer)
	return owner
}

// preserveUnionSiblings stores the raw oneOf/anyOf of s under the owning node's
// Extensions and emits one info diagnostic naming the preserved construct.
func (l *lowerer) preserveUnionSiblings(id ir.TypeID, s *oas3.Schema, pointer string) {
	td, ok := l.out.Types[id]
	if !ok {
		return
	}
	c := td.Common()
	if len(s.GetOneOf()) > 0 {
		if raw := nodeToRaw(rawPropertyNode(s, "oneOf")); raw != nil {
			c.Extensions = mergeExtensions(c.Extensions, ir.Extensions{"openapi:oneOf": raw})
		}
	}
	if len(s.GetAnyOf()) > 0 {
		if raw := nodeToRaw(rawPropertyNode(s, "anyOf")); raw != nil {
			c.Extensions = mergeExtensions(c.Extensions, ir.Extensions{"openapi:anyOf": raw})
		}
	}
	l.diag(ir.SeverityInfo, codeDegradedConstruct, pointer,
		"oneOf/anyOf co-declared with structural keywords; union branches preserved verbatim under extensions")
}

// falseSchema hoists a boolean `false` schema as a closed empty model (it
// matches nothing) and records one info diagnostic on first visit.
func (l *lowerer) falseSchema(pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		l.diag(ir.SeverityInfo, codeFalseSchema, pointer,
			"boolean false schema matches nothing; lowered as a closed empty model")
		return &ir.Model{TypeCommon: common, Additional: ir.AdditionalClosed}
	})
}

// lower interns the inline schema at pointer and returns its TypeID. Value
// constraints (const, enum) and allOf composition take precedence over the
// structural type; otherwise it dispatches on the effective (null-stripped)
// type set. const hoists through hoistLiteral — the same primitive that
// hoists each individual member of a heterogeneous enum (enumAsUnion) — since
// a bare `const` schema is exactly a Literal at its own pointer.
func (l *lowerer) lower(s *oas3.Schema, pointer, hint string) ir.TypeID {
	if c := s.GetConst(); c != nil {
		return l.hoistLiteral(c, pointer, hint)
	}
	if len(s.GetEnum()) > 0 {
		return l.lowerEnum(s, pointer, hint)
	}
	if len(s.GetAllOf()) > 0 {
		return l.lowerAllOf(s, pointer, hint)
	}
	types := effectiveTypes(s)
	switch {
	case len(types) > 1:
		return l.lowerUnion(s, pointer, hint, types)
	case len(types) == 1:
		return l.lowerTyped(s, pointer, hint, types[0])
	default:
		return l.lowerUntyped(s, pointer, hint)
	}
}

// lowerTyped dispatches a single-typed schema to its structural or scalar form.
func (l *lowerer) lowerTyped(s *oas3.Schema, pointer, hint string, st oas3.SchemaType) ir.TypeID {
	switch st {
	case oas3.SchemaTypeObject:
		return l.lowerModel(s, pointer, hint)
	case oas3.SchemaTypeArray:
		return l.lowerArray(s, pointer, hint)
	default:
		return l.scalarTypeID(s, st, pointer, hint)
	}
}

// lowerUntyped handles a schema with no declared type: a property set makes it a
// model; enum/const and composition are lowered by later passes; anything else
// is schemaless.
func (l *lowerer) lowerUntyped(s *oas3.Schema, pointer, hint string) ir.TypeID {
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return l.lowerModel(s, pointer, hint)
	}
	return l.primID(ir.PrimAny)
}

// lowerUnion hoists a multi-typed schema (e.g. type: [string, integer]) as an
// exclusive, untagged union with one variant per declared type.
func (l *lowerer) lowerUnion(s *oas3.Schema, pointer, hint string, types []oas3.SchemaType) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		variants := make([]ir.Variant, 0, len(types))
		for i, st := range types {
			vptr := pointer + ptr("type", strconv.Itoa(i))
			variants = append(variants, ir.Variant{
				Name: ir.Naming{Hint: string(st)},
				Type: ir.TypeRef{Target: l.lowerTyped(s, vptr, hint, st)},
			})
		}
		return &ir.Union{
			TypeCommon: common,
			Variants:   variants,
			Exclusive:  true,
		}
	})
}

// lowerModel hoists an object schema as a Model. This task lowers only the
// property shape (name, type, required); a later pass fills the rest. It reads
// no annotations: attachDeclaredAnnotations does that for every destination.
func (l *lowerer) lowerModel(s *oas3.Schema, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		m := &ir.Model{TypeCommon: common}
		l.fillModelProperties(m, s, pointer)
		l.fillAdditional(m, s, pointer, hint)
		if d := l.lowerDiscriminator(s, m, pointer); d != nil {
			m.Discriminator = d
		}
		return m
	})
}

// fillModelProperties lowers a model's own properties in source order, each with
// its full property-level detail (constraints, visibility, defaults, docs, ...).
func (l *lowerer) fillModelProperties(m *ir.Model, s *oas3.Schema, pointer string) {
	props := s.GetProperties()
	if props == nil {
		return
	}
	required := requiredSet(s.GetRequired())
	byWire := wireNameIndex(m.Properties)
	for name, js := range props.All() {
		ppointer := pointer + ptr("properties", name)
		p := ir.Property{
			ID:         propID(ppointer),
			Name:       ir.Naming{Source: name, Canonical: canonicalWords(name)},
			WireName:   name,
			Type:       l.schemaRef(js, ppointer, name),
			Required:   required[name],
			Provenance: ir.Provenance{Source: l.srcIndex, Pointer: ppointer},
		}
		l.fillPropertyDetail(&p, js, ppointer)
		l.mergeProperty(m, byWire, p, ppointer)
	}
}

// wireNameIndex maps each property's wire name to its position in props, so a
// redeclaration reconciles in one lookup rather than a rescan (fillModelProperties
// runs once per allOf branch, so a linear scan would be quadratic in a wide model).
func wireNameIndex(props []ir.Property) map[string]int {
	idx := make(map[string]int, len(props))
	for i := range props {
		idx[props[i].WireName] = i
	}
	return idx
}

// mergeProperty appends p to m and records it in byWire, or folds it into the
// property that already carries the same wire name — overlapping allOf branches
// (and properties co-declared alongside allOf) redeclare one logical field under
// allOf's intersection semantics (ir-design §4.3). Callers must set p.WireName
// (fillModelProperties always does); it keys byWire directly.
func (l *lowerer) mergeProperty(m *ir.Model, byWire map[string]int, p ir.Property, pointer string) {
	if i, ok := byWire[p.WireName]; ok {
		l.reconcileProperty(&m.Properties[i], p, pointer)
		return
	}
	byWire[p.WireName] = len(m.Properties)
	m.Properties = append(m.Properties, p)
}

// reconcileProperty folds a redeclaration src into the already-present property
// dst under allOf intersection semantics: required and secret are OR-ed, dst
// keeps its position/identity/type shape (first declaration wins), and every
// optional detail dst lacks — docs, default, constraints (merged per keyword via
// mergeConstraints), deprecation, XML, examples, extensions — is adopted from
// src. A description that differs between branches, an incompatible type, or a
// contradictory constraint keyword are genuine conflicts the merge cannot
// represent (see codeConflictingRedecl); each is diagnosed before any detail is
// folded in, rather than silently picking an arbitrary winner.
func (l *lowerer) reconcileProperty(dst *ir.Property, src ir.Property, pointer string) {
	l.diagnoseRedeclarationConflict(dst, &src, pointer)

	dst.Required = dst.Required || src.Required
	dst.Secret = dst.Secret || src.Secret

	if dst.Docs.Description == "" {
		dst.Docs.Description = src.Docs.Description
	} else if src.Docs.Description != "" && src.Docs.Description != dst.Docs.Description {
		l.diag(ir.SeverityInfo, codeDegradedConstruct, pointer,
			"allOf branches describe field %q differently; kept the first declaration", dst.WireName)
	}
	dst.Default = cmp.Or(dst.Default, src.Default)
	dst.Constraints = mergeConstraints(dst.Constraints, src.Constraints)
	dst.Deprecation = cmp.Or(dst.Deprecation, src.Deprecation)
	dst.XML = cmp.Or(dst.XML, src.XML)
	if len(dst.Examples) == 0 {
		// Examples is a slice, not comparable, so it cannot go through cmp.Or
		// like its neighbors above; the len()==0 predicate is the adoption rule.
		dst.Examples = src.Examples
	}
	dst.Extensions = mergeExtensions(dst.Extensions, src.Extensions)
}

// mergeConstraints folds src's constraint keywords into dst under allOf
// intersection semantics: dst keeps every keyword it already sets, and adopts
// from src any keyword dst leaves unset (nil/""/false) — a keyword only one
// branch constrains still applies to the merged field, so it is never dropped.
//
// Min and Max are adopted together with their exclusivity flag: taking src.Min
// without src.ExclusiveMin would silently flip an exclusive "> 5" into an
// inclusive ">= 5". UniqueItems has no absent state to detect via cmp.Or, but
// under intersection a true from either branch is always correct, so adopting
// it via cmp.Or never wrongly downgrades dst from true to false.
func mergeConstraints(dst, src *ir.Constraints) *ir.Constraints {
	if dst == nil {
		return src
	}
	if src == nil {
		return dst
	}
	if dst.Min == nil {
		dst.Min, dst.ExclusiveMin = src.Min, src.ExclusiveMin
	}
	if dst.Max == nil {
		dst.Max, dst.ExclusiveMax = src.Max, src.ExclusiveMax
	}
	dst.MultipleOf = cmp.Or(dst.MultipleOf, src.MultipleOf)
	dst.Precision = cmp.Or(dst.Precision, src.Precision)
	dst.Scale = cmp.Or(dst.Scale, src.Scale)
	dst.MinLength = cmp.Or(dst.MinLength, src.MinLength)
	dst.MaxLength = cmp.Or(dst.MaxLength, src.MaxLength)
	dst.Pattern = cmp.Or(dst.Pattern, src.Pattern)
	dst.PatternMessage = cmp.Or(dst.PatternMessage, src.PatternMessage)
	dst.MinItems = cmp.Or(dst.MinItems, src.MinItems)
	dst.MaxItems = cmp.Or(dst.MaxItems, src.MaxItems)
	dst.UniqueItems = cmp.Or(dst.UniqueItems, src.UniqueItems)
	dst.MinProps = cmp.Or(dst.MinProps, src.MinProps)
	dst.MaxProps = cmp.Or(dst.MaxProps, src.MaxProps)
	return dst
}

// maxTypeResolveDepth bounds Base-chain resolution when classifying a property
// type for conflict detection (styleguide bounded-recursion rule). A scalar Base
// chain is far shorter than this in a well-formed document, so the cap only
// guards a pathological or malformed registry.
const maxTypeResolveDepth = 64

// diagnoseRedeclarationConflict reports when redeclaration src contradicts dst
// under allOf intersection — an incompatible target type, or a constraint
// keyword both branches pin to different values — without altering the merge
// (dst keeps its shape). A type conflict is genuinely unsatisfiable; a
// constraint conflict is usually satisfiable alone, but the merge can't
// represent the true intersection and may keep the looser bound
// (codeConflictingRedecl). At most one diagnostic fires: a type conflict
// subsumes any constraint conflict.
func (l *lowerer) diagnoseRedeclarationConflict(dst, src *ir.Property, pointer string) {
	if l.typesConflict(dst.Type, src.Type) {
		l.redeclarationConflictDiag(dst, pointer,
			fmt.Sprintf("incompatible types %s and %s", dst.Type.Target, src.Type.Target))
		return
	}
	if detail, ok := constraintsConflict(dst.Constraints, src.Constraints); ok {
		l.redeclarationConflictDiag(dst, pointer, detail)
	}
}

// redeclarationConflictDiag emits the shared conflicting-redeclaration warning,
// naming the field and both declaration sites (dst's own, and the redeclaration
// at pointer). detail is the caller-formatted disagreement — two type IDs, or a
// constraint keyword and its two values — so one wording serves both callers.
// It says "declarations", not "allOf branches": dst's declaration is not always
// inside an allOf branch (a property can also be declared directly alongside
// allOf), so the message must read correctly either way. Severity is warning —
// the merged model is still usable — leaving escalation to the consumer via
// the stable code.
func (l *lowerer) redeclarationConflictDiag(dst *ir.Property, pointer, detail string) {
	l.diag(ir.SeverityWarning, codeConflictingRedecl, pointer,
		"declarations of field %q disagree: %s; kept the first declaration (%s) over the redeclaration (%s)",
		dst.WireName, detail, dst.Provenance.Pointer, pointer)
}

// typesConflict reports whether two reconciled property types describe an
// unsatisfiable intersection. Identical interned targets and the schemaless top
// type never conflict. Types resolving to different underlying primitives conflict
// (string vs integer, string vs uuid), as does a scalar against a structural type.
// Two distinct composite types of the same kind (two models, two lists) are not
// provably contradictory, so they are never reported — conflict detection does
// not guess.
func (l *lowerer) typesConflict(a, b ir.TypeRef) bool {
	if a.Target == b.Target || l.isAnyType(a) || l.isAnyType(b) {
		return false
	}
	ak, aok := l.resolvePrimKind(a)
	bk, bok := l.resolvePrimKind(b)
	switch {
	case aok && bok:
		return ak != bk
	case aok && !bok:
		return l.isStructuralType(b)
	case !aok && bok:
		return l.isStructuralType(a)
	default:
		return l.differentTypeKind(a, b)
	}
}

// isAnyType reports whether ref targets the schemaless top type (a PrimAny
// primitive or an Any node). The top type imposes no constraint under allOf
// intersection, so it never conflicts with a sibling redeclaration.
func (l *lowerer) isAnyType(ref ir.TypeRef) bool {
	td, ok := l.out.Types[ref.Target]
	if !ok {
		return false
	}
	if p, ok := td.(*ir.Primitive); ok {
		return p.Prim == ir.PrimAny
	}
	return td.Kind() == ir.KindAny
}

// resolvePrimKind follows ref through the registry to its underlying primitive
// kind, returning ok=false when the target doesn't ultimately resolve to a
// single one (a model, union, list, tuple, literal, external, or a base-less
// opaque scalar). The Base chain is bounded against a malformed registry.
//
// An Enum resolves via its already-computed ValueType (ir-design §4.5), so a
// string enum conflicts with an integer redeclaration but not a string one. A
// Literal is deliberately left unresolved: Value.Kind doesn't map cleanly to
// one PrimKind (a ValueNumber literal spans integer/number/float/decimal), so
// rather than guess it is excluded from isStructuralType below too.
func (l *lowerer) resolvePrimKind(ref ir.TypeRef) (ir.PrimKind, bool) {
	id := ref.Target
	for range maxTypeResolveDepth {
		td, ok := l.out.Types[id]
		if !ok {
			return "", false
		}
		switch t := td.(type) {
		case *ir.Primitive:
			return t.Prim, true
		case *ir.Enum:
			return t.ValueType, true
		case *ir.Scalar:
			if t.Base == nil {
				return "", false
			}
			id = t.Base.Target
		default:
			return "", false
		}
	}
	return "", false
}

// differentTypeKind reports whether two registry targets carry different
// TypeDef kinds (a model vs a union); an unresolvable target is never treated
// as a conflict. Reached only from typesConflict's default case, when neither
// side resolved a PrimKind — e.g. a base-less opaque scalar against a Union.
func (l *lowerer) differentTypeKind(a, b ir.TypeRef) bool {
	at, aok := l.out.Types[a.Target]
	bt, bok := l.out.Types[b.Target]
	if !aok || !bok {
		return false
	}
	return at.Kind() != bt.Kind()
}

// isStructuralType reports whether ref targets a type that can never be a
// scalar under allOf intersection: a model, list, map, or tuple — the only
// shapes provably incompatible with a scalar redeclaration.
//
// Enum, Union, and External are deliberately excluded: an enum member or union
// variant can itself be a scalar of the kind being redeclared, so a bare
// scalar sibling isn't provably contradictory. Enum instead resolves via its
// ValueType in resolvePrimKind above; Union/External/Literal stay unresolved
// (Literal for the same reason as in resolvePrimKind), and a base-less opaque
// scalar is likewise unknown, not structural — none of these count as
// conflicts.
func (l *lowerer) isStructuralType(ref ir.TypeRef) bool {
	td, ok := l.out.Types[ref.Target]
	if !ok {
		return false
	}
	switch td.(type) {
	case *ir.Model, *ir.List, *ir.MapT, *ir.Tuple:
		return true
	default:
		return false
	}
}

// constraintsConflict reports whether two constraint sets pin the same keyword
// to incompatible values, describing which keyword and both values when they
// do (e.g. "conflicting maxLength (10 and 20)"). A keyword set on only one
// side is not a conflict — mergeConstraints adopts it, narrowing the merged
// field rather than discarding it — so only a keyword present and differing on
// both sides counts; numeric bounds compare by magnitude, so 10 and 10.0
// aren't a false conflict. UniqueItems is never compared: it's a plain bool
// with no absent state, so a false on either side can't be told apart from
// "not set". Keywords are checked in the fixed order below, so the keyword
// named when several conflict at once is deterministic.
func constraintsConflict(a, b *ir.Constraints) (string, bool) {
	if a == nil || b == nil {
		return "", false
	}
	checks := []func() (string, bool){
		func() (string, bool) {
			return boundConflictDetail("minimum", a.Min, b.Min, a.ExclusiveMin, b.ExclusiveMin)
		},
		func() (string, bool) {
			return boundConflictDetail("maximum", a.Max, b.Max, a.ExclusiveMax, b.ExclusiveMax)
		},
		func() (string, bool) { return bigValConflictDetail("multipleOf", a.MultipleOf, b.MultipleOf) },
		func() (string, bool) { return intConflictDetail("precision", a.Precision, b.Precision) },
		func() (string, bool) { return intConflictDetail("scale", a.Scale, b.Scale) },
		func() (string, bool) { return intConflictDetail("minLength", a.MinLength, b.MinLength) },
		func() (string, bool) { return intConflictDetail("maxLength", a.MaxLength, b.MaxLength) },
		func() (string, bool) { return intConflictDetail("minItems", a.MinItems, b.MinItems) },
		func() (string, bool) { return intConflictDetail("maxItems", a.MaxItems, b.MaxItems) },
		func() (string, bool) { return intConflictDetail("minProps", a.MinProps, b.MinProps) },
		func() (string, bool) { return intConflictDetail("maxProps", a.MaxProps, b.MaxProps) },
		func() (string, bool) { return strConflictDetail("pattern", a.Pattern, b.Pattern) },
		func() (string, bool) { return strConflictDetail("patternMessage", a.PatternMessage, b.PatternMessage) },
	}
	for _, check := range checks {
		if detail, ok := check(); ok {
			return detail, ok
		}
	}
	return "", false
}

// boundConflictDetail reports whether two numeric bounds, each with its
// exclusivity flag, are both present and disagree in magnitude or in
// inclusive/exclusive sense, formatting the disagreement when they do. Such a
// disagreement is usually still individually satisfiable (minimum: 10 and
// exclusiveMinimum: 10 together just mean "> 10"), but it's diagnosed anyway:
// the merge keeps dst's bound (first declaration wins) over the true
// intersection, and the discarded bound is always the stricter one — staying
// silent would silently loosen the validation the spec intended.
func boundConflictDetail(keyword string, a, b *ir.BigVal, exclA, exclB bool) (string, bool) {
	if a == nil || b == nil || (exclA == exclB && bigValEqual(*a, *b)) {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%s and %s)", keyword, boundText(*a, exclA), boundText(*b, exclB)), true
}

// boundText renders a numeric bound for a conflict detail, marking an
// exclusive bound so "conflicting minimum (10 and exclusive 10)" reads as the
// differing sense it is, not a duplicate magnitude.
func boundText(v ir.BigVal, exclusive bool) string {
	if exclusive {
		return "exclusive " + v.String()
	}
	return v.String()
}

// bigValConflictDetail reports whether two optional numeric values are both
// present and differ by magnitude, formatting the disagreement when they do.
func bigValConflictDetail(keyword string, a, b *ir.BigVal) (string, bool) {
	if a == nil || b == nil || bigValEqual(*a, *b) {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%s and %s)", keyword, a.String(), b.String()), true
}

// bigValEqual reports whether two numeric literals denote the same value,
// comparing by magnitude so equal values spelled differently (10, 10.0, 1e1) are
// equal. Both are already-validated BigVals, so parsing succeeds; an unparseable
// pair falls back to exact string equality.
func bigValEqual(a, b ir.BigVal) bool {
	ar, aok := new(big.Rat).SetString(a.String())
	br, bok := new(big.Rat).SetString(b.String())
	if !aok || !bok {
		return a == b
	}
	return ar.Cmp(br) == 0
}

// intConflictDetail reports whether two optional integer bounds are both
// present and differ, formatting the disagreement when they do.
func intConflictDetail(keyword string, a, b *int64) (string, bool) {
	if a == nil || b == nil || *a == *b {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%d and %d)", keyword, *a, *b), true
}

// strConflictDetail reports whether two string keywords are both set and
// differ, formatting the disagreement when they do.
func strConflictDetail(keyword string, a, b string) (string, bool) {
	if a == "" || b == "" || a == b {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%q and %q)", keyword, a, b), true
}

// fillPropertyDetail enriches a property from its schema: docs, default,
// visibility, deprecation, secrecy, examples, XML, constraints, extensions, and
// validation-only keywords. Annotations present at a $ref use-site override the
// target's (ir-design §14).
func (l *lowerer) fillPropertyDetail(p *ir.Property, js *oas3.JSONSchema[oas3.Referenceable], pointer string) {
	ref := js.GetSchema()
	if ref == nil {
		return
	}
	tgt := l.refTargetSchema(js, ref)
	if d := effectiveDescription(ref, tgt); d != "" {
		p.Docs.Description = d
	}
	l.fillPropertyDefault(p, ref, tgt, pointer)
	if ref.GetFormat() == "password" {
		p.Secret = true
	}
	p.Visibility = effectiveVisibility(ref, tgt)
	if effectiveDeprecated(ref, tgt) {
		p.Deprecation = &ir.Deprecation{}
	}
	l.fillPropertyExamples(p, ref, pointer)
	if h := xmlHints(ref.GetXML()); h != nil {
		p.XML = h
	}
	l.fillPropertyConstraints(p, ref, pointer)
	p.Extensions = mergeExtensions(p.Extensions, l.schemaExtensions(ref))
	l.fillPropertyValidationOnly(p, ref, pointer)
}

// fillPropertyValidationOnly preserves the validation-only keywords written at
// the property's own position, but only when its schema hoisted no node to hold
// them — the same one-home-per-declaration rule fillPropertyExamples applies.
// A $ref position never hoists one, which is what gives an if/then/else written
// beside a $ref somewhere to land (GitHub #114).
func (l *lowerer) fillPropertyValidationOnly(p *ir.Property, ref *oas3.Schema, pointer string) {
	if l.ownsNode(pointer) {
		return
	}
	l.fillValidationOnly(&p.Extensions, ref, pointer)
}

// ownsNode reports whether the declaration at pointer hoisted a type node of
// its own — the node that then holds its annotations.
func (l *lowerer) ownsNode(pointer string) bool {
	_, ok := l.byPointer[pointer]
	return ok
}

// fillPropertyDefault sets the property default, preferring the use-site node
// over the $ref target's; an unconvertible node yields a diagnostic.
func (l *lowerer) fillPropertyDefault(p *ir.Property, ref, tgt *oas3.Schema, pointer string) {
	node := ref.GetDefault()
	if node == nil && tgt != nil {
		node = tgt.GetDefault()
	}
	if node == nil {
		return
	}
	v, err := valueFromNode(node)
	if err != nil {
		l.diag(ir.SeverityWarning, codeDegradedConstruct, pointer, "default: %s", err.Error())
		return
	}
	p.Default = &v
}

// fillPropertyExamples records the schema's examples on the property, but only
// when the schema hoisted no node of its own to hold them. A schema that reduced
// to a shared primitive has nowhere else to put them, and the primitive itself
// must never carry per-declaration annotations; every other schema keeps them on
// its own node (attachSchemaExamples). One home per declaration means the two can
// never drift apart.
func (l *lowerer) fillPropertyExamples(p *ir.Property, ref *oas3.Schema, pointer string) {
	if l.ownsNode(pointer) {
		return
	}
	if ex := l.schemaExamples(ref, pointer); len(ex) > 0 {
		p.Examples = ex
	}
}

// fillPropertyConstraints attaches the property's scalar constraints and stamps
// each constraint diagnostic with the property's provenance.
func (l *lowerer) fillPropertyConstraints(p *ir.Property, ref *oas3.Schema, pointer string) {
	c, diags := constraintsFromSchema(ref, l.exclusiveBoundIsBoolean())
	l.appendConstraintDiags(diags, pointer)
	if c != nil {
		p.Constraints = c
	}
}

// schemaExamples lowers a schema's single (3.0) and plural (3.1) examples into
// value examples in source order, pointer being the owning schema's own
// pointer so an unconvertible example's diagnostic can locate it.
func (l *lowerer) schemaExamples(s *oas3.Schema, pointer string) []ir.Example {
	var out []ir.Example
	if node := s.GetExample(); node != nil {
		out = l.appendExample(out, ir.Example{}, node, pointer, "example")
	}
	for i, node := range s.GetExamples() {
		out = l.appendExample(out, ir.Example{}, node, pointer, "examples", strconv.Itoa(i))
	}
	return out
}

// attachDeclaredAnnotations records every annotation s declares on the type
// node pointer owns — the one structural home a declaration's annotations have
// (ir.TypeCommon, embedded in every type node).
//
// It is the sole reader of declaration-scoped annotations, and it runs *above*
// lower()'s dispatch: every declaration reaches it through schemaBody or an
// alias fallback, whatever its body turns out to lower to. That is what keeps
// a new lowering destination from silently dropping annotations — a
// destination never reads them, so it cannot forget to (GitHub #114).
//
// A schema whose body reduced to a shared primitive owns no node; its
// annotations stay with the declaring property (fillPropertyDetail), and the
// shared primitive must never carry a per-declaration annotation. Ownership is
// checked before conversion because the callers cover a pointer in either
// order, and only the one that finds a node may emit conversion diagnostics.
func (l *lowerer) attachDeclaredAnnotations(s *oas3.Schema, pointer string) {
	id, owned := l.byPointer[pointer]
	if !owned {
		return
	}
	td, ok := l.out.Types[id]
	if !ok {
		return
	}
	c := td.Common()
	fillTypeDocs(&c.Docs, s)
	if effectiveDeprecated(s, nil) {
		c.Deprecation = &ir.Deprecation{}
	}
	if h := xmlHints(s.GetXML()); h != nil {
		c.XML = h
	}
	c.Extensions = mergeExtensions(c.Extensions, l.schemaExtensions(s))
	l.fillValidationOnly(&c.Extensions, s, pointer)
	if ex := l.schemaExamples(s, pointer); len(ex) > 0 {
		c.Examples = ex
	}
}

// appendExample converts node into proto's value and appends the result to out;
// an unconvertible node is skipped with a warning diagnostic rather than silently
// dropped — an example is an annotation, not a structural hole, so losing it is
// fine as long as it isn't silent. proto carries the annotations that surround
// the value (name, summary, description); base and seg locate the node, joined
// into a pointer only on the failure path, so an example that converts builds no
// pointer string at all. Shared by every example site: schema (schemaExamples),
// media type, header, and parameter (exampleList).
func (l *lowerer) appendExample(out []ir.Example, proto ir.Example, node *yaml.Node, base string, seg ...string) []ir.Example {
	v, err := valueFromNode(node)
	if err != nil {
		l.diag(ir.SeverityWarning, codeDegradedConstruct, base+ptr(seg...), "example: %s", err.Error())
		return out
	}
	proto.Value = &v
	return append(out, proto)
}

// fillAdditional lowers additionalProperties, patternProperties, and
// unevaluatedProperties into the model's openness and catch-all shape.
func (l *lowerer) fillAdditional(m *ir.Model, s *oas3.Schema, pointer, hint string) {
	ap := s.GetAdditionalProperties()
	switch {
	case isFalseSchema(ap):
		m.Additional = ir.AdditionalClosed
	case ap != nil && !ap.IsBool():
		ref := l.schemaRef(ap, pointer+ptr("additionalProperties"), hint+"_value")
		m.AdditionalProps = &ir.AdditionalProps{Value: ref}
	}
	if patterns := l.patternProps(s, pointer, hint); len(patterns) > 0 {
		if m.AdditionalProps == nil {
			m.AdditionalProps = &ir.AdditionalProps{Value: l.primRef(ir.PrimAny)}
		}
		m.AdditionalProps.Patterns = patterns
	}
	if isFalseSchema(s.GetUnevaluatedProperties()) {
		m.Additional = ir.AdditionalClosedAfterComposition
	}
}

// patternProps lowers patternProperties into pattern/value bindings in source
// order.
func (l *lowerer) patternProps(s *oas3.Schema, pointer, hint string) []ir.PatternProps {
	pp := s.GetPatternProperties()
	if pp == nil || pp.Len() == 0 {
		return nil
	}
	out := make([]ir.PatternProps, 0, pp.Len())
	for pattern, js := range pp.All() {
		ref := l.schemaRef(js, pointer+ptr("patternProperties", pattern), hint+"_pattern")
		out = append(out, ir.PatternProps{Pattern: pattern, Value: ref})
	}
	return out
}

// fillValidationOnly preserves JSON Schema keywords that have no structural IR
// home verbatim in namespaced Extensions, one info diagnostic each (§4.7). It
// takes the Extensions map rather than a node so it serves both homes a
// declaration's keywords can have: the type node the declaration owns, and the
// declaring property when it owns none.
func (l *lowerer) fillValidationOnly(ext *ir.Extensions, s *oas3.Schema, pointer string) {
	if s.GetNot() != nil {
		l.preserveKeyword(ext, "openapi:not", nodeToRaw(rawPropertyNode(s, "not")), pointer, "not")
	}
	if ite := ifThenElseRaw(s); ite != nil {
		l.preserveKeyword(ext, "openapi:if-then-else", ite, pointer, "if/then/else")
	}
	if ds := s.GetDependentSchemas(); ds != nil && ds.Len() > 0 {
		l.preserveKeyword(ext, "openapi:dependentSchemas",
			nodeToRaw(rawPropertyNode(s, "dependentSchemas")), pointer, "dependentSchemas")
	}
	if craw := containsRaw(s); craw != nil {
		l.preserveKeyword(ext, "openapi:contains", craw, pointer, "contains")
	}
	if u := unevaluatedRaw(s); u != nil {
		l.preserveKeyword(ext, "openapi:unevaluated", u, pointer, "unevaluated")
	}
}

// preserveKeyword records a validation-only keyword's raw payload under key in
// ext and emits one info diagnostic naming it. It is the
// fillValidationOnly-specific caller of preserveRaw.
func (l *lowerer) preserveKeyword(ext *ir.Extensions, key string, raw ir.RawValue, pointer, label string) {
	l.preserveRaw(ext, key, raw, pointer, codeValidationOnlyKeyword,
		fmt.Sprintf("validation-only keyword %q preserved verbatim in extensions", label))
}

// preserveRaw records raw under key in *ext (allocating the map on first
// write) and appends one info diagnostic at pointer with msg. It backs both
// preserveKeyword's validation-only keywords and fillSequential's itemEncoding
// preservation, which use different diagnostic codes — why code is a real
// parameter rather than a constant.
func (l *lowerer) preserveRaw(ext *ir.Extensions, key string, raw ir.RawValue, pointer, code, msg string) {
	if raw == nil {
		return
	}
	if *ext == nil {
		*ext = ir.Extensions{}
	}
	(*ext)[key] = raw
	l.diag(ir.SeverityInfo, code, pointer, "%s", msg)
}

// diag appends one diagnostic at pointer with the given severity and code,
// stamping it with l.srcIndex. It is the single primitive for constructing a
// Provenance from l.srcIndex — lowering sites should use it instead of
// hand-writing the append+diagf+Provenance triple.
func (l *lowerer) diag(sev ir.Severity, code, pointer, format string, args ...any) {
	l.appendDiag(diagf(sev, code, ir.Provenance{Source: l.srcIndex, Pointer: pointer}, format, args...))
}

// appendDiag records d unless one identical to it — same severity, code,
// provenance and message — was already recorded. It is the single append point
// for lowering diagnostics, so a shared declaration reported from N use sites
// yields one diagnostic rather than N indistinguishable copies.
func (l *lowerer) appendDiag(d ir.Diagnostic) {
	key := strings.Join([]string{
		string(d.Severity), d.Code, d.Message,
		strconv.Itoa(d.Provenance.Source), d.Provenance.Pointer, d.Provenance.Inferred,
	}, "\x00") // NUL separates: it cannot occur in a code, pointer, or coerced message
	if l.emitted[key] {
		return
	}
	if l.emitted == nil {
		l.emitted = make(map[string]bool) // a lowerer built field-by-field still de-duplicates
	}
	l.emitted[key] = true
	l.diags = append(l.diags, d)
}

// schemaExtensions lowers a schema's x-* extensions into namespaced Extensions.
func (l *lowerer) schemaExtensions(s *oas3.Schema) ir.Extensions {
	return l.extensions(s.GetExtensions())
}

// lowerArray hoists an array schema as a Tuple when prefixItems is present, else
// a List over its item schema with its collection constraints.
func (l *lowerer) lowerArray(s *oas3.Schema, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		if prefix := s.GetPrefixItems(); len(prefix) > 0 {
			return l.buildTuple(s, common, pointer, hint, prefix)
		}
		list := &ir.List{
			TypeCommon:  common,
			Elem:        l.schemaRef(s.GetItems(), pointer+ptr("items"), hint+"_item"),
			Constraints: listConstraints(s),
		}
		return list
	})
}

// buildTuple lowers prefixItems into a Tuple, preserving any trailing items
// residue raw so the closed/open distinction is not lost.
func (l *lowerer) buildTuple(s *oas3.Schema, common ir.TypeCommon, pointer, hint string, prefix []*oas3.JSONSchema[oas3.Referenceable]) ir.TypeDef {
	elems := make([]ir.TypeRef, 0, len(prefix))
	for i, ps := range prefix {
		elems = append(elems, l.schemaRef(ps, pointer+ptr("prefixItems", strconv.Itoa(i)), hint+"_"+strconv.Itoa(i)))
	}
	t := &ir.Tuple{TypeCommon: common, Elems: elems}
	if s.GetItems() != nil {
		if raw := nodeToRaw(rawPropertyNode(s, "items")); raw != nil {
			t.Extensions = ir.Extensions{"openapi:items-after-prefix": raw}
		}
	}
	return t
}

// scalarTypeID maps a scalar (type, format) pair to a TypeID via formatTable:
// a known pairing interns the shared primitive; byte and unknown formats hoist
// a named Scalar wrapping the base primitive with an Encoding.
func (l *lowerer) scalarTypeID(s *oas3.Schema, st oas3.SchemaType, pointer, hint string) ir.TypeID {
	format := s.GetFormat()
	if st == oas3.SchemaTypeString && format == "byte" {
		return l.hoistByteScalar(pointer, hint)
	}
	key := string(st)
	if format != "" {
		key += "/" + format
	}
	if prim, ok := formatTable[key]; ok {
		return l.primID(prim)
	}
	return l.hoistFormatScalar(baseForType(st), format, pointer, hint)
}

// hoistByteScalar hoists a base64-encoded byte scalar (string+byte).
func (l *lowerer) hoistByteScalar(pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := l.primRef(ir.PrimBytes)
		wire := l.primRef(ir.PrimString)
		return &ir.Scalar{
			TypeCommon: common,
			Base:       &base,
			Encoding:   &ir.Encoding{Name: "base64", WireType: &wire},
		}
	})
}

// hoistFormatScalar hoists a scalar over base carrying an unknown format as its
// encoding name, preserving the format losslessly.
func (l *lowerer) hoistFormatScalar(base ir.PrimKind, format, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		baseRef := l.primRef(base)
		return &ir.Scalar{
			TypeCommon: common,
			Base:       &baseRef,
			Encoding:   &ir.Encoding{Name: format},
		}
	})
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

// schemaHasNull reports whether a schema admits null via either dialect: 3.0
// nullable: true or a 3.1 type array containing "null".
func schemaHasNull(s *oas3.Schema) bool {
	if s.Nullable != nil && *s.Nullable {
		return true
	}
	return slices.Contains(s.GetType(), oas3.SchemaTypeNull)
}

// schemaAdmitsNull reports whether a schema admits null in any spelling: the two
// keyword dialects (schemaHasNull) or a oneOf/anyOf null branch. Lowering lifts
// all of them onto the enclosing TypeRef rather than into the type node, so this
// is the one predicate every site computing a Nullable bit goes through — a
// definition site, a union, and a $ref use site must never disagree about the
// same schema.
//
// A null branch counts only when the union is the type itself. Structural
// siblings intersect with the union (JSON Schema conjoins keywords), so
// `{type: object, oneOf: [{type: string}, {type: null}]}` admits neither string
// nor null; that union is preserved verbatim under Extensions instead.
func schemaAdmitsNull(s *oas3.Schema) bool {
	if schemaHasNull(s) {
		return true
	}
	return oneOfAnyOfHasNull(s) && !hasUnionSiblings(s)
}

// nullUnionCollapse detects a oneOf/anyOf that has exactly one non-null branch
// alongside one or more `type: null` branches and returns that branch's schema,
// pointer, and hint so it lowers as nullable X rather than a union node
// (ir-design §3.3). A set with two or more non-null branches falls through to a
// Union (with its null branches stripped and lifted onto the enclosing ref).
func nullUnionCollapse(s *oas3.Schema, pointer, hint string) (*oas3.JSONSchema[oas3.Referenceable], string, string, bool) {
	variants, key := s.GetOneOf(), "oneOf"
	if len(variants) == 0 {
		variants, key = s.GetAnyOf(), "anyOf"
	}
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
	return nonNull, pointer + ptr(key, strconv.Itoa(nonNullIdx)), hint, true
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

// nodeToRaw converts a YAML node to canonical JSON for lossless preservation in
// Extensions; a nil node or an unconvertible node yields nil.
func nodeToRaw(node *yaml.Node) ir.RawValue {
	if node == nil {
		return nil
	}
	var v any
	if err := node.Decode(&v); err != nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return ir.RawValue(data)
}

// effectiveDescription picks the description from the $ref use-site when present,
// else from the referent.
func effectiveDescription(ref, tgt *oas3.Schema) string {
	if ref != nil && ref.Description != nil {
		return *ref.Description
	}
	if tgt != nil && tgt.Description != nil {
		return *tgt.Description
	}
	return ""
}

// effectiveDeprecated reports the deprecated flag, use-site over referent.
func effectiveDeprecated(ref, tgt *oas3.Schema) bool {
	return pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.Deprecated })
}

// effectiveVisibility maps readOnly/writeOnly to a lifecycle visibility set
// (ir-design §5.2): readOnly is present in every response lifecycle
// (read/delete/query) and absent only from requests; writeOnly is create+update.
func effectiveVisibility(ref, tgt *oas3.Schema) ir.Visibility {
	switch {
	case pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.ReadOnly }):
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}
	case pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.WriteOnly }):
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}
	default:
		return ir.Visibility{}
	}
}

// pickFlag returns the bool field extracted by accessor from ref when present,
// else from tgt, else false. It is the single nil-safe "use-site overrides
// referent" primitive for boolean schema flags (readOnly, writeOnly, deprecated).
func pickFlag(ref, tgt *oas3.Schema, accessor func(*oas3.Schema) *bool) bool {
	if ref != nil {
		if v := accessor(ref); v != nil {
			return *v
		}
	}
	if tgt != nil {
		if v := accessor(tgt); v != nil {
			return *v
		}
	}
	return false
}

// fillTypeDocs maps a schema's title, description, and externalDocs onto Docs.
// Every field is assigned, never accumulated: a schema declares at most one
// externalDocs, and a pointer read twice (a sub-schema lowered both in place
// and through a $ref that hoists it) must not end up with two copies of it.
func fillTypeDocs(d *ir.Docs, s *oas3.Schema) {
	if t := s.GetTitle(); t != "" {
		d.Summary = t
	}
	if desc := s.GetDescription(); desc != "" {
		d.Description = desc
	}
	if ed := s.GetExternalDocs(); ed != nil {
		d.ExternalDocs = []ir.Link{{URL: ed.GetURL(), Description: ed.GetDescription()}}
	}
}

// xmlHints maps an OpenAPI XML object onto ir.XMLHints; an attribute flag becomes
// the "attribute" node type. Fields are read directly rather than via the
// library getters, which dereference unset (nil) field pointers.
func xmlHints(x *oas3.XML) *ir.XMLHints {
	if x == nil {
		return nil
	}
	h := &ir.XMLHints{}
	if x.Name != nil {
		h.Name = *x.Name
	}
	if x.Namespace != nil {
		h.Namespace = *x.Namespace
	}
	if x.Prefix != nil {
		h.Prefix = *x.Prefix
	}
	if x.Wrapped != nil {
		h.Wrapped = *x.Wrapped
	}
	if x.Attribute != nil && *x.Attribute {
		h.NodeType = "attribute"
	}
	return h
}

// extensionsFrom lowers an x-* extension map into namespaced ir.Extensions, keys
// prefixed "openapi:" and values serialized to raw JSON.
func extensionsFrom(ext *extensions.Extensions) (ir.Extensions, []ir.Diagnostic) {
	if ext == nil || ext.Len() == 0 {
		return nil, nil
	}
	out := ir.Extensions{}
	var diags []ir.Diagnostic
	for name, node := range ext.All() {
		raw := nodeToRaw(node)
		if raw == nil {
			diags = append(diags, diagf(ir.SeverityWarning, codeDegradedConstruct,
				ir.Provenance{}, "extension %q could not be serialized", name))
			continue
		}
		out["openapi:"+name] = raw
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// extensions lowers ext's x-* extensions into namespaced Extensions, recording
// any serialization-failure diagnostics unconditionally even when the result
// is empty. Every lowering site should call this rather than extensionsFrom
// directly: gating the diagnostic append behind the same "len(ext) > 0" that
// guards the assignment would drop every warning on an object whose
// extensions all failed to serialize — exactly when the result is empty.
func (l *lowerer) extensions(ext *extensions.Extensions) ir.Extensions {
	out, diags := extensionsFrom(ext)
	l.diags = append(l.diags, diags...)
	return out
}

// mergeExtensions overlays src onto dst, allocating dst on first write.
func mergeExtensions(dst, src ir.Extensions) ir.Extensions {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = ir.Extensions{}
	}
	maps.Copy(dst, src)
	return dst
}

// isFalseSchema reports whether js is the boolean `false` schema.
func isFalseSchema(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return js != nil && js.IsBool() && js.GetBool() != nil && !*js.GetBool()
}

// ifThenElseRaw combines the present if/then/else arms into one raw JSON object.
func ifThenElseRaw(s *oas3.Schema) ir.RawValue {
	return jsonObject(presentMembers(s, "if", "then", "else"))
}

// containsRaw combines contains/minContains/maxContains into one raw JSON object.
func containsRaw(s *oas3.Schema) ir.RawValue {
	if s.GetContains() == nil && s.GetMinContains() == nil && s.GetMaxContains() == nil {
		return nil
	}
	return jsonObject(presentMembers(s, "contains", "minContains", "maxContains"))
}

// unevaluatedRaw combines a non-false unevaluatedProperties and any
// unevaluatedItems into one raw JSON object (a false unevaluatedProperties is a
// structural mode, handled in fillAdditional).
func unevaluatedRaw(s *oas3.Schema) ir.RawValue {
	var members []rawMember
	if up := s.GetUnevaluatedProperties(); up != nil && !isFalseSchema(up) {
		if raw := nodeToRaw(rawPropertyNode(s, "unevaluatedProperties")); raw != nil {
			members = append(members, rawMember{key: "unevaluatedProperties", val: raw})
		}
	}
	if s.GetUnevaluatedItems() != nil {
		if raw := nodeToRaw(rawPropertyNode(s, "unevaluatedItems")); raw != nil {
			members = append(members, rawMember{key: "unevaluatedItems", val: raw})
		}
	}
	return jsonObject(members)
}

// presentMembers collects the given keywords that are present on s as raw JSON
// members, preserving the requested order.
func presentMembers(s *oas3.Schema, keys ...string) []rawMember {
	var members []rawMember
	for _, k := range keys {
		if raw := nodeToRaw(rawPropertyNode(s, k)); raw != nil {
			members = append(members, rawMember{key: k, val: raw})
		}
	}
	return members
}

// rawPropertyNode returns the raw YAML value node of a top-level schema keyword,
// or nil when absent. The library's GetPropertyNode resolves Go core field names
// and returns key nodes; this scans the schema's root mapping for the on-wire
// keyword and returns its value node, which is where exact literals live.
func rawPropertyNode(s *oas3.Schema, key string) *yaml.Node {
	if s == nil {
		return nil
	}
	return rawChildNode(s.GetRootNode(), key)
}

// rawMember is one key/raw-JSON pair of a combined validation-only object.
type rawMember struct {
	key string
	val ir.RawValue
}

// jsonObject renders ordered raw members into a JSON object, or nil when empty.
func jsonObject(members []rawMember) ir.RawValue {
	if len(members) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(m.key)
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return ir.RawValue(b.String())
}

// canonicalWords renders name as a neutral lower_snake word sequence: it splits
// on _/-/space and on camel-case and letter/digit boundaries, lowercases, and
// joins with "_". It holds no acronym opinion beyond boundary detection; casing
// policy is a emitter concern.
func canonicalWords(name string) string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		if r == '_' || r == '-' || r == ' ' {
			flush()
			continue
		}
		if len(cur) > 0 && wordBoundary(cur[len(cur)-1], r, runes, i) {
			flush()
		}
		cur = append(cur, r)
	}
	flush()
	return strings.Join(words, "_")
}

// wordBoundary reports whether a new word starts at runes[i] given the previous
// accumulated rune prev.
func wordBoundary(prev, r rune, runes []rune, i int) bool {
	switch {
	case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
		return true // lower/digit -> Upper: "userID" -> user|ID
	case unicode.IsUpper(prev) && unicode.IsUpper(r) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
		return true // acronym tail: "HTTPServer" -> HTTP|Server
	case unicode.IsLetter(prev) && unicode.IsDigit(r), unicode.IsDigit(prev) && unicode.IsLetter(r):
		return true // letter<->digit: "APIKey2" -> ...Key|2
	default:
		return false
	}
}
