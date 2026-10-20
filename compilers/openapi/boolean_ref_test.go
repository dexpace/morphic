package openapi_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_ARefToABooleanSchemaResolvesInEitherOrder pins GitHub #861: a
// $ref naming a boolean schema below a component takes what that position
// lowers to, whichever is declared first. Each case declares the $ref first,
// the order that left it unresolved, and then the other. A closed tuple's
// `items: false`, which nothing else lowers, and a value the parser reads as
// `false` resolve too, as do a union branch and a mapping value.
func TestCompile_ARefToABooleanSchemaResolvesInEitherOrder(t *testing.T) {
	t.Parallel()
	const items = "'#/components/schemas/L/items'"
	const prop = "'#/components/schemas/L/properties/p'"
	for _, tc := range []struct {
		name, user, owner string
		// base is what O aliases, or empty where O is no alias.
		base ir.TypeID
	}{
		{"items false", "{$ref: " + items + "}", "{type: array, items: false}", "t/anon/components/schemas/L/items"},
		{"a closed tuple's items", "{$ref: " + items + "}", "{type: array, prefixItems: [{type: string}], items: false}",
			"t/anon/components/schemas/L/items"},
		{"a true property", "{$ref: " + prop + "}", "{type: object, properties: {p: true}}",
			"t/anon/components/schemas/L/properties/p"},
		{"a property the parser reads as false", "{$ref: " + prop + "}", "{type: object, properties: {p: just text}}",
			"t/anon/components/schemas/L/properties/p"},
		{"a union branch and a mapping value", "{type: object, properties: {k: {type: string}}, " +
			"discriminator: {propertyName: k, mapping: {f: " + items + "}}, oneOf: [{$ref: " + items + "}]}",
			"{type: array, items: false}", ""},
	} {
		components := []string{"    O: " + tc.user + "\n", "    L: " + tc.owner + "\n"}
		types := make([]ir.TypeRegistry, 0, 2)
		for _, order := range [][]string{components, {components[1], components[0]}} {
			src := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
				strings.Join(order, "")
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}}, compilers.Options{})
			require.NoError(t, err, tc.name)
			for _, d := range diags {
				assert.NotEqual(t, "openapi/unresolved-ref", d.Code, "%s: %s", tc.name, d.Message)
			}
			types = append(types, doc.Types)
		}
		assert.Empty(t, cmp.Diff(types[0], types[1]), "%s: declaration order changed the registry", tc.name)
		if tc.base == "" {
			continue
		}
		o, ok := types[0]["t/openapi/components/schemas/O"].(*ir.Scalar)
		require.True(t, ok, tc.name)
		require.NotNil(t, o.Base, tc.name)
		assert.Equal(t, tc.base, o.Base.Target, tc.name)
		assert.Contains(t, types[0], tc.base, tc.name)
	}
}
