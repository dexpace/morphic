package load

import (
	"encoding/json/jsontext"
	"errors"
	"iter"
	"maps"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// decodeRoot decodes src, which must decode.
func decodeRoot(t *testing.T, src string) *yaml.Node {
	t.Helper()
	parsed, err := Decode([]byte(src))
	require.NoError(t, err)
	return parsed.Root()
}

// refValues returns the value of every scalar written as the value of a $ref
// key in the tree under root, aliases not followed, in document order.
func refValues(root *yaml.Node) []string {
	var out []string
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, child := range slices.Backward(n.Content) {
			stack = append(stack, child)
		}
		for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
			if v := n.Content[i+1]; n.Content[i].Value == "$ref" && v.Kind == yaml.ScalarNode {
				out = append(out, v.Value)
			}
		}
	}
	return out
}

// assertSameTree requires got to be want node for node: kind, tag, value, style,
// position, how many children, and where an alias points.
func assertSameTree(t *testing.T, want, got *yaml.Node) {
	t.Helper()
	type pair struct{ want, got *yaml.Node }
	stack := []pair{{want, got}}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		require.Equal(t, p.want.Kind, p.got.Kind)
		require.Equal(t, p.want.Tag, p.got.Tag)
		require.Equal(t, p.want.Value, p.got.Value, "the value at %d:%d", p.want.Line, p.want.Column)
		require.Equal(t, p.want.Style, p.got.Style)
		require.Equal(t, p.want.Line, p.got.Line)
		require.Equal(t, p.want.Column, p.got.Column)
		require.Len(t, p.got.Content, len(p.want.Content))
		require.Equal(t, p.want.Alias == nil, p.got.Alias == nil)
		if p.want.Alias != nil {
			require.Equal(t, p.want.Alias.Line, p.got.Alias.Line, "an alias points where it did")
			require.Equal(t, p.want.Alias.Column, p.got.Alias.Column)
		}
		for i := range p.want.Content {
			stack = append(stack, pair{p.want.Content[i], p.got.Content[i]})
		}
	}
}

// resolverReads is the pointer the library's resolver reads from ref's
// fragment, the decode it resolves an internal reference from.
func resolverReads(ref string) string {
	return string(references.Reference(ref).GetJSONPointer())
}

// fragmentNames is the pointer ref's fragment names as RFC 3986 reads it:
// percent escapes decoded and nothing else, so a '+' names itself. A fragment
// with an escape that cannot be decoded names itself. A ref here spells at
// most one '#', and no whitespace around it.
func fragmentNames(ref string) string {
	_, fragment, _ := strings.Cut(ref, "#")
	if pointer, err := url.PathUnescape(fragment); err == nil {
		return pointer
	}
	return fragment
}

