package scan

import (
	"bytes"
	"encoding/json/jsontext"
	"reflect"
	"slices"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3core "github.com/speakeasy-api/openapi/jsonschema/oas3/core"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	openapicore "github.com/speakeasy-api/openapi/openapi/core"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// TestSubSchemaKeys_AreTheModelsSchemaFields holds the three sub-schema key
// sets to the library's schema model: each set is the keys of the fields
// holding one schema, a list of them or a map of them, and every field holding
// a schema is one of those three. A key the model lacks names raw YAML, where
// the resolver resolves nothing and its walk reads a trailing empty token
// otherwise, so a set wider than the model refuses documents that compile.
func TestSubSchemaKeys_AreTheModelsSchemaFields(t *testing.T) {
	t.Parallel()
	shapes := map[reflect.Type]map[string]bool{
		reflect.TypeFor[marshaller.Node[oas3core.JSONSchema]]():                            {},
		reflect.TypeFor[marshaller.Node[[]oas3core.JSONSchema]]():                          {},
		reflect.TypeFor[marshaller.Node[*sequencedmap.Map[string, oas3core.JSONSchema]]](): {},
	}
	// A type argument is spelled with its full import path, so this is how any
	// shape holding a schema spells it.
	model := reflect.TypeFor[oas3core.Schema]()
	schema := model.PkgPath() + "." + model.Name()
	for f := range model.Fields() {
		if keys, ok := shapes[f.Type]; ok {
			keys[f.Tag.Get("key")] = true
			continue
		}
		assert.NotContains(t, f.Type.String(), schema, "%s holds schemas in a shape the scan does not read", f.Name)
	}

	assert.Equal(t, shapes[reflect.TypeFor[marshaller.Node[oas3core.JSONSchema]]()], subSchemaObjectKeys)
	assert.Equal(t, shapes[reflect.TypeFor[marshaller.Node[[]oas3core.JSONSchema]]()], subSchemaListKeys)
	assert.Equal(t, shapes[reflect.TypeFor[marshaller.Node[*sequencedmap.Map[string, oas3core.JSONSchema]]]()],
		subSchemaMapKeys)
}

// TestExampleKeys_HoldNoSchema holds exampleKeys to the library's OpenAPI
// model outside a schema: each key names a field there holding raw YAML, the
// example data the scan passes over, and no field so named can hold a schema,
// which the scan would then miss. A default fails the second, a default
// response holding schemas, and a server variable's enum the first.
func TestExampleKeys_HoldNoSchema(t *testing.T) {
	t.Parallel()
	fields := map[string][]reflect.Type{}
	walkModel(reflect.TypeFor[openapicore.OpenAPI](), func(key string, ft reflect.Type) {
		fields[key] = append(fields[key], ft)
	})

	holdsSchema := func(ft reflect.Type) bool { schema, _ := walkModel(ft, nil); return schema }
	holdsRaw := func(ft reflect.Type) bool { _, raw := walkModel(ft, nil); return raw }
	for key := range exampleKeys {
		assert.True(t, slices.ContainsFunc(fields[key], holdsRaw), "no field %s holds example data", key)
		for _, ft := range fields[key] {
			assert.False(t, holdsSchema(ft), "a field %s holds schemas: %s", key, ft)
		}
	}
	assert.True(t, slices.ContainsFunc(fields["default"], holdsSchema), "a default response holds schemas")
	assert.False(t, slices.ContainsFunc(fields["enum"], holdsRaw), "a server variable's enum is strings")
}

// walkModel walks the library's model by type from root, through its fields,
// references, maps and lists, and stops at a schema. It hands field each keyed
// field it meets, when field is not nil, and reports whether it reached a
// schema, and raw YAML.
func walkModel(root reflect.Type, field func(key string, ft reflect.Type)) (schema, raw bool) {
	const (
		marshallerPkg   = "github.com/speakeasy-api/openapi/marshaller"
		sequencedmapPkg = "github.com/speakeasy-api/openapi/sequencedmap"
		openapiCorePkg  = "github.com/speakeasy-api/openapi/openapi/core"
		valuesCorePkg   = "github.com/speakeasy-api/openapi/values/core"
	)
	schemaType, rawType := reflect.TypeFor[oas3core.Schema](), reflect.TypeFor[yaml.Node]()
	seen := map[reflect.Type]bool{}
	queue := []reflect.Type{root}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for at.Kind() == reflect.Pointer || at.Kind() == reflect.Slice || at.Kind() == reflect.Map {
			at = at.Elem()
		}
		if seen[at] || at.Kind() != reflect.Struct {
			continue
		}
		seen[at] = true
		raw = raw || at == rawType
		switch at.PkgPath() {
		case schemaType.PkgPath():
			schema = schema || at == schemaType
		case marshallerPkg: // a Node holds its Value; CoreModel holds nothing
			if v, ok := at.FieldByName("Value"); ok {
				queue = append(queue, v.Type)
			}
		case sequencedmapPkg:
			if m, ok := reflect.TypeAssert[interface{ GetValueType() reflect.Type }](reflect.New(at)); ok {
				queue = append(queue, m.GetValueType())
			}
		case openapiCorePkg, valuesCorePkg:
			for f := range at.Fields() {
				if key := f.Tag.Get("key"); key != "" && field != nil {
					field(key, f.Type)
				}
				queue = append(queue, f.Type)
			}
		default: // raw YAML, extensions, and the rest hold no model
		}
	}
	return schema, raw
}

