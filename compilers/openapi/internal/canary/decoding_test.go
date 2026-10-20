package canary_test

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	oas3core "github.com/speakeasy-api/openapi/jsonschema/oas3/core"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
)

// decoded is what unmarshal reports for a document it decodes, whatever its
// findings.
const decoded = "decoded"

// unmarshal decodes input into the library's model: decoded, or the error.
func unmarshal(ctx context.Context, input []byte) string {
	if _, _, err := soa.Unmarshal(ctx, bytes.NewReader(input)); err != nil {
		return err.Error()
	}
	return decoded
}

// goroutinesAlive decodes input on a goroutine of its own and reports the most
// goroutines alive at once beyond those alive before, read each time this one
// yields, or unfinished when decoding outlasts half the child's time. On one
// processor that is about every goroutine decoding has started and not yet
// run.
func goroutinesAlive(ctx context.Context, input []byte) string {
	before := runtime.NumGoroutine()
	done := make(chan error, 1)
	go func() {
		_, _, err := soa.Unmarshal(ctx, bytes.NewReader(input))
		done <- err
	}()
	most := 0
	for deadline := time.Now().Add(childTimeout / 2); time.Now().Before(deadline); runtime.Gosched() {
		select {
		case err := <-done:
			if err != nil {
				return "decode: " + err.Error()
			}
			return strconv.Itoa(most - before)
		default:
			most = max(most, runtime.NumGoroutine())
		}
	}
	return "unfinished"
}

// unknownKeyOrders decodes input, a document whose schema A writes keys the
// model does not know, a hundred times, and reports how many orders those
// keys came back in.
func unknownKeyOrders(ctx context.Context, input []byte) string {
	orders := map[string]bool{}
	for range 100 {
		doc, _, err := soa.Unmarshal(ctx, bytes.NewReader(input))
		if err != nil {
			return "decode: " + err.Error()
		}
		a, ok := doc.Components.Schemas.Get("A")
		if !ok {
			return "no schema A"
		}
		orders[strings.Join(a.GetSchema().GetCore().GetUnknownProperties(), ",")] = true
	}
	return strconv.Itoa(len(orders))
}

// goroutinesStarted decodes input and reports how many goroutines the process
// started meanwhile, with no collection running: a collection starts mark
// workers until there is one per processor, for every processor at a
// process's first and for any added since at a later one, and the count would
// include them. Collection is off, by percentage and by memory limit, and one
// runs first, so that a collection another goroutine began has started its
// workers before the count is read.
func goroutinesStarted(ctx context.Context, input []byte) string {
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(math.MaxInt64))
	runtime.GC()
	sample := []metrics.Sample{{Name: "/sched/goroutines-created:goroutines"}}
	metrics.Read(sample)
	before := sample[0].Value.Uint64()
	if _, _, err := soa.Unmarshal(ctx, bytes.NewReader(input)); err != nil {
		return "decode: " + err.Error()
	}
	metrics.Read(sample)
	return strconv.FormatUint(sample[0].Value.Uint64()-before, 10)
}

// aliasCycle pins alias-cycle: an alias to its own ancestor overflows the
// stack where the library samples string styles, unless that sample fills
// first; then decoding a schema expands it until memory runs out.
func aliasCycle(t *testing.T) {
	t.Run("met while sampling", func(t *testing.T) {
		t.Parallel()
		r := probe(t, "unmarshal", head+"x-a: &a [*a]\n")
		require.Equal(t, overflowed, r.outcome, revisit(t, r))
		assert.Contains(t, r.output, "getGlobalStringStyle", revisit(t, r))
	})
	t.Run("through a schema", func(t *testing.T) {
		t.Parallel()
		r := probe(t, "unmarshal", schemas("A: &a {type: object, properties: {p: *a}}"))
		assert.Equal(t, exhausted, r.outcome, revisit(t, r))
	})
	t.Run("in an extension, past the sample", func(t *testing.T) {
		t.Parallel()
		r := probe(t, "unmarshal", head+"x-a: &a {s: x, k: *a}\n")
		require.Equal(t, returned, r.outcome, revisit(t, r))
		assert.Equal(t, decoded, r.report, revisit(t, r))
	})
}

// doublings is how many times the aliases of the expansion pin double.
const doublings = 8

