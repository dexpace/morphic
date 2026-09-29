package openapi_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// overlaySpec is the document the cases below patch. Its one component schema
// and one operation are enough to show an overlay reaching both halves of the
// compiler, and its `info` block is what the line-preservation case adds to.
const overlaySpec = `openapi: 3.1.0
info:
  title: Pets
  version: "1"
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        '200': {description: ok}
components:
  schemas:
    Pet:
      type: object
      properties:
        name: {type: string}
`

// addTagProperty adds one property to a named schema — the smallest patch whose
// effect is visible as an IR node with a provenance of its own.
const addTagProperty = `overlay: 1.0.0
info: {title: Patch, version: "1"}
actions:
  - target: $.components.schemas.Pet.properties
    update:
      tag: {type: string}
`

// compileWith runs the public entry point over overlaySpec with opts, requiring
// that a document came back.
func compileWith(t *testing.T, opts openapi.Options) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(overlaySpec)}},
		compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	require.NotNil(t, doc, "compile refused: %+v", diags)
	return doc, diags
}

// propertyProvenance finds the named property of the named component schema and
// returns the provenance the compiler stamped on it.
func propertyProvenance(t *testing.T, doc *ir.Document, schema, property string) ir.Provenance {
	t.Helper()
	for _, def := range doc.Types {
		model, ok := def.(*ir.Model)
		if !ok || model.Name.Source != schema {
			continue
		}
		for _, p := range model.Properties {
			if p.Name.Source == property {
				return p.Provenance
			}
		}
		t.Fatalf("schema %q has no property %q", schema, property)
	}
	t.Fatalf("no component schema named %q", schema)
	return ir.Provenance{}
}

// TestCompile_OverlayAppliesBeforeLowering pins the first acceptance criterion:
// the IR reflects the patched document, not the bytes on disk. A property the
// source never declared is in the IR because the overlay put it there before the
// schema walk read the shape.
func TestCompile_OverlayAppliesBeforeLowering(t *testing.T) {
	t.Parallel()
	before, _ := compileWith(t, openapi.Options{})
	after, diags := compileWith(t, openapi.Options{
		Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(addTagProperty)},
	})

	assert.False(t, ir.HasError(diags), "the overlay applies cleanly: %+v", diags)

	names := func(doc *ir.Document) []string {
		var out []string
		for _, def := range doc.Types {
			if model, ok := def.(*ir.Model); ok && model.Name.Source == "Pet" {
				for _, p := range model.Properties {
					out = append(out, p.Name.Source)
				}
			}
		}
		return out
	}
	assert.Equal(t, []string{"name"}, names(before), "the source declares one property")
	assert.Equal(t, []string{"name", "tag"}, names(after), "and the overlay adds the second")
}

// TestCompile_OverlayIsRecordedAsASource pins the third acceptance criterion.
// A position the overlay introduced names the overlay as its Provenance.Source,
// the positions beside it still name the spec, and both indexes address a real
// entry in Document.Sources — an index naming nothing would be a dangling
// reference that reads as a valid one.
func TestCompile_OverlayIsRecordedAsASource(t *testing.T) {
	t.Parallel()
	doc, _ := compileWith(t, openapi.Options{
		Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(addTagProperty)},
	})

	require.Len(t, doc.Sources, 2, "the spec and the overlay applied to it")
	assert.Equal(t, "openapi@3.1", doc.Sources[0].Format)
	assert.Equal(t, "spec.yaml", doc.Sources[0].Path)
	assert.Equal(t, "overlay@1.0.0", doc.Sources[1].Format)
	assert.Equal(t, "patch.yaml", doc.Sources[1].Path)
	assert.NotEqual(t, doc.Sources[0].Hash, doc.Sources[1].Hash, "each hashes its own bytes")

	introduced := propertyProvenance(t, doc, "Pet", "tag")
	assert.Equal(t, 1, introduced.Source, "the overlay introduced this property")
	assert.Equal(t, jsontext.Pointer("/components/schemas/Pet/properties/tag"), introduced.Pointer)

	declared := propertyProvenance(t, doc, "Pet", "name")
	assert.Equal(t, 0, declared.Source, "the spec declared this one")

	// The structural check is what makes the two assertions above more than a
	// pair of numbers: irverify reports every Provenance.Source addressing no
	// declared entry, so an overlay index minted without the Sources entry to
	// match is a dangling reference here rather than a plausible-looking one.
	assert.Empty(t, irverify.Verify(doc), "the second source entry keeps the document valid")
}

