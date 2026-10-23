package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_APointerTokenPastUFFFFIsRefusedUpstream pins GitHub #516. The
// library caps its reference-token character class at U+FFFF
// (jsonpointer/navigation.go, tokenRegex), though RFC 6901 admits up to
// U+10FFFF, so a reference object through an emoji key is refused and one
// through a Basic Multilingual Plane key resolves. A schema $ref, which the
// compiler binds itself, resolves through either, in either declaration order.
//
// The twins differ only in that key. A refused path item mount is lost. When
// the astral twin mounts like the other, the library is fixed: assert that and
// close #516.
func TestCompile_APointerTokenPastUFFFFIsRefusedUpstream(t *testing.T) {
	t.Parallel()
	twin := func(key string) string {
		return `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  "/` + key + `":
    get: {operationId: getKey, responses: {"200": {description: ok}}}
  /mount: {$ref: '#/paths/~1` + key + `'}
components:
  schemas:
    Early: {$ref: '#/components/schemas/Later/properties/` + key + `'}
    Later: {type: object, properties: {"` + key + `": {type: object}}}
    Holder: {type: object, properties: {"` + key + `": {type: object}}}
    Uses: {$ref: '#/components/schemas/Holder/properties/` + key + `'}
`
	}
	property := func(owner, key string) ir.TypeID {
		return ir.TypeID("t/anon/components/schemas/" + owner + "/properties/" + key)
	}

	basic, basicDiags := parseFull(t, twin("é"))
	openapitest.RequireNoErrorDiags(t, basicDiags)
	assert.Len(t, operationsNamed(basic, "getKey"), 2,
		"a key inside the Basic Multilingual Plane resolves: the item is mounted at both paths")
	assertAliases(t, basic, "Uses", property("Holder", "é"), "a resolved reference aliases its target")
	assertAliases(t, basic, "Early", property("Later", "é"), "in either declaration order")

	astral, astralDiags := parseFull(t, twin("😀"))
	assert.True(t, openapitest.HasDiagCodeAt(astralDiags, diag.UnresolvedRef, "/paths/~1mount"),
		"the resolver still refuses a token past U+FFFF; if it no longer does, see this test's comment")
	assert.Len(t, operationsNamed(astral, "getKey"), 1,
		"the refused reference mounts nothing, which is what the fix will change")
	assertAliases(t, astral, "Uses", property("Holder", "😀"), "a schema $ref resolves past U+FFFF")
	assertAliases(t, astral, "Early", property("Later", "😀"), "in either declaration order")
}

// assertAliases asserts the component schema name lowered to an alias over
// target: what the position's $ref came to stand for.
func assertAliases(t *testing.T, doc *ir.Document, name string, target ir.TypeID, msg string) {
	t.Helper()
	alias, ok := doc.Types[componentID(name)].(*ir.Scalar)
	require.True(t, ok, "%s lowers to an alias over what its $ref names", name)
	require.NotNil(t, alias.Base, "the alias names what it stands for")
	assert.Equal(t, target, alias.Base.Target, msg)
}
