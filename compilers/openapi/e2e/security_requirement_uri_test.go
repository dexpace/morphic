// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// requirementEntry is a 3.2 document whose requirements name schemes by URI: one
// written in this document under an extension key, one in another file, and a
// path item from that file, whose own requirement names a scheme there.
const requirementEntry = `# entry.yaml
openapi: 3.2.0
info: {title: T, version: "1"}
x-schemes:
  extra: {type: http, scheme: bearer}
paths:
  /elsewhereInSource:
    get:
      operationId: elsewhereInSource
      security: [{"#/x-schemes/extra": []}]
      responses: {"200": {description: ok}}
  /otherDocument:
    get:
      operationId: otherDocument
      security: [{"schemes.yaml#/components/securitySchemes/shared": []}]
      responses: {"200": {description: ok}}
  /mounted:
    $ref: 'schemes.yaml#/components/pathItems/Mounted'
`

// requirementSchemes is the file requirementEntry reads: a scheme, and an
// operation whose requirement names it relative to this file.
const requirementSchemes = `# schemes.yaml
openapi: 3.2.0
info: {title: S, version: "1"}
paths: {}
components:
  pathItems:
    Mounted:
      get:
        operationId: mountedOwn
        security: [{"#/components/securitySchemes/shared": []}]
        responses: {"200": {description: ok}}
  securitySchemes:
    shared: {type: http, scheme: basic}
`

const (
	extraScheme  ir.AuthID = "auth/openapi/x-schemes/extra"
	sharedScheme ir.AuthID = "auth/openapi@schemes.yaml/components/securitySchemes/shared"
)

// compileRequirements compiles entry beside schemes, reading the one from the
// other from disk, with external references allowed or not, verified as
// `morphic compile --verify` verifies a run (compileVerified).
func compileRequirements(t *testing.T, entry, schemes string, allow bool) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	dir := t.TempDir()
	layout(t, dir, map[string]string{"schemes.yaml": schemes})
	doc, diags := compileVerified(t, compilers.Source{Path: filepath.Join(dir, "entry.yaml"), Data: []byte(entry)},
		openapi.Options{AllowExternalRefs: allow})
	require.NotNil(t, doc)
	return doc, diags
}

// schemeOf returns the scheme the one requirement of the operation named op
// names, failing the test unless it names exactly one.
func schemeOf(t *testing.T, doc *ir.Document, op string) ir.AuthID {
	t.Helper()
	o, ok := opByName(doc, op)
	require.True(t, ok, "no operation %s", op)
	require.Len(t, o.Auth, 1, op)
	require.Len(t, o.Auth[0].Schemes, 1, op)
	return o.Auth[0].Schemes[0].Scheme
}

// TestCompile_ASecurityRequirementKeyThatIsAURIIsBoundToItsScheme pins a 3.2
// requirement whose key is no component name but a URI: it is bound as a
// reference, read against the document that writes it, and the scheme it lands
// on is lowered where it is, in the ID space of the document that holds it, and
// placed there. Its kind says what it is: the IR fills Scheme only for an HTTP
// scheme no kind names.
func TestCompile_ASecurityRequirementKeyThatIsAURIIsBoundToItsScheme(t *testing.T) {
	t.Parallel()
	doc, diags := compileRequirements(t, requirementEntry, requirementSchemes, true)

	assert.Empty(t, diags)
	require.Len(t, doc.Auth, 2, "the two schemes, and no scheme standing for a key")
	require.Contains(t, doc.Auth, extraScheme)
	assert.Equal(t, ir.AuthKindHTTPBearer, doc.Auth[extraScheme].Kind)
	require.Contains(t, doc.Auth, sharedScheme)
	assert.Equal(t, ir.AuthKindHTTPBasic, doc.Auth[sharedScheme].Kind)
	for id, at := range map[ir.AuthID]string{
		extraScheme: "entry.yaml#/x-schemes/extra", sharedScheme: "schemes.yaml#/components/securitySchemes/shared",
	} {
		prov := doc.Auth[id].Provenance
		require.Less(t, prov.Source, len(doc.Sources), id)
		assert.Equal(t, at, filepath.Base(doc.Sources[prov.Source].Path)+"#"+string(prov.Pointer),
			"declared where it is written, in the document writing it")
	}

	assert.Equal(t, extraScheme, schemeOf(t, doc, "elsewhereInSource"))
	assert.Equal(t, sharedScheme, schemeOf(t, doc, "otherDocument"))
	assert.Equal(t, sharedScheme, schemeOf(t, doc, "mountedOwn"), "read against the file that writes the requirement")
	assert.Equal(t, []string{"openapi", "openapi@schemes.yaml", "unresolved"}, doc.IDSpaces["auth"],
		"the spaces declared: the source's, the other document's, and the one an unresolved scheme takes")
}

