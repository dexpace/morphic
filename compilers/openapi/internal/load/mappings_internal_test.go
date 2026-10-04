package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// mappingSpec is a source whose component schemas are schemas, and whose
// other top-level entries are rest, which follows them.
func mappingSpec(schemas, rest string) string {
	return openapitest.ComponentSpec(schemas) + rest
}

// petMapping is a Pet base whose discriminator maps each tag to its target.
func petMapping(mapping string) string {
	return "    Pet:\n      type: object\n      discriminator: {propertyName: k, mapping: {" + mapping + "}}\n"
}

// loadTargets loads spec at path, external references allowed or not, and
// requires it to load.
func loadTargets(t *testing.T, path, spec string, external bool) (*Document, []ir.Diagnostic) {
	t.Helper()
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(spec)},
		Options{AllowExternalRefs: external})
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)
	return got, diags
}

// describedAs returns the description of the schema at pointer in targets, or
// "" for none, which is how a test tells which declaration it was built from.
func describedAs(targets MappingTargets, pointer jsontext.Pointer) string {
	return targets.At(pointer).GetSchema().GetDescription()
}

// TestMappings_ATargetHeldAsRawYAMLIsResolvedAtLoad pins GitHub #757 at its
// source: each mapping target the model holds as raw YAML is resolved, as a
// $ref to it is, whether or not any $ref names it. Each row names it from a
// discriminator the lowering can meet: in the model, in what a $ref reaches,
// in a response a $ref reaches, and in another such target.
func TestMappings_ATargetHeldAsRawYAMLIsResolvedAtLoad(t *testing.T) {
	t.Parallel()
	const lib = `x-lib:
  Cat: {description: cat, type: object}
  Inner:
    type: object
    discriminator: {propertyName: k, mapping: {d: '#/x-lib/Dog'}}
  Dog: {description: dog, type: object}
  Resp:
    description: ok
    content:
      application/json:
        schema: {type: object, discriminator: {propertyName: k, defaultMapping: '#/x-lib/Dog'}}
`
	const toResp = "paths:\n  /a:\n    get:\n      responses:\n        \"200\": {$ref: '#/x-lib/Resp'}\n"
	for _, c := range []struct {
		name, schemas string
		paths         string // the document's paths, when not empty
		pointer       jsontext.Pointer
		want          string
	}{
		{"an extension's value, named by a mapping alone", petMapping("c: '#/x-lib/Cat'"), "", "/x-lib/Cat", "cat"},
		{"an enum member",
			petMapping("e: '#/components/schemas/Kennel/properties/e/enum/0'") +
				"    Kennel: {type: object, properties: {e: {enum: [{description: member, type: object}]}}}\n",
			"", "/components/schemas/Kennel/properties/e/enum/0", "member"},
		{"a mapping in what a $ref reaches", "    Holder: {$ref: '#/x-lib/Inner'}\n", "", "/x-lib/Dog", "dog"},
		{"a mapping in another target", petMapping("i: '#/x-lib/Inner'"), "", "/x-lib/Dog", "dog"},
		{"a defaultMapping in a response a $ref reaches", "    Pet: {type: object}\n", toResp, "/x-lib/Dog", "dog"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			spec := mappingSpec(c.schemas, lib)
			if c.paths != "" {
				spec = strings.Replace(spec, "paths: {}\n", c.paths, 1)
			}
			got, diags := loadTargets(t, "spec.yaml", spec, false)

			assert.Empty(t, diags)
			assert.Equal(t, c.want, describedAs(got.Targets, c.pointer))
		})
	}
}

