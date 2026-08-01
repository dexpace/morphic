package auth_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

func TestAuth_SchemeKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		scheme string
		check  func(t *testing.T, s ir.AuthScheme)
	}{
		{
			name:   "apiKey",
			scheme: "{type: apiKey, in: header, name: X-Key}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindAPIKey, s.Kind)
				assert.Equal(t, "header", s.In)
				assert.Equal(t, "X-Key", s.KeyName)
			},
		},
		{
			// RFC 7235 §2.1 makes the scheme token case-insensitive, and a
			// document is free to spell it as the RFC's own prose does. Getting
			// this wrong degrades a first-class kind to AuthKindCustom, which an
			// SDK emitter renders as an opaque header rather than as basic auth.
			name:   "http basic, capitalised as the RFC spells it",
			scheme: "{type: http, scheme: Basic}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindHTTPBasic, s.Kind)
				assert.Empty(t, s.Scheme, "a recognised token is not also carried as a custom one")
			},
		},
		{
			name:   "http bearer, upper case",
			scheme: "{type: http, scheme: BEARER, bearerFormat: JWT}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindHTTPBearer, s.Kind)
				assert.Equal(t, "JWT", s.BearerFormat)
			},
		},
		{
			name:   "http basic",
			scheme: "{type: http, scheme: basic}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindHTTPBasic, s.Kind)
			},
		},
		{
			name:   "http bearer",
			scheme: "{type: http, scheme: bearer, bearerFormat: JWT}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindHTTPBearer, s.Kind)
				assert.Equal(t, "JWT", s.BearerFormat)
			},
		},
		{
			name:   "oauth2 implicit",
			scheme: `{type: oauth2, flows: {implicit: {authorizationUrl: "https://a.example/auth", scopes: {read: r}}}}`,
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindOAuth2, s.Kind)
				require.Len(t, s.Flows, 1)
				assert.Equal(t, "implicit", s.Flows[0].Kind)
				assert.Equal(t, "https://a.example/auth", s.Flows[0].AuthorizationURL)
				assert.Equal(t, "r", s.Flows[0].Scopes["read"])
			},
		},
		{
			name:   "openIdConnect",
			scheme: `{type: openIdConnect, openIdConnectUrl: "https://a.example/.well-known"}`,
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindOpenIDConnect, s.Kind)
				assert.Equal(t, "https://a.example/.well-known", s.OpenIDConnectURL)
			},
		},
		{
			name:   "mutualTLS",
			scheme: "{type: mutualTLS}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindMutualTLS, s.Kind)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: 3.1.0\n" +
				"info: {title: T, version: \"1\"}\n" +
				"paths: {}\n" +
				"components:\n" +
				"  securitySchemes:\n" +
				"    s: " + tc.scheme + "\n"
			doc, _, diags := serviceSpec(t, spec)
			requireNoErrorDiags(t, diags)
			s, ok := doc.Auth[ids.Auth("s")]
			require.True(t, ok)
			tc.check(t, s)
		})
	}
}

func TestAuth_RequirementsOrOfAnds(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - {}
  - key: []
  - oauth: [read, write]
    key: []
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
    oauth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://a.example/token
          scopes: {read: r, write: w}
`
	doc, svc, diags := serviceSpec(t, spec)
	requireNoErrorDiags(t, diags)

	keyID := ids.Auth("key")
	scheme, ok := doc.Auth[keyID]
	require.True(t, ok)
	assert.Equal(t, ir.AuthKindAPIKey, scheme.Kind)
	assert.Equal(t, "header", scheme.In)
	assert.Equal(t, "X-Key", scheme.KeyName)

	oauth := doc.Auth[ids.Auth("oauth")]
	require.Len(t, oauth.Flows, 1)
	assert.Equal(t, "client_credentials", oauth.Flows[0].Kind)
	assert.Equal(t, "https://a.example/token", oauth.Flows[0].TokenURL)

	require.Len(t, svc.Auth, 3) // OR across options, source order
	assert.Empty(t, svc.Auth[0].Schemes, "empty requirement = no-auth is one acceptable choice")
	require.Len(t, svc.Auth[1].Schemes, 1)
	assert.Equal(t, keyID, svc.Auth[1].Schemes[0].Scheme)
	require.Len(t, svc.Auth[2].Schemes, 2, "one option, two schemes ANDed")
	assert.Equal(t, []string{"read", "write"}, svc.Auth[2].Schemes[0].Scopes)
}

// TestAuth_UnserializableExtensionStillWarns pins that a security scheme whose
// sole x-* extension fails to serialize still surfaces the warning:
// lowerSecurityScheme returns the diagnostic unconditionally, so a caller must
// not gate it behind the same "if len(ext) > 0" that guards the assignment —
// that guard is exactly false in the all-unserializable case, which used to
// drop the warning silently.
func TestAuth_UnserializableExtensionStillWarns(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    s: {type: apiKey, in: header, name: X-Key, x-bad: {1: intkey}}
`
	doc, _, diags := serviceSpec(t, spec)
	assert.True(t, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning) > 0,
		"an entirely unserializable extension still warns even though the scheme's own Unmodeled ends up empty")
	scheme, ok := doc.Auth[ids.Auth("s")]
	require.True(t, ok)
	assert.Empty(t, scheme.Unmodeled, "the unserializable extension is dropped, not stored")
}

const authSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get: {operationId: x, responses: {"200": {description: ok}}}
components:
  securitySchemes:
    apiKey: {type: apiKey, in: header, name: X-Key}
    basicAuth: {type: http, scheme: basic, deprecated: true}
    bearerAuth: {type: http, scheme: bearer, bearerFormat: JWT}
    customHttp: {type: http, scheme: negotiate}
    oauth:
      type: oauth2
      x-note: n
      flows:
        authorizationCode:
          authorizationUrl: 'https://a'
          tokenUrl: 'https://t'
          refreshUrl: 'https://r'
          scopes: {read: read access}
        clientCredentials: {tokenUrl: 'https://t'}
        implicit: {authorizationUrl: 'https://a', scopes: {}}
        password: {tokenUrl: 'https://t'}
        deviceAuthorization: {deviceAuthorizationUrl: 'https://d', tokenUrl: 'https://t'}
    oidc: {type: openIdConnect, openIdConnectUrl: 'https://oidc'}
    mtls: {type: mutualTLS}
`

func TestAuth_AllSchemeKinds(t *testing.T) {
	t.Parallel()
	doc, _, _ := serviceSpec(t, authSpec)
	byKind := map[ir.AuthKind]ir.AuthScheme{}
	for _, s := range doc.Auth {
		byKind[s.Kind] = s
	}
	assert.Equal(t, "header", byKind[ir.AuthKindAPIKey].In)
	require.NotNil(t, byKind[ir.AuthKindHTTPBasic].Deprecation, "deprecated basic scheme")
	assert.Equal(t, "JWT", byKind[ir.AuthKindHTTPBearer].BearerFormat)
	assert.Equal(t, "openIdConnect", "openIdConnect")
	assert.Equal(t, "https://oidc", byKind[ir.AuthKindOpenIDConnect].OpenIDConnectURL)
	_, hasMTLS := byKind[ir.AuthKindMutualTLS]
	assert.True(t, hasMTLS)

	oauth := byKind[ir.AuthKindOAuth2]
	assert.NotEmpty(t, oauth.Unmodeled, "oauth x-* extension")
	kinds := indexBy(oauth.Flows, func(f ir.OAuthFlow) string { return f.Kind })
	assert.Len(t, oauth.Flows, 5)
	assert.Equal(t, "https://r", kinds["authorization_code"].RefreshURL)
	assert.NotEmpty(t, kinds["authorization_code"].Scopes)
	assert.Nil(t, kinds["implicit"].Scopes, "empty scope map is nil")
	assert.Equal(t, "https://d", kinds["device"].AuthorizationURL, "device auth url rides AuthorizationURL")

	// The negotiate HTTP scheme is custom with the token preserved.
	var sawCustomHTTP bool
	for _, s := range doc.Auth {
		if s.Kind == ir.AuthKindCustom && s.Scheme == "negotiate" {
			sawCustomHTTP = true
		}
	}
	assert.True(t, sawCustomHTTP, "unknown http scheme is custom")
}

func TestAuth_OAuthNoFlowsUnknownTypeAndGhostRef(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /x:
    get: {operationId: x, responses: {"200": {description: ok}}}
components:
  securitySchemes:
    oauthNoFlows: {type: oauth2}
    weird: {type: bananas}
    ghost: {$ref: '#/components/securitySchemes/Missing'}
`)
	doc, _, _ := serviceSpec(t, spec)
	var oauth, custom ir.AuthScheme
	for _, s := range doc.Auth {
		if s.Kind == ir.AuthKindOAuth2 {
			oauth = s
		}
		if s.Kind == ir.AuthKindCustom {
			custom = s
		}
	}
	assert.Equal(t, ir.AuthKindOAuth2, oauth.Kind)
	assert.Nil(t, oauth.Flows, "oauth2 with no flows lowers to nil flows")
	assert.Equal(t, "bananas", custom.Scheme, "unknown scheme type degrades to custom")
}

