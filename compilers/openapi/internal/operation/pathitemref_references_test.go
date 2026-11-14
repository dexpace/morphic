// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// pathItemRefSiblingsExternalFixture's path item carries a $ref plus siblings,
// and the operation written beside the $ref writes a parameter reference that
// leaves the document for the target fixture beside it
// (resolve_target_path_item_sibling.yaml).
const pathItemRefSiblingsExternalFixture = "../../../../testdata/openapi/resolve_main_path_item_sibling.yaml"

// TestPathItemRefSiblings_ExternalRefFollowsTheCompilesPolicy pins that a
// reference written beside a $ref follows the compile's external-reference
// policy, as every reference the load phase resolves does. With the option the
// target file's component lands on the IR at the sibling operation; without it
// the reference is reported unresolved once, where it is written.
func TestPathItemRefSiblings_ExternalRefFollowsTheCompilesPolicy(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(pathItemRefSiblingsExternalFixture)
	require.NoError(t, err)

	compiled := func(allow bool) (*ir.Document, []ir.Diagnostic) {
		t.Helper()
		opts := compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: allow}}
		if allow {
			opts.Reader = reader.Disk{}
		}
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: pathItemRefSiblingsExternalFixture, Data: append([]byte(nil), data...)}}, opts)
		require.NoError(t, err)
		require.NotNil(t, doc)
		return doc, diags
	}

	allowed, allowedDiags := compiled(true)
	openapitest.RequireNoErrorDiags(t, allowedDiags)
	getA := opByPath(t, allowed, "GET", "/a")
	require.Len(t, getA.Params, 1, "the reference beside the $ref follows into the second file")
	assert.Equal(t, "extSibling", getA.Params[0].Name.Source)

	refused, refusedDiags := compiled(false)
	assert.Equal(t, []string{"/paths/~1a/get/parameters/0"}, unresolvedAt(refusedDiags),
		"without the option the sibling's external reference is reported once, where it is written")
	refusedA := opByPath(t, refused, "GET", "/a")
	assert.Empty(t, refusedA.Params, "and no unresolved parameter reaches the IR")
}

// crossDocumentTarget is a path item in another document whose item-level
// parameter's schema names that document's own S, which the referring
// document also declares, differently.
const crossDocumentTarget = `openapi: 3.1.0
info: {title: Target, version: "1"}
paths:
  /b:
    parameters:
      - {name: inl, in: header, schema: {$ref: '#/components/schemas/S'}}
    post:
      operationId: postB
      responses: {"200": {description: OK}}
components:
  schemas:
    S: {type: integer}
`

// crossDocumentUse refers to crossDocumentTarget's path item and writes an
// item parameter and an operation with one of its own beside the $ref.
const crossDocumentUse = `openapi: 3.1.0
info: {title: Main, version: "1"}
paths:
  /a:
    $ref: './target.yaml#/paths/~1b'
    parameters:
      - {$ref: '#/components/parameters/P'}
    get:
      operationId: getA
      parameters:
        - {$ref: '#/components/parameters/Q'}
      responses: {"200": {description: OK}}
components:
  parameters:
    P: {name: mainP, in: query, schema: {type: string}}
    Q: {name: mainQ, in: query, schema: {type: string}}
  schemas:
    S: {type: string}
`

// TestPathItemRefSiblings_EachSideReadsInItsOwnDocument pins the scope each
// side of a cross-document $ref lowers in, since every operation at the mount
// carries both sides' item parameters. Read in the source, the referent's
// parameter on the use site's operation bound the source's S; read in the
// referent's document, the use site's parameters lost the pointer of their
// declaration.
func TestPathItemRefSiblings_EachSideReadsInItsOwnDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.yaml"), []byte(crossDocumentTarget), 0o600))
	use := filepath.Join(dir, "main.yaml")
	doc, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: use, Data: []byte(crossDocumentUse)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	postB, getA := opByPath(t, doc, "POST", "/a"), opByPath(t, doc, "GET", "/a")
	referent := paramNamed(t, getA, "inl")
	assert.Equal(t, paramNamed(t, postB, "inl").Type, referent.Type,
		"the referent's parameter binds the same type on either side's operation")
	assert.NotEqual(t, ir.TypeID("t/openapi/components/schemas/S"), referent.Type.Target,
		"and never the source's S")
	assert.Equal(t, jsontext.Pointer("/components/parameters/P"), paramNamed(t, postB, "mainP").Provenance.Pointer,
		"the use site's parameter keeps its declaration's pointer on the referent's operation")
	assert.Equal(t, jsontext.Pointer("/components/parameters/Q"), paramNamed(t, getA, "mainQ").Provenance.Pointer,
		"and the use site's operation reads its own references in the source")
}

