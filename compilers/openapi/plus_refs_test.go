package openapi

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// responseSchema is the pointer of the 200 response's JSON schema of the
// operation at get under the path key.
func responseSchema(path string) []string {
	return []string{"paths", path, "get", "responses", "200", "content", "application/json", "schema"}
}

// TestCompile_APlusInAPointerNamesAPlus is the compiler-level half of GitHub
// #911: a '+' in a $ref fragment is a plus, so the pointer names the key
// "/a+b" and the type it interns, whose ID spells "~1a+b", never "~1a b".
func TestCompile_APlusInAPointerNamesAPlus(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a+b:
    get:
      operationId: g
      responses:
        "200": {description: ok, content: {application/json: {schema: {type: object, properties: {id: {type: string}}}}}}
  /c:
    get:
      operationId: h
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: '#/paths/~1a+b/get/responses/200/content/application~1json/schema'}
`)

	openapitest.RequireNoErrorDiags(t, diags)
	want := ids.Document{}.ForPointer(ids.Ptr(responseSchema("/a+b")...))
	assert.Contains(t, string(want), "~1a+b")
	require.NotNil(t, doc.Types[want], "the type is interned at the key the pointer names")
	assert.Equal(t, want, openapitest.BodyTarget(t, openapitest.FindOp(t, doc, "h").Responses[0].Payload))
	assert.Equal(t, want, openapitest.BodyTarget(t, openapitest.FindOp(t, doc, "g").Responses[0].Payload))
}

// plusCollisionSpec holds the paths "/a b" and "/a+b", each with an object of
// its own, and references to each by a plus, an escaped plus and an escaped
// space, in the orders given.
func plusCollisionSpec(paths, uses []string) string {
	path := map[string]string{
		"space": `  "/a b":
    get:
      operationId: sp
      responses:
        "200": {description: ok, content: {application/json: {schema: {type: object, properties: {s: {type: string}}}}}}
`,
		"plus": `  "/a+b":
    get:
      operationId: pl
      responses:
        "200": {description: ok, content: {application/json: {schema: {type: object, properties: {i: {type: integer}}}}}}
`,
	}
	const suffix = "/get/responses/200/content/application~1json/schema"
	use := map[string]string{
		"plus":    "    Plus: {$ref: '#/paths/~1a+b" + suffix + "'}\n",
		"escaped": "    Escaped: {$ref: '#/paths/~1a%2Bb" + suffix + "'}\n",
		"space":   "    Space: {$ref: '#/paths/~1a%20b" + suffix + "'}\n",
	}
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n")
	for _, p := range paths {
		b.WriteString(path[p])
	}
	b.WriteString("components:\n  schemas:\n")
	for _, u := range uses {
		b.WriteString(use[u])
	}
	return b.String()
}

// plusView is everything of a compile that must not depend on the order the
// document declares its paths and references in: the types, the diagnostics
// sorted, and the type each operation's response and each component names.
type plusView struct {
	Types       ir.TypeRegistry
	Diagnostics []ir.Diagnostic
	Responses   map[string]ir.TypeID
	Components  map[string]ir.TypeID
}

func viewOf(t *testing.T, src string) plusView {
	t.Helper()
	doc, diags := parseFull(t, src)
	slices.SortFunc(diags, func(a, b ir.Diagnostic) int {
		return cmp.Or(cmp.Compare(a.Code, b.Code), cmp.Compare(a.Provenance.Pointer, b.Provenance.Pointer),
			cmp.Compare(a.Message, b.Message))
	})
	v := plusView{Types: doc.Types, Diagnostics: diags, Responses: map[string]ir.TypeID{}, Components: map[string]ir.TypeID{}}
	for _, name := range []string{"sp", "pl"} {
		v.Responses[name] = openapitest.BodyTarget(t, openapitest.FindOp(t, doc, name).Responses[0].Payload)
	}
	for _, name := range []string{"Plus", "Escaped", "Space"} {
		scalar, ok := doc.Types[componentID(name)].(*ir.Scalar)
		require.True(t, ok, "%s is an alias of the type its $ref names", name)
		v.Components[name] = scalar.Base.Target
	}
	return v
}

// TestCompile_APlusAndAnEscapedPlusAndASpaceKeepTheirOwnTypesInEveryOrder pins
// that the types a '+', a "%2B" and a "%20" reference reach do not depend on the
// order the paths and the references are written in. The resolver caches an
// object by the pointer it decodes, first come first served, so one reading
// the '+' as a space handed it whichever of "/a b" and "/a+b" it met first.
// A single order passes whichever way the lowering resolves, so all four are
// compiled and compared.
func TestCompile_APlusAndAnEscapedPlusAndASpaceKeepTheirOwnTypesInEveryOrder(t *testing.T) {
	t.Parallel()
	plusType := ids.Document{}.ForPointer(ids.Ptr(responseSchema("/a+b")...))
	spaceType := ids.Document{}.ForPointer(ids.Ptr(responseSchema("/a b")...))
	orders := []struct {
		name        string
		paths, uses []string
	}{
		{"as written", []string{"space", "plus"}, []string{"plus", "escaped", "space"}},
		{"paths reversed", []string{"plus", "space"}, []string{"plus", "escaped", "space"}},
		{"references reversed", []string{"space", "plus"}, []string{"space", "escaped", "plus"}},
		{"both reversed", []string{"plus", "space"}, []string{"space", "escaped", "plus"}},
	}
	views := make([]plusView, len(orders))
	for i, order := range orders {
		views[i] = viewOf(t, plusCollisionSpec(order.paths, order.uses))
	}

	for i, order := range orders {
		assert.Equal(t, map[string]ir.TypeID{"Plus": plusType, "Escaped": plusType, "Space": spaceType},
			views[i].Components, "%s", order.name)
		assert.Equal(t, map[string]ir.TypeID{"pl": plusType, "sp": spaceType}, views[i].Responses, "%s", order.name)
		if d := gocmp.Diff(views[0], views[i]); d != "" {
			t.Errorf("%s differs from %s (-%s +%s):\n%s", order.name, orders[0].name, orders[0].name, order.name, d)
		}
	}
	assert.NotEqual(t, plusType, spaceType)
}

// TestCompile_AnUnresolvedPlusReferenceIsQuotedAsWritten pins that no stage of
// the compile spells a '+' the source wrote as "%2B" in what it reports: the
// load phase for a reference object and for a schema $ref, one inside raw YAML
// a $ref reaches included, and the lowering for a discriminator mapping value.
func TestCompile_AnUnresolvedPlusReferenceIsQuotedAsWritten(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters: [{$ref: '#/paths/~1nope+x/get'}]
      responses: {"200": {description: ok}}
components:
  schemas:
    S: {$ref: '#/x-defs/A'}
    Pet:
      type: object
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping:
          k: '#/x-defs/nope+x'
x-defs:
  A: {type: object, properties: {p: {$ref: '#/x-defs/nope+x'}, q: {$ref: 'other.yaml#/x+y'}}}
`)

	messages := map[string]string{}
	for _, d := range diags {
		messages[string(d.Provenance.Pointer)] = d.Message
		assert.NotContains(t, d.Message, "%2B", "%s", d.Provenance.Pointer)
	}
	assert.True(t, strings.HasPrefix(messages["/paths/~1a/get/parameters/0"], `unresolved $ref "#/paths/~1nope+x/get": `))
	const nothing = ": it names nothing in the document it points into"
	assert.Equal(t, `unresolved $ref "#/x-defs/nope+x"`+nothing, messages["/x-defs/A/properties/p"])
	assert.Equal(t, `unresolved $ref "other.yaml#/x+y": it names another document, and external references are not read`,
		messages["/x-defs/A/properties/q"])
	assert.Equal(t, `discriminator mapping "k" references unresolved schema "#/x-defs/nope+x"`+nothing,
		messages["/components/schemas/Pet/discriminator/mapping/k"])
}

