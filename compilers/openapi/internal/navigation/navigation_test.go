package navigation_test

import (
	"encoding/json/jsontext"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
)

// navDoc is a document whose reads leave the model at every kind of object the
// library dispatches on: the root, whose extension a `<<` decoy holds without
// the key below it and a second merge holds with it; a value-held info; paths,
// a path item, responses and callbacks, each embedding a map; a security
// requirement embedding one by value; a resolved reference; a schema with its
// own merge; raw YAML held by a field; and a key written twice.
const navDoc = `openapi: 3.1.0
info: {title: T, version: "1", x-info: {k: v}}
paths:
  /a:
    get:
      responses:
        "200": {$ref: '#/components/responses/R'}
        default: {description: ok, x-resp: {a: 1}}
        x-codes: {b: 2}
      callbacks:
        cb:
          '{$request.body#/url}': {post: {responses: {"200": {description: c}}}}
          x-cb: {c: 3}
      security: [{api_key: [], x-scope: [s]}]
    x-path: {p: 1}
  x-paths: {q: 1}
components:
  responses:
    R: {description: r, x-r: {deep: {k: 1}}}
  schemas:
    S:
      type: object
      properties: {p: {type: string, default: {d: 1}, enum: [{e: 1}]}}
      x-ext: {k: {type: object}}
      x-own: &own {m: 0}
      <<: {x-own: {n: 1}}
x-decoy: &decoy {x-lib: {k: 0}}
x-hit: &hit {x-lib: {D0: {type: object}, D1: {$ref: '#/components/schemas/S'}}}
<<: *decoy
<<: *hit
x-dup: {a: 1}
x-dup: {a: 2}
x-alias: *own
`

// navPointers are the pointers TestWalk_AnswersAsTheWholeReadDoes reads, with
// each prefix and each with a stray token appended.
var navPointers = []jsontext.Pointer{
	"/x-lib/D0/type", "/x-lib/D1/$ref", "/x-lib/k", "/x-decoy/x-lib/k", "/x-dup/a", "/x-alias/m",
	"/info/x-info/k", "/info/title",
	"/paths/~1a/get/responses/default/x-resp/a", "/paths/~1a/get/responses/200/x-r/deep/k",
	"/paths/~1a/get/responses/200/description", "/paths/~1a/get/responses/x-codes/b",
	"/paths/~1a/get/callbacks/cb/{$request.body#~1url}/post/responses/200/description",
	"/paths/~1a/get/callbacks/cb/x-cb/c",
	"/paths/~1a/get/security/0/api_key", "/paths/~1a/get/security/0/x-scope/0",
	"/paths/~1a/x-path/p", "/paths/x-paths/q",
	"/components/schemas/S/properties/p/default/d", "/components/schemas/S/properties/p/enum/0/e",
	"/components/schemas/S/x-ext/k/type", "/components/schemas/S/x-own/n", "/components/schemas/S/x-own/m",
	"/components/responses/R/x-r/deep",
	"/", "",
}

// strays are the tokens appended to each pointer: one naming nothing, an index,
// the empty key, an escape, a field's own key, and a decoded slash.
var strays = []jsontext.Pointer{"", "/zz", "/0", "/", "/~1", "/type", "/a~1b"}

// parsed returns doc parsed and its references resolved by the library.
func parsed(t *testing.T, doc string) *soa.OpenAPI {
	t.Helper()
	model, _, err := soa.Unmarshal(t.Context(), strings.NewReader(doc))
	require.NoError(t, err)
	findings, err := model.ResolveAllReferences(t.Context(), soa.ResolveAllOptions{
		OpenAPILocation: "spec.yaml", DisableExternalRefs: true,
	})
	require.NoError(t, err)
	require.Empty(t, findings)
	return model
}

