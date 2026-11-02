// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// pathItemRefCollisionSpec declares the same constructs on both sides of one
// $ref: the method key, the summary, the servers, an x-* name, an undeclared key
// and an item parameter pair, typed apart so the one lowered names its side.
const pathItemRefCollisionSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: use summary
    description: use description
    servers: [{url: https://use.example}]
    x-shared: use
    bogusBoth: {responses: {"200": {description: USE}}}
    parameters:
      - {name: dup, in: query, schema: {type: string}}
    get:
      operationId: useGet
      responses: {"200": {description: OK}}
  /b:
    summary: ref summary
    description: ref description
    servers: [{url: https://ref.example}]
    x-shared: ref
    x-refOnly: ref
    bogusBoth: {responses: {"200": {description: REF}}}
    parameters:
      - {name: dup, in: query, schema: {type: integer}}
    get:
      operationId: refGet
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_CollisionIsLoudAndUseSiteWins pins the collision rule
// GitHub #577 settles: OpenAPI leaves the result of a field written beside a
// $ref undefined, so the use site's declaration wins every construct, as a
// $ref-adjacent schema sibling's does, and the losing one is reported rather
// than dropped in silence.
func TestPathItemRefSiblings_CollisionIsLoudAndUseSiteWins(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefCollisionSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	// One finding per colliding construct, each sited at the use site's own key
	// and naming the referent's.
	for _, at := range []string{
		"/paths/~1a/get",
		"/paths/~1a/summary",
		"/paths/~1a/description",
		"/paths/~1a/servers",
		"/paths/~1a/x-shared",
		"/paths/~1a/bogusBoth",
		"/paths/~1a/parameters/0",
	} {
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.PathItemRefCollision, at),
			"%s is reported at the use site's own key", at)
	}
	message := openapitest.DiagMessageAt(t, diags, diag.PathItemRefCollision,
		ir.SeverityWarning, "/paths/~1a/get")
	assert.Contains(t, message, "/paths/~1b/get", "the referent's position is named too")

	// The use site's declaration is the one lowered, and the referent's get is
	// not lowered at this mount at all.
	getA := opByPath(t, doc, "GET", "/a")
	assert.Equal(t, "useGet", getA.Name.Source)
	assert.Equal(t, jsontext.Pointer("/paths/~1a/get"), getA.Provenance.Pointer)
	getB := opByPath(t, doc, "GET", "/b")
	assert.Equal(t, "refGet", getB.Name.Source, "the referent still lowers at its own path")

	summary, ok := getA.Unmodeled["openapi:pathItem/summary"]
	require.True(t, ok)
	assert.JSONEq(t, `"use summary"`, string(summary.Value), "the use site's summary survives")
	assert.Equal(t, []string{"https://use.example"}, serversNamed(t, doc, getA.Servers),
		"the use site's servers win the collision")
	ext, ok := getA.Unmodeled["openapi:pathItem/x-shared"]
	require.True(t, ok)
	assert.JSONEq(t, `"use"`, string(ext.Value))
	only, ok := getA.Unmodeled["openapi:pathItem/x-refOnly"]
	require.True(t, ok, "an extension only the referent declares is kept beside the one it cedes")
	assert.Equal(t, jsontext.Pointer("/paths/~1b/x-refOnly"), only.Provenance.Pointer)
	bogus, ok := getA.Unmodeled["openapi:pathItem/bogusBoth"]
	require.True(t, ok)
	assert.JSONEq(t, `{"responses":{"200":{"description":"USE"}}}`, string(bogus.Value),
		"an undeclared key is the use site's too, as every other construct is")
	assert.Equal(t, jsontext.Pointer("/paths/~1a/bogusBoth"), bogus.Provenance.Pointer)
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownKeyEntryTaken),
		"the referent's key is not written, so nothing is left to say it was dropped")
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.PathItemRefCollision, ir.SeverityWarning,
		"/paths/~1a/parameters/0"), `the parameter "dup" in query is declared here`, "a parameter is named as written")
	require.Len(t, getA.Params, 1, "the referent's parameter is not lowered beside the use site's")
	assert.Equal(t, ir.TypeID("t/prim/string"), getA.Params[0].Type.Target, "the use site's is the one kept")
	assert.Equal(t, jsontext.Pointer("/paths/~1a/parameters/0"), getA.Params[0].Provenance.Pointer)

	// One operation at the mount, so no OpID is claimed twice.
	assert.False(t, openapitest.HasDiag(diags, diag.DuplicateOperationID))
	assert.False(t, openapitest.HasDiag(diags, diag.ConflictingOperationID))
}

