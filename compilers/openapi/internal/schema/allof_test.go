package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// TestAllOf_NestedLowersAsFlat holds the claim of the flattening itself: a
// nested spelling of a composition lowers as its flat one does. It compares the
// composed models with pointers, provenance and hoisted IDs projected away —
// base, mixins, properties in order with their type and required flag, the
// required names held for no property, openness, discriminator value, variants —
// and the codes reported. The per-case rows of
// TestAllOf_NestedInlineWrapperFlattens each pin one consequence; this holds the
// property for every reader of the list.
func TestAllOf_NestedLowersAsFlat(t *testing.T) {
	t.Parallel()
	for _, tc := range equivalencePairs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			nested, nestedCodes := compileView(t, tc.nested)
			flat, flatCodes := compileView(t, tc.flat)
			assert.NotEqual(t, composedView{}, flat, "the flat spelling lowers to something to compare")
			assert.Empty(t, cmp.Diff(flat, nested), "the nested spelling composes as the flat one does")
			assert.Equal(t, flatCodes, nestedCodes, "and reports the same codes")
		})
	}
}

// TestAllOf_NestedLowersAsFlat_ReachesWhatItCompares pins that the projection
// sees the distinctions the pairs rely on, so a pair that agrees does so for a
// reason: a distributed union has variants, the discriminated subtype has its
// tag, a false branch closes the model, and a required name the base declares
// is held in Requires. The residue and the false branch are also kept, which
// only a positive Kept shows.
func TestAllOf_NestedLowersAsFlat_ReachesWhatItCompares(t *testing.T) {
	t.Parallel()
	byName := map[string]string{}
	for _, tc := range equivalencePairs {
		byName[tc.name] = tc.flat
	}
	distributed, _ := compileView(t, byName["a distributed oneOf beside the wrappers"])
	require.Len(t, distributed.Variants, 2, "the oneOf distributes")
	assert.Equal(t, []string{"w"}, wires(distributed.Variants[0].Props), "each variant carries the body's own property")
	assert.Equal(t, string(componentID("C")), distributed.Variants[0].Base, "and the ref the wrapper held as its base")

	closed, _ := compileView(t, byName["a false branch"])
	assert.Equal(t, ir.AdditionalClosed, closed.Additional)
	assert.Positive(t, closed.Kept, "the false branch is kept beside closing the model")

	required, _ := compileView(t, byName["a required name only the base declares"])
	assert.Equal(t, []string{"a"}, required.Requires, "the name is required of the composition, not of a property it holds")

	residue, _ := compileView(t, byName["a residue description"])
	assert.Positive(t, residue.Kept, "the residue is kept")

	subtype, _ := compileView(t, byName["a discriminated base inside a wrapper"])
	assert.Equal(t, "dog", subtype.DiscValue)
	assert.Equal(t, componentID("Pet"), ir.TypeID(subtype.Base), "the discriminated ref is the base")
}

func TestAllOf_SoleRefBecomesBase(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Animal:
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
	openapitest.RequireNoErrorDiags(t, diags)
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
	spec := openapitest.ComponentSpec(`    Forkish:
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
	openapitest.RequireNoErrorDiags(t, diags)
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
	spec := openapitest.ComponentSpec(`    Tokenish:
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
	openapitest.RequireNoErrorDiags(t, diags)
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

// TestAllOf_RedeclarationOrsSecret pins that a redaction one branch writes
// survives the fold in both declaration orders, and that it costs the fold
// nothing else: `format: password` names no shape, so both declarations share
// the bare string's target and every detail folds rather than the losing one
// being dropped whole.
func TestAllOf_RedeclarationOrsSecret(t *testing.T) {
	t.Parallel()
	secret := "            token: {type: string, format: password}\n"
	bounded := "            token: {type: string, minLength: 3, description: the token}\n"
	branch := "        - type: object\n          properties:\n"
	for _, tc := range []struct{ name, first, second string }{
		{"password first", secret, bounded},
		{"password second", bounded, secret},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.ComponentSpec("    Tokenish:\n      allOf:\n" +
				branch + tc.first + branch + tc.second)
			doc, diags := lowerSpec(t, spec)
			assert.Empty(t, diags, "nothing conflicts")
			m, ok := doc.Types[componentID("Tokenish")].(*ir.Model)
			require.True(t, ok, "Tokenish should be a model")
			require.Len(t, m.Properties, 1, "token reconciles to a single property")
			tok := m.Properties[0]
			assert.True(t, tok.Secret, "either branch's redaction makes the merged field secret")
			assert.Equal(t, ir.TypeID("t/prim/string"), tok.Type.Target, "both branches share the target")
			require.NotNil(t, tok.Constraints, "the other branch's bound is folded, not dropped")
			assert.Equal(t, new(int64(3)), tok.Constraints.MinLength)
			assert.Equal(t, "the token", tok.Docs.Description)
			assert.Empty(t, tok.Unmodeled, "no declaration was kept as a loser")
		})
	}
}

func TestAllOf_ConflictingRedeclaredDescriptionDiagnosed(t *testing.T) {
	t.Parallel()
	// Two branches describe the same field differently. The first declaration in
	// source order wins the shape, but the dropped description is surfaced as an
	// info diagnostic rather than vanishing silently.
	spec := openapitest.ComponentSpec(`    Clashish:
      allOf:
        - type: object
          properties:
            id: {type: integer, description: the first meaning}
        - type: object
          properties:
            id: {type: integer, description: a different meaning}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags) // a description clash is info-level, never an error
	m, ok := doc.Types[componentID("Clashish")].(*ir.Model)
	require.True(t, ok, "Clashish should be a model")
	require.Len(t, m.Properties, 1, "id still reconciles to one property")
	assert.Equal(t, "the first meaning", m.Properties[0].Docs.Description,
		"the first declaration in source order wins the description")
	assert.True(t, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"a differing redeclared description is surfaced, not dropped silently")
}

func TestAllOf_ConflictingRedeclaredTypeDiagnosedAndKept(t *testing.T) {
	t.Parallel()
	// allOf is an intersection, so a field one branch types `string` and another
	// types `integer` describes an unsatisfiable schema. Reconciliation keeps the
	// first declaration's shape (as before) but must no longer swallow the
	// conflict — it names the field and both branch sites so the author can find
	// and fix them.
	spec := openapitest.ComponentSpec(`    Conflictish:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags) // a redeclaration conflict is a warning, not a refusal
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

	// A diagnostic is not part of the document, so the losing declaration is
	// also kept where a consumer reading the IR will reach it (GitHub #424).
	lost, ok := m.Properties[0].Unmodeled["openapi:conflicting-redeclaration"+
		"/components/schemas/Conflictish/allOf/1/properties/id"]
	require.True(t, ok,
		"the discarded type is kept beside the winner; got %v", m.Properties[0].Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, lost.Reason)
	assert.JSONEq(t, `{"type":"integer"}`, string(lost.Value),
		"and it is the declaration the second branch wrote, not the one that won")
	assert.Equal(t, jsontext.Pointer("/components/schemas/Conflictish/allOf/1/properties/id"), lost.Provenance.Pointer)
}

// TestAllOf_OverlappingPrimitivesAreReportedNotConflicting pins that two
// formats whose kinds share values draw one info at the redeclaration, instead
// of the unsatisfiable-type warning, keep the first declaration's kind and keep
// the redeclaration, in either branch order.
func TestAllOf_OverlappingPrimitivesAreReportedNotConflicting(t *testing.T) {
	t.Parallel()
	const at = "/components/schemas/Overlapish/allOf/1/properties/n"
	for _, order := range [][2]string{{"int32", "uint32"}, {"uint32", "int32"}} {
		t.Run(order[0]+" first", func(t *testing.T) {
			t.Parallel()
			bounds := [2]int{10, 20}
			if order[0] == "uint32" {
				bounds = [2]int{20, 10}
			}
			spec := openapitest.ComponentSpec(fmt.Sprintf(`    Overlapish:
      allOf:
        - type: object
          properties:
            n: {type: integer, format: %s, minimum: %d}
        - type: object
          properties:
            n: {type: integer, format: %s, minimum: %d}
`, order[0], bounds[0], order[1], bounds[1]))
			doc, diags := lowerSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := doc.Types[componentID("Overlapish")].(*ir.Model)
			require.True(t, ok, "Overlapish should be a model")
			require.Len(t, m.Properties, 1)
			assert.Equal(t, ir.TypeID("t/prim/"+order[0]), m.Properties[0].Type.Target, "the first declaration's kind stays")

			assert.Empty(t, conflictDiags(diags), "overlapping kinds are not an unsatisfiable type")
			assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at),
				"overlap", "the overlap is reported at the redeclaration, as an info")
			var atProperty []ir.Diagnostic
			for _, d := range diags {
				if d.Provenance.Pointer == at {
					atProperty = append(atProperty, d)
				}
			}
			assert.Len(t, atProperty, 1, "exactly one diagnostic at the property; got %v", atProperty)
			assert.Contains(t, m.Properties[0].Unmodeled,
				"openapi:conflicting-redeclaration"+at, "and the redeclaration is kept")
		})
	}
}

// TestAllOf_EnumPairsAreJudgedByKind pins what an enum side draws at the schema
// level, in either order: an enum over a bare kind it equals or narrows wins
// with no diagnostic; a bare kind that admits one of its members overlaps it,
// which is info; a kind that admits none of them warns, as does another family;
// and two enums of one kind, each with members, overlap. A Scalar node of a
// judged kind is judged by member too, and one with no judge refuses nothing.
func TestAllOf_EnumPairsAreJudgedByKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, a, b string
		info, warn bool
	}{
		{"enum against uuid", "{type: string, enum: [a]}", "{type: string, format: uuid}", false, true},
		{"enum against int32", "{type: integer, enum: [3000000000]}", "{type: integer, format: int32}", false, true},
		{"enum against a date it admits", `{type: string, enum: ["2024-01-01"]}`, "{type: string, format: date}", true, false},
		{"enum against a uuid scalar", "{type: string, enum: [a]}", "{type: string, format: uuid, contentMediaType: text/plain}", false, true},
		{"enum against a date scalar it admits", `{type: string, enum: ["2024-01-01"]}`, "{type: string, format: date, contentMediaType: text/plain}", true, false},
		{"empty enum against byte", "{type: string, enum: []}", "{type: string, format: byte}", false, true},
		{"enum against byte", "{type: string, enum: [a]}", "{type: string, format: byte}", true, false},
		{"enum against integer", "{type: string, enum: [a]}", "{type: integer}", false, true},
		{"enum against enum", "{type: string, enum: [a]}", "{type: string, enum: [b]}", true, false},
		{"enum against string", "{type: string, enum: [a]}", "{type: string}", false, false},
	}
	for _, tc := range cases {
		for _, swap := range []bool{false, true} {
			a, b := tc.a, tc.b
			if swap {
				a, b = b, a
			}
			t.Run(fmt.Sprintf("%s swapped=%t", tc.name, swap), func(t *testing.T) {
				t.Parallel()
				_, diags := lowerSpec(t, allOfConflictSpec(a, b))
				openapitest.RequireNoErrorDiags(t, diags)
				assert.Equal(t, tc.info, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
					"the overlap info")
				if tc.warn {
					assert.Len(t, conflictDiags(diags), 1, "one conflict warning")
					return
				}
				assert.Empty(t, conflictDiags(diags), "no conflict")
			})
		}
	}
}

// TestAllOf_ALosingRedeclarationIsKeptAsWritten pins what the entry holds at
// the two property shapes that have no schema node of their own to speak of: a
// `$ref` is kept as the `$ref` the position wrote, not as the target it names,
// and a boolean schema — which has no source node to render — is kept as its value.
func TestAllOf_ALosingRedeclarationIsKeptAsWritten(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Str: {type: string}
    Referred:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - type: object
          properties:
            id: {$ref: '#/components/schemas/Str'}
    Nothing:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - type: object
          properties:
            id: false
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	for _, tc := range []struct{ model, want string }{
		{"Referred", `{"$ref":"#/components/schemas/Str"}`},
		{"Nothing", `false`},
	} {
		m, ok := doc.Types[componentID(tc.model)].(*ir.Model)
		require.True(t, ok, "%s should be a model", tc.model)
		require.Len(t, m.Properties, 1)
		key := "openapi:conflicting-redeclaration/components/schemas/" + tc.model + "/allOf/1/properties/id"
		lost, ok := m.Properties[0].Unmodeled[key]
		require.True(t, ok, "%s keeps its losing declaration; got %v", tc.model, m.Properties[0].Unmodeled)
		assert.JSONEq(t, tc.want, string(lost.Value))
	}
}

