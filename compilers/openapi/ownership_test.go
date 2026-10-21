// This file is a package-level suite, not a per-source-file test: it holds the
// rule that a coordinate is built by the lowering that owns it whichever path
// reaches it first (docs/micro-compiler-design.md §6.2) to every shape that
// broke it, so it has no single source file to pair with.
package openapi_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

const ownershipHead = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n"

// okResponse is a GET operation answering 200 with content holding schema.
func okResponse(path, opID, schema string) string {
	return fmt.Sprintf("  %s:\n    get:\n      operationId: %s\n      responses:\n        '200':\n"+
		"          description: ok\n          content: {application/json: {schema: %s}}\n", path, opID, schema)
}

// nestedArrays is an object body wrapped in arrays, close enough to the schema
// depth bound that a lowering entering it from a reference's depth, rather than
// its own, would cross the bound.
func nestedArrays() string {
	body := "{type: object, properties: {v: {type: string}}}"
	for range 254 {
		body = "{type: array, items: " + body + "}"
	}
	return body
}

// ownershipCases are the shapes in which a reference reached a coordinate before
// the lowering that owns it, each written with the reference first, so that the
// reversed order has the owner first.
var ownershipCases = []struct {
	name, spec string
	// check asserts what the shape must give in either order, on top of the
	// harness's oracles.
	check func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic)
}{
	{name: "a redeclaration conflicting with a type declared later (GitHub #898)",
		spec: ownershipHead + "paths: {}\ncomponents:\n  schemas:\n" +
			"    B:\n      allOf:\n        - properties: {m: {type: array, items: {type: string}}}\n" +
			"        - properties: {m: {$ref: '#/components/schemas/A'}}\n" +
			"    A: {type: object, properties: {x: {type: string}}}\n",
		check: func(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
			assert.Equal(t, 1, countAt(diags, "openapi/conflicting-redeclaration", "/components/schemas/B/allOf/1/properties/m"),
				"an array against an object is reported whichever lowers first")
		}},
	{name: "a $ref to a property's schema before the model declaring it (GitHub #750)",
		spec: ownershipHead + "paths: {}\ncomponents:\n  schemas:\n" +
			"    Use: {type: object, properties: {t: {$ref: '#/components/schemas/Pet/properties/tags'}}}\n" +
			"    Pet:\n      type: object\n" +
			"      properties: {tags: {type: array, items: {type: string}, default: [], readOnly: true}}\n",
		check: func(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
			assert.Zero(t, countAt(diags, "openapi/degraded-construct", "/components/schemas/Pet/properties/tags/readOnly"),
				"the property carries readOnly, so no node keeps it, whoever reaches the position first")
		}},
	{name: "a $ref to a response body's property under /paths (GitHub #750)",
		spec: ownershipHead + "paths:\n" +
			okResponse("/a", "getA", "{$ref: '#/paths/~1b/get/responses/200/content/application~1json/schema/properties/p'}") +
			okResponse("/b", "getB", "{type: object, properties: {p: {type: object, readOnly: true, properties: {v: {type: string}}}}}"),
		check: func(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
			assert.Zero(t, countAt(diags, "openapi/degraded-construct",
				"/paths/~1b/get/responses/200/content/application~1json/schema/properties/p/readOnly"))
		}},
	{name: "a $ref to a parameter's schema before its operation (GitHub #750)",
		spec: ownershipHead + "paths:\n" +
			okResponse("/a", "getA", "{$ref: '#/paths/~1b/get/parameters/0/schema'}") +
			"  /b:\n    get:\n      operationId: getB\n" +
			"      parameters: [{name: q, in: query, schema: {type: object, writeOnly: true, properties: {v: {type: string}}}}]\n" +
			"      responses: {'204': {description: none}}\n",
		check: func(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
			const at = "/paths/~1b/get/parameters/0/schema/writeOnly"
			require.Equal(t, 1, countAt(diags, "openapi/degraded-construct", at),
				"the parameter's own report, and no node's beside it")
			for _, d := range diags {
				if string(d.Provenance.Pointer) == at {
					assert.Contains(t, d.Message, "no ir.Parameter home", "the parameter keeps writeOnly, no node")
				}
			}
		}},
	{name: "a body nested to the depth bound, referenced before it (GitHub #748)",
		spec: ownershipHead + "paths:\n" +
			okResponse("/a", "getA", "{$ref: '#/paths/~1b/get/responses/200/content/application~1json/schema'}") +
			okResponse("/b", "getB", nestedArrays()),
		check: func(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
			assert.Zero(t, countAt(diags, "openapi/degraded-construct", ""), "no position crosses the bound")
		}},
	{name: "a $ref into an enum member (GitHub #730)",
		spec: ownershipHead + "paths: {}\ncomponents:\n  schemas:\n" +
			"    Out: {$ref: '#/components/schemas/A/enum/0', description: x}\n" +
			"    A: {enum: [{type: object}, s]}\n",
		check: func(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
			_, literal := doc.Types["t/anon/components/schemas/A/enum/0"].(*ir.Literal)
			assert.True(t, literal, "the member is the enum's literal, which owns the position")
		}},
	{name: "a $ref to a boolean schema before its owner (GitHub #861)",
		spec: ownershipHead + "paths: {}\ncomponents:\n  schemas:\n" +
			"    O: {$ref: '#/components/schemas/L/items'}\n    L: {type: array, items: false}\n",
		check: func(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
			_, built := doc.Types["t/anon/components/schemas/L/items"]
			assert.True(t, built)
		}},
	{name: "a request body another operation mounts (GitHub #433)",
		spec: ownershipHead + "paths:\n" +
			"  /b:\n    post:\n      operationId: postB\n      requestBody: {$ref: '#/paths/~1a/post/requestBody'}\n" +
			"      responses: {'204': {description: none}}\n" +
			"  /a:\n    post:\n      operationId: postA\n" +
			"      requestBody: {content: {application/json: {schema: {type: object, properties: {v: {type: string}}}}}}\n" +
			"      responses: {'204': {description: none}}\n",
		check: func(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
			body := doc.Types["t/anon/paths/~1a/post/requestBody/content/application~1json/schema"]
			require.NotNil(t, body)
			assert.Equal(t, "post_a_request", body.Common().Name.Hint, "named by the operation declaring it")
		}},
	{name: "a header another response mounts under another key (GitHub #433)",
		spec: ownershipHead + "paths:\n" +
			"  /b:\n    get:\n      operationId: getB\n      responses:\n        '200':\n          description: ok\n" +
			"          headers: {Y: {$ref: '#/paths/~1a/get/responses/200/headers/X'}}\n" +
			"  /a:\n    get:\n      operationId: getA\n      responses:\n        '200':\n          description: ok\n" +
			"          headers: {X: {schema: {type: object, properties: {v: {type: string}}}}}\n",
		check: func(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
			header := doc.Types["t/anon/paths/~1a/get/responses/200/headers/X/schema"]
			require.NotNil(t, header)
			assert.Equal(t, "x", header.Common().Name.Hint, "named by the key it is declared under")
		}},
}

