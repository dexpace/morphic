// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
)

// twinSpec writes a $ref at every schema position the lowering reads one at:
// a property, a parameter, a header, request and response bodies, a part, a
// file body, items, prefixItems, additionalProperties, patternProperties,
// allOf, oneOf and anyOf members, a mapped variant, `not`, an alias, a chain,
// an anchor, siblings, a bound its target's kind excludes, a refinement, an
// allOf beside it, a restating branch, disjoint conjuncts, a property merged
// through an alias and a composition, a name its resource declares twice while
// another declares it a $dynamicAnchor, a missing target, a loop, and another
// document (twinOther).
const twinSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /x:
    post:
      operationId: postX
      parameters:
        - {name: q, in: query, schema: {$ref: '#/components/schemas/Name'}}
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/Pet'}
      responses:
        '201':
          description: made
          content:
            multipart/form-data:
              schema: {$ref: '#/components/schemas/Form'}
            application/octet-stream:
              schema: {$ref: '#/components/schemas/Blob'}
        '200':
          description: ok
          headers:
            X-Rate: {schema: {$ref: '#/components/schemas/Name'}}
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Pet'}
components:
  schemas:
    Name: {type: [string, 'null'], maxLength: 5}
    Pet: {type: object, required: [name], properties: {name: {type: string}}}
    Cat: {type: object, required: [kind], properties: {kind: {type: string}}}
    Dog: {type: object, required: [kind], properties: {kind: {type: string}}}
    Mid: {$ref: '#/components/schemas/Name'}
    Top: {$ref: '#/components/schemas/Mid'}
    Anchored: {$anchor: here, type: integer}
    Holder:
      type: object
      properties:
        direct: {$ref: '#/components/schemas/Name'}
        chained: {$ref: '#/components/schemas/Top'}
        anchored: {$ref: '#here'}
        external: {$ref: 'other.yaml#/components/schemas/Ext'}
        list: {type: array, items: {$ref: '#/components/schemas/Pet'}}
        tuple: {type: array, prefixItems: [{$ref: '#/components/schemas/Pet'}, {$ref: '#/components/schemas/Name'}]}
        map: {type: object, additionalProperties: {$ref: '#/components/schemas/Pet'}}
        pattern: {type: object, patternProperties: {'^p': {$ref: '#/components/schemas/Pet'}}}
        negated: {type: string, not: {$ref: '#/components/schemas/Name'}}
    Blob: {type: string, contentMediaType: application/octet-stream}
    Form: {type: object, properties: {file: {$ref: '#/components/schemas/Blob'}, meta: {$ref: '#/components/schemas/Pet'}}}
    Loop1: {$ref: '#/components/schemas/Loop2'}
    Loop2: {$ref: '#/components/schemas/Loop1'}
    Sided:
      type: object
      properties:
        narrowed: {$ref: '#/components/schemas/Name', maxLength: 3, description: short}
        missing: {$ref: '#/components/schemas/Missing'}
        looped: {$ref: '#/components/schemas/Loop1'}
        objectBound: {$ref: '#/components/schemas/Pet', maxLength: 3}
        besideAllOf: {$ref: '#/components/schemas/Pet', allOf: [{required: [name]}, {type: 'null'}]}
    Narrow:
      allOf:
        - {$ref: '#/components/schemas/Name'}
        - {maxLength: 2}
    Twice:
      $id: https://example.com/twice
      properties:
        x: {$anchor: a, type: string}
        y: {$anchor: a, type: integer}
        twin: {$ref: '#a'}
    Unrelated: {$id: 'https://example.com/unrelated', $defs: {d: {$dynamicAnchor: a, type: boolean}}}
    BesideTop: {$ref: '#/components/schemas/Name', allOf: [{maxLength: 2}, {type: 'null'}]}
    Beside:
      allOf:
        - {$ref: '#/components/schemas/Pet', allOf: [{type: object, properties: {extra: {type: string}}}]}
        - {type: object}
    Animal2:
      oneOf:
        - {$ref: '#/components/schemas/Missing'}
        - {$ref: '#/components/schemas/Cat'}
    Child2:
      allOf:
        - {$ref: '#/components/schemas/Missing'}
        - {type: object}
    Target: {type: object, properties: {t: {type: string}}}
    Restating:
      type: object
      properties: {a: {allOf: [{$ref: '#/components/schemas/Target'}], description: d}}
      oneOf: [{required: [a], properties: {a: {$ref: '#/components/schemas/Target'}}}]
    Props: {properties: {a: {type: string}}}
    PropsAlias: {$ref: '#/components/schemas/Props'}
    Merged:
      allOf:
        - {properties: {p: {$ref: '#/components/schemas/PropsAlias'}}}
        - {properties: {p: {type: string}}}
    PropsComposed: {allOf: [{$ref: '#/components/schemas/Props'}], properties: {b: {type: string}}}
    Merged2:
      allOf:
        - {properties: {p: {$ref: '#/components/schemas/PropsComposed'}}}
        - {properties: {p: {type: string}}}
    Clash:
      allOf:
        - {$ref: '#/components/schemas/Pet'}
        - {type: string}
    Child:
      allOf:
        - {$ref: '#/components/schemas/Pet'}
        - {type: object, properties: {age: {type: integer}}}
    Animal:
      oneOf:
        - {$ref: '#/components/schemas/Cat'}
        - {$ref: '#/components/schemas/Dog'}
      discriminator:
        propertyName: kind
        mapping: {cat: '#/components/schemas/Cat', dog: '#/components/schemas/Dog'}
    Maybe:
      anyOf:
        - {$ref: '#/components/schemas/Pet'}
        - {type: 'null'}
`

// twinOther is the document twinSpec reaches, writing a $ref of its own.
const twinOther = `openapi: 3.1.0
info: {title: o, version: "1"}
paths: {}
components:
  schemas:
    Ext: {type: object, properties: {id: {type: string}, next: {$ref: '#/components/schemas/Ext'}}}
`

// TestCompile_AStaticDynamicRefLowersAsItsRefTwinAtEveryPosition pins that a
// $dynamicRef naming no $dynamicAnchor is the $ref it spells (JSON Schema
// 2020-12 §8.2.3.2) to every reader: each $ref of twinSpec and twinOther
// rewritten as a $dynamicRef lowers to the same document, the keyword's own
// spelling aside, and draws the same diagnostics as a multiset.
func TestCompile_AStaticDynamicRefLowersAsItsRefTwinAtEveryPosition(t *testing.T) {
	t.Parallel()
	compile := func(spell func(string) string) (string, []string) {
		dir := t.TempDir()
		layout(t, dir, map[string]string{"main.yaml": spell(twinSpec), "other.yaml": spell(twinOther)})
		doc, diags := compileDir(t, dir, openapi.Options{})
		got := lines(diags)
		for i := range got {
			got[i] = strings.ReplaceAll(got[i], "$dynamicRef", "$ref")
		}
		slices.Sort(got)
		return strings.ReplaceAll(jsonOf(t, doc), "$dynamicRef", "$ref"), got
	}
	ref, refDiags := compile(func(s string) string { return s })
	dyn, dynDiags := compile(func(s string) string { return strings.ReplaceAll(s, "$ref:", "$dynamicRef:") })

	require.Contains(t, ref, `"t/openapi@other.yaml/components/schemas/Ext"`, "the other document is read")
	require.NotEmpty(t, refDiags, "the missing target and the loop report")
	assert.Empty(t, cmp.Diff(ref, dyn), "the document")
	assert.Empty(t, cmp.Diff(refDiags, dynDiags), "the diagnostics")
}
