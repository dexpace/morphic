// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// resolved is what resolveAll reports for a document it resolves cleanly.
const resolved = "resolved"

// resolveAll decodes input and resolves every reference in it, with external
// references off: what the resolver reports, or resolved. The library's own
// walk calls each reference's Resolve in turn.
func resolveAll(ctx context.Context, input []byte) string {
	doc, _, err := soa.Unmarshal(ctx, bytes.NewReader(input))
	if err != nil {
		return "decode: " + err.Error()
	}
	opts := soa.ResolveAllOptions{OpenAPILocation: location, DisableExternalRefs: true}
	if _, err := doc.ResolveAllReferences(ctx, opts); err != nil {
		return err.Error()
	}
	return resolved
}

// resolveTargets decodes input and resolves each discriminator mapping value
// in it as a $ref to it would be: a schema built from the value as a $ref,
// resolved on its own. It reports a line per value.
func resolveTargets(ctx context.Context, input []byte) string {
	doc, _, err := soa.Unmarshal(ctx, bytes.NewReader(input))
	if err != nil {
		return "decode: " + err.Error()
	}
	var values []string
	for item := range soa.Walk(ctx, doc) {
		err := item.Match(soa.Matcher{Discriminator: func(d *oas3.Discriminator) error {
			for _, v := range d.GetMapping().All() {
				values = append(values, v)
			}
			return nil
		}})
		if err != nil {
			return "walk: " + err.Error()
		}
	}
	opts := references.ResolveOptions{TargetLocation: location, RootDocument: doc, DisableExternalRefs: true}
	lines := make([]string, 0, len(values))
	for _, v := range values {
		outcome := resolved
		if _, err := oas3.NewJSONSchemaFromReference(references.Reference(v)).Resolve(ctx, opts); err != nil {
			outcome = err.Error()
		}
		lines = append(lines, v+": "+outcome)
	}
	return strings.Join(lines, "\n")
}

// schemaCycleOverflow pins schema-cycle-overflow: a cycle of schema $refs
// overflows the stack, except for the first reference from outside it, when
// the walk meets that reference before any member. A pointer through the
// schema it is written in lands on the member it names, closing a cycle where
// that member leads back, and ending in an empty token fails as naming
// nothing.
func schemaCycleOverflow(t *testing.T) {
	ref := func(name, target string) string { return refTo("schemas", name, target) }
	overflows := map[string]string{
		"one schema":                      schemas(ref("A", "A")),
		"two schemas":                     schemas(ref("A", "B"), ref("B", "A")),
		"three schemas":                   schemas(ref("A", "B"), ref("B", "C"), ref("C", "A")),
		"an outside reference met last":   schemas(ref("A", "B"), ref("B", "A"), ref("O", "A")),
		"a second outside reference":      schemas(ref("O", "A"), ref("P", "A"), ref("A", "B"), ref("B", "A")),
		"an outside reference to another": schemas(ref("O", "A"), ref("P", "B"), ref("A", "B"), ref("B", "A")),
		"a pointer through its own schema to a member back": schemas(
			"A: {$ref: '#/components/schemas/A/properties/p', properties: {p: {$ref: '#/components/schemas/A'}}}"),
	}
	for name, doc := range overflows {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := probe(t, "resolve", doc)
			assert.Equal(t, overflowed, r.outcome, revisit(t, r))
		})
	}
	t.Run("an outside reference met first", func(t *testing.T) {
		t.Parallel()
		r := probe(t, "resolve", schemas(ref("O", "A"), ref("A", "B"), ref("B", "A")))
		require.Equal(t, returned, r.outcome, revisit(t, r))
		assert.Contains(t, r.report, "circular reference detected", revisit(t, r))
	})
	t.Run("a pointer through its own schema", func(t *testing.T) {
		t.Parallel()
		r := probe(t, "resolve", schemas("A: {$ref: '#/components/schemas/A/'}"))
		require.Equal(t, returned, r.outcome, revisit(t, r))
		assert.Contains(t, r.report, "not found", revisit(t, r))
	})
}

