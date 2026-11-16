// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// compileKindInference compiles spec with the kind-inference policy on, or off
// where off is set, and requires the document and its findings to verify.
func compileKindInference(t *testing.T, name, spec string, off bool) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	opts := compilers.Options{FormatOptions: openapi.Options{KindInference: openapi.KindInference{Disabled: off}}}
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: []byte(spec)}}, opts)
	require.NoError(t, err)
	requireVerifies(t, doc, diags)
	return doc, diags
}

// holderOfInlineShapes holds one inline schema per position that owns a node
// of its own, each writing `properties` and no type: a property, a list's
// element and a union's variant.
const holderOfInlineShapes = "Holder: {type: object, properties: {" +
	"p: {properties: {a: {type: string}}}, " +
	"l: {type: array, items: {properties: {a: {type: string}}}}, " +
	"u: {oneOf: [{properties: {a: {type: string}}}, {type: integer}]}}}"

// inlineBody is a request body whose schema writes `properties` and no type.
const inlineBody = `paths:
  /x:
    post:
      operationId: post
      requestBody:
        content:
          application/json:
            schema: {properties: {a: {type: string}}}
      responses: {"204": {description: ok}}
`

// TestKindInference_AnUntypedPropertiesSchemaIsMarked holds that a schema
// writing `properties` and no type lowers to a model whose provenance names the
// policy, that the marker stays on the node and not on a reference to it, and
// that nothing is kept or reported: the reading is the policy's, not a loss.
func TestKindInference_AnUntypedPropertiesSchemaIsMarked(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Members: {properties: {foo: {type: string}}, required: [foo]}",
		"Holder: {type: object, properties: {m: {$ref: '#/components/schemas/Members'}}}",
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileKindInference(t, "members", admissionSpec(version, "", schemas...), false)
				requireErrorsAndWarnings(t, diags, nil)
				assert.Empty(t, degradedInfoPointers(diags), "the inferred kind keeps nothing")

				members, ok := kindNode(t, doc, componentID("Members")).(*ir.Model)
				require.True(t, ok, "properties alone speak of an object")
				assert.Equal(t, kindInferenceMarker, members.Provenance.Inferred)
				assert.Empty(t, members.Unmodeled)
				require.Len(t, members.Properties, 1)
				assert.True(t, members.Properties[0].Required)
				assert.Equal(t, prim(ir.PrimString), members.Properties[0].Type.Target)

				holder := kindNode(t, doc, componentID("Holder"))
				assert.False(t, markedKindInference(holder), "a declared object is not marked")
				m := holderProps(t, doc)["m"]
				assert.Equal(t, componentID("Members"), m.Type.Target)
				assert.Empty(t, m.Type.Inferred, "the marker is on the node, not on each use of it")
			})
		})
	}
}

// TestKindInference_ANullTypeStillInfersTheKind holds that a `type` written
// null names no kind, so the shape keywords beside it decide the node and
// mark it: the schema says no more than one with no `type`.
func TestKindInference_ANullTypeStillInfersTheKind(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"TypeNullProps: {type: null, properties: {a: {type: string}}}",
		"Plain: {type: string}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileKindInference(t, "typenull", admissionSpec("3.1.0", "", schemas...), false)
		requireErrorsAndWarnings(t, diags, []wantFinding{
			{code: typeMismatch, pointer: "/components/schemas/TypeNullProps/type"},
		})
		td := kindNode(t, doc, componentID("TypeNullProps"))
		assert.Equal(t, ir.KindModel, td.Kind())
		assert.True(t, markedKindInference(td))
		assert.Empty(t, keptOn(td))
	})
}