// TestEscapes_RewritesOnlyWhatTheResolverMisreads holds escape to the library's
// reading of each $ref value a row writes. The resolver's decode of a value is
// compared with the pointer the source names, and a row's count of rewrites
// must be the number of values it reads otherwise. escape must then rewrite
// those and no others, and the resolver must read from each value that
// pointer. Which positions hold a $ref value is pinned by the rows alone.
func TestEscapes_RewritesOnlyWhatTheResolverMisreads(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		src      string
		want     []string
		recorded int
	}{
		{"a plus in a fragment", "{$ref: '#/a+b'}", []string{"#/a%2Bb"}, 1},
		{"another document's fragment", "{$ref: 'o.yaml#/a+b'}", []string{"o.yaml#/a%2Bb"}, 1},
		{"nothing to rewrite", "{$ref: '#/a b'}", []string{"#/a b"}, 0},
		{"a plus in the document part", "{$ref: 'a+b.yaml#/c'}", []string{"a+b.yaml#/c"}, 0},
		{"an escaped plus", "{$ref: '#/a%2Bb'}", []string{"#/a%2Bb"}, 0},
		{"a fragment the resolver cannot decode", "{$ref: '#/a+b%zz'}", []string{"#/a+b%zz"}, 0},
		{"a mapping where a value is expected", "{$ref: {a: '#/a+b'}}", nil, 0},
		{"a sequence where a value is expected", "{$ref: ['#/a+b']}", nil, 0},
		{"a plus in a key", "{'#/a+b': {$ref: '#/c'}}", []string{"#/c"}, 0},
		{"a sequence of strings spelling $ref", "['$ref', '#/a+b']", nil, 0},
		{"a plus in some other member", "{description: '#/a+b', $ref: '#/c'}", []string{"#/c"}, 0},
		{"under an extension", "{x-e: {$ref: '#/a+b'}}", []string{"#/a%2Bb"}, 1},
		{"inside an example", "{example: {$ref: '#/a+b'}, examples: [{$ref: '#/c+d'}]}", []string{"#/a%2Bb", "#/c%2Bd"}, 2},
		{"in a sequence of mappings", "[{$ref: '#/a+b'}, {$ref: '#/c+d'}]", []string{"#/a%2Bb", "#/c%2Bd"}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := decodeRoot(t, tc.src)
			written := refValues(root)
			misread := 0
			for _, ref := range written {
				if resolverReads(ref) != fragmentNames(ref) {
					misread++
				}
			}
			require.Equal(t, tc.recorded, misread, "the values the resolver misreads in %q", written)
			esc := newEscapes()

			esc.escape(root)

			got := refValues(root)
			require.Len(t, got, len(written), "escape rewrites values and adds none")
			assert.Equal(t, tc.want, got)
			assert.Len(t, esc.written, misread, "exactly what the resolver misreads is recorded")
			for i, ref := range written {
				assert.Equal(t, fragmentNames(ref), resolverReads(got[i]), "the resolver reads what the source names from %q", got[i])
			}
		})
	}
}

func TestEscapes_LeavesAnAnchoredValueAlone(t *testing.T) {
	t.Parallel()
	const src = "a: &r '#/x+y'\nb: {$ref: *r}\nc: {$ref: *r}\n"
	root := decodeRoot(t, src)
	anchored := root.Content[0].Content[1]
	aliases := []*yaml.Node{root.Content[0].Content[3].Content[1], root.Content[0].Content[5].Content[1]}
	at := [][2]int{{aliases[0].Line, aliases[0].Column}, {aliases[1].Line, aliases[1].Column}}
	require.NotEqual(t, [2]int{anchored.Line, anchored.Column}, at[0])
	esc := newEscapes()

	esc.escape(root)

	assert.Equal(t, "#/x+y", anchored.Value, "the scalar the aliases name is not changed")
	assert.Equal(t, []string{"#/x%2By", "#/x%2By"}, refValues(root), "each alias is a scalar of its own")
	for i, alias := range aliases {
		assert.Nil(t, alias.Alias)
		assert.Equal(t, at[i], [2]int{alias.Line, alias.Column}, "at its own position")
	}
	assert.Len(t, esc.written, 2)
	esc.restoreTrees()
	assertSameTree(t, decodeRoot(t, src), root)
	assert.Same(t, anchored, aliases[0].Alias, "and an alias again")
}

func TestEscapes_KeepsTextAnAliasSharesWithARefAsWritten(t *testing.T) {
	t.Parallel()
	const src = "b: {$ref: &r '#/x+y'}\nc: {description: *r, *r : k}\n"
	root := decodeRoot(t, src)
	anchored := root.Content[0].Content[1].Content[1]
	text := root.Content[0].Content[3].Content[1]
	key := root.Content[0].Content[3].Content[2]
	esc := newEscapes()

	esc.escape(root)

	assert.Equal(t, "#/x%2By", anchored.Value, "the $ref is respelled")
	for _, n := range []*yaml.Node{text, key} {
		assert.Equal(t, yaml.ScalarNode, n.Kind, "an alias of it is a scalar of its own")
		assert.Equal(t, "#/x+y", n.Value, "holding it as written")
		assert.Equal(t, 2, n.Line)
	}
	assert.Equal(t, "r", anchored.Anchor, "and the anchor stays on the one node")
	assert.Empty(t, text.Anchor)
	assert.Equal(t, map[*yaml.Node]string{anchored: "#/x+y"}, esc.written, "only the $ref is a rewritten value")
	esc.restoreTrees()
	assertSameTree(t, decodeRoot(t, src), root)
	assert.Same(t, anchored, text.Alias)
}

