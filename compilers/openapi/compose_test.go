package openapi

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestAllOf_SoleRefBecomesBase(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Animal:
      type: object
      properties:
        name: {type: string}
    Dog:
      allOf:
        - {$ref: "#/components/schemas/Animal"}
        - type: object
          properties:
            bark: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	dog, ok := doc.Types[componentID("Dog")].(*ir.Model)
	require.True(t, ok, "Dog should be a model")
	require.NotNil(t, dog.Base, "sole $ref becomes Base")
	assert.Equal(t, componentID("Animal"), dog.Base.Target)
	assert.Empty(t, dog.Mixins, "no extra refs, no mixins")
	require.Len(t, dog.Properties, 1, "only the inline branch's own property")
	assert.Equal(t, "bark", dog.Properties[0].Name.Source)
	assert.Contains(t, dog.Properties[0].Provenance.Pointer, "/allOf/1",
		"merged property provenance points into its allOf branch")
}

func TestAllOf_OverlappingInlineBranchesReconcile(t *testing.T) {
	t.Parallel()
	// Two inline allOf branches redeclare the same fields — the shape GitHub's
	// webhook `forkee` uses (a documented object plus a doc-stripped duplicate
	// that marks some fields required). allOf is an intersection, so each wire
	// name must reconcile to a single property, not append a duplicate.
	spec := componentSpec(`    Forkish:
      allOf:
        - type: object
          properties:
            id: {type: integer, description: the identifier}
            name: {type: string}
        - type: object
          required: [id]
          properties:
            id: {type: integer}
            url: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Forkish")].(*ir.Model)
	require.True(t, ok, "Forkish should be a model")

	byWire := map[string]int{}
	for _, p := range m.Properties {
		byWire[p.WireName]++
	}
	assert.Equal(t, 1, byWire["id"], "id declared in both branches reconciles to one property")
	assert.Equal(t, 1, byWire["name"])
	assert.Equal(t, 1, byWire["url"])
	require.Len(t, m.Properties, 3, "no duplicate properties across overlapping inline branches")

	var id ir.Property
	for _, p := range m.Properties {
		if p.WireName == "id" {
			id = p
		}
	}
	assert.True(t, id.Required, "required in the second branch => required on the merged model (allOf intersection)")
	assert.Equal(t, "the identifier", id.Docs.Description,
		"the richer (documented) declaration defines the reconciled shape")
}

func TestAllOf_ReconcileAccumulatesRicherDetailWhateverTheOrder(t *testing.T) {
	t.Parallel()
	// The bare declaration comes first and the richer one second: reconciliation
	// must still surface every optional detail, so branch order never loses
	// information (the reverse of the forkee documented-first shape).
	spec := componentSpec(`    Tokenish:
      allOf:
        - type: object
          properties:
            token: {type: string}
        - type: object
          required: [token]
          properties:
            token:
              type: string
              format: password
              description: the access token
              default: none
              minLength: 1
              deprecated: true
              examples: [abc]
              xml: {name: tok}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Tokenish")].(*ir.Model)
	require.True(t, ok, "Tokenish should be a model")
	require.Len(t, m.Properties, 1, "token reconciles to a single property")

	tok := m.Properties[0]
	assert.True(t, tok.Required, "required in the richer branch => required on the merged property")
	assert.True(t, tok.Secret, "secrecy (format:password) adopted from the richer branch")
	assert.Equal(t, "the access token", tok.Docs.Description, "description adopted whichever branch carries it")
	require.NotNil(t, tok.Default, "default adopted from the richer branch")
	require.NotNil(t, tok.Constraints, "constraints adopted from the richer branch")
	require.NotNil(t, tok.Constraints.MinLength, "minLength adopted from the richer branch")
	require.NotNil(t, tok.Deprecation, "deprecation adopted from the richer branch")
	require.NotNil(t, tok.XML, "xml hints adopted from the richer branch")
	require.Len(t, tok.Examples, 1, "examples adopted from the richer branch")
}

func TestAllOf_ConflictingRedeclaredDescriptionDiagnosed(t *testing.T) {
	t.Parallel()
	// Two branches describe the same field differently. The first declaration in
	// source order wins the shape, but the dropped description is surfaced as an
	// info diagnostic rather than vanishing silently.
	spec := componentSpec(`    Clashish:
      allOf:
        - type: object
          properties:
            id: {type: integer, description: the first meaning}
        - type: object
          properties:
            id: {type: integer, description: a different meaning}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags) // a description clash is info-level, never an error
	m, ok := doc.Types[componentID("Clashish")].(*ir.Model)
	require.True(t, ok, "Clashish should be a model")
	require.Len(t, m.Properties, 1, "id still reconciles to one property")
	assert.Equal(t, "the first meaning", m.Properties[0].Docs.Description,
		"the first declaration in source order wins the description")
	assert.True(t, hasDiagAt(diags, codeDegradedConstruct, ir.SeverityInfo),
		"a differing redeclared description is surfaced, not dropped silently")
}

func TestAllOf_ConflictingRedeclaredTypeDiagnosed(t *testing.T) {
	t.Parallel()
	// allOf is an intersection, so a field one branch types `string` and another
	// types `integer` describes an unsatisfiable schema. Reconciliation keeps the
	// first declaration's shape (as before) but must no longer swallow the
	// conflict — it names the field and both branch sites so the author can find
	// and fix them.
	spec := componentSpec(`    Conflictish:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags) // a redeclaration conflict is a warning, not a refusal
	m, ok := doc.Types[componentID("Conflictish")].(*ir.Model)
	require.True(t, ok, "Conflictish should be a model")
	require.Len(t, m.Properties, 1, "id still reconciles to one property")
	assert.Equal(t, ir.TypeID("t/prim/string"), m.Properties[0].Type.Target,
		"the first declaration in source order still wins the shape")

	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "the incompatible type redeclaration is diagnosed exactly once")
	d := conflicts[0]
	assert.Equal(t, ir.SeverityWarning, d.Severity, "a usable-but-suspicious model is a warning")
	assert.Contains(t, d.Message, `"id"`, "the diagnostic names the conflicting field")
	assert.Contains(t, d.Message, "allOf/0", "the diagnostic names the first branch site")
	assert.Contains(t, d.Message, "allOf/1", "the diagnostic names the second branch site")
}