// TestCompile_AnAnchoredReferenceKeepsItsPlusWhereItIsText pins that a scalar
// an alias makes both a $ref and a description stays as written in the
// description, wherever the anchor is: load respells the $ref for the resolver
// only where it is a $ref.
func TestCompile_AnAnchoredReferenceKeepsItsPlusWhereItIsText(t *testing.T) {
	t.Parallel()
	const text = "#/paths/~1a+b/get/responses/200/content/application~1json/schema"
	tests := []struct{ name, schemas string }{
		{"anchored at the text", `    Named: {type: object, description: &a '` + text + `'}
    Plus: {$ref: *a}
    Text: {type: object, description: *a}
`},
		{"anchored at the $ref", `    Plus: {$ref: &a '` + text + `'}
    Named: {type: object, description: *a}
    Text: {type: object, description: *a}
`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a+b:
    get:
      operationId: g
      responses:
        "200": {description: ok, content: {application/json: {schema: {type: object, properties: {id: {type: string}}}}}}
components:
  schemas:
`+tc.schemas)

			openapitest.RequireNoErrorDiags(t, diags)
			plus, ok := typeByName(doc, "Plus").(*ir.Scalar)
			require.True(t, ok, "Plus is an alias of the type its $ref names")
			assert.Equal(t, ids.Document{}.ForPointer(ids.Ptr(responseSchema("/a+b")...)), plus.Base.Target)
			for _, name := range []string{"Named", "Text"} {
				assert.Equal(t, text, typeByName(doc, name).Common().Docs.Description, name)
			}
		})
	}
}

// TestCompile_AReferenceOfEveryKindThroughAPlusKeyIsLoweredWhereItIsDeclared
// pins what a reference to an object under the key "/a+b" lowers to: the object
// the key names, declared at the pointer that spells the plus. The parameter and
// the header carry that pointer as their provenance and their ID, and the
// path item's operations are mounted at the path that references it and
// declared at the key.
func TestCompile_AReferenceOfEveryKindThroughAPlusKeyIsLoweredWhereItIsDeclared(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, string(readReproducer(t, "ref_fragment_plus")))
	openapitest.RequireNoErrorDiags(t, diags)

	relay := openapitest.FindOp(t, doc, "relay")
	require.Len(t, relay.Params, 1)
	assert.Equal(t, "/paths/~1a+b/get/parameters/0", string(relay.Params[0].Provenance.Pointer), "the parameter")
	require.Len(t, relay.Responses, 1)
	require.Len(t, relay.Responses[0].Headers, 1)
	assert.Equal(t, ir.PropID("p/openapi/paths/~1a+b/get/responses/200/headers/H"), relay.Responses[0].Headers[0].ID, "the header")
	assert.Equal(t, ids.Document{}.ForPointer(ids.Ptr(responseSchema("/a+b")...)),
		openapitest.BodyTarget(t, &ir.Payload{Contents: relay.Responses[0].Payload.Contents[:1]}), "the response")

	var mounted []string
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if strings.HasPrefix(string(op.ID), "op/openapi/paths/~1c/") {
				mounted = append(mounted, string(op.Provenance.Pointer))
			}
		}
	}
	assert.ElementsMatch(t, []string{"/paths/~1a+b/get", "/paths/~1a+b/post"}, mounted, "the path item")
}

// TestCompile_APointerPastAPathItemRefThroughAPlusKeyNamesTheOtherDocument pins
// GitHub #762 for a key with a plus: a reference whose pointer passes the $ref
// of the path item "/a+b" into ext.yaml names what ext.yaml holds there, so the
// $ref written in it, which names ext.yaml's own Pet, is unresolved in the source
// that reached it, where read through a key spelled with a space it would name
// the source's Pet without a word. The references are declared first in one
// order and last in the other, the first being the order that went wrong.
func TestCompile_APointerPastAPathItemRefThroughAPlusKeyNamesTheOtherDocument(t *testing.T) {
	t.Parallel()
	const (
		ext = `openapi: 3.1.0
info: {title: E, version: "1"}
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  pet: {$ref: '#/components/schemas/Pet'}
components:
  schemas:
    Pet: {type: object, properties: {theirs: {type: string}}}
`
		item = "  /a+b: {$ref: './ext.yaml#/paths/~1a'}\n"
		uses = `  /c:
    get:
      responses:
        "200": {$ref: '#/paths/~1a+b/get/responses/200'}
  /d:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: '#/paths/~1a+b/get/responses/200/content/application~1json/schema'}}}
  /e:
    get:
      responses:
        "200": {$ref: '#/components/responses/Alias'}