func TestEscapes_RewritesAValueAMergeKeySupplies(t *testing.T) {
	t.Parallel()
	const src = "base: &m {$ref: '#/x+y'}\nuse: {<<: *m, other: 1}\n"
	root := decodeRoot(t, src)
	esc := newEscapes()

	esc.escape(root)

	assert.Equal(t, []string{"#/x%2By"}, refValues(root))
	assert.Len(t, esc.written, 1, "the merged mapping is the anchored one, so its scalar is rewritten once")
}

// TestEscapes_ReadsAKeyAsTheParserReadsIt pins that a key written as an alias
// is the key it stands for. The parser is asked first: it reads an object whose
// aliased key stands for $ref as a reference, and one whose aliased key stands
// for another keyword as that keyword's object. escape must then rewrite the
// value of the first and leave the second's alone.
func TestEscapes_ReadsAKeyAsTheParserReadsIt(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
x-ref: &ref $ref
x-description: &description description
components:
  parameters:
    Aliased: {*ref : '#/paths/~1a+b'}
    Other: {name: q, in: query, *description : '#/paths/~1a+b', schema: {type: string}}
`
	parsed, _, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
	require.NoError(t, err)
	read := maps.Collect(parsed.Components.GetParameters().All())
	require.True(t, read["Aliased"].IsReference(), "the parser reads an alias standing for $ref as $ref")
	require.False(t, read["Other"].IsReference(), "and one standing for another keyword as that keyword")
	require.Equal(t, references.Reference("#/paths/~1a+b"), read["Aliased"].GetReference())
	require.Equal(t, "#/paths/~1a+b", read["Other"].GetObject().GetDescription())

	escaped, esc := escapedParameters(t, src)

	assert.Equal(t, references.Reference("#/paths/~1a%2Bb"), escaped["Aliased"].GetReference(), "the $ref is rewritten")
	assert.Equal(t, "#/paths/~1a+b", escaped["Other"].GetObject().GetDescription(), "the description is not")
	assert.Len(t, esc.written, 1)
}

func TestEscapes_RestoreTreesPutsTheSourceBack(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
a: &r '#/x+y'
b: {$ref: *r}
c: {$ref: '#/p+q', x-e: {$ref: 'o.yaml#/r+s'}}
d: [{$ref: '#/t+u'}]
`
	root := decodeRoot(t, src)
	esc := newEscapes()
	esc.escape(root)
	require.NotEmpty(t, esc.written)
	require.NotEqual(t, refValues(decodeRoot(t, src)), refValues(root), "the tree was rewritten")

	esc.restoreTrees()

	assertSameTree(t, decodeRoot(t, src), root)
	assert.Empty(t, esc.written, "and forgotten")
	esc.restoreTrees()
	assertSameTree(t, decodeRoot(t, src), root)
}

func TestEscapes_LeavesATreeWithNoCandidateAlone(t *testing.T) {
	t.Parallel()
	const src = "a: {$ref: '#/b'}\nc: '+'\nd: '#'\n"
	root := decodeRoot(t, src)
	esc := newEscapes()

	esc.escape(root)

	assert.Empty(t, esc.written)
	assertSameTree(t, decodeRoot(t, src), root)
}

func TestEscapes_NilRegistryDoesNothing(t *testing.T) {
	t.Parallel()
	var esc *escapes
	root := decodeRoot(t, "a: {$ref: '#/a+b'}\n")
	node := root.Content[0].Content[1].Content[1]

	esc.escape(root)
	esc.restoreTrees()
	site, err := esc.restore(t.Context(), nil, Siblings{}, MappingTargets{})
	_, was := esc.original(node)

	assert.Equal(t, []string{"#/a+b"}, refValues(root), "nothing is rewritten")
	require.NoError(t, err)
	assert.Empty(t, site)
	assert.False(t, was)
	built := soa.NewReferencedParameterFromRef("#/a+b")
	assert.Equal(t, references.Reference("#/a+b"), esc.writtenOf(built), "a model answers with its own")
	assert.Equal(t, trail{stopped: "#/a+b"}, esc.trailOf(chain{stopped: "#/a+b", stoppedAt: built}))
	text := []ir.Diagnostic{{Message: "unable to resolve reference: #/a%2Bb"}}
	assert.Equal(t, text, esc.asWritten(slices.Clone(text)))
}