// TestCompile_AnOverlaidDocumentRoundTripsAndIsDeterministic runs the two
// document-level oracles over a patched compile.
//
// internal/harness drives every corpus spec through them, but it takes bytes and
// no options, so no overlay has ever reached them — a check that runs is not a
// check that reaches. The overlay path adds a second Sources entry and decides
// provenance through a map, so both oracles have something new to say here:
// round-tripping pins the entry against invariant 7, and repeating the compile
// is what would surface any output ordered by that map's iteration.
func TestCompile_AnOverlaidDocumentRoundTripsAndIsDeterministic(t *testing.T) {
	t.Parallel()
	marshalled := func() string {
		doc, _ := compileWith(t, openapi.Options{
			Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(addTagProperty)},
		})
		b, err := json.Marshal(doc)
		require.NoError(t, err)
		return string(b)
	}

	first := marshalled()
	assert.Equal(t, first, marshalled(), "two compiles of one input must agree byte for byte")

	var back ir.Document
	require.NoError(t, json.Unmarshal([]byte(first), &back))
	again, err := json.Marshal(&back)
	require.NoError(t, err)
	assert.JSONEq(t, first, string(again), "the document survives a round trip through JSON")
}

// TestCompile_WithoutAnOverlayRecordsOneSource is the control for
// TestCompile_OverlayIsRecordedAsASource. Without it, an assertion that the
// overlay entry is present would pass on a compiler that appended it
// unconditionally.
func TestCompile_WithoutAnOverlayRecordsOneSource(t *testing.T) {
	t.Parallel()
	doc, _ := compileWith(t, openapi.Options{})

	require.Len(t, doc.Sources, 1)
	assert.Equal(t, "openapi@3.1", doc.Sources[0].Format)
	assert.Equal(t, 0, propertyProvenance(t, doc, "Pet", "name").Source)
}

// primKindsSpec declares properties of several primitive kinds, so the rule is
// held across kinds rather than for one.
const primKindsSpec = `openapi: 3.1.0
info:
  title: Prims
  version: "1"
paths: {}
components:
  schemas:
    Widget:
      type: object
      properties:
        name: {type: string}
        count: {type: integer}
        ratio: {type: number}
        active: {type: boolean}
`

// addBirthProperty overlays a property of another primitive kind, date, onto
// Widget, at a position whose own Provenance.Source is the overlay's index (1)
// rather than the spec's (0). No position the spec declares reaches that kind.
const addBirthProperty = `overlay: 1.0.0
info: {title: Patch, version: "1"}
actions:
  - target: $.components.schemas.Widget.properties
    update:
      born: {type: string, format: date}
`

// primitiveProvenances returns the Provenance of every *ir.Primitive in
// doc.Types, keyed by kind.
func primitiveProvenances(doc *ir.Document) map[ir.PrimKind]ir.Provenance {
	out := make(map[ir.PrimKind]ir.Provenance)
	for _, def := range doc.Types {
		if prim, ok := def.(*ir.Primitive); ok {
			out[prim.Prim] = prim.Provenance
		}
	}
	return out
}

// TestCompile_PrimitivesNameNoSource pins GitHub #528: a shared primitive is
// reached by kind from every position of it in every source, so no source is
// its own and its Provenance names none.
//
// The overlaid case adds a kind, date, that only the overlay's position
// reaches, in a document with two sources. Crediting the overlay with it would
// look accurate, but the node is the kind's, shared by any source that uses it,
// so it names no source there either.
func TestCompile_PrimitivesNameNoSource(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		overlay *openapi.Overlay
		kinds   []ir.PrimKind
	}{
		"plain": {
			kinds: []ir.PrimKind{ir.PrimBool, ir.PrimInteger, ir.PrimNumber, ir.PrimString},
		},
		"overlaid": {
			overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(addBirthProperty)},
			kinds:   []ir.PrimKind{ir.PrimBool, ir.PrimDate, ir.PrimInteger, ir.PrimNumber, ir.PrimString},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: "spec.yaml", Data: []byte(primKindsSpec)}},
				compilers.Options{FormatOptions: openapi.Options{Overlay: tc.overlay}})
			require.NoError(t, err)
			require.NotNil(t, doc, "compile refused: %+v", diags)
			if tc.overlay != nil {
				require.Equal(t, 1, propertyProvenance(t, doc, "Widget", "born").Source,
					"the one position reaching date is the overlay's")
			}

			want := make(map[ir.PrimKind]ir.Provenance, len(tc.kinds))
			for _, k := range tc.kinds {
				want[k] = ir.Provenance{Source: ir.NoSource}
			}
			if diff := cmp.Diff(want, primitiveProvenances(doc)); diff != "" {
				t.Errorf("primitive provenance by kind (-want +got):\n%s", diff)
			}
		})
	}
}

