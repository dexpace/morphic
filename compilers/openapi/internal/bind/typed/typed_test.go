package typed_test

import (
	"encoding/json/jsontext"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/walk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
)

const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
x-lib:
  S: {type: object, properties: {inner: {$ref: '#/components/schemas/A'}, x: {type: string}}, discriminator: {propertyName: k}}
  T: true
components:
  schemas:
    A: {type: object, properties: {p: {type: integer}}}
    R: {$ref: '#/x-lib/S'}
    Q: {$ref: '#/components/schemas/R'}
    B: {$ref: '#/x-lib/T'}
    U: {$ref: '#/components/schemas/Missing'}
    D: {discriminator: {propertyName: k, mapping: {s: '#/x-lib/S'}}}
    X: {$ref: '#/x-lib/S/properties/x'}
`

// bindings decodes spec, binds it, and builds its bindings as the load phase
// does: every schema of the model recorded where the walk reads it, then a
// schema built for each landing.
func bindings(t *testing.T) (*typed.Bindings, *soa.OpenAPI, []typed.Built) {
	t.Helper()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(spec))
	require.NoError(t, err)
	bd := typed.NewBuilder(bind.Bind(doc.GetRootNode(), bind.Config{Self: "spec.yaml"}))
	for item := range soa.Walk(t.Context(), doc) {
		at := bind.Position{Pointer: typed.PointerOf(item.Location)}
		require.NoError(t, item.Match(soa.Matcher{
			Schema:        func(js *oas3.JSONSchema[oas3.Referenceable]) error { bd.Schema(js, at); return nil },
			Discriminator: func(d *oas3.Discriminator) error { bd.Discriminator(d, at); return nil },
		}))
	}
	built := bd.Build(t.Context())
	return bd.Bindings(), doc, built
}

// component returns the component schema name declares.
func component(t *testing.T, doc *soa.OpenAPI, name string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	js, ok := doc.Components.Schemas.Get(name)
	require.True(t, ok, name)
	return js
}

// TestBindings_AnswerByTheSchemaTheLoweringHolds pins what a schema's own
// object answers: where it was read, its $ref's entry, the schema written
// where that lands, the one its chain ends on, the model's own for a schema of
// the model and the one built for raw YAML, and nothing for a $ref that lands
// on none or a schema the bindings never met.
func TestBindings_AnswerByTheSchemaTheLoweringHolds(t *testing.T) {
	t.Parallel()
	b, doc, _ := bindings(t)
	a, r, q, u := component(t, doc, "A"), component(t, doc, "R"), component(t, doc, "Q"), component(t, doc, "U")

	site, ok := b.Site(q)
	require.True(t, ok)
	assert.Equal(t, bind.Position{Pointer: "/components/schemas/Q"}, site)
	assert.Same(t, r, b.Declared(q), "Q lands on R, the model's own schema")
	s := b.End(q)
	require.NotNil(t, s, "Q's chain ends on the schema built for /x-lib/S")
	assert.Same(t, s, b.At(bind.Position{Pointer: "/x-lib/S"}))
	assert.Same(t, s, b.Declared(r))
	inner, ok := s.GetSchema().GetProperties().Get("inner")
	require.True(t, ok)
	assert.Same(t, a, b.End(inner), "a $ref inside a built schema is bound too")

	assert.Nil(t, b.Declared(u))
	assert.Nil(t, b.End(u))
	_, ok = b.Entry(a)
	assert.False(t, ok, "a schema writing no $ref has no entry")
	stranger := oas3.NewJSONSchemaFromReference("#/components/schemas/A")
	_, ok = b.Site(stranger)
	assert.False(t, ok, "a schema the bindings never met")
	assert.Nil(t, b.Declared(stranger))
	assert.NotNil(t, b.Table())
}

// TestBindings_AnswerAMappingValueByItsDiscriminator pins Mapping: a value of
// a discriminator the bindings met, and nothing for one they did not.
func TestBindings_AnswerAMappingValueByItsDiscriminator(t *testing.T) {
	t.Parallel()
	b, doc, _ := bindings(t)
	d := component(t, doc, "D").GetSchema().GetDiscriminator()
	m, ok := b.Mapping(d, "#/x-lib/S")
	require.True(t, ok)
	assert.Equal(t, bind.Position{Pointer: "/x-lib/S"}, m.Hop.Target)
	_, ok = b.Mapping(&oas3.Discriminator{}, "#/x-lib/S")
	assert.False(t, ok)
	_, ok = b.Mapping(nil, "#/x-lib/S")
	assert.False(t, ok)
	built := b.At(bind.Position{Pointer: "/x-lib/S"}).GetSchema().GetDiscriminator()
	_, ok = b.Mapping(built, "#/x-lib/S")
	assert.False(t, ok, "a discriminator of a built schema is known, though it maps nothing")
}

// TestBuilder_BuildsEachLandingOnce pins Build: a schema for each landing, in
// order, with what decoding it found, a boolean one included, and none built
// twice for a position a schema is held at.
func TestBuilder_BuildsEachLandingOnce(t *testing.T) {
	t.Parallel()
	b, _, built := bindings(t)
	require.Len(t, built, 3)
	assert.Equal(t, jsontext.Pointer("/x-lib/S"), built[0].Landing.At.Pointer)
	assert.Equal(t, jsontext.Pointer("/x-lib/S/properties/x"), built[1].Landing.At.Pointer)
	assert.Equal(t, jsontext.Pointer("/x-lib/T"), built[2].Landing.At.Pointer)
	for _, x := range built {
		require.NoError(t, x.Err)
		assert.Same(t, x.Schema, b.At(x.Landing.At))
	}
	x, ok := built[0].Schema.GetSchema().GetProperties().Get("x")
	require.True(t, ok)
	assert.Same(t, x, built[1].Schema, "a landing inside one built before holds that one's schema")
	assert.True(t, built[2].Schema.IsBool())
}

// TestBindings_ANilOneKnowsNothing pins the nil Bindings a context with no
// load carries: every answer is none.
func TestBindings_ANilOneKnowsNothing(t *testing.T) {
	t.Parallel()
	var b *typed.Bindings
	js := oas3.NewJSONSchemaFromReference("#/x")
	_, ok := b.Site(js)
	assert.False(t, ok)
	assert.Nil(t, b.Declared(js))
	assert.Nil(t, b.End(js))
	assert.Nil(t, b.At(bind.Position{}))
	assert.Nil(t, b.Table())
	_, ok = b.Mapping(&oas3.Discriminator{}, "#/x")
	assert.False(t, ok)
	_, ok = b.Unplaced(js)
	assert.False(t, ok)
	bd := typed.NewBuilder(nil)
	bd.Schema(nil, bind.Position{})
	assert.Nil(t, bd.Bindings().At(bind.Position{}), "recording no schema holds none")
}

// TestBindings_AnswersWhereAnUnplacedModelWasRead pins Unplaced: the
// reference object a schema or discriminator was recorded under, and nothing
// for one never recorded.
func TestBindings_AnswersWhereAnUnplacedModelWasRead(t *testing.T) {
	t.Parallel()
	bd := typed.NewBuilder(nil)
	js, d := oas3.NewJSONSchemaFromReference("#/x"), &oas3.Discriminator{}
	bd.Unplaced(js, "/paths/~1a/get/parameters/0")
	bd.Unplaced(d, "/components/parameters/P")
	b := bd.Bindings()
	for model, want := range map[any]string{js: "/paths/~1a/get/parameters/0", d: "/components/parameters/P"} {
		site, ok := b.Unplaced(model)
		assert.True(t, ok)
		assert.Equal(t, want, site)
	}
	_, ok := b.Unplaced(oas3.NewJSONSchemaFromReference("#/x"))
	assert.False(t, ok, "another schema, though written alike")
}

// TestPointerOf pins the pointer a walk location spells: none for the walk's
// own start, a field, a key escaped as RFC 6901 escapes it, and an index,
// where the library's own rendering writes a lone '/' and '//' before a key.
func TestPointerOf(t *testing.T) {
	t.Parallel()
	type loc = walk.LocationContext[any]
	assert.Equal(t, jsontext.Pointer(""), typed.PointerOf(walk.Locations[any]{}))
	assert.Equal(t, jsontext.Pointer("/a~1b/~0c"), typed.PointerOf(walk.Locations[any]{{ParentKey: new("a/b")},
		{ParentKey: new("~c")}}))
	assert.Equal(t, jsontext.Pointer("/properties/p/allOf/2"), typed.PointerOf(walk.Locations[any]{
		{ParentField: "properties", ParentKey: new("p")}, loc{ParentField: "allOf", ParentIndex: new(2)}}))
}

// TestPointerOf_CostsItsLength pins that a location is spelled once: one twice
// as deep allocates about twice the bytes, where extending the pointer a token
// at a time copied it at each token and cost four times. Bytes count the work,
// since they do not depend on the machine. It is not parallel, as the count is
// the process's.
func TestPointerOf_CostsItsLength(t *testing.T) {
	deep := func(levels int) walk.Locations[any] {
		out := make(walk.Locations[any], levels)
		for i := range out {
			out[i] = walk.LocationContext[any]{ParentField: "properties", ParentKey: new("p")}
		}
		return out
	}
	short, long := deep(500), deep(1000)
	require.Equal(t, jsontext.Pointer(strings.Repeat("/properties/p", 1000)), typed.PointerOf(long))
	shortBytes := allocated(func() { _ = typed.PointerOf(short) })
	longBytes := allocated(func() { _ = typed.PointerOf(long) })
	assert.Less(t, longBytes, 3*shortBytes, "twice the depth, about twice the bytes")
}

// TestBuild_CostsLinearInItsLandings pins that the landings of one wide mapping
// read their paths through one view: twice the landings allocate about twice
// the bytes, where a view built for each landing expanded the mapping again
// each time and cost four times. It is not parallel, as the count is the
// process's.
func TestBuild_CostsLinearInItsLandings(t *testing.T) {
	cost := func(n int) uint64 {
		var b strings.Builder
		b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n")
		for i := range n {
			fmt.Fprintf(&b, "    S%d: {$ref: '#/x-lib/T%d'}\n", i, i)
		}
		b.WriteString("x-lib:\n")
		for i := range n {
			fmt.Fprintf(&b, "  T%d: {type: string}\n", i)
		}
		var root yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(b.String()), &root))
		table := bind.Bind(&root, bind.Config{Self: "spec.yaml"})
		require.Len(t, table.Landings(), n)
		return allocated(func() { typed.NewBuilder(table).Build(t.Context()) })
	}
	assert.Less(t, cost(2_000), 3*cost(1_000), "twice the landings, about twice the bytes")
}

// allocated is the fewest bytes five runs of f allocate on the heap, over
// three tries: whatever else the process allocates meanwhile only adds to a
// count, so the least is the one f alone accounts for.
func allocated(f func()) uint64 {
	var stats runtime.MemStats
	least := uint64(math.MaxUint64)
	for range 3 {
		runtime.ReadMemStats(&stats)
		before := stats.TotalAlloc
		for range 5 {
			f()
		}
		runtime.ReadMemStats(&stats)
		least = min(least, stats.TotalAlloc-before)
	}
	return least
}
