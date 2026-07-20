package openapi

import (
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/ir"
)

// lowerSecuritySchemes interns every declared security scheme into the auth
// registry keyed by authIDFor(name) (ir-design §9). Run before the service walk
// so operation- and document-level requirements reference registered IDs.
func (l *lowerer) lowerSecuritySchemes() {
	comps := l.doc.Components
	if comps == nil {
		return
	}
	schemes := comps.GetSecuritySchemes()
	if schemes == nil || schemes.Len() == 0 {
		return
	}
	out := make(map[ir.AuthID]ir.AuthScheme, schemes.Len())
	for name, rs := range schemes.All() {
		ss := resolveSecurityScheme(rs)
		if ss == nil {
			continue
		}
		out[authIDFor(name)] = l.lowerSecurityScheme(name, ss)
	}
	if len(out) > 0 {
		l.out.Auth = out
	}
}

// lowerSecurityScheme lowers one named security scheme into its AuthScheme,
// dispatching the mechanism-specific fields by type.
func (l *lowerer) lowerSecurityScheme(name string, ss *soa.SecurityScheme) ir.AuthScheme {
	scheme := ir.AuthScheme{
		ID:         authIDFor(name),
		Name:       ir.Naming{Source: name, Canonical: canonicalWords(name)},
		Docs:       ir.Docs{Description: ss.GetDescription()},
		Provenance: ir.Provenance{Source: l.srcIndex, Pointer: ptr("components", "securitySchemes", name)},
	}
	if ss.GetDeprecated() {
		scheme.Deprecation = &ir.Deprecation{}
	}
	fillSchemeKind(&scheme, ss)
	if ext, diags := extensionsFrom(ss.GetExtensions()); len(ext) > 0 {
		scheme.Extensions = ext
		l.diags = append(l.diags, diags...)
	}
	return scheme
}

// fillSchemeKind sets the mechanism kind and its per-kind fields (ir-design §9).
// Unknown or unmodeled types degrade to a custom scheme carrying the raw type.
func fillSchemeKind(scheme *ir.AuthScheme, ss *soa.SecurityScheme) {
	switch ss.GetType() {
	case soa.SecuritySchemeTypeAPIKey:
		scheme.Kind = ir.AuthKindAPIKey
		scheme.In = string(ss.GetIn())
		scheme.KeyName = ss.GetName()
	case soa.SecuritySchemeTypeHTTP:
		fillHTTPScheme(scheme, ss)
	case soa.SecuritySchemeTypeOAuth2:
		scheme.Kind = ir.AuthKindOAuth2
		scheme.Flows = oauthFlows(ss.GetFlows())
		scheme.OAuth2MetadataURL = ss.GetOAuth2MetadataUrl()
	case soa.SecuritySchemeTypeOpenIDConnect:
		scheme.Kind = ir.AuthKindOpenIDConnect
		scheme.OpenIDConnectURL = ss.GetOpenIdConnectUrl()
	case soa.SecuritySchemeTypeMutualTLS:
		scheme.Kind = ir.AuthKindMutualTLS
	default:
		scheme.Kind = ir.AuthKindCustom
		scheme.Scheme = string(ss.GetType())
	}
}

// fillHTTPScheme classifies an HTTP scheme by its RFC 7235 scheme token: basic
// and bearer get first-class kinds; any other scheme is custom with the token
// preserved. BearerFormat rides along regardless (ir-design §9).
func fillHTTPScheme(scheme *ir.AuthScheme, ss *soa.SecurityScheme) {
	scheme.BearerFormat = ss.GetBearerFormat()
	switch strings.ToLower(ss.GetScheme()) {
	case "basic":
		scheme.Kind = ir.AuthKindHTTPBasic
	case "bearer":
		scheme.Kind = ir.AuthKindHTTPBearer
	default:
		scheme.Kind = ir.AuthKindCustom
		scheme.Scheme = ss.GetScheme()
	}
}

// oauthFlows lowers each present OAuth2 flow in a fixed, deterministic order.
// The device flow's deviceAuthorizationUrl rides OAuthFlow.AuthorizationURL
// (ir-design §9).
func oauthFlows(flows *soa.OAuthFlows) []ir.OAuthFlow {
	if flows == nil {
		return nil
	}
	var out []ir.OAuthFlow
	if f := flows.GetAuthorizationCode(); f != nil {
		out = append(out, oauthFlow("authorization_code", f))
	}
	if f := flows.GetClientCredentials(); f != nil {
		out = append(out, oauthFlow("client_credentials", f))
	}
	if f := flows.GetImplicit(); f != nil {
		out = append(out, oauthFlow("implicit", f))
	}
	if f := flows.GetPassword(); f != nil {
		out = append(out, oauthFlow("password", f))
	}
	if f := flows.GetDeviceAuthorization(); f != nil {
		out = append(out, deviceFlow(f))
	}
	return out
}

// oauthFlow lowers one OAuth2 flow of the given kind.
func oauthFlow(kind string, f *soa.OAuthFlow) ir.OAuthFlow {
	return ir.OAuthFlow{
		Kind:             kind,
		AuthorizationURL: f.GetAuthorizationURL(),
		TokenURL:         f.GetTokenURL(),
		RefreshURL:       f.GetRefreshURL(),
		Scopes:           scopeMap(f),
	}
}

// deviceFlow lowers the RFC 8628 device flow, carrying its
// deviceAuthorizationUrl on AuthorizationURL.
func deviceFlow(f *soa.OAuthFlow) ir.OAuthFlow {
	fl := oauthFlow("device", f)
	if u := f.GetDeviceAuthorizationURL(); u != "" {
		fl.AuthorizationURL = u
	}
	return fl
}

// scopeMap lowers a flow's scope map, or nil when it declares none.
func scopeMap(f *soa.OAuthFlow) map[string]string {
	scopes := f.GetScopes()
	if scopes == nil || scopes.Len() == 0 {
		return nil
	}
	out := make(map[string]string, scopes.Len())
	for name, desc := range scopes.All() {
		out[name] = desc
	}
	return out
}

// lowerSecurityRequirements lowers an OR-of-ANDs security list (ir-design §9): a
// nil list inherits the enclosing default; a non-nil list yields one
// AuthRequirement per option in source order. An empty option object {} means
// "no auth is one acceptable choice".
func (l *lowerer) lowerSecurityRequirements(reqs []*soa.SecurityRequirement) []ir.AuthRequirement {
	if reqs == nil {
		return nil
	}
	out := make([]ir.AuthRequirement, 0, len(reqs))
	for _, req := range reqs {
		out = append(out, lowerSecurityRequirement(req))
	}
	return out
}

// lowerSecurityRequirement lowers one requirement option: each member is a
// scheme reference plus the scopes required of it within this option.
func lowerSecurityRequirement(req *soa.SecurityRequirement) ir.AuthRequirement {
	if req == nil {
		return ir.AuthRequirement{}
	}
	var uses []ir.SchemeUse
	for name, scopes := range req.All() {
		uses = append(uses, ir.SchemeUse{Scheme: authIDFor(name), Scopes: scopes})
	}
	return ir.AuthRequirement{Schemes: uses}
}

// resolveSecurityScheme returns the concrete SecurityScheme of a
// reference-or-inline entry, preferring the inline object.
func resolveSecurityScheme(rs *soa.ReferencedSecurityScheme) *soa.SecurityScheme {
	if rs == nil {
		return nil
	}
	if obj := rs.GetObject(); obj != nil {
		return obj
	}
	return rs.GetResolvedObject()
}
