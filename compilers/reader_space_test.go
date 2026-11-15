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

// TestDocumentRead_MostPrivateIsTheMorePrivateOfItsSpaces pins the rank of a
// read as a target: its bound, or the space that answered where a reader
// states that more private, and none where it states neither.
func TestDocumentRead_MostPrivateIsTheMorePrivateOfItsSpaces(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ space, bound, want compilers.AddressSpace }{
		{compilers.SpaceUnstated, compilers.SpaceUnstated, compilers.SpaceUnstated},
		{compilers.SpacePublic, compilers.SpacePublic, compilers.SpacePublic},
		{compilers.SpaceUnstated, compilers.SpacePrivate, compilers.SpacePrivate},
		{compilers.SpaceLocal, compilers.SpacePublic, compilers.SpaceLocal},
	} {
		got := compilers.DocumentRead{Space: tc.space, Bound: tc.bound}.MostPrivate()
		assert.Equal(t, tc.want, got, "space %d, bound %d", tc.space, tc.bound)
	}
}
