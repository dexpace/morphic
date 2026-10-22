package load

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// itemsDeep is a schema nested n levels through items, the shape GitHub #905
// was reported on.
func itemsDeep(n int) string {
	return strings.Repeat("{type: array, items: ", n) + "{type: string}" + strings.Repeat("}", n)
}

// deepSpec is a 3.1 document whose one schema nests n levels through items.
func deepSpec(n int) string {
	return minimal31 + "components:\n  schemas:\n    A: " + itemsDeep(n) + "\n"
}

// TestLoad_RefusesANestingPastTheBoundBeforeBuildingTheModel pins GitHub #905:
// a document nested past scan.MaxNesting is refused as one source-too-deep
// error, before the parser builds the model it would spend seconds and
// gigabytes on, at the first level past the bound. The document just within
// it loads.
func TestLoad_RefusesANestingPastTheBoundBeforeBuildingTheModel(t *testing.T) {
	t.Parallel()
	// The root, components, schemas and A are four levels; each items one more.
	within := scan.MaxNesting - 4
	got, diags, err := Load(t.Context(), 4, openapitest.SourceOf(deepSpec(within)), Options{})
	require.NoError(t, err)
	require.NotNil(t, got, "a document at the bound loads: %+v", diags)
	assert.Zero(t, countErrorsAt(diags, diag.SourceTooDeep))

	got, diags, err = Load(t.Context(), 4, openapitest.SourceOf(deepSpec(within+1)), Options{})
	require.NoError(t, err, "a document too deep is a spec problem, not a Go error")
	assert.Nil(t, got, "nothing is built")
	require.Len(t, diags, 1)
	d := diags[0]
	assert.Equal(t, diag.SourceTooDeep, d.Code)
	assert.Equal(t, ir.SeverityError, d.Severity)
	assert.Equal(t, 4, d.Provenance.Source)
	want := "/components/schemas/A" + strings.Repeat("/items", within+1)
	assert.Equal(t, jsontext.Pointer(want), d.Provenance.Pointer, "the first level past the bound")
}

// nestedValue is n mappings, each holding the next under k, around a scalar.
func nestedValue(n int) string {
	return strings.Repeat("{k: ", n) + "1" + strings.Repeat("}", n)
}

// sourceDeepDiag is the one source-too-deep error among diags, which it
// requires.
func sourceDeepDiag(t *testing.T, diags []ir.Diagnostic) ir.Diagnostic {
	t.Helper()
	i := slices.IndexFunc(diags, func(d ir.Diagnostic) bool { return d.Code == diag.SourceTooDeep })
	require.GreaterOrEqual(t, i, 0, "diagnostics: %+v", diags)
	require.Equal(t, 1, countErrorsAt(diags, diag.SourceTooDeep))
	return diags[i]
}

// TestLoad_RefusesAnOverlayNestedPastTheBoundBeforeApplyingIt pins that an
// overlay document is held to the bound as the source is, before the library
// applies it: the library clones an update into every node its target
// selects, at a cost growing with the update's depth. The refusal is placed in
// the overlay's own coordinates, where nothing has been grafted.
func TestLoad_RefusesAnOverlayNestedPastTheBoundBeforeApplyingIt(t *testing.T) {
	t.Parallel()
	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31),
		overlayOptions("  - target: $.info\n    update: {x-deep: "+nestedValue(scan.MaxNesting)+"}\n"))

	require.NoError(t, err)
	assert.Nil(t, got)
	d := sourceDeepDiag(t, diags)
	assert.Equal(t, 1, d.Provenance.Source, "the overlay is the document past the bound")
	assert.True(t, strings.HasPrefix(string(d.Provenance.Pointer), "/actions/0/update/x-deep/"), "at %s", d.Provenance.Pointer)
}