// aliasExpansion pins alias-expansion: decoding builds the model afresh for
// every path through an alias, sharing nothing between two aliases of one
// node, so aliases doubling at each level cost the doubling. The last level
// holds a schema for every path beneath it, and so does the core model
// decoding builds before the schemas are populated from it.
func aliasExpansion(t *testing.T) {
	entries := []string{"L0: &l0 {type: string}"}
	for i := 1; i <= doublings; i++ {
		entries = append(entries, fmt.Sprintf("L%d: &l%d {allOf: [*l%d, *l%d]}", i, i, i-1, i-1))
	}
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(schemas(entries...)))
	require.NoError(t, err)
	last, ok := doc.Components.Schemas.Get(fmt.Sprintf("L%d", doublings))
	require.True(t, ok)

	paths := 1<<(doublings+1) - 1
	models := distinct(last.GetSchema(), func(s *oas3.Schema) []*oas3.Schema {
		branches := make([]*oas3.Schema, 0, len(s.GetAllOf()))
		for _, js := range s.GetAllOf() {
			branches = append(branches, js.GetSchema())
		}
		return branches
	})
	assert.Equal(t, paths, models, "schemas: %s", revisit(t, result{}))
	cores := distinct(last.GetCore(), func(c oas3core.JSONSchema) []oas3core.JSONSchema {
		return c.Left.Value.AllOf.Value
	})
	assert.Equal(t, paths, cores, "core models: %s", revisit(t, result{}))
}

// distinct counts the distinct nodes reachable from root, itself included,
// through children.
func distinct[T comparable](root T, children func(T) []T) int {
	seen := map[T]bool{}
	stack := []T{root}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[top] {
			continue
		}
		seen[top] = true
		stack = append(stack, children(top)...)
	}
	return len(seen)
}

// allocations decodes input and walks the model it builds, and reports the
// bytes each allocated on the heap, decoding's first. It reads them through
// runtime.ReadMemStats, which counts what every processor's cache holds, where
// a runtime/metrics read leaves that uncounted until the cache is refilled.
func allocations(ctx context.Context, input []byte) string {
	var stats runtime.MemStats
	allocated := func() uint64 {
		runtime.ReadMemStats(&stats)
		return stats.TotalAlloc
	}
	start := allocated()
	doc, _, err := soa.Unmarshal(ctx, bytes.NewReader(input))
	if err != nil {
		return "decode: " + err.Error()
	}
	decoded := allocated()
	for range soa.Walk(ctx, doc) {
	}
	return fmt.Sprintf("%d %d", decoded-start, allocated()-decoded)
}

// nestingDepth is how deep the shallower schema nestingCost measures nests.
// The deeper nests four times as deep, so it holds four times the nodes.
const nestingDepth = 250

// nestingGrowth is how many times what the shallower schema costs the deeper
// must cost, in decoding and in walking: four times at most were each node's
// cost fixed, sixteen were it in proportion to the node's depth.
const nestingGrowth = 6

// nestingCost pins nesting-cost: decoding and walking a node cost in
// proportion to its depth, so four times the depth costs more than
// nestingGrowth times as much.
func nestingCost(t *testing.T) {
	shallow, deep := nestedCost(t, nestingDepth), nestedCost(t, 4*nestingDepth)
	for i, phase := range []string{"decoding", "walking"} {
		assert.Greater(t, deep[i], nestingGrowth*shallow[i], "%s: %s", phase, revisit(t, result{}))
	}
}

// nestedCost returns the bytes decoding and walking a schema nested depth
// levels deep allocate, measured in a child, where no other test allocates.
func nestedCost(t *testing.T, depth int) [2]uint64 {
	t.Helper()
	nested := strings.Repeat("{type: array, items: ", depth) + "{type: string}" + strings.Repeat("}", depth)
	r := probe(t, "allocations", schemas("A: "+nested))
	require.Equal(t, returned, r.outcome, revisit(t, r))
	var cost [2]uint64
	_, err := fmt.Sscanf(r.report, "%d %d", &cost[0], &cost[1])
	require.NoError(t, err, revisit(t, r))
	return cost
}

// nodeCount is how many entries and elements the documents measuring
// goroutines write.
const nodeCount = 1000

// aliveShare is the share of a map's or a sequence's goroutines that must be
// alive at once, as one over it: a fifth, under half what the race detector's
// slower scheduling leaves, and far past any cap an errgroup limit sets.
const aliveShare = 5

