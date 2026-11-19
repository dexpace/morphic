// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// keptOnceCase is a keyword an entry kept whole and one kept apart both write
// alike, the entry kept whole being the one indexOf finds first.
type keptOnceCase struct {
	whole, apart, key string
}

// TestEntryKeptOnce_AKeywordAnEntryKeptWholeSharesIsKeptInsideItAlone pins a
// keyword an entry kept whole for a closure writes alike with another entry: the
// entry kept whole holds its own, and the other entry's is kept at its keyword,
// not at the position, in every order of the three entries. A vendor extension
// and a validation-only keyword fold; readOnly is kept as each folded branch's residue.
func TestEntryKeptOnce_AKeywordAnEntryKeptWholeSharesIsKeptInsideItAlone(t *testing.T) {
	t.Parallel()
	ref := "{$ref: '#/components/schemas/Str'}"
	for _, keptOnceCaseValue := range []keptOnceCase{
		{"{additionalProperties: false, x-a: 1}", "{x-a: 1}", "openapi:x-a"},
		{"{additionalProperties: false, not: {required: [z]}}", "{not: {required: [z]}}", "openapi:not"},
		{"{additionalProperties: false, readOnly: true}", "{readOnly: true}", "openapi:readOnly"},
	} {
		for _, perm := range permutations(3) {
			written := []string{ref, keptOnceCaseValue.whole, keptOnceCaseValue.apart}
			order := []string{written[perm[0]], written[perm[1]], written[perm[2]]}
			name := strings.Join(order, ", ")
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Str: {type: string}\n"+
				"    S: {allOf: ["+name+"]}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			at := "/components/schemas/S/allOf/"
			kept := typeByName(doc, "S").Common().Unmodeled
			whole, apart := indexOf(order, keptOnceCaseValue.whole), indexOf(order, keptOnceCaseValue.apart)
			assert.Equal(t, []string{"openapi:allOf/" + whole, keptOnceCaseValue.key}, unmodeledKeys(kept), name)
			assert.Equal(t, jsontext.Pointer(at+apart+"/"+strings.TrimPrefix(keptOnceCaseValue.key, "openapi:")),
				kept[keptOnceCaseValue.key].Provenance.Pointer, name)
		}
	}
}

// indexOf returns where item stands in items, as a pointer token.
func indexOf(items []string, item string) string {
	for i, s := range items {
		if s == item {
			return strconv.Itoa(i)
		}
	}
	return "-1"
}

// TestEntryKeptOnce_AtACarrierTheEntryKeptWholeHoldsItsOwn pins the same at a
// property and a parameter, which keep what the position writes themselves.
func TestEntryKeptOnce_AtACarrierTheEntryKeptWholeHoldsItsOwn(t *testing.T) {
	t.Parallel()
	const schema = "{allOf: [{$ref: '#/components/schemas/Str'}, {additionalProperties: false, x-a: 1, readOnly: true}, " +
		"{x-a: 1, readOnly: true}]}"
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /p:
    get:
      parameters:
        - {name: q, in: query, schema: `+schema+`}
      responses:
        '200': {description: ok}
components:
  schemas:
    Str: {type: string}
    P: {type: object, properties: {a: `+schema+`}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	property := propertyOf(t, doc, "P", "a")
	assert.Equal(t, []string{"openapi:allOf/1", "openapi:x-a"}, unmodeledKeys(property.Unmodeled))
	assert.Equal(t, jsontext.Pointer("/components/schemas/P/properties/a/allOf/2/x-a"), property.Unmodeled["openapi:x-a"].Provenance.Pointer)
	parameter, ok := openapitest.ParamBySource(openapitest.FirstOp(t, doc.Services[0]), "q")
	require.True(t, ok)
	assert.Equal(t, []string{"openapi:schema/allOf/1", "openapi:schema/readOnly", "openapi:schema/x-a"}, unmodeledKeys(parameter.Unmodeled))
	for _, key := range []string{"openapi:schema/readOnly", "openapi:schema/x-a"} {
		assert.Equal(t, jsontext.Pointer("/paths/~1p/get/parameters/0/schema/allOf/2/"+strings.TrimPrefix(key, "openapi:schema/")),
			parameter.Unmodeled[key].Provenance.Pointer, key)
	}
}

// TestEntryKeptOnce_APromotedKeyStaysBesideTheEntryKeptWhole pins the one
// exception: a key the promotion policy reads, which an entry kept whole alone
// writes, is kept beside it too, where the promotion reads it; one another
// entry writes alike is that entry's.
func TestEntryKeptOnce_APromotedKeyStaysBesideTheEntryKeptWhole(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Str: {type: string}\n"+
		"    Alone: {allOf: [{$ref: '#/components/schemas/Str'}, {additionalProperties: false, x-stability: beta}]}\n"+
		"    Shared: {allOf: [{$ref: '#/components/schemas/Str'}, {additionalProperties: false, x-stability: beta}, {x-stability: beta}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	for name, at := range map[string]string{"Alone": "/allOf/1/x-stability", "Shared": "/allOf/2/x-stability"} {
		kept := typeByName(doc, name).Common().Unmodeled
		assert.Equal(t, []string{"openapi:allOf/1", "openapi:x-stability"}, unmodeledKeys(kept), name)
		assert.Equal(t, jsontext.Pointer("/components/schemas/"+name+at), kept["openapi:x-stability"].Provenance.Pointer, name)
	}
}

