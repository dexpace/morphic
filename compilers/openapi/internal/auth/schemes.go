// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// LowerSecuritySchemes lowers every securitySchemes entry into the Registry
// (docs/ir/auth.md), keyed by ids.Auth(name). Run it before the service walk so
// requirements reference registered IDs.
//
// An entry that yields no mechanism lowers all the same, keyed by
// ids.UnresolvedAuth(name), so nothing can reach it as a declared scheme: one
// whose $ref resolves to nothing, reported at its own components pointer (see
// unresolvableScheme), and one naming no mechanism, reported the same way (see
// noMechanismDiag).
func LowerSecuritySchemes(c lowering.Ctx) (*Registry, []ir.Diagnostic) {
	reg := &Registry{byID: map[ir.AuthID]ir.AuthScheme{}, root: c.ProvenanceAt("")}
	var diags []ir.Diagnostic
	for name, rs := range c.Doc.Components.GetSecuritySchemes().All() {
		scheme, schemeDiags := lowerEntry(c, name, rs)
		diags = append(diags, schemeDiags...)
		// A summary or description beside a $ref of the entry's chain documents
		// this entry rather than the scheme it names (GitHub #610).
		scheme.Docs = resolve.UseDocs[soa.SecurityScheme](rs, scheme.Docs)
		reg.byID[scheme.ID] = scheme
	}
	return reg, diags
}

// lowerEntry lowers the securitySchemes entry called name, as written in rs.
func lowerEntry(c lowering.Ctx, name string, rs *soa.ReferencedSecurityScheme) (ir.AuthScheme, []ir.Diagnostic) {
	entry := ids.Ptr("components", "securitySchemes", name)
	ss, dc, decl := lowering.Declared[soa.SecurityScheme](c, rs, entry)
	if ss == nil {
		return unresolvableScheme(c, name, rs, entry)
	}
	return lowerSecurityScheme(c, dc, name, ss, entry, decl)
}

// unresolvableScheme lowers a securitySchemes entry that resolved to no object:
// a scheme of no mechanism at the entry, keeping the $ref that reached nothing,
// the one thing the source says of where its mechanism is. The $ref's own error
// is the one report of it, so keeping it announces nothing. rs is the entry as
// written; a nil rs is unreachable from a parsed document, so that guard is for a
// hand-built node.
func unresolvableScheme(c lowering.Ctx, name string, rs *soa.ReferencedSecurityScheme, entry jsontext.Pointer,
) (ir.AuthScheme, []ir.Diagnostic) {
	scheme := unresolvedScheme(name, c.ProvenanceAt(entry))
	if rs == nil {
		return scheme, nil
	}
	_, keptDiags := annotation.PreserveNodeInto(&scheme.Unmodeled, "openapi:$ref", rs.GetCore().Reference.ValueNode,
		ir.ReasonDegradedLowering, c.ProvenanceAt(entry+ids.Ptr("$ref")))
	return scheme, append(unresolvableSchemeDiags(c, name, rs, entry), keptDiags...)
}

// unresolvableSchemeDiags reports a securitySchemes entry that resolved to no
// object, but only when it was written as a $ref naming something. The compiler
// keeps the load phase's report at that pointer instead, which has the resolver's
// reason (GitHub #385); this one stands where the load phase met no reference, as
// when a resolver panic ends its walk or the model did not read the $ref, which
// is why the $ref is read from its node (docs/parser-library.md#undecoded-scalar).
//
// An entry or a $ref of any other shape is the load phase's to report.
func unresolvableSchemeDiags(c lowering.Ctx, name string, rs *soa.ReferencedSecurityScheme,
	entry jsontext.Pointer,
) []ir.Diagnostic {
	ref := nodeview.Deref(rs.GetCore().Reference.ValueNode)
	if ref == nil || ref.Value == "" {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.UnresolvedRef, entry,
		"security scheme %q has a $ref that resolves to nothing: %q", name, ref.Value)}
}