// whole is the library's read of pointer in doc.
func whole(doc any, pointer jsontext.Pointer) (any, error) {
	return jsonpointer.GetTarget(doc, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
}

// walked is Walk's read of pointer in doc, and the library's read of the tokens
// it leaves in the raw YAML it stops at.
func walked(doc any, pointer jsontext.Pointer) (any, error) {
	tokens, ok := navigation.Tokens(pointer)
	if !ok {
		return nil, jsonpointer.ErrValidation
	}
	at, rest, err := navigation.Walk(doc, tokens)
	if err != nil || len(rest) == 0 {
		return at, err
	}
	return whole(at, jsontext.Pointer(jsonpointer.PartsToJSONPointer(rest)))
}

// settled reports whether err is one the library returns for a pointer that
// names nothing, as against one it cannot read through yet.
func settled(err error) bool {
	return errors.Is(err, jsonpointer.ErrNotFound) || errors.Is(err, jsonpointer.ErrInvalidPath) ||
		errors.Is(err, jsonpointer.ErrValidation)
}

// requireSameTarget fails unless walk found what the whole read found: the
// same pointer or value, or, where the whole read hands a model held by value
// back as the value and Walk as a pointer to a copy, one built from the same
// node.
func requireSameTarget(t *testing.T, want, got any, pointer jsontext.Pointer) {
	t.Helper()
	w, g := reflect.ValueOf(want), reflect.ValueOf(got)
	switch {
	case w.Kind() == reflect.Struct && g.Kind() == reflect.Pointer && g.Type().Elem() == w.Type():
		boxed := reflect.New(w.Type())
		boxed.Elem().Set(w)
		require.Same(t, rootOf(t, boxed.Interface()), rootOf(t, got), "%q", pointer)
	case w.Kind() == reflect.Pointer:
		require.Equal(t, w.Type(), g.Type(), "%q", pointer)
		require.Equal(t, w.Pointer(), g.Pointer(), "%q", pointer)
	default:
		require.Equal(t, want, got, "%q", pointer)
	}
}

// rootOf returns the node the model v was built from.
func rootOf(t *testing.T, v any) *yaml.Node {
	t.Helper()
	built, ok := v.(interface{ GetRootNode() *yaml.Node })
	require.True(t, ok, "%T is built from no node", v)
	return built.GetRootNode()
}

// TestWalk_AnswersAsTheWholeReadDoes holds Walk, followed by the library's read
// of what it leaves in raw YAML, to the library's read of the whole pointer, for
// every pointer of navPointers, each prefix, and each with a stray token. A
// Leaves that sent a field's token to raw YAML would answer the raw node; one
// that kept a raw token in the model would miss the `<<` the library reaches
// past a decoy holding the token alone.
func TestWalk_AnswersAsTheWholeReadDoes(t *testing.T) {
	t.Parallel()
	doc := parsed(t, navDoc)
	reads := 0
	for _, base := range navPointers {
		for prefix := range prefixesOf(base) {
			for _, stray := range strays {
				pointer := prefix + stray
				want, wantErr := whole(doc, pointer)
				got, gotErr := walked(doc, pointer)
				if wantErr != nil {
					require.Error(t, gotErr, "%q: the whole read fails with %v", pointer, wantErr)
					require.Equal(t, settled(wantErr), settled(gotErr), "%q: %v, then %v", pointer, wantErr, gotErr)
					continue
				}
				require.NoError(t, gotErr, "%q", pointer)
				requireSameTarget(t, want, got, pointer)
				reads++
			}
		}
	}
	assert.Greater(t, reads, 150, "the pointers find things, not only nothing")
}

// prefixesOf yields pointer and each pointer it extends, longest first.
func prefixesOf(pointer jsontext.Pointer) func(func(jsontext.Pointer) bool) {
	return func(yield func(jsontext.Pointer) bool) {
		for p := pointer; ; p = p.Parent() {
			if !yield(p) || p == "" {
				return
			}
		}
	}
}

// TestWalk_LeavesTheModelWhereTheLibraryWouldScan pins where Walk stops and
// what it leaves: at the first token no field answers, with that token and the
// rest, in the mapping the object was built from.
func TestWalk_LeavesTheModelWhereTheLibraryWouldScan(t *testing.T) {
	t.Parallel()
	doc := parsed(t, navDoc)
	for pointer, want := range map[jsontext.Pointer]struct {
		at   any
		rest []string
	}{
		"/x-lib/D0":                   {doc.GetRootNode(), []string{"x-lib", "D0"}},
		"/components/schemas/S/x-ext": {schemaS(t, doc).GetSchema().GetRootNode(), []string{"x-ext"}},
		"/paths/x-paths/q":            {doc.Paths.GetRootNode(), []string{"x-paths", "q"}},
	} {
		tokens, ok := navigation.Tokens(pointer)
		require.True(t, ok)
		at, rest, err := navigation.Walk(doc, tokens)
		require.NoError(t, err, pointer)
		assert.Equal(t, want.rest, rest, pointer)
		assert.Equal(t, reflect.ValueOf(want.at).Pointer(), reflect.ValueOf(at).Pointer(), pointer)
	}
}

// schemaS returns navDoc's component schema S.
func schemaS(t *testing.T, doc *soa.OpenAPI) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	s, ok := doc.Components.Schemas.Get("S")
	require.True(t, ok)
	return s
}

