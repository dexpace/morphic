// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compilers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers"
)

// TestAddressSpaceError_IsErrAddressSpaceNamingTheAddress pins that the typed
// refusal is the sentinel a compile branches on, naming the address.
func TestAddressSpaceError_IsErrAddressSpaceNamingTheAddress(t *testing.T) {
	t.Parallel()
	var err error = &compilers.AddressSpaceError{Address: "10.0.0.5:80", Space: compilers.SpacePrivate}

	assert.ErrorIs(t, err, compilers.ErrAddressSpace)
	assert.Equal(t, "10.0.0.5:80: at an address more private than the read may reach", err.Error())
}
