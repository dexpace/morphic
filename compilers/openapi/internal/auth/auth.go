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
	"slices"
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
//
// An entry whose $ref resolves to nothing is reported at its own components
// pointer and interned nowhere. It is reported here rather than left to the
// requirements that name it, because nothing has to name it: the load phase's
// report of the same failure carries no pointer at all (issue #235), so an
// unreferenced entry would otherwise be a scheme the document declares, the IR
// silently drops, and no diagnostic sites.
//
// An entry that resolves to an object but names no mechanism is refused for the
// same reason and reported the same way — see mechanismRefusalDiag. Those two
// are the only entries reported as interning nothing; every other diagnostic
// from here is about a scheme that did intern (see preserveUnreadFields).
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
			diags = append(diags, unresolvableSchemeDiags(c, name, rs)...)
			continue
		}
		scheme, ok, schemeDiags := lowerSecurityScheme(c, name, ss)
		diags = append(diags, schemeDiags...)
		if !ok {
			continue
		}
		out[ids.Auth(name)] = scheme
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// unresolvableSchemeDiags reports a securitySchemes entry that lowered to no
// scheme — but only the one shape that nothing else places.
//
// Two kinds of entry reach the caller's nil: one written as something other
// than an object (null, a scalar, a sequence), and one whose $ref resolves to
// nothing — a missing internal target, or an external one this compile refuses.
// Only the second is unplaced. The first already draws the loader's
// type-mismatch, which names both the entry and what was wrong with it, so a
// second report here would send the reader to the same position to learn less.
//
// rs is the entry as the document wrote it, which is what separates the two:
// the reference is empty for everything that is not one. Its own nil is not
// reachable from a parsed document — a malformed entry still arrives as an
// object — so that guard is for a hand-built node, matching resolve.Object's.
func unresolvableSchemeDiags(c lowering.Ctx, name string, rs *soa.ReferencedSecurityScheme) []ir.Diagnostic {
	if rs == nil {
		return nil
	}
	ref := rs.GetReference().String()
	if ref == "" {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
		ids.Ptr("components", "securitySchemes", name),
		"security scheme %q has a $ref that resolves to nothing: %q", name, ref)}
}

// lowerSecurityScheme lowers one named security scheme into its AuthScheme,
// dispatching the mechanism-specific fields by type. ok reports whether the
// entry named a mechanism at all; when it did not, the caller interns nothing.
func lowerSecurityScheme(c lowering.Ctx, name string, ss *soa.SecurityScheme,
) (scheme ir.AuthScheme, ok bool, diags []ir.Diagnostic) {
	pointer := ids.Ptr("components", "securitySchemes", name)
	scheme = ir.AuthScheme{
		ID:         ids.Auth(name),
		Name:       compile.NamingFor(name),
		Docs:       ir.Docs{Description: ss.GetDescription()},
		Provenance: c.ProvenanceAt(pointer),
	}
	if ss.GetDeprecated() {
		scheme.Deprecation = &ir.Deprecation{}
	}
	missing, named := fillSchemeKind(&scheme, ss)
	if !named {
		return ir.AuthScheme{}, false, []ir.Diagnostic{mechanismRefusalDiag(c, name, missing, pointer)}
	}
	diags = preserveUnreadFields(c, &scheme, ss, pointer)
	ext, extDiags := annotation.ExtensionsFrom(ss.GetExtensions(), c.SrcIndex, pointer)
	scheme.Unmodeled = annotation.MergeUnmodeled(scheme.Unmodeled, ext)
	return scheme, true, append(diags, extDiags...)
}

// mechanismRefusalDiag reports a securitySchemes entry that declares a scheme
// without saying what it is, having omitted the field named by missing.
//
// It is refused rather than interned because ir.AuthKind has no value for "the
// document did not say": every kind names a mechanism, and AuthKindCustom names
// one the IR does not model rather than one the entry never gave. Interning it
// would put a scheme no emitter can implement in Document.Auth, recognisable
// only by an empty Scheme — an in-band error for every consumer to rediscover —
// and assert that the API is authenticated by nothing in particular (#294).
//
// Refusing reuses the remedy #41 gave a requirement naming an undeclared
// scheme, and does so deliberately rather than by inheritance: the two faults
// differ — that entry was never declared, this one was declared and left
// unsaid — but the IR has no more room for the second than for the first, and
// the cost is one already documented and diagnosed at every step. A requirement
// naming this entry drops whole, and a list whose every option drops collapses
// to nil (see LowerSecurityRequirements).
//
// What is not kept, deliberately: the fields the entry did declare. An entry
// that names no mechanism is a defect in the document rather than a construct
// the IR declines to model, which is the call an unresolvable $ref already gets
// here — and there is no scheme left to hang an Unmodeled map on. The entry
// that *does* intern keeps everything it wrote; see preserveUnreadFields.
func mechanismRefusalDiag(c lowering.Ctx, name, missing, pointer string) ir.Diagnostic {
	return c.DiagAt(ir.SeverityError, diag.IncompleteSecurityScheme, pointer,
		"security scheme %q declares no %s, so it names no authentication mechanism: "+
			"no scheme is interned for it, and every requirement naming it is dropped", name, missing)
}