// pathItemRefOpIDSpec has the operation written beside the $ref repeat the
// referent's operationId, which is two declarations of one id rather than one
// declaration mounted twice (GitHub #502).
const pathItemRefOpIDSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      operationId: sameId
      responses: {"200": {description: OK}}
  /b:
    get:
      operationId: sameId
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_RepeatedOpIDIsAConflict pins which of the two
// operationId findings a repeated id reaches: the sibling's operation is a
// declaration of its own, so repeating the referent's id is the document writing
// the id twice, not one declaration mounted twice.
func TestPathItemRefSiblings_RepeatedOpIDIsAConflict(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, pathItemRefOpIDSpec)

	assert.True(t, openapitest.HasDiag(diags, diag.ConflictingOperationID),
		"two declarations writing one id is a conflict")
	assert.False(t, openapitest.HasDiag(diags, diag.DuplicateOperationID),
		"and not a remount: the sibling operation is not the referent's declaration")
}

// pathItemRefCollisionReversedSpec is pathItemRefCollisionSpec with the two
// paths declared the other way round, for the two-order diff.
const pathItemRefCollisionReversedSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /b:
    summary: ref summary
    description: ref description
    servers: [{url: https://ref.example}]
    x-shared: ref
    x-refOnly: ref
    bogusBoth: {responses: {"200": {description: REF}}}
    parameters:
      - {name: dup, in: query, schema: {type: integer}}
    get:
      operationId: refGet
      responses: {"200": {description: OK}}
  /a:
    $ref: '#/paths/~1b'
    summary: use summary
    description: use description
    servers: [{url: https://use.example}]
    x-shared: use
    bogusBoth: {responses: {"200": {description: USE}}}
    parameters:
      - {name: dup, in: query, schema: {type: string}}
    get:
      operationId: useGet
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_CollisionIsOrderIndependent pins that what a colliding
// mount reports depends on the document and not on the order its two paths were
// declared in: the two fixtures differ only in that order, so the findings must
// be the same set of (code, pointer) pairs.
func TestPathItemRefSiblings_CollisionIsOrderIndependent(t *testing.T) {
	t.Parallel()
	forwardDoc, forward := parseFull(t, pathItemRefCollisionSpec)
	reversedDoc, reversed := parseFull(t, pathItemRefCollisionReversedSpec)

	assert.Equal(t, collisionFindings(t, forward), collisionFindings(t, reversed),
		"a collision is a property of the document, not of the order it was written in")
	for _, at := range []struct{ method, uri string }{{"GET", "/a"}, {"GET", "/b"}} {
		want, got := opByPath(t, forwardDoc, at.method, at.uri), opByPath(t, reversedDoc, at.method, at.uri)
		assert.Empty(t, cmp.Diff(want, got), "what %s %s lowers to, the winners included, follows no order", at.method, at.uri)
	}
}

// collisionFindings returns the colliding constructs' findings as sorted
// "code pointer" strings, which is what must not depend on declaration order.
func collisionFindings(t *testing.T, diags []ir.Diagnostic) []string {
	t.Helper()
	var out []string
	for _, d := range diags {
		if d.Code == diag.PathItemRefCollision {
			out = append(out, d.Code+" "+string(d.Provenance.Pointer))
		}
	}
	slices.Sort(out)
	return out
}
