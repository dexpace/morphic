package canary_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolved is what resolveAll reports for a document it resolves cleanly.
const resolved = "resolved"

// resolveAll decodes input and resolves every reference in it, with external
// references off: what the resolver reports, or resolved. The library's own
// walk calls each reference's Resolve in turn, as the load phase does.
func resolveAll(ctx context.Context, input []byte) string {
	return resolveWith(ctx, input, soa.ResolveAllOptions{OpenAPILocation: location, DisableExternalRefs: true})
}

// resolveWith is resolveAll under opts.
func resolveWith(ctx context.Context, input []byte, opts soa.ResolveAllOptions) string {
	doc, _, err := soa.Unmarshal(ctx, bytes.NewReader(input))
	if err != nil {
		return "decode: " + err.Error()
	}
	if _, err := doc.ResolveAllReferences(ctx, opts); err != nil {
		return err.Error()
	}
	return resolved
}

// resolveTargets decodes input and resolves each discriminator mapping value
// in it as the load phase resolves a mapping target: a schema built from the
// value as a $ref, resolved on its own. It reports a line per value.
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
// the walk meets that reference before any member, and a pointer through the
// schema it is written in fails as naming nothing.
func schemaCycleOverflow(t *testing.T) {
	ref := func(name, target string) string { return refTo("schemas", name, target) }
	overflows := map[string]string{
		"one schema":                      schemas(ref("A", "A")),
		"two schemas":                     schemas(ref("A", "B"), ref("B", "A")),
		"three schemas":                   schemas(ref("A", "B"), ref("B", "C"), ref("C", "A")),
		"an outside reference met last":   schemas(ref("A", "B"), ref("B", "A"), ref("O", "A")),
		"a second outside reference":      schemas(ref("O", "A"), ref("P", "A"), ref("A", "B"), ref("B", "A")),
		"an outside reference to another": schemas(ref("O", "A"), ref("P", "B"), ref("A", "B"), ref("B", "A")),
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

// resolveOptionFields are the fields references.ResolveOptions declares,
// none of which bounds depth, work, fetches or size.
var resolveOptionFields = []string{
	"RootDocument", "TargetLocation", "TargetDocument", "DisableExternalRefs",
	"VirtualFS", "HTTPClient", "SkipValidation",
}

// chainLength is how many external documents the chains here name.
const chainLength = 1000

// resolutionUnbounded pins resolution-unbounded: nothing in the options bounds
// a resolution, the resolver never checks its context, only handing it to the
// requests it makes, and every document a chain names is read, through the
// file system or client the options give.
func resolutionUnbounded(t *testing.T) {
	t.Run("no option bounds it", func(t *testing.T) {
		t.Parallel()
		ty := reflect.TypeFor[references.ResolveOptions]()
		var fields []string
		for f := range ty.Fields() {
			fields = append(fields, f.Name)
		}
		assert.Equal(t, resolveOptionFields, fields, revisit(t, result{}))
	})
	t.Run("a cancelled context resolves", func(t *testing.T) {
		t.Parallel()
		doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(schemas(refTo("schemas", "A", "B"), "B: {type: string}")))
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = doc.ResolveAllReferences(ctx, soa.ResolveAllOptions{OpenAPILocation: location, DisableExternalRefs: true})
		require.NoError(t, err, revisit(t, result{}))
		a, ok := doc.Components.Schemas.Get("A")
		require.True(t, ok)
		assert.NotNil(t, a.GetResolvedSchema(), revisit(t, result{}))
	})
	t.Run("a chain of files is read whole", func(t *testing.T) {
		t.Parallel()
		files := &chainFS{}
		got := resolveWith(t.Context(), []byte(schemas("A: {$ref: 'd0.yaml#/x'}")),
			soa.ResolveAllOptions{OpenAPILocation: location, VirtualFS: files})
		assert.Equal(t, resolved, got, revisit(t, result{report: got}))
		assert.Equal(t, int64(chainLength), files.opens.Load(), revisit(t, result{}))
	})
	t.Run("a chain of URLs is read whole", func(t *testing.T) {
		t.Parallel()
		client := &chainClient{}
		got := resolveWith(t.Context(), []byte(schemas("A: {$ref: 'https://example.com/d0.yaml#/x'}")),
			soa.ResolveAllOptions{OpenAPILocation: location, HTTPClient: client})
		assert.Equal(t, resolved, got, revisit(t, result{report: got}))
		assert.Equal(t, int64(chainLength), client.requests.Load(), revisit(t, result{}))
	})
	t.Run("each request carries the context", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		client := &chainClient{}
		got := resolveWith(ctx, []byte(schemas("A: {$ref: 'https://example.com/d0.yaml#/x'}")),
			soa.ResolveAllOptions{OpenAPILocation: location, HTTPClient: client})
		assert.Equal(t, resolved, got, "a client that ignores it is read to the end: %s", revisit(t, result{report: got}))
		assert.Equal(t, int64(chainLength), client.cancelled.Load(), revisit(t, result{}))
	})
}

