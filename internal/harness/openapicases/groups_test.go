// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
)

// TestGroups_TheAxesInTheirOrder pins the axes: the versions reading JSON
// Schema 2020-12, a group per suite group holding a valid instance, named with
// its slashes encoded, and the positions whose defaults the pass judges.
func TestGroups_TheAxesInTheirOrder(t *testing.T) {
	axes := openapicases.Groups().Axes()
	names := make([]string, len(axes))
	for i, a := range axes {
		names[i] = a.Name
	}
	assert.Equal(t, []string{"version", "group", "position", "spelling"}, names)
	assert.Equal(t, []harness.Value{{Name: "3.1.0"}, {Name: "3.2.0", Like: "3.1.0"}}, axes[0].Values)
	assert.Equal(t, []harness.Value{{Name: "property"}, {Name: "parameter"}, {Name: "header"}}, axes[2].Values)

	groups, err := openapicases.SuiteGroups()
	require.NoError(t, err)
	valid := slices.DeleteFunc(groups, func(g openapicases.Group) bool {
		return !slices.ContainsFunc(g.Tests, func(t openapicases.Test) bool { return t.Valid })
	})
	require.Len(t, axes[1].Values, len(valid))
	assert.Contains(t, axes[1].Values,
		harness.Value{Name: "unevaluatedItems.json%2FunevaluatedItems with if%2Fthen%2Felse (prefixItems)"})
}

func TestGroups_WritesNothingOutsideTheProduct(t *testing.T) {
	c := openapicases.Groups()
	for _, at := range [][]int{nil, {0, 0, 0}, {0, 0, 0, 1}, {0, -1, 0, 0}, {2, 0, 0, 0}, {0, 0, 3, 0}} {
		_, ok := c.Write(at)
		assert.False(t, ok, "%v", at)
	}
}

// groupAt returns the coordinates of the case of the group named name at the
// position named position, at 3.1.0.
func groupAt(t *testing.T, c harness.Cases, name, position string) []int {
	t.Helper()
	axes := c.Axes()
	g := slices.IndexFunc(axes[1].Values, func(v harness.Value) bool { return v.Name == name })
	p := slices.IndexFunc(axes[2].Values, func(v harness.Value) bool { return v.Name == position })
	require.True(t, g >= 0 && p >= 0, "%s at %s", name, position)
	return []int{0, g, p, 0}
}

// TestGroups_WritesEachValidInstanceAsADefault writes a group serving a schema
// at a URI: the root holds a carrier per valid instance, defaulting to it,
// the group's schema is read in 2020-12, and the schema it serves is held at
// its URI.
func TestGroups_WritesEachValidInstanceAsADefault(t *testing.T) {
	c := openapicases.Groups()
	doc, ok := c.Write(groupAt(t, c, "ref.json%2Fremote ref", "header"))
	require.True(t, ok)
	require.Len(t, doc.Sources, 3)
	assert.Equal(t, "openapi.json", doc.Sources[0].Path)
	assert.JSONEq(t, `{"openapi": "3.1.0", "info": {"title": "cases", "version": "1"},
		"paths": {"/c": {"get": {"responses": {"200": {"description": "ok",
			"headers": {"H0": {"schema": {"$ref": "g.json", "default": 1}}}}}}}},
		"components": {"schemas": {}}}`, string(doc.Sources[0].Data))
	assert.Equal(t, "g.json", doc.Sources[1].Path)
	assert.JSONEq(t, `{"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$ref": "http://localhost:1234/integer.json"}`, string(doc.Sources[1].Data))
	assert.Equal(t, "http://localhost:1234/integer.json", doc.Sources[2].Path)
	assert.JSONEq(t, `{"type": "integer"}`, string(doc.Sources[2].Data))
	assert.Equal(t, map[string]string{"allow-external-refs": "true"}, doc.Options)
	assert.Equal(t, []harness.Instance{
		{Node: "p/openapi/paths/~1c/get/responses/200/headers/H0", Valid: true, Name: "remote ref valid"},
	}, doc.Instances)
}

// TestGroups_NamesEachCarriersNode pins the node each position's carriers
// compile to, in the order of the group's valid instances.
func TestGroups_NamesEachCarriersNode(t *testing.T) {
	c := openapicases.Groups()
	for position, want := range map[string][]string{
		"property": {"p/openapi/components/schemas/C/properties/t0", "p/openapi/components/schemas/C/properties/t1"},
		"parameter": {"param/openapi/paths/~1c/get/parameters/q0/query",
			"param/openapi/paths/~1c/get/parameters/q1/query"},
		"header": {"p/openapi/paths/~1c/get/responses/200/headers/H0",
			"p/openapi/paths/~1c/get/responses/200/headers/H1"},
	} {
		doc, ok := c.Write(groupAt(t, c, "required.json%2Frequired properties whose names are Javascript object "+
			"property names", position))
		require.True(t, ok)
		require.Len(t, doc.Instances, 3, position)
		assert.Equal(t, want, []string{doc.Instances[0].Node, doc.Instances[1].Node}, position)
		assert.Equal(t, "ignores arrays", doc.Instances[0].Name)
	}
}