// schemaKeywords are keywords the schema model holds a field for, each written
// with a scalar value.
var schemaKeywords = []string{
	"type: string", "title: t", "description: d", "format: f", "pattern: p", "minLength: 1",
	"maxLength: 2", "minimum: 1", "maximum: 2", "multipleOf: 1", "minItems: 1", "maxItems: 2",
	"uniqueItems: true", "minProperties: 1", "maxProperties: 2", "readOnly: true", "writeOnly: false",
	"deprecated: true", "contentMediaType: m", "contentEncoding: e", "$id: i", "$anchor: a",
	"default: 1", "const: 1",
}

// goroutinePerNode pins goroutine-per-node: decoding starts a goroutine for
// every key written on an object, entry of a map and element of a sequence, and
// nothing caps how many of a map's or a sequence's are alive at once: on one
// processor, a fifth of them at least. An object's few are gone before a
// sample. A model's unknown keys come back in the order their goroutines end.
func goroutinePerNode(t *testing.T) {
	t.Run("fields started", func(t *testing.T) {
		t.Parallel()
		fields := childCount(t, probe(t, "goroutines", schemas("S: {"+strings.Join(schemaKeywords, ", ")+"}")))
		none := childCount(t, probe(t, "goroutines", schemas("S: {}")))
		assert.GreaterOrEqual(t, fields-none, len(schemaKeywords), revisit(t, result{}))
	})
	t.Run("unknown keys in no fixed order", func(t *testing.T) {
		t.Parallel()
		keys := make([]string, 0, 20)
		for i := range 20 {
			keys = append(keys, fmt.Sprintf("k%02d: %d", i, i))
		}
		r := probe(t, "orders", schemas("A: {"+strings.Join(keys, ", ")+"}"), "GOMAXPROCS=4")
		assert.Greater(t, childCount(t, r), 1, revisit(t, r))
	})
	var entries, elements strings.Builder
	for i := range nodeCount {
		fmt.Fprintf(&entries, "    S%d: {}\n", i)
		fmt.Fprintf(&elements, "        - p%d\n", i)
	}
	documents := []struct {
		name, doc string
		nodes     int
	}{
		{"map entries", head + "components:\n  schemas:\n" + entries.String(), nodeCount},
		{"sequence elements", head + "components:\n  schemas:\n    S:\n      required:\n" + elements.String(),
			nodeCount},
	}
	for _, d := range documents {
		t.Run(d.name+" started", func(t *testing.T) {
			t.Parallel()
			assert.GreaterOrEqual(t, childCount(t, probe(t, "goroutines", d.doc)), d.nodes, revisit(t, result{}))
		})
		t.Run(d.name+" alive at once", func(t *testing.T) {
			t.Parallel()
			alive := childCount(t, probe(t, "alive", d.doc, "GOMAXPROCS=1"))
			assert.Greater(t, alive, d.nodes/aliveShare, revisit(t, result{}))
		})
	}
}

// childCount returns the count a child that returned reported, failing the
// test on any other ending.
func childCount(t *testing.T, r result) int {
	t.Helper()
	require.Equal(t, returned, r.outcome, revisit(t, r))
	n, err := strconv.Atoi(r.report)
	require.NoError(t, err, revisit(t, r))
	return n
}

// taggedSections are the components sections where a mapping tagged with
// anything but !!map panics the decoder, each with a body for the mapping.
var taggedSections = map[string]string{
	"requestBodies":   "{content: {application/json: {schema: {type: string}}}}",
	"responses":       "{description: d}",
	"parameters":      "{name: q, in: query}",
	"headers":         "{schema: {type: string}}",
	"examples":        "{value: 1}",
	"links":           "{operationId: o}",
	"securitySchemes": "{type: http, scheme: basic}",
}

// untaggedSections are sections where the same tag decodes.
var untaggedSections = map[string]string{
	"schemas":   "{type: string}",
	"pathItems": "{}",
	"callbacks": "{}",
}

// taggedTrials is how many times each tagged mapping is decoded. Whether one
// decode panics or fails turns on how its goroutines are scheduled, and the
// panic is so much the likelier that some trial all but surely panics.
const taggedTrials = 5

