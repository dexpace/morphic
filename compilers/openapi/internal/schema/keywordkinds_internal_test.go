// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
)

// TestKeywordKinds_ListsEveryValueBoundAndTheContentVocabulary holds the
// table's rows to the keywords annotation.Constraints and scalarEncoding read,
// so a bound added to one list and not to the table, or the reverse, fails here.
func TestKeywordKinds_ListsEveryValueBoundAndTheContentVocabulary(t *testing.T) {
	t.Parallel()
	want := slices.Sorted(slices.Values(slices.Concat(valueConstraintKeywords, contentKeywords)))
	assert.Equal(t, want, slices.Sorted(maps.Keys(keywordKinds)))
}

// TestBoundsFor_ReadsEachBoundForTheKindsItAppliesTo holds the table, the groups
// boundsFor maps kinds to and the groups annotation.Constraints reads to one
// another: each value bound is read by a reader of the kinds the table gives
// it, and by no reader of every other kind.
func TestBoundsFor_ReadsEachBoundForTheKindsItAppliesTo(t *testing.T) {
	t.Parallel()
	for _, keyword := range valueConstraintKeywords {
		literal := "2"
		if keyword == "pattern" {
			literal = "'^a'"
		}
		s := openapitest.SchemaFromYAML(t.Context(), t, keyword+": "+literal+"\n").GetSchema()
		kinds := keywordKinds[keyword]
		read, _, _ := annotation.Constraints(s, boundsFor(kinds), false, "/p", provenance.Doc{})
		assert.NotNil(t, read, keyword)
		unread, _, _ := annotation.Constraints(s, boundsFor(anyInstance&^kinds), false, "/p", provenance.Doc{})
		assert.Nil(t, unread, keyword)
	}
}

// TestDeclaredKinds_ReadsTheTypesASchemaDeclares pins what a position reads
// bounds for: the kinds of the types it declares, null aside, and every kind
// but null where it declares none, writes null alone or a spelling no dialect
// defines.
func TestDeclaredKinds_ReadsTheTypesASchemaDeclares(t *testing.T) {
	t.Parallel()
	for body, want := range map[string]kindSet{
		"description: d\n":                 anyInstance,
		"type: 'null'\n":                   anyInstance,
		"type: strin\n":                    anyInstance,
		"type: [string, 'null']\n":         kindString,
		"type: [integer, object]\n":        kindInteger | kindObject,
		"type: [number, array, boolean]\n": kindNumber | kindArray | kindBoolean,
	} {
		s := openapitest.SchemaFromYAML(t.Context(), t, body).GetSchema()
		assert.Equal(t, want, declaredKinds(s), body)
	}
}

// TestTypeKinds_ReadsOneListedType pins a variant's kinds: its type's, and
// every kind but null for a spelling no dialect defines.
func TestTypeKinds_ReadsOneListedType(t *testing.T) {
	t.Parallel()
	assert.Equal(t, kindString, typeKinds("string"))
	assert.Equal(t, kindNumber, typeKinds("number"))
	assert.Equal(t, anyInstance, typeKinds("strin"))
}

// TestTaken_AKeywordTheTableDoesNotListAppliesToEveryKind pins the default of
// taken: a keyword applying to no kind in particular is read by every reader.
func TestTaken_AKeywordTheTableDoesNotListAppliesToEveryKind(t *testing.T) {
	t.Parallel()
	assert.True(t, taken(0, "description"))
	assert.False(t, taken(kindString|kindObject, "minimum"))
	assert.True(t, taken(kindInteger, "minimum"))
}
