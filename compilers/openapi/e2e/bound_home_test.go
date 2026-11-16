// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// homeRow is one schema written at a carrier, a property, a parameter or a
// header, and where its bounds are held: the carrier's own, the kind of the
// node its type names, and that node's.
type homeRow struct {
	name    string
	schema  string
	kind    string
	carrier map[string]any
	node    map[string]any
	// sibling names the keyword written beside a $ref, which OpenAPI 3.0
	// ignores there and the compiler warns of.
	sibling string
}

// position is a kind of carrier: how it writes the schemas of rows, the ID of
// the carrier a row names and the pointer of the schema it writes.
type position struct {
	name      string
	spec      func(version string, rows []homeRow, schemas []string) string
	carrierID func(row homeRow) string
	schemaAt  func(i int, row homeRow) string
}

const (
	holderPropertiesAt = "/components/schemas/Holder/properties/"
	boundOperationAt   = "/paths/~1a/get"
)

// positions are the three places a schema is written beside the carrier that
// names it. The rows written at each are the same, so a fact that holds of one
// is asked of the others.
var positions = []position{
	{
		name: "property",
		spec: func(version string, rows []homeRow, schemas []string) string {
			props := make([]string, 0, len(rows))
			for _, r := range rows {
				props = append(props, r.name+": "+r.schema)
			}
			holder := "Holder: {type: object, properties: {" + strings.Join(props, ", ") + "}}"
			// Between the shared schemas, so the reversed order puts it on the
			// other side of each.
			return admissionSpec(version, "", slices.Concat(schemas[:1], []string{holder}, schemas[1:])...)
		},
		carrierID: func(r homeRow) string { return "p/openapi/components/schemas/Holder/properties/" + r.name },
		schemaAt:  func(_ int, r homeRow) string { return holderPropertiesAt + r.name },
	},
	{
		name: "parameter",
		spec: func(version string, rows []homeRow, schemas []string) string {
			var b strings.Builder
			b.WriteString("paths:\n  /a:\n    get:\n      parameters:\n")
			for _, r := range rows {
				fmt.Fprintf(&b, "        - {name: %s, in: query, schema: %s}\n", r.name, r.schema)
			}
			b.WriteString("      responses:\n        '200': {description: ok}\n")
			return admissionSpec(version, b.String(), schemas...)
		},
		carrierID: func(r homeRow) string { return "param/openapi/paths/~1a/get/parameters/" + r.name + "/query" },
		schemaAt:  func(i int, _ homeRow) string { return fmt.Sprintf("%s/parameters/%d/schema", boundOperationAt, i) },
	},
	{
		name: "header",
		spec: func(version string, rows []homeRow, schemas []string) string {
			var b strings.Builder
			b.WriteString("paths:\n  /a:\n    get:\n      responses:\n        '200':\n          description: ok\n          headers:\n")
			for _, r := range rows {
				fmt.Fprintf(&b, "            %s: {schema: %s}\n", r.name, r.schema)
			}
			return admissionSpec(version, b.String(), schemas...)
		},
		carrierID: func(r homeRow) string { return "p/openapi/paths/~1a/get/responses/200/headers/" + r.name },
		schemaAt: func(_ int, r homeRow) string {
			return boundOperationAt + "/responses/200/headers/" + r.name + "/schema"
		},
	},
}

// sharedSchemas are the components every row may name.
var sharedSchemas = []string{
	"Str: {type: string}",
	"Obj: {type: object, properties: {a: {type: string}}}",
}

// placement is a carrier as the document holds it and the node its type
// names, nil where the type is a primitive every use shares.
type placement struct {
	carrier map[string]any
	typeID  string
	node    map[string]any
}

// placementOf finds the property, parameter or header with ID carrierID in doc,
// read as the JSON encodes it, and the type it names.
func placementOf(t *testing.T, doc *ir.Document, carrierID string) placement {
	t.Helper()
	tree, ok := documentTree(t, doc).(map[string]any)
	require.True(t, ok)
	var carrier map[string]any
	walkObjects(tree, func(m map[string]any) {
		if id, _ := m["id"].(string); id == carrierID {
			carrier = m
		}
	})
	require.NotNil(t, carrier, "the document holds no carrier %s", carrierID)
	ref, ok := carrier["type"].(map[string]any)
	require.True(t, ok, "the carrier %s names a type", carrierID)
	typeID, _ := ref["target"].(string)
	types, _ := tree["types"].(map[string]any)
	node, _ := types[typeID].(map[string]any)
	return placement{carrier: carrier, typeID: typeID, node: node}
}

