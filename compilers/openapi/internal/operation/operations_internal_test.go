package operation

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

func TestParameters_PathItemMergeOverride(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /users/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}, description: path-level}
      - {name: trace, in: header, schema: {type: string}}
    get:
      operationId: g
      parameters:
        - {name: id, in: path, required: true, schema: {type: integer}, description: op-level}
      responses: {"200": {description: ok}}
`)
	loadedDoc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	var pi *soa.PathItem
	for _, rp := range loadedDoc.Doc.GetPaths().All() {
		pi = resolve.Object[soa.PathItem](rp)
	}
	require.NotNil(t, pi)
	op := pi.Get()
	require.NotNil(t, op)
	pathPtr := ids.Ptr("paths", "/users/{id}")
	opPtr := pathPtr + ids.Ptr("get")
	merged := mergeParameters(appendSourced(nil, pi.GetParameters(), pathPtr, nil, nil), op.GetParameters(), opPtr)
	require.Len(t, merged, 2, "shared (name,in) collapses to one; op wins")
	assert.Same(t, op.GetParameters()[0], merged[0].ref, "operation parameter overrides the path-item one")
	assert.Equal(t, jsontext.Pointer("/paths/~1users~1{id}/get/parameters/0"), merged[0].pointer,
		"the op-level parameter keeps its own declaration pointer")

	byName := map[string]sourcedParam{}
	for _, sp := range merged {
		byName[resolve.Object[soa.Parameter](sp.ref).GetName()] = sp
	}
	_, hasID := byName["id"]
	_, hasTrace := byName["trace"]
	assert.True(t, hasID)
	assert.True(t, hasTrace)
	assert.Equal(t, jsontext.Pointer("/paths/~1users~1{id}/parameters/1"), byName["trace"].pointer,
		"the unshadowed path-level parameter keeps its own declaration pointer, at its own path-item index")
}

// TestApplyOperationServers_WithoutRootNode is the operation half of the test
// above: a declared list whose source node cannot be read keeps nothing, and
// announces nothing it did not keep.
func TestApplyOperationServers_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	op := &ir.Operation{}
	diags := applyOperationServers(l.ctx, op, &soa.Operation{Servers: []*soa.Server{{URL: "https://x"}}}, "/paths/~1a/get")
	assert.Nil(t, op.Unmodeled, "servers with no raw node are not preserved")
	assert.Empty(t, diags)
}

// TestParamKey_NilParameterIsNotAKey pins the guard on the merge key. A nil
// entry in a parameter list has no name and no location, so it cannot key
// anything — and answering with the zero key would silently merge every such
// entry onto one another.
func TestParamKey_NilParameterIsNotAKey(t *testing.T) {
	t.Parallel()
	_, ok := paramKey(nil)
	assert.False(t, ok)
}

// writtenFixture's one operation is written at /paths/~1z/get and reused every
// way a path can reuse it: /a aliases the path item, /m merges it in, /r
// references it, and /o aliases the operation itself. /s is a sequence, which
// no operation pointer passes through.
const writtenFixture = `paths:
  /z: &item
    get: &op {operationId: dup}
  /a: *item
  /m:
    <<: *item
  /r: {$ref: '#/paths/~1z'}
  /o:
    get: *op
  /s: [1, 2]
