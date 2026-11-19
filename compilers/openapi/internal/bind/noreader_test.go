// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// TestBind_ASourceAtAURLWithNoReaderReadsNoOtherDocument pins what a source
// located at an http(s) URL binds with no Config.Open: a $ref, a mapping value
// and a static $dynamicRef naming the source by its own URL, or a resource it
// declares by its $id, resolve, and each naming another URL is unread. None
// asks to judge a document read before, as none is read.
func TestBind_ASourceAtAURLWithNoReaderReadsNoOtherDocument(t *testing.T) {
	t.Parallel()
	const self, other, id = "https://x.test/spec.yaml", "https://other.test/o.yaml#/X", "https://x.test/c"
	spec := head + `components:
  schemas:
    T: {type: string}
    C: {$id: '` + id + `', type: string}
    RSelf: {$ref: '` + self + `#/components/schemas/T'}
    ROther: {$ref: '` + other + `'}
    RID: {$ref: '` + id + `'}
    DSelf: {$dynamicRef: '` + self + `#/components/schemas/T'}
    DOther: {$dynamicRef: '` + other + `'}
    DID: {$dynamicRef: '` + id + `'}
    M:
      oneOf: [{$ref: '#/components/schemas/T'}]
      discriminator:
        propertyName: k
        mapping: {s: '` + self + `#/components/schemas/T', o: '` + other + `', i: '` + id + `'}
`
	table := bound(t, spec, bind.Config{Self: self})
	for name, want := range map[string]bind.Status{
		"RSelf": bind.Resolved, "RID": bind.Resolved, "DSelf": bind.Resolved, "DID": bind.Resolved,
		"ROther": bind.Unresolved, "DOther": bind.Unresolved,
	} {
		assert.Equal(t, want, entryAt(t, table, jsontext.Pointer("/components/schemas/"+name)).Hop.Status, name)
	}
	at := bind.DocumentPointer{Pointer: "/components/schemas/M/discriminator"}
	for value, want := range map[string]bind.Status{
		self + "#/components/schemas/T": bind.Resolved, id: bind.Resolved, other: bind.Unresolved,
	} {
		m, ok := table.Mapping(at, value)
		if assert.True(t, ok, value) {
			assert.Equal(t, want, m.Chain.Status, value)
		}
	}
	assert.Equal(t, 1, table.Documents(), "no other document is read")
}