// TestLoad_RefusesANestingAnOverlayGrafted pins that the bound covers the tree
// the parser is handed, not only the bytes read: an overlay within the bound
// that grafts onto a deep target leaves a tree past it, refused after it
// applies, at a position the overlay answers for.
func TestLoad_RefusesANestingAnOverlayGrafted(t *testing.T) {
	t.Parallel()
	const deep = 1020 // the overlay's root, actions, its action and the update are four levels more
	source := minimal31 + "components:\n  schemas:\n    A: {type: object, properties: {p: {type: object, properties: {q: {type: string}}}}}\n"
	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(source),
		overlayOptions("  - target: $.components.schemas.A.properties.p.properties.q\n    update: {x-deep: "+nestedValue(deep)+"}\n"))

	require.NoError(t, err)
	assert.Nil(t, got)
	d := sourceDeepDiag(t, diags)
	assert.Equal(t, 1, d.Provenance.Source, "the overlay grafted what passes the bound")
	// q is the eighth level and x-deep's value the ninth, so the first past the
	// bound is MaxNesting-8 levels below that value.
	want := "/components/schemas/A/properties/p/properties/q/x-deep" + strings.Repeat("/k", scan.MaxNesting-8)
	assert.Equal(t, jsontext.Pointer(want), d.Provenance.Pointer)
}

// TestExternal_AdmitRefusesANestingPastTheBound pins that a document an
// external reference names is held to the source's bound, its finding the
// reason the reference fails.
func TestExternal_AdmitRefusesANestingPastTheBound(t *testing.T) {
	t.Parallel()
	root, err := parseTree([]byte("a: " + itemsDeep(scan.MaxNesting) + "\n"))
	require.NoError(t, err)

	err = newExternal(t.Context(), &soa.OpenAPI{}, Options{}, newExternalReads(sourceDocument{})).admit("k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Contains(t, err.Error(),
		fmt.Sprintf("document nests %d levels deep with its aliases expanded, past the %d-level bound", scan.MaxNesting+2, scan.MaxNesting))
}

// placedRefusals runs the pre-parse refusals over src, each finding placed by
// its pointer as the loader places it.
func placedRefusals(t *testing.T, src string) []ir.Diagnostic {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	return refusals(scan.InTree(0, scan.Pointers(&root)), &root, Options{})
}

// TestRefusals_NamesTheLeastTaggedMapping pins the tagged-mapping refusal's
// site when a document holds several: the least pointer, in either order.
func TestRefusals_NamesTheLeastTaggedMapping(t *testing.T) {
	t.Parallel()
	members := []string{"b: !x {k: 1}\n", "a: !y {k: 1}\n"}
	for _, order := range [][]string{members, {members[1], members[0]}} {
		diags := placedRefusals(t, strings.Join(order, ""))
		require.Len(t, diags, 1)
		assert.Equal(t, diag.TaggedMapping, diags[0].Code)
		assert.Equal(t, jsontext.Pointer("/a"), diags[0].Provenance.Pointer, "in %q", order)
	}
}

// TestRefusals_ReportsEachRefusalAlongsideTheOthers pins that no pre-parse
// refusal hides another: a document too deep, whose aliases add more nodes than
// the alias budget allows, and holding a tagged mapping, draws all three. A
// schema $ref cycle is none of them: the compiler binds it, and reports it,
// once the document loads. A recursive anchor is not either, for the nesting
// refusal leaves it to the anchor-cycle one.
func TestRefusals_ReportsEachRefusalAlongsideTheOthers(t *testing.T) {
	t.Parallel()
	src := "x-a: &a {k: 1}\nx-b: *a\nT: !t {k: 1}\nx-deep: " + itemsDeep(scan.MaxNesting) + "\n"
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))

	diags := refusals(scan.InTree(0, scan.Pointers(&root)), &root, Options{MaxAliasSurplus: 1})

	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	slices.Sort(codes)
	assert.Equal(t, []string{diag.BudgetExceeded, diag.SourceTooDeep, diag.TaggedMapping}, codes)
}
