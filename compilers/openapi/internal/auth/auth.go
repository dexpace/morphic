// Package auth lowers what a document says about authentication: the security
// schemes it declares, and the requirements that name them.
//
// It is a package of its own because both halves of the compiler reach it and
// neither may reach the other. The document walk lowers the schemes once, before
// anything references them; the service and operation walks lower requirements
// against the result. Putting either half's lowering with its caller would make
// the other import it.
package auth

import (
	"maps"
	"strconv"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// LowerSecuritySchemes interns every declared security scheme into the auth
// registry keyed by ids.Auth(name) (ir-design §9). Run before the service walk
// so operation- and document-level requirements reference registered IDs.
func LowerSecuritySchemes(c lowering.Ctx) (map[ir.AuthID]ir.AuthScheme, []ir.Diagnostic) {
	comps := c.Doc.Components
	if comps == nil {
		return nil, nil
	}
	schemes := comps.GetSecuritySchemes()
	if schemes == nil || schemes.Len() == 0 {
		return nil, nil
	}
	out := make(map[ir.AuthID]ir.AuthScheme, schemes.Len())
	var diags []ir.Diagnostic
	for name, rs := range schemes.All() {
		ss := resolve.Object[soa.SecurityScheme](rs)
		if ss == nil {
			continue
		}
		scheme, schemeDiags := lowerSecurityScheme(c, name, ss)
		out[ids.Auth(name)] = scheme
		diags = append(diags, schemeDiags...)
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// lowerSecurityScheme lowers one named security scheme into its AuthScheme,
// dispatching the mechanism-specific fields by type.
func lowerSecurityScheme(c lowering.Ctx, name string, ss *soa.SecurityScheme) (ir.AuthScheme, []ir.Diagnostic) {
	scheme := ir.AuthScheme{
		ID:         ids.Auth(name),
		Name:       compile.NamingFor(name),
		Docs:       ir.Docs{Description: ss.GetDescription()},
		Provenance: c.ProvenanceAt(ids.Ptr("components", "securitySchemes", name)),
	}
	if ss.GetDeprecated() {
		scheme.Deprecation = &ir.Deprecation{}
	}
	fillSchemeKind(&scheme, ss)
	ext, diags := annotation.ExtensionsFrom(ss.GetExtensions(), c.SrcIndex, scheme.Provenance.Pointer)
	if len(ext) > 0 {
		scheme.Unmodeled = ext
	}
	return scheme, diags
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
	out := maps.Collect(scopes.All())
	return out
}

// LowerSecurityRequirements lowers an OR-of-ANDs security list (ir-design §9),
// under base — the pointer of the node the list is declared on ("" for the
// document root, an operation's own decl pointer otherwise — see ids.Ptr): a
// nil list inherits the enclosing default; a non-nil list yields one
// AuthRequirement per surviving option, each diagnosed if need be at its own
// base+/security/<index> pointer. An empty option object {} means "no auth is
// one acceptable choice".
//
// A requirement is a conjunction: every member must resolve for the option to
// mean anything, so an option naming even one undeclared scheme is dropped in
// full rather than surviving with just that member gone (issue #41) — the
// latter would silently rewrite "this option requires an undeclared scheme" as
// "no auth is also fine", the empty-option encoding above. When every option in
// an originally non-empty list drops this way, the list itself collapses to nil
// — "inherits the enclosing default" — rather than surfacing as [], which reads
// as the operator's own deliberate "explicitly public" (ir-design §9). A list
// the source declared empty to begin with is left untouched: that [] is real,
// not a byproduct of dropping.
func LowerSecurityRequirements(c lowering.Ctx, reqs []*soa.SecurityRequirement, base string) ([]ir.AuthRequirement, []ir.Diagnostic) {
	if reqs == nil {
		return nil, nil
	}
	out := make([]ir.AuthRequirement, 0, len(reqs))
	var diags []ir.Diagnostic
	for i, req := range reqs {
		pointer := base + ids.Ptr("security", strconv.Itoa(i))
		r, ok, reqDiags := lowerSecurityRequirement(c, req, pointer)
		diags = append(diags, reqDiags...)
		if ok {
			out = append(out, r)
		}
	}
	if len(reqs) > 0 && len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// lowerSecurityRequirement lowers one requirement option declared at pointer:
// each member is a scheme reference plus the scopes required of it within this
// option. A member naming a scheme that is not declared under
// components.securitySchemes (or one that failed to resolve into the auth
// registry) invalidates the whole option, which the caller must drop rather
// than write out short a member — never a dangling AuthID (issue #14), and
// never an unintended empty-option encoding (issue #41). ok reports whether the
// option survives; every unresolved member is still diagnosed individually, at
// the shared requirement-level pointer, so a multi-member option reports each
// of its bad names.
func lowerSecurityRequirement(c lowering.Ctx, req *soa.SecurityRequirement, pointer string,
) (r ir.AuthRequirement, ok bool, diags []ir.Diagnostic) {
	if req == nil {
		return ir.AuthRequirement{}, true, nil
	}
	var uses []ir.SchemeUse
	ok = true
	for name, scopes := range req.All() {
		id := ids.Auth(name)
		if !c.DeclaresAuth(id) {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef, pointer,
				"security requirement references undeclared scheme %q", name))
			ok = false
			continue
		}
		uses = append(uses, ir.SchemeUse{Scheme: id, Scopes: scopes})
	}
	if !ok {
		return ir.AuthRequirement{}, false, diags
	}
	return ir.AuthRequirement{Schemes: uses}, true, diags
}