// landingDoc holds a member of each kind a pointer's last empty token is read
// in: maps of schemas, a schema and its sub-schemas, a list of schemas, keys a
// schema has no field for, a link's parameters, a map of raw values, objects
// outside any schema, and schemas below keys the scan skips by name. Each holds
// a member keyed "" where it can, so a reading that took the key where the
// library stays, or stayed where it takes the key, lands elsewhere.
const landingDoc = `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a:
    get:
      responses:
        default:
          description: d
          content:
            application/json:
              schema: {properties: {'': {type: string}}}
webhooks:
  x-h:
    post:
      requestBody:
        content:
          application/json:
            schema: {properties: {'': {type: string}}}
x-l: {'': {k: v}}
components:
  '': {k: v}
  links:
    L: {operationId: o, '': {k: v}, parameters: {'': {k: v}, p: {k: v}}}
  schemas:
    '': {type: string}
    A:
      '': {type: string}
      properties: {'': {type: string}, p: {type: string, '': {type: string}}}
      patternProperties: {'': {type: string}}
      dependentSchemas: {'': {type: string}}
      $defs: {'': {type: string}}
      items: {'': {type: string}}
      allOf: [{'': {type: string}}]
      definitions: {'': {type: string}}
      additionalItems: {'': {type: string}}
      u: {'': {type: string}, v: {'': {type: string}}}
      x-u: {'': {type: string}}
      example: {'': {type: string}}
      discriminator: {propertyName: k, mapping: {'': x}}
`

// TestLanding_IsWhereTheLibraryLands holds landing to the call the resolver
// makes, jsonpointer.GetTarget on the parsed model: where the library lands a
// pointer the scan lands it too, and where the library fails it the scan lands
// nowhere. Rows marked over are positions holding no schema whose kind the scan
// does not read, where a landing only adds a loop through a reference the
// resolver reports anyway. The row marked raw pins the one gap: outside a
// schema the scan reads the key "" even in raw YAML (landsOnKey).
func TestLanding_IsWhereTheLibraryLands(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), bytes.NewReader([]byte(landingDoc)))
	require.NoError(t, err)
	var tree yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(landingDoc), &tree))
	root := nodeview.DocumentRoot(&tree)

	tests := []struct {
		pointer   jsontext.Pointer
		over, raw bool
	}{
		{pointer: "/components/schemas/"},
		{pointer: "/components/schemas/A"},
		{pointer: "/components/schemas/A/"},
		{pointer: "/components/schemas/A/properties/"},
		{pointer: "/components/schemas/A/properties/p/"},
		{pointer: "/components/schemas/A/properties//type"},
		{pointer: "/components/schemas/A/patternProperties/"},
		{pointer: "/components/schemas/A/dependentSchemas/"},
		{pointer: "/components/schemas/A/$defs/"},
		{pointer: "/components/schemas/A/items/"},
		{pointer: "/components/schemas/A/allOf/"},
		{pointer: "/components/schemas/A/allOf/0/"},
		{pointer: "/components/schemas/A/definitions/"},
		{pointer: "/components/schemas/A/additionalItems/"},
		{pointer: "/components/schemas/A/u/"},
		{pointer: "/components/schemas/A/u/v/"},
		{pointer: "/components/schemas/A/u/missing/"},
		{pointer: "/components/schemas/A/x-u/"},
		{pointer: "/components/schemas/A/example/"},
		{pointer: "/components/schemas/A/discriminator/", over: true},
		{pointer: "/components/schemas/A/discriminator/mapping/", over: true},
		{pointer: "/components/links/L/parameters/"},
		{pointer: "/components/links/L/parameters/p"},
		{pointer: "/components/links/L/", over: true},
		{pointer: "/components/", over: true},
		{pointer: "/paths/~1a/get/responses/default/content/application~1json/schema/properties/"},
		{pointer: "/webhooks/x-h/post/requestBody/content/application~1json/schema/properties/"},
		{pointer: "/x-l/", raw: true},
	}
	for _, tc := range tests {
		t.Run(string(tc.pointer), func(t *testing.T) {
			t.Parallel()
			want, landed := libraryLanding(t, doc, tc.pointer)
			got := landing(nodeview.New(), root, tc.pointer)
			switch {
			case tc.raw:
				require.True(t, landed, "the library stays on the node reached")
				rfc, _ := nodeview.New().PointerTarget(root, tc.pointer)
				require.NotNil(t, rfc)
				assert.Same(t, rfc, got, `the scan lands on the member keyed ""`)
				assert.NotEqual(t, [2]int{want.Line, want.Column}, [2]int{got.Line, got.Column})
			case landed:
				require.NotNil(t, got, "the library lands the pointer at %d:%d", want.Line, want.Column)
				assert.Equal(t, [2]int{want.Line, want.Column}, [2]int{got.Line, got.Column})
			case tc.over:
				assert.NotNil(t, got, "the library fails the pointer, and the scan's landing is the over-reading")
			default:
				assert.Nil(t, got, "the library fails the pointer")
			}
		})
	}
}

// libraryLanding returns the node the library lands pointer on, read from doc
// as the resolver reads a $ref's pointer, and false where it fails it or lands
// on a string, which the resolver fails to read as a schema.
func libraryLanding(t *testing.T, doc *soa.OpenAPI, pointer jsontext.Pointer) (*yaml.Node, bool) {
	t.Helper()
	target, err := jsonpointer.GetTarget(doc, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
	if err != nil {
		return nil, false
	}
	switch v := target.(type) {
	case *yaml.Node:
		return v, true
	case marshaller.RootNodeAccessor:
		return v.GetRootNode(), true
	case string:
		return nil, false // a scalar field's value, which no schema is built from
	default:
		require.Failf(t, "the library landed on what the test cannot place", "%T at %s", target, pointer)
		return nil, false
	}
}