// taggedMappingPanic pins tagged-mapping-panic: a mapping tagged with anything
// but !!map, where the model reads a reference object of seven kinds, panics
// in a goroutine the decoder started, where no recover of the caller reaches,
// or now and then fails to decode instead.
func taggedMappingPanic(t *testing.T) {
	for s, body := range taggedSections {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			panics := 0
			for range taggedTrials {
				r := probe(t, "unmarshal", section(s, "A: !thing "+body))
				switch r.outcome {
				case panicked:
					panics++
					assert.Contains(t, r.report, "nil pointer dereference", revisit(t, r))
				case returned:
					assert.Contains(t, r.report, "got <nil>", revisit(t, r))
				default:
					t.Error(revisit(t, r))
				}
			}
			assert.NotZero(t, panics, "no trial panicked: %s", revisit(t, result{}))
		})
	}
	for s, body := range untaggedSections {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			r := probe(t, "unmarshal", section(s, "A: !thing "+body))
			require.Equal(t, returned, r.outcome, revisit(t, r))
			assert.Equal(t, decoded, r.report, revisit(t, r))
		})
	}
}

// recoverablePanic pins recoverable-panic: decoding a document of nothing but
// whitespace, and resolving a reference to a $ref written with no value, panic
// on the goroutine that called them, where its recover reaches the panic.
func recoverablePanic(t *testing.T) {
	tests := map[string]struct{ child, input, message string }{
		"a whitespace document": {"unmarshal", " \n", "unknown node kind"},
		"a $ref with no value": {"resolve", section("responses", "A: {$ref: '#/B'}") + "B: {$ref}\n",
			"nil pointer dereference"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := probe(t, tc.child, tc.input)
			require.Equal(t, returned, r.outcome, revisit(t, r))
			assert.True(t, strings.HasPrefix(r.report, recoveredMark), revisit(t, r))
			assert.Contains(t, r.report, tc.message, revisit(t, r))
		})
	}
}

// anchoredEntryDropped pins anchored-entry-dropped: an anchored entry of a
// map the model embeds (a path, a method, a response, a callback expression,
// a security requirement's scheme) is left out, with no finding, while one in
// a named map is kept.
func anchoredEntryDropped(t *testing.T) {
	doc, findings, err := soa.Unmarshal(t.Context(), strings.NewReader(
		"openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n"+
			"  /x: &p {get: {responses: {'200': {description: ok}}}}\n"+
			"  /y:\n"+
			"    get: &g {responses: {'200': {description: ok}}}\n"+
			"    post:\n"+
			"      responses: {'200': {description: ok}, '404': &r {description: gone}}\n"+
			"      callbacks: {c: {'{$request.body#/u}': &c {post: {responses: {'200': {description: ok}}}}}}\n"+
			"      security: [{k: &q []}]\n"+
			"components:\n  schemas:\n    S: &s {type: string}\n  responses:\n    R: &rr {description: kept}\n"+
			"  securitySchemes:\n    k: {type: http, scheme: basic}\n"))
	require.NoError(t, err)
	assert.Empty(t, findings, revisit(t, result{}))

	_, path := doc.Paths.Get("/x")
	assert.False(t, path, "an anchored path: %s", revisit(t, result{}))
	y, ok := doc.Paths.Get("/y")
	require.True(t, ok)
	assert.Nil(t, y.GetObject().Get(), "an anchored method: %s", revisit(t, result{}))
	post := y.GetObject().Post()
	require.NotNil(t, post)
	_, response := post.GetResponses().Get("404")
	assert.False(t, response, "an anchored response: %s", revisit(t, result{}))
	callback, ok := post.GetCallbacks().Get("c")
	require.True(t, ok)
	assert.Zero(t, callback.GetObject().Len(), "an anchored callback expression: %s", revisit(t, result{}))
	require.Len(t, post.GetSecurity(), 1)
	assert.Zero(t, post.GetSecurity()[0].Len(), "an anchored security requirement: %s", revisit(t, result{}))

	_, schema := doc.Components.Schemas.Get("S")
	_, named := doc.Components.Responses.Get("R")
	assert.True(t, schema && named, "an anchored component is kept: %s", revisit(t, result{}))
}