func TestAllOf_ConflictingRedeclaredConstraintDiagnosed(t *testing.T) {
	t.Parallel()
	// Same target type, but the two branches pin the same keyword to different
	// values (maxLength 10 vs 20). The chosen winner is arbitrary source order, so
	// the dropped bound is surfaced rather than silently discarded.
	spec := openapitest.ComponentSpec(`    Boundish:
      allOf:
        - type: object
          properties:
            code: {type: string, maxLength: 10}
        - type: object
          properties:
            code: {type: string, maxLength: 20}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
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
	spec := openapitest.ComponentSpec(`    Compatish:
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
	openapitest.RequireNoErrorDiags(t, diags)
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
	spec := openapitest.ComponentSpec(`    Mixish:
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
	openapitest.RequireNoErrorDiags(t, diags)
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
// must end up Required=true, which leaves Requires empty, since every name
// matches an own property.
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
			spec: openapitest.ComponentSpec(`    Thing:
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
			spec: openapitest.ComponentSpec(`    ThingB:
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
			name: "required-only branch, property declared alongside allOf",
			spec: openapitest.ComponentSpec(`    Foo:
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
			spec: openapitest.ComponentSpec(`    ThingF:
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
			spec: openapitest.ComponentSpec(`    ThingG:
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
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := doc.Types[componentID(tc.model)].(*ir.Model)
			require.True(t, ok, "%s should be a model", tc.model)
			p, ok := openapitest.PropsByWire(m.Properties)[tc.wire]
			require.True(t, ok, "%s should have a %q property", tc.model, tc.wire)
			assert.True(t, p.Required, "%s.%s should be required via composition-scope required", tc.model, tc.wire)
			assert.Empty(t, m.Requires, "every required name matches an own property, so none is listed beside them")
		})
	}
}

// TestAllOf_RequiredOnlyBranchNamingBaseOwnedPropertyLandsInRequires pins an
// allOf composed of a sole $ref (its Base) and a required-only branch naming a
// property only the base declares. ir-design §4.3 stores a model's own
// properties, never a flattened view, so the name lands in Model.Requires: the
// composed model makes the inherited property required, and nothing is
// misattached to an unrelated property or reported as lost.
func TestAllOf_RequiredOnlyBranchNamingBaseOwnedPropertyLandsInRequires(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Human:
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

	_, hasLevel := openapitest.PropsByWire(m.Properties)["level"]
	assert.False(t, hasLevel, "level belongs to the base, not to SuperBaby's own properties")
	gender, hasGender := openapitest.PropsByWire(m.Properties)["gender"]
	require.True(t, hasGender, "gender is SuperBaby's own property")
	assert.False(t, gender.Required, "level's requiredness never misattaches to gender")
	assert.Equal(t, []string{"level"}, m.Requires, "SuperBaby makes the level Human declares required")
	assert.Empty(t, diags, "the requirement is carried, so nothing is reported")
}

// TestAllOf_RequiredNameNothingDeclaresLandsInRequires covers an allOf with no
// $ref branch at all, so nothing becomes Base or a Mixin and the required name
// is one no schema declares: legal JSON Schema, which requires the member
// whatever its value. It lands in Model.Requires as the inherited one does,
// with no property invented for it.
func TestAllOf_RequiredNameNothingDeclaresLandsInRequires(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Thing:
      allOf:
        - type: object
          properties:
            id: {type: integer}
        - required: [ghost]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "Thing should be a model")
	assert.Nil(t, m.Base, "no $ref branch at all: no Base")
	assert.Empty(t, m.Mixins, "no $ref branch at all: no Mixins")

	id, hasID := openapitest.PropsByWire(m.Properties)["id"]
	require.True(t, hasID, "the branch's own id property still lowers")
	assert.False(t, id.Required, "ghost's requiredness never misattaches to id")
	_, hasGhost := openapitest.PropsByWire(m.Properties)["ghost"]
	assert.False(t, hasGhost, "ghost is never invented as a property")
	assert.Equal(t, []string{"ghost"}, m.Requires)
	assert.Empty(t, diags, "the requirement is carried, so nothing is reported")
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
	spec := openapitest.ComponentSpec(`    Human:
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
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("SuperBoyLike")].(*ir.Model)
	require.True(t, ok, "SuperBoyLike should be a model")
	require.NotNil(t, m.Base, "the sole $ref still becomes Base despite the sibling required")

	level, ok := openapitest.PropsByWire(m.Properties)["level"]
	require.True(t, ok, "level is declared by the inline branch")
	assert.True(t, level.Required,
		"a required sibling on a $ref branch is read off the branch's own local schema and attaches to level")
}

// TestAllOf_InlineBranchResidueKeptVerbatim covers GitHub #123: an inline allOf
// branch is merged in place, so the merge reads only its properties and its
// required list and everything else it declared used to be dropped without a
// diagnostic. The branch now survives verbatim beside the composed model.
func TestAllOf_InlineBranchResidueKeptVerbatim(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      allOf:
        - type: object
          properties: {a: {type: string}}
          required: [a]
          additionalProperties: false
          not: {type: string}
          minProperties: 2
          description: BranchDoc
          x-vendor: keepme
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok, "S should be a model")

	a, ok := openapitest.PropsByWire(m.Properties)["a"]
	require.True(t, ok, "the merge still contributes the branch's properties")
	assert.True(t, a.Required, "and still ORs the branch's required list onto them")

	entry, ok := m.Unmodeled["openapi:allOf/0"]
	require.True(t, ok, "the branch is kept verbatim; got %+v", m.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/allOf/0"), entry.Provenance.Pointer,
		"the entry locates the branch, not the composed schema")
	for _, kept := range []string{
		`"additionalProperties":false`, `"not":{"type":"string"}`, `"minProperties":2`,
		`"description":"BranchDoc"`, `"x-vendor":"keepme"`,
	} {
		assert.Contains(t, string(entry.Value), kept, "the whole branch is preserved")
	}
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S/allOf/0"),
		"additionalProperties, not, minProperties, description, x-vendor",
		"the diagnostic names every keyword the merge left behind, in source order")
}

// TestAllOf_InlineBranchNonObjectTypeWarns is the worse half of #123: a scalar
// branch declares no properties, so the merge composed an *empty* ir.Model —
// which asserts the value is an object the source says is a string. Preserving
// the branch keeps the source recoverable, and the severity says the IR states
// something wrong rather than merely incomplete.
func TestAllOf_InlineBranchNonObjectTypeWarns(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    T:
      allOf:
        - {type: string, maxLength: 3}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "T").(*ir.Model)
	require.True(t, ok, "the composed node is still a model")
	assert.Empty(t, m.Properties, "a scalar branch declares no properties to merge")

	entry, ok := m.Unmodeled["openapi:allOf/0"]
	require.True(t, ok, "the whole branch is kept verbatim; got %+v", m.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `{"type":"string","maxLength":3}`, string(entry.Value))
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, "/components/schemas/T/allOf/0"),
		"declares a type that is not an object")
	assert.False(t, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"a contradicted model is a warning, not the info a merely narrowed one gets")
}

// TestAllOf_BranchWrittenByReferenceStillDerivesResidue pins the residue against
// the two ways YAML lets a branch be written by reference rather than in place.
// A branch spelled `*anchor` writes no keys of its own and one spelled
// `<<: *anchor` writes only `<<`, so reading the literal text reports either no
// residue at all — merging a branch that declares plenty to an empty model, in
// silence, which is exactly the loss #123 closed for branches written in place —
// or a residue named `<<`, which names nothing a reader can act on.
func TestAllOf_BranchWrittenByReferenceStillDerivesResidue(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Shared: &br {type: string, maxLength: 3, description: Aliased}
    Aliased: {allOf: [*br]}
    Merged: {allOf: [{<<: *br}]}
    Overridden: {allOf: [{<<: *br, type: object, properties: {a: {type: string}}}]}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	cases := []struct {
		name, wantKeys string
		sev            ir.Severity
	}{
		{"Aliased", "type, maxLength, description", ir.SeverityWarning},
		{"Merged", "type, maxLength, description", ir.SeverityWarning},
		{"Overridden", "maxLength, description", ir.SeverityInfo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, ok := typeByName(doc, tc.name).(*ir.Model)
			require.True(t, ok, "%s should be a model", tc.name)

			entry, ok := m.Unmodeled["openapi:allOf/0"]
			require.True(t, ok, "%s keeps the branch verbatim; got %+v", tc.name, m.Unmodeled)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.Contains(t, string(entry.Value), `"maxLength":3`,
				"the payload resolves the reference too, so the branch is recoverable")
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, tc.sev,
					"/components/schemas/"+tc.name+"/allOf/0"),
				"the branch ("+tc.wantKeys+")",
				"the keywords the branch effectively declares are named, not `<<`")
		})
	}
}

// TestAllOf_MergedBranchKeywordsAreNotResidue guards the opposite direction: the
// keywords the merge does consume must not be preserved, or every allOf in the
// corpus would grow an Unmodeled entry restating what the composed model already
// holds. `type: object` counts as consumed because the composed node is a Model
// already. TypedBranch is the shape allof-inline-merge.yaml writes and
// RequiredOnlyBranch is allof-required-only.yaml's last branch; UntypedBranch —
// properties with no `type` at all — is written out here rather than borrowed
// from the corpus, which writes no such branch.
func TestAllOf_MergedBranchKeywordsAreNotResidue(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Base: {type: object, properties: {id: {type: string}}}
    TypedBranch:
      allOf:
        - {$ref: '#/components/schemas/Base'}
        - type: object
          properties: {name: {type: string}}
    UntypedBranch:
      allOf:
        - {$ref: '#/components/schemas/Base'}
        - properties: {name: {type: string}}
    RequiredOnlyBranch:
      allOf:
        - {$ref: '#/components/schemas/Base'}
        - type: object
          properties: {name: {type: string}}
        - required: [name]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"TypedBranch", "UntypedBranch", "RequiredOnlyBranch"} {
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s should be a model", name)
		assert.Empty(t, m.Unmodeled,
			"%s: properties, required and a bare `type: object` are merged, not residue", name)
	}
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"a branch the merge fully consumes announces nothing; got %+v", diags)
}

// TestAllOf_BranchResidueDerivedFromDeclaredKeys pins that the residue is derived
// from the branch's own declared keys rather than from a list of keywords worth
// keeping — a keyword this compiler models nothing for still counts, so a keyword
// a later dialect adds needs no change here to survive. The `[object, "null"]`
// case is the same rule from the other side: `type` is consumed only when it is
// exactly `object`, so the null a set also declares stays recoverable.
func TestAllOf_BranchResidueDerivedFromDeclaredKeys(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Unknown:
      allOf:
        - type: object
          properties: {a: {type: string}}
          futureKeyword: {some: thing}
    NullableBranch:
      allOf:
        - type: [object, 'null']
          properties: {a: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	for name, want := range map[string]string{
		"Unknown":        `"futureKeyword":{"some":"thing"}`,
		"NullableBranch": `"type":["object","null"]`,
	} {
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s should be a model", name)
		require.Len(t, m.Properties, 1, "%s still merges the branch's properties", name)
		entry, ok := m.Unmodeled["openapi:allOf/0"]
		require.True(t, ok, "%s keeps the branch verbatim; got %+v", name, m.Unmodeled)
		assert.Contains(t, string(entry.Value), want, "%s", name)
		assert.Contains(t,
			openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
				"/components/schemas/"+name+"/allOf/0"),
			"kept verbatim under Unmodeled", name)
	}
}

// TestAllOf_EachInlineBranchKeyedSeparately: two branches that both leave residue
// must not overwrite each other's entry, and each is reported at its own branch.
func TestAllOf_EachInlineBranchKeyedSeparately(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Multi:
      allOf:
        - type: object
          properties: {a: {type: string}}
          description: FirstDoc
        - type: object
          properties: {b: {type: string}}
          minProperties: 1
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "Multi").(*ir.Model)
	require.True(t, ok, "Multi should be a model")
	require.Len(t, m.Properties, 2, "both branches still merge their properties")
	require.Len(t, m.Unmodeled, 2, "one entry per branch; got %+v", m.Unmodeled)
	assert.Contains(t, string(m.Unmodeled["openapi:allOf/0"].Value), `"description":"FirstDoc"`)
	assert.Contains(t, string(m.Unmodeled["openapi:allOf/1"].Value), `"minProperties":1`)

	for i, want := range []string{"description", "minProperties"} {
		at := fmt.Sprintf("/components/schemas/Multi/allOf/%d", i)
		assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at), want,
			"branch %d is reported at its own pointer, naming its own residue", i)
	}
}

// TestAllOf_BranchResidueRidesEveryDistributedVariant covers fillAllOf's second
// caller: buildComposedVariant re-runs the same merge once per union variant, so
// the residue has to land on every variant the composition rides on. The
// diagnostic is announced once even so — the branch is one source construct
// however many variants carry it.
func TestAllOf_BranchResidueRidesEveryDistributedVariant(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Base: {type: object, properties: {id: {type: string}}}
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Distributed:
      allOf:
        - {$ref: '#/components/schemas/Base'}
        - type: object
          properties: {c: {type: string}}
          description: BranchDoc
      oneOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "Distributed").(*ir.Union)
	require.True(t, ok, "Distributed distributes into a union")
	require.Len(t, u.Variants, 2)

	for i, v := range u.Variants {
		variant, ok := doc.Types[v.Type.Target].(*ir.Model)
		require.True(t, ok, "variant %d is a composed model", i)
		entry, ok := variant.Unmodeled["openapi:allOf/1"]
		require.True(t, ok, "variant %d carries the branch residue; got %+v", i, variant.Unmodeled)
		assert.Contains(t, string(entry.Value), `"description":"BranchDoc"`, "variant %d", i)
	}
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"one branch, one diagnostic, however many variants carry it; got %+v", diags)
}

// TestAllOf_RequiresIsTheSameInEitherComponentOrder pins that an allOf making
// an inherited property required, and naming one nothing declares, lands both
// names in Model.Requires whichever of the composed model and its base is
// declared first. The composed model has no property of its own to hold either
// name, and the names come from the composition's required lists alone, so
// nothing about the base's declaration may reach them.
func TestAllOf_RequiresIsTheSameInEitherComponentOrder(t *testing.T) {
	t.Parallel()
	const (
		user    = "    User: {type: object, properties: {id: {type: string}}}\n"
		created = `    CreatedUser:
      allOf:
        - {$ref: '#/components/schemas/User'}
        - {required: [id, extra]}
`
	)
	nodes := make([]*ir.Model, 0, 2)
	for _, src := range []string{user + created, created + user} {
		doc, diags := lowerSpec(t, openapitest.ComponentSpec(src))
		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := typeByName(doc, "CreatedUser").(*ir.Model)
		require.True(t, ok, "CreatedUser is a model")
		nodes = append(nodes, m)
	}

	assert.Empty(t, cmp.Diff(nodes[0], nodes[1]), "declaration order must not change the composed model")
	for i, m := range nodes {
		assert.Equal(t, []string{"id", "extra"}, m.Requires, "order %d: in source order, the inherited name included", i)
		assert.Empty(t, m.Properties, "order %d: neither name is declared here, so no property holds one", i)
		require.NotNil(t, m.Base, "order %d", i)
		assert.Equal(t, componentID("User"), m.Base.Target)
	}
}

// TestAllOf_ARequiredNameWrittenInSeveralListsIsListedOnce pins that a name an
// allOf writes in more than one required list, a branch's and the composed
// schema's own, is in Model.Requires once, where it is first written. Each list
// is free of repeats, so the validation reports nothing, and only the lowering
// that gathers them can list a name twice.
func TestAllOf_ARequiredNameWrittenInSeveralListsIsListedOnce(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    User: {type: object, properties: {id: {type: string}}}
    CreatedUser:
      allOf:
        - {$ref: '#/components/schemas/User'}
        - {required: [id, extra]}
        - {required: [extra, other]}
      required: [extra, id]
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, diags, "no list repeats a name, so nothing is reported")

	m, ok := typeByName(doc, "CreatedUser").(*ir.Model)
	require.True(t, ok, "CreatedUser is a model")
	assert.Equal(t, []string{"id", "extra", "other"}, m.Requires, "once each, in the order first written")
}

func TestAllOf_ExtraRefsBecomeMixins(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A:
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
	openapitest.RequireNoErrorDiags(t, diags)
	c, ok := doc.Types[componentID("C")].(*ir.Model)
	require.True(t, ok, "C should be a model")
	assert.Nil(t, c.Base, "two non-hierarchy refs, neither sole: no Base")
	require.Len(t, c.Mixins, 2, "both refs become mixins")
	assert.Equal(t, componentID("A"), c.Mixins[0].Target)
	assert.Equal(t, componentID("B"), c.Mixins[1].Target)
	require.Len(t, c.Properties, 1)
	assert.Equal(t, "c", c.Properties[0].Name.Source)
}

func TestAllOf_DiscriminatorHierarchy(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.2.0", `    Pet:
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

func TestAllOf_UnresolvedRefBranch(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Bad:
      allOf:
        - {$ref: '#'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, openapitest.HasDiag(diags, diag.UnresolvedRef))
}

func TestAllOf_MultiRefWithUnresolvedDoesNotAnchor(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Base:
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

// TestAllOf_BoolBranchSkippedInCompositionRequired covers an allOf branch that
// is a bare boolean schema (`true` here) rather than a $ref or an inline
// object, so b.GetSchema() returns nil for it. It pins that the bool branch
// contributes no required names, while the sibling object branch's property and
// required still lower normally.
func TestAllOf_BoolBranchSkippedInCompositionRequired(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Thing:
      allOf:
        - type: object
          required: [id]
          properties:
            id: {type: integer}
        - true
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Thing")].(*ir.Model)
	require.True(t, ok, "Thing should be a model")
	assert.Nil(t, m.Base, "a bare boolean branch is never a $ref, so no Base")
	assert.Empty(t, m.Mixins)

	id, hasID := openapitest.PropsByWire(m.Properties)["id"]
	require.True(t, hasID, "the object branch's own property still lowers despite the sibling bool branch")
	assert.True(t, id.Required, "the object branch's own required still attaches to its own property")
	assert.Empty(t, m.Requires, "the boolean branch contributes no required names, so nothing goes unattached")
}

// TestAllOf_AnInadmissibleBroadValueIsKeptInEitherOrder pins that the broader
// branch of a narrowing, whichever comes first, never lands a default the
// narrower kind refuses on the merged field: it is kept verbatim at its own
// branch with one info, so the two orders give the same field and the same
// diagnostics.
func TestAllOf_AnInadmissibleBroadValueIsKeptInEitherOrder(t *testing.T) {
	t.Parallel()
	const broad = "            p: {type: number, default: 1.5}\n"
	const narrow = "            p: {type: integer}\n"
	branch := "        - type: object\n          properties:\n"
	for _, tc := range []struct{ name, first, second, broadPtr string }{
		{"broad first", broad, narrow, "/components/schemas/Ranged/allOf/0/properties/p"},
		{"narrow first", narrow, broad, "/components/schemas/Ranged/allOf/1/properties/p"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, openapitest.ComponentSpec("    Ranged:\n      allOf:\n"+
				branch+tc.first+branch+tc.second))
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := doc.Types[componentID("Ranged")].(*ir.Model)
			require.True(t, ok, "Ranged should be a model")
			require.Len(t, m.Properties, 1)

			p := m.Properties[0]
			assert.Equal(t, ir.TypeID("t/prim/integer"), p.Type.Target)
			assert.Nil(t, p.Default, "the refused default does not land")
			require.Len(t, diags, 1, "one info and nothing else")
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
			assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
			assert.Equal(t, jsontext.Pointer(tc.broadPtr), diags[0].Provenance.Pointer)
			entry, ok := p.Unmodeled["openapi:conflicting-redeclaration"+tc.broadPtr]
			require.True(t, ok, "the broad branch is kept at its own pointer; got %v", p.Unmodeled)
			assert.JSONEq(t, `{"type":"number","default":1.5}`, string(entry.Value))
			assert.Len(t, p.Unmodeled, 1)
		})
	}
}

// TestAllOf_ANullOnARefOverTheTopTypeIsKept folds two $refs to a scalar over
// the top type, one writing default: null. The top type admits null, a fact
// only the Base walk reaches, so the default lands and nothing is reported or
// kept aside, in either order.
func TestAllOf_ANullOnARefOverTheTopTypeIsKept(t *testing.T) {
	t.Parallel()
	const withNull = "            p: {$ref: '#/components/schemas/Y', default: null}\n"
	const plain = "            p: {$ref: '#/components/schemas/Y'}\n"
	branch := "        - type: object\n          properties:\n"
	for _, tc := range []struct{ name, first, second string }{
		{"null first", withNull, plain},
		{"null second", plain, withNull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, openapitest.ComponentSpec("    Y: {description: free}\n    Held:\n      allOf:\n"+
				branch+tc.first+branch+tc.second))
			assert.Empty(t, diags, "nothing is refused")
			m, ok := doc.Types[componentID("Held")].(*ir.Model)
			require.True(t, ok, "Held should be a model")
			require.Len(t, m.Properties, 1)
			p := m.Properties[0]
			require.NotNil(t, p.Default, "the null default lands")
			assert.Equal(t, ir.ValueNull, p.Default.Kind)
			assert.Empty(t, p.Unmodeled)
		})
	}
}