// TestCompile_OverlayPreservesSourceLineNumbers pins the reason the overlay is
// applied to the node tree rather than to re-serialised bytes.
//
// The overlay adds a line above the position the diagnostic is about. Under a
// re-serialise-and-reparse the reported line moves, and every diagnostic in the
// document starts naming a position in a file that exists nowhere; applying to
// the tree leaves the untouched nodes exactly where the parser found them. The
// document with no overlay is the reference, so this compares the compiler
// against itself rather than against a number written down here.
func TestCompile_OverlayPreservesSourceLineNumbers(t *testing.T) {
	t.Parallel()
	// A response object spelled as a string: a validation finding sited by
	// position, several lines below the info block the overlay grows.
	const spec = `openapi: 3.1.0
info:
  title: Pets
  version: "1"
paths:
  /pets:
    get:
      responses: "not an object"
`
	const addsALine = `overlay: 1.0.0
info: {title: Patch, version: "1"}
actions:
  - target: $.info
    update: {description: added above the finding}
`
	sited := func(opts openapi.Options) []ir.Diagnostic {
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: "spec.yaml", Data: []byte(spec)}},
			compilers.Options{FormatOptions: opts})
		require.NoError(t, err)
		require.NotNil(t, doc)
		var out []ir.Diagnostic
		for _, d := range diags {
			if d.Provenance.Position != (ir.Position{}) {
				out = append(out, d)
			}
		}
		require.NotEmpty(t, out, "the fixture must produce a sited diagnostic to compare")
		return out
	}

	assert.Equal(t, sited(openapi.Options{}),
		sited(openapi.Options{Overlay: &openapi.Overlay{Data: []byte(addsALine)}}),
		"an overlay above a finding must not move it")
}

// noMatch targets a path the spec does not declare — the typo the strict default
// exists to catch.
const noMatch = `overlay: 1.0.0
info: {title: Patch, version: "1"}
actions:
  - target: $.paths['/nope']
    update: {description: x}
`

// TestCompile_StrictRefusesAnActionThatMatchesNothing pins the second acceptance
// criterion. Strict is the default, so a JSONPath that matches nothing is
// reported and the compile refuses rather than quietly producing an SDK missing
// the fix the overlay was written to make.
func TestCompile_StrictRefusesAnActionThatMatchesNothing(t *testing.T) {
	t.Parallel()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(overlaySpec)}},
		compilers.Options{FormatOptions: openapi.Options{
			Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(noMatch)},
		}})

	require.NoError(t, err, "a bad overlay is a document problem, not a Go error")
	assert.Nil(t, doc, "nothing is lowered")
	require.True(t, ir.HasError(diags), "the typo is fatal: %+v", diags)

	named := false
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning {
			named = true
			assert.Contains(t, d.Message, "$.paths['/nope']", "the action is named")
		}
		assert.Equal(t, 1, d.Provenance.Source, "the report is about the overlay")
	}
	assert.True(t, named, "the refusal names which action caused it: %+v", diags)
}

// TestCompile_LaxIgnoresAnActionThatMatchesNothing pins the other side of the
// same switch, on the same overlay: what strict refuses, lax passes over in
// silence and compiles. Using the identical input is what makes this a test of
// the flag rather than of two unrelated documents.
func TestCompile_LaxIgnoresAnActionThatMatchesNothing(t *testing.T) {
	t.Parallel()
	doc, diags := compileWith(t, openapi.Options{
		Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(noMatch), Lax: true},
	})

	assert.False(t, ir.HasError(diags), "lax reports nothing: %+v", diags)
	assert.Len(t, doc.Sources, 2, "the overlay still applied, it just changed nothing")
}

// TestCompile_OverlayIsNotReadFromDisk pins the fourth acceptance criterion, and
// the compiler contract behind it: compilers.Source is the whole input and the
// caller loads the bytes, so an overlay Path is a label rather than a handle.
//
// It observes the behaviour rather than reading the code for it. A file sits at
// the named path holding an overlay that would add a different property, so a
// compiler that opened it would produce a visibly different document — and the
// path is one the process can genuinely read, which a nonexistent path would not
// prove.
func TestCompile_OverlayIsNotReadFromDisk(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "patch.yaml")
	onDisk := `overlay: 1.0.0
info: {title: Decoy, version: "1"}
actions:
  - target: $.components.schemas.Pet.properties
    update:
      fromDisk: {type: string}
`
	require.NoError(t, os.WriteFile(path, []byte(onDisk), 0o600))

	doc, _ := compileWith(t, openapi.Options{
		Overlay: &openapi.Overlay{Path: path, Data: []byte(addTagProperty)},
	})

	assert.Equal(t, path, doc.Sources[1].Path, "the path is recorded")
	for _, def := range doc.Types {
		model, ok := def.(*ir.Model)
		if !ok || model.Name.Source != "Pet" {
			continue
		}
		for _, p := range model.Properties {
			assert.NotEqual(t, "fromDisk", p.Name.Source, "the file at that path was read")
		}
	}
}

