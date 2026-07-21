package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

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
	doc, _, _ := lowerServiceSpec(t, authSpec)
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
	assert.NotEmpty(t, oauth.Extensions, "oauth x-* extension")
	kinds := map[string]ir.OAuthFlow{}
	for _, f := range oauth.Flows {
		kinds[f.Kind] = f
	}
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

func TestLowerSecurityRequirement_Nil(t *testing.T) {
	t.Parallel()
	got := (&lowerer{out: &ir.Document{}}).lowerSecurityRequirement(nil)
	assert.Empty(t, got.Schemes)
}