`

// writtenOp parses writtenFixture, returning its root and the node declaring
// its one operation.
func writtenOp(t *testing.T) (root, op *yaml.Node) {
	t.Helper()
	root = openapitest.YAMLNode(t, writtenFixture)
	op = annotation.RawChildNode(annotation.RawChildNode(annotation.RawChildNode(root, "paths"), "/z"), "get")
	require.NotNil(t, op, "the fixture writes its operation at /z")
	return root, op
}

// TestWrittenTree_HoldsANodeOnlyWhereTheTextWritesIt pins the walk declare
// reads a declaration's place from: the pointer the text writes the operation
// at holds it, and no pointer reaching it through an alias, a merge key or a
// $ref does, which is what tells a declaration from its reuses.
func TestWrittenTree_HoldsANodeOnlyWhereTheTextWritesIt(t *testing.T) {
	t.Parallel()
	root, op := writtenOp(t)
	w := newWrittenTree(root)

	assert.True(t, w.holds("/paths/~1z/get", op), "the text writes the operation at /z")
	for _, reuse := range []jsontext.Pointer{"/paths/~1a/get", "/paths/~1m/get", "/paths/~1r/get", "/paths/~1o/get"} {
		assert.False(t, w.holds(reuse, op), "%s reuses the operation rather than writing it", reuse)
	}
	assert.False(t, w.holds("/paths/~1s/0", op), "a walk reads mappings only")
	assert.False(t, newWrittenTree(nil).holds("/paths/~1z/get", op), "a tree with no text holds nothing")

	_, indexed := w.children[annotation.RawChildNode(root, "paths")]
	assert.True(t, indexed, "the paths mapping is indexed for the walks after the first")
}

// TestDeclare_ReadsWhereADeclarationIsWritten pins how declare places one
// declaration: at the pointer its text is at, with the mount there first, even
// where a reuse's pointer sorts before it. A declaration written nowhere the
// tree holds, as one in another document is, falls back to pointer order. Each
// case runs its claims in both orders.
func TestDeclare_ReadsWhereADeclarationIsWritten(t *testing.T) {
	t.Parallel()
	root, op := writtenOp(t)
	for _, tc := range []struct {
		name   string
		claims []operationIDClaim
		want   declaration
	}{
		{
			name: "an alias site sorting first",
			claims: []operationIDClaim{
				{node: op, ptrs: opPointers{mount: "/paths/~1a/get", decl: "/paths/~1a/get"}},
				{node: op, ptrs: opPointers{mount: "/paths/~1z/get", decl: "/paths/~1z/get"}},
			},
			want: declaration{at: "/paths/~1z/get", mounts: []jsontext.Pointer{"/paths/~1z/get", "/paths/~1a/get"}},
		},
		{
			name: "a ref sorting first",
			claims: []operationIDClaim{
				{node: op, ptrs: opPointers{mount: "/paths/~1r/get", decl: "/paths/~1z/get"}},
				{node: op, ptrs: opPointers{mount: "/paths/~1z/get", decl: "/paths/~1z/get"}},
			},
			want: declaration{at: "/paths/~1z/get", mounts: []jsontext.Pointer{"/paths/~1z/get", "/paths/~1r/get"}},
		},
		{
			name: "written where no claim is mounted",
			claims: []operationIDClaim{
				{node: op, ptrs: opPointers{mount: "/paths/~1y/get", decl: "/paths/~1z/get"}},
				{node: op, ptrs: opPointers{mount: "/paths/~1x/get", decl: "/paths/~1z/get"}},
			},
			want: declaration{at: "/paths/~1z/get", mounts: []jsontext.Pointer{"/paths/~1x/get", "/paths/~1y/get"}},
		},
		{
			name: "written nowhere the tree holds",
			claims: []operationIDClaim{
				{node: op, ptrs: opPointers{mount: "/paths/~1y/get", decl: "/paths/~1y/get"}},
				{node: op, ptrs: opPointers{mount: "/paths/~1x/get", decl: "/paths/~1x/get"}},
			},
			want: declaration{at: "/paths/~1x/get", mounts: []jsontext.Pointer{"/paths/~1x/get", "/paths/~1y/get"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reversed := slices.Clone(tc.claims)
			slices.Reverse(reversed)
			for _, claims := range [][]operationIDClaim{tc.claims, reversed} {
				got := declare(claims, newWrittenTree(root))
				assert.Empty(t, cmp.Diff(tc.want, got, cmp.AllowUnexported(declaration{})))
			}
		})
	}
}

// TestByDeclaration_GroupsByNodeOrDeclarationPointer pins both ways claims
// mount one declaration, and that grouping follows a chain of them: a shares a
// node with b, and b a declaration pointer with c, so the three are one
// declaration. The c-first order is the one that splits them when each claim
// joins the first group it matches, and every order gives one answer.
func TestByDeclaration_GroupsByNodeOrDeclarationPointer(t *testing.T) {
	t.Parallel()
	shared, copied, other := &yaml.Node{}, &yaml.Node{}, &yaml.Node{}
	a := operationIDClaim{node: shared, ptrs: opPointers{mount: "/paths/~1a/get", decl: "/paths/~1a/get"}}
	b := operationIDClaim{node: shared, ptrs: opPointers{mount: "/paths/~1b/get", decl: "/components/pathItems/S/get"}}
	c := operationIDClaim{node: copied, ptrs: opPointers{mount: "/paths/~1c/get", decl: "/components/pathItems/S/get"}}
	d := operationIDClaim{node: other, ptrs: opPointers{mount: "/paths/~1d/get", decl: "/paths/~1d/get"}}
	want := [][]jsontext.Pointer{{"/paths/~1a/get", "/paths/~1b/get", "/paths/~1c/get"}, {"/paths/~1d/get"}}

	for _, claims := range [][]operationIDClaim{{a, b, c, d}, {c, a, d, b}, {d, c, b, a}} {
		var got [][]jsontext.Pointer
		for _, group := range byDeclaration(claims) {
			var mounts []jsontext.Pointer
			for _, cl := range group {
				mounts = append(mounts, cl.ptrs.mount)
			}
			slices.Sort(mounts)
			got = append(got, mounts)
		}
		slices.SortFunc(got, func(x, y []jsontext.Pointer) int { return strings.Compare(string(x[0]), string(y[0])) })
		assert.Empty(t, cmp.Diff(want, got))
	}
}

// TestByDeclaration_ANilNodeGroupsByDeclarationPointerAlone pins the stricter
// reading for a claim with no node: two such claims are one declaration only
// at one declaration pointer, never through the missing node they share.
func TestByDeclaration_ANilNodeGroupsByDeclarationPointerAlone(t *testing.T) {
	t.Parallel()
	apart := byDeclaration([]operationIDClaim{
		{ptrs: opPointers{mount: "/paths/~1a/get", decl: "/paths/~1a/get"}},
		{ptrs: opPointers{mount: "/paths/~1b/get", decl: "/paths/~1b/get"}},
	})
	assert.Len(t, apart, 2, "two claims with no node are two declarations")

	together := byDeclaration([]operationIDClaim{
		{ptrs: opPointers{mount: "/paths/~1a/get", decl: "/components/pathItems/S/get"}},
		{ptrs: opPointers{mount: "/paths/~1b/get", decl: "/components/pathItems/S/get"}},
	})
	assert.Len(t, together, 1, "unless they resolve to one declaration pointer")
}

// TestJudge_ReportsEveryDeclarationAndMountButTheFirst pins the findings judge
// makes of declarations already ordered: an error at each declaration but the
// first, naming where the first is written, and a warning at each mount but a
// declaration's first, naming that first mount.
func TestJudge_ReportsEveryDeclarationAndMountButTheFirst(t *testing.T) {
	t.Parallel()
	diags := judge(newRawLowerer(nil).ctx, "dup", []declaration{
		{at: "/paths/~1a/get", mounts: []jsontext.Pointer{"/paths/~1a/get", "/paths/~1b/get"}},
		{at: "/components/pathItems/S/get", mounts: []jsontext.Pointer{"/paths/~1c/get"}},
	})

	require.Len(t, diags, 2, "one remount and one second declaration: %+v", diags)
	assert.Equal(t, diag.DuplicateOperationID, diags[0].Code)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, jsontext.Pointer("/paths/~1b/get"), diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "/paths/~1a/get", "the warning names the mount it shares a declaration with")
	assert.Equal(t, diag.ConflictingOperationID, diags[1].Code)
	assert.Equal(t, ir.SeverityError, diags[1].Severity)
	assert.Equal(t, jsontext.Pointer("/components/pathItems/S/get"), diags[1].Provenance.Pointer,
		"the error is where the repeat is written")
	assert.Contains(t, diags[1].Message, "/paths/~1a/get", "and names where the first declaration is")
}

// TestDeclaringNode_FollowsAnAliasOneHop pins what an operation alias needs:
// `get: *op` builds the operation from the alias node, which declaringNode
// follows to the mapping it names. A node that is not an alias, and a missing
// one, are their own declaring node.
func TestDeclaringNode_FollowsAnAliasOneHop(t *testing.T) {
	t.Parallel()
	root := openapitest.YAMLNode(t, "a: &op {operationId: x}\nb: *op\n")
	anchored := annotation.RawChildNode(root, "a")
	alias := annotation.RawChildNode(root, "b")
	require.Equal(t, yaml.AliasNode, alias.Kind, "the fixture reuses the mapping through an alias")

	assert.Same(t, anchored, declaringNode(alias))
	assert.Same(t, anchored, declaringNode(anchored))
	assert.Nil(t, declaringNode(nil))
}

// TestOperationIDClaims_AddSkipsAnEmptyOperationID pins add's guard: an
// operation with no operationId claims nothing, because an emitter synthesizes
// its name from the method and path rather than reading one that was never
// declared.
func TestOperationIDClaims_AddSkipsAnEmptyOperationID(t *testing.T) {
	t.Parallel()
	claims := newOperationIDClaims()
	claims.add(&soa.Operation{}, opPointers{mount: "/paths/~1a/get", decl: "/paths/~1a/get"})

	assert.Empty(t, claims.names, "nothing was claimed, so nothing is queued to report")
	assert.Empty(t, claims.report(newRawLowerer(nil).ctx))
}