func TestAllOf_ConflictingRedeclaredConstraintDiagnosed(t *testing.T) {
	t.Parallel()
	// Same target type, but the two branches pin the same keyword to different
	// values (maxLength 10 vs 20). The chosen winner is arbitrary source order, so
	// the dropped bound is surfaced rather than silently discarded.
	spec := componentSpec(`    Boundish:
      allOf:
        - type: object
          properties:
            code: {type: string, maxLength: 10}
        - type: object
          properties:
            code: {type: string, maxLength: 20}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Boundish")].(*ir.Model)
	require.True(t, ok, "Boundish should be a model")
	require.Len(t, m.Properties, 1, "code reconciles to one property")
	require.NotNil(t, m.Properties[0].Constraints, "the first declaration's constraints are kept")
	require.NotNil(t, m.Properties[0].Constraints.MaxLength)
	assert.Equal(t, int64(10), *m.Properties[0].Constraints.MaxLength,
		"the first declaration in source order still wins the constraint")

	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "the incompatible constraint redeclaration is diagnosed exactly once")
	d := conflicts[0]
	assert.Equal(t, ir.SeverityWarning, d.Severity)
	assert.Contains(t, d.Message, `"code"`, "the diagnostic names the conflicting field")
	assert.Contains(t, d.Message, "allOf/0")
	assert.Contains(t, d.Message, "allOf/1")
}

func TestAllOf_CompatibleRedeclarationStaysSilent(t *testing.T) {
	t.Parallel()
	// The reconcilable case: identical target type, the second branch only adds
	// `required`. This must stay silent — a redeclaration is not by itself a
	// conflict, only an incompatible one is.
	spec := componentSpec(`    Compatish:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - type: object
          required: [id]
          properties:
            id: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Compatish")].(*ir.Model)
	require.True(t, ok, "Compatish should be a model")
	require.Len(t, m.Properties, 1, "id reconciles to one property")
	assert.True(t, m.Properties[0].Required, "required from the second branch is OR-ed in")

	assert.Empty(t, conflictDiags(diags),
		"a compatible redeclaration is not a conflict")
}

func TestAllOf_PropertyAlongsideAllOfReconciles(t *testing.T) {
	t.Parallel()
	// A property declared directly on the allOf schema redeclares a field an inline
	// branch already contributed; the sibling and the branch declaration reconcile
	// into one property instead of colliding on the wire.
	spec := componentSpec(`    Mixish:
      required: [id]
      properties:
        id: {type: integer}
      allOf:
        - type: object
          properties:
            id: {type: integer, description: the identifier}
            name: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Mixish")].(*ir.Model)
	require.True(t, ok, "Mixish should be a model")
	require.Len(t, m.Properties, 2, "id (branch + sibling) reconciles to one; name stays its own")

	var id ir.Property
	for _, p := range m.Properties {
		if p.WireName == "id" {
			id = p
		}
	}
	assert.True(t, id.Required, "required alongside allOf => required on the reconciled property")
	assert.Equal(t, "the identifier", id.Docs.Description,
		"detail from the branch declaration survives reconciliation with the sibling property")
}

// TestAllOf_CompositionRequiredAttaches covers issue #29: allOf is an
// intersection, so a `required` list constrains the whole composed object
// regardless of which branch declares the named property, or whether it is
// paired with a properties map at all. Each case names a wire property that
// must end up Required=true, and none of them should produce an
// unattachable-required diagnostic — every name matches an own property.
func TestAllOf_CompositionRequiredAttaches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string // subtest name, describing the case
		spec  string
		model string // component under test
		wire  string // property expected to end up Required=true
	}{
		{
			name: "required-only branch after the branch declaring the property (issue #29 repro)",
			spec: componentSpec(`    Thing:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - required: [id]
`),
			model: "Thing",
			wire:  "id",
		},
		{
			name: "required-only branch before the branch declaring the property",
			spec: componentSpec(`    ThingB:
      allOf:
        - required: [id]
        - type: object
          properties:
            id: {type: integer}
`),
			model: "ThingB",
			wire:  "id",
		},
		{
			name: "required-only branch, property declared alongside allOf (mirrors allof_required.yaml's Bar)",
			spec: componentSpec(`    Foo:
      type: object
      properties:
        a: {type: string}
    Bar:
      type: object
      properties:
        type: {type: string}
        name: {type: string}
      allOf:
        - {$ref: "#/components/schemas/Foo"}
        - required: [type]
`),
			model: "Bar",
			wire:  "type",
		},
		{
			name: "branch declares properties: {} (empty, non-nil) plus required",
			spec: componentSpec(`    ThingF:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - type: object
          properties: {}
          required: [id]
`),
			model: "ThingF",
			wire:  "id",
		},
		{
			name: "the schema's own required (sibling of allOf, no sibling properties) names a property declared inside a branch",
			spec: componentSpec(`    ThingG:
      required: [id]
      allOf:
        - type: object
          properties:
            id: {type: integer}
`),
			model: "ThingG",
			wire:  "id",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, tc.spec)
			requireNoErrorDiags(t, diags)
			m, ok := doc.Types[componentID(tc.model)].(*ir.Model)
			require.True(t, ok, "%s should be a model", tc.model)
			p, ok := propsByWire(m.Properties)[tc.wire]
			require.True(t, ok, "%s should have a %q property", tc.model, tc.wire)
			assert.True(t, p.Required, "%s.%s should be required via composition-scope required", tc.model, tc.wire)
			assert.False(t, hasDiag(diags, codeUnattachableRequired),
				"every required name matches an own property; no unattachable-required diagnostic expected")
		})
	}
}

// TestAllOf_RequiredOnlyBranchNamingBaseOwnedPropertyDiagnosed mirrors
// openapi-generator's SuperBaby fixture: an allOf composed of a sole $ref
// (becomes Base) plus a required-only branch naming a property that only the
// base declares. ir-design §4.3 stores only a model's own properties — never a
// flattened view across Base/Mixins — so the requirement has no IR home to
// attach to; it must be diagnosed rather than dropped silently or misattached
// to an unrelated property.
func TestAllOf_RequiredOnlyBranchNamingBaseOwnedPropertyDiagnosed(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Human:
      type: object
      properties:
        name: {type: string}
        level: {type: integer}
    SuperBaby:
      allOf:
        - {$ref: "#/components/schemas/Human"}
        - required: [level]
          properties:
            gender: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	m, ok := doc.Types[componentID("SuperBaby")].(*ir.Model)
	require.True(t, ok, "SuperBaby should be a model")
	require.NotNil(t, m.Base, "the sole $ref still becomes Base")
	assert.Equal(t, componentID("Human"), m.Base.Target)

	_, hasLevel := propsByWire(m.Properties)["level"]
	assert.False(t, hasLevel, "level belongs to the base, not to SuperBaby's own properties")
	_, hasGender := propsByWire(m.Properties)["gender"]
	assert.True(t, hasGender, "gender is SuperBaby's own property")

	var unattachable []ir.Diagnostic
	for _, d := range diags {
		if d.Code == codeUnattachableRequired {
			unattachable = append(unattachable, d)
		}
	}
	require.Len(t, unattachable, 1, "exactly one unattachable-required diagnostic")
	assert.Equal(t, ir.SeverityWarning, unattachable[0].Severity,
		"SuperBaby composes a ref, so the requirement plausibly belongs to a base property: real fidelity is lost")
	assert.Contains(t, unattachable[0].Message, `"level"`, "the diagnostic names the unattached property")
	assert.Contains(t, unattachable[0].Provenance.Pointer, "/allOf/1",
		"the diagnostic points at the branch that declared the requirement")
}

// TestAllOf_RequiredOnlyBranchNoBaseOrMixinDiagnosedInfo covers the other side
// of diagUnattachableRequired's severity split: an allOf with no $ref branch at
// all, so nothing becomes Base or a Mixin. Unlike
// TestAllOf_RequiredOnlyBranchNamingBaseOwnedPropertyDiagnosed, there is no
// base/mixin the missing name could plausibly belong to — the spec just names a
// property nothing declares, which is legal JSON Schema with nothing lost — so
// the diagnostic is info, not warning.
func TestAllOf_RequiredOnlyBranchNoBaseOrMixinDiagnosedInfo(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Thing:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - required: [ghost]
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "Thing should be a model")
	assert.Nil(t, m.Base, "no $ref branch at all: no Base")
	assert.Empty(t, m.Mixins, "no $ref branch at all: no Mixins")

	id, hasID := propsByWire(m.Properties)["id"]
	require.True(t, hasID, "the branch's own id property still lowers")
	assert.False(t, id.Required, "ghost's requiredness never misattaches to id")
	_, hasGhost := propsByWire(m.Properties)["ghost"]
	assert.False(t, hasGhost, "ghost is never invented as a property")

	require.Equal(t, 1, countDiagsAt(diags, codeUnattachableRequired, ir.SeverityInfo),
		"exactly one info-severity unattachable-required diagnostic")
	var unattachable ir.Diagnostic
	for _, d := range diags {
		if d.Code == codeUnattachableRequired {
			unattachable = d
		}
	}
	assert.Contains(t, unattachable.Message, `"ghost"`, "the diagnostic names the unattached property")
	assert.Contains(t, unattachable.Provenance.Pointer, "/allOf/1",
		"the diagnostic points at the branch that declared the requirement")
}

// TestAllOf_RefBranchWithSiblingRequired31 pins the real behavior of a $ref
// allOf branch that also carries a sibling `required` (legal in OpenAPI 3.1 /
// JSON Schema 2020-12, where $ref no longer has to be the schema object's only
// keyword). compositionRequired reads a branch's required list off its own
// local schema node (GetSchema()) — never the resolved $ref target, which
// belongs to that target's own model — so this pins what that local schema
// node actually exposes for a $ref-with-siblings branch in this library.
func TestAllOf_RefBranchWithSiblingRequired31(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Human:
      type: object
      properties:
        name: {type: string}
    SuperBoyLike:
      allOf:
        - $ref: "#/components/schemas/Human"
          required: [level]
        - type: object
          properties:
            level: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("SuperBoyLike")].(*ir.Model)
	require.True(t, ok, "SuperBoyLike should be a model")
	require.NotNil(t, m.Base, "the sole $ref still becomes Base despite the sibling required")

	level, ok := propsByWire(m.Properties)["level"]
	require.True(t, ok, "level is declared by the inline branch")
	assert.True(t, level.Required,
		"a required sibling on a $ref branch is read off the branch's own local schema and attaches to level")
}

// TestModel_PlainRequiredUndeclaredPropertyUnaffected is a regression guard:
// fillModelProperties (the plain, non-allOf path) is untouched by this fix. A
// required name matching no declared property on a plain object schema is
// legal JSON Schema (required doesn't imply declaration) and must stay silent,
// exactly as before — this path never reaches applyCompositionRequired.
func TestModel_PlainRequiredUndeclaredPropertyUnaffected(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Plain:
      type: object
      required: [ghost]
      properties:
        id: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Plain")].(*ir.Model)
	require.True(t, ok, "Plain should be a model")
	require.Len(t, m.Properties, 1)
	assert.False(t, hasDiag(diags, codeUnattachableRequired),
		"the plain (non-allOf) path is untouched by this fix; no new diagnostic")
}

func TestAllOf_ExtraRefsBecomeMixins(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    A:
      type: object
      properties:
        a: {type: string}
    B:
      type: object
      properties:
        b: {type: string}
    C:
      allOf:
        - {$ref: "#/components/schemas/A"}
        - {$ref: "#/components/schemas/B"}
        - type: object
          properties:
            c: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	c, ok := doc.Types[componentID("C")].(*ir.Model)
	require.True(t, ok, "C should be a model")
	assert.Nil(t, c.Base, "two non-hierarchy refs, neither sole: no Base")
	require.Len(t, c.Mixins, 2, "both refs become mixins")
	assert.Equal(t, componentID("A"), c.Mixins[0].Target)
	assert.Equal(t, componentID("B"), c.Mixins[1].Target)
	require.Len(t, c.Properties, 1)
	assert.Equal(t, "c", c.Properties[0].Name.Source)
}

func TestAllOf_DiscriminatorSubtypeValue(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Pet:
      type: object
      discriminator:
        propertyName: petType
        mapping:
          kitty: "#/components/schemas/Cat"
      properties:
        petType: {type: string}
    Cat:
      allOf:
        - {$ref: "#/components/schemas/Pet"}
        - type: object
          properties:
            meow: {type: string}
    Dog:
      allOf:
        - {$ref: "#/components/schemas/Pet"}
        - type: object
          properties:
            bark: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok, "Pet should be a model")
	require.NotNil(t, pet.Discriminator, "the base carries the discriminator")
	assert.Empty(t, pet.DiscriminatorValue, "the base itself has no wire tag value")

	cat, ok := doc.Types[componentID("Cat")].(*ir.Model)
	require.True(t, ok, "Cat should be a model")
	assert.Equal(t, componentID("Pet"), cat.Base.Target)
	assert.Equal(t, "kitty", cat.DiscriminatorValue,
		"mapping key pointing at Cat becomes its wire tag value")

	dog, ok := doc.Types[componentID("Dog")].(*ir.Model)
	require.True(t, ok, "Dog should be a model")
	assert.Equal(t, componentID("Pet"), dog.Base.Target)
	assert.Equal(t, "Dog", dog.DiscriminatorValue,
		"a subtype absent from the mapping falls back to its schema name")
}

func TestOneOf_WithDiscriminator(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Cat:
      type: object
      properties:
        petType: {type: string}
    Dog:
      type: object
      properties:
        petType: {type: string}
    Pet:
      oneOf:
        - {$ref: "#/components/schemas/Cat"}
        - {$ref: "#/components/schemas/Dog"}
      discriminator:
        propertyName: petType
        mapping:
          cat: "#/components/schemas/Cat"
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	pet, ok := doc.Types[componentID("Pet")].(*ir.Union)
	require.True(t, ok, "Pet should be a union")
	assert.True(t, pet.Exclusive, "oneOf is exclusive")
	assert.False(t, pet.WireTagged, "OpenAPI oneOf is not wire-tagged")
	require.Len(t, pet.Variants, 2, "both variants present")
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, "petType", pet.Discriminator.PropertyName)
	assert.Equal(t, map[string]ir.TypeID{"cat": "t/openapi/components/schemas/Cat"},
		pet.Discriminator.Mapping)
}

func TestAnyOf_IsNonExclusiveUnion(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    U:
      anyOf:
        - {type: string}
        - {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u, ok := doc.Types[componentID("U")].(*ir.Union)
	require.True(t, ok, "U should be a union")
	assert.False(t, u.Exclusive, "anyOf is non-exclusive")
	require.Len(t, u.Variants, 2)
}

func TestOneOf_NullVariantCollapses(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        p:
          oneOf:
            - {type: string}
            - {type: "null"}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	s, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, s.Properties, 1)
	assert.Equal(t, ir.TypeRef{Target: "t/prim/string", Nullable: true}, s.Properties[0].Type)
	for id, def := range doc.Types {
		_, isUnion := def.(*ir.Union)
		assert.False(t, isUnion, "null-variant oneOf must not produce a union node: %s", id)
	}
}

func TestOneOf_ThreeVariantsWithNullStripsNullLiftsNullable(t *testing.T) {
	t.Parallel()
	// A oneOf with two non-null branches plus a null branch stays a Union of the
	// two non-null variants (the null branch is NOT emitted as an `any` variant),
	// and the enclosing ref becomes Nullable.
	spec := componentSpec(`    S:
      type: object
      properties:
        p:
          oneOf:
            - {type: string}
            - {type: integer}
            - {type: "null"}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	s := doc.Types[componentID("S")].(*ir.Model)
	require.Len(t, s.Properties, 1)
	ref := s.Properties[0].Type
	assert.True(t, ref.Nullable, "the null branch lifts onto the enclosing ref")

	u, ok := doc.Types[ref.Target].(*ir.Union)
	require.True(t, ok, "the non-null branches form a Union")
	require.Len(t, u.Variants, 2, "the null branch is stripped, not emitted as a variant")
	for _, v := range u.Variants {
		assert.NotEqual(t, ir.TypeID("t/prim/any"), v.Type.Target,
			"no variant degraded to any")
	}
}

func TestEnum_StringClosed(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    E:
      type: string
      enum: [a, b]
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	e, ok := doc.Types[componentID("E")].(*ir.Enum)
	require.True(t, ok, "E should be an enum")
	assert.True(t, e.Closed, "JSON Schema enum is closed")
	assert.Equal(t, ir.PrimString, e.ValueType)
	require.Len(t, e.Members, 2)
	assert.Equal(t, "a", e.Members[0].Name.Source)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "a"}, e.Members[0].Value)
	assert.Equal(t, "b", e.Members[1].Name.Source)
}