// TestCompile_RejectsAnOverlayThatIntroducesAReferenceCycle pins the refusals
// that run before the parser against the tree the parser is actually handed.
//
// The spec on its own is acyclic and the overlay is what closes the loop, so the
// pre-parse scan over the source bytes cannot see it. Without a second scan over
// the patched tree the cycle reaches the third-party resolver, which is the
// crash those refusals exist to prevent.
func TestCompile_RejectsAnOverlayThatIntroducesAReferenceCycle(t *testing.T) {
	t.Parallel()
	const acyclic = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: '#/components/schemas/B'}
    B: {type: string}
`
	const closesTheLoop = `overlay: 1.0.0
info: {title: Patch, version: "1"}
actions:
  - target: $.components.schemas.B
    update: {$ref: '#/components/schemas/A'}
`
	clean, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(acyclic)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, clean, "the source alone is acyclic, so the scan has nothing to find")

	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(acyclic)}},
		compilers.Options{FormatOptions: openapi.Options{
			Overlay: &openapi.Overlay{Data: []byte(closesTheLoop)},
		}})

	require.NoError(t, err)
	assert.Nil(t, doc, "the patched document is refused before it reaches the resolver")

	// The code, not merely the presence of an error: an overlay has several ways
	// to fail, and a refusal for any of the others would satisfy a bare
	// HasError while leaving the cycle this test is named for unexercised.
	found, _ := ir.FirstError(diags)
	assert.Equal(t, diag.CyclicRef, found.Code, "the introduced cycle is what refused it: %+v", diags)
}

// provenanceSpec and provenancePatch exercise GitHub #522's fix on every
// annotation-package call path a clean 3.1 document reaches: component schemas
// and a property of one, an operation and its externalDocs, a response with its
// header, media type and example, a request body and its encoding, a parameter
// and its schema, both security schemes and an OAuth flow, the document's info
// and servers, and a path item the overlay mounts that no operation reaches.
// The call paths only a 3.0 document or a failing read reaches are
// TestCompile_OverlayIsCreditedWithAKeptModifierAndAnUnkeptBranchSet's.
const provenanceSpec = `openapi: 3.1.0
info:
  title: t
  version: "1"
servers:
  - url: https://{env}.example
    variables:
      env: {default: api}
