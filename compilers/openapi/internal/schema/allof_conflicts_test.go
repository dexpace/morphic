// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
			doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
			doc, diags := schema.LowerSpec(t, spec)
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
				_, diags := schema.LowerSpec(t, allOfConflictSpec(a, b))
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	_, diags := schema.LowerSpec(t, spec)
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
	_, diags := schema.LowerSpec(t, spec)
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
	_, diags := schema.LowerSpec(t, spec)
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
	_, diags := schema.LowerSpec(t, spec)
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
			_, diags := schema.LowerSpec(t, allOfConflictSpec(tc.a, tc.b))
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
			doc, diags := schema.LowerSpec(t, allOfConflictSpec(tc.a, tc.b))
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
	_, diags := schema.LowerSpec(t, spec)
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
	_, diags := schema.LowerSpec(t, spec)
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
	_, diags := schema.LowerSpec(t, spec)
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
			_, diags := schema.LowerSpec(t, allOfConflictSpec(tc.a, tc.b))
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
	_, diags := schema.LowerSpec(t, allOfConflictSpec(
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
	_, diags := schema.LowerSpec(t, allOfConflictSpec("{type: string}", "{type: integer}"))
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
	_, diags := schema.LowerSpec(t, spec)
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

// redeclaredModelsSpec declares Meeting, then T and U, which compose it with an
// inline object in each order.
const redeclaredModelsSpec = `    Meeting:
      type: object
      properties:
        id: {type: string}
    T:
      allOf:
        - {type: object, properties: {data: {$ref: '#/components/schemas/Meeting'}}}
        - {type: object, properties: {data: {type: object, properties: {ai: {type: string}}}}}
    U:
      allOf:
        - {type: object, properties: {data: {type: object, properties: {ai: {type: string}}}}}
        - {type: object, properties: {data: {$ref: '#/components/schemas/Meeting'}}}
`

// composedField returns the model the field wire of the component name composes
// to, and asserts its ID is the field's pointer in the composed space, and that
// it is anonymous and points back at that pointer.
func composedField(t *testing.T, doc *ir.Document, name, wire string) *ir.Model {
	t.Helper()
	p := propertyOf(t, doc, name, wire)
	field := "/components/schemas/" + name + "/properties/" + wire
	want := ir.TypeID("t/composed" + field)
	require.Equal(t, want, p.Type.Target)
	m, ok := doc.Types[want].(*ir.Model)
	require.True(t, ok, "%s should hold a registered model", want)
	assert.Equal(t, field, string(m.Provenance.Pointer), "it points back at the field")
	assert.True(t, m.Anonymous)
	return m
}

// TestAllOf_AFieldRedeclaredAsTwoModelsComposesThem pins GitHub #1085 in both
// branch orders: the field names one minted model holding both declarations, so
// neither the component's properties nor the inline object's are lost, and the
// branch order decides nothing but the order inline nodes are named in. The
// component stays the Base as the sole $ref does in a written allOf, and each
// declaration keeps the node its own pointer denotes.
func TestAllOf_AFieldRedeclaredAsTwoModelsComposesThem(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(redeclaredModelsSpec))
	assert.Empty(t, diags, "nothing is lost, so nothing is reported")

	for _, tc := range []struct {
		name, inline string
	}{
		{"T", "t/anon/components/schemas/T/allOf/1/properties/data"},
		{"U", "t/anon/components/schemas/U/allOf/0/properties/data"},
	} {
		m := composedField(t, doc, tc.name, "data")
		require.NotNil(t, m.Base, tc.name)
		assert.Equal(t, componentID("Meeting"), m.Base.Target, tc.name)
		require.Len(t, m.Mixins, 1, tc.name)
		assert.Equal(t, ir.TypeID(tc.inline), m.Mixins[0].Target, tc.name)
		assert.Empty(t, m.Properties, "%s: the composition stays in source form", tc.name)
		assert.Empty(t, propertyOf(t, doc, tc.name, "data").Unmodeled, "%s: nothing is kept", tc.name)

		inline, ok := doc.Types[ir.TypeID(tc.inline)].(*ir.Model)
		require.True(t, ok, tc.name)
		require.Len(t, inline.Properties, 1, tc.name)
		assert.Equal(t, "ai", inline.Properties[0].WireName, tc.name)
	}
}

// TestAllOf_AComposedFieldKeepsADiscriminatedBase pins the Base a minted model
// takes as a written allOf would: among several $ref declarations, the one
// carrying a discriminator, wherever it is written.
func TestAllOf_AComposedFieldKeepsADiscriminatedBase(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Plain:
      type: object
      properties:
        note: {type: string}
    Pet:
      type: object
      required: [kind]
      properties:
        kind: {type: string}
      discriminator: {propertyName: kind}
    Owner:
      allOf:
        - {type: object, properties: {pet: {$ref: '#/components/schemas/Plain'}}}
        - {type: object, properties: {pet: {$ref: '#/components/schemas/Pet'}}}
`))
	assert.Empty(t, diags)
	m := composedField(t, doc, "Owner", "pet")
	require.NotNil(t, m.Base)
	assert.Equal(t, componentID("Pet"), m.Base.Target)
	require.Len(t, m.Mixins, 1)
	assert.Equal(t, componentID("Plain"), m.Mixins[0].Target)
}

// TestAllOf_EveryDistributedVariantSharesOneComposedField pins the mint across
// variants. A distributed union's variants each fold the body's declarations,
// and each keeps the field's one PropID, so each names the one node.
func TestAllOf_EveryDistributedVariantSharesOneComposedField(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    A:
      type: object
      properties:
        a: {type: string}
    B:
      type: object
      properties:
        b: {type: string}
    C:
      type: object
      properties:
        c: {type: string}
    D:
      type: object
      properties:
        d: {type: string}
    S:
      allOf:
        - {type: object, properties: {data: {$ref: '#/components/schemas/A'}}}
        - {type: object, properties: {data: {$ref: '#/components/schemas/B'}}}
      oneOf:
        - {$ref: '#/components/schemas/C'}
        - {$ref: '#/components/schemas/D'}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	const shared = ir.TypeID("t/composed/components/schemas/S/properties/data")
	for _, variant := range []ir.TypeID{
		"t/composed/components/schemas/S/oneOf/0",
		"t/composed/components/schemas/S/oneOf/1",
	} {
		m, ok := doc.Types[variant].(*ir.Model)
		require.True(t, ok, variant)
		require.Len(t, m.Properties, 1, variant)
		assert.Equal(t, shared, m.Properties[0].Type.Target, variant)
	}
	body, ok := doc.Types[shared].(*ir.Model)
	require.True(t, ok)
	assert.Nil(t, body.Base)
	require.Len(t, body.Mixins, 2)
	assert.Equal(t, componentID("A"), body.Mixins[0].Target)
	assert.Equal(t, componentID("B"), body.Mixins[1].Target)
}

// TestAllOf_AnAliasIsJudgedAsTheTypeItStandsFor pins the judgment through an alias
// component, in either order: an alias of a model composes as that model does,
// and against a kind it is the conflict that model would be.
func TestAllOf_AnAliasIsJudgedAsTheTypeItStandsFor(t *testing.T) {
	t.Parallel()
	alias, other, kind := "{$ref: '#/components/schemas/Z'}", "{$ref: '#/components/schemas/B'}", "{type: string}"
	for _, tc := range []struct {
		first, second string
		mixins        []ir.TypeID // nil where the pair conflicts
	}{
		{first: alias, second: other, mixins: []ir.TypeID{componentID("Z"), componentID("B")}},
		{first: other, second: alias, mixins: []ir.TypeID{componentID("B"), componentID("Z")}},
		{first: alias, second: kind},
		{first: kind, second: alias},
	} {
		doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(fmt.Sprintf(`    A:
      type: object
      properties:
        a: {type: string}
    B:
      type: object
      properties:
        b: {type: string}
    Z:
      $ref: '#/components/schemas/A'
    T:
      allOf:
        - {type: object, properties: {v: %s}}
        - {type: object, properties: {v: %s}}
`, tc.first, tc.second)))
		openapitest.RequireNoErrorDiags(t, diags)
		name := tc.first + " then " + tc.second
		if tc.mixins == nil {
			assert.Len(t, conflictDiags(diags), 1, name)
			continue
		}
		assert.Empty(t, diags, name)
		m := composedField(t, doc, "T", "v")
		require.Len(t, m.Mixins, 2, name)
		assert.Equal(t, tc.mixins, []ir.TypeID{m.Mixins[0].Target, m.Mixins[1].Target}, name)
	}
}

// TestAllOf_RelatedStructuresAreNotCalledAConflict pins the warning to pairs
// that cannot both hold (GitHub #899). `["a"]` satisfies a list and a tuple, a
// union may hold the very model it meets, and a `const` object may be a value of
// a model, so each draws an info saying the merge does not compare them, in
// either order.
func TestAllOf_RelatedStructuresAreNotCalledAConflict(t *testing.T) {
	t.Parallel()
	list := "{type: array, items: {type: string}}"
	tuple := "{type: array, prefixItems: [{type: string}]}"
	model := "{$ref: '#/components/schemas/A'}"
	union := "{oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}"
	object := "{const: {a: x}}"
	for _, tc := range []struct{ name, a, b string }{
		{"a list, then a tuple", list, tuple},
		{"a tuple, then a list", tuple, list},
		{"a model, then a union", model, union},
		{"a union, then a model", union, model},
		{"a model, then a const object", model, object},
		{"a const object, then a model", object, model},
	} {
		spec := openapitest.ComponentSpec(fmt.Sprintf(`    A:
      type: object
      properties:
        a: {type: string}
    B:
      type: object
      properties:
        b: {type: string}
    T:
      allOf:
        - {type: object, properties: {v: %s}}
        - {type: object, properties: {v: %s}}
`, tc.a, tc.b))
		doc, diags := schema.LowerSpec(t, spec)
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Empty(t, conflictDiags(diags), tc.name)
		merged := withCode(diags, diag.DegradedConstruct)
		require.Len(t, merged, 1, tc.name)
		assert.Contains(t, merged[0].Message, "that the merge does not compare", tc.name)
		assert.Contains(t, propertyOf(t, doc, "T", "v").Unmodeled,
			"openapi:conflicting-redeclaration/components/schemas/T/allOf/1/properties/v", tc.name)
	}
}
