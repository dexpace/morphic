package operation

import (
	"encoding/json/jsontext"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/openapi/core"
	"github.com/speakeasy-api/openapi/sequencedmap"
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

func TestWebhooks_WebhookGroup(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  newPet:
    post:
      operationId: onNewPet
      responses: {"200": {description: ok}}
`
	svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	var group ir.OperationGroup
	found := false
	for _, g := range svc.Groups {
		if g.Name.Hint == "webhooks" {
			group, found = g, true
		}
	}
	require.True(t, found, "webhook operations land in the webhooks group")
	require.Len(t, group.Operations, 1)
	op := group.Operations[0]
	assert.Equal(t, ir.OpID("op/openapi/webhooks/newPet/post"), op.ID)
	require.Len(t, op.Bindings.HTTP, 1)
	assert.True(t, op.Bindings.HTTP[0].IsWebhook)
}

func TestCallbacks_RegisteredAndBound(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /subscribe:
    post:
      operationId: sub
      callbacks:
        onEvent:
          '{$request.body#/cb}':
            post:
              operationId: cbPost
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
`)
	svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	group := svc.Groups[0]
	require.Len(t, group.Operations, 2, "parent op and callback op both registered")
	byName := openapitest.IndexBy(group.Operations, func(op ir.Operation) string { return op.Name.Source })
	sub, ok := byName["sub"]
	require.True(t, ok)
	cb, ok := byName["cbPost"]
	require.True(t, ok)
	require.Len(t, sub.Bindings.HTTP, 1)
	require.Len(t, sub.Bindings.HTTP[0].Callbacks, 1)
	call := sub.Bindings.HTTP[0].Callbacks[0]
	assert.Equal(t, "{$request.body#/cb}", call.Expression)
	require.Len(t, call.Operations, 1)
	assert.Equal(t, cb.ID, call.Operations[0])
}

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
	merged := mergeParameters(pi.GetParameters(), op.GetParameters(), pathPtr, opPtr)
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

func TestStatusRange_NamesAStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code     string
		from, to int
	}{
		{"default", 0, 0},
		{"200", 200, 200},
		{"100", 100, 100}, // the low bound of what HTTP defines
		{"599", 599, 599}, // and the high one
		{"4XX", 400, 499},
		{"5xx", 500, 599}, // lowercase is read too
		{"1XX", 100, 199},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			r, ok := statusRange(tc.code)
			require.True(t, ok, "%q names a status", tc.code)
			assert.Equal(t, tc.from, r.From)
			assert.Equal(t, tc.to, r.To)
		})
	}
}

// TestStatusRange_NamesNoStatus is the half that used to be silent. Every key
// here reached {0,0} or a range OpenAPI cannot declare, with no diagnostic and
// no way for a consumer to tell the result from a declared default (GitHub
// #262).
//
// The zero range beside the false is asserted because it is load-bearing, not
// because it is obvious: lowerResponses routes on isErrorRange without
// re-testing ok, so a false paired with anything from 400 up would send a key
// that names no status to lowerErrorCase, which would classify a fault from a
// range nothing derived and drop the key entirely — ErrorCase holds no naming.
// This is what holds that pairing.
func TestStatusRange_NamesNoStatus(t *testing.T) {
	t.Parallel()
	for _, code := range []string{
		"wat",  // not a number at all; used to reach {0,0}
		"20A",  // two digits and a letter
		"2X0",  // half a wildcard
		"1XY",  // the third character the wildcard test never read
		"9XX",  // a leading digit past the 1XX-5XX OpenAPI defines
		"6XX",  // the first one past it
		"0",    // Atoi read this as 0, which is default's own range
		"00",   // and this
		"000",  // and this, at the right width
		"600",  // three digits past what HTTP defines
		"099",  // and below it
		"+200", // Atoi accepts a sign; a status key has none
		"",     // no key at all
		"Default",
		"2XXX",
	} {
		// Quoted so the empty key names a subtest of its own rather than "#00".
		t.Run(strconv.Quote(code), func(t *testing.T) {
			t.Parallel()
			r, ok := statusRange(code)
			assert.False(t, ok, "%q names no status", code)
			assert.Equal(t, ir.StatusRange{}, r,
				"a false is paired with the zero range; lowerResponses routes on that")
		})
	}
}

// TestStatusConditions_RecordsNothingForAnUnreadableKey pins the disposition
// half: an unreadable key must not borrow default's {0,0} range, or the two
// become one shape in the IR.
func TestStatusConditions_RecordsNothingForAnUnreadableKey(t *testing.T) {
	t.Parallel()
	assert.Empty(t, statusConditions(ir.StatusRange{}, false).StatusCodes)
	assert.Equal(t,
		[]ir.StatusRange{{From: 0, To: 0}},
		statusConditions(ir.StatusRange{}, true).StatusCodes,
		"a declared default still records the catch-all it means")
}

func TestFaultFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "client", faultFor(ir.StatusRange{From: 404, To: 404}))
	assert.Equal(t, "server", faultFor(ir.StatusRange{From: 503, To: 503}))
	assert.Equal(t, "", faultFor(ir.StatusRange{}))
}

