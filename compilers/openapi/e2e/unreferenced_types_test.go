// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_RedeclaredPropertyPrunesLosingAnonymousNode pins GitHub #2050:
// when a property redeclared across allOf entries (or across a $ref entry and
// an inline entry, or at an operation response body) resolves to one
// declaration's type, an anonymous node lowered for a superseded declaration —
// and any anonymous node only it reached — does not remain unreferenced in
// Document.Types.
func TestCompile_RedeclaredPropertyPrunesLosingAnonymousNode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		spec     string
		wantLive []ir.TypeID
		wantGone []ir.TypeID
	}{
		{
			name: "narrow_before_bare_object",
			spec: `openapi: 3.0.3
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Both:
      allOf:
        - type: object
          properties: {result: {type: object, properties: {a: {type: string}}}}
        - type: object
          properties: {result: {type: object}}
`,
			wantLive: []ir.TypeID{"type/anonymous/components/schemas/Both/allOf/0/properties/result"},
			wantGone: []ir.TypeID{"type/anonymous/components/schemas/Both/allOf/1/properties/result"},
		},
		{
			name: "bare_object_before_narrow",
			spec: `openapi: 3.0.3
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Both:
      allOf:
        - type: object
          properties: {result: {type: object}}
        - type: object
          properties: {result: {type: object, properties: {a: {type: string}}}}
`,
			wantLive: []ir.TypeID{"type/anonymous/components/schemas/Both/allOf/1/properties/result"},
			wantGone: []ir.TypeID{"type/anonymous/components/schemas/Both/allOf/0/properties/result"},
		},
		{
			name: "ref_entry_wins_over_inline_bare_object",
			spec: `openapi: 3.0.3
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Base:
      type: object
      properties: {result: {type: object, properties: {a: {type: string}}}}
    Derived:
      allOf:
        - {$ref: '#/components/schemas/Base'}
        - type: object
          properties: {result: {type: object}}
`,
			wantLive: []ir.TypeID{"type/anonymous/components/schemas/Base/properties/result"},
			wantGone: []ir.TypeID{"type/anonymous/components/schemas/Derived/allOf/1/properties/result"},
		},
		{
			name: "nested_children_of_losing_node_are_pruned",
			spec: `openapi: 3.0.3
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Both:
      allOf:
        - type: object
          properties:
            result:
              type: array
              items:
                type: object
                properties: {a: {type: string}}
        - type: object
          properties:
            result: {type: string}
`,
			wantGone: []ir.TypeID{
				"type/anonymous/components/schemas/Both/allOf/0/properties/result",
				"type/anonymous/components/schemas/Both/allOf/0/properties/result/items",
			},
		},
		{
			name: "operation_response_position",
			spec: `openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "4XX":
          description: err
          content:
            application/json:
              schema:
                allOf:
                  - type: object
                    properties: {result: {type: object}}
                  - type: object
                    properties: {result: {type: object, properties: {code: {type: string}}}}
`,
			wantLive: []ir.TypeID{
				"type/anonymous/paths/~1items/get/responses/4XX/content/application~1json/schema/allOf/1/properties/result",
			},
			wantGone: []ir.TypeID{
				"type/anonymous/paths/~1items/get/responses/4XX/content/application~1json/schema/allOf/0/properties/result",
			},
		},
		{
			name: "distributed_union_prunes_superseded_outer_property_and_items",
			spec: `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Check:
      type: object
      properties:
        transferable: {type: boolean}
        reasons:
          description: reasons
          type: array
          items:
            type: object
            properties: {code: {type: string}}
      oneOf:
        - type: object
          required: [transferable]
          properties:
            transferable: {type: boolean, enum: [true]}
            reasons:
              type: array
              maxItems: 0
              items:
                type: object
                properties: {code: {type: string}}
        - type: object
          required: [transferable]
          properties:
            transferable: {type: boolean, enum: [false]}
            reasons:
              type: array
              minItems: 1
              items:
                type: object
                properties: {code: {type: string}}
`,
			wantLive: []ir.TypeID{
				"type/composed/components/schemas/Check/oneOf/0",
				"type/composed/components/schemas/Check/oneOf/1",
			},
			wantGone: []ir.TypeID{
				"type/anonymous/components/schemas/Check/properties/reasons",
				"type/anonymous/components/schemas/Check/properties/reasons/items",
			},
		},
		{
			name: "false_allof_entry_prunes_dropped_additional_properties",
			spec: `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Closed:
      allOf:
        - type: object
          additionalProperties: {type: object, properties: {a: {type: string}}}
        - false
`,
			wantGone: []ir.TypeID{
				"type/anonymous/components/schemas/Closed/allOf/0/additionalProperties",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := parseVerified(t, tc.spec)
			for _, id := range tc.wantLive {
				assert.Contains(t, doc.Types, id, "winning node %s must stay", id)
			}
			for _, id := range tc.wantGone {
				assert.NotContains(t, doc.Types, id, "superseded node %s must be pruned", id)
			}
			assert.Empty(t, unreferencedAnonymous(doc), "no anonymous type is left unreferenced")
		})
	}
}