// paramNamed returns op's parameter whose source name is name.
func paramNamed(t *testing.T, op ir.Operation, name string) ir.Parameter {
	t.Helper()
	for _, p := range op.Params {
		if p.Name.Source == name {
			return p
		}
	}
	require.Failf(t, "parameter not found", "%s has no parameter %q", op.Name.Source, name)
	return ir.Parameter{}
}

// TestPathItemRefSiblings_ReferenceKindsResolve pins that a reference written
// inside the fields beside a $ref is resolved before it is lowered, which the
// load phase does with the rest of the document.
//
// It exercises every reference kind a path-item subtree reaches: a parameter, a
// request body, a response and a callback on the sibling operation, and a
// header, a link and an example on its response. A link that names nothing is
// TestPathItemRefSiblings_UnresolvedLinkIsReported's (GitHub #275).
func TestPathItemRefSiblings_ReferenceKindsResolve(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathItemRefKindsSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	getA := opByPath(t, doc, "GET", "/a")
	require.Len(t, getA.Params, 1, "a $ref'd parameter beside the $ref resolves")
	assert.Equal(t, "refParam", getA.Params[0].Name.Source)
	require.NotNil(t, getA.Request, "a $ref'd request body resolves")
	assert.Equal(t, "application/json", getA.Request.Contents[0].MediaType)

	success := responseByStatus(t, getA, 200)
	assert.Equal(t, 200, success.Conditions.StatusCodes[0].From, "a $ref'd response resolves")

	created := responseByStatus(t, getA, 201)
	require.Len(t, created.Headers, 1, "a $ref'd header on a sibling response resolves")
	assert.Equal(t, "X-Rate", created.Headers[0].WireName)

	require.NotNil(t, created.Payload, "the response written beside the $ref keeps its body")
	require.NotEmpty(t, created.Payload.Contents)
	examples := created.Payload.Contents[0].Examples
	require.Len(t, examples, 1, "a $ref'd example on a sibling media type resolves")
	assert.Equal(t, "refEx", examples[0].Name)
	require.NotNil(t, examples[0].Value, "the referenced example's own value is lowered")

	require.Len(t, created.Links, 1, "a $ref'd link on a sibling response resolves")
	assert.Equal(t, "next", created.Links[0].Name.Source)
	assert.Equal(t, getA.ID, created.Links[0].Operation)
	assert.Equal(t, jsontext.Pointer("/components/links/RefL"), created.Links[0].Provenance.Pointer,
		"the link is declared where its $ref ends")

	cbPost := openapitest.FindOp(t, doc, "cbPost")
	assert.Equal(t, ir.OpID("op/openapi/paths/~1a/get/callbacks/onEvent/{$request.body#~1url}/post"), cbPost.ID,
		"a $ref'd callback resolves, and its operation mounts under the operation beside the $ref")
	require.Len(t, getA.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, []ir.OpID{cbPost.ID}, getA.Bindings.HTTP[0].Callbacks[0].Operations)
}

// responseByStatus returns the single success response an operation declares for
// code, requiring it to be there rather than indexing into a list that a second
// response would have made ambiguous.
func responseByStatus(t *testing.T, op ir.Operation, code int) ir.Response {
	t.Helper()
	for _, r := range op.Responses {
		for _, sc := range r.Conditions.StatusCodes {
			if sc.From == code && sc.To == code {
				return r
			}
		}
	}
	t.Fatalf("operation %s declares no response for status %d", op.ID, code)
	return ir.Response{}
}

const pathItemRefKindsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      operationId: getA
      parameters:
        - {$ref: '#/components/parameters/RefP'}
      requestBody: {$ref: '#/components/requestBodies/RefB'}
      responses:
        "200": {$ref: '#/components/responses/RefR'}
        "201":
          description: created
          headers:
            X-Rate: {$ref: '#/components/headers/RefH'}
          content:
            application/json:
              schema: {type: string}
              examples:
                refEx: {$ref: '#/components/examples/RefE'}
          links:
            next: {$ref: '#/components/links/RefL'}
      callbacks:
        onEvent: {$ref: '#/components/callbacks/RefC'}
  /b:
    post:
      operationId: postB
      responses: {"200": {description: OK}}
