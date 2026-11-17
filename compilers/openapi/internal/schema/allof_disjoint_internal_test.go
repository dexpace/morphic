// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// aliasChain writes n schemas, the first named name, each a $ref to the next,
// and the last a $ref to last.
func aliasChain(name string, n int, last string) string {
	var b strings.Builder
	for i := range n {
		next := fmt.Sprintf("%s%d", name, i+1)
		if i == n-1 {
			next = last
		}
		fmt.Fprintf(&b, "    %s%d: {$ref: '#/components/schemas/%s'}\n", name, i, next)
	}
	return b.String()
}

// TestToldWhereWritten_ReadsWhatSaysASchemaAdmitsNothing holds which schemas
// admitting no value say so where they are written: `false`, a value set none
// of whose members its type admits, an empty enum and an allOf of conjuncts
// sharing no kind, each also through a chain of $refs up to the read's cap;
// and which do not: a type beside a union none of whose branches it admits, a
// type beside a $ref to another kind, a chain past the cap, and `true`, which
// admits everything.
func TestToldWhereWritten_ReadsWhatSaysASchemaAdmitsNothing(t *testing.T) {
	t.Parallel()
	rows := map[string]bool{
		"F": true, "S": true, "K": true, "E": true, "D": true, "AliasS": true, "AliasF": true,
		"Short0": true, "U": false, "AliasU": false, "Beside": false, "Long0": false, "T": false,
	}
	l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", `    F: false
    T: true
    Str: {type: string}
    S: {type: string, enum: [1]}
    K: {type: string, const: 1}
    E: {enum: []}
    D: {allOf: [{type: string}, {type: integer}]}
    AliasS: {$ref: '#/components/schemas/S'}
    AliasF: {$ref: '#/components/schemas/F'}
    U: {type: object, anyOf: [{type: string}, {type: integer}]}
    AliasU: {$ref: '#/components/schemas/U'}
    Beside: {$ref: '#/components/schemas/Str', type: object}
`+aliasChain("Short", maxAdmissionReads-1, "F")+aliasChain("Long", maxAdmissionReads, "F")))
	for name, want := range rows {
		st := l.Ctx.Statements().At(bind.Position{Doc: bind.Source, Pointer: jsontext.Pointer("/components/schemas/" + name)})
		assert.Equal(t, want, toldWhereWritten(l.Ctx, st), name)
	}
}
