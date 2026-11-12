// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWritesRef_ReadsARefAsTheBinderDoes pins which schemas writesRef says
// write a $ref: those whose `$ref` the binder reads as one, a string, empty or
// not, an alias to one among them, so the identifier SiteAt says a dialect
// ignores beside it is the one the binder ignores. A null, a mapping or a list
// writes none, nor does a schema writing no `$ref`.
func TestWritesRef_ReadsARefAsTheBinderDoes(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-r: &r '#/components/schemas/N'\n"+
		"components:\n  schemas:\n    N: {}\n"+
		"    S: {$ref: '#/components/schemas/N'}\n    E: {$ref: ''}\n    A: {$ref: *r}\n"+
		"    Null: {$ref: null}\n    M: {$ref: {a: b}}\n    L: {$ref: [x]}\n    None: {type: string}\n")
	for name, want := range map[string]bool{"S": true, "E": true, "A": true, "Null": false, "M": false, "L": false, "None": false} {
		assert.Equal(t, want, writesRef(declared(t, l, name)), name)
	}
}