func TestConst_BecomesLiteral(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    K:
      const: "fixed"
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	k, ok := doc.Types[componentID("K")].(*ir.Literal)
	require.True(t, ok, "K should be a literal")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "fixed"}, k.Value)
}

func TestHoistLiteral_UnconvertibleConstBecomesAny(t *testing.T) {
	t.Parallel()
	// A custom tag is structurally unconvertible (no scalarValue case resolves
	// it), forcing hoistLiteral's fallback. Before the fix this silently
	// produced a Literal asserting the value is null, which the spec never said.
	spec := componentSpec("    K:\n      const: !foo bar\n")
	doc, diags := lowerSpec(t, spec)
	k, ok := doc.Types[componentID("K")].(*ir.Any)
	require.True(t, ok, "an unconvertible const hoists the schemaless top type at its own pointer")
	assert.Equal(t, ir.KindAny, k.Kind())
	for _, td := range doc.Types {
		_, isLiteral := td.(*ir.Literal)
		assert.False(t, isLiteral, "no Literal is produced anywhere; nothing lies about the value being null")
	}
	require.Equal(t, 1, countDiagsAt(diags, codeDegradedConstruct, ir.SeverityWarning),
		"exactly one warning fires for the unconvertible value")
	d, ok := firstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, "/components/schemas/K", d.Provenance.Pointer)
}