// TestCompile_ASecurityRequirementURIThatLandsNowhereIsUnresolved pins the other
// outcome: a key that names nothing is reported once, at the requirement, its
// scheme of the kind that says so, and every requirement beside it binds as it
// does where none is unresolved.
func TestCompile_ASecurityRequirementURIThatLandsNowhereIsUnresolved(t *testing.T) {
	t.Parallel()
	entry := requirementEntry + `  /nowhere:
    get:
      operationId: nowhere
      security: [{"./nowhere.yaml#/x": []}]
      responses: {"200": {description: ok}}
`
	doc, diags := compileRequirements(t, entry, requirementSchemes, true)

	assert.Equal(t, []string{"error openapi/unresolved-security-scheme /paths/~1nowhere/get/security/0"}, findingsAt(diags))
	missing := schemeOf(t, doc, "nowhere")
	require.Contains(t, doc.Auth, missing)
	assert.Equal(t, ir.AuthKindUnresolved, doc.Auth[missing].Kind)
	assert.Equal(t, extraScheme, schemeOf(t, doc, "elsewhereInSource"))
	assert.Equal(t, sharedScheme, schemeOf(t, doc, "otherDocument"))
	assert.Equal(t, sharedScheme, schemeOf(t, doc, "mountedOwn"))
}

// TestCompile_ASecurityRequirementURIOfAnUnreadDocumentIsUnresolved pins the
// same requirements where external documents are not read: the two that name
// one are reported unresolved, each with the scheme of its own, and the two
// documents' requirements share none.
func TestCompile_ASecurityRequirementURIOfAnUnreadDocumentIsUnresolved(t *testing.T) {
	t.Parallel()
	doc, diags := compileRequirements(t, requirementEntry, requirementSchemes, false)

	for _, pointer := range []string{"/paths/~1otherDocument/get/security/0"} {
		assert.Contains(t, findingsAt(diags), "error "+diag.UnresolvedSecurityScheme+" "+pointer)
	}
	other, mounted := schemeOf(t, doc, "otherDocument"), schemeOf(t, doc, "mountedOwn")
	assert.NotEqual(t, other, mounted, "two documents' keys are two schemes")
	assert.Equal(t, ir.AuthKindUnresolved, doc.Auth[other].Kind)
	assert.Equal(t, ir.AuthKindUnresolved, doc.Auth[mounted].Kind)
}

// TestCompile_TheSameUnresolvedKeyInTwoDocumentsIsTwoSchemes pins that a key
// naming nothing is a scheme of the document that writes it: two documents
// writing one key draw two findings and mint two scheme IDs, not one.
func TestCompile_TheSameUnresolvedKeyInTwoDocumentsIsTwoSchemes(t *testing.T) {
	t.Parallel()
	const key = `[{"#/x-missing": []}]`
	entry := requirementEntry + `  /sameKey:
    get:
      operationId: sameKey
      security: ` + key + `
      responses: {"200": {description: ok}}
`
	schemes := strings.Replace(requirementSchemes, "  securitySchemes:\n", `      post:
        operationId: mountedSameKey
        security: `+key+`
        responses: {"200": {description: ok}}
  securitySchemes:
`, 1)
	doc, diags := compileRequirements(t, entry, schemes, true)

	assert.Equal(t, 1, countCode(diags, diag.UnresolvedSecurityScheme, "/paths/~1sameKey/get/security/0"))
	assert.NotEqual(t, schemeOf(t, doc, "sameKey"), schemeOf(t, doc, "mountedSameKey"),
		"one key written in two documents is two schemes")
}

