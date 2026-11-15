// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// aliasLinkSpec is a document whose operation listPets answers with the schema
// body, and carries a link whose cursor reads the body's next_cursor member.
// The schemas are written as schemas, so a test can put an alias before or
// after the model it stands for.
func aliasLinkSpec(body, schemas string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /pets:
    get:
      operationId: listPets
      parameters: [{name: cursor, in: query, schema: {type: string}}]
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/` + body + `'}}}
          links:
            next: {operationId: listPets, parameters: {cursor: '$response.body#/next_cursor'}}
components:
  schemas:
` + schemas + `
`
}

// TestLinks_ReadAMemberThroughAnAlias holds a link reading a member of a body
// whose schema is one reference or two to a model to lowering that member, in
// whichever order the aliases and the model are declared: the link is typed,
// kept nowhere verbatim, and draws no finding.
func TestLinks_ReadAMemberThroughAnAlias(t *testing.T) {
	t.Parallel()
	const (
		page = "    Page:\n      type: object\n      properties:\n" +
			"        result: {type: array, items: {type: string}}\n        next_cursor: {type: string}"
		pets = "    Pets: {$ref: '#/components/schemas/Page'}"
		dogs = "    Dogs: {$ref: '#/components/schemas/Pets'}"
	)
	for name, tc := range map[string]struct{ body, schemas string }{
		"one hop, the alias first":        {"Pets", pets + "\n" + page},
		"one hop, the model first":        {"Pets", page + "\n" + pets},
		"two hops, aliases first":         {"Dogs", dogs + "\n" + pets + "\n" + page},
		"two hops, the model first":       {"Dogs", page + "\n" + pets + "\n" + dogs},
		"two hops, the nearer alias last": {"Dogs", dogs + "\n" + page + "\n" + pets},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, aliasLinkSpec(tc.body, tc.schemas))
			assert.Empty(t, diags, "no finding at all")

			op := openapitest.FindOp(t, doc, "listPets")
			require.Len(t, op.Responses, 1)
			require.Len(t, op.Responses[0].Links, 1, "the link is lowered")
			link := op.Responses[0].Links[0]
			require.Len(t, link.Params, 1)
			assert.Equal(t, ir.LinkValue{Kind: ir.LinkResponse, Response: &ir.PropPath{
				Segments: []ir.PropID{"p/openapi/components/schemas/Page/properties/next_cursor"},
			}}, link.Params[0].Value)
			for key := range op.Responses[0].Unmodeled {
				assert.False(t, strings.HasPrefix(key, "openapi:links"), "the link is kept verbatim: %s", key)
			}
		})
	}
}