components:
  parameters:
    RefP: {name: refParam, in: query, schema: {type: string}}
  requestBodies:
    RefB:
      content:
        application/json: {schema: {type: string}}
  responses:
    RefR:
      description: ok
      content:
        application/json: {schema: {type: string}}
  headers:
    RefH: {schema: {type: integer}}
  examples:
    RefE: {value: {from: component}}
  links:
    RefL: {operationId: getA}
  callbacks:
    RefC:
      '{$request.body#/url}':
        post:
          operationId: cbPost
          responses: {"200": {description: OK}}
`

// pathItemRefDanglingLinkSpec writes a $ref'd link on a sibling response whose
// target names nothing.
const pathItemRefDanglingLinkSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          links:
            next: {$ref: '#/components/links/Missing'}
  /b:
    post:
      operationId: postB
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_UnresolvedLinkIsReported pins that resolution reaches
// a link reference written beside a $ref: a link whose declaration is
// absent is reported like any other unresolved reference there, which is what
// makes the resolving half of the kinds fixture above mean something. It is also
// the one reference kind whose resolution no IR field records — nothing reads a
// resolved link — so the diagnostic is the only evidence that it happened.
func TestPathItemRefSiblings_UnresolvedLinkIsReported(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, pathItemRefDanglingLinkSpec)
	assert.Equal(t, []string{"/paths/~1a/get/responses/200/links/next"}, unresolvedAt(diags),
		"the link reference beside the $ref is resolved with the document and reported where it is written")
}

// pathItemRefBrokenSpec writes a reference beside the $ref that names nothing.
const pathItemRefBrokenSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    parameters:
      - {$ref: '#/components/parameters/Missing'}
    get:
      operationId: getA
      responses: {"200": {description: OK}}
  /b:
    post:
      operationId: postB
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_UnresolvedIsReported pins that a reference written
// beside a $ref that does not resolve is reported once, where it is written,
// rather than silently reaching the IR unresolved.
func TestPathItemRefSiblings_UnresolvedIsReported(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, pathItemRefBrokenSpec)

	assert.Equal(t, []string{"/paths/~1a/parameters/0"}, unresolvedAt(diags),
		"the failure is reported once, at the reference written beside the $ref")
}

// unresolvedAt returns the pointer of every unresolved-ref finding, in the
// order they were reported.
func unresolvedAt(diags []ir.Diagnostic) []string {
	var at []string
	for _, d := range diags {
		if d.Code == diag.UnresolvedRef {
			at = append(at, string(d.Provenance.Pointer))
		}
	}
	return at
}

// TestPathItemRefSiblings_AForeignReferentKeepsItsOwnPositions pins a use site
// writing item parameters beside a $ref to another document's path item: each
// parameter keeps the type its own schema declares, the referent's lowered
// where that document declares it, at the last copy of a key it writes twice,
// as the model reads one. A finding names the referent's positions by the
// reference.
func TestPathItemRefSiblings_AForeignReferentKeepsItsOwnPositions(t *testing.T) {
	t.Parallel()
	const item = `
    parameters:
      - {name: refP, in: header, schema: {type: object, properties: {fromRef: {type: integer}}}}
    get:
      operationId: refGet
      responses: {"200": {description: OK}}
    post:
      operationId: postB
      responses: {"200": {description: OK}}
`
	for name, tc := range map[string]struct {
		target, ref string
		refType     ir.TypeID
	}{
		"declared once": {
			target:  "openapi: 3.1.0\ninfo: {title: Target, version: \"1\"}\npaths:\n  /b:" + item,
			ref:     "./target.yaml#/paths/~1b",
			refType: ids.In("target.yaml").AnonType("/paths/~1b/parameters/0/schema"),
		},
		"a key written twice": {
			target:  "B: {}\nB:" + strings.ReplaceAll(item, "\n    ", "\n  "),
			ref:     "./target.yaml#/B",
			refType: ids.In("target.yaml").AnonType("/B/parameters/0/schema"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "target.yaml"), []byte(tc.target), 0o600))
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Join(dir, "main.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: Main, version: "1"}
paths:
  /a:
    $ref: '` + tc.ref + `'
    parameters:
      - {name: useP, in: query, schema: {type: object, properties: {fromUse: {type: string}}}}
    get:
      operationId: useGet
      responses: {"200": {description: OK}}
`)}}, compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
			require.NoError(t, err)
			require.NotNil(t, doc)
			assert.Empty(t, irverify.Verify(doc))

			for _, op := range []ir.Operation{opByPath(t, doc, "GET", "/a"), opByPath(t, doc, "POST", "/a")} {
				assert.Equal(t, ids.Document{}.AnonType("/paths/~1a/parameters/0/schema"), paramNamed(t, op, "useP").Type.Target)
				assert.Equal(t, tc.refType, paramNamed(t, op, "refP").Type.Target,
					"the referent's parameter keeps its own schema, not the use site's at the same index")
			}
			message := openapitest.DiagMessageAt(t, diags, diag.PathItemRefCollision, ir.SeverityWarning, "/paths/~1a/get")
			assert.Contains(t, message, tc.ref+"/get", "another document's position is named by the reference")
		})
	}
}

