// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// versionOf returns the plan version of doc under policy
// (docs/emitters/plan/overview.md#the-plans-version): the SHA-256, in
// hexadecimal, of doc's digest followed by policy's canonical JSON. The digest is
// the SHA-256 of doc's canonical JSON with its sources and diagnostics left
// out, which Build never reads. It fails for a document that does not encode.
func versionOf(doc *ir.Document, policy Policy) (string, error) {
	// A shallow copy: the encoder reads it and nothing writes it.
	read := *doc
	read.Sources, read.Diagnostics = nil, nil

	// Each part is hashed after the sum of the one before it, so the document's
	// digest, of a fixed length, ends where the policy's bytes begin.
	var sum []byte
	for _, part := range []any{&read, policy} {
		h := sha256.New()
		h.Write(sum)
		if err := json.MarshalWrite(h, part, ir.CanonicalOptions()); err != nil {
			return "", fmt.Errorf("plan: version: %w", err)
		}
		sum = h.Sum(nil)
	}
	return hex.EncodeToString(sum), nil
}
