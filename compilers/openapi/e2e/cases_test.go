// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
)

// notCases names each keyword of the rule table the schema cases do not
// write, with why not. A keyword the table gains is written by a construct
// row of openapicases, or listed here.
func notCases() map[string]string { return map[string]string{} }

// TestCases_CoverTheRuleTable holds the OpenAPI schema cases to the rule
// table: at each version the cases are written at, every keyword the Schema
// Object defines there is a construct some case writes, or notCases says why
// not; and every construct a case writes is a field the Schema Object defines
// at that version.
func TestCases_CoverTheRuleTable(t *testing.T) {
	t.Parallel()
	schema, ok := specrules.Table().Object(specrules.Schema)
	require.True(t, ok)
	written := writtenConstructs(t, openapicases.New())
	require.NotEmpty(t, written)

	for version, constructs := range written {
		minor, ok := specrules.ParseMinor(version)
		require.True(t, ok, "%s is a version the rule table covers", version)
		for _, f := range schema.Fields {
			if !f.IsJSONSchemaKeyword || !f.Defined.Holds(minor) {
				continue
			}
			_, excused := notCases()[f.Name]
			assert.True(t, constructs[f.Name] || excused,
				"%s: keyword %s of the rule table is no schema case's construct; write it in a "+
					"construct row of openapicases, or say in notCases why not", version, f.Name)
		}
		for name := range constructs {
			assert.True(t, slices.ContainsFunc(schema.Fields, func(f specrules.Field) bool {
				return f.Name == name && f.Defined.Holds(minor)
			}), "%s: construct %s names no field the Schema Object defines there", version, name)
		}
	}
	for name, why := range notCases() {
		assert.NotEmpty(t, why, name)
		for version, constructs := range written {
			assert.False(t, constructs[name], "%s: %s is written, so notCases need not excuse it", version, name)
		}
	}
}

// writtenConstructs returns, by the name of each version the version axis
// lists, the keywords of the constructs some case writes at it, read through
// the cases' own axes and Write. A case is written only while its pair of
// version and keyword is not yet known written, since writing is the cost.
func writtenConstructs(t *testing.T, c harness.Cases) map[string]map[string]bool {
	t.Helper()
	axes := c.Axes()
	version := slices.IndexFunc(axes, func(a harness.Axis) bool { return a.Name == "version" })
	construct := slices.IndexFunc(axes, func(a harness.Axis) bool { return a.Name == "construct" })
	require.GreaterOrEqual(t, version, 0)
	require.GreaterOrEqual(t, construct, 0)

	written := map[string]map[string]bool{}
	each, err := harness.Each(c)
	require.NoError(t, err)
	for _, at := range each {
		v, k := axes[version].Values[at[version]].Name, strings.TrimSuffix(axes[construct].Values[at[construct]].Name, "~")
		if written[v] == nil {
			written[v] = map[string]bool{}
		}
		if k == "none" || written[v][k] {
			continue
		}
		if _, ok := c.Write(at); ok {
			written[v][k] = true
		}
	}
	return written
}
