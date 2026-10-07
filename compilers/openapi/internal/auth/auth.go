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
	"encoding/json/jsontext"
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
// registry keyed by ids.Auth(name) (ir-design §9). Run it before the service
// walk so requirements reference registered IDs.
//
// An entry whose $ref resolves to nothing is reported at its own components
// pointer and interned nowhere; see unresolvableSchemeDiags.
//
// An entry that resolves to an object but names no mechanism is refused and
// reported the same way; see mechanismRefusalDiag. Every other diagnostic from
// here concerns a scheme that did intern.
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
		entry := ids.Ptr("components", "securitySchemes", name)
		ss, decl := resolve.ObjectAt[soa.SecurityScheme](c.RefScope(), rs, entry)
		if ss == nil {
			diags = append(diags, unresolvableSchemeDiags(c, name, rs, entry)...)
			continue
		}
		scheme, ok, schemeDiags := lowerSecurityScheme(c, name, ss, entry, decl)
		diags = append(diags, schemeDiags...)
		if !ok {
			continue
		}
		// A securitySchemes entry written as a Reference Object keeps the summary
		// and description written beside its $ref, which describe this entry rather
		// than the declaration it names (GitHub #610).
		scheme.Docs = resolve.RefDocs(rs, scheme.Docs)
		out[ids.Auth(name)] = scheme
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// unresolvableSchemeDiags reports a securitySchemes entry that lowered to no
// scheme, but only when it was written as a $ref that resolves to nothing. The
// compiler keeps the load phase's report at that pointer instead, which has the
// resolver's reason (GitHub #385); this one stands where the load phase never
// got that far, as when a resolver panic ends its walk.
//
// An entry not written as an object already draws the loader's type-mismatch,
// which names it and its fault. rs is the entry as written; a nil rs is
// unreachable from a parsed document, so that guard is for a hand-built node.
func unresolvableSchemeDiags(c lowering.Ctx, name string, rs *soa.ReferencedSecurityScheme,
	entry jsontext.Pointer,
) []ir.Diagnostic {
	if rs == nil {
		return nil
	}
	ref := rs.GetReference().String()
	if ref == "" {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.UnresolvedRef, entry,
		"security scheme %q has a $ref that resolves to nothing: %q", name, ref)}
}

// lowerSecurityScheme lowers one named security scheme into its AuthScheme. ok
// reports whether the entry named a mechanism; if not, the caller interns
// nothing.
//
// For an entry written as a $ref the pointers differ (issue #107). entry is
// where the document names the scheme and places anything said of the entry as
// a whole; decl is where the fields live. The ID and provenance stay on entry,
// since aliases of one declaration are separate schemes and irverify holds an
// AuthID to agree with its provenance path. A `$ref` entry holds only the
// reference, so `<entry>/in` names a position no document holds.
func lowerSecurityScheme(c lowering.Ctx, name string, ss *soa.SecurityScheme,
	entry, decl jsontext.Pointer,
) (scheme ir.AuthScheme, ok bool, diags []ir.Diagnostic) {
	scheme = ir.AuthScheme{
		ID:         ids.Auth(name),
		Name:       compile.NamingFor(name),
		Docs:       ir.Docs{Description: ss.GetDescription()},
		Provenance: c.ProvenanceAt(entry),
	}
	if ss.GetDeprecated() {
		scheme.Deprecation = &ir.Deprecation{}
	}
	missing, named := fillSchemeKind(&scheme, ss)
	if !named {
		return ir.AuthScheme{}, false, []ir.Diagnostic{mechanismRefusalDiag(c, name, missing, entry)}
	}
	diags = preserveUnreadFields(c, &scheme, ss, decl)
	diags = append(diags, applySchemeAnnotations(c, &scheme, ss, decl)...)
	// Distinct from preserveUnreadFields above it: that keeps the fields OpenAPI
	// defines for a securityScheme which this entry's own mechanism gives no
	// meaning to, while this keeps the keys OpenAPI defines for no securityScheme
	// at all.
	diags = append(diags, annotation.UnknownKeysIn(&scheme.Unmodeled, ss, c.ProvenanceAt, decl)...)
	// After the extensions, whose entries are what a promotion reads.
	return scheme, true, append(diags,
		c.PromoteDeprecation(scheme.Unmodeled, scheme.Deprecation, &scheme.Provenance)...)
}