// TestAllOf_NonBareNarrowingIsOrderInvariant pins that a redeclaration with a
// side that is not a bare primitive settles on its narrower side whichever
// branch is written first, with no conflict or degradation reported and
// nothing kept.
func TestAllOf_NonBareNarrowingIsOrderInvariant(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, extra, narrow, broad string
		kind                       ir.TypeKind
	}{
		{"an enum over its kind", "", "{type: integer, enum: [1, 2]}", "{type: integer}", ir.KindEnum},
		{"a const over its kind", "", "{const: 1}", "{type: integer}", ir.KindLiteral},
		{"an untyped, described schema", "", "{type: integer}", "{description: the id}", ir.KindPrimitive},
		{"a list of a narrower kind", "", "{type: array, items: {type: integer, format: int32}}",
			"{type: array, items: {type: integer}}", ir.KindList},
		{"a map of a narrower kind", "", "{type: object, additionalProperties: {type: integer, format: int32}}",
			"{type: object, additionalProperties: {type: integer}}", ir.KindMap},
		{"an unknown format over its kind", "", "{type: string, format: hex-token}", "{type: string}", ir.KindScalar},
		{"a ref declared before the schema", "    Id: {type: integer, format: int32}\n",
			"{$ref: '#/components/schemas/Id'}", "{type: integer}", ir.KindScalar},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			types := make([]ir.TypeDef, 0, 2)
			for _, order := range [][2]string{{tc.narrow, tc.broad}, {tc.broad, tc.narrow}} {
				doc, diags := lowerSpec(t, nonBareSpec(tc.extra, order[0], order[1]))
				openapitest.RequireNoErrorDiags(t, diags)
				m, ok := doc.Types[componentID("T")].(*ir.Model)
				require.True(t, ok)
				require.Len(t, m.Properties, 1)
				p := m.Properties[0]
				types = append(types, doc.Types[p.Type.Target])
				assert.Equal(t, tc.kind, doc.Types[p.Type.Target].Kind(), "the narrower declaration's type wins")
				assert.Empty(t, p.Unmodeled, "nothing is kept")
				for _, d := range diags {
					assert.NotContains(t, []string{diag.ConflictingRedecl, diag.DegradedConstruct}, d.Code, d.Message)
				}
			}
			assert.Empty(t, cmp.Diff(types[0], types[1],
				cmpopts.IgnoreFields(ir.TypeCommon{}, "ID", "Name", "Provenance"),
				cmpopts.IgnoreFields(ir.EnumMember{}, "ID")),
				"the winner does not depend on branch order")
		})
	}
}

// TestAllOf_AnUnknownFormatWinsInEitherOrder pins GitHub #782: a format that
// hoists a Scalar over the bare kind narrows it whichever branch is first, and
// the format's own details land on the field rather than being parked.
func TestAllOf_AnUnknownFormatWinsInEitherOrder(t *testing.T) {
	t.Parallel()
	plain := "{type: string}"
	formatted := "{type: string, format: hex-token, minLength: 8, default: x, description: the token}"
	for i, order := range [][2]string{{plain, formatted}, {formatted, plain}} {
		doc, diags := lowerSpec(t, nonBareSpec("", order[0], order[1]))
		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		p := m.Properties[0]

		assert.Equal(t, ir.KindScalar, doc.Types[p.Type.Target].Kind(), "the format's scalar types the field (order %d)", i)
		require.NotNil(t, p.Default, "order %d", i)
		assert.Equal(t, "x", p.Default.Str, "order %d", i)
		require.NotNil(t, p.Constraints, "order %d", i)
		require.NotNil(t, p.Constraints.MinLength, "order %d", i)
		assert.Equal(t, int64(8), *p.Constraints.MinLength, "order %d", i)
		sc, ok := doc.Types[p.Type.Target].(*ir.Scalar)
		require.True(t, ok, "order %d", i)
		assert.Equal(t, "the token", sc.Docs.Description, "the description stays on the scalar that wrote it (order %d)", i)
		assert.Empty(t, p.Unmodeled, "nothing is parked (order %d)", i)
		assert.Empty(t, diags, "order %d", i)
	}
}

// TestAllOf_ReconcileAccumulatesRicherDetailForAnUnknownFormat is
// TestAllOf_ReconcileAccumulatesRicherDetailWhateverTheOrder with a format that
// hoists, in the richer branch.
func TestAllOf_ReconcileAccumulatesRicherDetailForAnUnknownFormat(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Tokenish:
      allOf:
        - type: object
          properties:
            token: {type: string}
        - type: object
          required: [token]
          properties:
            token:
              type: string
              format: hex-token
              description: the access token
              default: none
              minLength: 1
              deprecated: true
              examples: [abc]
              xml: {name: tok}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("Tokenish")].(*ir.Model)
	require.True(t, ok, "Tokenish should be a model")
	require.Len(t, m.Properties, 1, "token reconciles to a single property")

	tok := m.Properties[0]
	assert.Equal(t, ir.KindScalar, doc.Types[tok.Type.Target].Kind(), "the format's scalar types the field")
	assert.True(t, tok.Required)
	sc, ok := doc.Types[tok.Type.Target].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, "the access token", sc.Docs.Description, "the description stays on the scalar that wrote it")
	assert.NotNil(t, sc.Deprecation, "so does the deprecation")
	assert.Len(t, sc.Examples, 1, "and the examples")
	require.NotNil(t, tok.Default)
	require.NotNil(t, tok.Constraints)
	require.NotNil(t, tok.Constraints.MinLength)
	require.NotNil(t, sc.XML)
	assert.Empty(t, tok.Unmodeled)
}

// TestAllOf_TheTopTypeAndANullableKindMeetAsNullableInEitherOrder pins that `{}`
// against `type: [integer, "null"]` is a nullable integer whichever is first.
func TestAllOf_TheTopTypeAndANullableKindMeetAsNullableInEitherOrder(t *testing.T) {
	t.Parallel()
	top, nullable := "{}", `{type: [integer, "null"]}`
	for i, order := range [][2]string{{top, nullable}, {nullable, top}} {
		doc, diags := lowerSpec(t, nonBareSpec("", order[0], order[1]))
		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		p := m.Properties[0]

		assert.Equal(t, ir.TypeRef{Target: "t/prim/integer", Nullable: true}, p.Type, "order %d", i)
		assert.Empty(t, p.Unmodeled, "order %d", i)
		assert.Empty(t, diags, "order %d", i)
	}
}

