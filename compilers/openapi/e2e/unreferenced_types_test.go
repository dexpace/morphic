// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestCompile_RedeclaredPropertyPrunesLosingAnonymousNode pins GitHub #2050:
// when a property redeclared across allOf entries (or across a $ref entry and
// an inline entry, or at an operation response body, or on a distributed union)
// resolves to one declaration's type, an anonymous node lowered for a
// superseded declaration — and any anonymous node only it reached — is pruned
// and the compiled document passes irverify.Verify.
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
		{
			name: "unreferenced_defs_ref_chain_is_not_reserved",
			spec: `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Unused:
      type: object
      $defs:
        A: {$ref: '#/components/schemas/Unused/$defs/B'}
        B: {type: object, properties: {x: {type: string}}}
`,
			wantGone: []ir.TypeID{
				"type/anonymous/components/schemas/Unused/$defs/A",
				"type/anonymous/components/schemas/Unused/$defs/B",
			},
		},
		{
			name: "false_allof_entry_prunes_dropped_additional_properties_while_keeping_preserved_not_target",
			spec: `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Closed:
      $defs:
        OnlyInDropped: {type: object, properties: {z: {type: string}}}
      allOf:
        - type: object
          additionalProperties:
            type: object
            properties: {a: {type: string}}
            not: {$ref: '#/components/schemas/Closed/$defs/OnlyInDropped'}
        - false
`,
			wantLive: []ir.TypeID{
				"type/anonymous/components/schemas/Closed/$defs/OnlyInDropped",
			},
			wantGone: []ir.TypeID{
				"type/anonymous/components/schemas/Closed/allOf/0/additionalProperties",
			},
		},
		{
			name: "distributed_union_prunes_superseded_outer_property_and_its_defs_ref",
			spec: `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Check:
      $defs:
        OuterItem: {type: object}
      type: object
      properties:
        transferable: {type: boolean}
        reasons:
          type: array
          items: {$ref: '#/components/schemas/Check/$defs/OuterItem'}
      oneOf:
        - type: object
          required: [transferable]
          properties:
            transferable: {type: boolean, enum: [true]}
            reasons:
              type: array
              maxItems: 0
              items: {type: object, properties: {code: {type: string}}}
        - type: object
          required: [transferable]
          properties:
            transferable: {type: boolean, enum: [false]}
            reasons:
              type: array
              minItems: 1
              items: {type: object, properties: {code: {type: string}}}
`,
			wantGone: []ir.TypeID{
				"type/anonymous/components/schemas/Check/properties/reasons",
				"type/anonymous/components/schemas/Check/$defs/OuterItem",
			},
		},
		{
			name: "losing_conflict_prunes_model_while_keeping_losing_declaration_ref_target",
			spec: `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Both:
      $defs:
        KeptInLoser: {type: object, properties: {z: {type: string}}}
      allOf:
        - type: object
          properties:
            result:
              type: object
              properties: {a: {type: string}}
              not: {$ref: '#/components/schemas/Both/$defs/KeptInLoser'}
        - type: object
          properties:
            result: {type: string}
`,
			wantLive: []ir.TypeID{
				"type/anonymous/components/schemas/Both/$defs/KeptInLoser",
			},
			wantGone: []ir.TypeID{
				"type/anonymous/components/schemas/Both/allOf/0/properties/result",
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
			assert.Empty(t, irverify.Verify(doc))
		})
	}
}

