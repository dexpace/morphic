package operation_test

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// paramIdentitySpec mounts one path item twice and lists a path-level and an
// operation-level parameter named id. The shared item is declared after its two
// uses, and /b before /a, so a derivation that depended on order would show.
const paramIdentitySpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    $ref: "#/components/pathItems/Shared"
  /a:
    $ref: "#/components/pathItems/Shared"
  /items/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}}
    get:
      operationId: getItem
      parameters:
        - {name: id, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
    post:
      operationId: postItem
      responses: {"200": {description: ok}}
components:
  pathItems:
    Shared:
      parameters:
        - {name: limit, in: query, schema: {type: integer}}
      get:
        responses: {"200": {description: ok}}
`

// paramIDs returns the IDs of op's parameters, keyed by the location each one
// binds.
func paramIDs(op ir.Operation) map[ir.HTTPLocation]ir.ParamID {
	out := map[ir.HTTPLocation]ir.ParamID{}
	for _, pb := range op.Bindings.HTTP[0].ParamBindings {
		out[pb.Location] = pb.Param
	}
	return out
}

func TestParams_SameNameInTwoLocationsGetsDistinctIDs(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, paramIdentitySpec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "getItem")

	require.Len(t, op.Params, 2)
	assert.NotEqual(t, op.Params[0].ID, op.Params[1].ID, "id in the path and id in the query are two parameters")
	ids := paramIDs(op)
	assert.Equal(t, ir.ParamID("param/openapi/paths/~1items~1{id}/get/parameters/id/path"), ids[ir.HTTPLocationPath])
	assert.Equal(t, ir.ParamID("param/openapi/paths/~1items~1{id}/get/parameters/id/query"), ids[ir.HTTPLocationQuery])
	assert.ElementsMatch(t, []ir.ParamID{op.Params[0].ID, op.Params[1].ID}, []ir.ParamID{ids[ir.HTTPLocationPath], ids[ir.HTTPLocationQuery]},
		"each binding names the ID of its own parameter")
}

func TestParams_IDsFollowNameNotPosition(t *testing.T) {
	t.Parallel()
	byName := func(first, second string) map[string]ir.ParamID {
		doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /items:
    get:
      operationId: getItem
      parameters:
        - {name: `+first+`, in: query, schema: {type: string}}
        - {name: `+second+`, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
`)
		openapitest.RequireNoErrorDiags(t, diags)
		out := map[string]ir.ParamID{}
		for _, p := range openapitest.FindOp(t, doc, "getItem").Params {
			out[p.Name.Source] = p.ID
		}
		return out
	}
	ba, ab := byName("b", "a"), byName("a", "b")
	assert.Equal(t, ab, ba, "reordering a parameter list renames nothing")
	assert.Equal(t, ir.ParamID("param/openapi/paths/~1items/get/parameters/a/query"), ba["a"])
}

func TestParams_PathItemParamIsScopedPerOperation(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, paramIdentitySpec)
	openapitest.RequireNoErrorDiags(t, diags)
	get, post := openapitest.FindOp(t, doc, "getItem"), openapitest.FindOp(t, doc, "postItem")

	require.Len(t, post.Params, 1)
	assert.Equal(t, ir.ParamID("param/openapi/paths/~1items~1{id}/post/parameters/id/path"), post.Params[0].ID)
	assert.NotContains(t, []ir.ParamID{get.Params[0].ID, get.Params[1].ID}, post.Params[0].ID,
		"a path-item parameter copied into two operations is two parameters")

	var shared []ir.ParamID
	for _, g := range doc.Services[0].Groups {
		for _, o := range g.Operations {
			if len(o.Params) == 1 && o.Params[0].Name.Source == "limit" {
				shared = append(shared, o.Params[0].ID)
			}
		}
	}
	assert.ElementsMatch(t, []ir.ParamID{
		"param/openapi/paths/~1a/get/parameters/limit/query",
		"param/openapi/paths/~1b/get/parameters/limit/query",
	}, shared, "a path item mounted twice roots each mount's parameters at that mount")
}

