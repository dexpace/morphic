// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// aliasPassed is one kind of reference a schema $ref's pointer can pass: the
// components it declares, Y and its alias X, and the pointer from either to
// the schema Y declares, %s naming the one passed.
type aliasPassed struct {
	name, components, pointer string
}

// aliasesPassed holds each kind of reference object a pointer can pass, and a
// path item $ref, under which X is the path /x and Y the path /y.
var aliasesPassed = []aliasPassed{
	{"parameter", `
  parameters:
    X: {$ref: '#/components/parameters/Y'}
    Y: {name: p, in: query, schema: ` + identitySchema + `}`, "/components/parameters/%s/schema"},
	{"header", `
  headers:
    X: {$ref: '#/components/headers/Y'}
    Y: {schema: ` + identitySchema + `}`, "/components/headers/%s/schema"},
	{"request body", `
  requestBodies:
    X: {$ref: '#/components/requestBodies/Y'}
    Y: {content: {application/json: {schema: ` + identitySchema + `}}}`,
		"/components/requestBodies/%s/content/application~1json/schema"},
	{"response", `
  responses:
    X: {$ref: '#/components/responses/Y'}
    Y: {description: ok, content: {application/json: {schema: ` + identitySchema + `}}}`,
		"/components/responses/%s/content/application~1json/schema"},
	{"path item", `
  pathItems:
    X: {$ref: '#/components/pathItems/Y'}
    Y: {get: {responses: {"200": {description: ok, content: {application/json: {schema: ` + identitySchema + `}}}}}}`,
		"/components/pathItems/%s/get/responses/200/content/application~1json/schema"},
	{"media type", `
  mediaTypes:
    X: {$ref: '#/components/mediaTypes/Y'}
    Y: {schema: ` + identitySchema + `}`, "/components/mediaTypes/%s/schema"},
	{"path item in paths", "", "/paths/~1%s/get/responses/200/content/application~1json/schema"},
}

// spell is a's pointer through the component named name, or for a path item
// in paths, the path named by it in lower case.
func (a aliasPassed) spell(name string) string {
	if a.components == "" {
		name = strings.ToLower(name)
	}
	return fmt.Sprintf(a.pointer, name)
}

// identitySchema is a schema a lowering hoists to a node of its own.
const identitySchema = "{type: object, properties: {a: {type: string}}}"

// identitySpec is a spec whose two operations each type a body by a schema
// $ref to the schema of a, one spelled through Y, one through its alias X.
func identitySpec(a aliasPassed) string {
	return `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /through:
    get:
      operationId: through
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {$ref: '#` + a.spell("X") + `'}}
  /direct:
    get:
      operationId: direct
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {$ref: '#` + a.spell("Y") + `'}}
  /x: {$ref: '#/paths/~1y'}
  /y:
    get:
      operationId: y
      responses: {"200": {description: ok, content: {application/json: {schema: ` + identitySchema + `}}}}
components:` + a.components + "\n"
}

// TestReferenceIdentity_ASchemaRefThroughAnAliasIsOneNode pins that a schema
// $ref whose pointer passes a reference lowers what it reaches where that is
// declared, at the end of the reference's chain, so it and a $ref spelled to
// the declaration name one node (docs/compilers/openapi.md#references). The
// alias is declared before what it names, and each spec compiles alike with
// every mapping reversed.
func TestReferenceIdentity_ASchemaRefThroughAnAliasIsOneNode(t *testing.T) {
	t.Parallel()
	for _, a := range aliasesPassed {
		t.Run(a.name, func(t *testing.T) {
			t.Parallel()
			spec := identitySpec(a)
			doc, diags := parseFull(t, spec)
			assertNoErrorDiags(t, diags)
			declared := ir.TypeID("t/anon" + a.spell("Y"))
			require.Contains(t, doc.Types, declared, "the schema lowers where it is declared")
			alias := ir.TypeID("t/anon" + a.spell("X"))
			assert.NotContains(t, doc.Types, alias, "and at no pointer an alias spells")
			for _, name := range []string{"through", "direct"} {
				op, ok := opByName(doc, name)
				require.True(t, ok, name)
				assert.Equal(t, declared, firstContent(t, op).Type.Target, "%s types its body by that node", name)
			}

			assertOrderFree(t, spec)
		})
	}
}

// assertOrderFree compiles spec as written and with every mapping reversed,
// and requires one type registry and one set of findings of the two. Neither
// may hold an error, since harness.Check's order oracle runs only past them.
func assertOrderFree(t *testing.T, spec string) {
	t.Helper()
	forward, forwardDiags := parseFull(t, spec)
	back, backDiags := parseFull(t, string(reversed(t, spec)))
	assert.Empty(t, cmp.Diff(typesJSON(t, forward), typesJSON(t, back)), "one registry in either order")
	assert.Equal(t, placedFindings(forwardDiags), placedFindings(backDiags), "one set of findings in either order")
}

// typesJSON is doc's type registry encoded, keys sorted.
func typesJSON(t *testing.T, doc *ir.Document) string {
	t.Helper()
	out, err := json.Marshal(doc.Types, json.Deterministic(true))
	require.NoError(t, err)
	return string(out)
}

// placedFindings returns each of diags as its severity, code, pointer and
// message, sorted: what a finding says and where, whichever order a source's
// mappings are written in, which moves the lines and columns alone.
func placedFindings(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s: %s", d.Severity, d.Code, d.Provenance.Pointer, d.Message))
	}
	slices.Sort(out)
	return out
}