// TestCompile_PromisedLandingsResolveAfterPruning guards against over-pruning:
// after full compilation and pruning, every kept $ref landing, kept
// discriminator route, composed loser, direct $ref to a loser, and empty-named
// root component schema remains in Document.Types, is recorded on the owning
// UnmodeledEntry.Targets where kept verbatim, and passes irverify.Verify.
func TestCompile_PromisedLandingsResolveAfterPruning(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: t, version: "1"}
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                doc: {type: object, properties: {inner: {type: string}}}
            encoding:
              doc:
                contentType: multipart/form-data
                encoding:
                  inner:
                    headers:
                      X-Kept: {schema: {$ref: '#/x-defs/FromEncoding'}}
      responses:
        "200": {description: ok}
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
    ChainThroughIf:
      type: object
      $defs:
        Mid: {$ref: '#/components/schemas/ChainThroughIf/$defs/End', description: mid}
        End: {type: integer}
      if: {$ref: '#/components/schemas/ChainThroughIf/$defs/Mid'}
    AgreeingAllOfNot:
      $defs:
        Sub: {type: object, properties: {a: {type: string}}}
      allOf:
        - type: object
          not: {$ref: '#/components/schemas/AgreeingAllOfNot/$defs/Sub'}
        - type: object
          not: {$ref: '#/components/schemas/AgreeingAllOfNot/$defs/Sub'}
    KeptDiscriminator:
      type: string
      $defs:
        Mapped: {type: object, properties: {m: {type: string}}}
        Fallback: {type: object, properties: {f: {type: string}}}
      discriminator:
        propertyName: kind
        mapping:
          m: '#/components/schemas/KeptDiscriminator/$defs/Mapped'
        defaultMapping: '#/components/schemas/KeptDiscriminator/$defs/Fallback'
    WithEntryRef:
      allOf:
        - type: object
          properties: {result: {type: object, properties: {a: {type: string}}}}
        - type: object
          properties: {result: {type: object}}
    EntryRef:
      $ref: '#/components/schemas/WithEntryRef/allOf/1'
x-defs:
  FromEncoding: {type: object, properties: {e: {type: string}}}
`
	doc, _ := parseVerified(t, spec)
	assert.Empty(t, irverify.Verify(doc))

	emptyComp := doc.Types["type/openapi/components/schemas/"]
	require.NotNil(t, emptyComp)
	assert.False(t, emptyComp.Common().Anonymous, "source empty-named component schema is a declared root")

	wantKept := []ir.TypeID{
		"type/openapi/components/schemas/",
		"type/anonymous/components/schemas//properties/inner",
		"type/anonymous/components/schemas/Composed/allOf/0/properties/r",
		"type/anonymous/components/schemas/Composed/allOf/1/properties/r",
		"type/anonymous/components/schemas/WithRefToLoser/allOf/1/properties/result",
		"type/anonymous/components/schemas/WithKeptRefToLoser/allOf/1/properties/result",
		"type/anonymous/components/schemas/ChainThroughIf/$defs/Mid",
		"type/anonymous/components/schemas/ChainThroughIf/$defs/End",
		"type/anonymous/components/schemas/AgreeingAllOfNot/$defs/Sub",
		"type/anonymous/components/schemas/KeptDiscriminator/$defs/Mapped",
		"type/anonymous/components/schemas/KeptDiscriminator/$defs/Fallback",
		"type/anonymous/components/schemas/WithEntryRef/allOf/1",
		"type/anonymous/components/schemas/WithEntryRef/allOf/1/properties/result",
		"type/anonymous/x-defs/FromEncoding",
	}
	for _, id := range wantKept {
		assert.Contains(t, doc.Types, id, "promised landing or root %s must resolve after pruning", id)
	}

	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/$ref": "type/anonymous/components/schemas/WithKeptRefToLoser/allOf/1/properties/result"},
		doc.Types["type/openapi/components/schemas/KeptRef"].Common().Unmodeled["openapi:not"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/if/$ref": "type/anonymous/components/schemas/ChainThroughIf/$defs/Mid"},
		doc.Types["type/openapi/components/schemas/ChainThroughIf"].Common().Unmodeled["openapi:if-then-else"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/$ref": "type/anonymous/components/schemas/AgreeingAllOfNot/$defs/Sub"},
		doc.Types["type/openapi/components/schemas/AgreeingAllOfNot"].Common().Unmodeled["openapi:not"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{
		"/mapping/m":      "type/anonymous/components/schemas/KeptDiscriminator/$defs/Mapped",
		"/defaultMapping": "type/anonymous/components/schemas/KeptDiscriminator/$defs/Fallback",
	}, doc.Types["type/openapi/components/schemas/KeptDiscriminator"].Common().Unmodeled["openapi:discriminator"].Targets)
}

// TestCompile_ExternalKeptRefLandingResolvesAfterPruning verifies that a $ref
// inside a kept keyword pointing into an external document (and a kept keyword
// inside a response loaded from an external document) stays recorded on
// UnmodeledEntry.Targets, survives pruning, leaves unreferenced external $defs
// chains unreserved, and passes irverify.Verify.
func TestCompile_ExternalKeptRefLandingResolvesAfterPruning(t *testing.T) {
	t.Parallel()
	mainSpec := `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /ext:
    get:
      operationId: getExt
      responses:
        "200": {$ref: 'other.yaml#/Resp'}