// applySchemeAnnotations keeps what the securitySchemes entry and, for an oauth2
// scheme, the flows object and each flow inside it declare that reaches no IR
// field: their x-*, and the keys OpenAPI defines for none of them. ir.OAuthFlow
// carries an Unmodeled map of its own; the flows object does not lower to a node
// at all, so its entries are kept on the scheme under the keyword.
//
// Only oauth2 reads inside `flows`: on any other type the whole node is kept
// verbatim by preserveUnreadFields, extensions and all, and no ir.OAuthFlow was
// lowered for a flow's own to land on.
func applySchemeAnnotations(c lowering.Ctx, scheme *ir.AuthScheme, ss *soa.SecurityScheme, decl jsontext.Pointer) []ir.Diagnostic {
	ext, diags := annotation.ExtensionsFrom(ss.GetExtensions(), c.ProvenanceAt, decl)
	scheme.Unmodeled = annotation.MergeUnmodeled(scheme.Unmodeled, ext)
	if scheme.Kind != ir.AuthKindOAuth2 {
		return diags
	}
	flows := ss.GetFlows()
	flowsPtr := decl + ids.Ptr("flows")
	flowsExt, flowsDiags := annotation.ExtensionsUnder(flows.GetExtensions(), c.ProvenanceAt, flowsPtr, "flows")
	scheme.Unmodeled = annotation.MergeUnmodeled(scheme.Unmodeled, flowsExt)
	diags = append(diags, flowsDiags...)
	diags = append(diags, annotation.UnknownKeysUnder(&scheme.Unmodeled, flows, c.ProvenanceAt, flowsPtr, "flows")...)
	return append(diags, applyFlowAnnotations(c, scheme.Flows, flows, flowsPtr)...)
}

// applyFlowAnnotations writes each declared flow's own x-* and undeclared keys
// onto the ir.OAuthFlow it lowered to. Both lists are one ordered reading of the
// same object — scheme.Flows came from oauthFlows, which walks presentFlows — so
// the i-th lowered flow is the i-th declared one and the two cannot differ in
// length.
func applyFlowAnnotations(c lowering.Ctx, lowered []ir.OAuthFlow, flows *soa.OAuthFlows, flowsPtr jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for i, f := range presentFlows(flows) {
		fptr := flowsPtr + ids.Ptr(f.keyword)
		ext, extDiags := annotation.ExtensionsFrom(f.src.GetExtensions(), c.ProvenanceAt, fptr)
		lowered[i].Unmodeled = annotation.MergeUnmodeled(lowered[i].Unmodeled, ext)
		diags = append(diags, extDiags...)
		diags = append(diags, annotation.UnknownKeysIn(&lowered[i].Unmodeled, f.src, c.ProvenanceAt, fptr)...)
	}
	return diags
}