// escapedMessages returns each message in turn as asWritten renders it, once
// escape has rewritten src.
func escapedMessages(t *testing.T, src string, messages ...string) []string {
	t.Helper()
	esc := newEscapes()
	esc.escape(decodeRoot(t, src))
	diags := make([]ir.Diagnostic, 0, len(messages))
	for _, m := range messages {
		diags = append(diags, ir.Diagnostic{Message: m})
	}
	out := make([]string, 0, len(messages))
	for _, d := range esc.asWritten(diags) {
		out = append(out, d.Message)
	}
	return out
}

func TestEscapes_AsWrittenPutsASpellingBackWhereItEnds(t *testing.T) {
	t.Parallel()
	got := escapedMessages(t, "[{$ref: '#/a+b'}, {$ref: '#/c+d&<e>'}]",
		"unable to resolve reference: #/a%2Bb",
		`got (value: {"Ref":"#/a%2Bb","Type":null})`,
		`got (value: {"Ref":"#/c%2Bd\u0026\u003ce\u003e"})`,
		"reference: #/c%2Bd&<e>",
		`unresolved $ref "#/a%2Bb/f": not found`,
		"no reference here",
	)

	assert.Equal(t, []string{
		"unable to resolve reference: #/a+b",
		`got (value: {"Ref":"#/a+b","Type":null})`,
		`got (value: {"Ref":"#/c+d\u0026\u003ce\u003e"})`,
		"reference: #/c+d&<e>",
		`unresolved $ref "#/a%2Bb/f": not found`,
		"no reference here",
	}, got, "at the end of a message or a quoted string, as text and as JSON writes it, never inside a longer one")
}

func TestEscapes_AsWrittenKeepsASpellingTheSourceWrites(t *testing.T) {
	t.Parallel()
	got := escapedMessages(t, "[{$ref: '#/a+b'}, {$ref: '#/a%2Bb'}, {$ref: '#/c+d'}]",
		`unresolved $ref "#/a%2Bb": unable to resolve reference: #/a%2Bb`,
		"unable to resolve reference: #/c%2Bd",
	)

	assert.Equal(t, []string{
		`unresolved $ref "#/a%2Bb": unable to resolve reference: #/a%2Bb`,
		"unable to resolve reference: #/c+d",
	}, got)
}

func TestEscapes_AsWrittenChoosesOneSpellingWhereSeveralRespellAlike(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"[{$ref: '#/a+%2Bb'}, {$ref: '#/a%2B+b'}]",
		"[{$ref: '#/a%2B+b'}, {$ref: '#/a+%2Bb'}]",
	} {
		got := escapedMessages(t, src, "reference: #/a%2B%2Bb")

		assert.Equal(t, []string{"reference: #/a%2B+b"}, got, "the least, whichever comes first: %s", src)
	}
}

func TestJSONQuoted_ReportsOnlyAnEscapedSpelling(t *testing.T) {
	t.Parallel()
	s, w, ok := jsonQuoted("#/a%2B&", "#/a+&")
	assert.True(t, ok)
	assert.Equal(t, [2]string{`#/a%2B\u0026`, `#/a+\u0026`}, [2]string{s, w})

	_, _, ok = jsonQuoted("#/a%2Bb", "#/a+b")
	assert.False(t, ok, "written alike raw")
	_, _, ok = jsonQuoted("#/a%2B\xff", "#/a+\xff")
	assert.False(t, ok, "not UTF-8, so in no encoded value")
	_, _, ok = jsonQuoted("#/a%2B&", "#/a+\xff")
	assert.False(t, ok)
}

// escapedDoc builds the model of src from a tree rewritten as load rewrites it,
// and returns it with the registry that rewrote the tree.
func escapedDoc(t *testing.T, src string) (*soa.OpenAPI, *escapes) {
	t.Helper()
	root := decodeRoot(t, src)
	releaseAnchors(root)
	esc := newEscapes()
	esc.escape(root)
	doc, _, err := unmarshal(t.Context(), []byte(src), root)
	require.NoError(t, err)
	return doc, esc
}

