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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Animal:
      type: object
      properties:
        name: {type: string}
    Dog:
      allOf:
        - {$ref: "#/components/schemas/Animal"}
        - type: object
          properties:
            bark: {type: string}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	dog, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Dog")].(*ir.Model)
	require.True(t, ok, "Dog should be a model")
	require.NotNil(t, dog.Base, "sole $ref becomes Base")
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Animal"), dog.Base.Target)
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Forkish:
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
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Forkish")].(*ir.Model)
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Tokenish:
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
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Tokenish")].(*ir.Model)
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Clashish:
      allOf:
        - type: object
          properties:
            id: {type: integer, description: the first meaning}
        - type: object
          properties:
            id: {type: integer, description: a different meaning}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags) // a description clash is info-level, never an error
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Clashish")].(*ir.Model)
	require.True(t, ok, "Clashish should be a model")
	require.Len(t, m.Properties, 1, "id still reconciles to one property")
	assert.Equal(t, "the first meaning", m.Properties[0].Docs.Description,
		"the first declaration in source order wins the description")

	var sawConflict bool
	for _, d := range diags {
		if d.Code == codeDegradedConstruct && d.Severity == ir.SeverityInfo {
			sawConflict = true
		}
	}
	assert.True(t, sawConflict, "a differing redeclared description is surfaced, not dropped silently")
}

func TestAllOf_ConflictingRedeclaredTypeDiagnosed(t *testing.T) {
	t.Parallel()
	// allOf is an intersection, so a field one branch types `string` and another
	// types `integer` describes an unsatisfiable schema. Reconciliation keeps the
	// first declaration's shape (as before) but must no longer swallow the
	// conflict — it names the field and both branch sites so the author can find
	// and fix them.
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Conflictish:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: integer}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags) // a redeclaration conflict is a warning, not a refusal
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Conflictish")].(*ir.Model)
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Boundish:
      allOf:
        - type: object
          properties:
            code: {type: string, maxLength: 10}
        - type: object
          properties:
            code: {type: string, maxLength: 20}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Boundish")].(*ir.Model)
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Compatish:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - type: object
          required: [id]
          properties:
            id: {type: integer}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Compatish")].(*ir.Model)
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Mixish:
      required: [id]
      properties:
        id: {type: integer}
      allOf:
        - type: object
          properties:
            id: {type: integer, description: the identifier}
            name: {type: string}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Mixish")].(*ir.Model)
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

func TestAllOf_ExtraRefsBecomeMixins(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A:
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
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	c, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/C")].(*ir.Model)
	require.True(t, ok, "C should be a model")
	assert.Nil(t, c.Base, "two non-hierarchy refs, neither sole: no Base")
	require.Len(t, c.Mixins, 2, "both refs become mixins")
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/A"), c.Mixins[0].Target)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/B"), c.Mixins[1].Target)
	require.Len(t, c.Properties, 1)
	assert.Equal(t, "c", c.Properties[0].Name.Source)
}

func TestAllOf_DiscriminatorSubtypeValue(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet:
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
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	pet, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Pet")].(*ir.Model)
	require.True(t, ok, "Pet should be a model")
	require.NotNil(t, pet.Discriminator, "the base carries the discriminator")
	assert.Empty(t, pet.DiscriminatorValue, "the base itself has no wire tag value")

	cat, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Cat")].(*ir.Model)
	require.True(t, ok, "Cat should be a model")
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Pet"), cat.Base.Target)
	assert.Equal(t, "kitty", cat.DiscriminatorValue,
		"mapping key pointing at Cat becomes its wire tag value")

	dog, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Dog")].(*ir.Model)
	require.True(t, ok, "Dog should be a model")
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Pet"), dog.Base.Target)
	assert.Equal(t, "Dog", dog.DiscriminatorValue,
		"a subtype absent from the mapping falls back to its schema name")
}

func TestOneOf_WithDiscriminator(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Cat:
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
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	pet, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Pet")].(*ir.Union)
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    U:
      anyOf:
        - {type: string}
        - {type: integer}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/U")].(*ir.Union)
	require.True(t, ok, "U should be a union")
	assert.False(t, u.Exclusive, "anyOf is non-exclusive")
	require.Len(t, u.Variants, 2)
}

func TestOneOf_NullVariantCollapses(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        p:
          oneOf:
            - {type: string}
            - {type: "null"}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	s, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        p:
          oneOf:
            - {type: string}
            - {type: integer}
            - {type: "null"}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	s := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    E:
      type: string
      enum: [a, b]
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	e, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/E")].(*ir.Enum)
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
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    K:
      const: "fixed"
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	k, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/K")].(*ir.Literal)
	require.True(t, ok, "K should be a literal")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "fixed"}, k.Value)
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
	var sawBad bool
	for _, d := range diags {
		if d.Code == codeUnresolvedRef {
			sawBad = true
		}
	}
	assert.True(t, sawBad, "bad mapping target diagnostic")
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
	var sawUnresolved bool
	for _, d := range diags {
		if d.Code == codeUnresolvedRef {
			sawUnresolved = true
		}
	}
	assert.True(t, sawUnresolved)
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
	var sawDegraded, sawValueWarn bool
	for _, d := range diags {
		if d.Code == codeDegradedConstruct && d.Severity == ir.SeverityInfo {
			sawDegraded = true
		}
		if d.Severity == ir.SeverityWarning {
			sawValueWarn = true
		}
	}
	assert.True(t, sawDegraded, "heterogeneous-enum info diagnostic")
	assert.True(t, sawValueWarn, "unconvertible literal value warning")
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