// TestCompile_ReferencedLosersAndRootsAreKept pins that pruning removes no node
// still referenced by a composed field, a $ref (lowered or inside a kept
// keyword), a $ref to an allOf entry, or an empty-named component schema.
func TestCompile_ReferencedLosersAndRootsAreKept(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    "":
      type: object
      properties:
        inner: {type: object, properties: {k: {type: string}}}
    Composed:
      allOf:
        - type: object
          properties: {r: {type: object, properties: {x: {type: string}}}}
        - type: object
          properties: {r: {type: object, properties: {x: {type: string}, y: {type: integer}}}}
    WithRefToLoser:
      allOf:
        - type: object
          properties: {result: {type: object, properties: {a: {type: string}}}}
        - type: object
          properties: {result: {type: object}}
    PointsAtLoser:
      $ref: '#/components/schemas/WithRefToLoser/allOf/1/properties/result'
    WithKeptRefToLoser:
      allOf:
        - type: object
          properties: {result: {type: object, properties: {a: {type: string}}}}
        - type: object
          properties: {result: {type: object}}
    KeptRef:
      type: object
      not: {$ref: '#/components/schemas/WithKeptRefToLoser/allOf/1/properties/result'}
    WithEntryRef:
      allOf:
        - type: object
          properties: {result: {type: object, properties: {a: {type: string}}}}
        - type: object
          properties: {result: {type: object}}
    EntryRef:
      $ref: '#/components/schemas/WithEntryRef/allOf/1'
`
	doc, _ := parseVerified(t, spec)
	wantKept := []ir.TypeID{
		"type/anonymous/components/schemas/",
		"type/anonymous/components/schemas//properties/inner",
		"type/anonymous/components/schemas/Composed/allOf/0/properties/r",
		"type/anonymous/components/schemas/Composed/allOf/1/properties/r",
		"type/anonymous/components/schemas/WithRefToLoser/allOf/1/properties/result",
		"type/anonymous/components/schemas/WithKeptRefToLoser/allOf/1/properties/result",
		"type/anonymous/components/schemas/WithEntryRef/allOf/1",
		"type/anonymous/components/schemas/WithEntryRef/allOf/1/properties/result",
	}
	for _, id := range wantKept {
		assert.Contains(t, doc.Types, id, "referenced or root node %s must stay", id)
	}
}

// TestCompile_NoUnreferencedAnonymousTypes sweeps the committed corpus and
// asserts that every anonymous type in Document.Types is reachable from a
// non-anonymous type, a typed TypeID outside Document.Types, an empty-named
// root component schema, or a $ref landing inside Unmodeled.
func TestCompile_NoUnreferencedAnonymousTypes(t *testing.T) {
	t.Parallel()
	c := openapi.New()
	for _, f := range corpusSpecs(t) {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(f)
			require.NoError(t, err)
			doc, _, err := c.Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Base(f), Data: data}}, compilers.Options{})
			if err != nil || doc == nil {
				t.Skip("compiler refused the input")
			}
			assert.Empty(t, unreferencedAnonymous(doc), "%s left unreferenced anonymous types", f)
		})
	}
}

var (
	e2eTypeIDType       = reflect.TypeFor[ir.TypeID]()
	e2eTypeRegistryType = reflect.TypeFor[ir.TypeRegistry]()
	e2eUnmodeledType    = reflect.TypeFor[ir.UnmodeledEntry]()
)

// unreferencedAnonymous returns every anonymous type ID in doc.Types that no
// non-anonymous type, reference outside doc.Types, empty-named root component,
// or kept Unmodeled $ref reaches.
func unreferencedAnonymous(doc *ir.Document) []ir.TypeID {
	var roots []ir.TypeID
	for id, td := range doc.Types {
		if !ir.IsNilTypeDef(td) && !td.Common().Anonymous {
			roots = append(roots, id)
		}
	}
	if _, ok := doc.Types["type/anonymous/components/schemas/"]; ok {
		roots = append(roots, "type/anonymous/components/schemas/")
	}
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
		switch v.Type() {
		case e2eTypeRegistryType:
			return false
		case e2eTypeIDType:
			if id := ir.TypeID(v.String()); id != "" {
				roots = append(roots, id)
			}
			return false
		case e2eUnmodeledType:
			roots = append(roots, keptRefTargets(v.FieldByName("Value").Bytes(), doc.Types)...)
			return false
		default:
			return true
		}
	}))
	reached := make(map[ir.TypeID]bool, len(doc.Types))
	for len(roots) > 0 {
		id := roots[len(roots)-1]
		roots = roots[:len(roots)-1]
		if reached[id] {
			continue
		}
		td, ok := doc.Types[id]
		if !ok || ir.IsNilTypeDef(td) {
			continue
		}
		reached[id] = true
		ir.WalkValues(td, "type", ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
			switch v.Type() {
			case e2eTypeIDType:
				if next := ir.TypeID(v.String()); next != "" && !reached[next] {
					roots = append(roots, next)
				}
				return false
			case e2eUnmodeledType:
				roots = append(roots, keptRefTargets(v.FieldByName("Value").Bytes(), doc.Types)...)
				return false
			default:
				return true
			}
		}))
	}
	var dead []ir.TypeID
	for id, td := range doc.Types {
		if !ir.IsNilTypeDef(td) && td.Common().Anonymous && !reached[id] {
			dead = append(dead, id)
		}
	}
	slices.Sort(dead)
	return dead
}

// keptRefTargets extracts local JSON Pointer $ref targets from a raw Unmodeled
// JSON payload and maps each to its anonymous TypeID when present in types.
func keptRefTargets(raw []byte, types ir.TypeRegistry) []ir.TypeID {
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	var out []ir.TypeID
	var walk func(x any)
	walk = func(x any) {
		switch node := x.(type) {
		case map[string]any:
			if ref, ok := node["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
				id := ir.TypeID("type/anonymous" + string(jsontext.Pointer(ref[1:])))
				if _, exists := types[id]; exists {
					out = append(out, id)
				}
			}
			for _, v := range node {
				walk(v)
			}
		case []any:
			for _, v := range node {
				walk(v)
			}
		}
	}
	walk(parsed)
	return out
}