components:
  schemas:
    Holder:
      type: object
      not: {$ref: 'other.yaml#/$defs/ExtSub'}
`
	otherSpec := `Resp:
  description: ok
  content:
    application/json:
      schema:
        type: object
        not: {$ref: '#/$defs/FromResp'}
$defs:
  ExtSub:
    type: object
    properties: {x: {type: string}}
  FromResp:
    type: object
    properties: {r: {type: string}}
  DeadA:
    $ref: '#/$defs/DeadB'
  DeadB:
    type: object
    properties: {d: {type: string}}
`
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(otherSpec), 0o600))
	doc, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "main.yaml"), Data: []byte(mainSpec)}},
		compilers.Options{
			Reader:        reader.Disk{},
			FormatOptions: openapi.Options{AllowExternalRefs: true},
		})
	require.NoError(t, err)
	require.NotNil(t, doc)
	const want ir.TypeID = "type/anonymous@other.yaml/$defs/ExtSub"
	const wantResp ir.TypeID = "type/anonymous@other.yaml/$defs/FromResp"
	assert.Contains(t, doc.Types, want)
	assert.Contains(t, doc.Types, wantResp)
	assert.NotContains(t, doc.Types, ir.TypeID("type/anonymous@other.yaml/$defs/DeadA"))
	assert.NotContains(t, doc.Types, ir.TypeID("type/anonymous@other.yaml/$defs/DeadB"))
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/$ref": want}, doc.Types["type/openapi/components/schemas/Holder"].Common().Unmodeled["openapi:not"].Targets)
	assert.Empty(t, irverify.Verify(doc))
}

// TestCompile_UnusedReusableComponentsDoNotReserveDefs verifies that
// unreferenced reusable components (parameters, headers, requestBodies,
// responses, pathItems, callbacks, mediaTypes) referencing a $defs entry do
// not reserve that $defs entry and compile cleanly under irverify.Verify,
// while referenced components and kept verbatim operation constructs still
// reserve and attach their targets.
func TestCompile_UnusedReusableComponentsDoNotReserveDefs(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: t, version: "1"}
paths:
  /used:
    get:
      operationId: getUsed
      parameters:
        - {$ref: '#/components/parameters/UsedParam'}
        - name: multi
          in: query
          content:
            application/json:
              schema: {type: string}
            text/plain:
              schema: {$ref: '#/components/schemas/Holder/$defs/FromPassedContent'}
      responses:
        "200": {description: ok}
  /empty:
    parameters:
      - name: inlineOnEmpty
        in: query
        schema: {$ref: '#/components/schemas/Holder/$defs/FromEmptyPath'}
components:
  schemas:
    Holder:
      type: object
      $defs:
        Unused: {type: object, properties: {u: {type: string}}}
        FromUsed: {type: object, properties: {l: {type: string}}}
        FromPassedContent: {type: object, properties: {c: {type: string}}}
        FromEmptyPath: {type: object, properties: {e: {type: string}}}
  parameters:
    UsedParam:
      name: used
      in: query
      schema: {$ref: '#/components/schemas/Holder/$defs/FromUsed'}
    UnusedParam:
      name: p
      in: query
      schema: {$ref: '#/components/schemas/Holder/$defs/Unused'}
  headers:
    UnusedHeader:
      schema: {$ref: '#/components/schemas/Holder/$defs/Unused'}
  requestBodies:
    UnusedBody:
      content:
        application/json:
          schema: {$ref: '#/components/schemas/Holder/$defs/Unused'}
  responses:
    UnusedResp:
      description: unused
      content:
        application/json:
          schema: {$ref: '#/components/schemas/Holder/$defs/Unused'}
  pathItems:
    UnusedPath:
      get:
        responses:
          "200":
            description: ok
            content:
              application/json:
                schema: {$ref: '#/components/schemas/Holder/$defs/Unused'}
  callbacks:
    UnusedCb:
      '{$request.body#/url}':
        post:
          requestBody:
            content:
              application/json:
                schema: {$ref: '#/components/schemas/Holder/$defs/Unused'}
          responses:
            "200": {description: ok}
  mediaTypes:
    UnusedMedia:
      schema: {$ref: '#/components/schemas/Holder/$defs/Unused'}
`
	doc, _ := parseVerified(t, spec)
	assert.Empty(t, irverify.Verify(doc))
	assert.NotContains(t, doc.Types, ir.TypeID("type/anonymous/components/schemas/Holder/$defs/Unused"))
	for _, want := range []ir.TypeID{
		"type/anonymous/components/schemas/Holder/$defs/FromUsed",
		"type/anonymous/components/schemas/Holder/$defs/FromPassedContent",
		"type/anonymous/components/schemas/Holder/$defs/FromEmptyPath",
	} {
		assert.Contains(t, doc.Types, want, "referenced/kept target %s must stay live", want)
	}
}