// TestKindInference_ANullableWithNoTypeStillInfersTheKind holds that a 3.0
// `nullable: true` beside `properties` and no type claims no kind: the node
// is the model the shape names, marked by the kind policy, beside the finding
// the nullable keyword draws; the same keyword beside a declared object leaves
// the node unmarked.
func TestKindInference_ANullableWithNoTypeStillInfersTheKind(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"NullableProps: {nullable: true, properties: {a: {type: string}}}",
		"NullableTyped: {type: object, nullable: true, properties: {a: {type: string}}}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileKindInference(t, "nullable", admissionSpec("3.0.3", "", schemas...), false)
		requireErrorsAndWarnings(t, diags, []wantFinding{
			{code: diag.Validation + "/validation-no-effect", pointer: "/components/schemas/NullableProps/nullable"},
		})
		inferred := kindNode(t, doc, componentID("NullableProps"))
		assert.Equal(t, ir.KindModel, inferred.Kind())
		assert.True(t, markedKindInference(inferred), "the marker list holds the kind policy: %q",
			inferred.Common().Provenance.Inferred)
		assert.False(t, markedKindInference(kindNode(t, doc, componentID("NullableTyped"))))
	})
}

// TestKindInference_AnInlineSchemaIsMarkedAtEachPositionOwningANode holds that
// the marker stands on the node an inline schema owns at a property, a list
// element, a union variant and a request body, as it does on a component.
func TestKindInference_AnInlineSchemaIsMarkedAtEachPositionOwningANode(t *testing.T) {
	t.Parallel()
	owned := []ir.TypeID{
		"t/anon/components/schemas/Holder/properties/p",
		"t/anon/components/schemas/Holder/properties/l/items",
		"t/anon/components/schemas/Holder/properties/u/oneOf/0",
		"t/anon/paths/~1x/post/requestBody/content/application~1json/schema",
	}
	declared := []ir.TypeID{
		"t/anon/components/schemas/Holder/properties/l",
		"t/anon/components/schemas/Holder/properties/u",
		componentID("Holder"),
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, []string{holderOfInlineShapes, "Plain: {type: string}"}, func(t *testing.T, schemas []string) {
				doc, diags := compileKindInference(t, "inline", admissionSpec(version, inlineBody, schemas...), false)
				requireErrorsAndWarnings(t, diags, nil)
				for _, id := range owned {
					td := kindNode(t, doc, id)
					assert.Equal(t, ir.KindModel, td.Kind(), id)
					assert.True(t, markedKindInference(td), "%s reads its kind off properties", id)
				}
				for _, id := range declared {
					assert.False(t, markedKindInference(kindNode(t, doc, id)), "%s declares its kind", id)
				}
			})
		})
	}
}

// untypedShapes is each untyped schema whose shape keywords the policy reads
// as a kind, with the keywords it keeps beside the top type when the policy is
// off. A key is written where the schema writes the keyword.
var untypedShapes = []struct {
	schema string
	name   string
	kept   []string
	only31 bool
}{
	{"Members: {properties: {foo: {type: string}}, required: [foo]}", "Members", []string{"properties", "required"}, false},
	{"Elements: {items: {type: string}}", "Elements", []string{"items"}, false},
	{"AddlOnly: {additionalProperties: {type: string}}", "AddlOnly", []string{"additionalProperties"}, false},
	{"ReqOnly: {required: [a]}", "ReqOnly", []string{"required"}, false},
	{"DiscOnly: {discriminator: {propertyName: k}}", "DiscOnly", []string{"discriminator"}, false},
	{"PatOnly: {patternProperties: {'^x': {type: string}}}", "PatOnly", []string{"patternProperties"}, true},
	{"PrefixOnly: {prefixItems: [{type: string}]}", "PrefixOnly", []string{"prefixItems"}, true},
	{"Both: {properties: {a: {type: string}}, items: {type: string}}", "Both", []string{"properties", "items"}, false},
}