// TestCompile_AReferenceLeavesItsTargetToItsOwnerInEitherOrder pins the rule on
// each shape that broke it: a reference never builds the coordinate it names,
// so the coordinate's owner builds it, in its own context, in either order.
// Each shape passes every harness oracle, the two-order one included, and
// gives what check asserts in both orders.
func TestCompile_AReferenceLeavesItsTargetToItsOwnerInEitherOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range ownershipCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := harness.Check(t.Context(), "spec.yaml", []byte(tc.spec))
			require.Equal(t, harness.OutcomeOK, res.Outcome, res.Detail)
			for _, src := range [][]byte{[]byte(tc.spec), reversed(t, tc.spec)} {
				doc, diags, err := openapi.New().Compile(t.Context(),
					[]compilers.Source{{Path: "spec.yaml", Data: src}}, compilers.Options{})
				require.NoError(t, err)
				require.NotNil(t, doc)
				tc.check(t, doc, diags)
			}
		})
	}
}

// countAt counts the diagnostics coded code at pointer, or anywhere for an
// empty pointer.
func countAt(diags []ir.Diagnostic, code, pointer string) int {
	n := 0
	for _, d := range diags {
		if d.Code == code && (pointer == "" || string(d.Provenance.Pointer) == pointer) {
			n++
		}
	}
	return n
}