// TestPathItemRefSiblings_AWholeDocumentReferentIsNamedByItsFragment pins how a
// finding names a position in a document a $ref takes whole, the common shape
// of a path split into its own file: with the empty fragment, so the pointer
// after it reads as one rather than as a file path.
func TestPathItemRefSiblings_AWholeDocumentReferentIsNamedByItsFragment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "item.yaml"), []byte(`get:
  operationId: itemGet
  responses: {"200": {description: OK}}
`), 0o600))
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "main.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: Main, version: "1"}
paths:
  /a:
    $ref: './item.yaml'
    get:
      operationId: useGet
      responses: {"200": {description: OK}}
`)}}, compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	message := openapitest.DiagMessageAt(t, diags, diag.PathItemRefCollision, ir.SeverityWarning, "/paths/~1a/get")
	assert.Contains(t, message, "again at ./item.yaml#/get in", "the position is the fragment of the document named")
}

// TestPathItemRefSiblings_AnAliasDeclaresAtItsOwnPosition pins where an alias
// of a use site declares the fields beside its $ref: at the alias's own path,
// as an aliased inline item declares there, not where the load phase first met
// the mapping.
func TestPathItemRefSiblings_AnAliasDeclaresAtItsOwnPosition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, item string }{
		{"beside a $ref", `{$ref: '#/paths/~1b', put: {operationId: putA, responses: {"200": {description: OK}}}}`},
		{"an inline item", `{put: {operationId: putA, responses: {"200": {description: OK}}}}`},
	} {
		doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: &a `+tc.item+`
  /x: *a
  /b:
    get: {operationId: getB, responses: {"200": {description: OK}}}
`)
		openapitest.RequireNoErrorDiags(t, diags)

		assert.Equal(t, jsontext.Pointer("/paths/~1a/put"), opByPath(t, doc, "PUT", "/a").Provenance.Pointer, tc.name)
		assert.Equal(t, jsontext.Pointer("/paths/~1x/put"), opByPath(t, doc, "PUT", "/x").Provenance.Pointer,
			"%s: the alias declares at its own path", tc.name)
	}
}

