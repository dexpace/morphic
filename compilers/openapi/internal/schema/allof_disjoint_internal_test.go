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
// of whose members its type admits, an empty enum, an allOf of conjuncts
// sharing no kind, an object beside a union no branch of which an object or
// null satisfies and another type beside a union none of whose branches it
// admits, each also through a chain of $refs up to the read's cap; and which
// do not: a type beside a $ref to another kind, a chain past the cap, and
// `true`, which admits everything.
func TestToldWhereWritten_ReadsWhatSaysASchemaAdmitsNothing(t *testing.T) {
	t.Parallel()
	rows := map[string]bool{
		"F": true, "S": true, "K": true, "E": true, "D": true, "AliasS": true, "AliasF": true,
		"Short0": true, "U": true, "AliasU": true, "W": true, "AliasW": true, "Str": false, "Beside": false, "Long0": false,
		"T": false,
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
    W: {type: string, anyOf: [{type: integer}, {type: boolean}]}
    AliasW: {$ref: '#/components/schemas/W'}
    Beside: {$ref: '#/components/schemas/Str', type: object}
`+aliasChain("Short", maxAdmissionReads-1, "F")+aliasChain("Long", maxAdmissionReads, "F")))
	for name, want := range rows {
		st := l.Ctx.Statements().At(bind.DocumentPointer{Doc: bind.Source, Pointer: jsontext.Pointer("/components/schemas/" + name)})
		assert.Equal(t, want, toldWhereWritten(l.Ctx, st), name)
	}
}

// TestInstanceWords_NamesEverySetOfKinds holds that every set of instance
// kinds a conjunct can admit is named, each kind by a word of its own and a
// number with a fraction apart from an integer, with no empty word.
func TestInstanceWords_NamesEverySetOfKinds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kinds kindSet
		words string
	}{
		{0, "null"},
		{kindFraction, "number that is no integer"},
		{kindNumber, "number"},
		{kindInteger, "integer"},
		{kindFraction | kindString, "string or number that is no integer"},
		{kindInteger | kindBoolean, "integer or boolean"},
		{anyInstance, "object or array or string or number or boolean"},
	} {
		assert.Equal(t, tc.words, instanceWords(tc.kinds), "%07b", tc.kinds)
	}
	for kinds := kindSet(1); kinds <= anyInstance; kinds++ {
		if kinds&^anyInstance != 0 {
			continue
		}
		for word := range strings.SplitSeq(instanceWords(kinds), " or ") {
			assert.NotEmpty(t, word, "%07b is named %q", kinds, instanceWords(kinds))
		}
	}
}

// TestUnionApartFromBody_ReadsKindsAndACutReadSharesOne holds that a union
// beside a string body shares no kind with it where its branches admit
// integers, shares one where they admit a string, and shares one where a
// branch's read is cut short on a cycle, which admits more than it reads.
func TestUnionApartFromBody_ReadsKindsAndACutReadSharesOne(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", `    Cyc: {allOf: [{$ref: '#/components/schemas/Cyc'}]}
    Apart: {type: string, anyOf: [{type: integer}]}
    Shared: {type: string, anyOf: [{type: string}, {type: integer}]}
    Cut: {type: string, anyOf: [{type: integer}, {$ref: '#/components/schemas/Cyc'}]}
`))
	for name, want := range map[string]bool{"Apart": true, "Shared": false, "Cut": false} {
		st := l.Ctx.Statements().At(bind.DocumentPointer{Doc: bind.Source, Pointer: jsontext.Pointer("/components/schemas/" + name)})
		assert.Equal(t, want, unionApartFromBody(l.Ctx, st.Schema), name)
	}
}