// escapedParameters is escapedDoc for a document whose components hold
// parameters, which it returns by name.
func escapedParameters(t *testing.T, src string) (map[string]*soa.ReferencedParameter, *escapes) {
	t.Helper()
	doc, esc := escapedDoc(t, src)
	out := maps.Collect(doc.Components.GetParameters().All())
	require.NotEmpty(t, out)
	return out, esc
}

func TestEscapes_WrittenOfNamesTheScalarAModelWasBuiltFrom(t *testing.T) {
	t.Parallel()
	models, esc := escapedParameters(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
x-a: &a '#/paths/~1a+b'
components:
  parameters:
    P: {name: p, in: query, schema: {type: string}}
    Rewritten: {$ref: '#/paths/~1a+b'}
    Alone: {$ref: '#/components/parameters/P'}
    Aliased: {$ref: *a}
`)

	assert.Equal(t, references.Reference("#/paths/~1a%2Bb"), models["Rewritten"].GetReference(), "the model holds the rewritten spelling")
	assert.Equal(t, references.Reference("#/paths/~1a+b"), esc.writtenOf(models["Rewritten"]), "and the source's is answered")
	assert.Equal(t, references.Reference("#/components/parameters/P"), esc.writtenOf(models["Alone"]))
	assert.Equal(t, references.Reference("#/paths/~1a+b"), esc.writtenOf(models["Aliased"]), "the scalar an alias names")
	assert.Equal(t, references.Reference("#/paths/~1a%2Bb"), models["Aliased"].GetReference())
	assert.Empty(t, esc.writtenOf(models["P"]), "an inline parameter has no $ref")
	assert.Empty(t, esc.writtenOf(nil))
	assert.Empty(t, esc.writtenOf(&yaml.Node{}), "nor has anything that is no reference")
}

func TestEscapes_TrailOfQuotesTheReferenceTheChainStoppedAt(t *testing.T) {
	t.Parallel()
	models, esc := escapedParameters(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  parameters:
    Rewritten: {$ref: '#/paths/~1a+b'}
`)
	stopped := models["Rewritten"]
	c := chain{stopped: stopped.GetReference(), stoppedAt: stopped}

	assert.Equal(t, references.Reference("#/paths/~1a%2Bb"), c.trail().stopped, "the chain's own is what the resolver read")
	assert.Equal(t, references.Reference("#/paths/~1a+b"), esc.trailOf(c).stopped)
	assert.Empty(t, esc.trailOf(chain{}).stopped, "a chain that stopped nowhere names none")
}

// coreRefOf returns the core's copy of a reference model's $ref.
func coreRefOf(t *testing.T, model any) *string {
	t.Helper()
	switch r := model.(type) {
	case *schemaRef:
		return r.GetSchema().GetCore().Ref.Value
	case *soa.ReferencedPathItem:
		return r.GetCore().Reference.Value
	case *soa.ReferencedParameter:
		return r.GetCore().Reference.Value
	case *soa.ReferencedHeader:
		return r.GetCore().Reference.Value
	case *soa.ReferencedRequestBody:
		return r.GetCore().Reference.Value
	case *soa.ReferencedResponse:
		return r.GetCore().Reference.Value
	case *soa.ReferencedExample:
		return r.GetCore().Reference.Value
	case *soa.ReferencedLink:
		return r.GetCore().Reference.Value
	case *soa.ReferencedCallback:
		return r.GetCore().Reference.Value
	case *soa.ReferencedSecurityScheme:
		return r.GetCore().Reference.Value
	default:
		require.Failf(t, "not a reference kind", "%T", model)
		return nil
	}
}