// TestCompile_CollapsedAllOfAndDiscriminatorTargets pins UnmodeledEntry.Targets
// attachment and reachability across collapsed agreeing allOf validation-only
// wrappers (if-then-else, contains), collapsed discriminators beside sibling
// properties, literal "mapping" payloads, discriminators moved off a model
// beside a kept union, and transitive $refs reached from a hoisted
// discriminator.
func TestCompile_CollapsedAllOfAndDiscriminatorTargets(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {meow: {type: boolean}}}
    Dog: {type: object, properties: {bark: {type: boolean}}}
    AgreeingIf:
      $defs:
        Sub: {type: object, properties: {a: {type: string}}}
      allOf:
        - type: object
          if: {$ref: '#/components/schemas/AgreeingIf/$defs/Sub'}
          then: {type: object, properties: {x: {type: string}}}
        - type: object
          if: {$ref: '#/components/schemas/AgreeingIf/$defs/Sub'}
          then: {type: object, properties: {x: {type: string}}}
    AgreeingContains:
      $defs:
        Sub: {type: object, properties: {a: {type: string}}}
      allOf:
        - type: array
          items: {type: object}
          contains: {$ref: '#/components/schemas/AgreeingContains/$defs/Sub'}
        - type: array
          items: {type: object}
          contains: {$ref: '#/components/schemas/AgreeingContains/$defs/Sub'}
    LiteralMappingNoFalseTarget:
      type: object
      $defs:
        Sub: {type: object, properties: {a: {type: string}}}
      if:
        const:
          mapping:
            a: just-a-literal
      then: {type: object}
      properties:
        p:
          type: string
          discriminator:
            propertyName: k
            mapping:
              a: '#/components/schemas/LiteralMappingNoFalseTarget/$defs/Sub'
    CollapsedDiscBesideSibling:
      allOf:
        - type: string
          discriminator:
            propertyName: k
            mapping:
              a: '#/components/schemas/Cat'
        - type: string
          discriminator:
            propertyName: k
            mapping:
              a: '#/components/schemas/Cat'
      properties:
        z:
          type: string
          discriminator:
            propertyName: k
            mapping:
              a: '#/components/schemas/Dog'
    BesideKeptUnionDisc:
      type: object
      properties:
        id: {type: string}
      $defs:
        Sub: {type: object, properties: {x: {type: string}}}
      oneOf:
        - required: [id]
        - required: [other]
      discriminator:
        propertyName: kind
        mapping:
          sub: '#/components/schemas/BesideKeptUnionDisc/$defs/Sub'
    AgreeingNotWithNestedIDResource:
      $id: https://example.com/outer
      $defs:
        X: {type: string}
      allOf:
        - type: object
          not: {$ref: '#/$defs/X'}
        - type: object
          not: {$ref: '#/$defs/X'}
      additionalProperties:
        $id: https://example.com/inner
        type: object
        $defs:
          X: {type: integer}
        not: {$ref: '#/$defs/X'}
    LiveInlineLoser:
      allOf:
        - type: object
          properties:
            result: {type: object, properties: {a: {type: string}}}
        - type: object
          properties:
            result: {type: string}
    UserOfLiveLoser:
      type: object
      properties:
        s: {$ref: '#/components/schemas/LiveInlineLoser/allOf/0/properties/result'}
    TransitiveHoistedDisc:
      type: object
      not: {$ref: '#/components/schemas/TransitiveHoistedDisc/$defs/Mid'}
      $defs:
        Mid:
          type: string
          discriminator:
            propertyName: k
            mapping:
              m: '#/components/schemas/TransitiveHoistedDisc/$defs/Mapped'
        Mapped:
          type: object
          not: {$ref: '#/components/schemas/TransitiveHoistedDisc/$defs/Deep'}
        Deep:
          type: object
          properties: {z: {type: string}}
