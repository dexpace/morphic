// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/cache"
)

// entrySeparator ends an entry's seal and its head. Neither holds a raw newline,
// one being hex and the other compact JSON, so the first two end them.
const entrySeparator = '\n'

// entryHead is what a run needs to tell whether the document after it still
// answers, ahead of decoding that document.
type entryHead struct {
	// Format is Result.Format, which a hit does not detect.
	Format compilers.SourceFormat `json:"format"`
	// Reads is every document the compile read. An entry answers only a run
	// reading the same bytes from each.
	Reads []documentRead `json:"reads,omitempty"`
}

// sealOf hashes key and then parts. Sealing with the key is what ties an entry
// to the run it answers: a hash of the bytes alone would pass an intact entry
// handed back for another key, and serve that run another spec's document.
func sealOf(key cache.Key, parts ...[]byte) []byte {
	h := sha256.New()
	h.Write(key[:])
	for _, part := range parts {
		h.Write(part)
	}
	return hex.AppendEncode(nil, h.Sum(nil))
}

// encodeEntry writes res as the entry named key: the seal over all that follows,
// the head, then the compact document, a line each.
func encodeEntry(key cache.Key, res *Result, reads []documentRead) ([]byte, error) {
	body, err := json.Marshal(res.Document)
	if err != nil {
		return nil, fmt.Errorf("encode document: %w", err)
	}
	head, err := json.Marshal(entryHead{Format: res.Format, Reads: reads})
	if err != nil {
		return nil, fmt.Errorf("encode head: %w", err)
	}
	separator := []byte{entrySeparator}
	return slices.Concat(sealOf(key, head, separator, body), separator, head, separator, body), nil
}

// decodeEntry splits entry into its head and document, refusing one its seal
// does not hold for key: damaged anywhere, cut short, or some other key's.
func decodeEntry(key cache.Key, entry []byte) (entryHead, []byte, error) {
	seal, content, _ := bytes.Cut(entry, []byte{entrySeparator})
	if !bytes.Equal(seal, sealOf(key, content)) {
		return entryHead{}, nil, errors.New("seal does not hold")
	}
	line, body, _ := bytes.Cut(content, []byte{entrySeparator})
	var head entryHead
	if err := json.Unmarshal(line, &head); err != nil {
		return entryHead{}, nil, fmt.Errorf("decode head: %w", err)
	}
	return head, body, nil
}