// TestAllOf_ADefaultTheNarrowerNodeRefusesIsKeptInEitherOrder pins that the
// broader declaration's default is judged against a winner that is not a bare
// primitive: one the enum does not admit is not adopted, and the declaration
// that wrote it is kept at its own pointer, whichever branch it is.
func TestAllOf_ADefaultTheNarrowerNodeRefusesIsKeptInEitherOrder(t *testing.T) {
	t.Parallel()
	enum, broad := "{type: integer, enum: [1, 2]}", "{type: integer, default: 5}"
	for i, tc := range []struct {
		order [2]string
		kept  string
	}{
		{[2]string{enum, broad}, "/components/schemas/T/allOf/1/properties/v"},
		{[2]string{broad, enum}, "/components/schemas/T/allOf/0/properties/v"},
	} {
		doc, diags := lowerSpec(t, nonBareSpec("", tc.order[0], tc.order[1]))
		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		p := m.Properties[0]

		assert.Equal(t, ir.KindEnum, doc.Types[p.Type.Target].Kind(), "the enum wins (order %d)", i)
		assert.Nil(t, p.Default, "the refused default is not adopted (order %d)", i)
		require.Len(t, p.Unmodeled, 1, "order %d", i)
		assert.Contains(t, p.Unmodeled, "openapi:conflicting-redeclaration"+tc.kept, "order %d", i)
		require.Len(t, diags, 1, "order %d", i)
		assert.Equal(t, diag.DegradedConstruct, diags[0].Code, "order %d", i)
		assert.Equal(t, ir.SeverityInfo, diags[0].Severity, "order %d", i)
	}
}

// TestAllOf_AWinnersConstraintsRefuseADefaultInEitherOrder pins that the
// winning node holds the broader declaration's default to its constraints as
// well as its kind: a string past a component's maxLength, or a list past its
// maxItems, is not adopted, and the declaration that wrote it is kept at its
// own pointer, whichever branch it is and wherever the component is declared.
func TestAllOf_AWinnersConstraintsRefuseADefaultInEitherOrder(t *testing.T) {
	t.Parallel()
	named := "{$ref: '#/components/schemas/C'}"
	lowerings := map[string]func(extra, a, b string) (*ir.Document, []ir.Diagnostic){
		"declared before": func(extra, a, b string) (*ir.Document, []ir.Diagnostic) {
			return lowerSpec(t, nonBareSpec(extra, a, b))
		},
		"declared after": func(extra, a, b string) (*ir.Document, []ir.Diagnostic) { return settledSpec(t, extra, a, b) },
	}
	for _, tc := range []struct{ name, component, broad string }{
		{"a string past maxLength", "    C: {type: string, maxLength: 3}\n", "{type: string, default: hello}"},
		{"a list past maxItems", "    C: {type: array, items: {type: integer, format: int32}, maxItems: 2}\n",
			"{type: array, items: {type: integer}, default: [1, 2, 3, 4]}"},
	} {
		for where, lower := range lowerings {
			for _, order := range []struct{ a, b, kept string }{
				{named, tc.broad, "/components/schemas/T/allOf/1/properties/v"},
				{tc.broad, named, "/components/schemas/T/allOf/0/properties/v"},
			} {
				doc, diags := lower(tc.component, order.a, order.b)
				at := tc.name + ", " + where + ", kept " + order.kept
				openapitest.RequireNoErrorDiags(t, diags)
				m, ok := doc.Types[componentID("T")].(*ir.Model)
				require.True(t, ok, at)
				require.Len(t, m.Properties, 1, at)
				p := m.Properties[0]

				assert.Equal(t, componentID("C"), p.Type.Target, "the component wins: %s", at)
				assert.Nil(t, p.Default, "the refused default is not adopted: %s", at)
				require.Len(t, p.Unmodeled, 1, at)
				assert.Contains(t, p.Unmodeled, "openapi:conflicting-redeclaration"+order.kept, at)
				require.Len(t, diags, 1, at)
				assert.Equal(t, diag.DegradedConstruct, diags[0].Code, at)
				assert.Equal(t, jsontext.Pointer(order.kept), diags[0].Provenance.Pointer, at)
			}
		}
	}
}

// TestAllOf_TwoEqualListsRefuseANullTheMergeDoesNotAdmit pins that two lists of
// one element, which are two targets, settle on a non-nullable list, so a null
// default the nullable declaration wrote is not adopted and the declaration is
// kept at its own pointer, whichever branch it is.
func TestAllOf_TwoEqualListsRefuseANullTheMergeDoesNotAdmit(t *testing.T) {
	t.Parallel()
	nullable := `{type: [array, "null"], items: {type: integer}, default: null}`
	plain := "{type: array, items: {type: integer}}"
	for i, tc := range []struct {
		order [2]string
		kept  string
	}{
		{[2]string{nullable, plain}, "/components/schemas/T/allOf/0/properties/v"},
		{[2]string{plain, nullable}, "/components/schemas/T/allOf/1/properties/v"},
	} {
		doc, diags := lowerSpec(t, nonBareSpec("", tc.order[0], tc.order[1]))
		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		p := m.Properties[0]

		require.Equal(t, ir.KindList, doc.Types[p.Type.Target].Kind(), "order %d", i)
		assert.False(t, p.Type.Nullable, "order %d", i)
		assert.Nil(t, p.Default, "the refused null is not adopted (order %d)", i)
		require.Len(t, p.Unmodeled, 1, "order %d", i)
		assert.Contains(t, p.Unmodeled, "openapi:conflicting-redeclaration"+tc.kept, "order %d", i)
		require.Len(t, diags, 1, "order %d", i)
		assert.Equal(t, diag.DegradedConstruct, diags[0].Code, "order %d", i)
		assert.Equal(t, ir.SeverityInfo, diags[0].Severity, "order %d", i)
		assert.Equal(t, jsontext.Pointer(tc.kept), diags[0].Provenance.Pointer, "order %d", i)
	}
}

// TestAllOf_IdenticalNodesAreOneType compiles a field written twice with the
// same enum, const or format, in either order. Two inline copies are one type,
// and a component wins over an inline copy of itself: no diagnostic and nothing
// kept. Two components declared alike are two types the source named apart: an
// info, with the first kept and the second's $ref kept verbatim.
func TestAllOf_IdenticalNodesAreOneType(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{
		"{type: string, enum: [a, b]}",
		"{const: a}",
		"{type: string, format: hex}",
		"{type: string, enum: [a, b], x-foo: 1}",
		"{type: array, items: {type: integer, format: int32}}",
		"{type: object, additionalProperties: {type: integer}}",
	} {
		t.Run(schema, func(t *testing.T) {
			t.Parallel()
			extra := "    P: " + schema + "\n    Q: " + schema + "\n"
			p, q := "{$ref: '#/components/schemas/P'}", "{$ref: '#/components/schemas/Q'}"
			for i, tc := range []struct {
				order [2]string
				win   ir.TypeID // the type that stands
				same  bool
			}{
				{[2]string{schema, schema}, "t/anon/components/schemas/T/allOf/0/properties/v", true},
				{[2]string{p, schema}, componentID("P"), true},
				{[2]string{schema, p}, componentID("P"), true},
				{[2]string{p, q}, componentID("P"), false},
				{[2]string{q, p}, componentID("Q"), false},
			} {
				doc, diags := lowerSpec(t, nonBareSpec(extra, tc.order[0], tc.order[1]))
				m, ok := doc.Types[componentID("T")].(*ir.Model)
				require.True(t, ok, "order %d", i)
				require.Len(t, m.Properties, 1, "order %d", i)
				prop := m.Properties[0]
				assert.Equal(t, tc.win, prop.Type.Target, "order %d", i)
				if tc.same {
					assert.Empty(t, diags, "order %d", i)
					assert.Empty(t, prop.Unmodeled, "order %d", i)
				} else {
					require.Len(t, diags, 1, "order %d", i)
					assert.Equal(t, diag.DegradedConstruct, diags[0].Code, "order %d", i)
					assert.Len(t, prop.Unmodeled, 1, "the second $ref is kept (order %d)", i)
				}
				for _, name := range []string{"P", "Q"} {
					td, ok := doc.Types[componentID(name)]
					require.True(t, ok, "%s (order %d)", name, i)
					for key, e := range td.Common().Unmodeled {
						assert.True(t, strings.HasPrefix(string(e.Provenance.Pointer), "/components/schemas/"+name+"/"),
							"%s keeps where its %s came from, got %q (order %d)", name, key, e.Provenance.Pointer, i)
					}
				}
			}
		})
	}
}

// TestAllOf_ARefToAnInlineNodeIsDeclaredApart compiles a field redeclared as
// a $ref to a node written inline elsewhere (X/properties/c), in either order.
// That node was declared apart from the field, as a component is: it wins over
// an inline copy of itself, and against a $ref to its twin (X/properties/d) the
// pair is an info, with the first kept and the second's $ref kept verbatim.
func TestAllOf_ARefToAnInlineNodeIsDeclaredApart(t *testing.T) {
	t.Parallel()
	const enum = "{type: string, enum: [a, b]}"
	extra := "    X: {type: object, properties: {c: " + enum + ", d: " + enum + "}}\n"
	c, d := "{$ref: '#/components/schemas/X/properties/c'}", "{$ref: '#/components/schemas/X/properties/d'}"
	at := func(p string) ir.TypeID { return ir.TypeID("t/anon/components/schemas/X/properties/" + p) }
	for i, tc := range []struct {
		order [2]string
		win   ir.TypeID
		same  bool
	}{
		{[2]string{c, enum}, at("c"), true},
		{[2]string{enum, c}, at("c"), true},
		{[2]string{c, d}, at("c"), false},
		{[2]string{d, c}, at("d"), false},
	} {
		doc, diags := lowerSpec(t, nonBareSpec(extra, tc.order[0], tc.order[1]))
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok, "order %d", i)
		require.Len(t, m.Properties, 1, "order %d", i)
		prop := m.Properties[0]
		assert.Equal(t, tc.win, prop.Type.Target, "order %d", i)
		if tc.same {
			assert.Empty(t, diags, "order %d", i)
			assert.Empty(t, prop.Unmodeled, "order %d", i)
			continue
		}
		require.Len(t, diags, 1, "order %d", i)
		assert.Equal(t, diag.DegradedConstruct, diags[0].Code, "order %d", i)
		assert.Len(t, prop.Unmodeled, 1, "the second $ref is kept (order %d)", i)
	}
}

// TestAllOf_ACopyANarrowingKeptIsStillACopy compiles a field declared as {},
// an inline enum and a $ref to a component of the same enum, in every order,
// with the component declared before the composing schema and after it.
// Where {} comes first, the inline enum narrows it and the property holds a node
// written at the second declaration, not the first; it is still an inline copy,
// so the component wins in all six orders with nothing reported or kept.
func TestAllOf_ACopyANarrowingKeptIsStillACopy(t *testing.T) {
	t.Parallel()
	const enum = "{type: string, enum: [a, b]}"
	decls := []string{"{}", enum, "{$ref: '#/components/schemas/Color'}"}
	for _, colorLast := range []bool{false, true} {
		for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
			var branches strings.Builder
			for _, i := range order {
				branches.WriteString("        - type: object\n          properties: {v: " + decls[i] + "}\n")
			}
			color, composed := "    Color: "+enum+"\n", "    T:\n      allOf:\n"+branches.String()
			schemas := color + composed
			if colorLast {
				schemas = composed + color
			}
			doc, diags := lowerSpec(t, openapitest.ComponentSpec(schemas))
			m, ok := doc.Types[componentID("T")].(*ir.Model)
			require.True(t, ok, "order %v, Color last %t", order, colorLast)
			require.Len(t, m.Properties, 1, "order %v, Color last %t", order, colorLast)
			assert.Equal(t, componentID("Color"), m.Properties[0].Type.Target, "order %v, Color last %t", order, colorLast)
			assert.Empty(t, diags, "order %v, Color last %t", order, colorLast)
			assert.Empty(t, m.Properties[0].Unmodeled, "order %v, Color last %t", order, colorLast)
		}
	}
}

// TestAllOf_AListOfAComponentWinsOverAListOfItsCopy compiles a field redeclared
// as a list of a component and as a list of an inline copy of it, in either
// order. The copy is written under the field's own declaration, so the list
// holding the component wins in both orders with nothing reported or kept.
func TestAllOf_AListOfAComponentWinsOverAListOfItsCopy(t *testing.T) {
	t.Parallel()
	const enum = "{type: string, enum: [a, b]}"
	ofRef, ofCopy := "{type: array, items: {$ref: '#/components/schemas/Color'}}", "{type: array, items: "+enum+"}"
	for i, order := range [][2]string{{ofRef, ofCopy}, {ofCopy, ofRef}} {
		doc, diags := lowerSpec(t, nonBareSpec("    Color: "+enum+"\n", order[0], order[1]))
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok, "order %d", i)
		require.Len(t, m.Properties, 1, "order %d", i)
		list, ok := doc.Types[m.Properties[0].Type.Target].(*ir.List)
		require.True(t, ok, "the field is a list (order %d)", i)
		assert.Equal(t, componentID("Color"), list.Elem.Target, "order %d", i)
		assert.Empty(t, diags, "order %d", i)
		assert.Empty(t, m.Properties[0].Unmodeled, "order %d", i)
	}
}

