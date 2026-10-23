package operation

import (
	"encoding/json/jsontext"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// encodingConfig lowers one Encoding object's declared wire config: content
// types, per-part headers, and form-style serialization. The structural flags
// (Multi, Filename) come from the part, not from here.
//
// It returns what the object declared that PartEncoding has no field for as a
// second value rather than writing it, because PartEncoding carries no
// Unmodeled map of its own; scope is where that belongs on the owning Content
// (see encodingUnmodeled).
func encodingConfig(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, enc *soa.Encoding, encPtr jsontext.Pointer, scope string) (ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	pe := ir.PartEncoding{}
	if enc == nil {
		return pe, nil, nil
	}
	pe.ContentTypes = splitContentTypes(enc.GetContentTypeValue())
	headers, diags := lowerHeaders(c, ts, anchors, enc.GetHeaders(), encPtr)
	pe.Headers = headers
	if enc.Style != nil {
		pe.Style = string(*enc.Style)
	}
	pe.Explode = enc.Explode
	unmodeled, encDiags := encodingUnmodeled(c, enc, encPtr, scope)
	return pe, unmodeled, append(diags, encDiags...)
}

// encodingUnmodeled keeps what an Encoding Object declares that nothing in the
// IR holds: `allowReserved`, which ir.PartEncoding has no field for, the
// object's own x-*, and the keys the specification defines for no encoding at
// all. Otherwise documents differing only in those compile to one IR (GitHub
// #291).
//
// Entries ride on the owning ir.Content, since PartEncoding carries no
// Unmodeled map, keyed under scope: "encoding/<part>" or "itemEncoding". One
// content can hold several such entries in the same map, so the part tells them
// apart.
//
// allowReserved carries ReasonNoIRHome and announces itself only when something
// was written, as preserveHeaderSerialization does.
func encodingUnmodeled(c lowering.Ctx, enc *soa.Encoding, encPtr jsontext.Pointer, scope string) (ir.Unmodeled, []ir.Diagnostic) {
	var out ir.Unmodeled
	at := encPtr + ids.Ptr("allowReserved")
	kept, diags := annotation.PreserveNodeInto(&out, "openapi:"+scope+"/allowReserved",
		nodeview.Child(enc.GetRootNode(), "allowReserved"), ir.ReasonNoIRHome, c.ProvenanceAt(at))
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"encoding allowReserved has no ir.PartEncoding home; kept verbatim under Unmodeled"))
	}
	diags = append(diags, nestedEncodings(c, &out, enc, encPtr, scope)...)
	ext, extDiags := annotation.ExtensionsUnder(enc.GetExtensions(), c.ProvenanceAt, encPtr, scope)
	out = annotation.MergeUnmodeled(out, ext)
	diags = append(diags, extDiags...)
	return out, append(diags, annotation.UnknownKeysUnder(&out, enc, c.ProvenanceAt, encPtr, scope)...)
}

// nestedEncodingFields are the OpenAPI 3.2 Encoding Object fields that carry a
// nested Encoding Object: the object's own encoding map, the positional prefix
// encodings, and the item encoding governing what follows them. This compiler
// lowers an Encoding Object to ir.PartEncoding, which has no encoding fields of
// its own, so each reaches the IR in no modelled form.
var nestedEncodingFields = []string{"encoding", "prefixEncoding", "itemEncoding"}

// nestedEncodings keeps the nested Encoding Objects OpenAPI 3.2 lets an
// encoding write, verbatim under the scope the part's own entries ride on, one
// info each (GitHub #615). Recording them at the census's own key and pointer
// keeps the census from calling them undefined, at every version, as load
// reports them below 3.2 (diag.LaterVersionConstruct).
//
// ReasonNoIRHome, since PartEncoding could grow the fields. Nothing is lowered:
// a part inside a part has no shape here, and keeping the source is lossless.
func nestedEncodings(c lowering.Ctx, out *ir.Unmodeled, enc *soa.Encoding, encPtr jsontext.Pointer, scope string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range nestedEncodingFields {
		at := encPtr + ids.Ptr(keyword)
		kept, keptDiags := annotation.PreserveNodeInto(out, annotation.EntryKey(scope, keyword),
			nodeview.Child(enc.GetRootNode(), keyword), ir.ReasonNoIRHome, c.ProvenanceAt(at))
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"encoding %s has no ir.PartEncoding home; kept verbatim under Unmodeled", keyword))
	}
	return diags
}

// splitContentTypes splits an encoding contentType value on "," into trimmed,
// non-empty media types.
func splitContentTypes(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}