// TestMappings_OnlyATargetInTheSourceIsResolved pins which targets the load
// phase resolves: those the lowering reads as naming a position in the source,
// spelled internally or by the source's file name, whether or not external
// references are allowed. A spelling through a directory is another document
// to the lowering (GitHub #576). A component's name is the component's, an
// anchor or the whole document names no pointer, and another document is not
// read for a mapping: the finding in its Cat would be reported if it were.
func TestMappings_OnlyATargetInTheSourceIsResolved(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": "x-lib:\n  Cat: {type: object, minLength: abc}\n"})
	path := filepath.Join(dir, "root.yaml")
	for _, c := range []struct {
		name, target string
		external     bool
		resolved     bool
	}{
		{"an internal pointer", "#/x-lib/Cat", false, true},
		{"the source's file name, read as the source", "root.yaml#/x-lib/Cat", true, true},
		{"the source's file name, with external references disallowed", "root.yaml#/x-lib/Cat", false, true},
		{"the source's file name through a directory", "./root.yaml#/x-lib/Cat", true, false},
		{"another document", "other.yaml#/x-lib/Cat", true, false},
		{"an anchor", "#cat", false, false},
		{"the whole source, by its file name", "root.yaml", true, false},
		{"the whole source, internally", "#", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			spec := mappingSpec(petMapping("c: '"+c.target+"'"), "x-lib:\n  Cat: {description: cat, type: object}\n")
			got, diags := loadTargets(t, path, spec, c.external)

			assert.Empty(t, diags, "nothing is reported, and no other document is read")
			assert.Equal(t, c.resolved, got.Targets.At("/x-lib/Cat") != nil)
		})
	}

	t.Run("a definition, however spelled", func(t *testing.T) {
		t.Parallel()
		// The document-rooted $defs is no definition the mapping names: the
		// lowering reads a $defs value relative to the discriminator. Resolving
		// it here reported the finding in it at the mapping entry.
		for _, target := range []string{"#/$defs/X", "root.yaml#/$defs/X"} {
			spec := mappingSpec("    Pet:\n      type: object\n"+
				"      discriminator: {propertyName: k, mapping: {x: '"+target+"'}}\n"+
				"      $defs: {X: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object}}\n",
				"$defs:\n  X: {type: object, minLength: abc}\n")
			got, diags := loadTargets(t, path, spec, true)
			assert.Empty(t, diags, target)
			assert.Nil(t, got.Targets.At("/$defs/X"), target)
		}
	})

	t.Run("a declared component's name", func(t *testing.T) {
		t.Parallel()
		spec := mappingSpec(petMapping("c: '#/x-lib/Cat'")+"    '#/x-lib/Cat': {type: object}\n",
			"x-lib:\n  Cat: {description: cat, type: object}\n")
		got, diags := loadTargets(t, path, spec, false)
		assert.Empty(t, diags)
		assert.Nil(t, got.Targets.At("/x-lib/Cat"), "the name is the component's, not the pointer's")
	})
}

// TestMappings_ATargetThatDoesNotResolveIsLeftToTheLowering pins that the load
// phase records nothing for a target that does not resolve, and reports
// nothing either: the lowering reports every target it cannot resolve.
func TestMappings_ATargetThatDoesNotResolveIsLeftToTheLowering(t *testing.T) {
	t.Parallel()
	got, diags := loadTargets(t, "spec.yaml", mappingSpec(petMapping("m: '#/x-lib/Missing'"), "x-lib: {}\n"), false)
	assert.Empty(t, diags)
	assert.Nil(t, got.Targets.At("/x-lib/Missing"))
}

// TestMappings_AFindingInATargetIsReportedOnce pins where what resolving a
// target draws is reported: at the mapping entry, once, or at the least of the
// sites that reach the target when a $ref reaches it too, in either order.
func TestMappings_AFindingInATargetIsReportedOnce(t *testing.T) {
	t.Parallel()
	const lib = "x-lib:\n  Bad: {type: object, minLength: abc}\n"
	const finding = " openapi/validation/validation-type-mismatch"
	pet := petMapping("b: '#/x-lib/Bad'")
	holder := "    A: {$ref: '#/x-lib/Bad'}\n"
	for _, c := range []struct {
		name string
		docs []string
		want []string
	}{
		{"named by a mapping alone", []string{mappingSpec(pet, lib)},
			[]string{"/components/schemas/Pet/discriminator/mapping/b" + finding}},
		{"named by a $ref too, at the lesser site", []string{mappingSpec(pet+holder, lib), mappingSpec(holder+pet, lib)},
			[]string{"/components/schemas/A" + finding}},
	} {
		for _, spec := range c.docs {
			_, diags := loadTargets(t, "spec.yaml", spec, false)
			assert.Empty(t, cmp.Diff(c.want, diagLines(diags)), c.name)
		}
	}
}

