// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func assertExtensionsX(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	raw, ok := m.Unmodeled["openapi:x-rate-limit"]
	require.True(t, ok, "x-* extensions are namespaced under openapi:")
	assert.JSONEq(t, "100", string(raw.Value))
	assert.Equal(t, ir.ReasonVendorExtension, raw.Reason)

	assertEveryObjectKeepsItsExtensions(t, doc)
	assertRawPreservedBinary(t, m)
}

// assertEveryObjectKeepsItsExtensions holds the whole contract, not the
// positions that happened to be noticed: every OpenAPI object that admits an
// x-* keeps it. The fixture writes `x-mark` on each with a value naming the
// object, so a row that stops arriving names the lowering that stopped reading.
//
// Carriers are derived from the value graph: a row says which Unmodeled map the
// entry must land on by the path the walk reaches it at. The match is a
// substring one, so a bare ".Unmodeled" ends every path and checks nothing;
// carrierNarrows holds each row to narrowing the document.
func assertEveryObjectKeepsItsExtensions(t *testing.T, doc *ir.Document) {
	t.Helper()
	sites := unmodeledSites(doc)
	for _, tc := range []struct{ object, key, want, carrier string }{
		{"openapi root", "openapi:x-audience", `"public"`, "doc.Unmodeled"},
		{"info", "openapi:info/x-mark", `"XINFO"`, "doc.Unmodeled"},
		{"contact", "openapi:info/contact/x-mark", `"XCONTACT"`, "doc.Unmodeled"},
		{"license", "openapi:info/license/x-mark", `"XLICENSE"`, "doc.Unmodeled"},
		{"externalDocs", "openapi:externalDocs/x-mark", `"XEXTERNALDOCS"`, "doc.Unmodeled"},
		{"components", "openapi:components/x-mark", `"XCOMPONENTS"`, "doc.Unmodeled"},
		{"tag", "openapi:tags/0/x-mark", `"XTAG"`, "doc.Unmodeled"},
		{"tag externalDocs", "openapi:tags/0/externalDocs/x-mark", `"XTAGEXTERNALDOCS"`, "doc.Unmodeled"},
		{"server", "openapi:x-mark", `"XSERVER"`, "doc.Servers[server/openapi/servers/0].Unmodeled"},
		{"server variable", "openapi:x-mark", `"XSERVERVARIABLE"`, "doc.Servers[server/openapi/servers/0].Variables[0].Unmodeled"},
		{"paths", "openapi:paths/x-mark", `"XPATHS"`, "doc.Services[0].Unmodeled"},
		{"path item", "openapi:pathItem/x-mark", `"XPATHITEM"`, ".Operations[0].Unmodeled"},
		{"operation", "openapi:x-internal", `true`, ".Operations[0].Unmodeled"},
		{"operation externalDocs", "openapi:externalDocs/x-mark", `"XOPERATIONEXTERNALDOCS"`,
			".Operations[0].Unmodeled"},
		{"responses", "openapi:responses/x-mark", `"XRESPONSES"`, ".Operations[0].Unmodeled"},
		{"parameter", "openapi:x-mark", `"XPARAMETER"`, ".Params[0].Unmodeled"},
		{"request body", "openapi:x-mark", `"XREQUESTBODY"`, ".Request.Unmodeled"},
		{"media type", "openapi:x-mark", `"XMEDIATYPE"`, ".Contents[0].Unmodeled"},
		{"encoding", "openapi:encoding/f/x-mark", `"XENCODING"`, ".Contents[0].Unmodeled"},
		{"example", "openapi:x-mark", `"XEXAMPLE"`, ".Examples[0].Unmodeled"},
		{"response", "openapi:x-mark", `"XRESPONSE"`, ".Responses[0].Unmodeled"},
		{"error response", "openapi:x-mark", `"XERRORRESPONSE"`, ".Errors[0].Unmodeled"},
		{"header", "openapi:x-mark", `"XHEADER"`, ".Headers[0].Unmodeled"},
		{"callback", "openapi:callbacks/onEvent/x-mark", `"XCALLBACK"`, ".Bindings.HTTP[0].Unmodeled"},
		{"schema xml", "openapi:xml/x-mark", `"XXML"`, "doc.Types[t/openapi/components/schemas/S].Unmodeled"},
		{"schema externalDocs", "openapi:externalDocs/x-mark", `"XSCHEMAEXTERNALDOCS"`,
			"doc.Types[t/openapi/components/schemas/S].Unmodeled"},
		{"discriminator", "openapi:discriminator/x-mark", `"XDISCRIMINATOR"`,
			"doc.Types[t/openapi/components/schemas/D].Unmodeled"},
		{"security scheme", "openapi:x-mark", `"XSECURITYSCHEME"`,
			"doc.Auth[auth/openapi/components/securitySchemes/k].Unmodeled"},
		{"oauth flows", "openapi:flows/x-mark", `"XOAUTHFLOWS"`,
			"doc.Auth[auth/openapi/components/securitySchemes/o].Unmodeled"},
		{"oauth flow", "openapi:x-mark", `"XOAUTHFLOW"`,
			"doc.Auth[auth/openapi/components/securitySchemes/o].Flows[0].Unmodeled"},
	} {
		site, found := findUnmodeled(sites, tc.key, tc.want)
		if !assert.True(t, found, "%s extension is dropped: no %s = %s anywhere in the document",
			tc.object, tc.key, tc.want) {
			continue
		}
		assert.Equal(t, ir.ReasonVendorExtension, site.entry.Reason, "%s extension reason", tc.object)
		assert.True(t, carrierNarrows(sites, tc.carrier),
			"%s carrier %q matches every Unmodeled map in the document, so it asserts nothing "+
				"about where the entry landed", tc.object, tc.carrier)
		assert.Contains(t, site.path, tc.carrier, "%s extension lands on the wrong carrier", tc.object)
	}
}