// heldBounds is the bounds m's constraints hold, without the members a false
// or null value leaves unset.
func heldBounds(m map[string]any) map[string]any {
	held := map[string]any{}
	bounds, _ := m["constraints"].(map[string]any)
	for key, v := range bounds {
		if v != nil && v != false {
			held[key] = v
		}
	}
	return held
}

// kindOf is the kind of the node a placement names, "primitive" where the
// type is one no use owns.
func (p placement) kindOf() string {
	if p.node == nil {
		return "primitive"
	}
	kind, _ := p.node["kind"].(string)
	return kind
}

// requireHome holds both sides of a row: the bounds on the carrier and the
// bounds on its node, so a test cannot pass with a bound on both or on neither.
func requireHome(t *testing.T, p placement, row homeRow) {
	t.Helper()
	assert.Equal(t, row.kind, p.kindOf(), "the kind of the node %s names", p.typeID)
	assert.Equal(t, row.carrier, heldBounds(p.carrier), "the bounds the carrier holds")
	assert.Equal(t, row.node, heldBounds(p.node), "the bounds the node holds")
}

// requireNoNodeAt fails t where doc holds an anonymous type minted at pointer
// or below it: a carrier over a reference owns no node.
func requireNoNodeAt(t *testing.T, doc *ir.Document, pointer string) {
	t.Helper()
	prefix := "t/anon" + pointer
	for id := range doc.Types {
		assert.False(t, id == ir.TypeID(prefix) || strings.HasPrefix(string(id), prefix+"/"),
			"%s mints no node for a carrier over a reference", id)
	}
}

// warningsOf is the findings a row draws at version: OpenAPI 3.0 ignores a
// keyword beside a $ref, and the compiler warns that it reads it as 3.1 does.
func warningsOf(version string, pos position, rows []homeRow) []wantFinding {
	if version != "3.0.3" {
		return nil
	}
	var want []wantFinding
	for i, r := range rows {
		if r.sibling != "" {
			want = append(want, wantFinding{
				code: diag.LaterVersionConstruct, pointer: pos.schemaAt(i, r) + "/" + r.sibling,
			})
		}
	}
	return want
}

// keepRows are schemas whose carrier owns no node, because each lowers to a
// reference or to a primitive every use shares.
var keepRows = []homeRow{
	{
		name: "ref", schema: "{$ref: '#/components/schemas/Str', minLength: 2}", kind: "scalar",
		carrier: map[string]any{"minLength": 2.0}, node: map[string]any{}, sibling: "minLength",
	},
	{
		name: "prim", schema: "{type: string, maxLength: 9}", kind: "primitive",
		carrier: map[string]any{"maxLength": 9.0}, node: map[string]any{},
	},
	{
		name: "uuid", schema: "{type: string, format: uuid, maxLength: 36}", kind: "primitive",
		carrier: map[string]any{"maxLength": 36.0}, node: map[string]any{},
	},
}

// TestCompile_ACarrierOverAReferenceOrASharedPrimitiveKeepsItsBound holds that a
// property, a parameter or a header whose schema is a reference with a keyword
// beside it, or a string every use shares, keeps its bound on itself: it owns
// no node to hold it, and no node is minted at its pointer. It holds at both
// versions and in both orders of the components.
func TestCompile_ACarrierOverAReferenceOrASharedPrimitiveKeepsItsBound(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.1.0", "3.0.3"} {
		for _, pos := range positions {
			t.Run(version+"/"+pos.name, func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, sharedSchemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "keeps", pos.spec(version, keepRows, schemas))
					for i, row := range keepRows {
						p := placementOf(t, doc, pos.carrierID(row))
						requireHome(t, p, row)
						requireNoNodeAt(t, doc, pos.schemaAt(i, row))
					}
					requireErrorsAndWarnings(t, diags, warningsOf(version, pos, keepRows))
				})
			})
		}
	}
}

