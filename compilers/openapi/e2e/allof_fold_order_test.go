// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestAllOfFold_AModelReportsWhereItsFirstRedeclarationIsWritten pins when a
// model's folds are queued: at its first redeclared field, as the walk meets
// it. A model holding another, each redeclaring a field, reports before or
// after the one it holds as the two are written, whichever order that is, and
// both orders report the same findings. Queued once a model's lowering
// returned instead, the model it holds would report first in both orders.
func TestAllOfFold_AModelReportsWhereItsFirstRedeclarationIsWritten(t *testing.T) {
	t.Parallel()
	const (
		field  = "p: {type: integer}"
		nested = "q: {allOf: [{properties: {r: {type: string}}}, {properties: {r: {type: integer}}}]}"
		p      = jsontext.Pointer("/components/schemas/X/allOf/1/properties/p")
		r      = jsontext.Pointer("/components/schemas/X/allOf/1/properties/q/allOf/1/properties/r")
	)
	reported := map[string][]jsontext.Pointer{}
	for name, tc := range map[string]struct {
		props string
		want  []jsontext.Pointer
	}{
		"the field first":      {props: "{" + field + ", " + nested + "}", want: []jsontext.Pointer{p, r}},
		"the held model first": {props: "{" + nested + ", " + field + "}", want: []jsontext.Pointer{r, p}},
	} {
		_, diags := parseFull(t, openapitest.ComponentSpec(
			"    X:\n      allOf:\n        - properties: {p: {type: string}}\n        - properties: "+tc.props+"\n"))
		var got []jsontext.Pointer
		for _, d := range diags {
			if d.Code == diag.ConflictingRedecl {
				got = append(got, d.Provenance.Pointer)
			}
		}
		assert.Equal(t, tc.want, got, name)
		reported[name] = got
	}
	assert.ElementsMatch(t, reported["the field first"], reported["the held model first"],
		"the declaration order moves the reports, and changes none")
}