// TestMappings_AFindingIsPlacedWhateverTheEntryOrder pins that every entry
// naming a target takes part in placing what resolving it draws: the finding
// lands at the least of their sites, in either declaration order, though only
// the first entry resolved draws it.
func TestMappings_AFindingIsPlacedWhateverTheEntryOrder(t *testing.T) {
	t.Parallel()
	const lib = "x-lib:\n  Bad: {type: object, minLength: abc}\n"
	a := strings.Replace(petMapping("b: '#/x-lib/Bad'"), "Pet:", "A:", 1)
	z := strings.Replace(petMapping("b: '#/x-lib/Bad'"), "Pet:", "Z:", 1)
	want := []string{"/components/schemas/A/discriminator/mapping/b openapi/validation/validation-type-mismatch"}
	for _, schemas := range []string{a + z, z + a} {
		_, diags := loadTargets(t, "spec.yaml", mappingSpec(schemas, lib), false)
		assert.Empty(t, cmp.Diff(want, diagLines(diags)))
	}
}

// TestMappings_AnEntryInABuiltObjectIsSitedAtItsKey pins where an entry in an
// object built from raw YAML is placed: under the key as written. The resolver
// records the pointer it reached decoded already, so decoding it again read a
// '+' in the key as a space and '%41' as 'A', siting the finding at a pointer
// that names nothing. Each row reaches the object by a mapping and by a $ref.
func TestMappings_AnEntryInABuiltObjectIsSitedAtItsKey(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ key, spelled string }{{"a+b", "a%2Bb"}, {"a%41", "a%2541"}} {
		lib := "x-lib:\n  '" + c.key + "':\n    type: object\n" +
			"    discriminator: {propertyName: k, mapping: {bad: '#/x-lib/Bad'}}\n" +
			"  Bad: {type: object, minLength: abc}\n"
		want := []string{"/x-lib/" + c.key + "/discriminator/mapping/bad openapi/validation/validation-type-mismatch"}
		for _, schemas := range []string{petMapping("r: '#/x-lib/" + c.spelled + "'"),
			"    Holder: {$ref: '#/x-lib/" + c.spelled + "'}\n"} {
			_, diags := loadTargets(t, "spec.yaml", mappingSpec(schemas, lib), false)
			assert.Empty(t, cmp.Diff(want, diagLines(diags)), "%s: %s", c.key, schemas)
		}
	}
}

// TestMappings_EverySpellingReachesOneObject pins that two spellings of one
// position resolve to one object, the one an internal $ref to it is cached as,
// in either order. Resolved as written, the file name spelling built an object
// of its own when it came first.
func TestMappings_EverySpellingReachesOneObject(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "root.yaml")
	internal, byFile := "a: '#/x-lib/Cat'", "b: 'root.yaml#/x-lib/Cat'"
	for _, mapping := range []string{internal + ", " + byFile, byFile + ", " + internal} {
		got, diags := loadTargets(t, path,
			mappingSpec(petMapping(mapping), "x-lib:\n  Cat: {description: cat, type: object}\n"), true)
		require.Empty(t, diags)

		cached, ok := got.Doc.GetCachedReferencedObject(path + "#/x-lib/Cat")
		require.True(t, ok, "the internal spelling's object is cached under the source's key")
		assert.Same(t, cached, got.Targets.At("/x-lib/Cat"), mapping)
	}
}