// defsOrder pins defs-order: whether a "#/$defs/..." reference resolves
// depends on which reference into its schema the walk met first.
func defsOrder(t *testing.T) {
	holder := "H: {$defs: {A: {type: string}}, properties: {p: {$ref: '#/$defs/A'}}}"
	outside := "R: {$ref: '#/components/schemas/H/properties/p'}"

	got := resolveAll(t.Context(), []byte(schemas(holder, outside)))
	assert.Equal(t, resolved, got, revisit(t, result{report: got}))
	got = resolveAll(t.Context(), []byte(schemas(outside, holder)))
	assert.Contains(t, got, "definition not found: #/$defs/A", revisit(t, result{report: got}))
}

// mappingUnresolved pins mapping-unresolved: neither resolution nor decoding
// reads a discriminator mapping value, and one resolved as a $ref to it
// recurses as a schema's chain does.
func mappingUnresolved(t *testing.T) {
	t.Run("no value is read", func(t *testing.T) {
		t.Parallel()
		doc := []byte(schemas("P: {discriminator: {propertyName: k, mapping: {a: '#/components/schemas/Missing'}}}"))
		_, findings, err := soa.Unmarshal(t.Context(), bytes.NewReader(doc))
		require.NoError(t, err)
		assert.Empty(t, findings, revisit(t, result{}))
		got := resolveAll(t.Context(), doc)
		assert.Equal(t, resolved, got, revisit(t, result{report: got}))
	})
	loop := "x-lib:\n  a: {$ref: '#/x-lib/b'}\n  b: {$ref: '#/x-lib/a'}\n"
	mapping := func(values string) string {
		return schemas("P: {discriminator: {propertyName: k, mapping: {"+values+"}}}") + loop
	}
	t.Run("one value into a loop", func(t *testing.T) {
		t.Parallel()
		r := probe(t, "targets", mapping("a: '#/x-lib/a'"))
		require.Equal(t, returned, r.outcome, revisit(t, r))
		assert.Contains(t, r.report, "circular reference detected", revisit(t, r))
	})
	t.Run("two values into a loop", func(t *testing.T) {
		t.Parallel()
		r := probe(t, "targets", mapping("a: '#/x-lib/a', b: '#/x-lib/b'"))
		assert.Equal(t, overflowed, r.outcome, revisit(t, r))
	})
}

// referenceSections are the components sections holding the reference objects
// a $ref can name, besides schemas.
var referenceSections = []string{
	"parameters", "responses", "requestBodies", "headers", "pathItems", "callbacks",
	"examples", "links", "securitySchemes",
}

// referenceObjectCycle pins reference-object-cycle: a cycle of $refs among
// reference objects returns an error, and so does a $ref into a reference
// object it is reached through.
func referenceObjectCycle(t *testing.T) {
	h := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n"
	cycles := map[string]string{
		"a path item":            h + "paths:\n  /a: {$ref: '#/paths/~1a'}\n",
		"a webhook":              h + "paths: {}\nwebhooks:\n  w: {$ref: '#/webhooks/w'}\n",
		"a response by position": h + "paths:\n  /a:\n    get:\n      responses:\n        '200': {$ref: '#/paths/~1a/get/responses/200'}\n",
	}
	for _, s := range referenceSections {
		for n := 1; n <= 3; n++ {
			entries := make([]string, n)
			for i := range n {
				entries[i] = refTo(s, fmt.Sprintf("C%d", i), fmt.Sprintf("C%d", (i+1)%n))
			}
			cycles[fmt.Sprintf("%d %s", n, s)] = section(s, entries...)
		}
	}
	reentries := map[string]string{
		"a path item":         h + "paths:\n  /a: {$ref: '#/paths/~1a/t'}\n",
		"a webhook":           h + "paths: {}\nwebhooks:\n  w: {$ref: '#/webhooks/w/t'}\n",
		"a sibling it writes": h + "paths:\n  /a: {$ref: '#/paths/~1a/description', description: d}\n",
		"a component":         section("responses", "A: {$ref: '#/components/responses/A/t'}"),
	}
	for name, doc := range cycles {
		t.Run("cycle through "+name, func(t *testing.T) {
			t.Parallel()
			r := probe(t, "resolve", doc)
			require.Equal(t, returned, r.outcome, revisit(t, r))
			assert.Contains(t, r.report, "circular reference detected", revisit(t, r))
		})
	}
	for name, doc := range reentries {
		t.Run("reentering "+name, func(t *testing.T) {
			t.Parallel()
			r := probe(t, "resolve", doc)
			require.Equal(t, returned, r.outcome, revisit(t, r))
			assert.Contains(t, r.report, "unresolved reference", revisit(t, r))
		})
	}
}

