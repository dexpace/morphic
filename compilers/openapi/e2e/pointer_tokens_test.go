// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_APointerTokenPastUFFFFResolves pins GitHub #516: RFC 6901
// admits a reference token of any character up to U+10FFFF, and the compiler
// reads every pointer itself, so a reference object through an emoji key
// resolves as one through a Basic Multilingual Plane key does, and a schema
// $ref does too, in either declaration order. The twins differ only in that
// key.
func TestCompile_APointerTokenPastUFFFFResolves(t *testing.T) {
	t.Parallel()
	twin := func(key string) string {
		return `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  "/` + key + `":
    get: {operationId: getKey, parameters: [{$ref: '#/x-params/` + key + `'}], responses: {"200": {description: ok}}}
  /mount: {$ref: '#/paths/~1` + key + `'}
x-params:
  "` + key + `": {name: p, in: query, schema: {type: string}}
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

	for _, key := range []string{"é", "😀"} {
		doc, diags := parseFull(t, twin(key))
		openapitest.RequireNoErrorDiags(t, diags)
		ops := operationsNamed(doc, "getKey")
		assert.Len(t, ops, 2, "%s: the item is mounted at both paths", key)
		for _, op := range ops {
			require.Len(t, op.Params, 1, key)
			assert.Equal(t, "p", op.Params[0].Name.Source, "%s: the parameter resolves", key)
		}
		assertAliases(t, doc, "Uses", property("Holder", key), "a resolved reference aliases its target")
		assertAliases(t, doc, "Early", property("Later", key), "in either declaration order")
	}
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
