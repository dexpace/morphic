// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestBoundOf_FillsAFieldForEveryValueBound holds boundOf to
// valueConstraintKeywords: each keyword the constraint reader fills a field for
// projects a field of a full reading, so a keyword the list gains and the switch
// lacks agrees with nothing it should meet, and anything else projects none.
func TestBoundOf_FillsAFieldForEveryValueBound(t *testing.T) {
	t.Parallel()
	full := &ir.Constraints{
		Min: new(ir.BigVal("1")), ExclusiveMin: new(ir.BigVal("1")), Max: new(ir.BigVal("9")),
		ExclusiveMax: new(ir.BigVal("9")), MultipleOf: new(ir.BigVal("2")), MinLength: new(int64(1)),
		MaxLength: new(int64(9)), MinProps: new(int64(1)), MaxProps: new(int64(9)), Pattern: "^a$",
	}
	for _, keyword := range valueConstraintKeywords {
		assert.NotEqual(t, ir.Constraints{}, boundOf(keyword, full), keyword)
	}
	assert.Equal(t, ir.Constraints{}, boundOf("title", full), "a keyword that is no value bound fills nothing")
	assert.Equal(t, ir.Constraints{}, boundOf("maxLength", nil), "nor does a reading of nothing")
}

// TestAgreement_ReadsABoundInTheDialectOfItsSchemas pins the dialect the
// writers of a value bound are read in. A numeric exclusiveMinimum is a bound
// from 3.1 on, so two writers of one meet and fold; in 3.0 it is no bound of
// the dialect, which the lowering reports and drops, so the writers state
// nothing and stay apart where they write it differently.
func TestAgreement_ReadsABoundInTheDialectOfItsSchemas(t *testing.T) {
	t.Parallel()
	conjunct := func(text string) *oas3.Schema {
		js := &oas3.JSONSchema[oas3.Referenceable]{}
		valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(text), js)
		require.NoError(t, err)
		require.Empty(t, valErrs, "the fixture parses cleanly")
		return js.GetSchema()
	}
	for version, apart := range map[string]bool{"3.0.3": true, "3.1.0": false} {
		a := newAgreement(NewRawLowerer(&soa.OpenAPI{OpenAPI: version}).Ctx)
		a.write(conjunct("{exclusiveMinimum: 5}"))
		a.write(conjunct("{exclusiveMinimum: 6}"))
		assert.Equal(t, apart, a.apart("exclusiveMinimum"), version)
	}
}
