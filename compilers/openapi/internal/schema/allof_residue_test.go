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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, diags := schema.LowerSpec(t, spec)
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

// TestAllOf_UnpreservableBranchIsNotAnnounced is
// TestUnhomedApplicator_UnpreservableKeywordIsNotAnnounced's pairing on the
// composition path: a branch the merge does not consume whole is normally kept
// beside the model and reported, and when the keeping fails the report must not
// happen either.
func TestAllOf_UnpreservableBranchIsNotAnnounced(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    M:
      allOf:
        - {type: string, maxLength: 3, x-t: `+unpreservableValue+`}
`))

	td := typeByName(doc, "M")
	require.NotNil(t, td)
	assert.NotContains(t, td.Common().Unmodeled, "openapi:allOf/0", "the branch did not convert")
	assert.True(t, openapitest.HasDiag(diags, diag.UnpreservableConstruct))
	assert.Empty(t, openapitest.PreservationClaims(diags),
		"no degradation is announced for a branch that was not kept")
}