// embeddedMapKeys pins embedded-map-keys: an object that embeds a map reads a
// key it does not define as an entry of that map, so it records no unknown
// key, where any other object records one.
func embeddedMapKeys(t *testing.T) {
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(
		"openapi: 3.1.0\ninfo: {title: t, version: '1', other: i}\npaths:\n  other: {}\n  /a:\n"+
			"    other: {responses: {'200': {description: ok}}}\n"+
			"    get:\n      responses: {'200': {description: ok}, other: {description: r}}\n"+
			"      callbacks: {c: {other: {}}}\n"))
	require.NoError(t, err)
	assert.Equal(t, []string{"other"}, doc.Info.GetCore().GetUnknownProperties(), revisit(t, result{}))

	a, ok := doc.Paths.Get("/a")
	require.True(t, ok)
	item := a.GetObject()
	var methods []string
	for m := range item.Keys() {
		methods = append(methods, string(m))
	}
	assert.ElementsMatch(t, []string{"other", "get"}, methods, "a path item: %s", revisit(t, result{}))
	assert.Empty(t, item.GetCore().GetUnknownProperties(), "a path item: %s", revisit(t, result{}))

	responses := item.Get().GetResponses()
	_, entry := responses.Get("other")
	assert.True(t, entry, "responses: %s", revisit(t, result{}))
	assert.Empty(t, responses.GetCore().GetUnknownProperties(), "responses: %s", revisit(t, result{}))

	_, entry = doc.Paths.Get("other")
	assert.True(t, entry, "paths: %s", revisit(t, result{}))
	assert.Empty(t, doc.Paths.GetCore().GetUnknownProperties(), "paths: %s", revisit(t, result{}))
	callback, ok := item.Get().GetCallbacks().Get("c")
	require.True(t, ok)
	_, entry = callback.GetObject().Get("other")
	assert.True(t, entry, "a callback: %s", revisit(t, result{}))
	assert.Empty(t, callback.GetObject().GetCore().GetUnknownProperties(), "a callback: %s", revisit(t, result{}))
}

// duplicateKey pins duplicate-key: a key written twice is read as its last
// occurrence through the model, which skips the earlier, with a warning, and
// as its first through raw YAML, the model's own fallback for what it does not
// hold included.
func duplicateKey(t *testing.T) {
	source := head + "components:\n  schemas:\n    A: {type: string, title: first}\n    A: {type: integer}\n" +
		"    X: {xml: {name: first}, xml: {prefix: last}}\n" +
		"x-e:\n  k: first\n  k: last\n"
	doc, found, err := soa.Unmarshal(t.Context(), strings.NewReader(source))
	require.NoError(t, err)
	require.NotEmpty(t, found, revisit(t, result{}))
	assert.Contains(t, found[0].Error(), "validation-duplicate-key", revisit(t, result{}))

	typed, err := jsonpointer.GetTarget(doc, "/components/schemas/A")
	require.NoError(t, err)
	js, ok := typed.(*oas3.JSONSchema[oas3.Referenceable])
	require.True(t, ok, "%T", typed)
	assert.Equal(t, []oas3.SchemaType{oas3.SchemaTypeInteger}, js.GetSchema().GetType(),
		"the model keeps the last: %s", revisit(t, result{}))
	assert.Empty(t, js.GetSchema().GetTitle(), "the model skips the first: %s", revisit(t, result{}))
	x, ok := doc.Components.Schemas.Get("X")
	require.True(t, ok)
	xml := x.GetSchema().GetXML()
	require.NotNil(t, xml, revisit(t, result{}))
	require.NotNil(t, xml.Prefix, revisit(t, result{}))
	assert.Equal(t, "last", *xml.Prefix, revisit(t, result{}))
	assert.Nil(t, xml.Name, "a repeated field skips the first, merging nothing: %s", revisit(t, result{}))

	assert.Equal(t, "first", scalarAt(t, doc, "/x-e/k"), "the model's fallback: %s", revisit(t, result{}))
	var tree yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(source), &tree))
	assert.Equal(t, "string", scalarAt(t, &tree, "/components/schemas/A/type"),
		"raw YAML keeps the first: %s", revisit(t, result{}))
}

// scalarAt returns the value of the scalar node pointer reaches in source.
func scalarAt(t *testing.T, source any, pointer string) string {
	t.Helper()
	target, err := jsonpointer.GetTarget(source, jsonpointer.JSONPointer(pointer))
	require.NoError(t, err)
	node, ok := target.(*yaml.Node)
	require.True(t, ok, "%T", target)
	return node.Value
}

