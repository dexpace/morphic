// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ids_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// TestDocumentIDNamespace_NamesASchemeByItsDocument pins the IDs of a scheme a
// requirement key reaches by position, in the space of its document, the
// source's bare, and of one it mints for a key reaching none, in the one space
// the kind takes (docs/ir/identity.md#unresolved-schemes), its document in the
// path: so one key or pointer in two documents is two schemes.
func TestDocumentIDNamespace_NamesASchemeByItsDocument(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.AuthID("auth/openapi/x/s"), ids.In("").Auth("/x/s"))
	assert.Equal(t, ir.AuthID("auth/openapi@a.yaml/x/s"), ids.In("a.yaml").Auth("/x/s"))
	assert.Equal(t, ids.UnresolvedAuth("#/x"), ids.In("").UnresolvedAuth("#/x"), "the source's is a name's")
	other := ids.In("b/c.yaml").UnresolvedAuth("#/x")
	assert.Equal(t, ir.AuthID("auth/unresolved/@b~1c.yaml/components/securitySchemes/#~1x"), other)
	space, _ := ir.IDSpace(ir.IDKindAuth, string(other))
	assert.Equal(t, ir.IDSpaceUnresolved, space)
}