// TestAllOf_ARefToASiblingFieldIsDeclaredApart compiles a field v redeclared as
// a $ref to its branch's sibling field vx and as an inline copy of vx, in either
// order. vx's pointer begins with v's but is not under it, so vx was declared
// apart and wins in both orders with nothing reported or kept.
func TestAllOf_ARefToASiblingFieldIsDeclaredApart(t *testing.T) {
	t.Parallel()
	const enum = "{type: string, enum: [a, b]}"
	for i := range 2 {
		vx := fmt.Sprintf("/components/schemas/T/allOf/%d/properties/vx", i)
		branches := [2]string{
			"        - type: object\n          properties: {vx: " + enum + ", v: {$ref: '#" + vx + "'}}\n",
			"        - type: object\n          properties: {v: " + enum + "}\n",
		}
		if i == 1 {
			branches[0], branches[1] = branches[1], branches[0]
		}
		doc, diags := lowerSpec(t, openapitest.ComponentSpec("    T:\n      allOf:\n"+branches[0]+branches[1]))
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok, "order %d", i)
		var v *ir.Property
		for k := range m.Properties {
			if m.Properties[k].WireName == "v" {
				v = &m.Properties[k]
			}
		}
		require.NotNil(t, v, "order %d", i)
		assert.Equal(t, ir.TypeID("t/anon"+vx), v.Type.Target, "order %d", i)
		assert.Empty(t, diags, "order %d", i)
		assert.Empty(t, v.Unmodeled, "order %d", i)
	}
}

// TestAllOf_ARedeclarationIsJudgedOnceTheComponentsHaveLowered pins that a
// field redeclared as two $refs to components declared after the composing
// schema is judged as it is when they are declared before it: two components
// declared alike, or a pair the merge does not narrow, are reported and kept. It
// compiles three arms: the components declared before the schema, and after it
// in either order of the two (#898).
func TestAllOf_ARedeclarationIsJudgedOnceTheComponentsHaveLowered(t *testing.T) {
	t.Parallel()
	p, q := "{$ref: '#/components/schemas/P'}", "{$ref: '#/components/schemas/Q'}"
	for _, tc := range []struct {
		name, pdecl, qdecl string
		items              bool
		kind               ir.TypeKind
		code               string // the one diagnostic, or "" for none
	}{
		{"enums declared alike", "{type: string, enum: [a, b]}", "{type: string, enum: [a, b]}", false, ir.KindEnum, diag.DegradedConstruct},
		{"an integer beside a number", "{type: number}", "{type: integer}", false, ir.KindScalar, diag.DegradedConstruct},
		{"a string against an integer", "{type: string}", "{type: integer}", false, ir.KindScalar, diag.ConflictingRedecl},
		{"enums declared alike as list items", "{type: string, enum: [a, b]}", "{type: string, enum: [a, b]}", true, ir.KindList, diag.DegradedConstruct},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, b := p, q
			if tc.items {
				a, b = "{type: array, items: "+p+"}", "{type: array, items: "+q+"}"
			}
			pq := "    P: " + tc.pdecl + "\n    Q: " + tc.qdecl + "\n"
			qp := "    Q: " + tc.qdecl + "\n    P: " + tc.pdecl + "\n"
			for i, lower := range []func() (*ir.Document, []ir.Diagnostic){
				func() (*ir.Document, []ir.Diagnostic) { return lowerSpec(t, nonBareSpec(pq, a, b)) },
				func() (*ir.Document, []ir.Diagnostic) { return settledSpec(t, pq, a, b) },
				func() (*ir.Document, []ir.Diagnostic) { return settledSpec(t, qp, a, b) },
			} {
				doc, diags := lower()
				m, ok := doc.Types[componentID("T")].(*ir.Model)
				require.True(t, ok, "order %d", i)
				require.Len(t, m.Properties, 1, "order %d", i)
				prop := m.Properties[0]

				assert.Equal(t, tc.kind, doc.Types[prop.Type.Target].Kind(), "order %d", i)
				if tc.code == "" {
					assert.Empty(t, diags, "order %d", i)
					assert.Empty(t, prop.Unmodeled, "order %d", i)
					continue
				}
				require.Len(t, diags, 1, "order %d", i)
				assert.Equal(t, tc.code, diags[0].Code, "order %d", i)
				assert.Len(t, prop.Unmodeled, 1, "the losing declaration is kept (order %d)", i)
			}
		})
	}
}

// TestAllOf_WhatAFieldKeepsOfItsDetailIsOrderFree compiles a field whose two
// declarations give an extension differently, alike in another, one promoting
// a deprecation from an extension, and one default spelled two ways. In either
// branch order the field keeps the first value of the extension and reports
// the other, names the heuristic behind its deprecation, and takes the default
// as the one value it is.
func TestAllOf_WhatAFieldKeepsOfItsDetailIsOrderFree(t *testing.T) {
	t.Parallel()
	a := "{type: number, default: 1, x-foo: 1, x-same: s}"
	b := "{type: number, default: 1.0, x-foo: 2, x-same: s, deprecated: true, x-deprecated-reason: use q}"
	for _, order := range [][2]string{{a, b}, {b, a}} {
		doc, diags := lowerSpec(t, nonBareSpec("", order[0], order[1]))
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		p := m.Properties[0]
		first := "1"
		if order[0] == b {
			first = "2"
		}

		require.Contains(t, p.Unmodeled, "openapi:x-foo", "order %v", order)
		assert.Equal(t, ir.RawValue(first), p.Unmodeled["openapi:x-foo"].Value, "the first value stands")
		assert.Contains(t, p.Unmodeled, "openapi:x-same")
		assert.Contains(t, p.Unmodeled, "openapi:conflicting-redeclaration/components/schemas/T/allOf/1/properties/v",
			"the other declaration is kept verbatim")
		require.NotNil(t, p.Deprecation)
		assert.Equal(t, "use q", p.Deprecation.Message)
		assert.Equal(t, "extension-promotion", p.Provenance.Inferred, "the promoted deprecation stays marked")
		require.NotNil(t, p.Default)
		require.Len(t, diags, 1, "only the extension given differently is reported: %+v", diags)
		assert.Contains(t, diags[0].Message, "openapi:x-foo differently")
	}
}

// TestAllOf_ContainerMembersAreJudgedAsFieldsAre pins that two lists' members
// get the verdict two fields of those types get. Enums sharing no value, one
// list never empty, are a conflict; the same lists both admitting the empty one
// overlap in it. Members judged only as a fallback for top-level fields were
// once left unrelated, which the merge reported as a pair it does not compare.
func TestAllOf_ContainerMembersAreJudgedAsFieldsAre(t *testing.T) {
	t.Parallel()
	str, num := "{type: array, items: {type: string, enum: [a]}%s}", "{type: array, items: {type: integer, enum: [1]}}"
	for _, tc := range []struct {
		name, min string
		code      string
		sev       ir.Severity
	}{
		{"one list never empty", ", minItems: 1", diag.ConflictingRedecl, ir.SeverityWarning},
		{"both lists admitting the empty one", "", diag.DegradedConstruct, ir.SeverityInfo},
	} {
		for _, order := range [][2]string{{fmt.Sprintf(str, tc.min), num}, {num, fmt.Sprintf(str, tc.min)}} {
			_, diags := lowerSpec(t, nonBareSpec("", order[0], order[1]))
			require.Len(t, diags, 1, "%s, order %v: %+v", tc.name, order, diags)
			assert.Equal(t, tc.code, diags[0].Code, "%s, order %v", tc.name, order)
			assert.Equal(t, tc.sev, diags[0].Severity, "%s, order %v", tc.name, order)
			if tc.code == diag.DegradedConstruct {
				assert.Contains(t, diags[0].Message, "whose values overlap", "%s, order %v", tc.name, order)
			}
		}
	}
}

// TestAllOf_NestedInlineWrapperFlattens pins the flattening of
// docs/compilers/openapi.md#allof-classification: a nested spelling classifies
// like the flat one, with every property keeping the pointer it was written at
// (GitHub #599).
func TestAllOf_NestedInlineWrapperFlattens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		m      string
		base   ir.TypeID
		mixins []ir.TypeID
		props  map[string]string // wire name -> provenance pointer
	}{
		{
			name: "the issue: a wrapper around two refs",
			m: `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - {$ref: '#/components/schemas/B'}
`,
			mixins: []ir.TypeID{componentID("A"), componentID("B")},
		},
		{
			name: "two levels of wrapper",
			m: `    M:
      allOf:
        - allOf:
            - allOf:
                - {$ref: '#/components/schemas/A'}
            - {$ref: '#/components/schemas/B'}
`,
			mixins: []ir.TypeID{componentID("A"), componentID("B")},
		},
		{
			name: "a ref with an inline branch beside it carries the nested pointer",
			m: `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - type: object
              properties: {c: {type: string}}
`,
			base:  componentID("A"),
			props: map[string]string{"c": "/components/schemas/M/allOf/0/allOf/1/properties/c"},
		},
		{
			name: "a wrapper with its own properties and type object",
			m: `    M:
      allOf:
        - type: object
          properties: {w: {type: string}}
          allOf:
            - {$ref: '#/components/schemas/A'}
`,
			base:  componentID("A"),
			props: map[string]string{"w": "/components/schemas/M/allOf/0/properties/w"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range nestedOrders(t, tc.m, nestedAB) {
				m, ok := typeByName(doc, "M").(*ir.Model)
				require.True(t, ok, "M should be a model")
				if tc.base == "" {
					assert.Nil(t, m.Base)
				} else if assert.NotNil(t, m.Base) {
					assert.Equal(t, tc.base, m.Base.Target)
				}
				assert.Equal(t, append([]ir.TypeID{}, tc.mixins...), refTargets(m.Mixins))
				assert.Empty(t, m.Unmodeled, "a flattened wrapper is not kept verbatim")
				assert.False(t, slices.ContainsFunc(doc.Diagnostics, func(d ir.Diagnostic) bool {
					return strings.HasPrefix(string(d.Provenance.Pointer), "/components/schemas/M")
				}), "flattening is exact, so nothing is reported under M: %+v", doc.Diagnostics)
				got := map[string]string{}
				for _, p := range m.Properties {
					got[p.WireName] = string(p.Provenance.Pointer)
				}
				want := tc.props
				if want == nil {
					want = map[string]string{}
				}
				assert.Equal(t, want, got)
			}
		})
	}
}

// TestAllOf_NestedWrapperRequiredAttaches pins that a required list inside a
// wrapper is read like one at the top: it attaches to a property a flattened
// branch declares, and a name none declares is listed in Model.Requires.
func TestAllOf_NestedWrapperRequiredAttaches(t *testing.T) {
	t.Parallel()
	t.Run("wrapper required and nested-nested required attach", func(t *testing.T) {
		t.Parallel()
		m := `    M:
      allOf:
        - required: [x]
          allOf:
            - type: object
              properties: {x: {type: integer}, y: {type: integer}}
            - allOf:
                - required: [y]
`
		for _, doc := range nestedOrders(t, m, nestedAB) {
			model, ok := typeByName(doc, "M").(*ir.Model)
			require.True(t, ok, "M should be a model")
			props := openapitest.PropsByWire(model.Properties)
			assert.True(t, props["x"].Required, "the wrapper's required reaches the property it names")
			assert.True(t, props["y"].Required, "so does a required two wrappers deep")
			assert.Empty(t, model.Requires, "every name found its property")
		}
	})
	t.Run("a name no own property declares is listed in Requires", func(t *testing.T) {
		t.Parallel()
		m := `    M:
      allOf:
        - required: [ghost]
          allOf:
            - {$ref: '#/components/schemas/A'}
`
		for _, doc := range nestedOrders(t, m, nestedAB) {
			model, ok := typeByName(doc, "M").(*ir.Model)
			require.True(t, ok, "M should be a model")
			assert.Equal(t, []string{"ghost"}, model.Requires, "the wrapper's required list is read")
		}
	})
}

// TestAllOf_NestedWrapperRequiredAttachesBesideUnion pins that a required two
// wrappers deep reaches its property in every distributed variant.
func TestAllOf_NestedWrapperRequiredAttachesBesideUnion(t *testing.T) {
	t.Parallel()
	u := `    U:
      oneOf: [{$ref: '#/components/schemas/P'}, {$ref: '#/components/schemas/Q'}]
      allOf:
        - allOf:
            - type: object
              properties: {x: {type: string}, y: {type: string}}
            - allOf:
                - required: [y]
`
	const pq = `    P: {type: object, properties: {p: {type: string}}}
    Q: {type: object, properties: {q: {type: string}}}
`
	for _, doc := range nestedOrders(t, u, nestedAB+pq) {
		for _, id := range []ir.TypeID{"t/composed/components/schemas/U/oneOf/0", "t/composed/components/schemas/U/oneOf/1"} {
			model, ok := doc.Types[id].(*ir.Model)
			require.True(t, ok, "%s should be a model", id)
			assert.True(t, openapitest.PropsByWire(model.Properties)["y"].Required, id)
		}
	}
}

// TestAllOf_NestedWrapperBaseSelection pins that the classification of
// docs/compilers/openapi.md#allof-classification runs over the flattened list:
// a discriminated ref inside a wrapper is the Base, whichever nested order it
// is written in, and still answers from its mapping.
func TestAllOf_NestedWrapperBaseSelection(t *testing.T) {
	t.Parallel()
	const pet = `    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {dog: '#/components/schemas/Dog'}
    Named: {type: object, properties: {name: {type: string}}}
`
	tests := []struct {
		name string
		dog  string
	}{
		{"pet first inside the wrapper", `    Dog:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/Pet'}
            - {$ref: '#/components/schemas/Named'}
`},
		{"pet last inside the wrapper", `    Dog:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/Named'}
            - {$ref: '#/components/schemas/Pet'}
`},
		{"pet inside a wrapper beside a top-level ref", `    Dog:
      allOf:
        - {$ref: '#/components/schemas/Named'}
        - allOf:
            - {$ref: '#/components/schemas/Pet'}
`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range nestedOrders(t, tc.dog, pet) {
				dog, ok := typeByName(doc, "Dog").(*ir.Model)
				require.True(t, ok, "Dog should be a model")
				require.NotNil(t, dog.Base)
				assert.Equal(t, componentID("Pet"), dog.Base.Target)
				assert.Equal(t, []ir.TypeID{componentID("Named")}, refTargets(dog.Mixins))
				assert.Equal(t, "dog", dog.DiscriminatorValue, "the mapping key, not the schema name")
			}
		})
	}
}

