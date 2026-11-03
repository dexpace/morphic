// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// referenceKeywords are the keywords whose value a schema resolves against its
// base URI.
var referenceKeywords = []string{"$ref", "$dynamicRef"}

// WritesReference reports whether raw, a schema or keyword value rendered as
// JSON, names a member referenceKeywords lists at any depth, so that what it
// says turns on the base it is read against. Text with none says the same under
// every base. A member written as data, under `default` or `examples`, counts,
// as does text that does not decode: each errs towards comparing the bases.
// Every token read consumes input, so the walk is bounded by raw's length.
func WritesReference(raw ir.RawValue) bool {
	dec := jsontext.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := dec.ReadToken()
		if err != nil {
			return !errors.Is(err, io.EOF)
		}
		// In an object a name is each odd token, its value each even one.
		kind, n := dec.StackIndex(dec.StackDepth())
		if tok.Kind() == '"' && kind == '{' && n%2 == 1 && slices.Contains(referenceKeywords, tok.String()) {
			return true
		}
	}
}
