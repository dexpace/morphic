// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// The ids the dialects the table has rows for are named by.
const (
	oasDialect = "https://spec.openapis.org/oas/3.1/dialect/base"
	draft2020  = "https://json-schema.org/draft/2020-12/schema"
	draft2019  = "https://json-schema.org/draft/2019-09/schema"
	draft07    = "http://json-schema.org/draft-07/schema#"
	draft06    = "http://json-schema.org/draft-06/schema#"
	draft04    = "http://json-schema.org/draft-04/schema#"
)

// rulesOf returns the table's rows at version in dialect.
func rulesOf(version, dialect string) oas.SchemaRules {
	r, _ := oas.Table().SchemaRules(version, dialect)
	return r
}

// TestSchemaRules_AnswerAsTheTableDoes pins each question the schema lowering
// asks at every minor the table covers, in each dialect it has rows for, and at
// versions it does not, where every answer is false. The two exclusive bounds
// share one row's answer, as the lowering reads them through one.
func TestSchemaRules_AnswerAsTheTableDoes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version, dialect                               string
		nullable, boolBound, numberBound, items, tuple bool
	}{
		{"3.0.3", "", true, true, false, true, false},
		{"3.1.0", "", false, false, true, false, true},
		{"3.2.0", "", false, false, true, false, true},
		{"3.1.0", oasDialect, false, false, true, false, true},
		{"3.2.0", draft2020, false, false, true, false, true},
		{"3.1.0", draft04, false, true, false, false, false},
		{"3.2.0", draft04, false, true, false, false, false},
		{"3.0.3", draft2020, true, true, false, true, false},
		{"", "", false, false, false, false, false},
		{"3.3.0", draft04, false, false, false, false, false},
	} {
		t.Run(tc.version+" "+tc.dialect, func(t *testing.T) {
			t.Parallel()
			r := rulesOf(tc.version, tc.dialect)
			assert.Equal(t, tc.nullable, r.Defines("nullable"))
			assert.Equal(t, tc.tuple, r.Defines("prefixItems"))
			for _, bound := range []string{"exclusiveMinimum", "exclusiveMaximum"} {
				assert.Equal(t, tc.boolBound, r.Takes(bound, oas.Boolean), bound)
				assert.Equal(t, tc.numberBound, r.Takes(bound, oas.Number), bound)
			}
			assert.Equal(t, tc.items, r.RequiredWhen("items", "type", "array"))
		})
	}
}

// TestSchemaRules_ADialectNamesItsKeywordsAlone pins what a dialect changes: the
// keywords, never a field OpenAPI adds to the Schema Object or a rule it
// states, which stay the minor's, so a reading of `nullable` or of a required
// `items` asks the document, not the schema. A dialect's id matches with or
// without its trailing empty fragment, and an absolute URI the table has no
// rows for reads as the minor's own and is unread; one no absolute URI names
// is validation's to report.
func TestSchemaRules_ADialectNamesItsKeywordsAlone(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.1.0", "3.2.0"} {
		own := rulesOf(version, "")
		for _, dialect := range []string{oasDialect, draft2020, draft2019, draft07, draft06, draft04, "https://example.com/custom"} {
			r := rulesOf(version, dialect)
			for _, field := range []string{"nullable", "discriminator", "xml", "externalDocs", "example"} {
				assert.Equal(t, own.Defines(field), r.Defines(field), "%s %s %s", version, dialect, field)
			}
			assert.Equal(t, own.RequiredWhen("items", "type", "array"), r.RequiredWhen("items", "type", "array"))
		}
	}
	d04 := rulesOf("3.1.0", draft04)
	assert.True(t, d04.Defines("definitions"))
	assert.True(t, d04.Defines("dependencies"))
	assert.True(t, d04.Takes("items", oas.List), "draft 4 writes a tuple as a list of items")
	assert.True(t, d04.Takes("type", oas.StringList))
	assert.False(t, d04.Defines("$id"), "draft 4 spells it id")
	assert.False(t, d04.Defines("const"))
	assert.True(t, rulesOf("3.1.0", "http://json-schema.org/draft-04/schema").Takes("exclusiveMinimum", oas.Boolean),
		"the empty fragment names the same dialect")
	assert.True(t, rulesOf("3.1.0", draft2020+"#").Takes("exclusiveMinimum", oas.Number))
	for _, tc := range []struct {
		version, dialect string
		unread           bool
	}{
		{"3.1.0", "", false}, {"3.1.0", draft04, false}, {"3.1.0", draft2020, false}, {"3.2.0", oasDialect, false},
		{"3.1.0", "https://example.com/custom", true}, {"3.2.0", "http://json-schema.org/draft-03/schema#", true},
		{"3.1.0", "https://json-schema.org/draft-04/schema#", true},
		{"3.1.0", "rel/dialect", false}, {"3.1.0", "4", false},
		{"3.0.3", "https://example.com/custom", false}, {"3.3.0", "https://example.com/custom", false},
	} {
		r, unread := oas.Table().SchemaRules(tc.version, tc.dialect)
		assert.Equal(t, tc.unread, unread, "%s %s", tc.version, tc.dialect)
		if unread {
			assert.Equal(t, rulesOf(tc.version, "").Defines("prefixItems"), r.Defines("prefixItems"),
				"%s %s reads as the minor's own", tc.version, tc.dialect)
		}
	}
}

