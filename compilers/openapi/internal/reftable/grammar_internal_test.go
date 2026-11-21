// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

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
	yaml "go.yaml.in/yaml/v3"
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

// lags is each field the rule table defines that the parser's model names no
// member for, by the state of the object defining it.
var lags = map[state][]string{
	sComponents: {"mediaTypes"}, sMediaType: {"description"}, sResponse: {"summary"},
	sEncoding: {"encoding", "prefixEncoding", "itemEncoding"},
	sSchema:   {"$vocabulary", "dependentRequired"},
}

// TestGrammar_FieldsAreTheModels holds the grammar to the parser's model, less
// where the model lags the rule table (lags): each object state defines the
// fields its object of the model does, each leading where the model's does: to
// a schema, a map or list of them, an object of a kind and its containers, or
// raw YAML or a list or map of it. A value and any other object are alike. A
// $ref, which the binding reads itself, is compared on neither side. No field
// of lags is one the model names, so a release adding one fails here.
func TestGrammar_FieldsAreTheModels(t *testing.T) {
	t.Parallel()
	for model, stateValue := range modelStates {
		want := map[string]state{}
		for f := range model.Fields() {
			if key := f.Tag.Get("key"); key != "" && key != "extensions" && key != "$ref" {
				want[key] = modelState(t, f.Type)
			}
		}
		if state, isContainer := entries[stateValue]; isContainer {
			for key, ws := range want {
				assert.Equal(t, class(ws), class(state), "%s.%s is read as an entry", model.Name(), key)
			}
			continue
		}
		got := maps.Clone(fields[stateValue])
		delete(got, "$ref")
		for _, lag := range lags[stateValue] {
			assert.NotContains(t, want, lag, "%s.%s is a field the model names", model.Name(), lag)
			delete(got, lag)
		}
		if stateValue == sPathItem {
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
	for model, state := range modelStates {
		_, holdsEntries := entries[state]
		_, hasExtensions := model.FieldByName("Extensions")
		assert.Equal(t, holdsEntries && hasExtensions, extensible[state], model.Name())
	}
}

// class is what a state means to a reading: a value and an object other than
// the model's kinds are alike, since neither holds a schema or raw YAML.
func class(stateValue state) state {
	if stateValue == sObject || stateValue == sSecurityList {
		return sValue
	}
	return stateValue
}

// modelState is the state the model reads a field of type ft at.
func modelState(t *testing.T, ft reflect.Type) state {
	t.Helper()
	for ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	if state, ok := modelStates[ft]; ok {
		return state
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
		return modelContainer(mapsOf, modelState(t, m.GetValueType()))
	case ft.Kind() == reflect.Slice:
		return modelContainer(listsOf, modelState(t, ft.Elem()))
	case ft == reflect.TypeFor[yaml.Node]():
		return sRaw
	default:
		return sValue
	}
}

// modelContainer is the container state in gives for holds, the state of what
// the container holds, or a value for a container of values.
func modelContainer(in map[state]state, holds state) state {
	if state, ok := in[holds]; ok {
		return state
	}
	return sValue
}

// loadModels holds the fields of lags the load phase models in the parser's
// place, so the model holds what they lead to: components/mediaTypes
// (docs/parser-library.md#media-type-references).
var loadModels = map[state][]string{sComponents: {"mediaTypes"}}

// TestGrammar_PlacesWhatNoModelHoldsAsUnheld holds unheld to lags: each field
// it names is one the parser's model lacks and the grammar places, and each
// lag leading to a schema, a schema container or an object is unheld or one
// the load phase models, so no schema the grammar places beneath one is read
// as one the model holds.
func TestGrammar_PlacesWhatNoModelHoldsAsUnheld(t *testing.T) {
	t.Parallel()
	for state, names := range unheld {
		for name := range names {
			assert.Contains(t, lags[state], name, "unheld %d.%s", state, name)
			assert.Contains(t, fields[state], name, "unheld %d.%s is placed", state, name)
		}
	}
	for state, names := range lags {
		for _, name := range names {
			if !modelled(fields[state][name]) {
				continue
			}
			assert.NotEqual(t, unheld[state][name], slices.Contains(loadModels[state], name),
				"lag %d.%s is unheld or modelled by the load phase", state, name)
		}
	}
}

// TestPlace_ReadsBeneathAFieldNoModelHoldsAsRaw pins place: a schema beneath
// a field whose content no model holds, at any depth past it, is raw YAML to
// the model, and so is nothing the grammar places as another object, which
// keeps its role but is held by no model; a read starting afresh, as one past
// a reference does, reads a schema below the same tokens as the model's.
func TestPlace_ReadsBeneathAFieldNoModelHoldsAsRaw(t *testing.T) {
	t.Parallel()
	const encoding = "paths/~1a/post/requestBody/content/m~1x/encoding/f/"
	tests := []struct {
		pointer string
		role    Role
		held    bool
	}{
		{encoding + "encoding/g/headers/H/schema/items", Raw, false},
		{encoding + "prefixEncoding/0/headers/H/schema", Raw, false},
		{encoding + "itemEncoding/encoding/g/headers/H/schema", Raw, false},
		{encoding + "encoding/g/headers/H", Object, false},
		{encoding + "encoding", Object, false},
		{encoding + "headers/H/schema/properties/encoding", Schema, true},
		{encoding + "x-ext/encoding/g/headers/H/schema", Raw, false},
		{"components/schemas/A/properties/encoding/properties/encoding", Schema, true},
		{encoding + "headers/H", Object, true},
	}
	for _, tc := range tests {
		placeValue := place{state: sDocument}.along(strings.Split(tc.pointer, "/"))
		assert.Equal(t, tc.role, placeValue.role(), tc.pointer)
		assert.Equal(t, tc.held, placeValue.held(), tc.pointer)
	}
	past := place{state: sDocument}.along(strings.Split(encoding+"encoding/g/headers/H", "/"))
	fresh := place{state: past.state}.along([]string{"schema"})
	assert.Equal(t, Schema, fresh.role(), "a read past a reference to the header reads its schema afresh")
}