// lowerSecurityScheme lowers one named security scheme into its AuthScheme, of
// kind unresolved where the entry names no mechanism.
//
// For an entry written as a $ref the pointers differ (issue #107). entry is
// where the document names the scheme and places anything said of the entry as
// a whole; decl is where the fields live. The ID and provenance stay on entry,
// since aliases of one declaration are separate schemes and irverify holds an
// AuthID to agree with its provenance path. A `$ref` entry holds only the
// reference, so `<entry>/in` names a position no document holds.
func lowerSecurityScheme(c, dc lowering.Ctx, name string, ss *soa.SecurityScheme,
	entry, decl jsontext.Pointer,
) (ir.AuthScheme, []ir.Diagnostic) {
	scheme := ir.AuthScheme{
		ID:         ids.Auth(name),
		Name:       compile.NamingFor(name),
		Docs:       ir.Docs{Description: ss.GetDescription()},
		Provenance: c.ProvenanceAt(entry),
	}
	if ss.GetDeprecated() {
		scheme.Deprecation = &ir.Deprecation{}
	}
	var diags []ir.Diagnostic
	if missing, named := fillSchemeKind(&scheme, ss); !named {
		scheme.ID, scheme.Kind = ids.UnresolvedAuth(name), ir.AuthKindUnresolved
		diags = append(diags, noMechanismDiag(c, name, missing, entry))
	}
	if tokenless(scheme) && !flowsMissing(ss) {
		// Reported rather than refused; see oauthNoFlowDiag.
		diags = append(diags, oauthNoFlowDiag(dc, decl))
	}
	diags = append(diags, preserveUnreadFields(dc, &scheme, ss, decl)...)
	diags = append(diags, applySchemeAnnotations(dc, &scheme, ss, decl)...)
	// Distinct from preserveUnreadFields above it: that keeps the fields OpenAPI
	// defines for a securityScheme which this entry's own mechanism gives no
	// meaning to, while this keeps the keys OpenAPI defines for no securityScheme
	// at all.
	diags = append(diags, annotation.UnknownKeysIn(&scheme.Unmodeled, ss, dc.ProvenanceAt, decl)...)
	// After the extensions, whose entries are what a promotion reads. A statement
	// of its own, since it writes the provenance of the scheme returned.
	diags = append(diags, c.PromoteDeprecation(scheme.Unmodeled, scheme.Deprecation, &scheme.Provenance)...)
	return scheme, diags
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

// noMechanismDiag reports a securitySchemes entry that omits missing, the field
// that would name its mechanism.
//
// Custom is no home for it: AuthKindCustom means a mechanism the IR does not
// model, and an entry naming none would assert that the API is authenticated by
// nothing in particular, which no emitter can implement (#294). So it takes the
// kind that says the compiler holds no mechanism, with what it wrote kept beside
// that, and a requirement naming it stands. An error: what the entry means is
// missing, and OpenAPI requires both fields.
func noMechanismDiag(c lowering.Ctx, name, missing string, entry jsontext.Pointer) ir.Diagnostic {
	// "has no", not "declares no": a $ref entry declares only the reference, yet
	// alias and target are each reported at their own entry, so the wording must
	// hold for both.
	return c.DiagAt(ir.SeverityError, diag.IncompleteSecurityScheme, entry,
		"security scheme %q has no %s, so it names no authentication mechanism: "+
			"it is kept without one, and every requirement naming it is kept", name, missing)
}

// fillSchemeKind sets the mechanism kind and its per-kind fields
// (docs/ir/auth.md). An unrecognized type degrades to a custom scheme carrying
// the raw type, which a later OpenAPI version's own type reaches as readily as
// a typo does.
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
// preserved. BearerFormat rides along regardless (docs/ir/auth.md).
//
// `type: http` without a scheme is the second shape that names no mechanism: a
// custom kind carrying an empty token says no more than a typeless entry. A
// token the RFC would reject still interns: the load phase reports it against
// the grammar, and trimming it would guess.
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

// preserveUnreadFields keeps verbatim under Unmodeled every mechanism field the
// entry declared that its own type gives no meaning to (#294). An entry naming no
// mechanism keeps all of them, with its type, which no kind can say.
//
// ir.AuthScheme has a field of each name, but filling one would give the
// mechanism a property it does not define, so the field is kept beside the
// scheme under ReasonDegradedLowering. One the entry did not write records
// nothing; an explicit `in: ""` is recorded as written.
//
// decl is where the fields are written, the declaration for a $ref entry (#107).
func preserveUnreadFields(c lowering.Ctx, scheme *ir.AuthScheme, ss *soa.SecurityScheme,
	decl jsontext.Pointer,
) []ir.Diagnostic {
	unresolved := scheme.Kind == ir.AuthKindUnresolved
	defined, fields := fieldsDefinedBy(ss.GetType()), mechanismFieldNames()
	if unresolved {
		defined, fields = nil, append(fields, "type")
	}
	var diags []ir.Diagnostic
	for _, field := range fields {
		if slices.Contains(defined, field) {
			continue
		}
		at := decl + ids.Ptr(field)
		kept, keptDiags := annotation.PreserveNodeInto(&scheme.Unmodeled, "openapi:"+field,
			nodeview.Child(ss.GetRootNode(), field), ir.ReasonDegradedLowering, c.ProvenanceAt(at))
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		if unresolved {
			diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
				"security scheme %s belongs to no mechanism the entry names; kept verbatim under Unmodeled", field))
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"security scheme %s is not defined by type %q; kept verbatim under Unmodeled",
			field, ss.GetType()))
	}
	return diags
}
