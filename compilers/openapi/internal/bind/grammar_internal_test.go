package bind

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	oas3core "github.com/speakeasy-api/openapi/jsonschema/oas3/core"
	soa "github.com/speakeasy-api/openapi/openapi"
	openapicore "github.com/speakeasy-api/openapi/openapi/core"
	"github.com/stretchr/testify/assert"
	yaml "gopkg.in/yaml.v3"
)

// modelStates names the state each object of the parser's model is read at.
var modelStates = map[reflect.Type]state{
	reflect.TypeFor[openapicore.OpenAPI]():        sDocument,
	reflect.TypeFor[openapicore.Components]():     sComponents,
	reflect.TypeFor[openapicore.Paths]():          sPaths,
	reflect.TypeFor[openapicore.PathItem]():       sPathItem,
	reflect.TypeFor[openapicore.Operation]():      sOperation,
	reflect.TypeFor[openapicore.Parameter]():      sParameter,
	reflect.TypeFor[openapicore.Header]():         sHeader,
	reflect.TypeFor[openapicore.RequestBody]():    sRequestBody,
	reflect.TypeFor[openapicore.Responses]():      sResponses,
	reflect.TypeFor[openapicore.Response]():       sResponse,
	reflect.TypeFor[openapicore.MediaType]():      sMediaType,
	reflect.TypeFor[openapicore.Encoding]():       sEncoding,
	reflect.TypeFor[openapicore.Callback]():       sCallback,
	reflect.TypeFor[openapicore.Example]():        sExample,
	reflect.TypeFor[openapicore.Link]():           sLink,
	reflect.TypeFor[openapicore.SecurityScheme](): sSecurityScheme,
	reflect.TypeFor[oas3core.Schema]():            sSchema,
}

// The containers of each kind, keyed by what they hold.
var (
	mapsOf = map[state]state{
		sSchema: sSchemaMap, sPathItem: sPathItemMap, sOperation: sOperationMap, sParameter: sParameterMap,
		sHeader: sHeaderMap, sRequestBody: sRequestBodyMap, sResponse: sResponseMap, sMediaType: sMediaTypeMap,
		sEncoding: sEncodingMap, sCallback: sCallbackMap, sExample: sExampleMap, sLink: sLinkMap,
		sSecurityScheme: sSecuritySchemeMap, sRaw: sRawEntries,
	}
	listsOf = map[state]state{sSchema: sSchemaList, sParameter: sParameterList, sEncoding: sEncodingList, sRaw: sRawEntries}
)

// TestGrammar_FieldsAreTheModels holds the grammar to the parser's model: each
// object state defines exactly the fields its object of the model does, and
// each leads where the model's field does: to a schema, a map or list of them,
// an object of a kind and its containers, raw YAML or a list or map of it. A
// field holding neither a schema nor raw YAML may be read as a value or an
// object alike. A $ref, which the binding reads itself, is compared on neither
// side.
func TestGrammar_FieldsAreTheModels(t *testing.T) {
	t.Parallel()
	for model, s := range modelStates {
		want := map[string]state{}
		for f := range model.Fields() {
			if key := f.Tag.Get("key"); key != "" && key != "extensions" && key != "$ref" {
				want[key] = modelState(t, f.Type)
			}
		}
		if e, isContainer := entries[s]; isContainer {
			for key, ws := range want {
				assert.Equal(t, class(ws), class(e), "%s.%s is read as an entry", model.Name(), key)
			}
			continue
		}
		got := maps.Clone(fields[s])
		delete(got, "$ref")
		if s == sPathItem {
			for key, ks := range got {
				if ks == sOperation {
					assert.True(t, soa.IsStandardMethod(key), "%s is no method the model reads", key)
					delete(got, key)
				}
			}
		}
		assert.ElementsMatch(t, slices.Collect(maps.Keys(want)), slices.Collect(maps.Keys(got)), "%s's fields", model.Name())
		for key, ws := range want {
			if gs, ok := got[key]; ok {
				assert.Equal(t, class(ws), class(gs), "%s.%s", model.Name(), key)
			}
		}
	}
}

// TestGrammar_ReadsEveryMethodTheModelDoes holds the path item's operations to
// the methods the model reads one under, over every method RFC 9110 and its
// extensions register, and some no one does.
func TestGrammar_ReadsEveryMethodTheModelDoes(t *testing.T) {
	t.Parallel()
	for _, m := range []string{"get", "head", "post", "put", "delete", "connect", "options", "trace", "patch",
		"query", "copy", "lock", "mkcol", "move", "propfind", "proppatch", "unlock", "search", "link", "unlink",
		"purge", "GET", "x-get"} {
		assert.Equal(t, soa.IsStandardMethod(m), fields[sPathItem][m] == sOperation, m)
	}
}

// TestGrammar_ReadsAnExtensionWhereTheModelDoes holds extensible to the
// containers of the model whose x- keys are extensions, beside the entries it
// holds in a map of its own: the ones with an extensions field and no other
// way to name an entry.
func TestGrammar_ReadsAnExtensionWhereTheModelDoes(t *testing.T) {
	t.Parallel()
	for model, s := range modelStates {
		_, holdsEntries := entries[s]
		_, hasExtensions := model.FieldByName("Extensions")
		assert.Equal(t, holdsEntries && hasExtensions, extensible[s], model.Name())
	}
}

// class is what a state means to a reading: a value and an object other than
// the model's kinds are alike, since neither holds a schema or raw YAML.
func class(s state) state {
	if s == sObject {
		return sValue
	}
	return s
}

// modelState is the state the model reads a field of type ft at.
func modelState(t *testing.T, ft reflect.Type) state {
	t.Helper()
	for ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	if s, ok := modelStates[ft]; ok {
		return s
	}
	name := ft.String()
	switch {
	case strings.HasPrefix(name, "marshaller.Node["):
		v, _ := ft.FieldByName("Value")
		return modelState(t, v.Type)
	case strings.HasPrefix(name, "core.EitherValue["):
		if strings.Contains(name, "oas3/core.Schema") {
			return sSchema
		}
		return sValue
	case strings.HasPrefix(name, "core.Reference["):
		o, _ := ft.FieldByName("Object")
		return modelState(t, o.Type)
	case strings.HasPrefix(name, "sequencedmap.Map["):
		m, ok := reflect.TypeAssert[interface{ GetValueType() reflect.Type }](reflect.New(ft))
		assert.True(t, ok, name)
		return containerOf(mapsOf, modelState(t, m.GetValueType()))
	case ft.Kind() == reflect.Slice:
		return containerOf(listsOf, modelState(t, ft.Elem()))
	case ft == reflect.TypeFor[yaml.Node]():
		return sRaw
	default:
		return sValue
	}
}

// containerOf is the container of kind of what holds, or a value for a
// container of values.
func containerOf(of map[state]state, holds state) state {
	if s, ok := of[holds]; ok {
		return s
	}
	return sValue
}