func TestEnumAsUnion_UnconvertibleMemberBecomesAny(t *testing.T) {
	t.Parallel()
	// The convertible member ("ok") must still hoist a real Literal; only the
	// genuinely unconvertible member ("!foo bar") falls back to the top type.
	spec := componentSpec("    M:\n      enum: [ok, !foo bar]\n")
	doc, diags := lowerSpec(t, spec)
	u, ok := doc.Types[componentID("M")].(*ir.Union)
	require.True(t, ok, "heterogeneous enum still lowers to a union of literals")
	require.Len(t, u.Variants, 2)

	member0, ok := doc.Types[u.Variants[0].Type.Target].(*ir.Literal)
	require.True(t, ok, "the convertible member still hoists a Literal")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "ok"}, member0.Value)

	member1, ok := doc.Types[u.Variants[1].Type.Target].(*ir.Any)
	require.True(t, ok, "the unconvertible member hoists the schemaless top type, not a lying null Literal")
	assert.Equal(t, ir.KindAny, member1.Kind())

	require.Equal(t, 1, countDiagsAt(diags, codeDegradedConstruct, ir.SeverityWarning),
		"exactly one warning for the unconvertible member, distinct from the heterogeneous-enum info diagnostic")
	d, ok := firstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, "/components/schemas/M/enum/1", d.Provenance.Pointer)
}