// TestWalk_ScansNoMappingWhereItLeavesTheModel pins GitHub #778's mechanism at
// the step itself: where a token leaves the model, Walk hands the mapping on
// unread. Each mapping is given a first key that is no node, which the
// library's scan of it faults on, as the whole read and Step show.
func TestWalk_ScansNoMappingWhereItLeavesTheModel(t *testing.T) {
	t.Parallel()
	doc := parsed(t, navDoc)
	for _, at := range []struct {
		built   *yaml.Node
		pointer jsontext.Pointer
	}{
		{doc.GetRootNode(), "/x-lib/D0"},
		{schemaS(t, doc).GetSchema().GetRootNode(), "/components/schemas/S/x-ext/k"},
		{doc.Paths.GetRootNode(), "/paths/x-paths/q"},
	} {
		mapping := at.built
		if mapping.Kind == yaml.DocumentNode {
			mapping = mapping.Content[0]
		}
		mapping.Content = append([]*yaml.Node{nil, {Kind: yaml.ScalarNode, Value: "v"}}, mapping.Content...)

		assert.Panics(t, func() { _, _ = whole(doc, at.pointer) }, "the whole read scans %q's mapping", at.pointer)
		tokens, ok := navigation.Tokens(at.pointer)
		require.True(t, ok)
		assert.NotPanics(t, func() {
			got, rest, err := navigation.Walk(doc, tokens)
			assert.NoError(t, err)
			assert.Same(t, at.built, got, "%q", at.pointer)
			assert.NotEmpty(t, rest, "%q", at.pointer)
		}, "%q", at.pointer)
	}
}

// TestLeaves_AsksEachMapTheLibraryAsks pins the maps a model embeds: by pointer
// (paths, a path item's methods, responses' codes, callbacks' expressions) and
// by value (a security requirement's schemes, an x- key among them), each
// answering its own keys and leaving the rest to the object's mapping.
func TestLeaves_AsksEachMapTheLibraryAsks(t *testing.T) {
	t.Parallel()
	doc := parsed(t, navDoc)
	item, ok := doc.Paths.Get("/a")
	require.True(t, ok)
	op := item.GetObject().Get()
	cb, ok := op.Callbacks.Get("cb")
	require.True(t, ok)
	for _, c := range []struct {
		node            any
		answers, misses string
	}{
		{doc.Paths, "/a", "x-paths"},
		{item.GetObject(), "get", "x-path"},
		{&op.Responses, "200", "x-codes"},
		{cb.GetObject(), "{$request.body#/url}", "x-cb"},
		{op.Security[0], "x-scope", "nope"},
	} {
		_, leaves := navigation.Leaves(c.node, c.answers)
		assert.False(t, leaves, "%T answers %q from its map", c.node, c.answers)
		built, leaves := navigation.Leaves(c.node, c.misses)
		assert.True(t, leaves, "%T reads %q in its mapping", c.node, c.misses)
		assert.Same(t, rootOf(t, c.node), built, "%T", c.node)
	}
}