// repeatSpec returns a document whose one operation, GET /x, merges pathLevel
// and opLevel, written as YAML flow entries. Limit and Limit2 are two
// components holding the same name and location.
func repeatSpec(pathLevel, opLevel []string) string {
	list := func(indent string, entries []string) string {
		if len(entries) == 0 {
			return ""
		}
		out := indent + "parameters:\n"
		for _, e := range entries {
			out += indent + "  - " + e + "\n"
		}
		return out
	}
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /x:\n" +
		list("    ", pathLevel) +
		"    get:\n      operationId: g\n" +
		list("      ", opLevel) +
		"      responses: {\"200\": {description: ok}}\n" +
		"components:\n  parameters:\n" +
		"    Limit: {name: limit, in: query, schema: {type: integer}}\n" +
		"    Limit2: {name: limit, in: query, schema: {type: string}}\n"
}

// TestParams_RepeatedNameAndLocationKeepsEveryParameter covers a merged list
// holding one name and location twice. OpenAPI forbids it, but the bundled
// parser checks only entries written inline, so a $ref'd repeat reaches the
// lowering. Each occurrence is kept under an ID of its own, the first holding
// the bare one, and the repeat is reported at its own list entry.
func TestParams_RepeatedNameAndLocationKeepsEveryParameter(t *testing.T) {
	t.Parallel()
	const (
		inline = "{name: limit, in: query, schema: {type: boolean}}"
		limit  = `{$ref: "#/components/parameters/Limit"}`
		limit2 = `{$ref: "#/components/parameters/Limit2"}`
	)
	tests := []struct {
		name               string
		pathLevel, opLevel []string
		first, repeat      string
	}{
		{name: "one component twice", opLevel: []string{limit, limit},
			first: "/paths/~1x/get/parameters/0", repeat: "/paths/~1x/get/parameters/1"},
		{name: "a component, then an inline entry", opLevel: []string{limit, inline},
			first: "/paths/~1x/get/parameters/0", repeat: "/paths/~1x/get/parameters/1"},
		{name: "an inline entry, then a component", opLevel: []string{inline, limit},
			first: "/paths/~1x/get/parameters/0", repeat: "/paths/~1x/get/parameters/1"},
		{name: "two components holding one slot", opLevel: []string{limit, limit2},
			first: "/paths/~1x/get/parameters/0", repeat: "/paths/~1x/get/parameters/1"},
		{name: "two inline entries", opLevel: []string{inline, inline},
			first: "/paths/~1x/get/parameters/0", repeat: "/paths/~1x/get/parameters/1"},
		{name: "a pair on the path item", pathLevel: []string{limit, limit2},
			first: "/paths/~1x/parameters/0", repeat: "/paths/~1x/parameters/1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, repeatSpec(tc.pathLevel, tc.opLevel))
			op := openapitest.FindOp(t, doc, "g")

			require.Len(t, op.Params, 2, "neither occurrence is dropped")
			assert.Equal(t, ir.ParamID("param/openapi/paths/~1x/get/parameters/limit/query"), op.Params[0].ID,
				"the first in list order holds the bare ID")
			assert.Equal(t, ir.ParamID("param/openapi/paths/~1x/get/parameters/limit/query/2"), op.Params[1].ID)
			bindings := op.Bindings.HTTP[0].ParamBindings
			require.Len(t, bindings, 2)
			assert.Equal(t, []ir.ParamID{op.Params[0].ID, op.Params[1].ID},
				[]ir.ParamID{bindings[0].Param, bindings[1].Param}, "each binding names its own parameter")

			assert.Equal(t, []string{diag.DuplicateParameter + " at " + tc.repeat}, errorFindings(diags),
				"the repeat is one error, whatever spells its entries")
			msg := openapitest.DiagMessageAt(t, diags, diag.DuplicateParameter, ir.SeverityError, tc.repeat)
			assert.Contains(t, msg, tc.first, "the report names the entry the repeat collides with")
		})
	}
}

// TestParams_RepeatsAreNumberedInListOrder holds the occurrence number to the
// entry's place among the parameters holding its slot, and each report to the
// first of them, not to the occurrence just before.
func TestParams_RepeatsAreNumberedInListOrder(t *testing.T) {
	t.Parallel()
	const limit = `{$ref: "#/components/parameters/Limit"}`
	doc, diags := parseFull(t, repeatSpec(nil, []string{
		limit, "{name: id, in: query, schema: {type: string}}", limit, limit,
	}))
	op := openapitest.FindOp(t, doc, "g")

	const base = "param/openapi/paths/~1x/get/parameters/limit/query"
	got := make([]ir.ParamID, 0, len(op.Params))
	for _, p := range op.Params {
		got = append(got, p.ID)
	}
	assert.Equal(t, []ir.ParamID{base, "param/openapi/paths/~1x/get/parameters/id/query", base + "/2", base + "/3"}, got)
	for _, repeat := range []string{"/paths/~1x/get/parameters/2", "/paths/~1x/get/parameters/3"} {
		msg := openapitest.DiagMessageAt(t, diags, diag.DuplicateParameter, ir.SeverityError, repeat)
		assert.Contains(t, msg, "/paths/~1x/get/parameters/0")
	}
	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DuplicateParameter, ir.SeverityError))
}