func TestRefCopies_NamesTheScalarAndPutsAValueInBothCopies(t *testing.T) {
	t.Parallel()
	doc, _ := buildDoc(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /pi: {$ref: '#/components/pathItems/PI'}
  /q:
    get:
      operationId: q
      parameters: [{$ref: '#/components/parameters/P'}]
      requestBody: {$ref: '#/components/requestBodies/RB'}
      responses:
        "200": {$ref: '#/components/responses/R'}
      callbacks:
        cb: {$ref: '#/components/callbacks/CB'}
components:
  pathItems: {PI: {}}
  parameters: {P: {name: p, in: query, schema: {$ref: '#/components/schemas/S'}}}
  requestBodies: {RB: {content: {application/json: {schema: {type: string}}}}}
  responses:
    R:
      description: ok
      headers: {H: {$ref: '#/components/headers/HH'}}
      links: {L: {$ref: '#/components/links/LL'}}
      content: {application/json: {examples: {E: {$ref: '#/components/examples/EE'}}}}
  headers: {HH: {schema: {type: string}}}
  links: {LL: {operationId: q}}
  examples: {EE: {value: 1}}
  callbacks: {CB: {}}
  securitySchemes:
    SS: {$ref: '#/components/securitySchemes/Basic'}
    Basic: {type: http, scheme: basic}
  schemas: {S: {type: string}}
`)
	kinds := map[reflect.Type]bool{}
	for item := range soa.Walk(t.Context(), doc) {
		require.NoError(t, item.Match(soa.Matcher{Any: func(model any) error {
			node, put, ok := refCopies(model)
			if !ok {
				return nil
			}
			kinds[reflect.TypeOf(model)] = true
			own, _ := model.(interface{ GetReference() references.Reference })
			require.NotNil(t, own, "%T", model)
			assert.Equal(t, string(own.GetReference()), node.Value, "%T: the scalar the reference was read from", model)

			put("#/put")

			assert.Equal(t, references.Reference("#/put"), own.GetReference(), "%T", model)
			assert.Equal(t, "#/put", *coreRefOf(t, model), "%T's core", model)
			return nil
		}}))
	}
	assert.Equal(t, libraryReferenceKinds(t), kinds, "every kind of reference the library has")
}

func TestRefCopies_RefusesWhatItCannotRestore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		model any
	}{
		{"nil", nil},
		{"not a reference kind", &soa.Parameter{}},
		{"a typed nil", (*soa.ReferencedParameter)(nil)},
		{"a typed nil schema", (*schemaRef)(nil)},
		{"built in code, from no scalar", soa.NewReferencedParameterFromRef("#/x")},
		{"a boolean schema", oas3.NewJSONSchemaFromBool(true)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node, put, ok := refCopies(tc.model)
			assert.False(t, ok)
			assert.Nil(t, node)
			assert.Nil(t, put)
		})
	}
}

func TestRestorer_AddVisitsAModelOnceAndOnlyAReference(t *testing.T) {
	t.Parallel()
	models, esc := escapedParameters(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  parameters:
    P: {$ref: '#/paths/~1a+b'}
`)
	r := &restorer{ctx: t.Context(), esc: esc, seen: map[any]bool{}, walked: map[any]bool{}}

	assert.True(t, r.add(models["P"]))
	assert.False(t, r.add(models["P"]), "a model is visited once")
	assert.False(t, r.add(&soa.Parameter{}), "and nothing but a reference is")
	assert.Equal(t, references.Reference("#/paths/~1a+b"), models["P"].GetReference())
}

func TestEscapes_RestoreFromWalksNothingWhenNothingWasRewritten(t *testing.T) {
	t.Parallel()
	panicking := iter.Seq[soa.WalkItem](func(func(soa.WalkItem) bool) { panic("walked") })

	site, err := newEscapes().restoreFrom(t.Context(), panicking, MappingTargets{})

	require.NoError(t, err)
	assert.Empty(t, site)
}

