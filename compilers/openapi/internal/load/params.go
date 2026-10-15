package load

import (
	"encoding/json/jsontext"
	"iter"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// parameterRepeats reports each entry of a parameter list written in the source
// that repeats the name and location of an earlier entry in the same list,
// which OpenAPI forbids. The compiler owns the rule (compilerOwned): the
// library read only entries written inline, so it missed a repeat through a
// $ref.
//
// items is walkSource's walk, which visits each list once where it is written,
// lowered or not, and never through a $ref. A list beside a path item's $ref is
// sibs's model's, checked where that model is walked from. A panic in the walk
// is reported at the model it reached.
func parameterRepeats(at func(jsontext.Pointer) ir.Provenance, sibs Siblings, items iter.Seq[soa.WalkItem],
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	site, err := eachModel(items, "parameter check", func(site jsontext.Pointer, model any) error {
		switch m := model.(type) {
		case *soa.ReferencedPathItem:
			if !m.IsReference() {
				diags = append(diags, listRepeats(at, m.GetObject().GetParameters(), site)...)
			} else if sib, ok := sibs.Of(m); ok && sib.Pointer == site {
				diags = append(diags, listRepeats(at, sib.Item.GetParameters(), site)...)
			}
		case *soa.Operation:
			diags = append(diags, listRepeats(at, m.GetParameters(), site)...)
		default:
			// No other model holds a parameter list.
		}
		return nil
	})
	if err != nil {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.Validation, at(site), "%s", err.Error()))
	}
	return diags
}

// paramSlot is what OpenAPI identifies a parameter by within one list: its name
// and location, compared as written.
type paramSlot struct {
	name, in string
}

// listRepeats reports each entry of the parameter list held by the object at
// owner that repeats an earlier entry's slot, at the repeat, naming the first
// holder. An entry is read as the lowering reads it (refscope.Object), so a
// repeat reported here is one the lowering numbers; one whose $ref resolved to
// nothing has no slot.
func listRepeats(at func(jsontext.Pointer) ir.Provenance, params []*soa.ReferencedParameter, owner jsontext.Pointer) []ir.Diagnostic {
	firstAt := make(map[paramSlot]jsontext.Pointer, len(params))
	var diags []ir.Diagnostic
	for i, rp := range params {
		p := refscope.Object[soa.Parameter](rp)
		if p == nil {
			continue
		}
		entry := owner.AppendToken("parameters").AppendToken(strconv.Itoa(i))
		slot := paramSlot{name: p.GetName(), in: string(p.GetIn())}
		first, repeated := firstAt[slot]
		if !repeated {
			firstAt[slot] = entry
			continue
		}
		diags = append(diags, diag.Newf(ir.SeverityError, diag.DuplicateParameter, at(entry),
			"parameter %q in %q repeats the entry at %s", slot.name, slot.in, first))
	}
	return diags
}

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
