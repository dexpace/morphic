package openapi

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
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
	assert.True(t, hasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
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
			assert.False(t, hasDiag(diags, diag.UnattachableRequired),
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
		if d.Code == diag.UnattachableRequired {
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

	require.Equal(t, 1, countDiagsAt(diags, diag.UnattachableRequired, ir.SeverityInfo),
		"exactly one info-severity unattachable-required diagnostic")
	var unattachable ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.UnattachableRequired {
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

// TestAllOf_InlineBranchResidueKeptVerbatim covers GitHub #123: an inline allOf
// branch is merged in place, so the merge reads only its properties and its
// required list and everything else it declared used to be dropped without a
// diagnostic. The branch now survives verbatim beside the composed model.
func TestAllOf_InlineBranchResidueKeptVerbatim(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
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
	requireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok, "S should be a model")

	a, ok := propsByWire(m.Properties)["a"]
	require.True(t, ok, "the merge still contributes the branch's properties")
	assert.True(t, a.Required, "and still ORs the branch's required list onto them")

	entry, ok := m.Unmodeled["openapi:allOf/0"]
	require.True(t, ok, "the branch is kept verbatim; got %+v", m.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.Equal(t, "/components/schemas/S/allOf/0", entry.Provenance.Pointer,
		"the entry locates the branch, not the composed schema")
	for _, kept := range []string{
		`"additionalProperties":false`, `"not":{"type":"string"}`, `"minProperties":2`,
		`"description":"BranchDoc"`, `"x-vendor":"keepme"`,
	} {
		assert.Contains(t, string(entry.Value), kept, "the whole branch is preserved")
	}
	assert.Contains(t,
		diagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S/allOf/0"),
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
	spec := componentSpec(`    T:
      allOf:
        - {type: string, maxLength: 3}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "T").(*ir.Model)
	require.True(t, ok, "the composed node is still a model")
	assert.Empty(t, m.Properties, "a scalar branch declares no properties to merge")

	entry, ok := m.Unmodeled["openapi:allOf/0"]
	require.True(t, ok, "the whole branch is kept verbatim; got %+v", m.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `{"type":"string","maxLength":3}`, string(entry.Value))
	assert.Contains(t,
		diagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, "/components/schemas/T/allOf/0"),
		"declares a type that is not an object")
	assert.False(t, hasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
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
	spec := componentSpec(`    Shared: &br {type: string, maxLength: 3, description: Aliased}
    Aliased: {allOf: [*br]}
    Merged: {allOf: [{<<: *br}]}
    Overridden: {allOf: [{<<: *br, type: object, properties: {a: {type: string}}}]}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

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
				diagMessageAt(t, diags, diag.DegradedConstruct, tc.sev,
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
	spec := componentSpec(`    Base: {type: object, properties: {id: {type: string}}}
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
	requireNoErrorDiags(t, diags)
	for _, name := range []string{"TypedBranch", "UntypedBranch", "RequiredOnlyBranch"} {
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s should be a model", name)
		assert.Empty(t, m.Unmodeled,
			"%s: properties, required and a bare `type: object` are merged, not residue", name)
	}
	assert.Zero(t, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
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
	spec := componentSpec(`    Unknown:
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
	requireNoErrorDiags(t, diags)

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
			diagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
				"/components/schemas/"+name+"/allOf/0"),
			"kept verbatim under Unmodeled", name)
	}
}

// TestAllOf_EachInlineBranchKeyedSeparately: two branches that both leave residue
// must not overwrite each other's entry, and each is reported at its own branch.
func TestAllOf_EachInlineBranchKeyedSeparately(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Multi:
      allOf:
        - type: object
          properties: {a: {type: string}}
          description: FirstDoc
        - type: object
          properties: {b: {type: string}}
          minProperties: 1
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "Multi").(*ir.Model)
	require.True(t, ok, "Multi should be a model")
	require.Len(t, m.Properties, 2, "both branches still merge their properties")
	require.Len(t, m.Unmodeled, 2, "one entry per branch; got %+v", m.Unmodeled)
	assert.Contains(t, string(m.Unmodeled["openapi:allOf/0"].Value), `"description":"FirstDoc"`)
	assert.Contains(t, string(m.Unmodeled["openapi:allOf/1"].Value), `"minProperties":1`)

	for i, want := range []string{"description", "minProperties"} {
		at := fmt.Sprintf("/components/schemas/Multi/allOf/%d", i)
		assert.Contains(t, diagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at), want,
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
	spec := componentSpec(`    Base: {type: object, properties: {id: {type: string}}}
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
	requireNoErrorDiags(t, diags)
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
	assert.Equal(t, 1, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"one branch, one diagnostic, however many variants carry it; got %+v", diags)
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
	assert.False(t, hasDiag(diags, diag.UnattachableRequired),
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
	require.Equal(t, 1, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
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

	require.Equal(t, 1, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
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
	// survived. The component-level default in the repro is reported and kept
	// as residue (it binds a use of the type, so the type node has no field for
	// it), which is the one diagnostic here; see
	// TestProperty_UnquotedDateDefaultPreserved for the default path.
	spec := componentSpec(`    D:
      type: string
      format: date
      default: 2021-01-01
      enum: [2021-01-01, 2022-02-02]
`)
	doc, diags := lowerSpec(t, spec)
	require.Len(t, diags, 1, "the dates convert cleanly: the only diagnostic is the default's")
	assert.Equal(t, "/components/schemas/D/default", diags[0].Provenance.Pointer)
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
	assert.True(t, hasDiag(diags, diag.UnresolvedRef), "bad mapping target diagnostic")
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
	assert.True(t, hasDiag(diags, diag.UnresolvedRef))
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
    EBytes: {enum: [!!binary aGk=, !!binary Ynll]}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	want := map[string]ir.PrimKind{
		"EInt": ir.PrimInteger, "ENum": ir.PrimNumber, "EBool": ir.PrimBool,
		"ENoTypeBool": ir.PrimBool, "ENoTypeNum": ir.PrimNumber,
		"ENoTypeStr": ir.PrimString, "EBytes": ir.PrimBytes,
	}
	for name, prim := range want {
		e, ok := typeByName(doc, name).(*ir.Enum)
		require.True(t, ok, "%s is an enum", name)
		assert.Equal(t, prim, e.ValueType, "%s value type", name)
	}
}

// TestEnum_ByteMembersAreNamedAndTyped pins the pair of defects an undeclared
// ValueKind produced: a !!binary member is a ValueBytes, which the old
// classification described as a string and gave no text form at all, so every
// byte member of an enum arrived with an empty source name — two members, one
// name, no diagnostic.
func TestEnum_ByteMembersAreNamedAndTyped(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    EBytes: {enum: [!!binary aGk=, !!binary Ynll]}\n")
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	e, ok := typeByName(doc, "EBytes").(*ir.Enum)
	require.True(t, ok, "a homogeneous byte enum stays an enum")
	assert.Equal(t, ir.PrimBytes, e.ValueType, "byte members declare the byte primitive, not string")
	require.Len(t, e.Members, 2)
	assert.Equal(t, "aGk=", e.Members[0].Name.Source, "a byte member is named by its base64 text")
	assert.Equal(t, "Ynll", e.Members[1].Name.Source)
	assert.NotEqual(t, e.Members[0].Name.Source, e.Members[1].Name.Source,
		"distinct byte members must not collapse onto one name")
	assert.NotEmpty(t, e.Members[0].Name.Canonical, "a named member has canonical words")
}

func TestEnum_HeterogeneousBecomesUnionWithBadValue(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    Mixed:\n      enum: [active, .inf]\n")
	doc, diags := lowerSpec(t, spec)
	u, ok := typeByName(doc, "Mixed").(*ir.Union)
	require.True(t, ok, "heterogeneous enum lowers to a union of literals")
	assert.Len(t, u.Variants, 2)
	assert.True(t, hasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"heterogeneous-enum info diagnostic")
	assert.True(t, hasDiagAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
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
	assert.False(t, hasDiag(diags, diag.UnattachableRequired),
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
		ctx: lowering.New(0, docDeclaring("Cat", "Dog", "A/B"), ir.SourceInfo{}, ""),
		out: &ir.Document{Types: ir.TypeRegistry{}},
	}
	// A $ref to a declared component.
	id, ok := mappingTargetID(l.ctx, l.types, "#/components/schemas/Cat")
	require.True(t, ok)
	assert.Equal(t, ids.NamedType("/components/schemas/Cat"), id)
	// A bare schema name.
	id, ok = mappingTargetID(l.ctx, l.types, "Dog")
	require.True(t, ok)
	assert.Equal(t, ids.NamedType(ids.Ptr("components", "schemas", "Dog")), id)
	// A bare name that contains '/' but names an existing schema must resolve, not
	// dangle as a misclassified external $ref (issue #14, f07).
	id, ok = mappingTargetID(l.ctx, l.types, "A/B")
	require.True(t, ok)
	assert.Equal(t, ids.NamedType(ids.Ptr("components", "schemas", "A/B")), id)
	// An undeclared component and a genuine external ref are dropped, never
	// synthesized into a dangling ID.
	_, ok = mappingTargetID(l.ctx, l.types, "#/components/schemas/Ghost")
	assert.False(t, ok, "undeclared component target dropped")
	_, ok = mappingTargetID(l.ctx, l.types, "a.yaml#/A")
	assert.False(t, ok, "external target dropped")
	// A declared but empty-named component ("") is interned anonymously, so its
	// bare mapping name must resolve to that anon ID, not an unbacked ids.NamedType
	// (issue #14, f31). It gets a context of its own rather than being added to the
	// one above: the declared set is derived from the document now, so saying "and
	// also this one" means saying it to a document.
	empty := lowering.New(0, docDeclaring(""), ir.SourceInfo{}, "")
	id, ok = mappingTargetID(empty, l.types, "")
	require.True(t, ok)
	assert.Equal(t, ids.AnonType(ids.Ptr("components", "schemas", "")), id)
	assert.NotEqual(t, ids.NamedType(ids.Ptr("components", "schemas", "")), id)
}

func TestDiscriminatorDefault_ResolvesDeclaredComponent(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(docDeclaring("Cat"))
	d := &oas3.Discriminator{PropertyName: "kind", DefaultMapping: new("Cat")}

	id, diags := discriminatorDefault(l.ctx, l.types, d, "/components/schemas/Pet")
	assert.Equal(t, ids.NamedType("/components/schemas/Cat"), id)
	assert.Empty(t, diags, "a resolvable defaultMapping produces no diagnostic")
}

func TestDiscriminatorDefault_DroppedWhenUnresolved(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// "Missing" is neither a declared component nor an internal pointer, so the
	// defaultMapping does not resolve and is dropped with one error diagnostic.
	d := &oas3.Discriminator{PropertyName: "kind", DefaultMapping: new("Missing")}

	id, diags := discriminatorDefault(l.ctx, l.types, d, "/components/schemas/Pet")
	assert.Empty(t, id, "an unresolved defaultMapping yields no target")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnresolvedRef, diags[0].Code)
}

func TestDiscriminatorDefault_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	id, diags := discriminatorDefault(l.ctx, l.types, &oas3.Discriminator{PropertyName: "kind"}, "/components/schemas/Pet")
	assert.Empty(t, id)
	assert.Empty(t, diags)
}

// TestOneOf_CoDeclaredCompositionDistributes is the regression from
// reference-learnings §B11: an allOf composition co-declared with a oneOf used
// to survive while the union was dropped to Unmodeled. Both must survive, and
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
	assert.Empty(t, u.Unmodeled, "distribution loses nothing, so nothing is kept verbatim")

	for i, branch := range []string{"A", "B"} {
		v := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.NotNil(t, v.Base, "the enclosing allOf's sole $ref stays Base (ir-design §4.3)")
		assert.Equal(t, componentID("Base"), v.Base.Target)
		require.Len(t, v.Mixins, 1, "the branch joins as a mixin, the composition already having a base")
		assert.Equal(t, componentID(branch), v.Mixins[0].Target)
		assert.Equal(t, branch, u.Variants[i].Name.Hint)
	}
	assert.Equal(t, 1, countDiagsAt(diags, diag.CompositionLowering, ir.SeverityInfo),
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

// comboRefStealSpec is the co-declared union of Base with A|B, plus one $ref
// aimed at a union branch's own pointer. The two orders of the same four
// components must lower identically: the branch pointer denotes the branch
// schema in both, and the variants are Morphic's own nodes in both.
func comboRefStealSpec(branch int, outsiderFirst bool) string {
	outsider := fmt.Sprintf(
		"    Outsider: {type: object, properties: {x: {$ref: '#/components/schemas/Combo/oneOf/%d'}}}\n", branch)
	combo := `    Base: {type: object, properties: {id: {type: string}}}
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      allOf: [{$ref: '#/components/schemas/Base'}]
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`
	if outsiderFirst {
		return componentSpec(outsider + combo)
	}
	return componentSpec(combo + outsider)
}

// TestOneOf_CoDeclaredVariantNotStolenByRefToBranch is the regression for the
// pointer collision: the composed variants used to be interned at the branch
// pointers, which a $ref elsewhere in the document denotes too. Whichever
// lowered first took the pointer, so an unrelated reference could leave one
// variant a bare alias of its branch while its siblings carried the body — the
// disagreement ir-design §4.3 forbids, decided by declaration order.
func TestOneOf_CoDeclaredVariantNotStolenByRefToBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		branch        int
		outsiderFirst bool
	}{
		{"outsider declared first", 0, true},
		{"outsider declared last", 0, false},
		{"ref to the second branch", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, comboRefStealSpec(tc.branch, tc.outsiderFirst))
			requireNoErrorDiags(t, diags)

			u, ok := typeByName(doc, "Combo").(*ir.Union)
			require.True(t, ok)
			require.Len(t, u.Variants, 2)
			for i, branch := range []string{"A", "B"} {
				v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
				require.True(t, ok, "variant %d keeps the composed body", i)
				require.NotNil(t, v.Base)
				assert.Equal(t, componentID("Base"), v.Base.Target)
				require.Len(t, v.Mixins, 1)
				assert.Equal(t, componentID(branch), v.Mixins[0].Target)
			}

			// The outside reference gets the branch schema, which is what its
			// pointer names — never the conjunction the variant stands for.
			outsider, ok := typeByName(doc, "Outsider").(*ir.Model)
			require.True(t, ok)
			require.Len(t, outsider.Properties, 1)
			branchNode, ok := doc.Types[outsider.Properties[0].Type.Target].(*ir.Scalar)
			require.True(t, ok, "the branch pointer hoists an alias of the branch's target")
			require.NotNil(t, branchNode.Base)
			assert.Equal(t, componentID([]string{"A", "B"}[tc.branch]), branchNode.Base.Target)
		})
	}
}

// comboDiscriminatedSpec is the co-declared union of a discriminated Base with
// A|B. Every variant is written on the wire with the enclosing schema's tag,
// which the base's mapping supplies, so the base and the schemas naming it are
// permutable against each other — components lower in source order.
func comboDiscriminatedSpec(baseFirst bool) string {
	base := `    Base:
      type: object
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {combo: '#/components/schemas/Combo'}
`
	rest := `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      allOf: [{$ref: '#/components/schemas/Base'}]
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`
	if baseFirst {
		return componentSpec(base + rest)
	}
	return componentSpec(rest + base)
}

// TestOneOf_CoDeclaredDistributionIsOrderIndependent states the property the
// pointer collision broke, over the whole compiled document rather than one
// node: the same components in two declaration orders compile to the same IR.
// Each case permutes a different site the distribution shares with something
// outside it — an outside $ref aimed at either union branch, and the
// discriminated base the variants take their tag from.
//
// Comparing whole documents covers the diagnostic list as well as the registry,
// which is why these cases permute only components that emit none: diagnostics
// are appended in traversal order, so a document whose two orders diagnose the
// same facts still lists them in two orders. The registry is then compared a
// second time with nothing excluded at all, because these shapes settle their
// name hints identically both ways and orderInvariantIR's Hint exclusion would
// otherwise hide a regression there.
func TestOneOf_CoDeclaredDistributionIsOrderIndependent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// spec writes the document with the permuted component declared first.
		spec func(first bool) string
	}{
		{"outside ref at the first branch", func(f bool) string { return comboRefStealSpec(0, f) }},
		{"outside ref at the second branch", func(f bool) string { return comboRefStealSpec(1, f) }},
		{"discriminated base", comboDiscriminatedSpec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first, diags := parseFull(t, tc.spec(true))
			requireNoErrorDiags(t, diags)
			last, diags := parseFull(t, tc.spec(false))
			requireNoErrorDiags(t, diags)

			assert.Empty(t, cmp.Diff(first, last, orderInvariantIR()...),
				"declaring the permuted component before or after the union must not change the IR")
			assert.Empty(t, cmp.Diff(first.Types, last.Types), "nor any name hint in the registry")
		})
	}
}

// branchAliasSpec writes a composition whose first branch is a $ref carrying a
// sibling — so the branch position owns a node — with an outside component
// naming that branch pointer. hostFirst permutes which of the two is declared
// first, and kind selects the composition keyword.
func branchAliasSpec(kind string, hostFirst bool) string {
	host := "    Host:\n      " + kind + ":\n" +
		"        - $ref: '#/components/schemas/Base'\n" +
		"          description: narrowed at this branch\n"
	if kind != "allOf" {
		host += "        - type: string\n" // a union needs a second variant
	}
	rest := "    Base: {type: object, properties: {a: {type: string}}}\n" +
		"    Outside: {$ref: '#/components/schemas/Host/" + kind + "/0'}\n"
	if hostFirst {
		return componentSpec(host + rest)
	}
	return componentSpec(rest + host)
}

// TestComposition_BranchAliasIsOrderIndependent pins the hint on the node a
// composition branch and an outside $ref both reach. Only the first lowering to
// arrive interns it, so the two must derive the same hint or the document
// depends on declaration order — silently, since either spelling is a valid hint
// and no check compares them.
//
// Hints are compared here rather than excluded (orderInvariantIR drops them for
// shapes that legitimately settle two ways): the hint *is* what differed.
func TestComposition_BranchAliasIsOrderIndependent(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"allOf", "oneOf", "anyOf"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			first, diags := parseFull(t, branchAliasSpec(kind, true))
			requireNoErrorDiags(t, diags)
			last, diags := parseFull(t, branchAliasSpec(kind, false))
			requireNoErrorDiags(t, diags)

			branch := ir.TypeID("t/anon/components/schemas/Host/" + kind + "/0")
			require.Contains(t, first.Types, branch, "the branch position owns a node")
			assert.Empty(t, cmp.Diff(first.Types, last.Types),
				"declaring the composition before or after the outside $ref must not change the registry")
		})
	}
}

// TestOneOf_CoDeclaredVariantCarriesDiscriminatorValue pins the tag the variants
// inherit. The enclosing schema is an allOf subtype of a discriminated base, so
// every variant is written on the wire with that subtype's tag — and the tag
// comes from the base's mapping entry for the *enclosing* schema, since that is
// the node the mapping names.
func TestOneOf_CoDeclaredVariantCarriesDiscriminatorValue(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, comboDiscriminatedSpec(true))
	requireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 2)
	for i := range u.Variants {
		v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok)
		assert.Equal(t, "combo", v.DiscriminatorValue,
			"variant %d carries the mapping key naming the enclosing schema, not its own hint", i)
	}
}

// TestOneOf_CoDeclaredAdditionalPropsHintNamesTheBody covers the naming of the
// one node every variant shares. additionalProperties is declared once, on the
// enclosing schema, and lowers at its pointer — so the first variant to reach it
// must not stamp its own branch name on the shared node.
func TestOneOf_CoDeclaredAdditionalPropsHintNamesTheBody(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Combo:
      type: object
      properties: {kind: {type: string}}
      additionalProperties: {type: object, properties: {v: {type: string}}}
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	u := typeByName(doc, "Combo").(*ir.Union)
	v, ok := doc.Types[u.Variants[0].Type.Target].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, v.AdditionalProps)
	assert.Equal(t, "Combo_value", doc.Types[v.AdditionalProps.Value.Target].Common().Name.Hint)
}

// TestOneOf_CoDeclaredNonModelBranchIsCarriedAsWritten pins what §4.3 says
// Base/Mixins may name. JSON Schema lets a composition conjoin any schema, so a
// branch can reference a scalar, an enum or another union; the referent is
// carried as written, because narrowing it would mean dropping a branch the
// source declared. fillAllOf classifies an allOf entry the same way.
func TestOneOf_CoDeclaredNonModelBranchIsCarriedAsWritten(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Scalar: {type: string, minLength: 2}
    Choice: {enum: [a, b]}
    Combo:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/Scalar'}, {$ref: '#/components/schemas/Choice'}]
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	u, ok := typeByName(doc, "Combo").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 2)
	for i, want := range []string{"Scalar", "Choice"} {
		v, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok)
		require.NotNil(t, v.Base, "variant %d conjoins its branch", i)
		assert.Equal(t, componentID(want), v.Base.Target)
	}
	assert.IsType(t, &ir.Scalar{}, doc.Types[componentID("Scalar")])
	assert.IsType(t, &ir.Enum{}, doc.Types[componentID("Choice")])
}

// TestOneOf_CoDeclaredUnresolvableBranchIsNotDistributed covers the branches
// that are $ref-shaped but name nothing this compilation can resolve. Each
// would otherwise half-distribute — the variant carrying the body while its
// branch vanished — so all of them keep the verbatim lowering, which loses
// nothing, and each still reports the broken reference.
func TestOneOf_CoDeclaredUnresolvableBranchIsNotDistributed(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    A: {type: object, properties: {a: {type: string}}}
    Undeclared:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/Ghost'}, {$ref: '#/components/schemas/A'}]
    CrossDocument:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: 'other.yaml#/components/schemas/A'}]
    EmptyRef:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: ''}]
    NoSuchPointer:
      type: object
      properties: {kind: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A/properties/missing'}]
`)
	doc, diags := lowerSpec(t, spec)

	for _, name := range []string{"Undeclared", "CrossDocument", "EmptyRef", "NoSuchPointer"} {
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s keeps its structural body rather than becoming a union", name)
		entry, ok := m.Unmodeled["openapi:oneOf"]
		require.True(t, ok, "%s keeps every branch verbatim", name)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, "%s", name)
		assert.Contains(t,
			diagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/"+name),
			"names no referent this compilation resolves", name)
		// One error per offending branch, at the branch itself.
		assert.Contains(t,
			diagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, "/components/schemas/"+name+"/oneOf/0"),
			"resolves to nothing this document declares", name)
	}
	for _, d := range diags {
		assert.NotEqual(t, "/components/schemas/Undeclared/oneOf/1", d.Provenance.Pointer,
			"the branch beside the broken one resolves, so nothing is reported against it")
	}
}

// TestOneOf_CoDeclaredNotDistributedReasons pins the five shapes distribution
// declines, each kept verbatim under ReasonDegradedLowering with its own reason
// in the diagnostic (ir-design §4.8). Half-distributing any of them would leave
// a Union whose variants disagree about whether they carry the body. The
// messages are asserted, not just the code: the reasons are the only thing
// telling four otherwise identical diagnostics apart.
func TestOneOf_CoDeclaredNotDistributedReasons(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    A: {type: object, properties: {a: {type: string}}}
    NotAModel:
      const: fixed
      oneOf: [{$ref: '#/components/schemas/A'}]
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

	for name, reason := range map[string]string{
		"NotAModel":       "the body is not a model, so it carries no composition to distribute into",
		"InlineBranch":    "a branch is written inline, so it names no referent to conjoin the body with",
		"NullBranch":      "a branch is written inline, so it names no referent to conjoin the body with",
		"BothCombinators": "oneOf and anyOf are both declared, so distributing either would drop the other",
		"Discriminated":   "a declared discriminator binds the branches by name, which distributing them would break",
	} {
		entry, ok := typeByName(doc, name).Common().Unmodeled["openapi:oneOf"]
		require.True(t, ok, "%s keeps its union verbatim", name)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, "%s", name)
		assert.Contains(t,
			diagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/"+name),
			reason, name)
	}
	both := typeByName(doc, "BothCombinators").(*ir.Model)
	_, ok := both.Unmodeled["openapi:anyOf"]
	assert.True(t, ok, "the second combinator is kept too, which is why neither is distributed")
	nullBranch := typeByName(doc, "NullBranch").(*ir.Model)
	assert.Contains(t, string(nullBranch.Unmodeled["openapi:oneOf"].Value), `"null"`,
		"a null branch is written inline, so it blocks distribution rather than lifting to Nullable")
	assert.Equal(t, 5, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"each declined shape is reported once; got %+v", diags)
}

// TestRawMappingKeys_OnlyEnumeratesAMapping pins the shape guards on the branch
// residue's key reader. It is handed whatever the source wrote at a branch, so a
// sequence or a bare scalar reaches it as readily as a mapping, and the residue
// derivation depends on it reporting no keys rather than guessing at some.
//
// The alias and `<<` rows are the same guard read the other way: a node written
// by reference stands for keys, so answering "none" there would report a branch
// that declares plenty as one the merge consumed entirely.
func TestRawMappingKeys_OnlyEnumeratesAMapping(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\nb: 2\n"), &doc))
	require.Equal(t, yaml.DocumentNode, doc.Kind)

	cases := []struct {
		name string
		node *yaml.Node
		want []string
	}{
		{"a nil node has no keys", nil, nil},
		{"a sequence is not a mapping", yamlNode(t, "- a\n- b\n"), nil},
		{"a bare scalar is not a mapping", yamlNode(t, "plain"), nil},
		{"a mapping yields its keys in source order", yamlNode(t, "b: 1\na: 2\n"), []string{"b", "a"}},
		{"a document unwraps to the mapping inside it", &doc, []string{"a", "b"}},
		{"an alias yields the anchored mapping's keys",
			useValue(t, "anchor: &a {b: 1, c: 2}\nuse: *a\n"), []string{"b", "c"}},
		{"an alias to a scalar is still not a mapping",
			useValue(t, "anchor: &a plain\nuse: *a\n"), nil},
		{"a merge key yields the keys it merges in",
			useValue(t, "anchor: &a {b: 1, c: 2}\nuse: {<<: *a}\n"), []string{"b", "c"}},
		{"an explicit key beats the one it merges over",
			useValue(t, "anchor: &a {b: 1, c: 2}\nuse: {c: 9, <<: *a}\n"), []string{"c", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, rawMappingKeys(tc.node))
		})
	}
}

// useValue parses src — an anchor plus a `use:` key written against it — and
// returns the raw node at `use` undereferenced, so a value spelled `*anchor`
// arrives as the alias node a branch reader is really handed rather than as the
// mapping it stands for.
func useValue(t *testing.T, src string) *yaml.Node {
	t.Helper()
	node := annotation.RawChildNode(yamlNode(t, src), "use")
	require.NotNil(t, node, `src writes no "use" key`)
	return node
}

// TestEnumMemberForm_ClassifiesEveryValueKind pins the classification arm by arm,
// including the kinds OpenAPI cannot produce. ValueKind is IR-wide, and
// TestEnumMemberForm_NamesEveryValueKind requires every member of it to be named
// here — this is what says the naming is a decision rather than a formality.
func TestEnumMemberForm_ClassifiesEveryValueKind(t *testing.T) {
	t.Parallel()
	admissible := []struct {
		name string
		val  ir.Value
		prim ir.PrimKind
		text string
	}{
		{"string", ir.Value{Kind: ir.ValueString, Str: "s"}, ir.PrimString, "s"},
		{"symbol", ir.Value{Kind: ir.ValueSymbol, Str: "ok"}, ir.PrimString, "ok"},
		{"number", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("1.5")}, ir.PrimNumber, "1.5"},
		{"bool", ir.Value{Kind: ir.ValueBool, Bool: true}, ir.PrimBool, "true"},
		{"bytes", ir.Value{Kind: ir.ValueBytes, Bytes: []byte("hi")}, ir.PrimBytes, "aGk="},
	}
	for _, tc := range admissible {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prim, text, ok := enumMemberForm(tc.val)
			require.True(t, ok, "%s is admissible as an enum member", tc.name)
			assert.Equal(t, tc.prim, prim)
			assert.Equal(t, tc.text, text)
		})
	}

	refused := []ir.ValueKind{
		ir.ValueNull, ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor,
		ir.ValueKind("a kind ir does not declare"),
	}
	for _, kind := range refused {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			prim, text, ok := enumMemberForm(ir.Value{Kind: kind})
			assert.False(t, ok, "%q has no place in an Enum", kind)
			assert.Empty(t, prim, "a refused kind asserts no primitive")
			assert.Empty(t, text)
		})
	}
}