func TestLowerResponses_NoResponses(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	responses, errs, diags := lowerResponses(l.ctx, l.types, &l.anchors, &soa.Operation{}, "/op")
	assert.Nil(t, responses)
	assert.Nil(t, errs)
	assert.Empty(t, diags)
}

func TestFirstPathSegment_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", firstPathSegment("/"))
	assert.Equal(t, "users", firstPathSegment("/users/{id}"))
}

func TestApplyPathServers_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	op := &ir.Operation{}
	diags := applyPathServers(l.ctx, onOperation(op),
		&soa.PathItem{Servers: []*soa.Server{{URL: "https://x"}}}, "/paths/~1a")
	assert.Nil(t, op.Unmodeled, "servers with no raw node are not preserved")
	assert.Empty(t, diags)
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

func TestLowerTagDefs_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{Tags: []*soa.Tag{nil, {}}})
	assert.Len(t, lowerTagDefs(l.ctx), 1, "nil tag entry skipped")
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

// TestFaultFor_ClassifiesAtTheClassBoundaries pins where one HTTP class ends and
// the next begins, which no fixture reaches: the corpus uses 400, 404, 429 and
// the 4XX/5XX wildcards, so both upper bounds are stated by the code and held by
// nothing. Narrowing either one by a single code leaves the whole suite green
// while a 499 stops being a client fault — and Fault is what an SDK emitter
// reads to decide which exception class it raises.
func TestFaultFor_ClassifiesAtTheClassBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		from int
		want string
	}{
		{name: "the first client code", from: 400, want: "client"},
		{name: "the last client code", from: 499, want: "client"},
		{name: "the first server code", from: 500, want: "server"},
		{name: "the last server code", from: 599, want: "server"},
		{name: "a success is neither", from: 200, want: ""},
		{name: "past the server class", from: 600, want: ""},
		{name: "the catch-all default range is unclassified", from: 0, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, faultFor(ir.StatusRange{From: tc.from, To: tc.from}))
		})
	}
}

// TestApplyPathItemDocs_WithoutRootNode is the documentation counterpart of the
// two tests above: a declared pair whose source node cannot be read keeps
// nothing, and announces nothing it did not keep.
//
// The input is synthetic, but the branch is not hypothetical — RawChildNode does
// not expand a merge key, so a real document supplying the pair through one
// reaches here with GetSummary non-empty and loses it silently (GitHub #384).
// This pins the current behaviour rather than endorsing it; when #384 makes the
// lookup view-aware, what changes is the node this receives, not this contract.
func TestApplyPathItemDocs_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	summary, description := "documented", "at length"
	op := &ir.Operation{}

	diags := applyPathItemDocs(l.ctx, onOperation(op),
		&soa.PathItem{Summary: &summary, Description: &description}, "/paths/~1a")

	assert.Nil(t, op.Unmodeled, "documentation with no raw node is not preserved")
	assert.Empty(t, diags)
}

// TestPathOperations_NilAdditionalOperationSkipped pins the guard on the
// additionalOperations walk. The parser never yields a nil entry, but the map is
// a plain pointer map and lowerOperation reads the operation's fields directly,
// so a nil would panic rather than lower.
func TestPathOperations_NilAdditionalOperationSkipped(t *testing.T) {
	t.Parallel()
	pi := &soa.PathItem{
		AdditionalOperations: sequencedmap.New(
			sequencedmap.NewElem("EMPTY", (*soa.Operation)(nil)),
			sequencedmap.NewElem("PURGE", &soa.Operation{}),
		),
	}

	ops := pathOperations(pi)

	require.Len(t, ops, 1, "the nil entry is skipped and the real one is not")
	assert.Equal(t, "PURGE", ops[0].method)
	assert.Equal(t, jsontext.Pointer("/additionalOperations/PURGE"), ops[0].seg)
}

// httpMethodsNames is the plain-string projection of httpMethods, which
// TestHTTPMethods_AgreesWithLibraryVocabulary compares against
// soa.IsStandardMethod. httpMethods itself carries a func field, so it cannot
// be a slices.Contains argument directly.
func httpMethodsNames() []string {
	names := make([]string, len(httpMethods))
	for i, m := range httpMethods {
		names[i] = m.name
	}
	return names
}

