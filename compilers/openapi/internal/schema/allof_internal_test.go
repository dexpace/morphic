package schema

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

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
		{"a sequence is not a mapping", openapitest.YAMLNode(t, "- a\n- b\n"), nil},
		{"a bare scalar is not a mapping", openapitest.YAMLNode(t, "plain"), nil},
		{"a mapping yields its keys in source order", openapitest.YAMLNode(t, "b: 1\na: 2\n"), []string{"b", "a"}},
		{"a document unwraps to the mapping inside it", &doc, []string{"a", "b"}},
		{"an alias yields the anchored mapping's keys",
			useValue(t, "anchor: &a {b: 1, c: 2}\nuse: *a\n"), []string{"b", "c"}},
		{"an alias to a scalar is still not a mapping",
			useValue(t, "anchor: &a plain\nuse: *a\n"), nil},
		{"a merge key yields the keys it merges in",
			useValue(t, "anchor: &a {b: 1, c: 2}\nuse: {<<: *a}\n"), []string{"b", "c"}},
		{"an explicit key beats the one it merges over, merged keys first",
			useValue(t, "anchor: &a {b: 1, c: 2}\nuse: {c: 9, <<: *a}\n"), []string{"b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, rawMappingKeys(tc.node))
		})
	}
}

// TestAllOfBranches_DepthCapKeepsWrapperVerbatim pins the bound on flattening: a
// chain of wrappers as deep as the cap is flattened whole, and one level more
// leaves the wrapper at the cap verbatim under a key spelling its path, with the
// ref inside it unclassified and everything above it flattened. The diagnostic
// blames the depth, not allOf.
func TestAllOfBranches_DepthCapKeepsWrapperVerbatim(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		wrappers int
		flat     bool
	}{
		{"at the cap", MaxAllOfFlattenDepth, true},
		{"one past the cap", MaxAllOfFlattenDepth + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			b.WriteString("    A: {type: object, properties: {a: {type: string}}}\n    M:\n      allOf:\n")
			for i := range tc.wrappers {
				b.WriteString(strings.Repeat("  ", i) + "        - allOf:\n")
			}
			b.WriteString(strings.Repeat("  ", tc.wrappers) + "        - {$ref: '#/components/schemas/A'}\n")

			doc, diags := LowerSpec(t, openapitest.ComponentSpec(b.String()))
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/M")].(*ir.Model)
			require.True(t, ok, "M should be a model")
			if tc.flat {
				require.NotNil(t, m.Base, "the ref at the bottom is classified")
				assert.Equal(t, ir.TypeID("t/openapi/components/schemas/A"), m.Base.Target)
				assert.Empty(t, m.Unmodeled)
				return
			}
			assert.Nil(t, m.Base, "the ref sits inside the wrapper kept verbatim")
			path := strings.TrimSuffix(strings.Repeat("allOf/0/", MaxAllOfFlattenDepth+1), "/")
			entry, ok := m.Unmodeled["openapi:"+path]
			require.True(t, ok, "the wrapper at the cap is kept under its path; got %v", m.Unmodeled)
			assert.Equal(t, jsontext.Pointer("/components/schemas/M/"+path), entry.Provenance.Pointer)
			assert.Len(t, m.Unmodeled, 1)
			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct,
				ir.SeverityInfo, "/components/schemas/M/"+path)
			assert.Equal(t, "inline allOf branch is nested past the flattening depth of "+strconv.Itoa(MaxAllOfFlattenDepth)+
				", so its own allOf was not flattened; only its properties and required list compose, and the branch is kept verbatim under Unmodeled", msg)
		})
	}
}