// TestLowerSecuritySchemes_NothingLoweredIsNilNotEmpty pins the guard that
// keeps an empty map out of the document. A components.securitySchemes block
// whose every entry fails to resolve declares no scheme the IR can carry, and
// an empty Auth map would be a field the source never wrote.
func TestLowerSecuritySchemes_NothingLoweredIsNilNotEmpty(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Components: &soa.Components{
		SecuritySchemes: sequencedmap.New(
			sequencedmap.NewElem("ghost", (*soa.ReferencedSecurityScheme)(nil))),
	}}

	got, diags := auth.LowerSecuritySchemes(lowering.Ctx{Doc: doc})

	assert.Nil(t, got, "an unresolvable entry leaves no map behind")
	assert.Empty(t, diags)
}

// TestLowerSecuritySchemes_NoComponentsAtAll pins the two earlier exits: a
// document with no components, and one whose components declare no schemes.
func TestLowerSecuritySchemes_NoComponentsAtAll(t *testing.T) {
	t.Parallel()
	got, diags := auth.LowerSecuritySchemes(lowering.Ctx{Doc: &soa.OpenAPI{}})
	assert.Nil(t, got)
	assert.Empty(t, diags)

	got, diags = auth.LowerSecuritySchemes(lowering.Ctx{Doc: &soa.OpenAPI{Components: &soa.Components{}}})
	assert.Nil(t, got)
	assert.Empty(t, diags)
}

// serviceSpec compiles src and returns the document, its single service, and
// every diagnostic. It drives the public entry point rather than this package
// directly: the requirements lowered here are reached through the service walk,
// which lives in the compiler above, and only an external test package may
// import it.
func serviceSpec(t *testing.T, src string) (*ir.Document, ir.Service, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Len(t, doc.Services, 1)
	return doc, doc.Services[0], diags
}

// requireNoErrorDiags fails the test if any diagnostic has error severity.
func requireNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
}

// countDiagsAt counts the diagnostics matching code and sev exactly.
func countDiagsAt(diags []ir.Diagnostic, code string, sev ir.Severity) int {
	var n int
	for _, d := range diags {
		if d.Code == code && d.Severity == sev {
			n++
		}
	}
	return n
}

// indexBy builds a lookup keyed by key(item).
func indexBy[T any, K comparable](items []T, key func(T) K) map[K]T {
	out := make(map[K]T, len(items))
	for _, item := range items {
		out[key(item)] = item
	}
	return out
}

// pathsSpec wraps a paths block in a minimal 3.1 document with no components.
func pathsSpec(paths string) string {
	return "openapi: 3.1.0\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths:\n" + paths
}

// TestSecurityRequirement_UndeclaredSchemeIsDroppedNotDangling pins the refusal
// issue #14 exists for. A requirement may name any string; only a name the
// document declares has an AuthID behind it, and writing one for a name it does
// not declare would put a reference into the IR that resolves to nothing. The
// requirement survives without that scheme, and the drop is reported.
func TestSecurityRequirement_UndeclaredSchemeIsDroppedNotDangling(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - ghost: []
    key: []
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
	require.Len(t, svc.Auth, 1, "the requirement itself survives")
	named := make([]ir.AuthID, 0, len(svc.Auth[0].Schemes))
	for _, use := range svc.Auth[0].Schemes {
		named = append(named, use.Scheme)
	}
	assert.Equal(t, []ir.AuthID{ids.Auth("key")}, named,
		"only the declared scheme is referenced; the other is dropped rather than dangling")
	assert.Equal(t, 1, countDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError),
		"and the drop is reported exactly once: %+v", diags)
}

// TestSecurityRequirements_AnEmptyListIsNotAnAbsentOne pins the difference
// between a document that says nothing about security and one that says
// explicitly that none is required.
//
// ir.Service.Auth and ir.Operation.Auth carry no omitempty, so the IR spells the
// two apart — null for "inherit whatever encloses this", [] for "no auth is an
// acceptable option" (ir-design §9). Collapsing them makes an explicitly public
// operation read as one that never mentioned security, which is the opposite
// claim.
func TestSecurityRequirements_AnEmptyListIsNotAnAbsentOne(t *testing.T) {
	t.Parallel()
	var c lowering.Ctx

	absent, absentDiags := auth.LowerSecurityRequirements(c, nil)
	assert.Nil(t, absent, "no security key at all inherits the enclosing default")
	assert.Empty(t, absentDiags)

	empty, emptyDiags := auth.LowerSecurityRequirements(c, []*soa.SecurityRequirement{})
	assert.NotNil(t, empty, "an empty list is a declaration, not an absence")
	assert.Empty(t, empty, "and it declares no options")
	assert.Empty(t, emptyDiags)
}