// keptOnceShapes are the entries the grid test below draws from: two vendor
// extensions apart, a closure and an unevaluated keyword that keep their entry
// whole, and a validation-only keyword with a residue keyword. Each key one
// writes is a member of the entry.
var keptOnceShapes = []string{
	"{x-a: 1}",
	"{x-a: 2}",
	"{additionalProperties: false, x-a: 1}",
	"{unevaluatedProperties: false, not: {required: [z]}}",
	"{not: {required: [z]}, readOnly: true}",
}

// keptOnceKeys are the members keptOnceShapes write that a position keeps.
var keptOnceKeys = []string{"x-a", "not", "readOnly"}

// TestEntryKeptOnce_EveryKeywordOfAGridIsKeptOnce composes every three of
// keptOnceShapes after a $ref to a string and to an object, and holds each
// member an entry writes to one record: inside its entry where that is kept
// whole, else at its keyword where no other entry kept apart writes it, else
// at the position, but readOnly, a residue keyword, at the nearest writer's
// (recordFoldedBranchResidue).
func TestEntryKeptOnce_EveryKeywordOfAGridIsKeptOnce(t *testing.T) {
	t.Parallel()
	var schemas strings.Builder
	schemas.WriteString("    Str: {type: string}\n    Obj: {type: object, properties: {a: {type: string}}}\n")
	n := 2 * len(keptOnceShapes) * len(keptOnceShapes) * len(keptOnceShapes)
	names, entries := make([]string, 0, n), make([][]string, 0, n)
	for _, target := range []string{"Str", "Obj"} {
		for i, a := range keptOnceShapes {
			for j, b := range keptOnceShapes {
				for k, d := range keptOnceShapes {
					name := fmt.Sprintf("%s%d%d%d", target, i, j, k)
					fmt.Fprintf(&schemas, "    %s: {allOf: [{$ref: '#/components/schemas/%s'}, %s, %s, %s]}\n", name, target, a, b, d)
					names, entries = append(names, name), append(entries, []string{a, b, d})
				}
			}
		}
	}
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", schemas.String()))
	openapitest.RequireNoErrorDiags(t, diags)
	for n, name := range names {
		td := typeByName(doc, name)
		require.NotNil(t, td, name)
		assertKeptOnce(t, name, td.Common().Unmodeled, entries[n])
	}
}

// assertKeptOnce holds each member of keptOnceKeys the entries of the allOf at
// the component name write to one record in kept.
func assertKeptOnce(t *testing.T, name string, kept ir.Unmodeled, entries []string) {
	t.Helper()
	at := "/components/schemas/" + name
	for _, key := range keptOnceKeys {
		var apart []string
		for i, e := range entries {
			index := strconv.Itoa(i + 1)
			if !strings.Contains(e, key+":") {
				continue
			}
			if _, whole := kept["openapi:allOf/"+index]; !whole {
				apart = append(apart, at+"/allOf/"+index+"/"+key)
			}
		}
		entry, ok := kept["openapi:"+key]
		switch len(apart) {
		case 0:
			assert.False(t, ok, "%s: %s kept beside the entries holding it", name, key)
		case 1:
			if assert.True(t, ok, "%s: %s kept nowhere", name, key) {
				assert.Equal(t, jsontext.Pointer(apart[0]), entry.Provenance.Pointer, "%s: %s", name, key)
			}
		default:
			shared := jsontext.Pointer(at)
			if key == "readOnly" {
				shared = jsontext.Pointer(apart[0])
			}
			if assert.True(t, ok, "%s: %s kept nowhere", name, key) {
				assert.Equal(t, shared, entry.Provenance.Pointer, "%s: %s", name, key)
			}
		}
	}
}
