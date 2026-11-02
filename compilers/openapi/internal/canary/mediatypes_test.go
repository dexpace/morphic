// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/openapi/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mediaTypeReference is the reference a media type would be, had the library
// named one: its generic reference over the Media Type Object.
type mediaTypeReference = soa.Reference[soa.MediaType, *soa.MediaType, *core.MediaType]

// mediaTypeReferences pins media-type-references: the library models neither
// components/mediaTypes nor a content entry written as a $ref, and its walk
// panics on a reference over a media type; that reference, built from such an
// entry, resolves through raw YAML, but at a position the model holds its own
// media type only from the resolver's cache, which it asks first; and the
// library registers its own kinds' core models with its marshaller.
func mediaTypeReferences(t *testing.T) {
	src := `openapi: 3.2.0
info: {title: t, version: '1'}
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/M'}
            text/plain: {$ref: '#/paths/~1a/get/responses/200/content/application~1xml'}
            application/xml: {schema: {type: string}}
components:
  mediaTypes:
    M: {schema: {type: string}}
`
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
	require.NoError(t, err)
	_, modelled := reflect.TypeFor[soa.Components]().FieldByName("MediaTypes")
	require.False(t, modelled, "the model names components/mediaTypes: %s", revisit(t, result{}))
	content := doc.Paths.GetOrZero("/a").GetObject().Get().GetResponses().GetOrZero("200").GetObject().GetContent()
	for item := range soa.Walk(t.Context(), doc) {
		_ = item.Match(soa.Matcher{Any: func(model any) error {
			_, isReference := model.(*mediaTypeReference)
			assert.False(t, isReference, "the walk yields a media type reference: %s", revisit(t, result{}))
			return nil
		}})
	}
	assert.Panics(t, func() {
		for range soa.Walk(t.Context(), &mediaTypeReference{}) {
			continue
		}
	}, revisit(t, result{}))

	build := func(mt string) *mediaTypeReference {
		var ref mediaTypeReference
		_, err := marshaller.UnmarshalNode(t.Context(), "", content.GetOrZero(mt).GetRootNode(), &ref)
		require.NoError(t, err, revisit(t, result{}))
		require.True(t, ref.IsReference(), revisit(t, result{}))
		return &ref
	}
	opts := soa.ResolveOptions{RootDocument: doc, TargetLocation: location, DisableExternalRefs: true}
	viaRaw := build("application/json")
	_, err = viaRaw.Resolve(t.Context(), opts)
	require.NoError(t, err, "through raw YAML: %s", revisit(t, result{}))
	assert.NotNil(t, viaRaw.GetObject().GetSchema(), revisit(t, result{}))

	_, err = build("text/plain").Resolve(t.Context(), opts)
	require.Error(t, err, "the model's own media type: %s", revisit(t, result{}))
	held := &mediaTypeReference{Object: content.GetOrZero("application/xml")}
	doc.InitCache()
	doc.StoreReferencedObjectInCache(location+"#/paths/~1a/get/responses/200/content/application~1xml", held)
	viaCache := build("text/plain")
	_, err = viaCache.Resolve(t.Context(), opts)
	require.NoError(t, err, "from the cache: %s", revisit(t, result{}))
	assert.Same(t, held.Object, viaCache.GetObject(), revisit(t, result{}))

	assert.True(t, marshaller.IsRegistered(reflect.TypeFor[core.Reference[*core.Parameter]]()), revisit(t, result{}))
}