// TestAllOf_NestedFalseBranchClosesModel pins that a `false` inside a wrapper
// still closes the composition, and is kept at the pointer it was written at.
func TestAllOf_NestedFalseBranchClosesModel(t *testing.T) {
	t.Parallel()
	m := `    M:
      allOf:
        - allOf: [false]
`
	for _, doc := range nestedOrders(t, m, nestedAB) {
		model, ok := typeByName(doc, "M").(*ir.Model)
		require.True(t, ok, "M should be a model")
		assert.Equal(t, ir.AdditionalClosed, model.Additional)
		entry, ok := model.Unmodeled["openapi:allOf/0/allOf/0"]
		require.True(t, ok, "the false is kept under its nested key; got %+v", model.Unmodeled)
		assert.Equal(t, "false", string(entry.Value))
		assert.Equal(t, jsontext.Pointer("/components/schemas/M/allOf/0/allOf/0"), entry.Provenance.Pointer)
		assert.NotContains(t, model.Unmodeled, "openapi:allOf/0", "the wrapper itself is not kept")
		assert.True(t, openapitest.HasDiagCodeAt(doc.Diagnostics, diag.FalseSchema,
			"/components/schemas/M/allOf/0/allOf/0"))
	}
}

// TestAllOf_NestedWrapperWithResidueKeptVerbatim pins that a wrapper writing
// a keyword the merge does not consume is still flattened: its entries and
// properties compose as the flat spelling's do, the whole wrapper is kept under
// its key, and the diagnostic names the extra keyword and not allOf.
func TestAllOf_NestedWrapperWithResidueKeptVerbatim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		sibling string
		residue string
	}{
		{"description", "description: Doc", "description"},
		{"title", "title: Grouped", "title"},
		{"vendor extension", "x-vendor: keepme", "x-vendor"},
		{"readOnly", "readOnly: true", "readOnly"},
		{"discriminator", "discriminator: {propertyName: kind}", "discriminator"},
		{"oneOf", "oneOf: [{type: string}]", "oneOf"},
		{"additionalProperties false", "additionalProperties: false", "additionalProperties"},
		{"type set with null", `type: [object, "null"]`, "type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
          ` + tc.sibling + `
          properties: {w: {type: string}}
`
			for _, doc := range nestedOrders(t, m, nestedAB) {
				model, ok := typeByName(doc, "M").(*ir.Model)
				require.True(t, ok, "M should be a model")
				require.NotNil(t, model.Base, "the wrapper's ref is classified as in the flat spelling")
				assert.Equal(t, componentID("A"), model.Base.Target)
				assert.Empty(t, model.Mixins)
				_, hasW := openapitest.PropsByWire(model.Properties)["w"]
				assert.True(t, hasW, "the wrapper's properties still merge")
				entry, ok := model.Unmodeled["openapi:allOf/0"]
				require.True(t, ok, "the wrapper is kept verbatim; got %+v", model.Unmodeled)
				assert.Equal(t, jsontext.Pointer("/components/schemas/M/allOf/0"), entry.Provenance.Pointer)
				assert.Contains(t, string(entry.Value), `"allOf"`, "the whole wrapper is kept")
				var infos int
				for _, d := range doc.Diagnostics {
					if d.Code == diag.DegradedConstruct && d.Provenance.Pointer == "/components/schemas/M/allOf/0" {
						infos++
					}
				}
				assert.Equal(t, 1, infos, "exactly one degraded-construct at the wrapper")
				msg := openapitest.DiagMessageAt(t, doc.Diagnostics, diag.DegradedConstruct,
					ir.SeverityInfo, "/components/schemas/M/allOf/0")
				assert.Contains(t, msg, "("+tc.residue+")")
				assert.Contains(t, msg, "nested allOf compose")
				assert.NotContains(t, msg, "(allOf")
			}
		})
	}
}

// TestAllOf_KeptEntryOfAKeptWrapperIsKeptTwice pins the trade-off
// docs/compilers/openapi.md#allof-inline-residue states: a wrapper kept for
// its residue is kept whole, its entries included, so an entry kept itself, or
// a false one, is in Unmodeled twice, inside the wrapper's copy and under its
// own path.
func TestAllOf_KeptEntryOfAKeptWrapperIsKeptTwice(t *testing.T) {
	t.Parallel()
	m := `    M:
      allOf:
        - description: grouped
          allOf:
            - {$ref: '#/components/schemas/A'}
            - {title: inner, properties: {p: {type: string}}}
            - false
`
	for _, doc := range nestedOrders(t, m, nestedAB) {
		model, ok := typeByName(doc, "M").(*ir.Model)
		require.True(t, ok, "M should be a model")
		assert.Equal(t, []string{"openapi:allOf/0", "openapi:allOf/0/allOf/1", "openapi:allOf/0/allOf/2"},
			slices.Sorted(maps.Keys(model.Unmodeled)), "the wrapper and each entry kept itself, each under its own path")
		wrapper := string(model.Unmodeled["openapi:allOf/0"].Value)
		assert.Contains(t, wrapper, `"title":"inner"`, "the wrapper's copy holds the kept entry")
		assert.Contains(t, wrapper, `,false]`, "and the false one")
		assert.Contains(t, string(model.Unmodeled["openapi:allOf/0/allOf/1"].Value), `"title":"inner"`)
		assert.Equal(t, "false", string(model.Unmodeled["openapi:allOf/0/allOf/2"].Value))
	}
}

// TestAllOf_NestedEmptyAllOfKeptVerbatim pins that a branch wrapping an empty
// allOf is not flattened: it is kept verbatim and reported, never dropped. The
// source breaks the schema's minItems, so the one error the load reports for it
// is expected and nothing else.
func TestAllOf_NestedEmptyAllOfKeptVerbatim(t *testing.T) {
	t.Parallel()
	m := `    M:
      allOf:
        - allOf: []
        - {$ref: '#/components/schemas/A'}
`
	docs := make([]*ir.Document, 0, 2)
	for _, src := range []string{m + nestedAB, nestedAB + m} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(src))
		docs = append(docs, doc)
		errs := slices.DeleteFunc(slices.Clone(diags), func(d ir.Diagnostic) bool {
			return d.Severity != ir.SeverityError
		})
		require.Len(t, errs, 1, "only the minItems finding is an error")
		assert.Equal(t, jsontext.Pointer("/components/schemas/M/allOf/0/allOf"), errs[0].Provenance.Pointer)

		model, ok := typeByName(doc, "M").(*ir.Model)
		require.True(t, ok, "M should be a model")
		require.NotNil(t, model.Base, "the sibling $ref is the sole classified ref")
		assert.Equal(t, componentID("A"), model.Base.Target)
		assert.Empty(t, model.Mixins)
		entry, ok := model.Unmodeled["openapi:allOf/0"]
		require.True(t, ok, "the empty wrapper is kept verbatim; got %+v", model.Unmodeled)
		assert.JSONEq(t, `{"allOf":[]}`, string(entry.Value))
		assert.Contains(t,
			openapitest.DiagMessageAt(t, doc.Diagnostics, diag.DegradedConstruct,
				ir.SeverityInfo, "/components/schemas/M/allOf/0"),
			"only its properties and required list compose; the branch (allOf) is kept verbatim")
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types, orderInvariantIR()...),
		"declaration order must not change the types")
}

// TestAllOf_EmptyAllOfBesideNonObjectTypeNamesBoth pins that a branch whose
// empty allOf was not flattened lists allOf among the kept keywords on the
// warning arm too: only a capped branch leaves it out. The source breaks the
// schema's minItems, an error the load reports and parseFull tolerates.
func TestAllOf_EmptyAllOfBesideNonObjectTypeNamesBoth(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpec(nestedAB+`    M:
      allOf:
        - {type: string, allOf: []}
        - {$ref: '#/components/schemas/A'}
`))
	msg := openapitest.DiagMessageAt(t, doc.Diagnostics, diag.DegradedConstruct,
		ir.SeverityWarning, "/components/schemas/M/allOf/0")
	assert.Contains(t, msg, "the branch (type, allOf) is kept verbatim")
}

// TestAllOf_NestedInlineBranchKeyedByPointer pins that an inline branch inside
// a wrapper is kept under a key spelling its path, so it can collide with
// neither a sibling nor a top-level branch of the same index.
func TestAllOf_NestedInlineBranchKeyedByPointer(t *testing.T) {
	t.Parallel()
	m := `    M:
      allOf:
        - type: object
          properties: {a: {type: string}}
          description: Top
        - allOf:
            - type: object
              properties: {b: {type: string}}
              description: First
            - type: object
              properties: {c: {type: string}}
              minProperties: 1
`
	for _, doc := range nestedOrders(t, m, nestedAB) {
		model, ok := typeByName(doc, "M").(*ir.Model)
		require.True(t, ok, "M should be a model")
		require.Len(t, model.Unmodeled, 3, "got %+v", model.Unmodeled)
		for key, want := range map[string]string{
			"openapi:allOf/0":         `"description":"Top"`,
			"openapi:allOf/1/allOf/0": `"description":"First"`,
			"openapi:allOf/1/allOf/1": `"minProperties":1`,
		} {
			entry, ok := model.Unmodeled[key]
			require.True(t, ok, "missing %s in %+v", key, model.Unmodeled)
			assert.Contains(t, string(entry.Value), want)
			assert.Equal(t, "/components/schemas/M/"+strings.TrimPrefix(key, "openapi:"),
				string(entry.Provenance.Pointer))
			assert.Contains(t,
				openapitest.DiagMessageAt(t, doc.Diagnostics, diag.DegradedConstruct, ir.SeverityInfo,
					"/components/schemas/M/"+strings.TrimPrefix(key, "openapi:")), "inline allOf branch")
		}
	}
}

// TestAllOf_NestedBranchHintAgreesWithThePointerWalk pins that a $ref branch
// inside a wrapper is named by its innermost index, as subSchemaHint names the
// same pointer: an outside $ref to that pointer lowers it from the other side,
// and the two must agree in either declaration order. The target is the
// empty-named component because branchHint returns a target's own name before it
// reads the index, so only a target with an empty hint lets the index reach the IR.
// The B before the wrapper puts the branch at flat position 2 but innermost
// index 1, so only the innermost index yields variant_1.
func TestAllOf_NestedBranchHintAgreesWithThePointerWalk(t *testing.T) {
	t.Parallel()
	m := `    M:
      allOf:
        - {$ref: '#/components/schemas/B'}
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - {$ref: '#/components/schemas/', description: Narrowed}
`
	outsider := `    Outsider: {$ref: '#/components/schemas/M/allOf/1/allOf/1'}
`
	unnamed := "    \"\": {type: object, properties: {e: {type: string}}}\n"
	for _, doc := range nestedOrders(t, m, outsider+unnamed+nestedAB) {
		narrowed, ok := doc.Types["t/anon/components/schemas/M/allOf/1/allOf/1"]
		require.True(t, ok, "the narrowed branch is hoisted at its pointer")
		assert.Equal(t, "variant_1", narrowed.Common().Name.Hint)
	}
}

// TestAllOf_FalseBranchBesideUnionClosesEveryVariant pins that a `false` allOf
// branch, flat or inside a wrapper, closes every variant of a schema that also
// declares a oneOf, and is kept and reported once for the document.
func TestAllOf_FalseBranchBesideUnionClosesEveryVariant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, allOf, key, pointer string
	}{
		{"flat", `        - {$ref: '#/components/schemas/A'}
        - false
`, "openapi:allOf/1", "/components/schemas/U/allOf/1"},
		{"nested", `        - allOf:
            - {$ref: '#/components/schemas/A'}
            - false
`, "openapi:allOf/0/allOf/1", "/components/schemas/U/allOf/0/allOf/1"},
	}
	const pq = `    P: {type: object, properties: {p: {type: string}}}
    Q: {type: object, properties: {q: {type: string}}}
`
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := "    U:\n      oneOf: [{$ref: '#/components/schemas/P'}, {$ref: '#/components/schemas/Q'}]\n      allOf:\n" + tc.allOf
			for _, doc := range nestedOrders(t, u, nestedAB+pq) {
				for _, id := range []ir.TypeID{
					"t/composed/components/schemas/U/oneOf/0",
					"t/composed/components/schemas/U/oneOf/1",
				} {
					model, ok := doc.Types[id].(*ir.Model)
					require.True(t, ok, "%s should be a model", id)
					assert.Equal(t, ir.AdditionalClosed, model.Additional, id)
					entry, ok := model.Unmodeled[tc.key]
					require.True(t, ok, "%s: the false is kept under %s; got %+v", id, tc.key, model.Unmodeled)
					assert.Equal(t, "false", string(entry.Value))
					assert.Equal(t, jsontext.Pointer(tc.pointer), entry.Provenance.Pointer)
				}
				n := 0
				for _, d := range doc.Diagnostics {
					if d.Code == diag.FalseSchema && d.Severity == ir.SeverityInfo &&
						d.Provenance.Pointer == jsontext.Pointer(tc.pointer) {
						n++
					}
				}
				assert.Equal(t, 1, n, "one false-schema diagnostic for the document, not one per variant")
			}
		})
	}
}

// TestObject_MapRedeclaredAsAModelIsNoConflict pins that an allOf branch
// declaring a property as a map and another declaring it as a model is no
// conflicting redeclaration: both are objects, and one instance can satisfy
// both. Comparing node kinds alone reported it, and only in the order where
// the model was built first, so the spec runs in both orders.
func TestObject_MapRedeclaredAsAModelIsNoConflict(t *testing.T) {
	t.Parallel()
	const (
		a      = "    A: {type: object, properties: {b: {$ref: '#/components/schemas/B'}}}\n"
		b      = "    B:\n      allOf:\n        - properties: {m: {type: object, additionalProperties: {type: string}}}\n        - properties: {m: {$ref: '#/components/schemas/A'}}\n"
		inline = "    C:\n      allOf:\n        - properties: {meta: {type: object, additionalProperties: {type: string}}}\n        - properties: {meta: {type: object, properties: {a: {type: string}}}}\n"
	)
	for _, src := range []string{a + b + inline, inline + b + a} {
		_, diags := parseFull(t, openapitest.ComponentSpec(src))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Empty(t, conflictDiags(diags), "a map and a model can describe one object")
	}
}

func TestAllOf_UntypedRedeclarationDoesNotConflict(t *testing.T) {
	t.Parallel()
	// One branch leaves the field schemaless (the top type), the other types it.
	// `any` intersects with everything under allOf, so this is a narrowing, not a
	// contradiction — it must not be reported.
	spec := openapitest.ComponentSpec(`    Anyish:
      allOf:
        - type: object
          properties:
            id: {description: the identifier}
        - type: object
          properties:
            id: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "Anyish").(*ir.Model)
	require.True(t, ok, "Anyish should be a model")
	require.Len(t, m.Properties, 1, "id reconciles to one property")
	assert.Empty(t, conflictDiags(diags),
		"the schemaless top type never conflicts with a sibling redeclaration")
}

