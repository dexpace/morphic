package defs

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaFromYAML unmarshals body as a standalone JSON Schema document. Nothing
// here calls Resolve: Target and TargetFrom read the parsed tree directly, so a
// raw, unresolved schema is everything a case needs. The schema itself doubles
// as the Navigable document — its own GetRootNode is the whole tree's root,
// exactly as an OpenAPI document's is for a compiled spec.
func schemaFromYAML(t *testing.T, body string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	js := &oas3.JSONSchema[oas3.Referenceable]{}
	valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(body), js)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	return js
}

// prop returns js's own property named name.
func prop(t *testing.T, js *oas3.JSONSchema[oas3.Referenceable], name string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	p, ok := js.GetSchema().GetProperties().Get(name)
	require.True(t, ok, "fixture declares property %q", name)
	return p
}

// defEntry returns js's own $defs entry named key.
func defEntry(t *testing.T, js *oas3.JSONSchema[oas3.Referenceable], key string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	d, ok := js.GetSchema().GetDefs().Get(key)
	require.True(t, ok, "fixture declares $defs/%s", key)
	return d
}

func TestIsPointer(t *testing.T) {
	t.Parallel()
	assert.True(t, IsPointer("/$defs/n"))
	assert.True(t, IsPointer("/$defs/n/properties/x"))
	assert.False(t, IsPointer("/components/schemas/A"), "not a $defs-rooted pointer")
	assert.False(t, IsPointer(""), "the whole-document pointer is not a $defs pointer")
	assert.False(t, IsPointer("/$defs"), "no trailing slash names the map itself, not an entry")
}

// TestTarget_LocalIDRuleAndEscapes pins tryResolveLocalDefs: a schema that
// itself carries $id and its own sibling $defs is read from ITS OWN
// definitions directly, without ever walking to an ancestor. That is the one
// case the generic ancestor search (TargetFrom) can never reach by itself,
// since it always starts one level above the referencing schema, never at it
// (GitHub #557). Without the rule, p's own reference is unresolved: the
// ancestor loop starting above p never walks back down into p's own $defs.
func TestTarget_LocalIDRuleAndEscapes(t *testing.T) {
	t.Parallel()
	const src = `
type: object
properties:
  p:
    $id: "https://x.test/p"
    $ref: "#/$defs/n"
    $defs:
      n: {type: string}
      a/b: {type: integer}
      "x~y": {type: boolean}
`
	root := schemaFromYAML(t, src)
	p := prop(t, root, "p")

	got, at, ok := NewReader(root).Target(p, "/$defs/n")
	require.True(t, ok)
	assert.Same(t, defEntry(t, p, "n"), got)
	assert.Equal(t, jsontext.Pointer("/properties/p/$defs/n"), at,
		"the position is where p's own $defs is written, not the root-relative pointer it spelled")

	got, _, ok = NewReader(root).Target(p, "/$defs/a~1b")
	require.True(t, ok, "~1 decodes to a literal slash in the key")
	assert.Same(t, defEntry(t, p, "a/b"), got)

	got, _, ok = NewReader(root).Target(p, "/$defs/x~0y")
	require.True(t, ok, "~0 decodes to a literal tilde in the key")
	assert.Same(t, defEntry(t, p, "x~y"), got)
}

// TestTargetFrom_DocumentItselfStep pins the step standard resolution takes
// before ever walking an ancestor: the pointer is read from the document
// itself first. An OpenAPI document has no $defs of its own, so only a
// standalone schema document — its own root — ever answers here.
func TestTargetFrom_DocumentItselfStep(t *testing.T) {
	t.Parallel()
	const src = `
$defs:
  k: {type: string}
properties:
  p: {type: string}
`
	root := schemaFromYAML(t, src)
	p := prop(t, root, "p")

	got, at, ok := NewReader(root).Target(p, "/$defs/k")
	require.True(t, ok)
	assert.Same(t, defEntry(t, root, "k"), got)
	assert.Equal(t, jsontext.Pointer("/$defs/k"), at, "found at the document's own position, no ancestor prefix")
}

