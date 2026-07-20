package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

func TestConstraintsFromSchema_NilSchema(t *testing.T) {
	t.Parallel()
	c, diags := constraintsFromSchema(nil)
	assert.Nil(t, c)
	assert.Nil(t, diags)
}

func TestNodeToRaw(t *testing.T) {
	t.Parallel()
	assert.Nil(t, nodeToRaw(nil), "nil node")
	assert.Nil(t, nodeToRaw(&yaml.Node{Kind: yaml.Kind(99)}), "decode error")
	assert.Nil(t, nodeToRaw(yamlNode(t, "1: a\n2: b")), "int-key map: json marshal error")
	raw := nodeToRaw(yamlNode(t, "{a: 1}"))
	assert.JSONEq(t, `{"a":1}`, string(raw))
}

func TestRawChildNode(t *testing.T) {
	t.Parallel()
	assert.Nil(t, rawChildNode(nil, "x"), "nil root")
	assert.Nil(t, rawChildNode(scalarNode("!!str", "x"), "k"), "non-mapping root")

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\nb: 2"), &doc))
	// doc is a DocumentNode wrapping the mapping — exercises the unwrap branch.
	got := rawChildNode(&doc, "b")
	require.NotNil(t, got)
	assert.Equal(t, "2", got.Value)
	assert.Nil(t, rawChildNode(&doc, "missing"), "absent key")
}

func TestRawPropertyNode_NilSchema(t *testing.T) {
	t.Parallel()
	assert.Nil(t, rawPropertyNode(nil, "x"))
}

func TestResolvers_NilInputs(t *testing.T) {
	t.Parallel()
	assert.Nil(t, resolvePathItem(nil))
	assert.Nil(t, resolveResponse(nil))
	assert.Nil(t, resolveHeader(nil))
	assert.Nil(t, resolveCallback(nil))
	assert.Nil(t, resolveParameter(nil))
	assert.Nil(t, resolveRequestBody(nil))
	assert.Nil(t, resolveExample(nil))
	assert.Nil(t, resolveSecurityScheme(nil))
	_, ok := paramKey(nil)
	assert.False(t, ok)
}

func TestPreserveKeyword_NilRaw(t *testing.T) {
	t.Parallel()
	l := &lowerer{}
	m := &ir.Model{}
	l.preserveKeyword(m, "openapi:not", nil, "/p", "not")
	assert.Nil(t, m.Extensions, "nil raw is a no-op")
	assert.Empty(t, l.diags)
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
	id, err := mappingTargetID("#/components/schemas/Cat")
	require.NoError(t, err)
	assert.Equal(t, namedTypeID("/components/schemas/Cat"), id)
	id, err = mappingTargetID("Dog") // bare name
	require.NoError(t, err)
	assert.Equal(t, namedTypeID(ptr("components", "schemas", "Dog")), id)
}

func TestRefTypeID_Errors(t *testing.T) {
	t.Parallel()
	_, err := refTypeID("")
	require.Error(t, err, "empty ref")
	_, err = refTypeID("#")
	require.Error(t, err, "unsupported ref form")
	id, err := refTypeID("other.yaml#/components/schemas/X")
	require.NoError(t, err, "external doc ref")
	assert.Contains(t, string(id), "ext/")
}

func TestStatusRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code     string
		from, to int
	}{
		{"default", 0, 0},
		{"200", 200, 200},
		{"4XX", 400, 499},
		{"5xx", 500, 599},
		{"20A", 0, 0}, // non-numeric, non-range → catch-all
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			r := statusRange(tc.code)
			assert.Equal(t, tc.from, r.From)
			assert.Equal(t, tc.to, r.To)
		})
	}
}

func TestFaultFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "client", faultFor(ir.StatusRange{From: 404, To: 404}))
	assert.Equal(t, "server", faultFor(ir.StatusRange{From: 503, To: 503}))
	assert.Equal(t, "", faultFor(ir.StatusRange{}))
}

func TestFirstPathSegment_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", firstPathSegment("/"))
	assert.Equal(t, "users", firstPathSegment("/users/{id}"))
}
