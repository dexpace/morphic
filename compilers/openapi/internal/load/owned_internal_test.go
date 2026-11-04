// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// ownedOf returns the values the table's check of the 3.1 document whose
// components.schemas are schemas leaves to the load phase, each placed where
// its node is written, as the source's check places one (walkedFrom).
func ownedOf(t *testing.T, schemas string) []oas.Owned {
	t.Helper()
	root := openapitest.YAMLDocument(t, openapitest.ComponentSpec(schemas))
	var owned []oas.Owned
	oas.NewChecker().Check(t.Context(), root, "3.1.0", oas.Options{
		Locate: walkedFrom(root, func(n *yaml.Node) ir.Provenance {
			return ir.Provenance{Position: ir.Position{Line: n.Line, Column: n.Column}}
		}),
		ReadAt: true,
		Owned:  func(o oas.Owned) { owned = append(owned, o) },
	})
	return owned
}

// TestOwnedFinding_JudgesEachValueInItsSchemasDialect pins the judgement of
// each value the check leaves to the load phase, read with the rows of the
// dialect its schema is read in: a bound no decimal spells, an exclusive bound
// in the other form, a list of items where the rows read no tuple, and an
// `$id` of another format than the rows give it, each an error at the keyword;
// a sound value, or a field the phase judges nothing of, none.
func TestOwnedFinding_JudgesEachValueInItsSchemasDialect(t *testing.T) {
	t.Parallel()
	rows := func(version, dialect string) oas.SchemaRules {
		r, _ := oas.Table().SchemaRules(version, dialect)
		return r
	}
	own, d4 := rows("3.1.0", ""), rows("3.1.0", "http://json-schema.org/draft-04/schema#")
	d7 := rows("3.1.0", "http://json-schema.org/draft-07/schema#")
	for _, tc := range []struct {
		name, schema string
		rules        oas.SchemaRules
		code         string
	}{
		{"a decimal", "{minimum: '5', maximum: 1e3, multipleOf: 2}", own, ""},
		{"no decimal", "{maximum: .inf}", own, diag.NumericPrecision},
		{"a step no decimal spells", "{multipleOf: abc}", own, diag.NumericPrecision},
		{"a number bound in 2020-12", "{exclusiveMinimum: 5}", own, ""},
		{"a boolean bound in 2020-12", "{exclusiveMinimum: true}", own, diag.ExclusiveBoundForm},
		{"a number bound past a decimal", "{exclusiveMaximum: .inf}", own, diag.NumericPrecision},
		{"a boolean bound in draft 4", "{minimum: 1, exclusiveMaximum: false}", d4, ""},
		{"a number bound in draft 4", "{exclusiveMinimum: 5}", d4, diag.ExclusiveBoundForm},
		{"a list of items in draft 4", "{items: [{type: string}]}", d4, ""},
		{"a list of items in 2020-12", "{items: [{type: string}]}", own, diag.InvalidItemsList},
		{"a fragment id in 2020-12", "{$id: '#leaf'}", own, diag.Validation + "/validation-invalid-format"},
		{"a fragment id in draft 7", "{$id: '#leaf'}", d7, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			owned := ownedOf(t, "    S: "+tc.schema+"\n")
			require.NotEmpty(t, owned, "the check leaves the value to the load phase")
			var codes []string
			for _, o := range owned {
				assert.Equal(t, jsontext.Pointer("/components/schemas/S"), o.Schema)
				if d, bad := ownedFinding(o, tc.rules); bad {
					codes = append(codes, d.Code)
					assert.Equal(t, ir.SeverityError, d.Severity)
					assert.Equal(t, o.Value.Line, d.Provenance.Position.Line, "placed at the keyword's value")
				}
			}
			var want []string
			if tc.code != "" {
				want = []string{tc.code}
			}
			assert.Equal(t, want, codes)
		})
	}
	assert.Empty(t, ownedOf(t, "    S: {items: {type: string}, type: array}\n"), "one items schema is the check's own")
	_, bad := ownedFinding(oas.Owned{Field: "type", Value: openapitest.StrNode("string")}, own)
	assert.False(t, bad, "a field the phase judges nothing of")
}

// TestOwnedFindings_ReportAnAliasedValueOnce pins that a value an alias places
// at several positions is one defect, reported once, where it is written.
func TestOwnedFindings_ReportAnAliasedValueOnce(t *testing.T) {
	t.Parallel()
	own, _ := oas.Table().SchemaRules("3.1.0", "")
	owned := ownedOf(t, "    A: {type: object, properties: {a: &bad {exclusiveMinimum: true}, b: *bad, c: *bad}}\n")
	require.Len(t, owned, 3, "each position reads the value")
	diags := ownedFindings(owned, func(oas.Owned) oas.SchemaRules { return own })
	require.Len(t, diags, 1)
	assert.Equal(t, diag.ExclusiveBoundForm, diags[0].Code)
	assert.NotContains(t, diags[0].Message, "read at", "the first read is where it is written")
}

// TestDialects_RowsAtFallBackToTheSource pins the rows a schema no root
// governs is read with: its document's, and the source's for a document the
// compile did not read beside it.
func TestDialects_RowsAtFallBackToTheSource(t *testing.T) {
	t.Parallel()
	own, _ := oas.Table().SchemaRules("3.1.0", "")
	d4, _ := oas.Table().SchemaRules("3.1.0", "http://json-schema.org/draft-04/schema#")
	d := Dialects{byDoc: map[bind.DocID]oas.SchemaRules{bind.Source: own, 1: d4}}
	assert.True(t, d.rowsAt(bind.Position{Doc: 1}).Takes("exclusiveMinimum", oas.Boolean), "its document's")
	assert.False(t, d.rowsAt(bind.Position{Doc: 2}).Takes("exclusiveMinimum", oas.Boolean), "the source's")
}

// TestReached_ASchemaTheSourceWritesUnmodelledIsJudgedInItsRows pins a value
// of a schema the source writes where the parser models none, an extension's
// content, which a $ref reaches: the source's own check reads no schema there,
// so the check of what references reach judges it, in the source's rows.
func TestReached_ASchemaTheSourceWritesUnmodelledIsJudgedInItsRows(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths: {}\n" +
		"x-defs: {A: {type: integer, exclusiveMinimum: true}}\n" +
		"components:\n  schemas:\n    S: {$ref: '#/x-defs/A'}\n"
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "s.yaml", Data: []byte(src)}, Options{})
	require.NoError(t, err)
	var at []jsontext.Pointer
	for _, d := range diags {
		if d.Code == diag.ExclusiveBoundForm {
			at = append(at, d.Provenance.Pointer)
		}
	}
	assert.Equal(t, []jsontext.Pointer{"/x-defs/A/exclusiveMinimum"}, at)
}