// mechanismRefusalDiag reports a securitySchemes entry that omits missing, the
// field that would name its mechanism.
//
// It is refused rather than interned because ir.AuthKind has no value for "the
// document did not say"; AuthKindCustom means a mechanism the IR does not
// model. An interned scheme would be one no emitter can implement, asserting
// that the API is authenticated by nothing in particular (#294).
//
// A requirement naming it drops whole (#41; see lowerSecurityRequirement). The
// entry's other fields are not kept: it is a document defect, as an
// unresolvable $ref is, and there is no scheme to hold an Unmodeled map.
func mechanismRefusalDiag(c lowering.Ctx, name, missing string, entry jsontext.Pointer) ir.Diagnostic {
	// "has no", not "declares no": a $ref entry declares only the reference, yet
	// alias and target are each reported at their own entry, so the wording must
	// hold for both.
	return c.DiagAt(ir.SeverityError, diag.IncompleteSecurityScheme, entry,
		"security scheme %q has no %s, so it names no authentication mechanism: "+
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
// `type: http` without a scheme is the second shape that names no mechanism: a
// custom kind carrying an empty token says no more than a typeless entry. A
// token the RFC would reject still interns; checking it against the grammar is
// validation this compiler does not do, and trimming it would guess.
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

// preserveUnreadFields keeps every mechanism field the entry declared that its
// own type gives no meaning to verbatim under Unmodeled (#294).
//
// Each mechanism's lowering reads only its own fields, so the rest would be
// lost silently. ir.AuthScheme has a field of each name, but filling one would
// give the mechanism a property it does not define, so the declaration is kept
// beside the scheme under ReasonDegradedLowering.
//
// A field the entry did not write records nothing; an explicit `in: ""` is
// recorded as written.
//
// decl is where the fields are written: the referenced declaration for a $ref
// entry (#107).
func preserveUnreadFields(c lowering.Ctx, scheme *ir.AuthScheme, ss *soa.SecurityScheme,
	decl jsontext.Pointer,
) []ir.Diagnostic {
	defined := fieldsDefinedBy(ss.GetType())
	var diags []ir.Diagnostic
	for _, field := range mechanismFieldNames() {
		if slices.Contains(defined, field) {
			continue
		}
		at := decl + ids.Ptr(field)
		kept, keptDiags := annotation.PreserveNodeInto(&scheme.Unmodeled, "openapi:"+field,
			annotation.RawChildNode(ss.GetRootNode(), field), ir.ReasonDegradedLowering, c.ProvenanceAt(at))
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

// sourceFlow is one declared OAuth2 flow: the IR kind it lowers to, the keyword
// it is written under, and the object itself.
type sourceFlow struct {
	kind    string
	keyword string
	src     *soa.OAuthFlow
}

// presentFlows returns the flows an OAuthFlows object declares, in a fixed,
// deterministic order. It is the single ordered reading of that object: the
// lowering and the extension reader both walk it, which is what lets the i-th
// ir.OAuthFlow be matched back to the i-th declaration without a second, and
// possibly divergent, enumeration.
func presentFlows(flows *soa.OAuthFlows) []sourceFlow {
	candidates := []sourceFlow{
		{"authorization_code", "authorizationCode", flows.GetAuthorizationCode()},
		{"client_credentials", "clientCredentials", flows.GetClientCredentials()},
		{"implicit", "implicit", flows.GetImplicit()},
		{"password", "password", flows.GetPassword()},
		{"device", "deviceAuthorization", flows.GetDeviceAuthorization()},
	}
	var out []sourceFlow
	for _, cand := range candidates {
		if cand.src != nil {
			out = append(out, cand)
		}
	}
	return out
}

// oauthFlows lowers each present OAuth2 flow in a fixed, deterministic order.
// The device flow's deviceAuthorizationUrl rides OAuthFlow.AuthorizationURL
// (ir-design §9).
func oauthFlows(flows *soa.OAuthFlows) []ir.OAuthFlow {
	var out []ir.OAuthFlow
	for _, f := range presentFlows(flows) {
		if f.kind == "device" {
			out = append(out, deviceFlow(f.src))
			continue
		}
		out = append(out, oauthFlow(f.kind, f.src))
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
// declared under base: "" at the root, else an operation's own declaration,
// never its mount. A nil list inherits the enclosing default; an empty one
// stays []. A non-nil list yields one AuthRequirement per surviving option,
// diagnosed at base+/security/<index>; an empty option {} means "no auth is one
// acceptable choice".
//
// If every option of a non-empty list drops, it collapses to nil, with an
// error: the IR cannot say "auth required, scheme undeclared", #14 forbids an
// unbacked AuthID, and nil never turns a demanded requirement into explicitly
// public []. Dropped names stay out of Unmodeled as document defects.
func LowerSecurityRequirements(c lowering.Ctx, reqs []*soa.SecurityRequirement, base jsontext.Pointer) ([]ir.AuthRequirement, []ir.Diagnostic) {
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
		// Every option dropped: nil, not [], which would read as explicitly
		// public. The IR cannot say "auth is required but its scheme is
		// undeclared" and #14 forbids minting an AuthID nothing backs, so the
		// carrier inherits the enclosing default, or is unauthenticated where
		// there is none; both misstate the source, but nil is the one spelling
		// that never reduces a demanded requirement to public. Each collapse
		// carries an error diagnostic.
		return nil, diags
	}
	return out, diags
}

// lowerSecurityRequirement lowers one requirement option declared at pointer:
// each member is a scheme reference plus its scopes. ok reports whether the
// option survives.
//
// A requirement is a conjunction, so a member naming a scheme the auth registry
// does not hold invalidates the whole option, which is dropped in full: never a
// dangling AuthID (issue #14), never the empty option, which would read "no
// auth is also fine" (issue #41). An entry that is no object drops for the
// second reason alone (issue #284), with no report of its own: the loader's
// type-mismatch already names it. Every unresolved member is still diagnosed.
func lowerSecurityRequirement(c lowering.Ctx, req *soa.SecurityRequirement, pointer jsontext.Pointer,
) (r ir.AuthRequirement, ok bool, diags []ir.Diagnostic) {
	if !writtenAsObject(req) {
		return ir.AuthRequirement{}, false, nil
	}
	var uses []ir.SchemeUse
	ok = true
	for name, scopes := range req.All() {
		id := ids.Auth(name)
		if !c.DeclaresAuth(id) {
			// "Unresolved", not "undeclared": the name may be declared as a $ref
			// resolving to nothing, or as an entry naming no mechanism (#294).
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

// writtenAsObject reports whether the document wrote req as an object, the only
// shape a requirement has.
//
// Null, a scalar and a sequence all unmarshal to a requirement with no members,
// like the {} meaning "no auth is one acceptable choice" (issue #284). The node
// it was read from separates them: the marshaller records a root node only for
// a value it could read as a mapping, so {} carries one inline or through an
// alias. Being library behavior, it is tested through compiled documents.
//
// A nil req is unreachable from a parsed document; that guard is for a
// hand-built slice, as in unresolvableSchemeDiags.
func writtenAsObject(req *soa.SecurityRequirement) bool {
	return req != nil && req.GetRootNode() != nil
}