// TestCompile_ABoundBesideAReferenceToAModelIsHeldOnce holds that a property
// count written beside a $ref to a model is held once, by the carrier or by a
// node standing for the use, never by both and never lost: a reader asking for
// the effective bound reads each home once.
func TestCompile_ABoundBesideAReferenceToAModelIsHeldOnce(t *testing.T) {
	t.Parallel()
	row := homeRow{name: "beside", schema: "{$ref: '#/components/schemas/Obj', minProperties: 2}", sibling: "minProperties"}
	for _, version := range []string{"3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, sharedSchemas, func(t *testing.T, schemas []string) {
				pos := positions[0]
				doc, diags := compileAnnotationSpec(t, "beside", pos.spec(version, []homeRow{row}, schemas))
				p := placementOf(t, doc, pos.carrierID(row))
				carrier, node := heldBounds(p.carrier), heldBounds(p.node)
				held := 0
				for _, side := range []map[string]any{carrier, node} {
					if v, ok := side["minProps"]; ok {
						assert.Equal(t, 2.0, v, "the bound as written")
						held++
					}
				}
				assert.Equal(t, 1, held, "one home holds the bound: carrier %v, node %v", carrier, node)
				requireErrorsAndWarnings(t, diags, warningsOf(version, pos, []homeRow{row}))
			})
		})
	}
}

// TestCompile_AMemberTheRedeclaredPropertyFoldComposesKeepsItsBounds holds that
// a property declared by a base and again by an inline entry, whose type the
// fold composes into a node of its own, keeps the folded bounds on the
// property while the composed node holds none: the fold has no one position to
// give the bound to. It holds in both orders of the components and of the
// entries, at both versions.
func TestCompile_AMemberTheRedeclaredPropertyFoldComposesKeepsItsBounds(t *testing.T) {
	t.Parallel()
	const inline = "{properties: {action_parameters: {type: object, minProperties: 1, maxProperties: 5, " +
		"properties: {x: {type: string}}}}}"
	for _, version := range []string{"3.1.0", "3.0.3"} {
		for _, swapped := range []bool{false, true} {
			entries := "{$ref: '#/components/schemas/Rule'}, " + inline
			if swapped {
				entries = inline + ", {$ref: '#/components/schemas/Rule'}"
			}
			schemas := []string{
				"Rule: {type: object, properties: {action_parameters: {type: object, description: d}}}",
				"LogRule: {allOf: [" + entries + "]}",
			}
			t.Run(fmt.Sprintf("%s/entries-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, _ := compileAnnotationSpec(t, "fold", admissionSpec(version, "", schemas...))
					model, ok := doc.Types["t/openapi/components/schemas/LogRule"].(*ir.Model)
					require.True(t, ok)
					require.Len(t, model.Properties, 1)
					prop := model.Properties[0]
					require.NotNil(t, prop.Constraints, "the property keeps the folded bounds")
					require.NotNil(t, prop.Constraints.MinProps)
					require.NotNil(t, prop.Constraints.MaxProps)
					assert.EqualValues(t, 1, *prop.Constraints.MinProps)
					assert.EqualValues(t, 5, *prop.Constraints.MaxProps)

					composed, ok := doc.Types[prop.Type.Target].(*ir.Model)
					require.True(t, ok, "the property names a model")
					assert.True(t, strings.HasPrefix(string(composed.ID), "t/composed/"),
						"%s is the composed node", composed.ID)
					assert.Nil(t, composed.Constraints, "the composed node holds none of them")
				})
			})
		}
	}
}

// TestCompile_ABoundOfAnotherKindIsKeptOnItsHomeAndBreaksNoInvariant holds that
// a string bound beside a schema that lowers to a model, written alone or
// beside a conjunct no object can satisfy, lands in no Constraints and is kept
// verbatim, and that the compiled document draws no bound-of-another-kind
// violation.
func TestCompile_ABoundOfAnotherKindIsKeptOnItsHomeAndBreaksNoInvariant(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Base: {type: object, properties: {id: {type: string}}}",
		"Untyped: {properties: {id: {type: string}}}",
		"O1: {properties: {a: {type: string}}, minLength: 3}",
		"Contradicted: {allOf: [{$ref: '#/components/schemas/Untyped'}, {type: string, maxLength: 3}]}",
	}
	for _, version := range []string{"3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, _ := compileAnnotationSpec(t, "kind", admissionSpec(version, "", schemas...))
				assert.Empty(t, boundHomes(t, doc, "minLength"), "no node or carrier holds the length")
				assert.Empty(t, boundHomes(t, doc, "maxLength"), "no node or carrier holds the other length")
				assert.Equal(t, []string{"/components/schemas/O1/minLength"}, keptEntries(t, doc, "openapi:minLength"))
				for _, v := range irverify.Verify(doc) {
					assert.NotEqual(t, "ir/bound-of-another-kind", v.Code, "violation at %s", v.Path)
				}
			})
		})
	}
}