// chainDocument returns document n of a chain of chainLength: each names the
// next, and the last holds a schema.
func chainDocument(n int) []byte {
	if n >= chainLength-1 {
		return []byte("x: {type: string}\n")
	}
	return fmt.Appendf(nil, "x: {$ref: 'd%d.yaml#/x'}\n", n+1)
}

// chainNumber reads n from a path ending in dn.yaml, or fails.
func chainNumber(path string) (int, error) {
	name := path[strings.LastIndex(path, "/")+1:]
	digits, ok := strings.CutPrefix(strings.TrimSuffix(name, ".yaml"), "d")
	if !ok || !strings.HasSuffix(name, ".yaml") {
		return 0, fmt.Errorf("%q is no document of the chain", path)
	}
	return strconv.Atoi(digits)
}

// chainFS serves the chain's documents as files, counting each open.
type chainFS struct{ opens atomic.Int64 }

// Open serves the chain document name ends with.
func (c *chainFS) Open(name string) (fs.File, error) {
	c.opens.Add(1)
	n, err := chainNumber(name)
	if err != nil {
		return nil, err
	}
	return fstest.MapFS{"d.yaml": {Data: chainDocument(n)}}.Open("d.yaml")
}

// chainClient serves the chain's documents over HTTP, counting each request,
// and each request whose context is done, which it serves all the same.
type chainClient struct{ requests, cancelled atomic.Int64 }

// Do serves the chain document the request's path ends with.
func (c *chainClient) Do(req *http.Request) (*http.Response, error) {
	c.requests.Add(1)
	if req.Context().Err() != nil {
		c.cancelled.Add(1)
	}
	n, err := chainNumber(req.URL.Path)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Request: req, Header: http.Header{},
		Body: io.NopCloser(bytes.NewReader(chainDocument(n)))}, nil
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

// anchorIDScope pins anchor-id-scope: a $anchor or $id is found only from
// inside the top-level schema declaring it. From another, the anchor is read
// as a JSON pointer and the $id as an external document.
func anchorIDScope(t *testing.T) {
	anchor := "{$anchor: x, type: string}"
	id := "{$id: 'https://example.com/b', type: string}"
	inside := map[string]string{
		"an anchor": schemas("A: {properties: {p: {$ref: '#x'}}, $defs: {X: " + anchor + "}}"),
		"an id":     schemas("A: {properties: {p: {$ref: 'https://example.com/b'}}, $defs: {B: " + id + "}}"),
	}
	for name, doc := range inside {
		got := resolveAll(t.Context(), []byte(doc))
		assert.Equal(t, resolved, got, "%s inside: %s", name, revisit(t, result{report: got}))
	}

	got := resolveAll(t.Context(), []byte(schemas("A: {properties: {p: {$ref: '#x'}}}", "B: "+anchor)))
	assert.Contains(t, got, "jsonpointer must start with /", revisit(t, result{report: got}))

	across := []byte(schemas("A: {properties: {p: {$ref: 'https://example.com/b'}}}", "B: "+id))
	got = resolveAll(t.Context(), across)
	assert.Contains(t, got, "external reference not allowed", revisit(t, result{report: got}))
	client := &chainClient{}
	resolveWith(t.Context(), across, soa.ResolveAllOptions{OpenAPILocation: location, HTTPClient: client})
	assert.Equal(t, int64(1), client.requests.Load(), "the $id is fetched: %s", revisit(t, result{}))
}

// mappingUnresolved pins mapping-unresolved: neither resolution nor decoding
// reads a discriminator mapping value, and one resolved as a $ref to it, as
// the load phase does, recurses as a schema's chain does.
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
// there.
func rootDefs(t *testing.T) {
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(
		head+"$defs:\n  X: {type: string}\ncomponents:\n  schemas:\n    A: {$ref: '#/$defs/X'}\n"))
	require.NoError(t, err)
	_, err = doc.ResolveAllReferences(t.Context(), soa.ResolveAllOptions{OpenAPILocation: location, DisableExternalRefs: true})
	require.NoError(t, err, revisit(t, result{}))

	a, ok := doc.Components.Schemas.Get("A")
	require.True(t, ok)
	target := a.GetResolvedSchema()
	require.NotNil(t, target, revisit(t, result{}))
	assert.Equal(t, []oas3.SchemaType{oas3.SchemaTypeString}, target.GetSchema().GetType(), revisit(t, result{}))
}