// TestLeaves_PlacesWhatNoObjectReads pins the nodes Leaves reads as the library
// does without a field: raw YAML is itself, and a map, a list, a scalar, a nil
// pointer, a map that is no model and an unresolved reference place nothing.
func TestLeaves_PlacesWhatNoObjectReads(t *testing.T) {
	t.Parallel()
	raw := &yaml.Node{Kind: yaml.MappingNode}
	got, leaves := navigation.Leaves(raw, "k")
	assert.True(t, leaves)
	assert.Same(t, raw, got)
	got, leaves = navigation.Leaves(*raw, "k")
	assert.True(t, leaves, "a node held by value is read as one")
	assert.Equal(t, raw.Kind, got.Kind)

	var none *soa.OpenAPI
	unresolved := parsedUnresolved(t)
	for _, node := range []any{
		nil, map[string]any{"k": 1}, []any{1}, "s", none, (*yaml.Node)(nil),
		sequencedmap.New[string, int](), unresolved,
	} {
		_, leaves := navigation.Leaves(node, "k")
		assert.False(t, leaves, "%T", node)
	}
}

// parsedUnresolved returns a response reference no resolution has followed,
// whose library read fails until one does.
func parsedUnresolved(t *testing.T) any {
	t.Helper()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(navDoc))
	require.NoError(t, err)
	r, ok := doc.Paths.Get("/a")
	require.True(t, ok)
	ref, ok := r.GetObject().Get().Responses.Get("200")
	require.True(t, ok)
	_, err = whole(ref, "/description")
	require.Error(t, err)
	require.False(t, settled(err), "the library reads past it once it resolves")
	return ref
}

// TestTokens_DecodesAsTheLibraryReads pins how a pointer is split: "/" is the
// root, each other token is decoded, an empty one is kept, and what the
// library's validation refuses is no pointer, though it accepts a byte that is
// no UTF-8.
func TestTokens_DecodesAsTheLibraryReads(t *testing.T) {
	t.Parallel()
	for pointer, want := range map[jsontext.Pointer][]string{
		"/":          {},
		"/a~1b/~0c/": {"a/b", "~c", ""},
		"//x":        {"", "x"},
		"/~01":       {"~1"},
		"/0/00":      {"0", "00"},
		"/a\xffb":    {"a\xffb"},
	} {
		got, ok := navigation.Tokens(pointer)
		require.True(t, ok, "%q", pointer)
		assert.Equal(t, len(want), len(got), "%q", pointer)
		assert.Equal(t, want, append([]string{}, got...), "%q", pointer)
	}
	for _, pointer := range []jsontext.Pointer{"", "a", "/~2", "/~", "/a~", "/\U0001F600"} {
		_, ok := navigation.Tokens(pointer)
		assert.False(t, ok, "%q", pointer)
		assert.Error(t, jsonpointer.JSONPointer(pointer).Validate(), "%q: the library refuses it too", pointer)
	}
}

// stepDoc is a document a step reads past a path item and an operation's
// responses, which the model holds by value, and through a response keyed by
// the empty string.
const stepDoc = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {responses: {"200": {description: ok}, default: {description: otherwise}}}}
components:
  responses:
    "": {description: keyed by the empty string}