// fillSchemeKind sets the mechanism kind and its per-kind fields (ir-design §9).
// An unrecognized type degrades to a custom scheme carrying the raw type, which
// a later OpenAPI version's own type reaches as readily as a typo does.
//
// ok reports whether the entry named a mechanism; missing is the field it had
// to declare to name one and did not. An absent type names nothing at all, and
// no degradation is available for it: the custom kind carries the token a type
// was spelled with, and there is no token.
func fillSchemeKind(scheme *ir.AuthScheme, ss *soa.SecurityScheme) (missing string, ok bool) {
	switch ss.GetType() {
	case "":
		return "type", false
	case soa.SecuritySchemeTypeAPIKey:
		scheme.Kind = ir.AuthKindAPIKey
		scheme.In = string(ss.GetIn())
		scheme.KeyName = ss.GetName()
	case soa.SecuritySchemeTypeHTTP:
		return fillHTTPScheme(scheme, ss)
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
	return "", true
}

// fillHTTPScheme classifies an HTTP scheme by its RFC 7235 scheme token: basic
// and bearer get first-class kinds; any other scheme is custom with the token
// preserved. BearerFormat rides along regardless (ir-design §9).
//
// `type: http` alone is the second shape that names no mechanism: the token is
// what an HTTP scheme *is*, and a custom kind carrying an empty one says no
// more than the typeless entry does. A token the RFC would reject is still a
// token and still interns — comparing it against the grammar is validation this
// compiler does not do, and trimming it would be a guess at what was meant.
func fillHTTPScheme(scheme *ir.AuthScheme, ss *soa.SecurityScheme) (missing string, ok bool) {
	token := ss.GetScheme()
	if token == "" {
		return "scheme", false
	}
	scheme.BearerFormat = ss.GetBearerFormat()
	switch strings.ToLower(token) {
	case "basic":
		scheme.Kind = ir.AuthKindHTTPBasic
	case "bearer":
		scheme.Kind = ir.AuthKindHTTPBearer
	default:
		scheme.Kind = ir.AuthKindCustom
		scheme.Scheme = token
	}
	return "", true
}

// mechanismFieldNames are the securityScheme fields only some types define,
// sorted — which is both the order preserveUnreadFields walks them in and what
// makes the list comparable to the source model it must keep pace with.
//
// type, description, deprecated and the x-* extensions are absent because every
// type defines them, so no type can leave one unread. Nothing here derives that
// split; TestMechanismFieldNames_AccountForEverySourceField holds it to the
// upstream struct, so a field that model gains fails a test rather than
// vanishing from the IR.
func mechanismFieldNames() []string {
	return []string{
		"bearerFormat", "flows", "in", "name",
		"oauth2MetadataUrl", "openIdConnectUrl", "scheme",
	}
}

// fieldsDefinedBy returns the mechanism fields OpenAPI gives t a meaning for,
// which are exactly the ones fillSchemeKind reads for it.
//
// mutualTLS is the whole mechanism and defines none of them. Nor does an
// unrecognized type: the custom kind it degrades to already spends its one
// Scheme field on the type token, so a `scheme` written beside it has no home
// left even though the same field would have held it under `type: http`.
func fieldsDefinedBy(t soa.SecuritySchemaType) []string {
	switch t {
	case soa.SecuritySchemeTypeAPIKey:
		return []string{"in", "name"}
	case soa.SecuritySchemeTypeHTTP:
		return []string{"scheme", "bearerFormat"}
	case soa.SecuritySchemeTypeOAuth2:
		return []string{"flows", "oauth2MetadataUrl"}
	case soa.SecuritySchemeTypeOpenIDConnect:
		return []string{"openIdConnectUrl"}
	default:
		return nil
	}
}

// preserveUnreadFields keeps every mechanism field the entry declared that
// its own type gives no meaning to — `in` on an oauth2 scheme, `flows` on an
// apiKey — verbatim under Unmodeled rather than dropping it (#294).
//
// Each mechanism's lowering reads only its own fields, so before this the rest
// reached no IR field and no Unmodeled entry either: declared source text gone
// with no diagnostic, which invariant 2 forbids. ir.AuthScheme is flat and does
// hold a field of each name, but filling one would say the mechanism has a
// property it does not define — an apiKey location on a scheme that is not an
// apiKey — so the declaration is kept beside the scheme instead of inside it.
// ReasonDegradedLowering for that reason: the entry is lowered to the weaker
// shape its type can hold, with what did not fit recoverable beside it.
//
// Presence, not truth: a field the entry did not write records nothing, and one
// it wrote records whatever it wrote. RawChildNode reads the entry as the
// document spelled it, so an explicit `in: ""` is a declaration like any other.
func preserveUnreadFields(c lowering.Ctx, scheme *ir.AuthScheme, ss *soa.SecurityScheme,
	pointer string,
) []ir.Diagnostic {
	defined := fieldsDefinedBy(ss.GetType())
	var diags []ir.Diagnostic
	for _, field := range mechanismFieldNames() {
		if slices.Contains(defined, field) {
			continue
		}
		at := pointer + ids.Ptr(field)
		kept, keptDiags := annotation.PreserveNodeInto(&scheme.Unmodeled, "openapi:"+field,
			annotation.RawChildNode(ss.GetRootNode(), field), ir.ReasonDegradedLowering, at, c.SrcIndex)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"security scheme %s is not defined by type %q; kept verbatim under Unmodeled",
			field, ss.GetType()))
	}
	return diags
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

