// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestAdmission_ASchemaNoBindingPlacesIsReadInTheContextsDialect pins the read
// of a schema the bindings do not place, as one parsed apart from the document:
// it is read in the dialect the context reads its own schemas in, so a 3.0
// nullable adds null to the type beside it and a 3.1 one adds nothing.
func TestAdmission_ASchemaNoBindingPlacesIsReadInTheContextsDialect(t *testing.T) {
	t.Parallel()
	js := &oas3.JSONSchema[oas3.Referenceable]{}
	valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader("{type: string, nullable: true}"), js)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	for _, tc := range []struct {
		version string
		kinds   kindSet
	}{
		{"3.0.3", kindString | kindNull},
		{"3.1.0", kindString},
	} {
		l, _ := LoweredFor(t, openapitest.ComponentSpecVer(tc.version, "    A: {type: string}\n"))
		w := newAdmission(l.Ctx)
		assert.Equal(t, tc.kinds, w.whole(js).kinds, tc.version)
	}
}