// carrierNarrows reports whether carrier picks out fewer than all of the
// document's Unmodeled maps. A carrier that matches every one of them makes the
// assertion beside it unconditionally true, which is how a row can name the
// wrong map and still pass.
//
// Matching none of them is narrow, not broad, so it passes here and fails on the
// assertion beside this one: that is a carrier naming a map the document has
// not got, and reporting it as the vacuous case would name the opposite defect.
func carrierNarrows(sites []unmodeledSite, carrier string) bool {
	all, matched := map[string]bool{}, map[string]bool{}
	for _, site := range sites {
		all[site.path] = true
		if strings.Contains(site.path, carrier) {
			matched[site.path] = true
		}
	}
	return len(matched) < len(all)
}

// assertRawPreservedBinary pins what a !!binary extension keeps: the base64 the
// source wrote. Decoding it and storing the bytes in a JSON string lost the
// spelling on every value and lost the data itself on any byte that is not
// valid UTF-8, since v1's encoding/json rewrote those to U+FFFD (GitHub #242).
//
// The block-form row carries the line breaks with it, because they are part of
// what the source wrote; base64.StdEncoding skips them, so a consumer resolving
// the tag reads the same bytes the decode used to store.
func assertRawPreservedBinary(t *testing.T, m *ir.Model) {
	t.Helper()
	for _, tc := range []struct{ key, want string }{
		{"openapi:x-blob", `"aGVsbG8="`},
		{"openapi:x-raw", `"/w=="`},
		{"openapi:x-wrapped", `"aGVs\nbG8=\n"`},
	} {
		entry, found := m.Unmodeled[tc.key]
		require.True(t, found, "%s is preserved; got %v", tc.key, m.Unmodeled)
		assert.Equal(t, tc.want, string(entry.Value),
			"%s keeps its base64 text, not the bytes it names", tc.key)
	}
}

// assertInlineAnnotations covers the positions with no ir.Property or
// ir.Parameter to carry what a declaration writes: each keeps its docs, bounds
// and x-* on a node of its own, while a position declaring nothing still
// resolves straight to the shared primitive.
func assertInlineAnnotations(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	elem := assertAnnotatedScalar(t, doc, "t/anon/components/schemas/Codes/items", 3)
	require.NotNil(t, elem.XML)
	assert.Equal(t, "Code", elem.XML.Name)
	assert.Contains(t, elem.Unmodeled, "openapi:x-facet")

	assertAnnotatedScalar(t, doc, "t/anon/components/schemas/CodeIndex/additionalProperties", 64)
	assertAnnotatedScalar(t, doc,
		"t/anon/paths/~1codes/get/responses/200/content/application~1json/schema", 8192)

	bare, ok := doc.Types[componentID("Bare")].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/string"), bare.Elem.Target,
		"an element declaring nothing must not grow a node of its own")

	op, ok := opByName(doc, "listCodes")
	require.True(t, ok)
	require.Len(t, op.Params, 1)
	assertCarriedAnnotations(t, op.Params[0].Docs, op.Params[0].Constraints, op.Params[0].Unmodeled, 4)
	require.Len(t, op.Responses[0].Headers, 1)
	h := op.Responses[0].Headers[0]
	assertCarriedAnnotations(t, h.Docs, h.Constraints, h.Unmodeled, 64)
}

// assertAnnotatedScalar requires the node at id to be a Scalar carrying a
// description and the given maxLength.
func assertAnnotatedScalar(t *testing.T, doc *ir.Document, id ir.TypeID, maxLength int64) *ir.Scalar {
	t.Helper()
	sc, ok := doc.Types[id].(*ir.Scalar)
	require.True(t, ok, "%s must own a Scalar; got %v", id, doc.Types[id])
	assert.NotEmpty(t, sc.Docs.Description)
	require.NotNil(t, sc.Constraints)
	require.NotNil(t, sc.Constraints.MaxLength)
	assert.Equal(t, maxLength, *sc.Constraints.MaxLength)
	return sc
}

// assertCarriedAnnotations requires a parameter's or header's own carrier to
// hold what its schema declared.
func assertCarriedAnnotations(t *testing.T, docs ir.Docs, c *ir.Constraints, p ir.Unmodeled, maxLength int64) {
	t.Helper()
	assert.NotEmpty(t, docs.Description)
	require.NotNil(t, c)
	require.NotNil(t, c.MaxLength)
	assert.Equal(t, maxLength, *c.MaxLength)
	assert.NotEmpty(t, p, "the schema's x-* rides on the carrier too")
}
