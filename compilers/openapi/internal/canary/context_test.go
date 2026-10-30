// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"context"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/yml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// contextUnread pins that a cancelled context stops neither the model build
// nor a walk over a model: each does all it does handed a live one.
func contextUnread(t *testing.T) {
	src := schemas("A: {type: object, properties: {p: {type: string}}}")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	t.Run("the model is built", func(t *testing.T) {
		t.Parallel()
		doc := buildModel(cancelled, t, src)

		a, ok := doc.Components.Schemas.Get("A")
		require.True(t, ok, revisit(t, result{}))
		_, ok = a.GetSchema().GetProperties().Get("p")
		assert.True(t, ok, revisit(t, result{}))
	})
	t.Run("a walk yields every item", func(t *testing.T) {
		t.Parallel()
		doc := buildModel(t.Context(), t, src)
		a, ok := doc.Components.Schemas.Get("A")
		require.True(t, ok)

		live, stopped := 0, 0
		for range soa.Walk(t.Context(), doc) {
			live++
		}
		for range soa.Walk(cancelled, doc) {
			stopped++
		}
		require.Positive(t, live)
		assert.Equal(t, live, stopped, revisit(t, result{}))

		live, stopped = 0, 0
		for range oas3.Walk(t.Context(), a) {
			live++
		}
		for range oas3.Walk(cancelled, a) {
			stopped++
		}
		require.Positive(t, live)
		assert.Equal(t, live, stopped, revisit(t, result{}))
	})
}

// buildModel builds the model of src under ctx as the compiler does: the
// YAML tree decoded first, then the model built from it in one call.
func buildModel(ctx context.Context, t *testing.T, src string) *soa.OpenAPI {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.NewDecoder(strings.NewReader(src)).Decode(&root))
	var doc soa.OpenAPI
	doc.InitCache()
	doc.GetCore().SetConfig(yml.GetConfigFromDoc([]byte(src), &root))
	_, err := marshaller.UnmarshalNode(ctx, "", &root, &doc)
	require.NoError(t, err)
	return &doc
}
