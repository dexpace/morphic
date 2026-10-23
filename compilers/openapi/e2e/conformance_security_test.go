package e2e_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

func assertServersVariables(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Servers, 1)
	assert.Equal(t, "https://{env}.example.com/v1", doc.Servers[0].URLTemplate)
	assert.Equal(t, "primary", doc.Servers[0].Name.Source,
		"a 3.2 server name is the server's naming, not part of its description")
	require.Len(t, doc.Servers[0].Variables, 1)
	v := doc.Servers[0].Variables[0]
	assert.Equal(t, "env", v.Name)
	assert.Equal(t, "api", v.Default)
	assert.Equal(t, []string{"api", "staging"}, v.Enum)
}

func assertSecuritySchemes(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	kinds := map[ir.AuthKind]bool{}
	for _, s := range doc.Auth {
		kinds[s.Kind] = true
	}
	assert.True(t, kinds[ir.AuthKindAPIKey])
	assert.True(t, kinds[ir.AuthKindHTTPBasic])
	assert.True(t, kinds[ir.AuthKindHTTPBearer])
	assert.True(t, kinds[ir.AuthKindOAuth2])
	assert.True(t, kinds[ir.AuthKindOpenIDConnect])
	assert.True(t, kinds[ir.AuthKindMutualTLS])
	assertSchemeDetail(t, doc)
}

// assertSchemeDetail pins the per-scheme detail beyond the mechanism kind: the
// annotations any scheme can carry, the RFC 7235 token an unmodelled HTTP scheme
// degrades to, and the two OAuth2 endpoints beyond the token URL.
func assertSchemeDetail(t *testing.T, doc *ir.Document) {
	t.Helper()
	byName := make(map[string]ir.AuthScheme, len(doc.Auth))
	for _, s := range doc.Auth {
		byName[s.Name.Source] = s
	}

	key, ok := byName["apiKeyAuth"]
	require.True(t, ok)
	assert.Equal(t, "Static per-tenant key.", key.Docs.Description)
	assert.NotNil(t, key.Deprecation, "a scheme can be deprecated like any other entity")
	entry, ok := key.Unmodeled["openapi:x-rotation-days"]
	require.True(t, ok, "a scheme's x-* extensions ride on the scheme; got %v", key.Unmodeled)
	assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)

	digest, ok := byName["digestAuth"]
	require.True(t, ok)
	assert.Equal(t, ir.AuthKindCustom, digest.Kind, "only basic and bearer get first-class kinds")
	assert.Equal(t, "digest", digest.Scheme, "the token itself is kept rather than dropped")

	stray, ok := byName["strayFieldAuth"]
	require.True(t, ok)
	assert.Empty(t, stray.BearerFormat, "an apiKey has no bearer-token format to fill")
	kept, ok := stray.Unmodeled["openapi:bearerFormat"]
	require.True(t, ok, "a field the type does not define is kept, not dropped; got %v", stray.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
	assert.Equal(t, ir.RawValue(`"JWT"`), kept.Value, "kept as the document wrote it")

	oauth, ok := byName["oauth2Auth"]
	require.True(t, ok)
	assert.Equal(t, "https://example.com/.well-known/oauth-authorization-server",
		oauth.OAuth2MetadataURL)
	require.Len(t, oauth.Flows, 1)
	assert.Equal(t, "https://example.com/refresh", oauth.Flows[0].RefreshURL)
}

// assertOAuth2NoFlow pins where a flowless oauth2 declaration is reported: once,
// at the declaration, however many of the document's aliases reach it, and
// never for a scheme whose metadata URL states its endpoints or that declares a
// flow. Every entry interns either way (GitHub #646).
func assertOAuth2NoFlow(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	const code = "openapi/oauth2-no-flow"
	reported := map[string]bool{"emptyFlows": true, "unknownFlowsOnly": true}
	byPointer := map[jsontext.Pointer]ir.AuthScheme{}
	for _, s := range doc.Auth {
		byPointer[s.Provenance.Pointer] = s
	}
	for _, name := range []string{
		"aliasFirst", "aliasSecond", "emptyFlows", "unknownFlowsOnly", "metadataOnly", "declaredFlow",
	} {
		s, ok := byPointer[jsontext.Pointer("/components/securitySchemes/"+name)]
		require.True(t, ok, "%s interns, flowless or not", name)
		assert.Equal(t, ir.AuthKindOAuth2, s.Kind, "%s", name)
		var want []ir.Severity
		if reported[name] {
			want = []ir.Severity{ir.SeverityWarning}
		}
		assert.Equal(t, want, diagsAt(diags, code, "/components/securitySchemes/"+name),
			"%s: reported once at a flowless declaration, never at an alias or a control", name)
	}
	n := 0
	for _, d := range diags {
		if d.Code == code {
			n++
		}
	}
	assert.Equal(t, 2, n, "two flowless declarations, two reports: %+v", diags)
	assert.Equal(t, "https://example.com/.well-known/oauth-authorization-server",
		byPointer["/components/securitySchemes/metadataOnly"].OAuth2MetadataURL, "the control's metadata URL is kept")
}