`

// TestStep_ReadsOneToken pins the one step a walk takes: what a node holds
// under a token, the empty token included, which the library reads as the root
// when it is the only one; and nothing where the node holds nothing.
func TestStep_ReadsOneToken(t *testing.T) {
	t.Parallel()
	doc := parsed(t, stepDoc)
	empty, ok := doc.Components.Responses.Get("")
	require.True(t, ok)

	next, ok := navigation.Step(doc.Components.Responses, "")
	require.True(t, ok)
	assert.Same(t, empty, next, "the empty token names the entry keyed by the empty string")
	next, ok = navigation.Step(doc, "components")
	require.True(t, ok)
	assert.Same(t, doc.Components, next)
	_, ok = navigation.Step(doc, "nope")
	assert.False(t, ok, "a token the node holds nothing under")
}

// TestStep_HandsOnAStructHeldByValue pins what a step hands on where the model
// holds a model by value, as an operation holds its responses: a pointer, from
// which the next step reads what only its methods answer, as the library's walk
// reads on from the field's address. Any other struct is handed on as itself.
func TestStep_HandsOnAStructHeldByValue(t *testing.T) {
	t.Parallel()
	doc := parsed(t, stepDoc)
	item, ok := navigation.Step(doc.Paths, "/a")
	require.True(t, ok)
	op, ok := navigation.Step(item, "get")
	require.True(t, ok)
	responses, ok := navigation.Step(op, "responses")
	require.True(t, ok)
	require.IsType(t, &soa.Responses{}, responses, "the responses, which the operation holds by value")

	next, ok := navigation.Step(responses, "default")
	require.True(t, ok, "the default response, which the responses' methods answer")
	want, err := whole(doc, "/paths/~1a/get/responses/default")
	require.NoError(t, err)
	assert.Same(t, want, next, "as the whole read finds it")

	type plain struct{ K int }
	held, ok := navigation.Step(map[string]any{"v": plain{K: 1}}, "v")
	require.True(t, ok)
	assert.Equal(t, plain{K: 1}, held, "a struct that is no model stays a value, as the library reads on from it")
}

// fielded is a struct that is no model, as the library reads one by its own
// fields: one keyed by its tag, one by its name, one unexported, which no key
// reaches, and the mapping it was built from.
type fielded struct {
	Tagged   int `key:"tagged"`
	Untagged int
	hidden   int
	built    *yaml.Node
}

// GetRootNode returns the mapping f was built from.
func (f *fielded) GetRootNode() *yaml.Node { return f.built }

// indexed is fielded that the library also reads by index.
type indexed struct{ fielded }

// NavigateWithIndex answers index 0 with the tagged field.
func (x *indexed) NavigateWithIndex(i int) (any, error) {
	if i != 0 {
		return nil, errors.New("no such index")
	}
	return x.Tagged, nil
}

// unbuilt is a struct that is no model and was built from no mapping.
type unbuilt struct{ Field int }

// endless stands for itself, which the library would follow until its stack
// runs out.
type endless struct{}

// GetNavigableNode returns e itself.
func (e *endless) GetNavigableNode() (any, error) { return e, nil }

// TestLeaves_ReadsAStructThatIsNoModelAsTheLibraryDoes pins the dispatch the
// corpus never reaches. A struct that is no model answers a token its fields
// key, by tag or by name, and reads any other, an unexported field's name
// included, in its mapping, where the library finds the same node. One built
// from none places nothing, nor does a map, which answers or fails itself. An
// index it would also read by index is left to the library, though a leading
// zero makes no index; and stand-ins past the hop bound place nothing.
func TestLeaves_ReadsAStructThatIsNoModelAsTheLibraryDoes(t *testing.T) {
	t.Parallel()
	var built yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{hidden: {k: 1}, other: {k: 2}, x: {k: 3}, '00': {k: 4}}"), &built))
	mapping := built.Content[0]
	for _, c := range []struct {
		node   any
		token  string
		leaves bool
	}{
		{&fielded{built: mapping}, "tagged", false},
		{&fielded{built: mapping}, "Untagged", false},
		{&fielded{built: mapping, hidden: 1}, "hidden", true},
		{&fielded{built: mapping}, "other", true},
		{&indexed{fielded{built: mapping}}, "0", false},
		{&indexed{fielded{built: mapping}}, "x", true},
		{&indexed{fielded{built: mapping}}, "00", true},
		{&unbuilt{}, "other", false},
		{&keyedBuilt{built: mapping}, "other", false},
		{&endless{}, "other", false},
		{&soa.Paths{}, "/a", false},
	} {
		got, leaves := navigation.Leaves(c.node, c.token)
		require.Equal(t, c.leaves, leaves, "%T %q", c.node, c.token)
		if !leaves {
			continue
		}
		require.Same(t, mapping, got, "%T %q", c.node, c.token)
		pointer := jsontext.Pointer("/" + c.token + "/k")
		want, err := whole(c.node, pointer)
		require.NoError(t, err, "%T %q", c.node, c.token)
		read, err := whole(got, pointer)
		require.NoError(t, err)
		assert.Same(t, want, read, "%T %q: the library reads the token where Leaves places it", c.node, c.token)
	}
}

// AnswersAll is a map that answers every key, and has no other map method. It
// is exported, as every type a library model embeds is: the library reads an
// embedded field through reflect, which refuses an unexported one.
type AnswersAll struct{}

// NavigateWithKey answers every key with 1.
func (*AnswersAll) NavigateWithKey(string) (any, error) { return 1, nil }

// fakeCore is the core of fakeModel: one field, keyed "name".
type fakeCore struct {
	Name string `key:"name"`
}

// fakeModel is a model embedding AnswersAll by value, which the library asks
// for no key, since the map's address has none of the other map methods, and
// one by pointer, which it asks for every key when it is not nil.
type fakeModel struct {
	AnswersAll
	*ByPointer
	core  fakeCore
	built *yaml.Node
}

// ByPointer is AnswersAll, embedded by pointer.
type ByPointer struct{ AnswersAll }

// GetCoreAny returns m's core.
func (m *fakeModel) GetCoreAny() any { return &m.core }

// SetCoreAny ignores core.
func (*fakeModel) SetCoreAny(any) {}

// GetRootNode returns the mapping m was built from.
func (m *fakeModel) GetRootNode() *yaml.Node { return m.built }

// TestLeaves_AsksAnEmbeddedMapOnlyWhereTheLibraryDoes pins navigateModel's
// rule for the maps a model embeds: one embedded by value is asked only when
// its address has every method of the library's own map, and one embedded by
// pointer when it is not nil. The library is read alongside, so the rule is
// held to it rather than to this test's reading of it.
func TestLeaves_AsksAnEmbeddedMapOnlyWhereTheLibraryDoes(t *testing.T) {
	t.Parallel()
	var built yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{x: {k: 1}}"), &built))
	mapping := built.Content[0]

	byValue := &fakeModel{built: mapping}
	got, leaves := navigation.Leaves(byValue, "x")
	require.True(t, leaves, "the map embedded by value is not asked")
	assert.Same(t, mapping, got)
	want, err := whole(byValue, "/x/k")
	require.NoError(t, err)
	read, err := whole(got, "/x/k")
	require.NoError(t, err)
	assert.Same(t, want, read, "the library reads x in the mapping too")

	both := &fakeModel{built: mapping, ByPointer: &ByPointer{}}
	_, leaves = navigation.Leaves(both, "x")
	assert.False(t, leaves, "the map embedded by pointer is asked")
	answer, err := whole(both, "/x")
	require.NoError(t, err)
	assert.Equal(t, 1, answer, "the library asks it too")
}

// TestLeaves_ReadsAModelsCoreByTagAlone pins how a model's core answers: by a
// field's key tag and never its name, so an untagged field answers the empty
// key, which the library then fails to find, and a field's name is read in
// the mapping like any key no field answers.
func TestLeaves_ReadsAModelsCoreByTagAlone(t *testing.T) {
	t.Parallel()
	doc := parsed(t, navDoc)
	for token, leaves := range map[string]bool{"info": false, "": false, "Info": true, "CoreModel": true} {
		_, got := navigation.Leaves(doc, token)
		assert.Equal(t, leaves, got, "%q", token)
	}
	_, err := whole(doc, "//title")
	assert.ErrorIs(t, err, jsonpointer.ErrNotFound, "the untagged field the empty key finds holds nothing")
}

// keyedBuilt is a map that is no model and answers no key, built from a
// mapping: the library reports its own failure rather than reading the
// mapping.
type keyedBuilt struct{ built *yaml.Node }

// NavigateWithKey answers no key.
func (*keyedBuilt) NavigateWithKey(key string) (any, error) { return nil, errors.New("no " + key) }

// GetRootNode returns the mapping k was built from.
func (k *keyedBuilt) GetRootNode() *yaml.Node { return k.built }
