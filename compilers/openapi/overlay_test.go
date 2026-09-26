package openapi_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
	assert.Equal(t, "/components/schemas/Pet/properties/tag", introduced.Pointer)

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
	// line:col, several lines below the info block the overlay grows.
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
	sited := func(opts openapi.Options) []string {
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: "spec.yaml", Data: []byte(spec)}},
			compilers.Options{FormatOptions: opts})
		require.NoError(t, err)
		require.NotNil(t, doc)
		var out []string
		for _, d := range diags {
			if d.Provenance.Pointer != "" {
				out = append(out, d.Code+" @ "+d.Provenance.Pointer)
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
// annotation-package call path it touches: a component schema, an operation,
// a response, a media type, a parameter, a security scheme, the document's
// own info block, and a path item the overlay mounts that no operation
// reaches. Pet also carries GitHub #534's combined entries the overlay writes
// entirely (if/then/else, contains); Widget carries the one it only adds to —
// the base declares then and the overlay adds if, deliberately the arm the
// combining reader lists first, so a rule that mistakenly named whichever
// document wrote the first-listed keyword would still read as the overlay's
// rather than surface the disagreement; Tags carries unevaluatedItems alone,
// on the array schema the keyword actually describes.
const provenanceSpec = `openapi: 3.1.0
info:
  title: t
  version: "1"
paths:
  /pets:
    get:
      operationId: listPets
      parameters:
        - name: limit
          in: query
          schema: {type: integer}
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {type: string}
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
    Widget:
      type: object
      then: {required: [w2]}
    Tags:
      type: array
      items: {type: string}
  securitySchemes:
    apiKey:
      type: apiKey
      name: X-Key
      in: header
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
      contains: {type: string}
      minContains: 1
      frobnicate: 1
      $id: "https://example.com/pet"
      properties:
        tag: {type: string}
  - target: $.components.schemas.Pet['x-gone']
    remove: true
  - target: $.components.schemas.Pet
    update:
      x-gone: 1
  - target: $.components.schemas.Widget
    update:
      if: {required: [w1]}
  - target: $.components.schemas.Tags
    update:
      unevaluatedItems: {type: string}
  - target: $.paths['/pets'].get.responses['200']
    update:
      bogus: 1
  - target: $.paths['/pets'].get.responses['200'].content['application/json']
    update:
      mediaBogus: 1
  - target: $.paths['/pets'].get.parameters[0]
    update:
      paramBogus: 1
  - target: $.paths['/pets'].get
    update:
      x-op: 1
  - target: $.components.securitySchemes.apiKey
    update:
      x-scheme: 1
  - target: $.info
    update:
      x-info: 1
  - target: $.paths
    update:
      /empty:
        x-pi: 1
        servers: [{url: "https://e.example"}]
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

// entriesKeyed is entryKeyed for a key the fixture deliberately writes at more
// than one position — Pet's and Widget's if/then/else entries share the map
// key "openapi:if-then-else" since a combined entry has no position of its
// own to fold into the key, so the two are told apart by Provenance.Pointer
// (the declaring schema, per §12) rather than by entryKeyed's one-path rule.
func entriesKeyed(entries map[string]ir.UnmodeledEntry, key string) []ir.UnmodeledEntry {
	suffix := "[" + key + "]"
	var out []ir.UnmodeledEntry
	for path, entry := range entries {
		if strings.HasSuffix(path, suffix) {
			out = append(out, entry)
		}
	}
	return out
}

// diagKey identifies a diagnostic by code and source pointer. The pair is
// unique everywhere except the one place GitHub #522 left mismatched: an
// unmounted path item's servers-kept note and its no-operation warning share
// both a code and a pointer, so diagnosticsByCodeAndPointer keeps every
// diagnostic found at a key instead of only the last.
type diagKey struct{ code, pointer string }

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
// controls (x-base, x-obj) must stay with the base: crediting the overlay
// with everything it touches, rather than with what it introduced or
// rewrote, would be the opposite defect. The if/then/else, contains and
// unevaluated rows extend that to GitHub #534's combined entries, which have
// no position of their own: Pet's is credited to the overlay because the
// overlay wrote every keyword it holds, and the dedicated subtest below adds
// Widget, where the base's own then keeps the entry once the overlay only
// adds an if beside it.
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
		{"schema extension the overlay adds (schema.go Read)", "openapi:x-added", 1},
		{"schema extension the base declares, control", "openapi:x-base", 0},
		{"scalar extension the overlay rewrites in place", "openapi:x-rw", 1},
		{"mapping extension the overlay only merges into", "openapi:x-obj", 0},
		{"extension removed then re-added by a later action", "openapi:x-gone", 1},
		{"validation-only keyword the overlay adds (accumulate.go path)", "openapi:not", 1},
		{"a second validation-only keyword added beside it", "openapi:dependentSchemas", 1},
		{"combined entry the overlay writes in full (contains + minContains)", "openapi:contains", 1},
		{"combined entry on an array schema (unevaluatedItems alone)", "openapi:unevaluated", 1},
		{"dialect keyword the overlay adds", "openapi:$id", 1},
		{"unknown schema keyword the overlay adds", "openapi:frobnicate", 1},
		{"operation extension (operations.go ExtensionsAt)", "openapi:x-op", 1},
		{"response unknown key (operations.go UnknownKeysIn)", "openapi:bogus", 1},
		{"media type unknown key (content.go)", "openapi:mediaBogus", 1},
		{"parameter unknown key (params.go)", "openapi:paramBogus", 1},
		{"security scheme extension (auth.go)", "openapi:x-scheme", 1},
		{"document info extension (meta.go)", "openapi:info/x-info", 1},
		{"unmounted path item's servers (onNearestNode)", "openapi:pathItem/paths/~1empty/servers", 1},
		{"unmounted path item's own extension (onNearestNode)", "openapi:pathItem/paths/~1empty/x-pi", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := entryKeyed(t, entries, tc.key)
			assert.Equal(t, tc.want, entry.Provenance.Source, tc.key)
		})
	}

	t.Run("if/then/else entry Source follows which document wrote every arm", func(t *testing.T) {
		// Pet and Widget both fold if/then/else into one "openapi:if-then-else"
		// entry — the map key that GitHub #534's rule is asked about — so the
		// two are told apart by Provenance.Pointer, which §12 leaves as the
		// declaring schema regardless of the fix.
		byPointer := map[string]ir.UnmodeledEntry{}
		for _, entry := range entriesKeyed(entries, "openapi:if-then-else") {
			byPointer[entry.Provenance.Pointer] = entry
		}
		require.Len(t, byPointer, 2, "Pet's all-overlay entry and Widget's mixed one")

		pet, ok := byPointer["/components/schemas/Pet"]
		require.True(t, ok, "Pet declares no if/then/else itself; both arms are the overlay's")
		assert.Equal(t, 1, pet.Provenance.Source, "every arm the overlay wrote credits the overlay")

		widget, ok := byPointer["/components/schemas/Widget"]
		require.True(t, ok, "Widget declares then itself; the overlay only adds if")
		assert.Equal(t, 0, widget.Provenance.Source,
			"if and then disagree, so the entry stays with the schema that declares it")
	})

	byCodeAndPointer := diagnosticsByCodeAndPointer(diags)
	for _, tc := range []struct {
		name    string
		code    string
		pointer string
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
		// not, if/then/else, dependentSchemas and contains each announce once,
		// all located at the schema declaration rather than at their own
		// keyword or keywords — the enclosing-object rule — even though the
		// overlay wrote every one of them, and two of the four (if/then/else,
		// contains) are now credited as the overlay's own entry above. The
		// note is a diagnostic about the schema, not about the entry, so it
		// keeps the schema's attribution regardless: only the entry moved.
		validationOnly := byCodeAndPointer[diagKey{diag.ValidationOnlyKeyword, "/components/schemas/Pet"}]
		require.Len(t, validationOnly, 4)
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
