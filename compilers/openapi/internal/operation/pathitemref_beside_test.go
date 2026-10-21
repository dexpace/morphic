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
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// besideRefPaths are /a and /b of every case below: /a writes get, put and its
// item parameters beside a $ref to /b, which declares a get and parameters too,
// so the resolver reads a pointer through /a's $ref to /b's, while /a's mount
// declares its own at the pointer the reference spells. /a is anchored for an
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
// /a's get request body, which the component walk resolves after the paths, so
// the reference resolves whichever path the document declares first.
const besideRefChain = `  /c:
    post:
      operationId: postC
      requestBody: {$ref: '#/components/requestBodies/X'}
      responses: {"200": {description: OK}}
`

// TestPathItemRefSiblings_APointerPastTheRefIsRefusedInEitherOrder pins the
// refusal of a reference into the fields beside a $ref (GitHub #824). The
// resolver read #/paths/~1a/get/requestBody as /b's, and the mount declares
// /a's own at that pointer, so whichever lowered first gave both its type: /c's
// body took /a's schema when /a came first. Refused, neither order lowers one,
// both report it where it is written, and both keep X, which nothing lowers,
// verbatim.
func TestPathItemRefSiblings_APointerPastTheRefIsRefusedInEitherOrder(t *testing.T) {
	t.Parallel()
	const components = "components:\n  requestBodies:\n    X: {$ref: '#/paths/~1a/get/requestBody'}\n"
	first, firstDiags := parseFull(t, besideRefSpec(besideRefChain, components))
	second, secondDiags := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n"+
		besideRefChain+besideRefPaths+components)

	for name, doc := range map[string]*ir.Document{"/a first": first, "/c first": second} {
		postC := openapitest.FindOp(t, doc, "postC")
		assert.Nil(t, postC.Request, "%s: /c takes no body through the refused reference", name)
		getA := openapitest.FindOp(t, doc, "getA")
		require.NotNil(t, getA.Request, name)
		body, ok := doc.Types[getA.Request.Contents[0].Type.Target].(*ir.Model)
		require.True(t, ok, name)
		assert.Equal(t, "near", body.Properties[0].WireName, "%s: /a keeps its own body", name)
		assert.Equal(t, ir.ReasonNoIRHome, doc.Unmodeled["openapi:components/requestBodies/X"].Reason,
			"%s: the component only the refused chain passes through is kept", name)
	}
	assert.Empty(t, cmp.Diff(first.Types, second.Types), "the registry does not depend on the order")
	for name, diags := range map[string][]ir.Diagnostic{"/a first": firstDiags, "/c first": secondDiags} {
		errs := errorsAt(diags)
		assert.Len(t, errs["/paths/~1c/post/requestBody"], 1, "%s: reported once at /c", name)
		assert.Len(t, errs["/components/requestBodies/X"], 1, "%s: and once at the component", name)
	}
	assert.Contains(t, errorsAt(firstDiags)["/paths/~1c/post/requestBody"][0], "GitHub #824")
}

