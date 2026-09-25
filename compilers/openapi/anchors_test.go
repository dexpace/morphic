package openapi

import (
	"encoding/json/v2"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// anchorName matches the anchors the fixtures below write. None of them writes
// an `&` anywhere else.
var anchorName = regexp.MustCompile(`&[A-Za-z0-9_]+`)

// unanchored returns src with every anchor blanked to spaces of its own width,
// so each line and column of the twin is the anchored document's own and the
// two compile to documents whose provenance agrees.
func unanchored(src string) string {
	return anchorName.ReplaceAllStringFunc(src, func(a string) string { return strings.Repeat(" ", len(a)) })
}

// anchorFixtureHead opens every fixture: a document with one security scheme,
// so a requirement naming it has something to name.
const anchorFixtureHead = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
	"components:\n  securitySchemes:\n    key: {type: apiKey, in: header, name: X-Key}\n"

// TestCompile_AnAnchoredEntryCompilesAsItsUnanchoredTwin pins GitHub #459 at
// every model the parser folds into a map. The parser skipped such an entry
// when its value carried an anchor, taking it for an alias definition, and the
// entry reached the IR in no form and with no diagnostic: a whole path item, an
// operation, a response, a callback, or a security requirement — which came
// back empty, and an empty requirement is the one that admits a caller with no
// credentials at all. An anchor names a node for an alias to reuse and says
// nothing about the node itself, so a document and its unanchored twin are one
// document, and they must compile to one IR.
func TestCompile_AnAnchoredEntryCompilesAsItsUnanchoredTwin(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, paths string }{
		{"a path item", `
  /x: &item
    get: {operationId: getX, responses: {"200": {description: ok}}}
`},
		{"an operation", `
  /x:
    get: &op
      operationId: getX
      responses: {"200": {description: ok}}
`},
		{"a response", `
  /x:
    get:
      operationId: getX
      responses:
        "200": {description: ok}
        "404": &nf {description: not found}
`},
		{"a callback expression", `
  /x:
    post:
      operationId: postX
      responses: {"200": {description: ok}}
      callbacks:
        onEvent:
          "{$request.body#/url}": &cb
            post: {operationId: onEventPost, responses: {"200": {description: ok}}}
`},
		{"a security requirement", `
  /x:
    get:
      operationId: getX
      security:
        - key: &scopes []
      responses: {"200": {description: ok}}
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := anchorFixtureHead + "paths:" + tc.paths
			require.True(t, anchorName.MatchString(src), "sanity: the fixture carries an anchor")

			anchored, diags := parseFull(t, src)
			openapitest.RequireNoErrorDiags(t, diags)
			twin, twinDiags := parseFull(t, unanchored(src))
			openapitest.RequireNoErrorDiags(t, twinDiags)

			if diff := cmp.Diff(withoutSourceHash(twin), withoutSourceHash(anchored)); diff != "" {
				t.Errorf("the anchored document compiled to other IR than its twin (-twin +anchored):\n%s", diff)
			}
		})
	}
}

// TestCompile_AnAliasStillStandsForAReleasedAnchor pins the half the twin
// comparison cannot: a twin has no alias to resolve. The anchor's name is
// cleared before the model is built, and an alias reaches its target through
// the pointer the YAML parser resolved it to, so the alias still stands for the
// anchored response — here, both operations carry the one 404 it names.
func TestCompile_AnAliasStillStandsForAReleasedAnchor(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, anchorFixtureHead+`paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {description: ok}
        "404": &nf {description: SHARED_NOT_FOUND}
  /b:
    get:
      operationId: getB
      responses:
        "200": {description: ok}
        "404": *nf
`)
	openapitest.RequireNoErrorDiags(t, diags)

	for _, name := range []string{"getA", "getB"} {
		op, ok := operationNamed(doc, name)
		require.True(t, ok, "%s is lowered", name)
		lowered, err := json.Marshal(op)
		require.NoError(t, err)
		assert.Contains(t, string(lowered), "SHARED_NOT_FOUND", "%s carries the anchored 404", name)
	}
}

// withoutSourceHash returns doc with each source's content hash cleared: the
// one field a document and its twin legitimately differ in, since their bytes
// do.
func withoutSourceHash(doc *ir.Document) *ir.Document {
	out := *doc
	out.Sources = make([]ir.SourceInfo, len(doc.Sources))
	for i, s := range doc.Sources {
		s.Hash = ""
		out.Sources[i] = s
	}
	return &out
}

// operationNamed finds the operation whose source operationId is name, in any
// group of any service.
func operationNamed(doc *ir.Document, name string) (ir.Operation, bool) {
	groups := make([]ir.OperationGroup, 0, len(doc.Services))
	for _, svc := range doc.Services {
		groups = append(groups, svc.Groups...)
	}
	for len(groups) > 0 {
		g := groups[0]
		groups = append(groups[1:], g.Groups...)
		for _, op := range g.Operations {
			if op.Name.Source == name {
				return op, true
			}
		}
	}
	return ir.Operation{}, false
}
