// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestRefinementSetUnion_AUnionTheValueSetStandsBesideIsKept pins a oneOf or
// anyOf written with the value set that stands at the position, on an entry
// in either order and beside the allOf: the node the set lowers to holds no
// union, so in every spelling it is kept under its keyword with one reason and
// reported, where the entry's was consumed in silence as said by the set.
func TestRefinementSetUnion_AUnionTheValueSetStandsBesideIsKept(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/U'}"
	for _, union := range []string{"oneOf", "anyOf"} {
		for _, set := range []string{"enum: [a]", "const: a"} {
			words := union + ": [{type: string}, {type: boolean}], " + set
			spellings := map[string]struct{ schema, at string }{
				"entry":    {"{allOf: [" + ref + ", {" + words + "}]}", "/allOf/1/" + union},
				"reversed": {"{allOf: [{" + words + "}, " + ref + "]}", "/allOf/0/" + union},
				"beside":   {"{allOf: [" + ref + "], " + words + "}", "/" + union},
			}
			var schemas strings.Builder
			schemas.WriteString("    U: {oneOf: [{type: string}, {type: integer}]}\n")
			for name, sp := range spellings {
				schemas.WriteString("    " + strings.ToUpper(name[:1]) + name[1:] + ": " + sp.schema + "\n")
			}
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", schemas.String()))
			openapitest.RequireNoErrorDiags(t, diags)
			for name, sp := range spellings {
				component := strings.ToUpper(name[:1]) + name[1:]
				kept, ok := typeByName(doc, component).Common().Unmodeled["openapi:"+union]
				require.True(t, ok, "%s %s %s: the union is kept", union, set, name)
				assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason, "%s %s %s", union, set, name)
				written := "/components/schemas/" + component + sp.at
				assert.Equal(t, jsontext.Pointer(written), kept.Provenance.Pointer, "%s %s %s", union, set, name)
				reported := slices.ContainsFunc(diags, func(diagnostic ir.Diagnostic) bool {
					return diagnostic.Severity == ir.SeverityInfo && strings.HasPrefix(written, string(diagnostic.Provenance.Pointer)) &&
						strings.Contains(diagnostic.Message, union)
				})
				assert.True(t, reported, "%s %s %s: the union kept is reported", union, set, name)
			}
		}
	}
}
