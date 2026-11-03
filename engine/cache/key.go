// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Key names one entry. Being a hash and never a caller's string, it is always
// safe to spell as a file name.
type Key [sha256.Size]byte

// KeyOf derives the Key of fields. Each is written after its length, so two
// sequences share a key only by being equal: moving a byte across a boundary
// changes it. A caller putting a list among them owes its count, unless the
// list comes last.
func KeyOf(fields ...string) Key {
	var framed []byte
	for _, field := range fields {
		framed = binary.AppendUvarint(framed, uint64(len(field)))
		framed = append(framed, field...)
	}
	return sha256.Sum256(framed)
}

// String spells k in lowercase hex.
func (k Key) String() string { return hex.EncodeToString(k[:]) }