// LowerSecurityRequirements lowers an OR-of-ANDs security list (ir-design §9)
// declared under base — the pointer of the node carrying the list, which is ""
// at the document root and an operation's own declaration pointer, never its
// mount, otherwise: a nil list inherits the enclosing default; a non-nil list
// yields one AuthRequirement per surviving option, each diagnosed if need be at
// its own base+/security/<index> pointer. An empty option object {} means "no
// auth is one acceptable choice".
//
// An entry the source wrote as something other than an object reaches here as
// an empty option rather than as a nil one, so it lowers to that same encoding
// and the collapse below never sees it. Telling the two apart needs the parse
// the loader already rejected, so it is issue #284's to fix and deliberately
// out of scope here — which is also why lowerSecurityRequirement's nil guard is
// not that site, however much it looks like it.
//
// A requirement is a conjunction: every member must resolve for the option to
// mean anything, so an option naming even one undeclared scheme is dropped in
// full rather than surviving with just that member gone (issue #41) — the
// latter would silently rewrite "this option requires an undeclared scheme" as
// "no auth is also fine", the empty-option encoding above. When every option in
// an originally non-empty list drops this way, the list itself collapses to nil
// — "inherits the enclosing default" — rather than surfacing as [], which
// ir-design §9 reserves for a deliberate "explicitly public" declaration. A
// list the source declared empty to begin with is left untouched: that [] is
// real, not a byproduct of dropping.
//
// What that collapse costs, deliberately: a carrier whose every option drops
// becomes indistinguishable from one that never declared security, so an
// operation reads as requiring whatever the service default requires — a scheme
// it never named — or as unauthenticated where there is no default. Both
// misstate the source, because the IR has no encoding for "auth is required but
// its scheme is undeclared" and issue #14 forbids minting an AuthID nothing
// backs. nil is chosen because it is the only spelling that never reduces a
// demanded requirement to explicitly public, and every collapse carries an
// error diagnostic. The dropped text is not kept under Unmodeled: a name that
// resolves to nothing is a defect in the document rather than a construct the
// IR declines to model, which is the call an unresolvable $ref in a schema
// position and an unresolvable discriminator mapping already get here.
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
// option. A member naming a scheme the auth registry does not hold invalidates
// the whole option, which the caller must drop in full rather than just that
// member — never a dangling AuthID (issue #14), and never an unintended
// empty-option encoding (issue #41). ok reports whether the option survives;
// every unresolved member is still diagnosed individually, at the shared
// requirement-level pointer, so a multi-member option reports each of its bad
// names.
//
// Three documents reach that state: one that never declared the name, one that
// declared it as a $ref resolving to nothing, and one whose entry named no
// mechanism (#294). What is said here is only that the name resolves to no
// scheme, which is true of all three — calling it undeclared would contradict
// the entry-level report LowerSecuritySchemes leaves beside it in the latter
// two cases.
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
				"security requirement references unresolved scheme %q", name))
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