// TestAllOfBranches_KeysSpellEachEntrysPath pins, for the entries of a nested
// composition, the key each is kept under, which spells its path from the
// composed schema, and that a wrapper follows its own branches.
func TestAllOfBranches_KeysSpellEachEntrysPath(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    M:
      allOf:
        - {type: object}
        - allOf:
            - {type: object}
            - allOf:
                - {type: object}
                - {type: object}
`))
	decl, ok := l.Ctx.Doc.Components.Schemas.Get("M")
	require.True(t, ok)
	comp := allOfBranches(decl.GetSchema(), "/components/schemas/M")
	entries := comp.branches
	require.Len(t, entries, 6, "four inline branches and two wrappers")
	wantKeys := []string{
		"openapi:allOf/0",
		"openapi:allOf/1/allOf/0",
		"openapi:allOf/1/allOf/1/allOf/0",
		"openapi:allOf/1/allOf/1/allOf/1",
		"openapi:allOf/1/allOf/1",
		"openapi:allOf/1",
	}
	var inline int
	for i, e := range entries {
		assert.Equal(t, wantKeys[i], comp.branchKey(e.pointer))
		if !e.wrapper {
			inline++
		}
	}
	assert.Equal(t, 4, inline)
	assert.True(t, entries[len(entries)-1].wrapper, "a wrapper follows its own branches")
}

// TestWalkAllOf_StopsWhenTheConsumerDoes pins that the walk and the schema view
// end when the loop reading them breaks: at a leaf, at a wrapper that follows
// its entries, and at a $ref branch the view yields.
func TestWalkAllOf_StopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    A: {type: object}
    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
        - {type: object}
    Flat:
      allOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/A'}
`))
	decl, ok := l.Ctx.Doc.Components.Schemas.Get("M")
	require.True(t, ok)
	m := decl.GetSchema()

	var all []bool // wrapper flag of every step, in order
	for st := range walkAllOf(m) {
		all = append(all, st.wrapper)
	}
	assert.Equal(t, []bool{false, true, false}, all, "the ref, its wrapper, then the inline branch")

	for stop := range all {
		var seen int
		for range walkAllOf(m) {
			seen++
			if seen > stop {
				break
			}
		}
		assert.Equal(t, stop+1, seen, "the walk yields nothing after the loop breaks")
	}

	var refs int
	for range allOfRefBranches(m) {
		refs++
		break
	}
	assert.Equal(t, 1, refs)

	flatDecl, ok := l.Ctx.Doc.Components.Schemas.Get("Flat")
	require.True(t, ok)
	var flatRefs int
	for range allOfRefBranches(flatDecl.GetSchema()) {
		flatRefs++
		break
	}
	assert.Equal(t, 1, flatRefs, "a flat list stops at the first $ref when the loop breaks")
}

// TestAllOfBranches_DepthCapNamesWhatElseTheWrapperWrites pins that a wrapper
// kept at the cap lists the keywords it writes besides allOf, so the message
// says both why it was kept and what rides along. A wrapper whose type is not an
// object reads as a warning, which names the depth as well.
func TestAllOfBranches_DepthCapNamesWhatElseTheWrapperWrites(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		sibling  string
		severity ir.Severity
		want     string
	}{
		{"description", "description: Capped", ir.SeverityInfo, "the branch (description) is kept verbatim"},
		{"non-object type", "type: string", ir.SeverityWarning, "the branch (type) is kept verbatim"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			b.WriteString("    A: {type: object, properties: {a: {type: string}}}\n    M:\n      allOf:\n")
			for i := range MaxAllOfFlattenDepth {
				b.WriteString(strings.Repeat("  ", i) + "        - allOf:\n")
			}
			pad := strings.Repeat("  ", MaxAllOfFlattenDepth)
			b.WriteString(pad + "        - " + tc.sibling + "\n" + pad + "          allOf:\n" +
				pad + "            - {$ref: '#/components/schemas/A'}\n")

			_, diags := LowerSpec(t, openapitest.ComponentSpec(b.String()))
			openapitest.RequireNoErrorDiags(t, diags)
			path := strings.TrimSuffix(strings.Repeat("allOf/0/", MaxAllOfFlattenDepth+1), "/")
			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct,
				tc.severity, "/components/schemas/M/"+path)
			assert.Contains(t, msg, "nested past the flattening depth of "+strconv.Itoa(MaxAllOfFlattenDepth))
			assert.Contains(t, msg, "was not flattened")
			assert.Contains(t, msg, tc.want)
			assert.NotContains(t, msg, "allOf)", "allOf is not listed as a kept keyword")
			assert.NotContains(t, msg, ", allOf")
		})
	}
}