// TestPathItemRefSiblings_AMergedRefIsNoReference pins the library's reading of
// a $ref a merge key supplies: none, so the item is inline, and nothing in it
// collides with itself.
func TestPathItemRefSiblings_AMergedRefIsNoReference(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
x-base: &base {$ref: '#/paths/~1b'}
paths:
  /a:
    <<: *base
    put: {responses: {"200": {description: OK}}}
  /b:
    get: {responses: {"200": {description: OK}}}
`)
	assert.False(t, openapitest.HasDiag(diags, diag.PathItemRefCollision))
}

// besideRefPaths are /a and /b of every case below: /a writes get, put and its
// item parameters beside a $ref to /b, which declares a get and parameters too,
// and a pointer to one of those fields reads /a's own, which /a's mount declares
// at the pointer the reference spells. /a is anchored for an
// alias to bring the same fields to a second path.
const besideRefPaths = `  /a: &a
    $ref: '#/paths/~1b'
    parameters: [{name: near, in: query, schema: {type: string}}]
    get:
      operationId: getA
      requestBody: {content: {application/json: {schema: {type: object, properties: {near: {type: string}}}}}}
      responses: {"200": {description: OK}}
    put:
      operationId: putA
      requestBody: {content: {application/json: {schema: {type: object, properties: {nearPut: {type: string}}}}}}
      responses: {"200": {description: OK}}
  /b:
    parameters: [{name: far, in: query, schema: {type: integer}}]
    get:
      operationId: getB
      requestBody: {content: {application/json: {schema: {type: object, properties: {far: {type: string}}}}}}
      responses: {"200": {description: OK}}
    post:
      operationId: postB
      requestBody: {content: {application/json: {schema: {type: object, properties: {farPost: {type: string}}}}}}
      responses: {"200": {description: OK}}
`

// besideRefSpec returns a document holding besideRefPaths, then paths, then
// rest, whose top-level keys follow.
func besideRefSpec(paths, rest string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" + besideRefPaths + paths + rest
}

// besideRefChain is /c taking its request body through a component that names
// /a's get request body.
const besideRefChain = `  /c:
    post:
      operationId: postC
      requestBody: {$ref: '#/components/requestBodies/X'}
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_APointerBesideTheRefReadsTheFieldsInEitherOrder pins
// a reference into the fields beside a $ref (GitHub #824): a pointer reads a
// key the path item writes beside its $ref where it is written, as the mount
// keeps the nearer layer (docs/compilers/openapi.md#path-item-ref-fields), so
// /c's body, through X, is the one /a's get declares, the node /a's own mount
// interns, in either declaration order, and X is reached.
func TestPathItemRefSiblings_APointerBesideTheRefReadsTheFieldsInEitherOrder(t *testing.T) {
	t.Parallel()
	const components = "components:\n  requestBodies:\n    X: {$ref: '#/paths/~1a/get/requestBody'}\n"
	first, firstDiags := parseFull(t, besideRefSpec(besideRefChain, components))
	second, secondDiags := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n"+
		besideRefChain+besideRefPaths+components)

	for name, doc := range map[string]*ir.Document{"/a first": first, "/c first": second} {
		own := openapitest.BodyTarget(t, openapitest.FindOp(t, doc, "getA").Request)
		body, ok := doc.Types[own].(*ir.Model)
		require.True(t, ok, name)
		assert.Equal(t, "near", body.Properties[0].WireName, "%s: /a keeps its own body", name)
		assert.Equal(t, own, openapitest.BodyTarget(t, openapitest.FindOp(t, doc, "postC").Request),
			"%s: /c takes the same one", name)
		assert.NotContains(t, doc.Unmodeled, "openapi:components/requestBodies/X", "%s: X is reached", name)
	}
	assert.Empty(t, cmp.Diff(first.Types, second.Types), "the registry does not depend on the order")
	assert.Empty(t, errorsAt(firstDiags))
	assert.Empty(t, errorsAt(secondDiags))
}

// TestPathItemRefSiblings_APointerIntoTheFieldsTakesWhatTheyDeclare pins that
// a pointer into the fields beside a $ref takes what they declare however the
// document is read: by a pointer, by the source's file name, through /z, whose
// YAML alias writes /a's fields again at a position of its own, and into put,
// which only /a writes, with or without a reader of other documents. /c lowers
// the declaration the mount interns at the same pointer, in both declaration
// orders, with nothing reported.
func TestPathItemRefSiblings_APointerIntoTheFieldsTakesWhatTheyDeclare(t *testing.T) {
	t.Parallel()
	for _, opts := range []compilers.Options{{}, {Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}}} {
		compile := func(src string) (*ir.Document, []ir.Diagnostic) {
			doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: []byte(src)}}, opts)
			require.NoError(t, err)
			require.NotNil(t, doc)
			return doc, diags
		}
		for _, tc := range []struct{ ref, method, mount, wire string }{
			{"#/paths/~1a/get/requestBody", "GET", "/a", "near"},
			{"spec.yaml#/paths/~1a/get/requestBody", "GET", "/a", "near"},
			{"spec.yaml#/paths/~1z/get/requestBody", "GET", "/z", "near"},
			{"#/paths/~1a/put/requestBody", "PUT", "/a", "nearPut"},
		} {
			ref := tc.ref
			components := "components:\n  requestBodies:\n    X: {$ref: '" + ref + "'}\n"
			first, firstDiags := compile(besideRefSpec("  /z: *a\n"+besideRefChain, components))
			second, secondDiags := compile("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
				besideRefChain + besideRefPaths + "  /z: *a\n" + components)

			for name, doc := range map[string]*ir.Document{"/a first": first, "/c first": second} {
				own := openapitest.BodyTarget(t, opByPath(t, doc, tc.method, tc.mount).Request)
				body, ok := doc.Types[own].(*ir.Model)
				require.True(t, ok, "%s %s", ref, name)
				assert.Equal(t, tc.wire, body.Properties[0].WireName, "%s %s: %s keeps its own body", ref, name, tc.mount)
				assert.Equal(t, own, openapitest.BodyTarget(t, openapitest.FindOp(t, doc, "postC").Request),
					"%s %s: /c takes the same one", ref, name)
			}
			assert.Empty(t, cmp.Diff(first.Types, second.Types), "%s: the registry does not depend on the order", ref)
			assert.Empty(t, errorsAt(firstDiags), ref)
			assert.Empty(t, errorsAt(secondDiags), ref)
		}
	}
}