func TestEnum_UnquotedDatesStayClosedEnum(t *testing.T) {
	t.Parallel()
	// The exact issue repro: YAML 1.1 resolves an unquoted date to !!timestamp.
	// Before the fix, scalarValue had no case for it, so both enum members
	// failed to convert, enumMembers bailed to enumAsUnion, and valueOrNull
	// turned every member into a null literal — the actual dates never
	// survived. A component-level default (as in the repro) is never lowered
	// onto anything by itself (only properties/params read one), so it
	// contributes no diagnostic either way; see
	// TestProperty_UnquotedDateDefaultPreserved for the default path.
	spec := componentSpec(`    D:
      type: string
      format: date
      default: 2021-01-01
      enum: [2021-01-01, 2022-02-02]
`)
	doc, diags := lowerSpec(t, spec)
	assert.Empty(t, diags, "no diagnostics at all: the dates convert cleanly")
	e, ok := doc.Types[componentID("D")].(*ir.Enum)
	require.True(t, ok, "D stays a closed Enum, never degrades to a Union of literals")
	assert.True(t, e.Closed)
	assert.Equal(t, ir.PrimString, e.ValueType)
	require.Len(t, e.Members, 2)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, e.Members[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2022-02-02"}, e.Members[1].Value)
}

func TestProperty_UnquotedDateDefaultPreserved(t *testing.T) {
	t.Parallel()
	// The repro's default sits at the component level, which nothing lowers by
	// itself; this covers the path that actually surfaces the bug in practice —
	// a date default declared on an object property.
	spec := componentSpec(`    S:
      type: object
      properties:
        d:
          type: string
          format: date
          default: 2021-01-01
`)
	doc, diags := lowerSpec(t, spec)
	assert.Empty(t, diags, "no diagnostics at all: the date default converts cleanly")
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	require.NotNil(t, m.Properties[0].Default)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *m.Properties[0].Default)
}