// TestHTTPMethods_AgreesWithLibraryVocabulary holds httpMethods to
// soa.IsStandardMethod, mirroring the shape of the schema package's 2020-12
// vocabulary tests (internal/schema/schema_test.go's vocabularyCases): one
// table enumerating the vocabulary, checked against the library predicate that
// is meant to track it.
//
// The two vocabularies answer different questions — httpMethods says what this
// compiler lowers, IsStandardMethod says what the specification defines — but
// httpMethods is meant to be a subset of what the library recognizes, so the
// two must agree on every name either one holds an opinion about. If the
// library learns a method before this compiler does, IsStandardMethod turns
// true for it while httpMethods stays silent: undeclaredPathItemKeys then
// grades the key as declared, so no field lowers it and no diagnostic reports
// it. That silent drop is GitHub #413; this test turns the disagreement that
// causes it into a build failure instead.
//
// The library exports no list IsStandardMethod is built from — it is the
// unexported standardHttpMethods in
// github.com/speakeasy-api/openapi/openapi@v1.24.1's paths.go — so this probes
// with an explicit candidate set instead: every method RFC 9110 §9.3 defines
// (GET, HEAD, POST, PUT, DELETE, CONNECT, OPTIONS, TRACE), plus QUERY and
// PATCH, plus every name httpMethods itself declares, each checked in both
// letter cases since IsStandardMethod compares case-sensitively against
// lowercase constants and a case mismatch would otherwise hide a real gap.
func TestHTTPMethods_AgreesWithLibraryVocabulary(t *testing.T) {
	t.Parallel()

	rfc9110 := []string{
		"GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE",
		"QUERY", "PATCH",
	}
	methods := httpMethodsNames()
	candidates := make([]string, 0, len(rfc9110)+len(methods))
	candidates = append(candidates, rfc9110...)
	candidates = append(candidates, methods...)

	seen := make(map[string]bool, 2*len(candidates))
	for _, c := range candidates {
		seen[strings.ToUpper(c)] = true
		seen[strings.ToLower(c)] = true
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, soa.IsStandardMethod(name), slices.Contains(httpMethodsNames(), name),
				"%q: soa.IsStandardMethod and httpMethods disagree on whether this is a "+
					"standard HTTP method; one vocabulary fell behind the other", name)
		})
	}
}

// TestPathItemFields_MatchTheLibraryModel holds pathItemFields to the key tags of
// the library's Path Item core model, the way the ir package holds its
// hand-written kind lists to the kinds the sources declare. The list decides
// which raw keys the census leaves alone as declared fields, so a field the
// library adds and this list does not name would be announced as a key the
// document was not permitted to write — and one this list names and the library
// has dropped would be a key the census never looks at.
//
// The extensions field is the one tag left out: it is spelled "extensions" in
// the tag and reached by x- prefix in the document, which pathItemDeclares tests
// for on its own.
func TestPathItemFields_MatchTheLibraryModel(t *testing.T) {
	t.Parallel()
	var declared []string
	typ := reflect.TypeFor[core.PathItem]()
	for field := range typ.Fields() {
		key := field.Tag.Get("key")
		if key == "" || key == "extensions" {
			continue
		}
		declared = append(declared, key)
	}
	diff := cmp.Diff(declared, pathItemFields)
	assert.Empty(t, diff,
		"pathItemFields must name every keyed field of core.PathItem, once each, in its order (-model +listed)")
}

// TestPathItemDeclares_AnchoredDeclaredKeysAreNotUndeclared is the control the
// raw reading needs: the library skips every anchored value on a path item, a
// declared field's included, so the raw mapping presents each declared key
// exactly as it presents an undeclared one and only the vocabulary tells them
// apart. Each class of declared key is written with an anchored value here, and
// the census must report none of them.
//
// The item is unmarshalled through the library directly, as the resolver
// unmarshals one it loads through an external reference: the source document's
// own anchors are cleared before its model is built (GitHub #459), so this is
// the path the census's raw reading still serves. The anchored `get` is not
// lowered there — the same skip unmounts it — and that is a loss of its own,
// outside what a census of undeclared keys can answer; what is pinned here is
// only that it is not misreported as a key the specification does not define.
func TestPathItemDeclares_AnchoredDeclaredKeysAreNotUndeclared(t *testing.T) {
	t.Parallel()
	pi := pathItemOf(t, `
summary: &s s
description: &d d
servers: &sv [{url: 'https://a.example'}]
parameters: &pa [{name: p, in: query, schema: {type: string}}]
additionalOperations: &ao
  PURGE: {operationId: purgeY, responses: {"200": {description: ok}}}
x-mark: &xm XVAL
get: &g {operationId: getY, responses: {"200": {description: ok}}}
put: {operationId: putY, responses: {"200": {description: ok}}}
bogus: &b {responses: {"200": {description: BOGUS}}}
`)
	assert.Equal(t, []string{"bogus"}, undeclaredPathItemKeys(pi),
		"every declared key is left alone whether or not the map holds it; the undeclared one is not")
}

// pathItemOf unmarshals src as a Path Item Object through the library, the way
// the compiler reads one, so the folded map and the raw node are both what a
// document produces.
func pathItemOf(t *testing.T, src string) *soa.PathItem {
	t.Helper()
	pi := &soa.PathItem{}
	valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(src), pi)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	return pi
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
	assert.Equal(t, "/paths/~1b/get", diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "/paths/~1a/get", "the warning names the mount it shares a declaration with")
	assert.Equal(t, diag.ConflictingOperationID, diags[1].Code)
	assert.Equal(t, ir.SeverityError, diags[1].Severity)
	assert.Equal(t, "/components/pathItems/S/get", diags[1].Provenance.Pointer, "the error is where the repeat is written")
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