paths:
  /pets:
    get:
      operationId: listPets
      externalDocs: {url: https://docs.example}
      parameters:
        - name: limit
          in: query
          schema: {type: integer}
      responses:
        '200':
          description: ok
          headers:
            X-Rate: {schema: {type: integer}}
          content:
            application/json:
              schema: {type: string}
              examples:
                one: {value: one}
    post:
      operationId: addPet
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file: {type: string, format: binary}
            encoding:
              file: {contentType: application/octet-stream}
      responses:
        '204': {description: ok}
components:
  schemas:
    Pet:
      type: object
      x-base: 1
      x-rw: 1
      x-obj: {a: 1}
      x-gone: 1
      properties:
        name: {type: string}
  securitySchemes:
    apiKey:
      type: apiKey
      name: X-Key
      in: header
    oauth:
      type: oauth2
      flows:
        implicit:
          authorizationUrl: https://auth.example
          scopes: {}
`

const provenancePatch = `overlay: 1.0.0
info: {title: o, version: "1"}
actions:
  - target: $.components.schemas.Pet
    update:
      x-added: 1
      x-rw: 2
      x-obj: {b: 2}
      not: {type: integer}
      if: {required: [name]}
      then: {required: [tag]}
      dependentSchemas: {name: {required: [name]}}
      frobnicate: 1
      $id: "https://example.com/pet"
      properties:
        tag: {type: string}
  - target: $.components.schemas.Pet['x-gone']
    remove: true
  - target: $.components.schemas.Pet
    update:
      x-gone: 1
  - target: $.paths['/pets'].get.responses['200']
    update:
      bogus: 1
      x-resp: 1
  - target: $.paths['/pets'].get.responses['200'].content['application/json']
    update:
      mediaBogus: 1
  - target: $.paths['/pets'].get.parameters[0]
    update:
      paramBogus: 1
  - target: $.paths['/pets'].get
    update:
      x-op: 1
      opBogus: 1
  - target: $.components.securitySchemes.apiKey
    update:
      x-scheme: 1
      schemeBogus: 1
      bearerFormat: JWT
  - target: $.info
    update:
      x-info: 1
      infoBogus: 1
  - target: $.paths
    update:
      /empty:
        x-pi: 1
        piBogus: {responses: {'200': {description: ok}}}
        servers: [{url: "https://e.example"}]
  - target: $.paths['/pets'].get.externalDocs
    update:
      edBogus: 1
  - target: $.paths['/pets'].get.parameters[0].schema
    update:
      x-ps: 1
  - target: $.paths['/pets'].get.responses['200'].headers['X-Rate']
    update:
      headerBogus: 1
  - target: $.paths['/pets'].get.responses['200'].content['application/json'].examples.one
    update:
      exBogus: 1
  - target: $.paths['/pets'].post.requestBody
    update:
      rbBogus: 1
  - target: $.paths['/pets'].post.requestBody.content['multipart/form-data'].encoding.file
    update:
      encBogus: 1
  - target: $.components.schemas.Pet.properties.name
    update:
      x-prop: 1
  - target: $.components.schemas
    update:
      Closed: {allOf: [{type: object}, false]}
      Narrowed:
        type: object
        properties: {a: {type: string}, b: {type: string}}
        oneOf: [{required: [a]}, {required: [b]}]
  - target: $.components.securitySchemes.oauth.flows
    update:
      x-flows: 1
      flowsBogus: 1
  - target: $.components.securitySchemes.oauth.flows.implicit
    update:
      x-flow: 1
      flowBogus: 1
  - target: $.servers[0]
    update:
      x-srv: 1
      srvBogus: 1
  - target: $.servers[0].variables.env
    update:
      x-var: 1
      varBogus: 1
`

// unmodeledEntriesByPath walks the whole compiled document for every
// ir.UnmodeledEntry it holds, keyed by the full path the walk reached it at.
// Deriving the set from the value graph, rather than naming each carrier, is
// what keeps the table below honest about which map an entry actually landed
// on instead of merely one it could have.
func unmodeledEntriesByPath(doc *ir.Document) map[string]ir.UnmodeledEntry {
	entryType := reflect.TypeFor[ir.UnmodeledEntry]()
	out := map[string]ir.UnmodeledEntry{}
	ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Type() == entryType && v.CanInterface() {
			out[path] = v.Interface().(ir.UnmodeledEntry)
		}
		return true
	})
	return out
}

// entryKeyed returns the one Unmodeled entry named key, wherever in the
// document the walk found it: the path down to it is an implementation detail
// this test does not pin, only the map key the reader that wrote it chose.
func entryKeyed(t *testing.T, entries map[string]ir.UnmodeledEntry, key string) ir.UnmodeledEntry {
	t.Helper()
	suffix := "[" + key + "]"
	var at string
	for path := range entries {
		if !strings.HasSuffix(path, suffix) {
			continue
		}
		require.Empty(t, at, "Unmodeled key %q found at two paths: %q and %q", key, at, path)
		at = path
	}
	require.NotEmpty(t, at, "no Unmodeled entry keyed %q anywhere in the document", key)
	return entries[at]
}

// diagKey identifies a diagnostic by code and source pointer. The pair is
// unique in these fixtures except at an unmounted path item, whose servers-kept
// note and no-operation warning share both, so diagnosticsByCodeAndPointer
// keeps every diagnostic found at a key instead of only the last.
type diagKey struct {
	code    string
	pointer jsontext.Pointer
}

// diagnosticsByCodeAndPointer groups diags by diagKey, in the order Compile
// returned them.
func diagnosticsByCodeAndPointer(diags []ir.Diagnostic) map[diagKey][]ir.Diagnostic {
	out := map[diagKey][]ir.Diagnostic{}
	for _, d := range diags {
		k := diagKey{d.Code, d.Provenance.Pointer}
		out[k] = append(out[k], d)
	}
	return out
}

// TestCompile_OverlayIsCreditedWithTheKeysItAdds pins GitHub #522 end to end.
// The annotation package's readers sit below lowering.Ctx in the import graph
// and used to be handed the base document's raw source index rather than the
// context's own attribution, so an extension, an unknown key, a dialect
// keyword or a validation-only keyword an overlay added claimed to come from
// the base document it patched — and so did the diagnostics announcing them.
//
// Every row here names the production call path it exercises, so that
// reverting the fix at any one of them reddens the row beside it. The
// controls (x-base, x-obj) must stay with the base: crediting the overlay with
// everything it touches, rather than with what it introduced or rewrote, would
// be the opposite defect. The if-then-else row is not a control. It pins a
// known gap: a combined entry takes the schema's attribution although the
// overlay wrote every keyword in it, and fixing that (GitHub #534) flips it.
func TestCompile_OverlayIsCreditedWithTheKeysItAdds(t *testing.T) {
	t.Parallel()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(provenanceSpec)}},
		compilers.Options{FormatOptions: openapi.Options{
			Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(provenancePatch)},
		}})
	require.NoError(t, err)
	require.NotNil(t, doc, "compile refused: %+v", diags)
	assert.False(t, ir.HasError(diags), "the overlay applies cleanly: %+v", diags)
	for _, d := range diags {
		assert.NotEqual(t, diag.OverlayAction, d.Code,
			"an action that silently matched nothing would hollow this test out: %+v", d)
	}
	assert.Empty(t, irverify.Verify(doc), "the fixture stays structurally valid")

	entries := unmodeledEntriesByPath(doc)
	for _, tc := range []struct {
		name string
		key  string
		want int
	}{
		{"schema extension the overlay adds (schema.go attachDeclaredAnnotations)", "openapi:x-added", 1},
		{"schema extension the base declares, control", "openapi:x-base", 0},
		{"scalar extension the overlay rewrites in place", "openapi:x-rw", 1},
		{"mapping extension the overlay only merges into", "openapi:x-obj", 0},
		{"extension removed then re-added by a later action", "openapi:x-gone", 1},
		{"validation-only keyword the overlay adds (schema.go attachDeclaredAnnotations)", "openapi:not", 1},
		{"a second validation-only keyword added beside it", "openapi:dependentSchemas", 1},
		{"combined if/then/else entry keeps the schema's attribution (GitHub #534)", "openapi:if-then-else", 0},
		{"dialect keyword the overlay adds", "openapi:$id", 1},
		{"unknown schema keyword the overlay adds (accumulate.go PreserveUnknownKeywords)", "openapi:frobnicate", 1},
		{"property extension (schema.go fillPropertyAnnotations)", "openapi:x-prop", 1},
		{"false allOf branch (compose.go applyFalseBranches)", "openapi:allOf/1", 1},
		{"validation-only oneOf beside a model (schema.go preserveBranchSets)", "openapi:oneOf", 1},
		{"operation extension (operations.go applyOperationAnnotations)", "openapi:x-op", 1},
		{"operation unknown key (operations.go applyOperationAnnotations)", "openapi:opBogus", 1},
		{"externalDocs unknown key (operations.go applyOperationAnnotations)", "openapi:externalDocs/edBogus", 1},
		{"response unknown key (operations.go preserveResponseExtras)", "openapi:bogus", 1},
		{"response extension (operations.go preserveResponseExtras)", "openapi:x-resp", 1},
		{"media type unknown key (content.go lowerContent)", "openapi:mediaBogus", 1},
		{"header unknown key (content.go applyHeaderAnnotations)", "openapi:headerBogus", 1},
		{"example unknown key (content.go appendPluralExample)", "openapi:exBogus", 1},
		{"request body unknown key (content.go lowerRequestBody)", "openapi:rbBogus", 1},
		{"encoding unknown key (content.go encodingUnmodeled)", "openapi:encoding/file/encBogus", 1},
		{"parameter unknown key (params.go fillParamDetail)", "openapi:paramBogus", 1},
		{"parameter schema extension (params.go fillParamSchemaAnnotations)", "openapi:x-ps", 1},
		{"security scheme extension (auth.go applySchemeAnnotations)", "openapi:x-scheme", 1},
		{"security scheme unknown key (auth.go lowerSecurityScheme)", "openapi:schemeBogus", 1},
		{"field the scheme's type leaves unread (auth.go preserveUnreadFields)", "openapi:bearerFormat", 1},
		{"OAuth flows extension (auth.go applySchemeAnnotations)", "openapi:flows/x-flows", 1},
		{"OAuth flows unknown key (auth.go applySchemeAnnotations)", "openapi:flows/flowsBogus", 1},
		{"OAuth flow extension (auth.go applyFlowAnnotations)", "openapi:x-flow", 1},
		{"OAuth flow unknown key (auth.go applyFlowAnnotations)", "openapi:flowBogus", 1},
		{"document info extension (meta.go documentExtensions)", "openapi:info/x-info", 1},
		{"document info unknown key (meta.go documentUnknownKeys)", "openapi:info/infoBogus", 1},
		{"server extension (meta.go lowerServer)", "openapi:x-srv", 1},
		{"server unknown key (meta.go lowerServer)", "openapi:srvBogus", 1},
		{"server variable extension (meta.go serverVariables)", "openapi:x-var", 1},
		{"server variable unknown key (meta.go serverVariables)", "openapi:varBogus", 1},
		{"unmounted path item's servers (operations.go applyPathServers)", "openapi:pathItem/paths/~1empty/servers", 1},
		{"unmounted path item's own extension (operations.go applyPathItem)", "openapi:pathItem/paths/~1empty/x-pi", 1},
		{"unmounted path item's undeclared key (operations.go applyPathItem)", "openapi:pathItem/paths/~1empty/piBogus", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := entryKeyed(t, entries, tc.key)
			assert.Equal(t, tc.want, entry.Provenance.Source, tc.key)
		})
	}

	byCodeAndPointer := diagnosticsByCodeAndPointer(diags)
	for _, tc := range []struct {
		name    string
		code    string
		pointer jsontext.Pointer
		want    int
	}{
		{"dialect keyword diagnostic", diag.DegradedConstruct, "/components/schemas/Pet/$id", 1},
		{"unknown schema keyword diagnostic", diag.UnknownSchemaKeyword, "/components/schemas/Pet/frobnicate", 1},
		{"response unknown key diagnostic", diag.UnknownObjectKey, "/paths/~1pets/get/responses/200/bogus", 1},
		{"media type unknown key diagnostic", diag.UnknownObjectKey,
			"/paths/~1pets/get/responses/200/content/application~1json/mediaBogus", 1},
		{"parameter unknown key diagnostic", diag.UnknownObjectKey,
			"/paths/~1pets/get/parameters/0/paramBogus", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			list := byCodeAndPointer[diagKey{tc.code, tc.pointer}]
			require.Len(t, list, 1)
			assert.Equal(t, tc.want, list[0].Provenance.Source)
		})
	}

	t.Run("validation-only announcements stay at the enclosing schema", func(t *testing.T) {
		// not, if/then/else and dependentSchemas each announce once, all
		// located at the schema declaration rather than at their own
		// keyword — the enclosing-object rule — even though the overlay
		// added every keyword they announce.
		validationOnly := byCodeAndPointer[diagKey{diag.ValidationOnlyKeyword, "/components/schemas/Pet"}]
		require.Len(t, validationOnly, 3)
		for _, d := range validationOnly {
			assert.Equal(t, 0, d.Provenance.Source, "%s", d.Message)
		}
	})

	t.Run("both diagnostics an unmounted path item draws name the overlay", func(t *testing.T) {
		// Before the fix, onNearestNode stamped the mount point's carrier
		// with the raw source index while the diagnostic beside it, built
		// through the lowering context, correctly named the overlay — so
		// the servers-kept note and the no-operation warning disagreed
		// about which document drew an entirely overlay-introduced path
		// item.
		unmounted := byCodeAndPointer[diagKey{diag.DegradedConstruct, "/paths/~1empty"}]
		require.Len(t, unmounted, 2)
		for _, d := range unmounted {
			assert.Equal(t, 1, d.Provenance.Source, "%s", d.Message)
		}
	})
}

// TestCompile_OverlayIsCreditedWithAKeptModifierAndAnUnkeptBranchSet covers the
// call paths provenanceSpec cannot reach. annotation.Constraints keeps a 3.0
// exclusiveMinimum or exclusiveMaximum that has no bound beside it to modify,
// which only a 3.0 document reads as a modifier: for a component schema
// (schema.go schemaConstraints) and for a parameter's schema (params.go
// fillParamSchema). And a oneOf that will not render is reported rather than
// kept (schema.go preserveBranchSets), which is an error a clean fixture cannot
// carry.
func TestCompile_OverlayIsCreditedWithAKeptModifierAndAnUnkeptBranchSet(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /n:
    get:
      operationId: getN
      parameters:
        - name: lo
          in: query
          schema: {type: number}
      responses:
        '200': {description: ok}
components:
  schemas:
    Num: {type: number}
    Str: {type: string}
`
	const patch = `overlay: 1.0.0
info: {title: o, version: "1"}
actions:
  - target: $.components.schemas.Num
    update:
      exclusiveMinimum: true
  - target: $.paths['/n'].get.parameters[0].schema
    update:
      exclusiveMaximum: true
  - target: $.components.schemas.Str
    update:
      oneOf: [{enum: [.nan]}]
`
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(spec)}},
		compilers.Options{FormatOptions: openapi.Options{
			Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(patch)},
		}})
	require.NoError(t, err)
	require.NotNil(t, doc, "compile refused: %+v", diags)
	for _, d := range diags {
		assert.NotEqual(t, diag.OverlayAction, d.Code, "every action must land: %+v", d)
	}

	entries := unmodeledEntriesByPath(doc)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/schemas/Num/exclusiveMinimum"},
		entryKeyed(t, entries, "openapi:exclusiveMinimum").Provenance,
		"a component schema's kept modifier (schema.go schemaConstraints)")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/paths/~1n/get/parameters/0/schema/exclusiveMaximum"},
		entryKeyed(t, entries, "openapi:exclusiveMaximum").Provenance,
		"a parameter schema's kept modifier (params.go fillParamSchema)")

	unkept := diagnosticsByCodeAndPointer(diags)[diagKey{diag.UnpreservableConstruct, "/components/schemas/Str/oneOf"}]
	require.Len(t, unkept, 1, "the oneOf that will not render is reported: %+v", diags)
	assert.Equal(t, 1, unkept[0].Provenance.Source,
		"a branch set the overlay added is reported as the overlay's (schema.go preserveBranchSets)")
}