// TestMappings_TheTargetsAreTheLastResolutions pins that a second resolution's
// targets are what Load returns: the first's belong to a model the rebuild
// replaced, and the lowering reads the one returned.
func TestMappings_TheTargetsAreTheLastResolutions(t *testing.T) {
	t.Parallel()
	trigger, _ := countingServer(t, anchoredExternalDoc)
	spec := mappingSpec(petMapping("k: '#/components/schemas/Kennel'")+"    Kennel: {type: object}\n", "")
	spec = strings.Replace(spec, "paths: {}\n",
		"paths:\n  /x: {$ref: \""+respelled(trigger.URL, "HTTP")+"/ext.yaml#/paths/~1x\"}\n", 1)
	data := []byte(spec)
	root, _, err := decodeStream(data)
	require.NoError(t, err)
	releaseAnchors(root)
	doc, _, err := unmarshal(t.Context(), data, root)
	require.NoError(t, err)
	rebuilt := false
	rebuild := func() (*soa.OpenAPI, error) {
		rebuilt = true
		again, _, err := unmarshal(t.Context(), data, root)
		return again, err
	}

	resolved, targets, _, err := resolveExternal(t.Context(), pointerAt(0, overlay.Origin{}), doc,
		newSourceDocument("root.yaml", data, root, nil), Options{AllowExternalRefs: true}, rebuild)

	require.NoError(t, err)
	require.True(t, rebuilt, "the fixture is resolved twice")
	kennel, ok := resolved.Components.Schemas.Get("Kennel")
	require.True(t, ok)
	assert.Same(t, kennel, targets.At("/components/schemas/Kennel"), "the target is the returned model's own")
}

// TestMappings_APanicIsReportedAtTheWorkRunning pins resolve's barrier: a panic
// stops it, as an error naming the reference or entry whose work was running.
func TestMappings_APanicIsReportedAtTheWorkRunning(t *testing.T) {
	t.Parallel()
	m := newMappings(sourceDocument{}, &soa.OpenAPI{}, oas3.ResolveOptions{}, nil)
	m.built = []builtObject{{site: "/x", record: record{object: 1,
		walk: func(context.Context) iter.Seq[soa.WalkItem] { panic("boom") }}}}

	site, err := m.resolve(t.Context(), &reachedFindings{})

	require.Error(t, err)
	assert.Equal(t, "discriminator mapping resolver panicked: boom", err.Error())
	assert.Equal(t, jsontext.Pointer("/x"), site)
}

// TestMappings_ACollectingWalkStopsAtAnError pins collectFrom's early return,
// which only a walk a test builds can reach: Match returns what its callback
// does, and collectFrom's never fails.
func TestMappings_ACollectingWalkStopsAtAnError(t *testing.T) {
	t.Parallel()
	m := newMappings(sourceDocument{}, &soa.OpenAPI{}, oas3.ResolveOptions{}, nil)
	discriminator := &oas3.Discriminator{DefaultMapping: new("#/x")}
	items := func(yield func(soa.WalkItem) bool) {
		failing := soa.WalkItem{Match: func(soa.Matcher) error { return assert.AnError }}
		if !yield(failing) {
			return
		}
		yield(fakeWalkItem("discriminator", discriminator))
	}

	m.collectFrom("/base", items)

	assert.Empty(t, m.entries, "nothing past the failing item is collected")
}

// TestMappings_NoDiscriminatorOutsideTheSourceIsCollected pins that only what
// the source holds is searched for discriminators: one in another document
// maps by that document's pointers, which the source's would misread. Its
// mapping names #/x-lib/Y, which the source declares too, so collecting it
// would record the source's Y.
func TestMappings_NoDiscriminatorOutsideTheSourceIsCollected(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": `components:
  schemas:
    X: {type: object, discriminator: {propertyName: k, mapping: {y: '#/x-lib/Y'}}}
x-lib:
  Y: {type: object}
`})
	path := filepath.Join(dir, "root.yaml")
	const lib = "x-lib:\n  Y: {description: the source's, type: object}\n"
	for name, schemas := range map[string]string{
		"reached by a $ref": "    A: {$ref: './other.yaml#/components/schemas/X'}\n",
		"reached by a mapping": petMapping("e: '#/components/schemas/Ext'") +
			"    Ext: {$ref: './other.yaml#/components/schemas/X'}\n",
	} {
		got, diags := loadTargets(t, path, mappingSpec(schemas, lib), true)
		assert.Empty(t, diags, name)
		assert.Nil(t, got.Targets.At("/x-lib/Y"), name)
	}
}