func TestEscapes_RestoreFromReportsAPanickingWalkAndStillRestoresTheTrees(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  parameters:
    P: {$ref: '#/paths/~1a+b'}
`
	root := decodeRoot(t, src)
	esc := newEscapes()
	esc.escape(root)
	require.Equal(t, []string{"#/paths/~1a%2Bb"}, refValues(root))
	panicking := iter.Seq[soa.WalkItem](func(func(soa.WalkItem) bool) { panic("walk fault") })

	site, err := esc.restoreFrom(t.Context(), panicking, MappingTargets{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "restoring $refs as written panicked")
	assert.Contains(t, err.Error(), "walk fault")
	assert.Empty(t, site, "the walk faulted before it reached a model")
	assert.Equal(t, []string{"#/paths/~1a+b"}, refValues(root), "the trees are put back whatever happened")
	assert.Empty(t, esc.written)
}

// TestEscapes_RestoreFromReachesTheModelsTheMappingTargetsHold pins what restore
// reaches past the walk of the source, which shows none of it: the schemas the
// targets hold by pointer and as built, and the references they resolved. Each
// is held with no chain behind it, so only reading the targets reaches the $ref
// inside. The schema left out is the control that nothing else restores it.
func TestEscapes_RestoreFromReachesTheModelsTheMappingTargetsHold(t *testing.T) {
	t.Parallel()
	doc, esc := escapedDoc(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    ByPointer: {type: object, properties: {p: {$ref: 'o.yaml#/a+b'}}}
    Built: {type: object, properties: {p: {$ref: 'o.yaml#/a+b'}}}
    Resolved: {type: object, properties: {p: {$ref: 'o.yaml#/a+b'}}}
    Left: {type: object, properties: {p: {$ref: 'o.yaml#/a+b'}}}
`)
	schema := func(name string) *schemaRef {
		js, ok := doc.Components.GetSchemas().Get(name)
		require.True(t, ok)
		return js
	}
	property := func(name string) *schemaRef {
		p, ok := schema(name).GetSchema().GetProperties().Get("p")
		require.True(t, ok)
		return p
	}
	targets := MappingTargets{
		byPointer: map[jsontext.Pointer]*schemaRef{"/x": schema("ByPointer")},
		built:     map[jsontext.Pointer]*schemaRef{"/y": schema("Built")},
		resolved:  []siteRef{{site: "/z", ref: property("Resolved")}},
	}
	nothing := iter.Seq[soa.WalkItem](func(func(soa.WalkItem) bool) {})
	for _, name := range []string{"ByPointer", "Built", "Resolved", "Left"} {
		require.Equal(t, references.Reference("o.yaml#/a%2Bb"), property(name).GetRef(), name)
	}

	_, err := esc.restoreFrom(t.Context(), nothing, targets)

	require.NoError(t, err)
	assert.Equal(t, references.Reference("o.yaml#/a+b"), property("ByPointer").GetRef(), "a schema held by pointer")
	assert.Equal(t, references.Reference("o.yaml#/a+b"), property("Built").GetRef(), "a schema held as built")
	assert.Equal(t, references.Reference("o.yaml#/a+b"), property("Resolved").GetRef(), "a reference resolved")
	assert.Equal(t, references.Reference("o.yaml#/a%2Bb"), property("Left").GetRef(), "and none that is held by nothing")
}

func TestRestoreFault_ReportsTheFaultWhereItEnded(t *testing.T) {
	t.Parallel()
	at := pointerAt(3, overlay.Origin{})

	assert.Empty(t, restoreFault(at, "/a", nil), "no fault, nothing to report")
	got := restoreFault(at, "/a", errors.New("restoring $refs as written panicked: boom"))

	require.Len(t, got, 1)
	assert.Equal(t, diag.InternalInvariant, got[0].Code)
	assert.Equal(t, ir.SeverityError, got[0].Severity)
	assert.Equal(t, ir.Provenance{Source: 3, Pointer: "/a"}, got[0].Provenance)
	assert.Equal(t, "internal: restoring $refs as written panicked: boom", got[0].Message)
}

func TestRestorer_FinishReportsAWalkOfAnObjectThatFaults(t *testing.T) {
	t.Parallel()
	// A hop whose object has no walk, which no resolution records: reading it
	// faults, and the barrier turns that into the error.
	hop := &soa.ReferencedParameter{}
	hop.GetCore().Reference.ValueNode = &yaml.Node{Kind: yaml.ScalarNode, Value: "#/x"}
	r := &restorer{ctx: t.Context(), esc: newEscapes(), seen: map[any]bool{}, walked: map[any]bool{},
		queue: []record{{object: hop, reached: true}}}

	err := r.finish(MappingTargets{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "restoring $refs as written panicked")
}