// TestPathItemRefSiblings_AReferenceIntoTheFieldsBesideARefReadsThem pins a
// reference into the fields beside a path item's $ref on every route a
// reference reaches a pointer by: a $ref of each kind, a discriminator mapping
// value, a reference written inside the fields themselves, and an alias's
// second position. Each takes what /a writes there, which /a's own mount
// lowers at that pointer; a key /a writes nothing beside its $ref under is read
// in /b, the item it names. Nothing is reported.
func TestPathItemRefSiblings_AReferenceIntoTheFieldsBesideARefReadsThem(t *testing.T) {
	t.Parallel()
	const discriminated = `      requestBody:
        content:
          application/json:
            schema:
              oneOf: [{$ref: '#/components/schemas/Cat'}]
              discriminator: {propertyName: k, mapping: {cat: '#/components/schemas/Cat', near: %s}}
      responses: {"200": {description: OK}}
components:
  schemas:
    Cat: {type: object, properties: {k: {type: string}}}
`
	// wires returns the property names of the body /c's operation of method
	// takes.
	wires := func(method string) func(*ir.Document) string {
		return func(doc *ir.Document) string {
			body, ok := doc.Types[openapitest.BodyTarget(t, opByPath(t, doc, method, "/c").Request)].(*ir.Model)
			if !ok {
				return ""
			}
			return body.Properties[0].WireName
		}
	}
	cases := []struct {
		name, paths, want string
		took              func(*ir.Document) string
	}{
		{"schema", `  /c:
    post:
      requestBody: {content: {application/json: {schema: {$ref: '#/paths/~1a/get/requestBody/content/application~1json/schema'}}}}
      responses: {"200": {description: OK}}
`, "near", wires("POST")},
		{"a schema's $ref through a component", `  /c:
    post:
      requestBody: {content: {application/json: {schema: {$ref: '#/components/schemas/X'}}}}
      responses: {"200": {description: OK}}
components:
  schemas:
    X: {$ref: '#/paths/~1a/get/requestBody/content/application~1json/schema'}
`, "near", func(doc *ir.Document) string {
			x, ok := doc.Types["t/openapi/components/schemas/X"].(*ir.Scalar)
			if !ok || x.Base == nil {
				return ""
			}
			body, ok := doc.Types[x.Base.Target].(*ir.Model)
			if !ok {
				return ""
			}
			return body.Properties[0].WireName
		}},
		{"parameter by index", `  /c:
    post:
      parameters: [{$ref: '#/paths/~1a/parameters/0'}]
      responses: {"200": {description: OK}}
`, "near", func(doc *ir.Document) string { return opByPath(t, doc, "POST", "/c").Params[0].Name.Source }},
		{"mapping value", "  /c:\n    post:\n" +
			strings.Replace(discriminated, "%s", "'#/paths/~1a/get/requestBody/content/application~1json/schema'", 1),
			"near", func(doc *ir.Document) string {
				if mapsTag(doc, "near") {
					return "near"
				}
				return ""
			}},
		{"inside the fields", `  /c:
    $ref: '#/paths/~1b'
    get:
      requestBody: {$ref: '#/paths/~1a/get/requestBody'}
      responses: {"200": {description: OK}}
`, "near", wires("GET")},
		{"a key not written beside", `  /c:
    post:
      requestBody: {$ref: '#/paths/~1a/post/requestBody'}
      responses: {"200": {description: OK}}
`, "farPost", wires("POST")},
		{"an alias's position", `  /d: *a
  /c:
    post:
      requestBody: {$ref: '#/paths/~1d/get/requestBody'}
      responses: {"200": {description: OK}}
`, "near", wires("POST")},
		{"a key only written beside", `  /c:
    post:
      requestBody: {$ref: '#/paths/~1a/put/requestBody'}
      responses: {"200": {description: OK}}
`, "nearPut", wires("POST")},
	}
	for _, tc := range cases {
		doc, diags := parseFull(t, besideRefSpec(tc.paths, ""))
		assert.Empty(t, errorsAt(diags), tc.name)
		assert.Equal(t, tc.want, tc.took(doc), tc.name)
	}
}