// rootDefs pins root-defs: a $ref to a "$defs" written at the document's
// root, which the OpenAPI object does not define, resolves to the schema
// there, before an ancestor of the reference holding an X of its own.
func rootDefs(t *testing.T) {
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(head+"$defs:\n  X: {type: string}\ncomponents:\n  schemas:\n"+
		"    A: {$ref: '#/$defs/X'}\n    B: {$defs: {X: {type: integer}}, properties: {p: {$ref: '#/$defs/X'}}}\n"))
	require.NoError(t, err)
	_, err = doc.ResolveAllReferences(t.Context(), soa.ResolveAllOptions{OpenAPILocation: location, DisableExternalRefs: true})
	require.NoError(t, err, revisit(t, result{}))

	b, ok := doc.Components.Schemas.Get("B")
	require.True(t, ok)
	p, ok := b.GetSchema().GetProperties().Get("p")
	require.True(t, ok)
	for name, js := range map[string]*oas3.JSONSchema[oas3.Referenceable]{"A": doc.Components.Schemas.GetOrZero("A"), "B/p": p} {
		target := js.GetResolvedSchema()
		require.NotNil(t, target, "%s\n%s", name, revisit(t, result{}))
		assert.Equal(t, []oas3.SchemaType{oas3.SchemaTypeString}, target.GetSchema().GetType(), "%s\n%s", name, revisit(t, result{}))
	}
	shadowedRootDefs(t)
}

// shadowedRootDefs pins the reading root-defs records for B, a schema with no
// $id that spells "#/$defs/X" and defines an X of its own: the root's when B is
// resolved first, its own once a chain has reached B, here from C.
func shadowedRootDefs(t *testing.T) {
	t.Helper()
	const body = "$defs:\n  X: {type: string}\ncomponents:\n  schemas:\n" +
		"    B: {$ref: '#/$defs/X', $defs: {X: {type: integer}}}\n    C: {$ref: '#/components/schemas/B'}\n"
	opts := references.ResolveOptions{TargetLocation: location, DisableExternalRefs: true}
	for name, want := range map[string]oas3.SchemaType{"first": oas3.SchemaTypeString, "after a chain": oas3.SchemaTypeInteger} {
		doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(head+body))
		require.NoError(t, err)
		opts.RootDocument = doc
		b, first := doc.Components.Schemas.GetOrZero("B"), doc.Components.Schemas.GetOrZero("C")
		if name == "first" {
			first = b
		}
		_, err = first.Resolve(t.Context(), opts)
		require.NoError(t, err, "%s\n%s", name, revisit(t, result{}))
		require.NotNil(t, b.GetResolvedSchema(), "%s\n%s", name, revisit(t, result{}))
		assert.Equal(t, []oas3.SchemaType{want}, b.GetResolvedSchema().GetSchema().GetType(), "%s\n%s", name, revisit(t, result{}))
	}
}