// TestTargetFrom_NearestAncestor pins the fallback for a schema that is not
// itself a resource: the search walks up one JSON-pointer segment at a time
// from the reference's own position — the properties container first, which
// holds no $defs, then the enclosing schema, which does — never the
// reference's own position.
func TestTargetFrom_NearestAncestor(t *testing.T) {
	t.Parallel()
	const src = `
type: object
properties:
  outer:
    $defs:
      k: {type: object}
    properties:
      inner:
        $ref: "#/$defs/k"
`
	root := schemaFromYAML(t, src)
	outer := prop(t, root, "outer")
	inner := prop(t, outer, "inner")

	got, at, ok := NewReader(root).Target(inner, "/$defs/k")
	require.True(t, ok)
	assert.Same(t, defEntry(t, outer, "k"), got)
	assert.Equal(t, jsontext.Pointer("/properties/outer/$defs/k"), at)
}

// TestTargetFrom_ParentThatHoldsTheDefinition pins the search's first step: a
// reference that is a schema keyword's own value, rather than an entry of a map
// or list, has the schema that holds the $defs as its parent, and reads them.
func TestTargetFrom_ParentThatHoldsTheDefinition(t *testing.T) {
	t.Parallel()
	const src = `
properties:
  outer:
    $defs:
      k: {type: object}
    not: {$ref: "#/$defs/k"}
`
	root := schemaFromYAML(t, src)
	outer := prop(t, root, "outer")
	not := outer.GetSchema().Not

	got, at, ok := NewReader(root).Target(not, "/$defs/k")
	require.True(t, ok)
	assert.Same(t, defEntry(t, outer, "k"), got)
	assert.Equal(t, jsontext.Pointer("/properties/outer/$defs/k"), at)

	_, at, ok = NewReader(root).TargetFrom("/properties/outer/not", "/$defs/k")
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/properties/outer/$defs/k"), at, "read from the position alone")
}

// TestTargetFrom_RestPath pins that a pointer deeper than the bare key
// navigates on past it once the owning schema is found: the definition itself
// is a container the rest of the pointer descends into.
func TestTargetFrom_RestPath(t *testing.T) {
	t.Parallel()
	const src = `
type: object
properties:
  outer:
    $defs:
      k:
        type: object
        properties:
          x: {type: string, format: uuid}
    properties:
      p: {$ref: "#/$defs/k/properties/x"}
`
	root := schemaFromYAML(t, src)
	outer := prop(t, root, "outer")
	p := prop(t, outer, "p")

	got, at, ok := NewReader(root).Target(p, "/$defs/k/properties/x")
	require.True(t, ok)
	require.NotNil(t, got.GetSchema())
	assert.Equal(t, "uuid", got.GetSchema().GetFormat())
	assert.Equal(t, jsontext.Pointer("/properties/outer/$defs/k/properties/x"), at)
}

// TestTargetFrom_NoMatch pins that an absent definition, with the loop running
// out of ancestors, reports ok=false rather than a stale or partial match.
func TestTargetFrom_NoMatch(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, "properties:\n  p: {type: string}\n")
	p := prop(t, root, "p")

	_, _, ok := NewReader(root).Target(p, "/$defs/missing")
	assert.False(t, ok)
}

// TestTarget_GuardClauses drives Target's own early returns directly, each
// isolated from the others: no document, no reference site, a pointer that is
// not $defs-shaped, and a reference site the document's tree does not contain
// (GetJSONPointer finds no position for it, so from is "").
func TestTarget_GuardClauses(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, "properties:\n  p: {type: string}\n")
	p := prop(t, root, "p")

	_, _, ok := NewReader(nil).Target(p, "/$defs/n")
	assert.False(t, ok, "no document to navigate")

	_, _, ok = NewReader(root).Target(nil, "/$defs/n")
	assert.False(t, ok, "no reference site")

	_, _, ok = NewReader(root).Target(p, "/properties/p")
	assert.False(t, ok, "not a $defs pointer")

	other := schemaFromYAML(t, "properties:\n  q: {type: string}\n")
	otherQ := prop(t, other, "q")
	_, _, ok = NewReader(root).Target(otherQ, "/$defs/n")
	assert.False(t, ok, "a schema this document's tree does not contain has no position to read the pointer from")
}