// assertOAuthFlowFields pins that a URL written on a flow whose kind does not
// define it is kept in that flow's Unmodeled, in no IR slot, and announced once
// at its own pointer, and that scopes keep the order the document writes them
// (GitHub #865, #639).
func assertOAuthFlowFields(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	byName := make(map[string]ir.AuthScheme, len(doc.Auth))
	for _, s := range doc.Auth {
		byName[s.Name.Source] = s
	}
	const e = "https://example.com/"
	cases := []struct {
		kind, keyword, field string
		// authorize and token are what the flow's own fields lower to; stray is
		// the undefined field's value as the document wrote it.
		authorize, token, stray string
	}{
		{"authorization_code", "authorizationCode", "deviceAuthorizationUrl", e + "authorize", e + "token", e + "device-undefined"},
		{"client_credentials", "clientCredentials", "authorizationUrl", "", e + "token", e + "authorize-undefined"},
		{"implicit", "implicit", "tokenUrl", e + "authorize", "", e + "token-undefined"},
		{"device", "deviceAuthorization", "authorizationUrl", e + "device", e + "token", e + "authorize-undefined"},
	}
	flows := map[string]ir.OAuthFlow{}
	for _, fl := range byName["flowFields"].Flows {
		flows[fl.Kind] = fl
	}
	require.Len(t, flows, len(cases), "every declared flow lowers")
	for _, tc := range cases {
		fl := flows[tc.kind]
		pointer := "/components/securitySchemes/flowFields/flows/" + tc.keyword + "/" + tc.field
		assert.Equal(t, tc.authorize, fl.AuthorizationURL, "%s: only a defined field fills this slot", tc.kind)
		assert.Equal(t, tc.token, fl.TokenURL, "%s: only a defined field fills this slot", tc.kind)
		require.Len(t, fl.Unmodeled, 1, "%s keeps its one undefined field: %v", tc.kind, fl.Unmodeled)
		kept, ok := fl.Unmodeled["openapi:"+tc.field]
		require.True(t, ok, "%s keeps %s; got %v", tc.kind, tc.field, fl.Unmodeled)
		assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
		assert.Equal(t, ir.RawValue(`"`+tc.stray+`"`), kept.Value)
		assert.Equal(t, jsontext.Pointer(pointer), kept.Provenance.Pointer)
		assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, diag.DegradedConstruct, pointer),
			"%s: announced once at the field", tc.kind)
	}
	var announced int
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct {
			announced++
		}
	}
	assert.Equal(t, len(cases), announced, "one announcement per kept field: %+v", diags)

	scoped := byName["scopeOrder"].Flows
	require.Len(t, scoped, 1)
	assert.Empty(t, cmp.Diff([]ir.OAuthScope{
		{Name: "write", Description: "Modify data"},
		{Name: "read", Description: "Read data"},
		{Name: "admin", Description: "Full access"},
	}, scoped[0].Scopes), "scopes keep source order")
}

func assertSecurityOrAnd(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	auth := doc.Services[0].Auth
	require.Len(t, auth, 3, "OR of three options in source order")
	assert.Len(t, auth[0].Schemes, 1)
	assert.Len(t, auth[1].Schemes, 2, "the ANDed option requires two schemes together")
	assert.Empty(t, auth[2].Schemes, "the empty option means no auth is acceptable")
	op, ok := opByName(doc, "publicOp")
	require.True(t, ok)
	assert.Empty(t, op.Auth, "security: [] makes the operation explicitly public")
}