`
	doc, diags := parseVerified(t, spec)
	assert.Empty(t, irverify.Verify(doc))

	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/if/$ref": "type/anonymous/components/schemas/AgreeingIf/$defs/Sub"},
		doc.Types["type/openapi/components/schemas/AgreeingIf"].Common().Unmodeled["openapi:if-then-else"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/contains/$ref": "type/anonymous/components/schemas/AgreeingContains/$defs/Sub"},
		doc.Types["type/openapi/components/schemas/AgreeingContains"].Common().Unmodeled["openapi:contains"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/$ref": "type/anonymous/components/schemas/AgreeingNotWithNestedIDResource/$defs/X"},
		doc.Types["type/openapi/components/schemas/AgreeingNotWithNestedIDResource"].Common().Unmodeled["openapi:not"].Targets)
	const liveLoserID ir.TypeID = "type/anonymous/components/schemas/LiveInlineLoser/allOf/0/properties/result"
	assert.Contains(t, doc.Types, liveLoserID)
	var sawLiveLoserDiag bool
	for _, d := range diags {
		if d.Code == "openapi/conflicting-redeclaration" && d.Provenance.Pointer == "/components/schemas/LiveInlineLoser/allOf/0/properties/result" {
			assert.Contains(t, d.Message, string(liveLoserID), "live referenced losing inline node keeps its TypeID in the warning")
			sawLiveLoserDiag = true
		}
	}
	assert.True(t, sawLiveLoserDiag, "expected conflicting-redeclaration warning for LiveInlineLoser")
	assert.Empty(t, doc.Types["type/openapi/components/schemas/LiteralMappingNoFalseTarget"].Common().Unmodeled["openapi:if-then-else"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/mapping/a": "type/openapi/components/schemas/Cat"},
		doc.Types["type/openapi/components/schemas/CollapsedDiscBesideSibling"].Common().Unmodeled["openapi:discriminator"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/mapping/sub": "type/anonymous/components/schemas/BesideKeptUnionDisc/$defs/Sub"},
		doc.Types["type/openapi/components/schemas/BesideKeptUnionDisc"].Common().Unmodeled["openapi:discriminator"].Targets)
	assert.Contains(t, doc.Types, ir.TypeID("type/anonymous/components/schemas/TransitiveHoistedDisc/$defs/Deep"))
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/$ref": "type/anonymous/components/schemas/TransitiveHoistedDisc/$defs/Deep"},
		doc.Types["type/anonymous/components/schemas/TransitiveHoistedDisc/$defs/Mapped"].Common().Unmodeled["openapi:not"].Targets)
}

// TestCompile_UnpackedObjectVariantAndDroppedFormHeadersPrune verifies that
// type-array unions unpacked to their object variant in allOf or distributed
// unions and lowered headers dropped when a form part's encoding is kept whole
// are cleanly pruned and pass irverify.Verify.
func TestCompile_UnpackedObjectVariantAndDroppedFormHeadersPrune(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /up:
    post:
      operationId: up
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: string
              properties:
                partA: {type: string}
            encoding:
              partA:
                headers:
                  X-Inline:
                    schema: {type: object, properties: {x: {type: string}}}
                  X-Ref:
                    $ref: '#/components/headers/H1'
      responses:
        "200": {description: ok}
components:
  headers:
    H1:
      schema:
        type: string
        not: {$ref: '#/components/schemas/Holder/$defs/Deep'}
  schemas:
    Holder:
      type: object
      $defs:
        Deep: {type: object, properties: {z: {type: string}}}
    AllOfUnpack:
      $defs:
        ObjOrStr:
          type: [object, string]
          properties: {a: {type: string}}
      allOf:
        - {$ref: '#/components/schemas/AllOfUnpack/$defs/ObjOrStr'}
        - type: object
          properties: {b: {type: string}}
    UnionUnpack:
      type: object
      properties: {common: {type: string}}
      $defs:
        ObjOrStr:
          type: [object, string]
          properties: {a: {type: string}}
      oneOf:
        - {$ref: '#/components/schemas/UnionUnpack/$defs/ObjOrStr'}
        - type: object
          properties: {b: {type: string}}
`
	doc, _ := parseVerified(t, spec)
	assert.Empty(t, irverify.Verify(doc))
	assert.Contains(t, doc.Types, ir.TypeID("type/anonymous/components/schemas/AllOfUnpack/$defs/ObjOrStr/type/0"))
	assert.Contains(t, doc.Types, ir.TypeID("type/anonymous/components/schemas/UnionUnpack/$defs/ObjOrStr/type/0"))
	for _, gone := range []ir.TypeID{
		"type/anonymous/components/schemas/AllOfUnpack/$defs/ObjOrStr",
		"type/anonymous/components/schemas/UnionUnpack/$defs/ObjOrStr",
		"type/anonymous/paths/~1up/post/requestBody/content/multipart~1form-data/encoding/partA/headers/X-Inline/schema",
		"type/anonymous/components/schemas/Holder/$defs/Deep",
	} {
		assert.NotContains(t, doc.Types, gone, "dropped anonymous type %s must be pruned", gone)
	}
}