// TestTargetFrom_GuardClauses drives the two guards Target's own filtering
// normally shields TargetFrom from: it is called directly elsewhere
// (resolve.Scope.MappingPointer, for a discriminator mapping value), always with a
// pointer already known to be $defs-shaped, so only a direct call exercises
// these two branches.
func TestTargetFrom_GuardClauses(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, "$defs:\n  k: {type: string}\n")

	_, _, ok := NewReader(nil).TargetFrom("/properties/p", "/$defs/k")
	assert.False(t, ok, "no document to navigate")

	_, _, ok = NewReader(root).TargetFrom("/properties/p", "/properties/p")
	assert.False(t, ok, "not a $defs pointer")
}

// TestLocalDef drives tryResolveLocalDefs' own branches directly: a schema
// that is not a resource at all, a resource whose pointer carries a rest path
// (never handled locally, whatever the schema is), a resource with no $defs of
// its own, and a resource whose $defs does not carry the requested key.
func TestLocalDef(t *testing.T) {
	t.Parallel()

	t.Run("not a resource", func(t *testing.T) {
		t.Parallel()
		root := schemaFromYAML(t, "$defs:\n  n: {type: string}\nproperties:\n  p: {type: string}\n")
		p := prop(t, root, "p")
		_, ok := localDef(p, "/$defs/n")
		assert.False(t, ok, "p carries no $id and inherits no other base")
	})

	t.Run("a rest path is never local", func(t *testing.T) {
		t.Parallel()
		owner := schemaFromYAML(t, "$id: \"https://x.test/a\"\n$defs:\n  n: {type: object, properties: {x: {type: string}}}\n")
		_, ok := localDef(owner, "/$defs/n/properties/x")
		assert.False(t, ok, "tryResolveLocalDefs only ever reads the bare key, never a path beyond it")
	})

	t.Run("own resource but no $defs at all", func(t *testing.T) {
		t.Parallel()
		owner := schemaFromYAML(t, "$id: \"https://x.test/a\"\n")
		_, ok := localDef(owner, "/$defs/n")
		assert.False(t, ok)
	})

	t.Run("own resource, own $defs, key absent", func(t *testing.T) {
		t.Parallel()
		owner := schemaFromYAML(t, "$id: \"https://x.test/a\"\n$defs:\n  n: {type: string}\n")
		_, ok := localDef(owner, "/$defs/missing")
		assert.False(t, ok)
	})
}

// readerDoc nests definitions at several levels, so a position's answer can
// come from itself, any ancestor, or the document, and from none.
const readerDoc = `
$defs: {top: {type: string}}
properties:
  a:
    $defs: {k: {type: string}, top: {type: integer}}
    properties:
      b:
        $defs: {k: {type: object}}
        properties:
          c: {type: string}
          d: {$defs: {z: {type: string}}, properties: {e: {type: string}}}
  f: {properties: {g: {type: string}}}
`

// readerQueries is every (position, pointer) pair readerDoc is asked about.
func readerQueries() (queries [][2]jsontext.Pointer) {
	positions := []jsontext.Pointer{
		"/properties/a/properties/b/properties/c", "/properties/a/properties/b/properties/d/properties/e",
		"/properties/a/properties/b/properties/d", "/properties/a/properties/b", "/properties/a",
		"/properties/f/properties/g", "/properties/f", "/properties/missing/properties/x", "properties/a", "",
	}
	for _, pos := range positions {
		for _, ptr := range []jsontext.Pointer{"/$defs/k", "/$defs/top", "/$defs/z", "/$defs/missing"} {
			queries = append(queries, [2]jsontext.Pointer{pos, ptr})
		}
	}
	return queries
}

// TestReader_AnswersDoNotDependOnWhatItReadBefore pins that remembering is
// invisible: every query gets the answer a reader that has read nothing gives
// it, whether the queries arrive in one order or the reverse. A reader that let
// one reference's walk colour another's would be the resolver's own caching
// fault again (GitHub #557).
func TestReader_AnswersDoNotDependOnWhatItReadBefore(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, readerDoc)
	queries := readerQueries()
	type result struct {
		def *oas3.JSONSchema[oas3.Referenceable]
		at  jsontext.Pointer
		ok  bool
	}
	ask := func(r *Reader, q [2]jsontext.Pointer) result {
		def, at, ok := r.TargetFrom(q[0], q[1])
		return result{def, at, ok}
	}

	fresh := make([]result, 0, len(queries))
	for _, q := range queries {
		fresh = append(fresh, ask(NewReader(root), q))
	}
	shared := NewReader(root)
	for i, q := range queries {
		assert.Equal(t, fresh[i], ask(shared, q), "forward: %v", q)
	}
	reversed := NewReader(root)
	for i := len(queries) - 1; i >= 0; i-- {
		assert.Equal(t, fresh[i], ask(reversed, queries[i]), "reversed: %v", queries[i])
	}
	var found int
	for _, r := range fresh {
		if r.ok {
			found++
		}
	}
	assert.Positive(t, found, "some queries are answered")
	assert.Less(t, found, len(fresh), "and some are not")
}