// TestPathItemRefSiblings_ADefsPointerBesideARefReadsItsOwnDefinitions pins a
// "#/$defs/..." pointer written in the fields beside a $ref, for a $ref and for
// a mapping value: its definitions are read from where it is written, /a's own
// fields, whose schema defines Y with near, not through /a's $ref into /b's.
func TestPathItemRefSiblings_ADefsPointerBesideARefReadsItsOwnDefinitions(t *testing.T) {
	t.Parallel()
	const schema = `            schema:
              $defs: {Y: {type: object, properties: {k: {type: string}, %s: {type: string}}}}
              type: object
              properties: {y: {$ref: '#/$defs/Y'}}
              oneOf: [{$ref: '#/components/schemas/Cat'}]
              discriminator: {propertyName: k, mapping: {cat: '#/components/schemas/Cat', y: '#/$defs/Y'}}
`
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      requestBody:
        content:
          application/json:
` + strings.Replace(schema, "%s", "near", 1) + `      responses: {"200": {description: OK}}
  /b:
    get:
      requestBody:
        content:
          application/json:
` + strings.Replace(schema, "%s", "far", 1) + `      responses: {"200": {description: OK}}
components:
  schemas:
    Cat: {type: object, properties: {k: {type: string}}}
`
	doc, diags := parseFull(t, spec)

	const at = "/paths/~1a/get/requestBody/content/application~1json/schema"
	assert.Empty(t, errorsAt(diags))
	y, ok := doc.Types[ir.TypeID("t/anon"+at+"/$defs/Y")].(*ir.Model)
	require.True(t, ok, "/a's own definition is hoisted where /a writes it")
	assert.Equal(t, "near", y.Properties[1].WireName)
	assert.True(t, mapsTag(doc, "y"))
}

// mapsTag reports whether a discriminator anywhere in doc maps tag.
func mapsTag(doc *ir.Document, tag string) bool {
	for _, td := range doc.Types {
		var d *ir.Discriminator
		switch n := td.(type) {
		case *ir.Union:
			d = n.Discriminator
		case *ir.Model:
			d = n.Discriminator
		default:
			continue
		}
		if d == nil {
			continue
		}
		if _, mapped := d.Mapping[tag]; mapped {
			return true
		}
	}
	return false
}

// errorsAt returns the messages of the unresolved-ref errors in diags by
// position.
func errorsAt(diags []ir.Diagnostic) map[jsontext.Pointer][]string {
	out := map[jsontext.Pointer][]string{}
	for _, d := range diags {
		if d.Severity == ir.SeverityError && d.Code == diag.UnresolvedRef {
			out[d.Provenance.Pointer] = append(out[d.Provenance.Pointer], d.Message)
		}
	}
	return out
}

// TestPathItemRefSiblings_APointerIntoAnotherDocumentIsNotRefused pins that a
// pointer is read in the document it names. /c names ext.yaml's /a, whose
// pointer spells a position /a here writes beside its $ref, so a reading of
// the pointer alone would take the source's field for ext.yaml's.
func TestPathItemRefSiblings_APointerIntoAnotherDocumentIsNotRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(`openapi: 3.1.0
info: {title: E, version: "1"}
paths:
  /a:
    get:
      requestBody: {content: {application/json: {schema: {type: object, properties: {ext: {type: string}}}}}}
      responses: {"200": {description: OK}}
`), 0o600))
	root := filepath.Join(dir, "root.yaml")
	src := besideRefSpec(`  /c:
    post:
      operationId: postC
      requestBody: {$ref: 'ext.yaml#/paths/~1a/get/requestBody'}
      responses: {"200": {description: OK}}
`, "")
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: root, Data: []byte(src)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	assert.Empty(t, errorsAt(diags))
	body, ok := doc.Types[openapitest.BodyTarget(t, openapitest.FindOp(t, doc, "postC").Request)].(*ir.Model)
	require.True(t, ok, "/c lowers the other document's body")
	assert.Equal(t, "ext", body.Properties[0].WireName)
}