func TestAllOf_EquivalentNumericBoundsDoNotConflict(t *testing.T) {
	t.Parallel()
	// The same bound spelled two ways (10 and 10.0) denotes one value, so it must
	// compare equal by magnitude and stay silent.
	spec := openapitest.ComponentSpec(`    Boundish:
      allOf:
        - type: object
          properties:
            n: {type: number, minimum: 10}
        - type: object
          properties:
            n: {type: number, minimum: 10.0}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"10 and 10.0 are the same numeric bound, not a conflict")
}

func TestAllOf_DifferingNumericBoundsConflict(t *testing.T) {
	t.Parallel()
	// Two branches pin the same lower bound to different magnitudes; the kept
	// winner is arbitrary source order, so the dropped bound is surfaced.
	spec := openapitest.ComponentSpec(`    Boundish:
      allOf:
        - type: object
          properties:
            n: {type: integer, minimum: 5}
        - type: object
          properties:
            n: {type: integer, minimum: 10}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "differing numeric bounds are diagnosed once")
	assert.Contains(t, conflicts[0].Message, `"n"`)
}

func TestAllOf_ScalarVersusObjectRedeclarationConflicts(t *testing.T) {
	t.Parallel()
	// A scalar in one branch and a structural type in the other cannot both hold.
	spec := openapitest.ComponentSpec(`    Mixed:
      allOf:
        - type: object
          properties:
            f: {type: string}
        - type: object
          properties:
            f:
              type: object
              properties:
                x: {type: string}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "a scalar against an object is diagnosed once")
	assert.Contains(t, conflicts[0].Message, `"f"`)
}

func TestAllOf_DistinctInlineObjectsDoNotConflict(t *testing.T) {
	t.Parallel()
	// Two branches redeclare the field as distinct inline objects. Each hoists its
	// own model at its own pointer, so the targets differ — but two objects of the
	// same kind are not provably contradictory, and conflict detection never
	// guesses.
	spec := openapitest.ComponentSpec(`    Objish:
      allOf:
        - type: object
          properties:
            f:
              type: object
              properties:
                x: {type: string}
        - type: object
          properties:
            f:
              type: object
              properties:
                x: {type: string}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"two distinct inline objects for one field are not a provable conflict")
}

func TestAllOf_ConstraintAndFormatConflictsDiagnosed(t *testing.T) {
	t.Parallel()
	// Each way a redeclaration can contradict across branches: a differing
	// minimum, exclusiveMinimum, pattern or multipleOf, a bound and a multipleOf
	// that differ past the magnitude a rational will build, and two
	// format-derived primitives (uuid vs date-time).
	// Each keeps the first declaration but surfaces exactly one conflict naming
	// the field, both branch sites, and — for the constraint cases — the
	// offending keyword with both of its conflicting values, so the author
	// never has to diff both branches by hand.
	cases := []struct {
		name, a, b, wantDetail string
	}{
		{
			name:       "minimum",
			a:          "{type: number, minimum: 10}",
			b:          "{type: number, minimum: 20}",
			wantDetail: "conflicting minimum (10 and 20)",
		},
		{
			// The exclusive bound is a keyword of its own, so it conflicts
			// under its own name rather than as a differing sense of minimum.
			name:       "exclusiveMinimum",
			a:          "{type: number, exclusiveMinimum: 10}",
			b:          "{type: number, exclusiveMinimum: 20}",
			wantDetail: "conflicting exclusiveMinimum (10 and 20)",
		},
		{
			name:       "pattern",
			a:          "{type: string, pattern: '^a$'}",
			b:          "{type: string, pattern: '^b$'}",
			wantDetail: `conflicting pattern ("^a$" and "^b$")`,
		},
		{
			name:       "multipleOf",
			a:          "{type: integer, multipleOf: 2}",
			b:          "{type: integer, multipleOf: 3}",
			wantDetail: "conflicting multipleOf (2 and 3)",
		},
		{
			// The other half of the past-a-rational rows in
			// TestAllOf_CompatibleConstraintRedeclarationsStaySilent: a
			// magnitude no rational holds still has to be ordered, not waved
			// through, or the fix for the equal pair would just silence every
			// pair that large.
			name:       "minimum too large for a rational",
			a:          "{type: number, minimum: 1e1000001}",
			b:          "{type: number, minimum: 2e1000001}",
			wantDetail: "conflicting minimum (1e1000001 and 2e1000001)",
		},
		{
			name:       "multipleOf too large for a rational",
			a:          "{type: number, multipleOf: 1e1000001}",
			b:          "{type: number, multipleOf: 1e1000002}",
			wantDetail: "conflicting multipleOf (1e1000001 and 1e1000002)",
		},
		{
			name: "uuid vs date-time",
			a:    "{type: string, format: uuid}",
			b:    "{type: string, format: date-time}",
			// A type conflict, not a constraint one — checked via the
			// type-conflict message shape in TestAllOf_TypeConflictMessageNamesBothTypes.
			wantDetail: "incompatible types",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := lowerSpec(t, allOfConflictSpec(tc.a, tc.b))
			openapitest.RequireNoErrorDiags(t, diags)
			conflicts := conflictDiags(diags)
			require.Len(t, conflicts, 1, "%s is diagnosed exactly once", tc.name)
			assert.Contains(t, conflicts[0].Message, `"v"`, "the diagnostic names the field")
			assert.Contains(t, conflicts[0].Message, "allOf/0", "names the first branch site")
			assert.Contains(t, conflicts[0].Message, "allOf/1", "names the second branch site")
			assert.Contains(t, conflicts[0].Message, tc.wantDetail,
				"%s: the diagnostic names what actually differs", tc.name)
		})
	}
}

// TestAllOf_PrimitiveNarrowingIsLosslessInEitherOrder pins that a redeclaration
// narrowing a bare primitive (GitHub #598, #446) loses nothing: the narrower
// target wins in both branch orders, with no diagnostic and no kept entry, and
// the two orders merge to the same type.
func TestAllOf_PrimitiveNarrowingIsLosslessInEitherOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, broad, narrow string
		want                ir.TypeID
	}{
		{"number and integer", "{type: number}", "{type: integer}", "t/prim/integer"},
		{"integer and int32", "{type: integer}", "{type: integer, format: int32}", "t/prim/int32"},
		{"string and date-time", "{type: string}", "{type: string, format: date-time}", "t/prim/datetime_offset"},
		{"int32 and int64", "{type: integer, format: int64}", "{type: integer, format: int32}", "t/prim/int32"},
		{"uri and string", "{type: string}", "{type: string, format: uri}", "t/prim/url"},
		{"double and number", "{type: number}", "{type: number, format: double}", "t/prim/float64"},
		{"uuid and string", "{type: string}", "{type: string, format: uuid}", "t/prim/uuid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var merged [2]ir.TypeRef
			for i, spec := range []string{allOfConflictSpec(tc.broad, tc.narrow), allOfConflictSpec(tc.narrow, tc.broad)} {
				doc, diags := lowerSpec(t, spec)
				openapitest.RequireNoErrorDiags(t, diags)
				assert.Empty(t, diags, "a narrowing draws no diagnostic (order %d)", i)
				model, ok := doc.Types[componentID("T")].(*ir.Model)
				require.True(t, ok)
				require.Len(t, model.Properties, 1)
				prop := model.Properties[0]
				assert.Equal(t, tc.want, prop.Type.Target, "the narrower target wins (order %d)", i)
				for key := range prop.Unmodeled {
					assert.NotContains(t, key, "openapi:conflicting-redeclaration", "nothing is kept (order %d)", i)
				}
				merged[i] = prop.Type
			}
			assert.Empty(t, cmp.Diff(merged[0], merged[1]), "the merged type does not depend on branch order")
		})
	}
}

func TestAllOf_CompatibleConstraintRedeclarationsStaySilent(t *testing.T) {
	t.Parallel()
	// The false-positive guards for the constraint path: a keyword present on
	// only one branch (both branches still carry constraints) is intersected,
	// not a conflict — and the merge must genuinely carry both keywords
	// forward on the reconciled property, not just stay quiet about the one it
	// used to drop; one magnitude spelled two ways is one value, whether or not
	// it is one a rational will build; and an unknown-format scalar narrows the
	// plain type it is based on, so it wins with nothing to report.
	cases := []struct {
		name         string
		a, b         string
		assertMerged func(t *testing.T, c *ir.Constraints)
	}{
		{
			name: "one-sided keywords",
			a:    "{type: string, maxLength: 10}",
			b:    "{type: string, minLength: 2}",
			assertMerged: func(t *testing.T, c *ir.Constraints) {
				t.Helper()
				require.NotNil(t, c)
				require.NotNil(t, c.MaxLength, "the first branch's maxLength is kept")
				require.NotNil(t, c.MinLength, "the second branch's minLength is adopted, not dropped")
				assert.Equal(t, int64(10), *c.MaxLength)
				assert.Equal(t, int64(2), *c.MinLength)
			},
		},
		{
			name: "one-sided pattern",
			a:    "{type: string, maxLength: 10}",
			b:    "{type: string, pattern: '^a'}",
			assertMerged: func(t *testing.T, c *ir.Constraints) {
				t.Helper()
				require.NotNil(t, c)
				require.NotNil(t, c.MaxLength, "the first branch's maxLength is kept")
				assert.Equal(t, int64(10), *c.MaxLength)
				assert.Equal(t, "^a", c.Pattern, "the second branch's pattern is adopted, not dropped")
			},
		},
		{
			// minimum and exclusiveMinimum are two keywords, so a branch
			// declaring one and a branch declaring the other intersect to a
			// field carrying both — not to whichever the merge picked.
			name: "minimum and exclusiveMinimum adopted side by side",
			a:    "{type: number, minimum: 1}",
			b:    "{type: number, exclusiveMinimum: 5}",
			assertMerged: func(t *testing.T, c *ir.Constraints) {
				t.Helper()
				require.NotNil(t, c)
				require.NotNil(t, c.Min, "the first branch's minimum stays")
				assert.Equal(t, "1", c.Min.String())
				require.NotNil(t, c.ExclusiveMin, "the second branch's exclusiveMinimum is adopted beside it")
				assert.Equal(t, "5", c.ExclusiveMin.String())
			},
		},
		{name: "equivalent multipleOf", a: "{type: number, multipleOf: 2}", b: "{type: number, multipleOf: 2.0}"},
		// Both BigVal keywords at a magnitude math/big will not build as a
		// rational. The value is the same on either branch, so there is nothing
		// to report; a comparison that gave up on the magnitude instead would
		// call one value two and fail a --fail-on warning build.
		{
			name: "equal minimum too large for a rational",
			a:    "{type: number, minimum: 1e1000001}",
			b:    "{type: number, minimum: 10e1000000}",
		},
		{
			name: "equal multipleOf too large for a rational",
			a:    "{type: number, multipleOf: 1e1000001}",
			b:    "{type: number, multipleOf: 10e1000000}",
		},
		{name: "custom format over base", a: "{type: string, format: weird}", b: "{type: string}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, allOfConflictSpec(tc.a, tc.b))
			assert.Empty(t, diags, "%s draws no diagnostic", tc.name)
			if tc.assertMerged == nil {
				return
			}
			m, ok := typeByName(doc, "T").(*ir.Model)
			require.True(t, ok, "T should be a model")
			require.Len(t, m.Properties, 1, "v reconciles to one property")
			tc.assertMerged(t, m.Properties[0].Constraints)
		})
	}
}