// TestCompile_UnpreservableConstructsStayVerified pins that unpreservable YAML
// payloads (.inf) beside $refs or kept discriminators report
// openapi/unpreservable-construct without leaving unreferenced anonymous types
// behind, and that a surviving sibling itemEncoding beside an unpreservable
// prefixEncoding still reserves and attaches its $ref targets.
func TestCompile_UnpreservableConstructsStayVerified(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: t, version: "1"}
paths:
  /seq-prefix-bad:
    post:
      operationId: seqPrefixBad
      requestBody:
        content:
          multipart/mixed:
            itemSchema: {type: string}
            prefixEncoding:
              - contentType: text/plain
                x-bad: .inf
            itemEncoding:
              contentType: application/json
              headers:
                X-Item: {schema: {$ref: '#/components/schemas/Holder/$defs/KeptItem'}}
      responses:
        "200": {description: ok}
  /seq-item-bad:
    post:
      operationId: seqItemBad
      requestBody:
        content:
          multipart/mixed:
            itemSchema: {type: string}
            prefixEncoding:
              - contentType: text/plain
            itemEncoding:
              contentType: application/json
              x-bad: .inf
              headers:
                X-Item: {schema: {$ref: '#/components/schemas/Holder/$defs/DroppedItem'}}
      responses:
        "200": {description: ok}
  /empty-bad:
    parameters:
      - name: p
        in: query
        x-bad: .inf
        schema: {$ref: '#/components/schemas/Holder/$defs/DroppedParam'}
components:
  schemas:
    Holder:
      type: object
      $defs:
        KeptItem: {type: object, properties: {k: {type: string}}}
        DroppedItem: {type: object, properties: {d: {type: string}}}
        DroppedParam: {type: object, properties: {p: {type: string}}}
        DroppedNot: {type: object, properties: {n: {type: string}}}
        KeptNot: {type: object, properties: {live: {type: string}}}
        DroppedDisc: {type: object, properties: {m: {type: string}}}
    UnpreservableNot:
      type: object
      not:
        $ref: '#/components/schemas/Holder/$defs/DroppedNot'
        x-bad: .inf
    PreservableNot:
      type: object
      not:
        $ref: '#/components/schemas/Holder/$defs/KeptNot'
    UnpreservableDisc:
      type: string
      discriminator:
        propertyName: kind
        x-bad: .inf
        mapping:
          m: '#/components/schemas/Holder/$defs/DroppedDisc'
`
	doc, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(spec)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Empty(t, irverify.Verify(doc))

	assert.Contains(t, doc.Types, ir.TypeID("type/anonymous/components/schemas/Holder/$defs/KeptItem"))
	assert.Contains(t, doc.Types, ir.TypeID("type/anonymous/components/schemas/Holder/$defs/KeptNot"))
	for _, gone := range []ir.TypeID{
		"type/anonymous/components/schemas/Holder/$defs/DroppedItem",
		"type/anonymous/components/schemas/Holder/$defs/DroppedParam",
		"type/anonymous/components/schemas/Holder/$defs/DroppedNot",
		"type/anonymous/components/schemas/Holder/$defs/DroppedDisc",
	} {
		assert.NotContains(t, doc.Types, gone, "unpreservable target %s must not leak", gone)
	}
	var seqPrefixBad *ir.Operation
	for _, grp := range doc.Services[0].Groups {
		for i := range grp.Operations {
			if grp.Operations[i].Name.Source == "seqPrefixBad" {
				seqPrefixBad = &grp.Operations[i]
			}
		}
	}
	require.NotNil(t, seqPrefixBad)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/headers/X-Item/schema/$ref": "type/anonymous/components/schemas/Holder/$defs/KeptItem"},
		seqPrefixBad.Request.Contents[0].Unmodeled["openapi:itemEncoding"].Targets)
}
