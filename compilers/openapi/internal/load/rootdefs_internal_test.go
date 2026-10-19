package load

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestDefsRules_ReadRawYAMLThroughAnIndex pins that each reader of the $defs
// rule in load reads raw YAML, where a root definition sits, through an index:
// the held references', the cycle model's and the mapping targets'. Asking the
// library instead scans the mapping once per reference (GitHub #778).
func TestDefsRules_ReadRawYAMLThroughAnIndex(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, twoDefsSpec)
	assert.True(t, heldPass(t, doc).rule.Indexed(), "the held references'")
	r, _ := newReach(t.Context(), maxReachWork, root, doc, Siblings{})
	assert.True(t, r.rule.Indexed(), "the cycle model's")
	assert.True(t, newMappings(sourceDocument{path: "spec.yaml"}, doc, references.ResolveOptions{}, nil).rule.Indexed(),
		"the mapping targets'")
}

// TestReach_DefsEdgeLandsOnARootDefinition pins the cycle model's edge for a
// reference the rule answers at the document's root: the raw YAML written
// there, so a chain back through it is a cycle the model refuses.
func TestReach_DefsEdgeLandsOnARootDefinition(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"
	doc, root := buildDoc(t, head+"$defs:\n  d: {type: string}\ncomponents:\n  schemas:\n    A: {$ref: '#/$defs/d'}\n")
	r, _ := newReach(t.Context(), maxReachWork, root, doc, Siblings{})
	a := r.tree.walk(r.tree.root, []string{"components", "schemas", "A"})
	d := r.tree.walk(r.tree.root, []string{"$defs", "d"})
	require.NotNil(t, a)
	require.NotNil(t, d)
	held, ok := r.held[a]
	require.True(t, ok)
	assert.Equal(t, []*yaml.Node{d}, held.members)

	assertRefusedByReach(t, head+"$defs:\n  d: {$ref: '#/components/schemas/A'}\ncomponents:\n  schemas:\n    A: {$ref: '#/$defs/d'}\n")
}

// TestLoad_ARootDefinitionResolvesAsTheResolverReadsIt pins GitHub #910: a
// "#/$defs/..." reference into a "$defs" written at the document's root
// resolves to the definition there, before an ancestor's of the same name, and
// a chain through it goes on from there. The root's key is still unknown to
// the OpenAPI object, which is a warning.
func TestLoad_ARootDefinitionResolvesAsTheResolverReadsIt(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n" +
		"$defs:\n  n: {type: string}\n  hop: {$ref: '#/components/schemas/Z'}\n" +
		"components:\n  schemas:\n" +
		"    A: {type: object, $defs: {n: {type: integer}}, properties: {p: {$ref: '#/$defs/n'}, q: {$ref: '#/$defs/hop'}}}\n" +
		"    Z: {type: boolean}\n"

	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})

	require.NoError(t, err)
	require.NotNil(t, got)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "%s %s", d.Code, d.Message)
	}
	a, ok := got.Doc.Components.Schemas.Get("A")
	require.True(t, ok)
	for name, want := range map[string]jsontext.Pointer{"p": "/$defs/n", "q": "/$defs/hop"} {
		prop, ok := a.GetSchema().GetProperties().Get(name)
		require.True(t, ok, name)
		info := prop.GetReferenceResolutionInfo()
		require.NotNil(t, info, "%s resolved", name)
		assert.Equal(t, want, jsontext.Pointer(info.AbsoluteReference.GetJSONPointer()), name)
	}
	q, _ := a.GetSchema().GetProperties().Get("q")
	require.NotNil(t, q.GetResolvedSchema())
	assert.Equal(t, []oas3.SchemaType{oas3.SchemaTypeBoolean}, q.GetResolvedSchema().GetSchema().GetType(), "the chain goes on to Z")
}

// shadowChildEnv hands a child (see inChild) a spec to load, in place of the
// test it re-ran: a reference the resolver reads into a cycle overflows the
// stack, which ends the process.
const shadowChildEnv = "MORPHIC_SHADOW_CHILD_SPEC"

// TestLoad_ARootDefinitionItsSchemaShadowsIsLeftUnresolved pins the one
// reading of GitHub #910 load cannot hand the resolver: a schema with no $id
// that spells "#/$defs/e" and defines an e of its own, where the root defines
// one too (docs/parser-library.md#root-defs). Once a chain has reached it, the
// resolver reads its own e, so a reference back to it from there closes a
// cycle the root's reading has none of. Each row, in both declaration orders,
// is reported unresolved at the schema, in a child, so an overflow fails the
// test rather than ending it.
func TestLoad_ARootDefinitionItsSchemaShadowsIsLeftUnresolved(t *testing.T) {
	if spec, ok := asChild(shadowChildEnv); ok {
		loadShadowed(t, spec)
		return
	}
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"
	for name, parts := range map[string][2]string{
		"its own definition leads back to it": {
			"components:\n  schemas:\n    A: {$ref: '#/$defs/e', $defs: {e: {$ref: '#/components/schemas/A'}}}\n",
			"$defs:\n  e: {type: object}\n",
		},
		"a chain reaches it first": {
			"components:\n  schemas:\n    C: {$ref: '#/components/schemas/A'}\n    A: {$ref: '#/$defs/e', $defs: {e: {type: integer}}}\n",
			"$defs:\n  e: {type: string}\n",
		},
	} {
		for order, spec := range map[string]string{"root last": head + parts[0] + parts[1], "root first": head + parts[1] + parts[0]} {
			out, end := inChild(t, "TestLoad_ARootDefinitionItsSchemaShadowsIsLeftUnresolved", shadowChildEnv, spec)
			require.Equal(t, childPassed, end, "%s, %s:\n%.1500s", name, order, out)
		}
	}
}

// loadShadowed is the child's side: it loads spec and requires the shadowed
// reference at A, alone, to be reported unresolved and why.
func loadShadowed(t *testing.T, spec string) {
	t.Helper()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})
	require.NoError(t, err)
	var errs []ir.Diagnostic
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			errs = append(errs, d)
		}
	}
	require.Len(t, errs, 1, "%v", errs)
	assert.Equal(t, diag.UnresolvedRef, errs[0].Code)
	assert.Equal(t, jsontext.Pointer("/components/schemas/A"), errs[0].Provenance.Pointer)
	assert.True(t, strings.HasSuffix(errs[0].Message, shadowedReason), errs[0].Message)
}