// TestKindInference_DisabledLowersAnUntypedShapeToTheTopType holds that with
// the policy off a schema writing shape keywords and no type is the top type
// with each keyword kept verbatim and one info naming the schema, unmarked,
// while a schema that declares its type is lowered as it is with the policy on.
func TestKindInference_DisabledLowersAnUntypedShapeToTheTopType(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		var schemas []string
		var infos []jsontext.Pointer
		for _, tc := range untypedShapes {
			if tc.only31 && version == "3.0.3" {
				continue
			}
			schemas = append(schemas, tc.schema)
			infos = append(infos, jsontext.Pointer("/components/schemas/"+tc.name))
		}
		for _, tc := range typedSchemas {
			if !(tc.only31 && version == "3.0.3") {
				schemas = append(schemas, tc.schema)
			}
		}
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileKindInference(t, "off", admissionSpec(version, "", schemas...), true)
				requireErrorsAndWarnings(t, diags, nil)
				assert.ElementsMatch(t, infos, degradedInfoPointers(diags), "one info for each untyped schema, none for a typed one")

				for _, tc := range untypedShapes {
					if tc.only31 && version == "3.0.3" {
						continue
					}
					scalar, ok := kindNode(t, doc, componentID(tc.name)).(*ir.Scalar)
					require.True(t, ok, "%s is the top type with the policy off", tc.name)
					require.NotNil(t, scalar.Base, tc.name)
					assert.Equal(t, prim(ir.PrimAny), scalar.Base.Target, tc.name)
					want := map[string]ir.UnmodeledReason{}
					for _, keyword := range tc.kept {
						want["openapi:"+keyword] = ir.ReasonDegradedLowering
					}
					assert.Equal(t, want, keptOn(scalar), tc.name)
					assert.False(t, markedKindInference(scalar), "%s is read as written", tc.name)
				}

				for _, tc := range typedSchemas {
					if tc.only31 && version == "3.0.3" {
						continue
					}
					td := kindNode(t, doc, componentID(tc.name))
					assert.Equal(t, tc.kind, td.Kind(), "%s declares its kind, so the policy leaves it", tc.name)
					assert.Empty(t, keptOn(td), tc.name)
				}
			})
		})
	}
}

// TestKindInference_DisabledReportsEveryKeywordOfASchemaOnce holds that a
// schema writing keywords of two kinds draws one info with the policy off, and
// that the message names both keywords it kept.
func TestKindInference_DisabledReportsEveryKeywordOfASchemaOnce(t *testing.T) {
	t.Parallel()
	inBothOrders(t, []string{untypedShapes[7].schema, "Plain: {type: string}"}, func(t *testing.T, schemas []string) {
		_, diags := compileKindInference(t, "both-off", admissionSpec("3.1.0", "", schemas...), true)
		assert.Equal(t, []jsontext.Pointer{"/components/schemas/Both"}, degradedInfoPointers(diags))
		message := diagMessageOf(t, diags, "/components/schemas/Both")
		assert.Contains(t, message, "properties")
		assert.Contains(t, message, "items")
	})
}

// diagMessageOf returns the message of the one degraded-construct finding at
// pointer.
func diagMessageOf(t *testing.T, diags []ir.Diagnostic, pointer string) string {
	t.Helper()
	var messages []string
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && string(d.Provenance.Pointer) == pointer {
			messages = append(messages, d.Message)
		}
	}
	require.Len(t, messages, 1, "one degraded-construct finding at %s", pointer)
	return messages[0]
}

// TestKindInference_TheTextOptionSwitchesThePolicy holds that the option a
// command line spells `kind-inference=false` turns the policy off as the Go
// field does, that `true` and no setting leave it on, and that the compile
// through the decoded options lowers an untyped shape as the field does.
func TestKindInference_TheTextOptionSwitchesThePolicy(t *testing.T) {
	t.Parallel()
	spec := admissionSpec("3.1.0", "", untypedShapes[0].schema, "Plain: {type: string}")
	for _, tc := range []struct {
		name     string
		settings map[string]string
		off      bool
	}{
		{"unset", nil, false},
		{"true", map[string]string{"kind-inference": "true"}, false},
		{"false", map[string]string{"kind-inference": "false"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decoded, err := openapi.New().DecodeOptions(compilers.OptionSet{Settings: tc.settings})
			require.NoError(t, err)
			opts, ok := decoded.(openapi.Options)
			require.True(t, ok, "the compiler decodes its own Options")
			assert.Equal(t, tc.off, opts.KindInference.Disabled)

			doc, _, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: "text.yaml", Data: []byte(spec)}}, compilers.Options{FormatOptions: opts})
			require.NoError(t, err)
			td := kindNode(t, doc, componentID("Members"))
			assert.Equal(t, tc.off, td.Kind() == ir.KindScalar, "off: the top type; on: a model, got %s", td.Kind())
			assert.Equal(t, !tc.off, markedKindInference(td))
		})
	}
}
