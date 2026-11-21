// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestReserveNodesAtReferenceTargets_ReservesNoMemberOfALoop pins that a loop
// of $refs, which lands nowhere a type can be read
// (reftable.ReferenceSite.Lands), is promised no node at any member: one
// through a schema's own property, and one between two keywords kept verbatim,
// each lowering to what it lowers to without the call.
func TestReserveNodesAtReferenceTargets_ReservesNoMemberOfALoop(t *testing.T) {
	t.Parallel()
	for name, schemas := range map[string]string{
		"through its own member": `    A:
      $ref: '#/components/schemas/A/properties/p'
      properties:
        p: {$ref: '#/components/schemas/A'}
`,
		"between kept keywords": `    B: {contentSchema: {$ref: '#/components/schemas/C/contentSchema'}}
    C: {contentSchema: {$ref: '#/components/schemas/B/contentSchema'}}
`,
	} {
		plain, promised := lowerBoth(t, openapitest.ComponentSpecVer("3.1.0", schemas))
		assert.Empty(t, cmp.Diff(plain.Types, promised.Types), name)
	}
}
