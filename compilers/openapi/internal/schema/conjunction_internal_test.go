// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
)

// TestConjunctsIn_ANilSchemaWritesNone pins the guards of the two readers of
// what a schema conjoins beside its own keywords: a nil schema writes no
// reference and no allOf.
func TestConjunctsIn_ANilSchemaWritesNone(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	assert.Empty(t, conjunctsIn(l.Ctx, nil))
	assert.Empty(t, allOfIn(nil))
}
