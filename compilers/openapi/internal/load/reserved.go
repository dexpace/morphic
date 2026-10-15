package load

import (
	"encoding/json/jsontext"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// reservedFinding reports a Parameter Object's allowReserved written where
// OpenAPI minor, the document's, gives it no effect (reservedIgnored), at the
// keyword. Presence is reported, not truth, as at in: querystring: a declared
// false has no effect either. model is one versionCheck walks, written at
// site; a $ref is judged where its target is written. The parameter is read
// through the model, as the lowering reads it, so the value the warning says
// is lowered as declared is the one the binding holds.
func reservedFinding(at func(jsontext.Pointer) ir.Provenance, site jsontext.Pointer, model any, minor string,
) (ir.Diagnostic, bool) {
	rp, ok := model.(*soa.ReferencedParameter)
	if !ok || rp.IsReference() {
		return ir.Diagnostic{}, false
	}
	p := rp.GetObject()
	if p == nil || p.AllowReserved == nil {
		return ir.Diagnostic{}, false // an entry that is not a mapping holds no object
	}
	place, ignored := reservedIgnored(minor, p.GetIn(), p.GetStyle())
	if !ignored {
		return ir.Diagnostic{}, false
	}
	return diag.Newf(ir.SeverityWarning, diag.InapplicableLocationKeyword, at(site.AppendToken("allowReserved")),
		"parameter field allowReserved has no effect for %s in OpenAPI %s, which applies it %s; lowered as declared",
		place, minor, reservedScope(minor)), true
}

// reservedIgnored reports whether OpenAPI minor gives allowReserved no effect
// at in, serialized in style, and names that place for the message. Before 3.2
// it applies to in: query alone. 3.2 applies it wherever the value is
// percent-encoded, which leaves out a header, and a cookie in style cookie, the
// one place a style decides.
//
// A querystring is outside this rule: 3.2 forbids the keyword there, which the
// lowering reports as diag.InvalidLocationKeyword (GitHub #885), so one keyword
// draws one warning. An in or style OpenAPI does not define is the parser's to
// refuse.
func reservedIgnored(minor string, in soa.ParameterIn, style soa.SerializationStyle) (string, bool) {
	before32 := before(minor, "3.2")
	switch {
	case in == soa.ParameterInHeader, before32 && (in == soa.ParameterInPath || in == soa.ParameterInCookie):
		return "in=" + string(in), true
	case in == soa.ParameterInCookie && style == serializationStyleCookie:
		return "in=cookie with style=cookie", true
	default:
		return "", false
	}
}

// serializationStyleCookie is the cookie style 3.2 added, which writes a value
// without percent-encoding it. The parser names no constant for it (GitHub
// #389).
const serializationStyleCookie soa.SerializationStyle = "cookie"

// reservedScope says where OpenAPI minor applies allowReserved.
func reservedScope(minor string) string {
	if before(minor, "3.2") {
		return "to in=query alone"
	}
	return "only where the value is percent-encoded"
}