// TestCompile_ASecurityRequirementURILowersTheSameInEitherOrder pins that a
// scheme a requirement lands on is lowered once, whichever requirement of
// whichever document reaches it first: the entry and the file written with
// every mapping reversed, the extension key before the paths and the paths
// themselves among them, so the other file's requirement reaches the shared
// scheme first, compile to one document, with the same findings. Operations
// are listed in the order the source writes their paths, which is not what
// this pins, so they compare in any order.
func TestCompile_ASecurityRequirementURILowersTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	forward, forwardDiags := compileRequirements(t, requirementEntry, requirementSchemes, true)
	backward, backwardDiags := compileRequirements(t,
		string(reversed(t, requirementEntry)), string(reversed(t, requirementSchemes)), true)

	assert.Empty(t, cmp.Diff(forward, backward,
		cmpopts.IgnoreFields(ir.Document{}, "Sources", "Diagnostics"),
		cmpopts.IgnoreFields(ir.Provenance{}, "Position"),
		cmpopts.SortSlices(func(a, b ir.Operation) bool { return a.ID < b.ID })))
	assert.True(t, slices.Equal(findingsAt(forwardDiags), findingsAt(backwardDiags)))
	assert.Empty(t, findingsAt(backwardDiags))
}

// TestCompile_ARequirementKeyLandingOnNoMechanismIsUnresolved pins a key that
// lands on a Security Scheme Object naming no mechanism, in the source and in
// another document: each is a scheme of the kind that says so, in the one space
// that kind takes, one per object whichever document's key reaches it, and the
// document verifies.
func TestCompile_ARequirementKeyLandingOnNoMechanismIsUnresolved(t *testing.T) {
	t.Parallel()
	entry := strings.Replace(requirementEntry, "{type: http, scheme: bearer}", "{type: http}", 1)
	schemes := strings.Replace(requirementSchemes, "{type: http, scheme: basic}", "{type: http}", 1)
	doc, diags := compileRequirements(t, entry, schemes, true)

	assert.Equal(t, []string{
		"error openapi/incomplete-security-scheme /components/securitySchemes/shared",
		"error openapi/incomplete-security-scheme /x-schemes/extra",
		"info openapi/degraded-construct /components/securitySchemes/shared/type",
		"info openapi/degraded-construct /x-schemes/extra/type",
	}, findingsAt(diags), "each object reported once, where it is written")
	inSource, other := schemeOf(t, doc, "elsewhereInSource"), schemeOf(t, doc, "otherDocument")
	for _, id := range []ir.AuthID{inSource, other} {
		require.Contains(t, doc.Auth, id)
		assert.Equal(t, ir.AuthKindUnresolved, doc.Auth[id].Kind, id)
		space, _ := ir.IDSpace(ir.IDKindAuth, string(id))
		assert.Equal(t, ir.IDSpaceUnresolved, space, id)
	}
	assert.NotEqual(t, inSource, other)
	assert.Equal(t, other, schemeOf(t, doc, "mountedOwn"), "one object is one scheme")
}

// TestCompile_ASchemeAtADocumentsRootIsNamedForItsDocument pins requirement
// keys landing on whole documents, each a Security Scheme Object at its root,
// which no key names: each scheme is named for its document, as a schema there
// is, so two of them take two names and the names pass finds none shared,
// whichever key is written first.
func TestCompile_ASchemeAtADocumentsRootIsNamedForItsDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout(t, dir, map[string]string{
		"shared": "type: http\nscheme: bearer\n",
		"lone":   "type: apiKey\nin: header\nname: X-Key\n",
	})
	for _, keys := range []string{`[{"./shared": []}, {"lone": []}]`, `[{"lone": []}, {"./shared": []}]`} {
		entry := "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a:\n    get:\n" +
			"      operationId: a\n      security: " + keys + "\n      responses: {\"200\": {description: ok}}\n"
		doc, diags := compileVerified(t, compilers.Source{Path: filepath.Join(dir, "entry.yaml"), Data: []byte(entry)},
			openapi.Options{})
		require.NotNil(t, doc)

		assert.Empty(t, findingsAt(diags), "no name-collision, nor any other finding: %s", keys)
		require.Len(t, doc.Auth, 2, keys)
		for id, name := range map[ir.AuthID]string{"auth/openapi@shared": "shared", "auth/openapi@lone": "lone"} {
			require.Contains(t, doc.Auth, id, keys)
			assert.Equal(t, ir.NamingFor(name), doc.Auth[id].Name, "%s: %s", keys, id)
		}
	}
}