// TestBranchPointerHint_Shapes pins which pointers name a composition branch.
//
// The parse decides whether hoistSubSchema answers what the composition would,
// so a pointer it misreads either reintroduces the disagreement (GitHub #181) or
// invents a variant hint for something that is not a branch. The rows that are
// not branches matter as much as the rows that are: `oneOf` is a legal property
// name, and a schema keyword taking numbered children is not necessarily a
// composition.
func TestBranchPointerHint_Shapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pointer string
		want    string
	}{
		{name: "a oneOf branch", pointer: "/components/schemas/S/oneOf/0", want: "variant_0"},
		{name: "an anyOf branch", pointer: "/components/schemas/S/anyOf/12", want: "variant_12"},
		{name: "an allOf branch", pointer: "/components/schemas/S/allOf/1", want: "variant_1"},
		{
			name:    "a branch of a schema reached through a property named oneOf",
			pointer: "/components/schemas/S/properties/oneOf/anyOf/0", want: "variant_0",
		},

		{name: "prefixItems takes indices but composes nothing", pointer: "/components/schemas/S/prefixItems/0"},
		{name: "items is not indexed at all", pointer: "/components/schemas/S/items"},
		{name: "a property named oneOf is not a branch", pointer: "/components/schemas/S/properties/oneOf"},
		{name: "a non-numeric child of a composition keyword", pointer: "/components/schemas/S/oneOf/x"},
		{name: "a negative index is not a segment the compiler writes", pointer: "/components/schemas/S/oneOf/-1"},
		{name: "an empty index", pointer: "/components/schemas/S/oneOf/"},
		{name: "a bare segment", pointer: "oneOf"},
		{name: "the empty pointer", pointer: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := branchPointerHint(tc.pointer)
			assert.Equal(t, tc.want != "", ok, "pointer %q", tc.pointer)
			assert.Equal(t, tc.want, got, "pointer %q", tc.pointer)
		})
	}
}

// TestBranchHint_AgreesWithThePointerWalk states the property the two
// derivations exist to share, at the functions rather than through a compile: for
// an inline branch, what the composition names the node and what a pointer walk
// names it are the same string. Whichever lowering arrives first, the node ends
// up with the same hint.
func TestBranchHint_AgreesWithThePointerWalk(t *testing.T) {
	t.Parallel()
	for i := range 3 {
		inline := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
		pointer := "/components/schemas/Host/oneOf/" + strconv.Itoa(i)

		fromComposition := branchHint(inline, i)
		fromPointer, ok := branchPointerHint(pointer)
		require.True(t, ok, "the branch pointer is recognized as one")
		assert.Equal(t, fromComposition, fromPointer,
			"branch %d must be named the same whichever lowering interns it first", i)
		assert.Equal(t, fromComposition, subSchemaHint(inline, pointer),
			"and subSchemaHint is the path that actually asks")
	}
}
