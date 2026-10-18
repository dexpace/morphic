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

// TestLoad_RefusesANestingAnOverlayGrafted pins that the bound covers the
// tree the parser is handed, not only the bytes read: an overlay grafting a
// deep subtree onto a shallow source is refused after it applies, at a
// position the overlay answers for.
func TestLoad_RefusesANestingAnOverlayGrafted(t *testing.T) {
	t.Parallel()
	deep := strings.Repeat("{k: ", scan.MaxNesting) + "1" + strings.Repeat("}", scan.MaxNesting)
	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31),
		overlayOptions("  - target: $.info\n    update: {x-deep: "+deep+"}\n"))

	require.NoError(t, err)
	assert.Nil(t, got)
	require.Equal(t, 1, countErrorsAt(diags, diag.SourceTooDeep), "diagnostics: %+v", diags)
	for _, d := range diags {
		if d.Code == diag.SourceTooDeep {
			assert.Equal(t, 1, d.Provenance.Source, "the overlay grafted what passes the bound")
		}
	}
}

// TestExternal_AdmitRefusesANestingPastTheBound pins that a document an
// external reference names is held to the source's bound, its finding the
// reason the reference fails.
func TestExternal_AdmitRefusesANestingPastTheBound(t *testing.T) {
	t.Parallel()
	root, parsed := parseTree([]byte("a: " + itemsDeep(scan.MaxNesting) + "\n"))
	require.True(t, parsed)

	err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads(sourceDocument{})).admit("k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Contains(t, err.Error(),
		fmt.Sprintf("document nests %d levels deep, past the %d-level bound", scan.MaxNesting+2, scan.MaxNesting))
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
// refusal hides another: a document too deep, holding a $ref cycle and a
// tagged mapping, draws all three.
func TestRefusals_ReportsEachRefusalAlongsideTheOthers(t *testing.T) {
	t.Parallel()
	src := "components:\n  schemas:\n    A: {$ref: '#/components/schemas/A'}\n    T: !t {k: 1}\n" +
		"x-deep: " + itemsDeep(scan.MaxNesting) + "\n"

	diags := placedRefusals(t, src)
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	slices.Sort(codes)
	assert.Equal(t, []string{diag.CyclicRef, diag.SourceTooDeep, diag.TaggedMapping}, codes)
}