func TestAllOf_OpaqueScalarVsPrimitiveNoConflict(t *testing.T) {
	t.Parallel()
	// An opaque scalar (format without a base type) is unknown, not structural,
	// so it's not provably incompatible with a primitive. The "never guess"
	// principle means we don't flag this as a conflict.
	spec := openapitest.ComponentSpec(`    OpaqueTest:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {format: custom}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"opaque scalar vs primitive is not flagged as a conflict")
}

func TestAllOf_ThreeWayRedeclarationProducesTwoDiagnostics(t *testing.T) {
	t.Parallel()
	// Three branches typing one field differently fold as one set: the first
	// declaration wins, and each other is reported against it, so two
	// diagnostics, though all three pairs are incompatible.
	spec := openapitest.ComponentSpec(`    ThreeWay:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: integer}
        - type: object
          properties:
            id: {type: boolean}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 2, "three-way redeclaration produces two diagnostics")
}

func TestAllOf_ThreeWayCompatibleRedeclarationStaysSilent(t *testing.T) {
	t.Parallel()
	// When three allOf branches declare the same field with compatible types
	// (all the same), no conflict is reported.
	spec := openapitest.ComponentSpec(`    ThreeWayCompat:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: string}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"three-way compatible redeclaration stays silent")
}

func TestAllOf_SatisfiableNarrowingsStaySilent(t *testing.T) {
	t.Parallel()
	// Each case narrows a base type to a stricter but satisfiable shape under
	// allOf intersection (enum of the base's own primitive, const pinning a
	// legal value, a union narrowed to one of its own members) — none is a
	// provable contradiction, so a bare scalar sibling against any of these must
	// not be flagged.
	cases := []struct {
		name, a, b string
	}{
		{"enum narrows base type", "{type: string}", "{type: string, enum: [active, inactive]}"},
		{"const narrows base type", "{type: string}", "{type: string, const: dog}"},
		{"union narrowed to one member", "{type: [string, integer]}", "{type: string}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := lowerSpec(t, allOfConflictSpec(tc.a, tc.b))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Empty(t, conflictDiags(diags), "%s must not be reported as a conflict", tc.name)
		})
	}
}

func TestAllOf_EnumVersusIncompatibleTypeStillConflicts(t *testing.T) {
	t.Parallel()
	// Guard against over-correcting the enum false-positive fix: an enum whose
	// declared ValueType is string still provably conflicts with an
	// incompatible primitive (integer).
	_, diags := lowerSpec(t, allOfConflictSpec(
		"{type: string, enum: [active, inactive]}", "{type: integer}"))
	openapitest.RequireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "enum of strings vs integer is still a provable conflict")
	assert.Contains(t, conflicts[0].Message, `"v"`, "the diagnostic names the conflicting field")
}

func TestAllOf_TypeConflictMessageNamesBothTypes(t *testing.T) {
	t.Parallel()
	// The type-conflict diagnostic must say what actually differs, not just
	// that something did: the two conflicting type identities, so the author
	// can see at a glance what disagreed without cross-referencing the spec.
	_, diags := lowerSpec(t, allOfConflictSpec("{type: string}", "{type: integer}"))
	openapitest.RequireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1)
	assert.Contains(t, conflicts[0].Message, "t/prim/string", "names the first branch's type")
	assert.Contains(t, conflicts[0].Message, "t/prim/integer", "names the second branch's type")
}

func TestAllOf_PropertyAlongsideAllOfConflictMessageIsAccurate(t *testing.T) {
	t.Parallel()
	// A property declared directly on the schema alongside allOf redeclares a
	// field the inline branch also declares. The redeclaration's provenance
	// pointer here is a plain "properties/id" path, never an "allOf/N" branch,
	// so the message must not claim "allOf branches redeclare" — it must read
	// correctly for a co-declared sibling property too (merge.PropIndex's
	// MergeProperty folds both cases the same way; see its doc comment).
	spec := openapitest.ComponentSpec(`    Along:
      type: object
      properties:
        id: {type: string}
      allOf:
        - type: object
          properties:
            id: {type: integer}
`)
	_, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "the co-declared property conflict is diagnosed once")
	d := conflicts[0]
	assert.Contains(t, d.Message, `"id"`, "the diagnostic names the conflicting field")
	assert.NotContains(t, d.Message, "allOf branches",
		"the redeclaration site is a co-declared property, not an allOf branch")
	assert.Equal(t, jsontext.Pointer("/components/schemas/Along/properties/id"), d.Provenance.Pointer,
		"the diagnostic's own site is the co-declared property, not an allOf branch")
}

// TestAllOf_FalseBranchClosesTheComposition pins what a `false` allOf branch
// does to the composed node. `false` admits nothing, so the composition admits
// nothing: the model closes and the branch is kept verbatim beside it, which is
// the same answer a bare `false` schema gets in its own right.
func TestAllOf_FalseBranchClosesTheComposition(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    Never:
      allOf:
        - false
        - type: object
          properties: {id: {type: string}}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := typeByName(doc, "Never").(*ir.Model)
	require.True(t, ok, "the composition still lowers to a model")
	assert.Equal(t, ir.AdditionalClosed, m.Additional, "a branch matching nothing closes the composition")
	require.Len(t, m.Unmodeled, 1, "the branch is kept verbatim")
	assert.Equal(t, ir.RawValue("false"), m.Unmodeled["openapi:allOf/0"].Value,
		"keyed by the branch index, so sibling branches cannot overwrite one another")
	assert.True(t, openapitest.HasDiagAt(diags, diag.FalseSchema, ir.SeverityInfo), "and it is announced")
}

// TestAllOf_UnpreservableBranchIsNotAnnounced is
// TestUnhomedApplicator_UnpreservableKeywordIsNotAnnounced's pairing on the
// composition path: a branch the merge does not consume whole is normally kept
// beside the model and reported, and when the keeping fails the report must not
// happen either.
func TestAllOf_UnpreservableBranchIsNotAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(`    M:
      allOf:
        - {type: string, maxLength: 3, x-t: `+unpreservableValue+`}
`))

	td := typeByName(doc, "M")
	require.NotNil(t, td)
	assert.NotContains(t, td.Common().Unmodeled, "openapi:allOf/0", "the branch did not convert")
	assert.True(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct))
	assert.Empty(t, preservationClaims(diags),
		"no degradation is announced for a branch that was not kept")
}

// compileView compiles the defs with the component m and returns M's view and
// the codes reported, requiring no error.
func compileView(t *testing.T, m string) (composedView, []string) {
	t.Helper()
	doc, diags := parseFull(t, openapitest.ComponentSpec(equivalenceDefs+m))
	openapitest.RequireNoErrorDiags(t, diags)
	return viewOf(doc, componentID("M"), 1), diagCodes(diags)
}

// equivalencePairs are (nested, flat) spellings of one composition M. Each
// nested spelling must lower as its flat one does
// (docs/compilers/openapi.md#allof-classification), at any depth up to
// MaxAllOfFlattenDepth.
var equivalencePairs = []struct {
	name   string
	nested string
	flat   string
}{
	{"one wrapper around two refs", `    M:
      allOf:
        - allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`},
	{"two wrappers", `    M:
      allOf:
        - allOf:
            - allOf: [{$ref: '#/components/schemas/A'}]
            - {$ref: '#/components/schemas/B'}
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`},
	{"three wrappers", `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - allOf:
                - allOf: [{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/C'}]
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/C'}]
`},
	{"four wrappers", `    M:
      allOf:
        - allOf:
            - allOf:
                - allOf:
                    - allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`},
	{"a discriminated base inside a wrapper", pet + `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - {$ref: '#/components/schemas/Pet'}
`, pet + `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/Pet'}]
`},
	{"inline branches with properties, required and type object", `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - type: object
              required: [x]
              properties: {x: {type: integer}, y: {type: string}}
            - allOf:
                - properties: {z: {type: boolean}}
`, `    M:
      allOf:
        - {$ref: '#/components/schemas/A'}
        - type: object
          required: [x]
          properties: {x: {type: integer}, y: {type: string}}
        - properties: {z: {type: boolean}}
`},
	{"a wrapper's own properties merge after its entries", `    M:
      allOf:
        - required: [w]
          properties: {w: {type: string}}
          allOf:
            - type: object
              properties: {x: {type: integer}}
            - properties: {y: {type: integer}}
`, `    M:
      allOf:
        - type: object
          properties: {x: {type: integer}}
        - properties: {y: {type: integer}}
        - required: [w]
          properties: {w: {type: string}}
`},
	{"a required name only the base declares", `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - {required: [a]}
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {required: [a]}]
`},
	{"a true branch", `    M:
      allOf:
        - allOf: [{$ref: '#/components/schemas/A'}, true]
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, true]
`},
	{"a false branch", `    M:
      allOf:
        - allOf: [{$ref: '#/components/schemas/A'}, false]
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, false]
`},
	{"a residue description", `    M:
      allOf:
        - description: grouped
          allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`, `    M:
      allOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
        - description: grouped
`},
	{"a distributed oneOf beside the wrappers", `    M:
      type: object
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/C'}
            - properties: {w: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`, `    M:
      type: object
      allOf:
        - {$ref: '#/components/schemas/C'}
        - properties: {w: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`},
}

// wires lists the wire names of props.
func wires(props []propView) []string {
	out := make([]string, 0, len(props))
	for _, p := range props {
		out = append(out, p.Wire)
	}
	return out
}

// nonBareSpec declares T with one property v written as schemaA in the first
// branch and schemaB in the second, after the components in extra.
func nonBareSpec(extra, schemaA, schemaB string) string {
	return openapitest.ComponentSpec(extra +
		"    T:\n" +
		"      allOf:\n" +
		"        - type: object\n" +
		"          properties: {v: " + schemaA + "}\n" +
		"        - type: object\n" +
		"          properties: {v: " + schemaB + "}\n")
}

// nestedOrders compiles first and last in both declaration orders and requires
// each to validate, report no error, and the two to agree on their types and on
// what they report. Flattening reads a nested branch's $refs, so a decision
// taken off the registry while M lowers would see only the targets declared
// before it.
func nestedOrders(t *testing.T, first, last string) []*ir.Document {
	t.Helper()
	require.NotEmpty(t, last, "one block compiles the same document twice")
	docs := make([]*ir.Document, 0, 2)
	for _, src := range []string{first + last, last + first} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(src))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Empty(t, pass.Validate(doc), "the document must validate")
		docs = append(docs, doc)
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types, orderInvariantIR()...),
		"declaration order must not change the types")
	assert.Equal(t, reported(docs[0].Diagnostics), reported(docs[1].Diagnostics),
		"declaration order must not change what is reported")
	return docs
}

const nestedAB = `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
`

// refTargets lists the TypeIDs refs target, in order.
func refTargets(refs []ir.TypeRef) []ir.TypeID {
	out := make([]ir.TypeID, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Target)
	}
	return out
}

// equivalenceDefs are the components every pair composes: two plain schemas, a
// third to compose beside them.
const equivalenceDefs = `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    C: {type: object, properties: {c: {type: integer}}}
`

// diagCodes lists the codes doc's compilation reported, in order.
func diagCodes(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

// pet is a discriminated base routing to M.
const pet = `    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {dog: '#/components/schemas/M'}
`

// allOfConflictSpec builds a component whose two inline allOf branches each
// declare field v with the given flow-style schemas, for exercising per-keyword
// redeclaration conflict detection.
func allOfConflictSpec(schemaA, schemaB string) string {
	return openapitest.ComponentSpec(
		"    T:\n" +
			"      allOf:\n" +
			"        - type: object\n" +
			"          properties: {v: " + schemaA + "}\n" +
			"        - type: object\n" +
			"          properties: {v: " + schemaB + "}\n")
}

// composedView is a composed model with what the nested and flat spellings may
// differ in projected away: pointers, provenance and the IDs of hoisted nodes.
// Kept counts the model's Unmodeled entries: their keys and values differ by
// spelling, but how many the composition kept must not.
type composedView struct {
	Base       string
	Mixins     []string
	Props      []propView
	Requires   []string
	Additional ir.AdditionalMode
	DiscValue  string
	Variants   []composedView
	Kept       int
}

// propView is one property of a composedView.
type propView struct {
	Wire     string
	Type     string
	Required bool
}

// projectTarget names a type reference without the ID of a node hoisted at a
// pointer, which differs by spelling: a component is kept by ID, anything else by
// its kind.
func projectTarget(doc *ir.Document, id ir.TypeID) string {
	if strings.HasPrefix(string(id), "t/anon/") {
		return "anon:" + string(doc.Types[id].Kind())
	}
	return string(id)
}

// viewOf projects the type at id: a model as itself, a union as its variants,
// each projected in turn. depth bounds the descent, which a union's variants
// reach once.
func viewOf(doc *ir.Document, id ir.TypeID, depth int) composedView {
	var v composedView
	switch td := doc.Types[id].(type) {
	case *ir.Model:
		v = modelView(doc, td)
	case *ir.Union:
		if depth > 0 {
			for _, variant := range td.Variants {
				v.Variants = append(v.Variants, viewOf(doc, variant.Type.Target, depth-1))
			}
		}
	default:
		v.Base = "not a model or union: " + string(id)
	}
	return v
}

// modelView projects a model's composition, properties, the required names it
// holds no property for, and its openness.
func modelView(doc *ir.Document, m *ir.Model) composedView {
	v := composedView{
		Requires: m.Requires, Additional: m.Additional, DiscValue: m.DiscriminatorValue, Kept: len(m.Unmodeled),
	}
	if m.Base != nil {
		v.Base = string(m.Base.Target)
	}
	for _, mixin := range m.Mixins {
		v.Mixins = append(v.Mixins, string(mixin.Target))
	}
	for _, p := range m.Properties {
		v.Props = append(v.Props, propView{Wire: p.WireName, Type: projectTarget(doc, p.Type.Target), Required: p.Required})
	}
	return v
}