// TestReader_NilReaderFindsNothing pins the boundary: a reader that is nil, or
// has no document, answers no question rather than faulting on one.
func TestReader_NilReaderFindsNothing(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, readerDoc)
	p := prop(t, root, "f")

	for name, r := range map[string]*Reader{"nil": nil, "no document": NewReader(nil)} {
		assert.Nil(t, r.Doc(), name)
		assert.Zero(t, r.Reads(), name)
		_, _, ok := r.Target(p, "/$defs/top")
		assert.False(t, ok, name)
		_, _, ok = r.TargetFrom("/properties/f", "/$defs/top")
		assert.False(t, ok, name)
	}
}

// TestReader_PlaceAtReadsEachPositionOnceFromItsParent pins what the walk down
// remembers: for each position it passes, the object, whether that object has a
// $defs of its own and the closest ancestor that does, so a second position
// below the same ancestors reads only what is new; and, for a position the
// document lacks, nothing below the first missing one.
func TestReader_PlaceAtReadsEachPositionOnceFromItsParent(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, readerDoc)
	r := NewReader(root)
	const a, b = "/properties/a", "/properties/a/properties/b"

	here := r.placeAt(b)
	require.True(t, here.held)
	assert.Same(t, prop(t, prop(t, root, "a"), "b"), here.value)
	assert.True(t, here.hasDefs, "b declares $defs")
	assert.Equal(t, jsontext.Pointer(a), here.holder, "a is the closest holder above b")
	assert.Len(t, r.places, 4, "/properties, /properties/a, .../properties, .../b")
	assert.Equal(t, jsontext.Pointer(""), r.places["/properties"].holder, "a map holds none, and the root is no holder")

	here = r.placeAt(b + "/properties/c")
	require.True(t, here.held)
	assert.False(t, here.hasDefs, "c declares none")
	assert.Equal(t, jsontext.Pointer(b), here.holder, "so its holder is b's")
	assert.Len(t, r.places, 6, "only the two positions below b are new")

	before := r.reads
	here = r.placeAt(a + "/properties/missing/properties/x")
	assert.False(t, here.held, "no object below a position the document lacks")
	assert.Equal(t, before+1, r.reads, "and none of them is read: the first missing one ends the walk")
	assert.False(t, r.placeAt("properties/a").held, "a position that is no pointer holds nothing")
	assert.Same(t, root, r.placeAt("").value, "the root is the document")
}

// TestDefinesDefs pins the filter the search rests on: an object has a $defs of
// its own when the library navigates to a value there, and the library reports
// an absent one as an empty value, not an error.
func TestDefinesDefs(t *testing.T) {
	t.Parallel()
	root := schemaFromYAML(t, readerDoc)
	a := prop(t, root, "a")

	assert.True(t, definesDefs(a), "a schema with $defs")
	assert.False(t, definesDefs(prop(t, root, "f")), "a schema without: a nil value, no error")
	assert.False(t, definesDefs(root.GetSchema().GetProperties()), "a map is no object with a $defs")
	assert.False(t, definesDefs(nil), "nothing is not one either")
	assert.True(t, definesDefs(struct {
		Defs string `key:"$defs"`
	}{Defs: "x"}), "a $defs that is a plain value is one, whatever it holds")
}

// deepChain is a schema whose property top holds the definitions m0 to
// m<definitions-1> and nests depth levels of property n below it, and the
// pointer to each level.
func deepChain(depth, definitions int) (body string, levels []jsontext.Pointer) {
	inner := "{type: string}"
	for range depth {
		inner = "{properties: {n: " + inner + "}}"
	}
	at := jsontext.Pointer("/properties/top")
	for range depth {
		at += "/properties/n"
		levels = append(levels, at)
	}
	defs := make([]string, 0, definitions)
	for i := range definitions {
		defs = append(defs, "m"+strconv.Itoa(i)+": {type: string}")
	}
	return "properties: {top: {$defs: {" + strings.Join(defs, ", ") + "}, properties: {n: " + inner + "}}}", levels
}