// TestSchemaRules_ReadEachRowAtItsMinor pins what the rows say beyond a field's
// own shape: a second shape counts only from the minor that admits it, and a
// required field only where the value beside it decides the rule.
func TestSchemaRules_ReadEachRowAtItsMinor(t *testing.T) {
	t.Parallel()
	r30, r31 := rulesOf("3.0.3", ""), rulesOf("3.1.0", "")
	assert.True(t, r30.Takes("additionalProperties", oas.Boolean), "3.0 admits a boolean beside the schema")
	assert.False(t, r30.Takes("type", oas.StringList), "3.0 types one name")
	assert.True(t, r31.Takes("type", oas.StringList))
	assert.False(t, r30.Takes("nonesuch", oas.String))
	assert.False(t, r30.RequiredWhen("items", "type", "object"), "an object needs no items")
	assert.False(t, r30.RequiredWhen("items", "format", "array"), "another field decides nothing")
	assert.False(t, r30.RequiredWhen("properties", "type", "array"))
	empty, unread := oas.Spec{}.SchemaRules("3.0.3", "")
	assert.False(t, empty.Defines("nullable"), "a table with no Schema Object defines nothing")
	assert.False(t, unread)
	checked, _ := oas.NewChecker().Spec().SchemaRules("3.0.3", "")
	assert.True(t, checked.Defines("nullable"), "a checker's table is the table")
}

// TestSchemaRules_EachDraftReadsItsOwnKeywords pins what tells the drafts
// before 2020-12 apart: which writes a tuple as a list of items with a rest in
// additionalItems, which has dependencies, which names a resource by id or by
// $id, which spells an exclusive bound as a boolean, and which ignores the
// keywords beside a $ref, which a minor's own rows never report it does.
func TestSchemaRules_EachDraftReadsItsOwnKeywords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		dialect                                      string
		list, rest, dependencies, id, boolean, inert bool
		recursive, defs, constant                    bool
	}{
		{draft04, true, true, true, true, true, true, false, false, false},
		{draft06, true, true, true, false, false, true, false, false, true},
		{draft07, true, true, true, false, false, true, false, false, true},
		{draft2019, true, true, false, false, false, false, true, true, true},
		{draft2020, false, false, false, false, false, false, false, true, true},
		{oasDialect, false, false, false, false, false, false, false, true, true},
		{"", false, false, false, false, false, false, false, true, true},
	} {
		r := rulesOf("3.1.0", tc.dialect)
		assert.Equal(t, tc.list, r.Takes("items", oas.List), "%s items list", tc.dialect)
		assert.Equal(t, tc.rest, r.Defines("additionalItems"), "%s additionalItems", tc.dialect)
		assert.Equal(t, tc.dependencies, r.Defines("dependencies"), "%s dependencies", tc.dialect)
		assert.Equal(t, tc.id, r.Defines("id") && !r.Defines("$id"), "%s id", tc.dialect)
		assert.Equal(t, tc.boolean, r.Takes("exclusiveMinimum", oas.Boolean), "%s bound", tc.dialect)
		assert.Equal(t, tc.inert, r.IgnoresRefFields(), "%s $ref siblings", tc.dialect)
		assert.Equal(t, tc.recursive, r.Defines("$recursiveRef"), "%s $recursiveRef", tc.dialect)
		assert.Equal(t, tc.defs, r.Defines("$defs"), "%s $defs", tc.dialect)
		assert.Equal(t, tc.constant, r.Defines("const"), "%s const", tc.dialect)
	}
	assert.False(t, rulesOf("3.0.3", "").IgnoresRefFields(), "3.0's own rows: the minor, not a dialect, ignores them")
	assert.False(t, oas.SchemaRules{}.IgnoresRefFields())
}

// TestSchemaRules_FormattedNamesTheFormatARowGives pins the format a row gives
// a field: 2019-09's $anchor the drafts' plain-name grammar, 2020-12's its own,
// and a field the rows do not define none.
func TestSchemaRules_FormattedNamesTheFormatARowGives(t *testing.T) {
	t.Parallel()
	d2019 := rulesOf("3.1.0", "https://json-schema.org/draft/2019-09/schema")
	assert.True(t, d2019.Formatted("$anchor", oas.PlainName))
	assert.False(t, rulesOf("3.1.0", "").Formatted("$anchor", oas.PlainName))
	assert.True(t, rulesOf("3.1.0", "").Formatted("$anchor", oas.AnchorName))
	assert.False(t, d2019.Formatted("nonesuch", oas.PlainName), "a field the rows do not define")
}
