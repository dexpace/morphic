// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// TestSecurityRequirement_A32URILandingIsLoweredOnce pins a scheme two keys
// reach, lowered once, a key whose chain passes an alias to nothing, reported
// with why, and one landing where no scheme could be built, which the load
// phase reports and the key mints a scheme of no mechanism for, saying nothing.
func TestSecurityRequirement_A32URILandingIsLoweredOnce(t *testing.T) {
	t.Parallel()
	doc, svc, diags := serviceSpec(t, `openapi: 3.2.0
info: {title: T, version: "1"}
security: [{"#/x-schemes/extra": []}, {"#/x-schemes/extra": []}, {"#/x-schemes/alias": []}, {"#/x-schemes/broken": []}]
paths: {}
x-schemes:
  extra: {type: http, scheme: bearer}
  alias: {$ref: '#/x-schemes/none'}
  broken: {$ref}
`)
	require.Len(t, svc.Auth, 4)
	extra := ids.In("").Auth("/x-schemes/extra")
	assert.Equal(t, []ir.AuthID{extra}, schemeUses(svc.Auth[0]))
	assert.Equal(t, []ir.AuthID{extra}, schemeUses(svc.Auth[1]))
	assert.Equal(t, ir.AuthKindHTTPBearer, doc.Auth[extra].Kind)
	assert.Equal(t, []ir.AuthID{ids.In("").UnresolvedAuth("#/x-schemes/alias")}, schemeUses(svc.Auth[2]))
	assert.Equal(t, []ir.AuthID{ids.In("").UnresolvedAuth("/x-schemes/broken")}, schemeUses(svc.Auth[3]))
	assert.Equal(t, []string{`security requirement references unresolved scheme "#/x-schemes/alias": the ` +
		`requirement is kept, and the scheme has no mechanism; the chain of references it starts ends on no ` +
		`Security Scheme Object`}, messagesAt(diags, diag.UnresolvedSecurityScheme))
}

// TestSecurityRequirement_A32URILandingThatNamesNoMechanismIsUnresolved pins a
// key landing on a Security Scheme Object that names no mechanism: its scheme
// is minted where that kind is, at its position in its document, and the
// object, not the key, is reported.
func TestSecurityRequirement_A32URILandingThatNamesNoMechanismIsUnresolved(t *testing.T) {
	t.Parallel()
	doc, svc, diags := serviceSpec(t, `openapi: 3.2.0
info: {title: T, version: "1"}
security: [{"#/x-schemes/bare": []}]
paths: {}
x-schemes:
  bare: {type: http}
`)
	require.Len(t, svc.Auth, 1)
	minted := ids.In("").UnresolvedAuth("/x-schemes/bare")
	assert.Equal(t, []ir.AuthID{minted}, schemeUses(svc.Auth[0]))
	assert.Equal(t, ir.AuthKindUnresolved, doc.Auth[minted].Kind)
	assert.NotContains(t, doc.Auth, ids.In("").Auth("/x-schemes/bare"), "no scheme of a mechanism's ID")
	assert.Empty(t, messagesAt(diags, diag.UnresolvedSecurityScheme))
}

// TestSecurityRequirement_A32URILandingOnAWholeDocumentIsNamedForIt pins a key
// naming another document whose root is a Security Scheme Object: no key names
// it there, so it is named for its document.
func TestSecurityRequirement_A32URILandingOnAWholeDocumentIsNamedForIt(t *testing.T) {
	t.Parallel()
	doc, _ := compileBeside(t, `openapi: 3.2.0
info: {title: T, version: "1"}
security: [{"other.yaml": []}]
paths: {}
`, "type: http\nscheme: bearer\n")
	scheme := doc.Auth[ids.In("other.yaml").Auth("")]
	assert.Equal(t, ir.AuthKindHTTPBearer, scheme.Kind)
	assert.Equal(t, ir.NamingFor("other.yaml"), scheme.Name)
}
