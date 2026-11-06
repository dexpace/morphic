// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestTwinKeyword_SpellsADraftsKeywordAs2020Does pins TwinKeyword: each
// keyword a draft spells otherwise is 2020-12's twin in a schema whose dialect
// defines it, and itself in one that does not, as is every other keyword.
func TestTwinKeyword_SpellsADraftsKeywordAs2020Does(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(
		"    D4: {$schema: 'http://json-schema.org/draft-04/schema#'}\n"+
			"    D7: {$schema: 'http://json-schema.org/draft-07/schema#'}\n"+
			"    D19: {$schema: 'https://json-schema.org/draft/2019-09/schema'}\n"+
			"    D20: {$schema: 'https://json-schema.org/draft/2020-12/schema'}\n"))
	for _, tc := range []struct{ schema, key, want string }{
		{"D4", "id", "$id"}, {"D7", "id", "id"}, {"D20", "id", "id"},
		{"D7", "definitions", "$defs"}, {"D19", "definitions", "definitions"},
		{"D4", "dependencies", "dependentSchemas"}, {"D19", "dependencies", "dependencies"},
		{"D19", "$recursiveAnchor", "$dynamicAnchor"}, {"D7", "$recursiveAnchor", "$recursiveAnchor"},
		{"D4", "type", "type"},
	} {
		assert.Equal(t, tc.want, TwinKeyword(l.Ctx, declared(t, l, tc.schema), tc.key), "%s in %s", tc.key, tc.schema)
	}
}