// addPathReusingTheBaseOperationID overlays a second path onto overlaySpec,
// giving it the operationId the base already declares on /pets. The base
// writes "listPets" once; this overlay writes a second declaration of it, so
// the two must conflict rather than merely remount one declaration.
//
// The new path is named /z rather than the /b a hand-drawn example might reach
// for. Declarations are ordered by the pointer each is written at, and /z sorts
// after /pets where /b would sort before it, which would put the conflict on the
// base's own /pets. /z is what pins it to the declaration the overlay wrote.
const addPathReusingTheBaseOperationID = `overlay: 1.0.0
info: {title: Patch, version: "1"}
actions:
  - target: $.paths
    update:
      /z:
        get:
          operationId: listPets
          responses:
            '200': {description: ok}
`

// operationIDFindings returns the operationId diagnostics in diags, keyed by code.
func operationIDFindings(diags []ir.Diagnostic) map[string][]ir.Diagnostic {
	found := map[string][]ir.Diagnostic{}
	for _, d := range diags {
		if d.Code == diag.ConflictingOperationID || d.Code == diag.DuplicateOperationID {
			found[d.Code] = append(found[d.Code], d)
		}
	}
	return found
}

// TestCompile_OverlayAddingAConflictingOperationIDIsAnError pins where the
// conflict lands when the overlay itself writes the second declaration: the
// pointer the overlay introduced, attributed to the overlay's own source
// index rather than the base spec's.
func TestCompile_OverlayAddingAConflictingOperationIDIsAnError(t *testing.T) {
	t.Parallel()
	doc, diags := compileWith(t, openapi.Options{
		Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(addPathReusingTheBaseOperationID)},
	})
	require.NotNil(t, doc)

	found := operationIDFindings(diags)
	require.Len(t, found[diag.ConflictingOperationID], 1, "one second declaration of listPets: %+v", diags)
	conflict := found[diag.ConflictingOperationID][0]
	assert.Equal(t, ir.SeverityError, conflict.Severity)
	assert.Equal(t, 1, conflict.Provenance.Source, "attributed to the overlay that wrote the second declaration")
	assert.Equal(t, jsontext.Pointer("/paths/~1z/get"), conflict.Provenance.Pointer)
	assert.Empty(t, found[diag.DuplicateOperationID], "nothing is mounted twice")
}