// TestParams_RepeatOnAPathItemMountedTwiceIsReportedOnce covers a repeat inside
// a path item two paths $ref. Each mount's operation numbers its own copy, but
// the defect is one written list entry, so it is one report.
func TestParams_RepeatOnAPathItemMountedTwiceIsReportedOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: "#/components/pathItems/Shared"}
  /b: {$ref: "#/components/pathItems/Shared"}
components:
  parameters:
    Limit: {name: limit, in: query, schema: {type: integer}}
  pathItems:
    Shared:
      parameters:
        - $ref: "#/components/parameters/Limit"
        - $ref: "#/components/parameters/Limit"
      get:
        responses: {"200": {description: ok}}
`)
	var got []ir.ParamID
	for _, g := range doc.Services[0].Groups {
		for _, o := range g.Operations {
			for _, p := range o.Params {
				got = append(got, p.ID)
			}
		}
	}
	assert.ElementsMatch(t, []ir.ParamID{
		"param/openapi/paths/~1a/get/parameters/limit/query",
		"param/openapi/paths/~1a/get/parameters/limit/query/2",
		"param/openapi/paths/~1b/get/parameters/limit/query",
		"param/openapi/paths/~1b/get/parameters/limit/query/2",
	}, got)
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DuplicateParameter, ir.SeverityError))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DuplicateParameter, "/components/pathItems/Shared/parameters/1"))
}

// TestParams_RepeatInAnotherDocumentIsReportedOnce covers the two places a
// repeat is reported, each at the entry where its list is written. A list the
// source writes is load's, even when its entries $ref another document; a list
// another document writes, which load's walk of the source does not reach, is
// the lowering's, at the entry it lowered, in that document. Each repeat is
// reported once.
func TestParams_RepeatInAnotherDocumentIsReportedOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const other = `X:
  parameters:
    - {name: limit, in: query, schema: {type: integer}}
    - {name: limit, in: query, schema: {type: string}}
  get:
    parameters:
      - {name: q, in: query, schema: {type: string}}
      - {name: q, in: header, schema: {type: string}}
      - {name: q, in: query, schema: {type: string}}
    responses: {"200": {description: ok}}
