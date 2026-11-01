// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth

import (
	"encoding/json/jsontext"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

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
func oauthFlows(flows *soa.OAuthFlows) []ir.OAuthFlow {
	present := presentFlows(flows)
	if len(present) == 0 {
		return nil
	}
	out := make([]ir.OAuthFlow, 0, len(present))
	for _, f := range present {
		out = append(out, oauthFlow(f))
	}
	return out
}

// oauthFlow lowers one OAuth2 flow, reading only the fields of flowFields its
// kind defines; preserveUndefinedFlowFields keeps the rest. The device flow's
// deviceAuthorizationUrl rides OAuthFlow.AuthorizationURL (docs/ir/auth.md).
func oauthFlow(f sourceFlow) ir.OAuthFlow {
	fl := ir.OAuthFlow{
		Kind:       f.kind,
		RefreshURL: f.src.GetRefreshURL(),
		Scopes:     scopes(f.src),
	}
	for _, field := range flowFields() {
		if slices.Contains(flowFieldsDefinedBy(f.keyword), field.name) {
			*field.slot(&fl) = field.read(f.src)
		}
	}
	return fl
}

// flowField is one field of the OAuth Flow Object that only some flows define:
// its name, how the model reads it, and the field of ir.OAuthFlow it lowers to
// where it is defined.
type flowField struct {
	name string
	read func(*soa.OAuthFlow) string
	slot func(*ir.OAuthFlow) *string
}

// flowFields returns the OAuth Flow Object fields only some flows define,
// sorted by name. refreshUrl and scopes are absent because every flow defines
// them; TestFlowFields_AccountForEverySourceField holds the split to the source
// model.
func flowFields() []flowField {
	authorization := func(fl *ir.OAuthFlow) *string { return &fl.AuthorizationURL }
	return []flowField{
		{name: "authorizationUrl", read: (*soa.OAuthFlow).GetAuthorizationURL, slot: authorization},
		{name: "deviceAuthorizationUrl", read: (*soa.OAuthFlow).GetDeviceAuthorizationURL, slot: authorization},
		{name: "tokenUrl", read: (*soa.OAuthFlow).GetTokenURL, slot: func(fl *ir.OAuthFlow) *string { return &fl.TokenURL }},
	}
}

// flowFieldsDefinedBy returns the fields of flowFields that OpenAPI gives the
// flow written under keyword a meaning for (3.2.0 §4.29; 3.0 and 3.1 alike,
// without the device flow): the authorization endpoint for the flows that
// redirect a user agent, the token endpoint for every flow that requests a
// token from one, and the device authorization endpoint for the device flow.
func flowFieldsDefinedBy(keyword string) []string {
	switch keyword {
	case "implicit":
		return []string{"authorizationUrl"}
	case "authorizationCode":
		return []string{"authorizationUrl", "tokenUrl"}
	case "deviceAuthorization":
		return []string{"deviceAuthorizationUrl", "tokenUrl"}
	default: // password, clientCredentials
		return []string{"tokenUrl"}
	}
}

// scopes lowers a flow's scopes in the order the document writes them, or nil
// when it declares none.
func scopes(f *soa.OAuthFlow) []ir.OAuthScope {
	declared := f.GetScopes()
	if declared == nil || declared.Len() == 0 {
		return nil
	}
	out := make([]ir.OAuthScope, 0, declared.Len())
	for name, description := range declared.All() {
		out = append(out, ir.OAuthScope{Name: name, Description: description})
	}
	return out
}

// tokenless reports whether scheme is an oauth2 scheme that states no way to
// obtain a token: it lowered no flow, and names no metadata URL to discover its
// authorization server from (RFC 8414).
func tokenless(scheme ir.AuthScheme) bool {
	return scheme.Kind == ir.AuthKindOAuth2 && len(scheme.Flows) == 0 && scheme.OAuth2MetadataURL == ""
}

// flowsMissing reports whether ss writes no flows object: flows absent, null,
// or a value that is no mapping, which the parser models as no object. The rule
// table reports each, as a REQUIRED field missing or a value of the wrong type,
// wherever the scheme is written (GitHub #872), so the lowering stands aside. A
// mapping lowering no flow, one whose only flow is null included, is the
// lowering's to report.
func flowsMissing(ss *soa.SecurityScheme) bool {
	return ss.GetFlows() == nil
}

// oauthNoFlowDiag reports a flows object declaring no flow; the design record
// is on diag.OAuth2NoFlow.
//
// decl places it, not entry: a flowless declaration is a fact about the text at
// decl, and a $ref entry holds no flows either way. The message names no entry,
// so the copies every alias of one declaration draws dedup to one report, placed
// in whichever document holds the declaration (GitHub #874).
func oauthNoFlowDiag(c lowering.Ctx, decl jsontext.Pointer) ir.Diagnostic {
	return c.DiagAt(ir.SeverityWarning, diag.OAuth2NoFlow, decl,
		"oauth2 security scheme declares no flow and no oauth2MetadataUrl, "+
			"so it states no way to obtain a token")
}

// applyFlowAnnotations writes each declared flow's own x-*, undeclared keys and
// the fields its kind does not define onto the ir.OAuthFlow it lowered to. Both
// lists are one ordered reading of the same object — scheme.Flows came from
// oauthFlows, which walks presentFlows — so the i-th lowered flow is the i-th
// declared one and the two cannot differ in length.
func applyFlowAnnotations(c lowering.Ctx, lowered []ir.OAuthFlow, flows *soa.OAuthFlows, flowsPtr jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for i, f := range presentFlows(flows) {
		fptr := flowsPtr + ids.Ptr(f.keyword)
		ext, extDiags := annotation.ExtensionsFrom(f.src.GetExtensions(), c.ProvenanceAt, fptr)
		lowered[i].Unmodeled = annotation.MergeUnmodeled(lowered[i].Unmodeled, ext)
		diags = append(diags, extDiags...)
		diags = append(diags, preserveUndefinedFlowFields(c, &lowered[i], f, fptr)...)
		diags = append(diags, annotation.UnknownKeysIn(&lowered[i].Unmodeled, f.src, c.ProvenanceAt, fptr)...)
	}
	return diags
}

// preserveUndefinedFlowFields keeps each field of flowFields the flow wrote but
// its kind does not define verbatim under the flow's Unmodeled (GitHub #865),
// as preserveUnreadFields does for a scheme. oauthFlow lowered none of them, so
// without this they would be lost: the parser models every field on every flow,
// and the unknown-key census never sees one.
//
// It reads the flow's mapping as the parser does, as every preservation site
// does, so a field merged in through a `<<` key is kept as a written one is
// (GitHub #384).
func preserveUndefinedFlowFields(c lowering.Ctx, fl *ir.OAuthFlow, f sourceFlow, fptr jsontext.Pointer) []ir.Diagnostic {
	defined := flowFieldsDefinedBy(f.keyword)
	var diags []ir.Diagnostic
	for _, field := range flowFields() {
		if slices.Contains(defined, field.name) {
			continue
		}
		at := fptr + ids.Ptr(field.name)
		kept, keptDiags := annotation.PreserveNodeInto(&fl.Unmodeled, "openapi:"+field.name,
			nodeview.Child(f.src.GetRootNode(), field.name), ir.ReasonDegradedLowering, c.ProvenanceAt(at))
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"oauth flow %s does not define %s; kept verbatim under Unmodeled", f.keyword, field.name))
	}
	return diags
}