// TestReader_WorkAndMemoryGrowLinearlyWithDepth pins what remembering is for:
// asking for a definition from every level of a schema nested d deep reads each
// position once, not once per question, so the navigations it makes and the
// positions it keeps double when the depth does. It holds whether every level
// asks for the same definition or each its own: what is kept is a property of
// the positions, never of the pointers asked about. Asked without memory, each
// question walks the whole chain above it and the work grows with the square of
// the depth; kept per pointer, the memory would.
func TestReader_WorkAndMemoryGrowLinearlyWithDepth(t *testing.T) {
	t.Parallel()
	for name, distinct := range map[string]bool{"one definition asked for everywhere": false, "a definition each": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cost := func(depth int) (reads, kept int) {
				definitions := 1
				if distinct {
					definitions = depth
				}
				body, levels := deepChain(depth, definitions)
				r := NewReader(schemaFromYAML(t, body))
				for i, at := range levels {
					_, found, ok := r.TargetFrom(at, jsontext.Pointer("/$defs/m"+strconv.Itoa(i%definitions)))
					require.True(t, ok, at)
					require.Equal(t, jsontext.Pointer("/properties/top/$defs/m"+strconv.Itoa(i%definitions)), found)
				}
				return r.reads, len(r.places)
			}
			smallReads, smallKept := cost(100)
			largeReads, largeKept := cost(200)
			assert.LessOrEqual(t, smallReads, 8*100, "a constant number of navigations per level")
			assert.LessOrEqual(t, largeReads, 2*smallReads+10, "twice the depth, twice the work: %d then %d", smallReads, largeReads)
			assert.LessOrEqual(t, smallKept, 2*100+4, "a constant number of positions per level")
			assert.LessOrEqual(t, largeKept, 2*smallKept+4, "twice the depth, twice the memory: %d then %d", smallKept, largeKept)
		})
	}
}

// TestTarget_SchemaInsideAnIDResourceReadsItsOwnDefs pins ownResource's second
// alternative, which parsing a document makes true of every schema inside one
// with an $id: q has none of its own, but its base is p's, not the document's,
// so tryResolveLocalDefs reads q's own $defs before any ancestor's. Without the
// alternative the answer is A's n, the nearest ancestor holding it.
func TestTarget_SchemaInsideAnIDResourceReadsItsOwnDefs(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(`openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A:
      $defs: {n: {type: integer}}
      properties:
        p:
          $id: https://x.test/p
          properties:
            q:
              $ref: "#/$defs/n"
              $defs: {n: {type: object}}
`))
	require.NoError(t, err)
	const site = "/components/schemas/A/properties/p/properties/q"
	target, err := jsonpointer.GetTarget(doc, jsonpointer.JSONPointer(site), jsonpointer.WithStructTags("key"))
	require.NoError(t, err)
	q, ok := target.(*oas3.JSONSchema[oas3.Referenceable])
	require.True(t, ok)

	got, at, ok := NewReader(doc).Target(q, "/$defs/n")
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer(site+"/$defs/n"), at, "q's own definition, not A's")
	assert.Same(t, defEntry(t, q, "n"), got)
}

func TestOwnResource(t *testing.T) {
	t.Parallel()
	withID := schemaFromYAML(t, "$id: \"https://x.test/a\"\n").GetSchema()
	assert.True(t, ownResource(withID))

	plain := schemaFromYAML(t, "type: string\n").GetSchema()
	assert.False(t, ownResource(plain), "no $id and no registered base on a freshly parsed schema")
}

// TestParentPointer pins getParentJSONPointer's own bound: everything before
// the last '/', and "" once there is no '/' left to strip — the root itself is
// never an answer.
func TestParentPointer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   jsontext.Pointer
		want jsontext.Pointer
	}{
		{"empty has no ancestor", "", ""},
		{"a single segment has no ancestor", "/a", ""},
		{"no leading slash has no ancestor", "a", ""},
		{"two segments strips the last", "/a/b", "/a"},
		{"three segments strips one", "/a/b/c", "/a/b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, parentPointer(tc.in))
		})
	}
}