Limit: {name: limit, in: query, schema: {type: integer}}
`
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x: {$ref: "./other.yaml#/X"}
  /y:
    get:
      parameters:
        - $ref: "./other.yaml#/Limit"
        - $ref: "./other.yaml#/Limit"
      responses: {"200": {description: ok}}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(other), 0o600))
	path := filepath.Join(dir, "root.yaml")
	require.NoError(t, os.WriteFile(path, []byte(root), 0o600))

	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: path, Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)

	require.Len(t, doc.Sources, 2)
	assert.Equal(t, filepath.Join(dir, "other.yaml"), doc.Sources[1].Path)
	repeat := func(source int, pointer jsontext.Pointer, name, in string, first jsontext.Pointer) ir.Diagnostic {
		return ir.Diagnostic{Severity: ir.SeverityError, Code: diag.DuplicateParameter,
			Message:    fmt.Sprintf("parameter %q in %q repeats the entry at %s", name, in, first),
			Provenance: ir.Provenance{Source: source, Pointer: pointer}}
	}
	var got []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.DuplicateParameter {
			got = append(got, d)
		}
	}
	slices.SortFunc(got, func(x, y ir.Diagnostic) int {
		return strings.Compare(string(x.Provenance.Pointer), string(y.Provenance.Pointer))
	})
	want := []ir.Diagnostic{
		repeat(1, "/X/get/parameters/2", "q", "query", "/X/get/parameters/0"),
		repeat(1, "/X/parameters/1", "limit", "query", "/X/parameters/0"),
		repeat(0, "/paths/~1y/get/parameters/1", "limit", "query", "/paths/~1y/get/parameters/0"),
	}
	assert.Empty(t, cmp.Diff(want, got))
}

// TestParameters_PathItemSharedAcrossOperationsInternsOnce is the fix's core
// scenario (issue #36): a path-item-level parameter merged into two sibling
// operations must intern its schema once, at the path item's own pointer —
// not once per operation under a pointer fabricated from that operation's
// unrelated parameter count.
func TestParameters_PathItemSharedAcrossOperationsInternsOnce(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema: {type: string, enum: [a, b]}
    get:
      operationId: getPet
      responses: {"200": {description: ok}}
    delete:
      operationId: deletePet
      parameters:
        - {name: force, in: query, schema: {type: boolean}}
      responses: {"200": {description: ok}}
`)
	doc, _, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	getPet := openapitest.FindOp(t, doc, "getPet")
	deletePet := openapitest.FindOp(t, doc, "deletePet")
	require.Len(t, getPet.Params, 1, "get inherits only the shared path-item parameter")
	require.Len(t, deletePet.Params, 2, "delete gets its own force plus the shared path-item parameter")

	wantID := ir.TypeID("t/anon/paths/~1pets~1{petId}/parameters/0/schema")
	assert.Equal(t, wantID, getPet.Params[0].Type.Target, "get resolves the shared path-item schema")

	byName := openapitest.IndexBy(deletePet.Params, func(p ir.Parameter) string { return p.Name.Source })
	assert.Equal(t, wantID, byName["petId"].Type.Target, "delete resolves the same shared schema, not a copy")

	typeDef, ok := doc.Types[wantID]
	require.True(t, ok, "the shared schema is registered under the path item's own pointer")
	assert.Equal(t, jsontext.Pointer("/paths/~1pets~1{petId}/parameters/0/schema"), typeDef.Common().Provenance.Pointer)

	_, fabricatedGet := doc.Types[ir.TypeID("t/anon/paths/~1pets~1{petId}/get/parameters/0/schema")]
	_, fabricatedDelete := doc.Types[ir.TypeID("t/anon/paths/~1pets~1{petId}/delete/parameters/1/schema")]
	assert.False(t, fabricatedGet, "no fabricated per-operation ID for get")
	assert.False(t, fabricatedDelete, "no fabricated per-operation ID for delete")
}

// TestParameters_PathItemSchemaIDStableAcrossOperationParamChanges guards the
// specific regression from issue #36: adding an unrelated operation-level
// parameter must not shift the TypeID already handed out for a sibling
// path-item-level parameter's schema.
func TestParameters_PathItemSchemaIDStableAcrossOperationParamChanges(t *testing.T) {
	t.Parallel()
	before := openapitest.PathsSpec(`  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema: {type: string, enum: [a, b]}
    get:
      operationId: getPet
      responses: {"200": {description: ok}}
`)
	after := openapitest.PathsSpec(`  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema: {type: string, enum: [a, b]}
    get:
      operationId: getPet
      parameters:
        - {name: verbose, in: query, schema: {type: boolean}}
      responses: {"200": {description: ok}}
`)
	_, svcBefore, diagsBefore := compileService(t, before)
	openapitest.RequireNoErrorDiags(t, diagsBefore)
	_, svcAfter, diagsAfter := compileService(t, after)
	openapitest.RequireNoErrorDiags(t, diagsAfter)

	opBefore := openapitest.FirstOp(t, svcBefore)
	opAfter := openapitest.FirstOp(t, svcAfter)
	require.Len(t, opBefore.Params, 1)
	require.Len(t, opAfter.Params, 2, "the unrelated verbose parameter adds a second entry")

	byName := openapitest.IndexBy(opAfter.Params, func(p ir.Parameter) string { return p.Name.Source })
	assert.Equal(t, opBefore.Params[0].Type.Target, byName["petId"].Type.Target,
		"an unrelated operation-level parameter must not shift the shared path-item schema's ID")
}