func TestAllOf_DiscriminatorHierarchy(t *testing.T) {
	t.Parallel()
	spec := componentSpecVer("3.2.0", `    Pet:
      type: object
      discriminator:
        propertyName: petType
        mapping:
          cat: '#/components/schemas/Cat'
        defaultMapping: '#/components/schemas/Dog'
      properties: {petType: {type: string}}
    Extra:
      type: object
      properties: {x: {type: string}}
    Cat:
      allOf:
        - {$ref: '#/components/schemas/Pet'}
        - {type: object, properties: {meow: {type: boolean}}}
    Dog:
      allOf:
        - {$ref: '#/components/schemas/Pet'}
        - {$ref: '#/components/schemas/Extra'}
        - {type: object, properties: {bark: {type: boolean}}}
`)
	doc, _ := lowerSpec(t, spec)

	pet := typeByName(doc, "Pet").(*ir.Model)
	require.NotNil(t, pet.Discriminator)
	assert.NotEmpty(t, pet.Discriminator.Property, "declared petType resolves to a PropID")
	assert.NotEmpty(t, pet.Discriminator.Mapping)
	assert.NotEmpty(t, pet.Discriminator.Default, "defaultMapping resolved")

	cat := typeByName(doc, "Cat").(*ir.Model)
	require.NotNil(t, cat.Base)
	assert.Equal(t, "cat", cat.DiscriminatorValue, "mapping key wins")

	dog := typeByName(doc, "Dog").(*ir.Model)
	require.NotNil(t, dog.Base, "the discriminator-anchoring ref is the base")
	require.Len(t, dog.Mixins, 1, "the second ref becomes a mixin")
	assert.Equal(t, "Dog", dog.DiscriminatorValue, "falls back to schema name")
}