// trailingEmptyToken pins trailing-empty-token: through raw YAML, a pointer
// whose last token is empty names the node before it, a lone '/' the root,
// and a reference object spelling one resolves to that node.
func trailingEmptyToken(t *testing.T) {
	src := head + "components:\n  parameters:\n    P: {$ref: '#/x-params/q/'}\nx-params:\n  q: {name: q, in: query}\n"
	var tree yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &tree))
	q, err := jsonpointer.GetTarget(&tree, "/x-params/q")
	require.NoError(t, err)
	for pointer, want := range map[jsonpointer.JSONPointer]any{"/x-params/q/": q, "/": tree.Content[0]} {
		got, err := jsonpointer.GetTarget(&tree, pointer)
		require.NoError(t, err, "%s\n%s", pointer, revisit(t, result{}))
		assert.Same(t, want, got, "%s\n%s", pointer, revisit(t, result{}))
	}

	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
	require.NoError(t, err)
	_, err = doc.ResolveAllReferences(t.Context(), soa.ResolveAllOptions{OpenAPILocation: location, DisableExternalRefs: true})
	require.NoError(t, err, revisit(t, result{}))
	p := doc.Components.Parameters.GetOrZero("P")
	require.NotNil(t, p.GetObject(), revisit(t, result{}))
	own, err := jsonpointer.GetTarget(doc.GetRootNode(), "/x-params/q")
	require.NoError(t, err)
	assert.Same(t, own, p.GetObject().GetRootNode(), "it resolves to q itself: %s", revisit(t, result{}))
}

// mergeListPointer pins merge-list-pointer: through raw YAML, a pointer walk
// reads past a `<<` naming one mapping, but passes over one naming a list of
// mappings, and so does a reference object resolving through it.
func mergeListPointer(t *testing.T) {
	const tail = "x-c: &c {other: {name: o, in: query}}\nx-b: &b {q: {name: q, in: query}}\n"
	for merge, found := range map[string]bool{"*b": true, "[*c, *b]": false, "[*b]": false} {
		src := head + "components:\n  parameters:\n    P: {$ref: '#/x-params/q'}\n" + tail + "x-params:\n  <<: " +
			merge + "\n"
		var tree yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(src), &tree))
		_, err := jsonpointer.GetTarget(&tree, "/x-params/q")
		assert.Equal(t, found, err == nil, "%s: %v\n%s", merge, err, revisit(t, result{}))

		doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
		require.NoError(t, err)
		_, err = doc.Components.Parameters.GetOrZero("P").Resolve(t.Context(),
			soa.ResolveOptions{RootDocument: doc, TargetLocation: location, DisableExternalRefs: true})
		assert.Equal(t, found, err == nil, "%s resolved: %v\n%s", merge, err, revisit(t, result{}))
	}
}

// pointerTokenRange pins pointer-token-range: a pointer token holding a
// character above U+FFFF is refused as no valid token, one in the Basic
// Multilingual Plane is read, and a reference object spelling either resolves
// as the walk reads it.
func pointerTokenRange(t *testing.T) {
	for key, found := range map[string]bool{"é": true, "😀": false} {
		src := head + "components:\n  parameters:\n    P: {$ref: '#/x-params/" + key + "'}\nx-params:\n  \"" + key +
			"\": {name: q, in: query}\n"
		var tree yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(src), &tree))
		_, err := jsonpointer.GetTarget(&tree, jsonpointer.JSONPointer("/x-params/"+key))
		assert.Equal(t, found, err == nil, "%s: %v\n%s", key, err, revisit(t, result{}))
		if !found {
			assert.ErrorContains(t, err, "jsonpointer part must be a valid token", revisit(t, result{}))
		}

		doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
		require.NoError(t, err)
		_, err = doc.Components.Parameters.GetOrZero("P").Resolve(t.Context(),
			soa.ResolveOptions{RootDocument: doc, TargetLocation: location, DisableExternalRefs: true})
		assert.Equal(t, found, err == nil, "%s resolved: %v\n%s", key, err, revisit(t, result{}))
	}
}
