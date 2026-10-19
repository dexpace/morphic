package scan

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/ir"
)

// nested is n mappings, each holding the next under key, around a scalar.
func nested(key string, n int) string {
	return strings.Repeat("{"+key+": ", n) + "1" + strings.Repeat("}", n)
}

// placedNesting runs nestingWithin over src under limit, placing its finding by
// pointer as load does.
func placedNesting(t *testing.T, src string, limit int64) (ir.Diagnostic, bool) {
	t.Helper()
	idx := indexOf(t, []byte(src))
	return nestingWithin(InTree(0, Pointers(idx.Root())), idx, limit)
}

// TestNesting_RefusesPastTheBoundAndNotAtIt pins the bound's edge on the
// exported check: a document nested exactly MaxNesting levels deep passes,
// one level more is refused as diag.SourceTooDeep, naming both numbers.
func TestNesting_RefusesPastTheBoundAndNotAtIt(t *testing.T) {
	t.Parallel()
	at := indexOf(t, []byte(nested("k", MaxNesting)+"\n"))
	require.Equal(t, int64(MaxNesting), at.Nesting())
	_, refused := Nesting(InSource(0), at)
	assert.False(t, refused, "a document at the bound is admitted")

	past := indexOf(t, []byte(nested("k", MaxNesting+1)+"\n"))
	d, refused := Nesting(InSource(0), past)
	require.True(t, refused)
	assert.Equal(t, diag.SourceTooDeep, d.Code)
	assert.Equal(t, ir.SeverityError, d.Severity)
	assert.Equal(t, fmt.Sprintf("document nests %d levels deep, past the %d-level bound", MaxNesting+1, MaxNesting), d.Message)
}

// TestNesting_SiteIsTheLeastPointerPastTheBound pins where the refusal falls
// when several parts of a document pass the bound: at the least pointer of
// the first levels past it, the same in either declaration order (GitHub
// #912's rule, kept by the refusal it was not written for).
func TestNesting_SiteIsTheLeastPointerPastTheBound(t *testing.T) {
	t.Parallel()
	deepB, deepA := "b: "+nested("k", 4), "a: [x, "+nested("j", 3)+"]"
	for _, src := range []string{deepB + "\n" + deepA + "\n", deepA + "\n" + deepB + "\n"} {
		d, refused := placedNesting(t, src, 3)
		require.True(t, refused)
		assert.Equal(t, jsontext.Pointer("/a/1/j"), d.Provenance.Pointer, "in\n%s", src)
	}
}

// TestDeepSites_NamesWhereEachWayDownPassesTheBound pins every kind of site:
// a collection one level past the bound; an alias whose target carries the
// nesting past it, at the alias, since the target is written elsewhere; and a
// node whose part under a merge key or a non-scalar key does, at that node,
// the deepest position on the way with a pointer of its own.
func TestDeepSites_NamesWhereEachWayDownPassesTheBound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want []jsontext.Pointer
	}{
		{"collections past the bound", "a: {b: {c: {d: 1}}}\ne: [[[[1]]]]\n", []jsontext.Pointer{"/e/0/0", "/a/b/c"}},
		{"an alias carrying it past", "x: &x {b: {c: 1}}\ny: {z: *x}\n", []jsontext.Pointer{"/y/z"}},
		{"a merge value carrying it past", "m: {<<: {b: {c: 1}}}\n", []jsontext.Pointer{"/m"}},
		{"a non-scalar key carrying it past", "m: {? {b: {c: 1}} : 1}\n", []jsontext.Pointer{"/m"}},
		{"nothing past it", "a: {b: 1}\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := indexOf(t, []byte(tc.src)).Root()
			pointers := Pointers(root)
			var got []jsontext.Pointer
			for _, site := range deepSites(root, 3) {
				p, held := pointers(site)
				require.True(t, held, "a site has a pointer")
				got = append(got, p)
			}
			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

// TestDeepSites_EdgeCases pins the two sites no parse produces: a bound below
// the top of the tree names the top, and a node passing the bound through
// nothing a pointer reaches, a document node nested by hand, is named itself.
func TestDeepSites_EdgeCases(t *testing.T) {
	t.Parallel()
	top := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	assert.Equal(t, []*yaml.Node{top}, deepSites(top, 0))

	inner := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{ynode.Seq(ynode.Seq(ynode.Seq()))}}
	root := ynode.Map(ynode.Scalar("d"), inner)
	assert.Equal(t, []*yaml.Node{inner}, deepSites(root, 3))
}

// TestNesting_CountsTheExpandedTree pins that the refusal reads the nesting
// the parser builds: anchors each nesting the one before through an alias pass
// the bound while the tree as written stays far within it.
func TestNesting_CountsTheExpandedTree(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("a0: &a0 " + nested("k", 3) + "\n")
	for i := 1; i < 4; i++ {
		fmt.Fprintf(&b, "a%d: &a%d {k: {k: {k: *a%d}}}\n", i, i, i-1)
	}
	idx := indexOf(t, []byte(b.String()))
	require.Equal(t, int64(13), idx.Nesting())
	require.Less(t, sourceindex.Build(idx.Root(), sourceindex.MaxIndexedNodes).Nodes(), int64(40))

	d, refused := placedNesting(t, b.String(), 10)
	require.True(t, refused)
	assert.Equal(t, jsontext.Pointer("/a3/k/k/k"), d.Provenance.Pointer, "the alias carrying a3 past the bound")
}

// TestNesting_LeavesARecursiveAnchorToItsOwnRefusal pins that a document whose
// count passes the bound only by going round a recursive anchor draws the
// anchor-cycle refusal and not a nesting one, which would have no site: the
// count of one turn is the index's, not a fact about the document.
func TestNesting_LeavesARecursiveAnchorToItsOwnRefusal(t *testing.T) {
	t.Parallel()
	src := "x: &a " + strings.Repeat("{k: ", 3) + "*a" + strings.Repeat("}", 3) + "\n"
	idx := indexOf(t, []byte(src))
	require.NotEmpty(t, idx.AnchorCycles())
	require.Greater(t, idx.Nesting(), int64(5), "one turn round the anchor counts past the bound below")

	_, refused := placedNesting(t, src, 5)
	assert.False(t, refused)
	diags := scanPlaced(t, []byte(src))
	require.Len(t, diags, 1)
	assert.Equal(t, diag.CyclicRef, diags[0].Code)
}
