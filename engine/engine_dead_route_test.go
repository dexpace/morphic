// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// deadRouteSpec writes P, a union of keyword over branches beside an object
// body, with a discriminator whose mapping names A and target under s.
func deadRouteSpec(version, keyword, branches, target string) string {
	return fmt.Sprintf(`openapi: %s
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {type: object, properties: {kind: {type: string}}}
    Str: {type: string}
    Other: {type: object, properties: {kind: {type: string}}}
    P:
      type: object
      properties: {kind: {type: string}}
      %s: [%s]
      discriminator:
        propertyName: kind
        mapping: {a: '#/components/schemas/A', s: '#/components/schemas/%s'}
`, version, keyword, branches, target)
}

// TestEngine_AMappingToADeadBranchValidatesClean drives the compiler and the
// validate pass together over a discriminator mapping entry naming a branch no
// object satisfies, beside an object body: the entry routes to no variant, so
// the pass finds nothing to report and the document errs nowhere, under oneOf
// and anyOf, at 3.0.3 and 3.1.0, in both orders of the branches. An entry
// naming a schema no branch lists is still the pass's error. Neither side's
// own suite sees this: the compiler's corpus never runs the pass.
func TestEngine_AMappingToADeadBranchValidatesClean(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	orders := []string{"{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/Str'}",
		"{$ref: '#/components/schemas/Str'}, {$ref: '#/components/schemas/A'}"}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, keyword := range []string{"oneOf", "anyOf"} {
			for _, order := range orders {
				t.Run(strings.Join([]string{version, keyword, order}, "/"), func(t *testing.T) {
					t.Parallel()
					res, err := eng.Run(t.Context(), writeSpec(t, deadRouteSpec(version, keyword, order, "Str")),
						engine.RunOptions{Verify: true})
					require.NoError(t, err)
					require.NotNil(t, res.Document)
					for _, d := range res.Diagnostics {
						assert.NotEqual(t, ir.SeverityError, d.Severity, "%s %s", d.Code, d.Message)
					}

					res, err = eng.Run(t.Context(), writeSpec(t, deadRouteSpec(version, keyword, order, "Other")),
						engine.RunOptions{Verify: true})
					require.NoError(t, err)
					var errs []string
					for _, d := range res.Diagnostics {
						if d.Severity == ir.SeverityError {
							errs = append(errs, d.Code)
						}
					}
					assert.Equal(t, []string{"pass/discriminator-missing-variant"}, errs,
						"an entry naming no branch is the pass's to report")
				})
			}
		}
	}
}