// unmodeledKeywords pins unmodeled-keywords: the schema model has no field for
// $dynamicRef, $dynamicAnchor, $comment, $vocabulary or dependentRequired, and
// records each, like any key it does not know, by name alone; the value stays
// in the raw node.
func unmodeledKeywords(t *testing.T) {
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(schemas(
		"A: {$dynamicAnchor: n, $comment: c, $dynamicRef: '#n', $vocabulary: {}, dependentRequired: {}, "+
			"unknownKeyword: u, type: object}")))
	require.NoError(t, err)
	a, ok := doc.Components.Schemas.Get("A")
	require.True(t, ok)

	unmodeled := []string{"DynamicRef", "DynamicAnchor", "Comment", "Vocabulary", "DependentRequired"}
	for f := range reflect.TypeFor[oas3.Schema]().Fields() {
		assert.NotContains(t, unmodeled, f.Name, revisit(t, result{}))
	}
	assert.ElementsMatch(t,
		[]string{"$dynamicAnchor", "$comment", "$dynamicRef", "$vocabulary", "dependentRequired", "unknownKeyword"},
		a.GetSchema().GetCore().GetUnknownProperties(), revisit(t, result{}))
	assert.Equal(t, "#n", scalarAt(t, a.GetSchema().GetRootNode(), "/$dynamicRef"), revisit(t, result{}))
}

// bareWalkPanics returns what soa.Walk panicked with, started from start not
// wrapped in its owner, or nil.
func bareWalkPanics[T any](ctx context.Context, start *T) (panicked any) {
	defer func() { panicked = recover() }()
	for range soa.Walk(ctx, start) {
	}
	return nil
}

// bareWalkPanic pins bare-walk-panic: Walk started from a reference object, or
// from a schema not wrapped as a referenceable one, panics; wrapped, it walks.
func bareWalkPanic(t *testing.T) {
	ctx := t.Context()
	bare := map[string]any{
		"PathItem":       bareWalkPanics(ctx, &soa.PathItem{}),
		"Parameter":      bareWalkPanics(ctx, &soa.Parameter{}),
		"Header":         bareWalkPanics(ctx, &soa.Header{}),
		"RequestBody":    bareWalkPanics(ctx, &soa.RequestBody{}),
		"Response":       bareWalkPanics(ctx, &soa.Response{}),
		"Callback":       bareWalkPanics(ctx, &soa.Callback{}),
		"Example":        bareWalkPanics(ctx, &soa.Example{}),
		"Link":           bareWalkPanics(ctx, &soa.Link{}),
		"SecurityScheme": bareWalkPanics(ctx, &soa.SecurityScheme{}),
		"concrete JSONSchema": bareWalkPanics(ctx,
			oas3.NewJSONSchemaFromSchema[oas3.Concrete](&oas3.Schema{})),
	}
	for name, got := range bare {
		assert.Contains(t, fmt.Sprint(got), "no match handler registered", "%s: %s", name, revisit(t, result{}))
	}
	wrapped := map[string]any{
		"PathItem":       bareWalkPanics(ctx, &soa.ReferencedPathItem{Object: &soa.PathItem{}}),
		"Parameter":      bareWalkPanics(ctx, &soa.ReferencedParameter{Object: &soa.Parameter{}}),
		"Header":         bareWalkPanics(ctx, &soa.ReferencedHeader{Object: &soa.Header{}}),
		"RequestBody":    bareWalkPanics(ctx, &soa.ReferencedRequestBody{Object: &soa.RequestBody{}}),
		"Response":       bareWalkPanics(ctx, &soa.ReferencedResponse{Object: &soa.Response{}}),
		"Callback":       bareWalkPanics(ctx, &soa.ReferencedCallback{Object: &soa.Callback{}}),
		"Example":        bareWalkPanics(ctx, &soa.ReferencedExample{Object: &soa.Example{}}),
		"Link":           bareWalkPanics(ctx, &soa.ReferencedLink{Object: &soa.Link{}}),
		"SecurityScheme": bareWalkPanics(ctx, &soa.ReferencedSecurityScheme{Object: &soa.SecurityScheme{}}),
		"referenceable JSONSchema": bareWalkPanics(ctx,
			oas3.ConcreteToReferenceable(oas3.NewJSONSchemaFromSchema[oas3.Concrete](&oas3.Schema{}))),
	}
	for name, got := range wrapped {
		assert.Nil(t, got, "%s: %s", name, revisit(t, result{}))
	}
}