// TestParameters_WebhookPathItemParameterPointer covers the same merge through
// lowerWebhooks: a webhook path-item-level parameter must hoist at the webhook
// path item's own pointer.
func TestParameters_WebhookPathItemParameterPointer(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
webhooks:
  petEvent:
    parameters:
      - name: kind
        in: query
        schema: {type: string, enum: [created, deleted]}
    post:
      operationId: onPetEvent
      responses: {"200": {description: ok}}
`
	doc, _, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "onPetEvent")
	require.Len(t, op.Params, 1)

	wantID := ir.TypeID("t/anon/webhooks/petEvent/parameters/0/schema")
	assert.Equal(t, wantID, op.Params[0].Type.Target)
	typeDef, ok := doc.Types[wantID]
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/webhooks/petEvent/parameters/0/schema"), typeDef.Common().Provenance.Pointer)
}

// TestParameters_CallbackPathItemParameterPointer covers the same merge
// through lowerCallbackOps: a callback path-item-level parameter must hoist at
// the callback path item's own pointer, nested under the parent operation and
// callback expression.
func TestParameters_CallbackPathItemParameterPointer(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /subscribe:
    post:
      operationId: subscribe
      callbacks:
        onEvent:
          '{$request.body#/callbackUrl}':
            parameters:
              - name: token
                in: query
                schema: {type: string, enum: [a, b]}
            post:
              operationId: onEvent
              responses: {"200": {description: ok}}
      responses: {"200": {description: ok}}
`)
	doc, _, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	cbOp := openapitest.FindOp(t, doc, "onEvent")
	require.Len(t, cbOp.Params, 1)

	wantPointer := "/paths/~1subscribe/post/callbacks/onEvent/{$request.body#~1callbackUrl}/parameters/0/schema"
	wantID := ir.TypeID("t/anon" + wantPointer)
	assert.Equal(t, wantID, cbOp.Params[0].Type.Target)
	typeDef, ok := doc.Types[wantID]
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer(wantPointer), typeDef.Common().Provenance.Pointer)
}

// TestParameters_ShadowedPathItemParamUsesOperationPointer covers the other
// side of the merge: when the operation redeclares a path-item parameter, the
// operation's own schema must be used and lowered at the operation's own
// pointer — the shadowed path-item schema is never lowered at all.
func TestParameters_ShadowedPathItemParamUsesOperationPointer(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema: {type: string, enum: [a, b]}
    get:
      operationId: getPet
      parameters:
        - name: petId
          in: path
          required: true
          schema: {type: string, enum: [c, d]}
      responses: {"200": {description: ok}}
`)
	doc, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Params, 1, "the operation parameter shadows the path-item one; no duplicate")

	wantID := ir.TypeID("t/anon/paths/~1pets~1{petId}/get/parameters/0/schema")
	assert.Equal(t, wantID, op.Params[0].Type.Target, "the operation's own schema wins, at the operation's pointer")

	enum, ok := doc.Types[wantID].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, "c", enum.Members[0].Value.Str, "the operation-level schema's own values are used")

	_, atPathLevel := doc.Types[ir.TypeID("t/anon/paths/~1pets~1{petId}/parameters/0/schema")]
	assert.False(t, atPathLevel, "the shadowed path-item schema is never lowered")
}

// TestRefSiteDocs_MountsKeepTheirOwnInEitherOrder is the hand-rolled two-order
// diff for GitHub #610. A use-site summary or description is neither part of the
// type registry nor a diagnostic, so the in-package order-invariance oracle does
// not reach it. One component referenced by two operations with different
// overrides, declared in both orders, must give each mount its own docs.
func TestRefSiteDocs_MountsKeepTheirOwnInEitherOrder(t *testing.T) {
	t.Parallel()
	const first = `  /a:
    get:
      operationId: a
      parameters: [{$ref: '#/components/parameters/Limit', description: from a}]
      responses: {"200": {description: ok}}
`
	const second = `  /b:
    get:
      operationId: b
      parameters: [{$ref: '#/components/parameters/Limit', description: from b}]
      responses: {"200": {description: ok}}
`
	projection := func(paths string) map[string]string {
		t.Helper()
		spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
` + paths + `components:
  parameters:
    Limit: {name: limit, in: query, description: declared, schema: {type: integer}}
`
		doc, _, diags := compileService(t, spec)
		openapitest.RequireNoErrorDiags(t, diags)
		out := map[string]string{}
		for _, name := range []string{"a", "b"} {
			p, ok := openapitest.ParamBySource(openapitest.FindOp(t, doc, name), "limit")
			require.True(t, ok)
			out[name] = p.Docs.Description
		}
		return out
	}
	assert.Empty(t, cmp.Diff(projection(first+second), projection(second+first)),
		"each mount keeps its own override whichever was lowered first")
}
