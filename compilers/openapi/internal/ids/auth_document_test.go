// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ids_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// TestDocument_NamesASchemeByItsDocument pins the IDs of a scheme a requirement
// key reaches by position, and of one it mints for a key reaching none: each in
// the space of its document, the source's bare, so one key or pointer in two
// documents is two schemes.
func TestDocument_NamesASchemeByItsDocument(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.AuthID("auth/openapi/x/s"), ids.In("").Auth("/x/s"))
	assert.Equal(t, ir.AuthID("auth/openapi@a.yaml/x/s"), ids.In("a.yaml").Auth("/x/s"))
	assert.Equal(t, ids.UnresolvedAuth("#/x"), ids.In("").UnresolvedAuth("#/x"), "the source's is a name's")
	assert.Equal(t, ir.AuthID("auth/unresolved@a.yaml/components/securitySchemes/#~1x"), ids.In("a.yaml").UnresolvedAuth("#/x"))
}