// copyPetsPathIntoZ models an Overlay 1.1 `copy` action reusing /pets'
// operationId under a new path. copy merges its source into whatever the
// target already selects rather than minting the key itself, so the first
// action makes an empty mapping at /z for the second to merge into; /z is
// named for the same ordering reason addPathReusingTheBaseOperationID is.
const copyPetsPathIntoZ = `overlay: 1.1.0
info: {title: Patch, version: "1"}
actions:
  - target: $.paths
    update:
      /z: {}
  - target: $.paths['/z']
    copy: $.paths['/pets']
`

// addPostToEveryPath adds a path, then gives every path, the base's and its
// own, a post carrying one operationId: one action writing the id at two
// targets.
const addPostToEveryPath = `overlay: 1.0.0
info: {title: Patch, version: "1"}
actions:
  - target: $.paths
    update:
      /z: {}
  - target: $.paths.*
    update:
      post:
        operationId: createPet
        responses:
          '200': {description: ok}
`

// TestCompile_OverlayWritingAnIDTwiceIsAConflictNotARemount pins the two ways
// an overlay writes one operation into several places against the alias
// reading, which would take each for one declaration mounted twice. An
// Overlay 1.1 `copy` and an `update` whose target selects several nodes both
// clone what they write, so each place gets a node of its own as well as a
// declaration pointer of its own. The overlay has written a second declaration
// into the document it produces, the conflict a hand-written one would be.
func TestCompile_OverlayWritingAnIDTwiceIsAConflictNotARemount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, overlay string
		at            jsontext.Pointer
	}{
		{name: "a copy of a path item", overlay: copyPetsPathIntoZ, at: "/paths/~1z/get"},
		{name: "an update applied to two paths", overlay: addPostToEveryPath, at: "/paths/~1z/post"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileWith(t, openapi.Options{
				Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(tc.overlay)},
			})
			require.NotNil(t, doc)

			found := operationIDFindings(diags)
			require.Len(t, found[diag.ConflictingOperationID], 1, "a second declaration: %+v", diags)
			conflict := found[diag.ConflictingOperationID][0]
			assert.Equal(t, ir.SeverityError, conflict.Severity)
			assert.Equal(t, 1, conflict.Provenance.Source, "attributed to the overlay that wrote it")
			assert.Equal(t, tc.at, conflict.Provenance.Pointer)
			assert.Empty(t, found[diag.DuplicateOperationID], "and not a remount of one declaration")
		})
	}
}