func TestModelDiscriminator_UndeclaredPropertyAndBadMapping(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Vehicle:
      type: object
      discriminator:
        propertyName: kind
        mapping:
          car: '#'
      properties: {name: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	v := typeByName(doc, "Vehicle").(*ir.Model)
	require.NotNil(t, v.Discriminator)
	assert.Empty(t, v.Discriminator.Property, "undeclared property")
	assert.Equal(t, "kind", v.Discriminator.PropertyName)
	assert.True(t, hasDiag(diags, codeUnresolvedRef), "bad mapping target diagnostic")
}

func TestOneOf_DiscriminatorWithDefault(t *testing.T) {
	t.Parallel()
	spec := componentSpecVer("3.2.0", `    Shape:
      oneOf:
        - {$ref: '#/components/schemas/Circle'}
        - {$ref: '#/components/schemas/Square'}
      discriminator:
        propertyName: shapeType
        mapping: {circle: '#/components/schemas/Circle'}
        defaultMapping: '#/components/schemas/Square'
    Circle: {type: object, properties: {r: {type: number}}}
    Square: {type: object, properties: {s: {type: number}}}
`)
	doc, _ := lowerSpec(t, spec)
	u := typeByName(doc, "Shape").(*ir.Union)
	require.NotNil(t, u.Discriminator)
	assert.Equal(t, "shapeType", u.Discriminator.PropertyName)
	assert.NotEmpty(t, u.Discriminator.Default)
}

func TestAnyOf_ThreeVariantsWithNull(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    N:
      anyOf:
        - {type: string}
        - {type: integer}
        - {type: 'null'}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u := typeByName(doc, "N").(*ir.Union)
	assert.Len(t, u.Variants, 2, "null branch stripped from variants")
	assert.False(t, u.Exclusive, "anyOf is not exclusive")
}

func TestUnion_VariantHints(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    U:
      oneOf:
        - {$ref: '#/components/schemas/Named', description: sibling}
        - {type: string}
    Named: {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u := typeByName(doc, "U").(*ir.Union)
	require.Len(t, u.Variants, 2)
	hints := []string{u.Variants[0].Name.Hint, u.Variants[1].Name.Hint}
	assert.Contains(t, hints, "Named", "ref-with-siblings hint from target name")
	assert.Contains(t, hints, "variant_1", "inline branch positional hint")
}

func TestAllOf_UnresolvedRefBranch(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Bad:
      allOf:
        - {$ref: '#'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, hasDiag(diags, codeUnresolvedRef))
}

func TestAllOf_MultiRefWithUnresolvedDoesNotAnchor(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Base:
      type: object
      discriminator: {propertyName: t}
      properties: {t: {type: string}}
    Sub:
      allOf:
        - {$ref: '#/components/schemas/Ghost'}
        - {$ref: '#/components/schemas/Base'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	sub := typeByName(doc, "Sub").(*ir.Model)
	require.NotNil(t, sub.Base, "the discriminator-anchoring Base becomes base despite an unresolved sibling ref")
	assert.Equal(t, "Sub", sub.DiscriminatorValue)
	_ = diags
}

func TestEnum_ValueTypeVariants(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    EInt: {type: integer, enum: [1, 2]}
    ENum: {type: number, enum: [1.5, 2.5]}
    EBool: {type: boolean, enum: [true, false]}
    ENoTypeBool: {enum: [true, false]}
    ENoTypeNum: {enum: [1, 2]}
    ENoTypeStr: {enum: [a, b]}
    EBytes: {enum: [!!binary aGk=]}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	want := map[string]ir.PrimKind{
		"EInt": ir.PrimInteger, "ENum": ir.PrimNumber, "EBool": ir.PrimBool,
		"ENoTypeBool": ir.PrimBool, "ENoTypeNum": ir.PrimNumber,
		"ENoTypeStr": ir.PrimString, "EBytes": ir.PrimString,
	}
	for name, prim := range want {
		e, ok := typeByName(doc, name).(*ir.Enum)
		require.True(t, ok, "%s is an enum", name)
		assert.Equal(t, prim, e.ValueType, "%s value type", name)
	}
}

func TestEnum_HeterogeneousBecomesUnionWithBadValue(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    Mixed:\n      enum: [active, .inf]\n")
	doc, diags := lowerSpec(t, spec)
	u, ok := typeByName(doc, "Mixed").(*ir.Union)
	require.True(t, ok, "heterogeneous enum lowers to a union of literals")
	assert.Len(t, u.Variants, 2)
	assert.True(t, hasDiagAt(diags, codeDegradedConstruct, ir.SeverityInfo),
		"heterogeneous-enum info diagnostic")
	assert.True(t, hasDiagAt(diags, codeDegradedConstruct, ir.SeverityWarning),
		"unconvertible literal value warning")
}

func TestAllOf_ModelWithOwnDiscriminator(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Base:
      allOf:
        - {$ref: '#/components/schemas/Common'}
      discriminator: {propertyName: kind}
      properties: {kind: {type: string}}
    Common: {type: object, properties: {id: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	base := typeByName(doc, "Base").(*ir.Model)
	require.NotNil(t, base.Discriminator, "allOf model may declare its own discriminator")
	assert.NotEmpty(t, base.Discriminator.Property)
}

func TestAllOf_BoolRefBranchHasNoDiscriminator(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    BoolComp: false
    Sub:
      allOf:
        - {$ref: '#/components/schemas/BoolComp'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	sub := typeByName(doc, "Sub").(*ir.Model)
	assert.Empty(t, sub.DiscriminatorValue, "a bool-schema ref target anchors no hierarchy")
	_ = diags
}

// TestAllOf_BoolBranchSkippedInCompositionRequired covers compositionRequired's
// `bs == nil` guard: an allOf branch can itself be a bare boolean schema
// (`true`/`false`), not just a $ref to one (as above) or an inline object. For
// such a branch, b.GetSchema() returns nil — it is the JSONSchema either-value's
// Left half, populated only for an object branch, never for a bool one — so
// without the guard, reading bs.GetRequired() would nil-deref on a real spec
// that composes a bare boolean into an allOf. This pins that the guard makes
// the bool branch inert rather than crashing, while the sibling object branch's
// own property and its own required both still lower normally.
func TestAllOf_BoolBranchSkippedInCompositionRequired(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Thing:
      allOf:
        - type: object
          required: [id]
          properties:
            id: {type: integer}
        - true
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "Thing should be a model")
	assert.Nil(t, m.Base, "a bare boolean branch is never a $ref, so no Base")
	assert.Empty(t, m.Mixins)

	id, hasID := propsByWire(m.Properties)["id"]
	require.True(t, hasID, "the object branch's own property still lowers despite the sibling bool branch")
	assert.True(t, id.Required, "the object branch's own required still attaches to its own property")
	assert.False(t, hasDiag(diags, codeUnattachableRequired),
		"the boolean branch contributes no required names, so nothing goes unattached")
}

func TestEnum_NonScalarAndMidListMismatch(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    ObjEnum:
      enum:
        - {a: 1}
        - {b: 2}
    MidMismatch:
      enum: [alpha, beta, 3]
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	_, objIsUnion := typeByName(doc, "ObjEnum").(*ir.Union)
	assert.True(t, objIsUnion, "object-valued enum degrades to a union")
	_, midIsUnion := typeByName(doc, "MidMismatch").(*ir.Union)
	assert.True(t, midIsUnion, "kind change mid-list degrades to a union")
	_ = diags
}

func TestPropIDByName_NotFound(t *testing.T) {
	t.Parallel()
	m := &ir.Model{Properties: []ir.Property{{ID: "p1", Name: ir.Naming{Source: "a"}}}}
	_, ok := propIDByName(m, "missing")
	assert.False(t, ok)
	id, ok := propIDByName(m, "a")
	assert.True(t, ok)
	assert.Equal(t, ir.PropID("p1"), id)
}

func TestRefLastSegment(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Pet", refLastSegment("#/components/schemas/Pet"))
	assert.Equal(t, "bare", refLastSegment("bare"))
}

func TestMappingTargetID(t *testing.T) {
	t.Parallel()
	l := &lowerer{
		schemas: map[string]bool{"Cat": true, "Dog": true, "A/B": true},
		out:     &ir.Document{Types: ir.TypeRegistry{}},
	}
	// A $ref to a declared component.
	id, ok := l.mappingTargetID("#/components/schemas/Cat")
	require.True(t, ok)
	assert.Equal(t, namedTypeID("/components/schemas/Cat"), id)
	// A bare schema name.
	id, ok = l.mappingTargetID("Dog")
	require.True(t, ok)
	assert.Equal(t, namedTypeID(ptr("components", "schemas", "Dog")), id)
	// A bare name that contains '/' but names an existing schema must resolve, not
	// dangle as a misclassified external $ref (issue #14, f07).
	id, ok = l.mappingTargetID("A/B")
	require.True(t, ok)
	assert.Equal(t, namedTypeID(ptr("components", "schemas", "A/B")), id)
	// An undeclared component and a genuine external ref are dropped, never
	// synthesized into a dangling ID.
	_, ok = l.mappingTargetID("#/components/schemas/Ghost")
	assert.False(t, ok, "undeclared component target dropped")
	_, ok = l.mappingTargetID("a.yaml#/A")
	assert.False(t, ok, "external target dropped")
	// A declared but empty-named component ("") is interned anonymously, so its
	// bare mapping name must resolve to that anon ID, not an unbacked namedTypeID
	// (issue #14, f31).
	l.schemas[""] = true
	id, ok = l.mappingTargetID("")
	require.True(t, ok)
	assert.Equal(t, anonTypeID(ptr("components", "schemas", "")), id)
	assert.NotEqual(t, namedTypeID(ptr("components", "schemas", "")), id)
}

func strptr(s string) *string { return &s }

func TestDiscriminatorDefault_ResolvesDeclaredComponent(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.schemas = map[string]bool{"Cat": true}
	d := &oas3.Discriminator{PropertyName: "kind", DefaultMapping: strptr("Cat")}

	id := l.discriminatorDefault(d, "/components/schemas/Pet")
	assert.Equal(t, namedTypeID("/components/schemas/Cat"), id)
	assert.Empty(t, l.diags, "a resolvable defaultMapping produces no diagnostic")
}

func TestDiscriminatorDefault_DroppedWhenUnresolved(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// "Missing" is neither a declared component nor an internal pointer, so the
	// defaultMapping does not resolve and is dropped with one error diagnostic.
	d := &oas3.Discriminator{PropertyName: "kind", DefaultMapping: strptr("Missing")}

	id := l.discriminatorDefault(d, "/components/schemas/Pet")
	assert.Empty(t, id, "an unresolved defaultMapping yields no target")
	require.Len(t, l.diags, 1)
	assert.Equal(t, codeUnresolvedRef, l.diags[0].Code)
}

func TestDiscriminatorDefault_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	id := l.discriminatorDefault(&oas3.Discriminator{PropertyName: "kind"}, "/components/schemas/Pet")
	assert.Empty(t, id)
	assert.Empty(t, l.diags)
}

// TestOneOf_CoDeclaredCompositionDistributes is the regression from
// reference-learnings §B11: an allOf composition co-declared with a oneOf used
// to survive while the union was dropped to Preserved. Both must survive, and
// the shape that says so is a Union whose every variant carries the composition
// — `Base ∧ (A | B)` written as `(Base ∧ A) | (Base ∧ B)`.
func TestOneOf_CoDeclaredCompositionDistributes(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Base: {type: object, properties: {id: {type: string}}}
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      allOf: [{$ref: '#/components/schemas/Base'}]
      oneOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok, "the union is the schema's value, not a preserved sibling")
	assert.True(t, u.Exclusive, "oneOf stays exclusive")
	require.Len(t, u.Variants, 2)
	assert.Empty(t, u.Preserved, "distribution loses nothing, so nothing is kept verbatim")

	for i, branch := range []string{"A", "B"} {
		v := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.NotNil(t, v.Base, "the enclosing allOf's sole $ref stays Base (ir-design §4.3)")
		assert.Equal(t, componentID("Base"), v.Base.Target)
		require.Len(t, v.Mixins, 1, "the branch joins as a mixin, the composition already having a base")
		assert.Equal(t, componentID(branch), v.Mixins[0].Target)
		assert.Equal(t, branch, u.Variants[i].Name.Hint)
	}
	assert.Equal(t, 1, countDiagsAt(diags, codeCompositionLowering, ir.SeverityInfo),
		"the reshaping is reported once; got %+v", diags)
}

// TestAnyOf_CoDeclaredPropertiesDistributeAsBase covers the other classification
// arm: with no enclosing composition to take Base, the branch takes it, and an
// anyOf stays non-exclusive through distribution.
func TestAnyOf_CoDeclaredPropertiesDistributeAsBase(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      type: object
      required: [kind]
      additionalProperties: false
      properties: {kind: {type: string}}
      anyOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok)
	assert.False(t, u.Exclusive, "anyOf stays non-exclusive")
	require.Len(t, u.Variants, 2)

	v := doc.Types[u.Variants[0].Type.Target].(*ir.Model)
	require.NotNil(t, v.Base, "with nothing composed yet, the branch becomes Base")
	assert.Equal(t, componentID("A"), v.Base.Target)
	assert.Empty(t, v.Mixins)
	require.Len(t, v.Properties, 1, "the body's own properties ride on every variant")
	assert.Equal(t, "kind", v.Properties[0].WireName)
	assert.True(t, v.Properties[0].Required, "so does its required list")
	assert.Equal(t, ir.AdditionalClosed, v.Additional, "and its openness")
}

func TestOneOf_CoDeclaredDistributionUnresolvedBranch(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    A: {type: object, properties: {a: {type: string}}}
    Combo:
      type: object
      properties: {kind: {type: string}}
      oneOf:
        - {$ref: '#/components/schemas/Ghost'}
        - {$ref: '#/components/schemas/A'}
`)
	doc, diags := lowerSpec(t, spec)
	assertHasErrorCode(t, diags, codeUnresolvedRef)

	u := typeByName(doc, "Combo").(*ir.Union)
	require.Len(t, u.Variants, 2, "an unresolved branch still gets a variant to hold the body")
	v := doc.Types[u.Variants[0].Type.Target].(*ir.Model)
	assert.Nil(t, v.Base, "nothing resolved to compose with")
	require.Len(t, v.Properties, 1, "the body still rides on the variant")
}

// TestOneOf_CoDeclaredNotDistributedReasons pins the three shapes distribution
// declines, each kept verbatim under ReasonDegradedLowering with its own reason
// (ir-design §4.8). Half-distributing any of them would leave a Union whose
// variants disagree about whether they carry the body.
func TestOneOf_CoDeclaredNotDistributedReasons(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    A: {type: object, properties: {a: {type: string}}}
    InlineBranch:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {type: string}]
    NullBranch:
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {type: 'null'}]
    BothCombinators:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}]
      anyOf: [{$ref: '#/components/schemas/A'}]
    Discriminated:
      type: object
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind}
      oneOf: [{$ref: '#/components/schemas/A'}]
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	for _, name := range []string{"InlineBranch", "NullBranch", "BothCombinators", "Discriminated"} {
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s keeps its structural body", name)
		entry, ok := m.Preserved["openapi:oneOf"]
		require.True(t, ok, "%s keeps its union verbatim", name)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, "%s", name)
	}
	both := typeByName(doc, "BothCombinators").(*ir.Model)
	_, ok := both.Preserved["openapi:anyOf"]
	assert.True(t, ok, "the second combinator is kept too, which is why neither is distributed")
	nullBranch := typeByName(doc, "NullBranch").(*ir.Model)
	assert.Contains(t, string(nullBranch.Preserved["openapi:oneOf"].Value), `"null"`,
		"a null branch is written inline, so it blocks distribution rather than lifting to Nullable")
	assert.Equal(t, 4, countDiagsAt(diags, codeDegradedConstruct, ir.SeverityInfo),
		"each declined shape is reported once; got %+v", diags)
}