`
		tail = `components:
  responses:
    Alias: {$ref: '#/paths/~1a+b/get/responses/200'}
  schemas:
    Pet: {type: object, properties: {ours: {type: integer}}}
`
	)
	// /0 names /a+b first, so the resolver has followed its $ref before any
	// pointer passes it, in either order.
	const first = "  /0: {$ref: '#/paths/~1a+b'}\n"
	reported := make([][]string, 0, 2)
	for _, paths := range []string{first + uses + item, first + item + uses} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(ext), 0o600))
		root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" + paths + tail

		doc, diags, err := New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: true}})

		require.NoError(t, err)
		require.NotNil(t, doc)
		got := make([]string, 0, len(diags))
		for _, d := range diags {
			got = append(got, string(d.Provenance.Pointer)+" "+d.Message)
		}
		slices.Sort(got)
		reported = append(reported, got)
	}
	const why = `unresolved $ref "#/components/schemas/Pet": it names a position in the other document holding it, which is not lowered`
	const pet = "/get/responses/200/content/application~1json/schema/properties/pet"
	assert.Equal(t, []string{"/paths/~10" + pet + " " + why, "/paths/~1a+b" + pet + " " + why}, reported[0],
		"each copy of ext.yaml's /a reports its Pet, and the references reach nothing of the source's")
	assert.Equal(t, reported[0], reported[1], "whichever order the references are declared in")
}

// TestCompile_FieldsBesideARefReachedPastAPlusKeyAreInTheOtherDocument pins the
// words a finding about fields written beside a path item's $ref uses for where
// they are: "/m" reaches x-n through the $ref of "/a+b" into ext.yaml, where x-n
// writes a description beside its own $ref. Read through a key spelled with a
// space, the pointer would pass no $ref and name a position of this document.
func TestCompile_FieldsBesideARefReachedPastAPlusKeyAreInTheOtherDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(`openapi: 3.1.0
info: {title: E, version: "1"}
paths:
  /a:
    x-n:
      $ref: './ext.yaml#/paths/~1b'
      description: beside
  /b:
    get:
      responses: {"200": {description: ok}}
`), 0o600))
	// /0 names /a+b first, so the resolver has followed its $ref before any
	// pointer passes it.
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /0: {$ref: '#/paths/~1a+b'}
  /a+b: {$ref: './ext.yaml#/paths/~1a'}
  /m: {$ref: '#/paths/~1a+b/x-n'}
`

	doc, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: true}})

	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "the path item at #/paths/~1a+b/x-n writes fields beside its $ref in another document, "+
		"which this compiler does not read; they are not lowered at this mount",
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, "/paths/~1m"))
}