// TestPathItemRefSiblings_AnAnswerWrittenThereIsTakenInEitherOrder pins the one
// answer the refusal takes. With external references allowed, the load phase
// holds the source for the resolver, so the chain through X finds the body /a's
// get writes, by a pointer or by the source's file name, and through /z, which
// aliases /a, a copy of it. It finds put's too, which only /a writes and which
// without them the resolver finds nowhere (GitHub #824). Each is the
// declaration the mount interns at the same pointer, so /c lowers it in both
// declaration orders with nothing reported.
func TestPathItemRefSiblings_AnAnswerWrittenThereIsTakenInEitherOrder(t *testing.T) {
	t.Parallel()
	compile := func(src string) (*ir.Document, []ir.Diagnostic) {
		doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
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

// TestPathItemRefSiblings_AReferenceIntoTheFieldsBesideARefIsRefused pins the
// refusal on every route a reference reaches a pointer by: a $ref of each kind,
// a discriminator mapping value, a reference written inside the fields
// themselves, and an alias's second position. A "#/$defs/..." pointer written
// in those fields is one too, since its definition is read through the $ref.
// Each is one error at the reference saying why, and nothing interned for it. A
// pointer to a key nothing writes beside the $ref is left as main reads it, and
// one only those fields write fails in the resolver, reported once there.
func TestPathItemRefSiblings_AReferenceIntoTheFieldsBesideARefIsRefused(t *testing.T) {
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
	const (
		refused    = "refused"    // reported here, at the reference, saying why
		unresolved = "unresolved" // reported by the resolver, which found nothing
		resolved   = "resolved"   // nothing to report
	)
	// leaked reports whether /c lowered the construct the refused reference
	// names, as the resolver read it.
	bodyAt := func(method string) func(*ir.Document) bool {
		return func(doc *ir.Document) bool { return opByPath(t, doc, method, "/c").Request != nil }
	}
	cases := []struct {
		name, paths string
		site        jsontext.Pointer
		want        string
		leaked      func(*ir.Document) bool
	}{
		{"schema", `  /c:
    post:
      requestBody: {content: {application/json: {schema: {$ref: '#/paths/~1a/get/requestBody/content/application~1json/schema'}}}}
      responses: {"200": {description: OK}}
`, "/paths/~1c/post/requestBody/content/application~1json/schema", refused,
			func(doc *ir.Document) bool {
				return openapitest.BodyTarget(t, opByPath(t, doc, "POST", "/c").Request) != ir.PrimTypeID(ir.PrimAny)
			}},
		{"a schema's $ref to a refused one", `  /c:
    post:
      requestBody: {content: {application/json: {schema: {$ref: '#/components/schemas/X'}}}}
      responses: {"200": {description: OK}}
components:
  schemas:
    X: {$ref: '#/paths/~1a/get/requestBody/content/application~1json/schema'}
`, "/components/schemas/X", refused, func(doc *ir.Document) bool {
			x, ok := doc.Types["t/openapi/components/schemas/X"].(*ir.Scalar)
			return !ok || x.Base == nil || x.Base.Target != ir.PrimTypeID(ir.PrimAny)
		}},
		{"parameter by index", `  /c:
    post:
      parameters: [{$ref: '#/paths/~1a/parameters/0'}]
      responses: {"200": {description: OK}}
`, "/paths/~1c/post/parameters/0", refused,
			func(doc *ir.Document) bool { return len(opByPath(t, doc, "POST", "/c").Params) > 0 }},
		{"mapping value", "  /c:\n    post:\n" +
			strings.Replace(discriminated, "%s", "'#/paths/~1a/get/requestBody/content/application~1json/schema'", 1),
			"/paths/~1c/post/requestBody/content/application~1json/schema/discriminator/mapping/near", refused,
			func(doc *ir.Document) bool { return mapsTag(doc, "near") }},
		{"inside the fields", `  /c:
    $ref: '#/paths/~1b'
    get:
      requestBody: {$ref: '#/paths/~1a/get/requestBody'}
      responses: {"200": {description: OK}}
`, "/paths/~1c/get/requestBody", refused, bodyAt("GET")},
		{"a key not written beside", `  /c:
    post:
      requestBody: {$ref: '#/paths/~1a/post/requestBody'}
      responses: {"200": {description: OK}}
`, "/paths/~1c/post/requestBody", resolved, nil},
		{"an alias's position", `  /d: *a
  /c:
    post:
      requestBody: {$ref: '#/paths/~1d/get/requestBody'}
      responses: {"200": {description: OK}}
`, "/paths/~1c/post/requestBody", refused, bodyAt("POST")},
		{"a key only written beside", `  /c:
    post:
      requestBody: {$ref: '#/paths/~1a/put/requestBody'}
      responses: {"200": {description: OK}}
`, "/paths/~1c/post/requestBody", unresolved, bodyAt("POST")},
	}
	for _, tc := range cases {
		doc, diags := parseFull(t, besideRefSpec(tc.paths, ""))
		errs := errorsAt(diags)
		if tc.want == resolved {
			assert.Empty(t, errs, "%s: resolved as main resolves it", tc.name)
			continue
		}
		require.Len(t, errs[tc.site], 1, "%s: one report at the reference: %v", tc.name, errs)
		assert.Len(t, errs, 1, "%s: and no other: %v", tc.name, errs)
		assert.Equal(t, tc.want == refused, strings.Contains(errs[tc.site][0], "GitHub #824"),
			"%s: %s", tc.name, errs[tc.site][0])
		assert.False(t, tc.leaked(doc), "%s: nothing is lowered for the reference", tc.name)
	}
}

// TestPathItemRefSiblings_ADefsPointerBesideARefIsRefused pins a "#/$defs/..."
// pointer written in the fields beside a $ref, for a $ref and for a mapping
// value. Its definitions are read from where it is written, so through /a's
// $ref: the mapping's took /b's Y, hoisted at /a's own position with /b's
// properties, and the $ref found none. Each is now one error saying why.
func TestPathItemRefSiblings_ADefsPointerBesideARefIsRefused(t *testing.T) {
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
	errs := errorsAt(diags)
	for _, site := range []jsontext.Pointer{at + "/properties/y", at + "/discriminator/mapping/y"} {
		require.Len(t, errs[site], 1, "one report at %s: %v", site, errs)
		assert.Contains(t, errs[site][0], "as is any definition it can name", site)
	}
	assert.NotContains(t, doc.Types, ir.TypeID("t/anon"+at+"/$defs/Y"),
		"nothing is hoisted at /a's position from /b's definitions")
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

// TestPathItemRefSiblings_APointerIntoAnotherDocumentIsNotRefused pins that the
// refusal reads a pointer in the source only. /c names ext.yaml's /a, whose
// pointer spells a position /a here writes beside its $ref, so a check reading
// the pointer alone would refuse a reference the resolver read correctly.
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
