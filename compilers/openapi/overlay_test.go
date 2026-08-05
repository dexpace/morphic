package openapi_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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
