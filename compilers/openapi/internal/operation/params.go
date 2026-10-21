package operation

import (
	"encoding/json/jsontext"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// lowerParameters lowers an operation's merged parameter list (path-item plus
// operation) into logical Parameters and their HTTP wire bindings, in source
// order (ir-design §7.2, §8.1). The logical side carries the protocol-neutral
// input; the binding side carries the location, style, and explode facts.
// Each parameter lowers at its declaration: the $ref target for a referenced
// entry, else the list entry itself (issue #107). Its ID is rooted at mount, the
// operation's mount pointer, not at the declaration it was copied from; a
// repeat of one name and location keeps an ID of its own (paramLedger).
func lowerParameters(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, mount jsontext.Pointer, params []sourcedParam) ([]ir.Parameter, []ir.HTTPParamBinding, []ir.Diagnostic) {
	if len(params) == 0 {
		return nil, nil, nil
	}
	var diags []ir.Diagnostic
	logical := make([]ir.Parameter, 0, len(params))
	bindings := make([]ir.HTTPParamBinding, 0, len(params))
	ledger := newParamLedger(mount)
	for _, sp := range params {
		pc := c
		if sp.in != nil {
			pc = c.InScope(*sp.in)
		}
		p, pptr := resolve.ObjectAt[soa.Parameter](pc.RefScope(), sp.ref, sp.pointer)
		if p == nil {
			continue
		}
		id, repeatDiags := ledger.next(pc, p.GetName(), string(p.GetIn()), sp.pointer)
		diags = append(diags, repeatDiags...)
		param, binding, paramDiags := lowerParameter(lowering.Within[soa.Parameter](pc, sp.ref), ts, anchors, id, p, pptr)
		diags = append(diags, paramDiags...)
		// A summary or description beside a $ref of the entry's chain documents
		// this use of the parameter, over what its declaration says (GitHub #610).
		param.Docs = resolve.UseDocs[soa.Parameter](sp.ref, param.Docs)
		logical = append(logical, param)
		bindings = append(bindings, binding)
	}
	return logical, bindings, diags
}

// lowerParameter lowers one resolved parameter into its logical Parameter and
// HTTP binding, both naming the parameter by id. Path parameters are always
// required regardless of the declared flag (OpenAPI requires it).
func lowerParameter(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, id ir.ParamID, p *soa.Parameter, pptr jsontext.Pointer) (ir.Parameter, ir.HTTPParamBinding, []ir.Diagnostic) {
	name, in := p.GetName(), p.GetIn()
	param := ir.Parameter{
		ID:         id,
		Name:       compile.NamingFor(name),
		Required:   p.GetRequired() || in == soa.ParameterInPath,
		Provenance: c.ProvenanceAt(pptr),
	}
	style, explode := resolveStyleExplode(p, in)
	binding := ir.HTTPParamBinding{
		Param:         id,
		Location:      httpLocation(in),
		WireName:      name,
		Style:         style,
		Explode:       explode,
		AllowReserved: p.GetAllowReserved(),
	}
	media, diags := fillParamType(c, ts, anchors, &param, &binding, p, pptr, name)
	diags = append(diags, reservedHeaderParamDiag(c, name, in, pptr)...)
	diags = append(diags, querystringKeywordDiags(c, in, p, pptr)...)
	return param, binding, append(diags, fillParamDetail(c, &param, p, pptr, media)...)
}

// querystringKeywordDiags reports explode and allowReserved when they are
// declared on a parameter at in: querystring. OpenAPI 3.2 forbids both there
// alongside style — the location binds the whole query string from the
// parameter's content, so its serialization is stated by the media type alone
// and neither keyword has anything left to qualify. The bundled parser
// enforces the rule for style but not for these two (GitHub #408), so this
// compiler reports the gap itself; resolveStyleExplode already lowers each
// value as declared regardless; see diag.InvalidLocationKeyword for why this
// is a warning rather than the error style gets from the parser.
func querystringKeywordDiags(c lowering.Ctx, in soa.ParameterIn, p *soa.Parameter, pptr jsontext.Pointer) []ir.Diagnostic {
	if in != soa.ParameterInQueryString {
		return nil
	}
	var diags []ir.Diagnostic
	if p.Explode != nil {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.InvalidLocationKeyword,
			pptr+ids.Ptr("explode"),
			"parameter field explode is not allowed for in=querystring; lowered as declared"))
	}
	if p.AllowReserved != nil {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.InvalidLocationKeyword,
			pptr+ids.Ptr("allowReserved"),
			"parameter field allowReserved is not allowed for in=querystring; lowered as declared"))
	}
	return diags
}

// reservedHeaderParamDiag reports a header parameter OpenAPI §4.8.12 reserves —
// one named Accept, Content-Type or Authorization, whose definition it says
// SHALL be ignored. The comparison is case-insensitive because HTTP field names
// are, so a parameter spelled "authorization" collides with the security scheme
// exactly as one spelled "Authorization" does.
//
// This is the parameter half of the rule, and reservedHeaderEntryDiag is the
// headers-map half. The parameter still lowers: see diag.ReservedHeaderName for
// why keeping it and reporting it is the choice, rather than dropping it here.
func reservedHeaderParamDiag(c lowering.Ctx, name string, in soa.ParameterIn, pptr jsontext.Pointer) []ir.Diagnostic {
	if in != soa.ParameterInHeader {
		return nil
	}
	for _, reserved := range []string{"Accept", "Content-Type", "Authorization"} {
		if !strings.EqualFold(name, reserved) {
			continue
		}
		return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.ReservedHeaderName, pptr,
			"header parameter %q is reserved: OpenAPI says a definition for %s SHALL be ignored, "+
				"so it is lowered as declared and left for the emitter to suppress", name, reserved)}
	}
	return nil
}

// fillParamType lowers a parameter's type from the spelling electTypeSpelling
// elects — its schema, or the single media-type entry of a content-style
// parameter, whose media type goes on the binding. Constraints come from that
// same schema position; the default comes from it too, falling back to its $ref
// target (docs/compilers/openapi.md#ref-use-site).
func fillParamType(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, param *ir.Parameter, binding *ir.HTTPParamBinding, p *soa.Parameter, pptr jsontext.Pointer, name string) ([]ir.Example, []ir.Diagnostic) {
	elected, diags := electTypeSpelling(c, p.GetSchema(), p.GetContent(), p.GetRootNode(), pptr,
		"parameter", "ir.Parameter")
	paramType, typeDiags := schema.CarriedRef(c, ts, anchors, schema.TopLevelDepth, elected.js, elected.pointer, name)
	diags = append(diags, typeDiags...)
	param.Type = paramType
	param.Unmodeled = annotation.MergeUnmodeled(param.Unmodeled, elected.unmodeled)
	binding.ContentType = elected.mediaType
	return elected.examples, append(diags, fillParamSchema(c, ts, param, elected.js, elected.pointer)...)
}

// fillParamSchema reads a parameter schema's default value and scalar
// constraints through the readers a property's go through (schema.DefaultOf,
// schema.ConstraintsOf), so the two carriers cannot read one schema apart.
//
// A schema spelled {$ref: …} resolves its target so the annotations that
// inherit from it still reach the parameter
// (docs/compilers/openapi.md#ref-use-site, GitHub #131). Constraints stay
// use-site-only, as fillPropertyConstraints keeps them: the referent's bounds
// are left where declared, since bounds conjoin rather than override and
// copying one down under use-site precedence would publish the wider bound as
// the whole truth (ir-design §12.2).
func fillParamSchema(c lowering.Ctx, ts *compile.Types, param *ir.Parameter, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) []ir.Diagnostic {
	if js == nil || !js.IsSchema() {
		return nil
	}
	s := js.GetSchema()
	if s == nil {
		return nil
	}
	// resolve.Scope.TargetSchema reads the end of a $ref chain, since one hop
	// would take the default and description off an intermediate reference
	// instead of the schema that declares them.
	tgt := c.RefScope().TargetSchema(js, s)
	def, diags := schema.DefaultOf(c, s, tgt, pointer)
	param.Default = def

	cons, consDiags := schema.ConstraintsOf(c, &param.Unmodeled, s, pointer)
	param.Constraints = cons
	diags = append(diags, consDiags...)
	diags = append(diags, fillParamSchemaAnnotations(c, ts, param, s, tgt, pointer)...)
	// A parameter is the third annotation.HomeCarrier, so the keywords beside a
	// $ref that the alias it resolves to cannot hold are kept here, exactly as a
	// property and a header keep them (schema.FillPropertyDetail).
	return append(diags, schema.PreserveRefSiteKeywords(c, ts, &param.Unmodeled, js, param.Type, pointer)...)
}

// fillParamSchemaAnnotations records what a parameter's schema declares on the
// parameter itself. ir.Parameter is the carrier here as ir.Property is for a
// model property, so a schema that reduced to a shared primitive still keeps
// what it wrote (GitHub #116); one that hoisted a node of its own keeps them
// there, one home per declaration.
//
// fillParamDetail writes the parameter's own annotations afterwards, and they
// win where both are set. tgt is the schema the use-site $ref resolves to,
// which docs and deprecation fall back to
// (docs/compilers/openapi.md#ref-use-site); examples, xml and the visibility
// keywords stay site-only, since they describe the position, not the type.
func fillParamSchemaAnnotations(c lowering.Ctx, ts *compile.Types, param *ir.Parameter, s, tgt *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	// Visibility is kept before the own-node guard, not after it. The guard exists
	// so an annotation with a home on the node is not also copied to the carrier,
	// but readOnly/writeOnly have no home on either: recordDeclarationResidue
	// skips this position because a parameter is an annotation.HomeCarrier, and
	// ir.Parameter has no Visibility field. Keeping them after the guard would
	// drop them for exactly the parameters whose schema owns a node — an object,
	// an enum, an array.
	diags := preserveParamVisibility(c, param, s, pointer)
	if schema.LoweredToOwnNode(c, ts, pointer, param.Type) {
		return diags
	}
	a, readDiags := annotation.Read(annotation.Site{Kind: annotation.Reference, Node: s, Referent: tgt}, pointer, c.ProvenanceAt)
	diags = append(diags, readDiags...)

	param.Docs = a.Docs
	if a.Deprecated {
		param.Deprecation = &ir.Deprecation{}
	}
	if len(a.Examples) > 0 {
		param.Examples = a.Examples
	}
	if a.XML != nil {
		diags = append(diags, preserveParamXML(c, param, s, pointer)...)
	}
	diags = append(diags, preserveParamRedaction(c, param, s, pointer)...)
	param.Unmodeled = annotation.MergeUnmodeled(param.Unmodeled, a.Unmodeled)
	// Last, per schema.PreserveUnknownKeywords: it keeps only the keywords no
	// reader above it kept, and everything annotation.Read recorded is already on
	// the parameter by this line.
	return append(diags, schema.PreserveUnknownKeywords(c, &param.Unmodeled, s, pointer)...)
}

// preserveParamXML keeps a parameter schema's xml hints instead of dropping
// them: ir.Parameter is the one annotation carrier with no XML field, and the
// hint is not inert at this position — a content-style parameter can bind
// application/xml, the media type OpenAPI §4.8.26 conditions xml on, and the
// binding records that content type. ReasonNoIRHome, since the IR can close the
// gap by adding the field (GitHub #124).
func preserveParamXML(c lowering.Ctx, param *ir.Parameter, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	at := pointer + ids.Ptr("xml")
	kept, diags := schema.PreserveSchemaKeyword(c, &param.Unmodeled, s, "xml", ir.ReasonNoIRHome, at)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"parameter schema xml hints have no ir.Parameter home; kept verbatim under Unmodeled"))
	}
	return diags
}

// preserveParamRedaction keeps a parameter schema's `format: password`
// verbatim: ir.Parameter is the one annotation carrier with no redaction field,
// where a type node has Sensitive and a property Secret. Only the format this
// site writes is kept; a referent's redaction is already Sensitive on the
// referent's own node, which the parameter's type reaches. ReasonNoIRHome, since
// the IR can close the gap by adding the field (GitHub #578).
func preserveParamRedaction(c lowering.Ctx, param *ir.Parameter, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if !annotation.DeclaresRedaction(s) {
		return nil
	}
	at := pointer + ids.Ptr("format")
	kept, diags := schema.PreserveSchemaKeyword(c, &param.Unmodeled, s, "format", ir.ReasonNoIRHome, at)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"parameter schema format: password asks for redaction, which has no ir.Parameter home; "+
				"kept verbatim under Unmodeled"))
	}
	return diags
}

// preserveParamVisibility keeps the residue keywords a parameter schema writes
// that ir.Parameter has no field for. The rule
// docs/compilers/openapi.md#use-bound-annotations lowers readOnly/writeOnly to
// a Visibility, which only ir.Property has (ir/operation.go) — so at a
// parameter they reach no field, and nothing else on this path reads them.
// ReasonNoIRHome, since the IR can close the gap by adding the field, exactly
// as the xml hints beside them are kept.
//
// The list is asked of the schema package rather than restated here, so a
// keyword added there is preserved here too instead of being dropped at this one
// carrier.
func preserveParamVisibility(c lowering.Ctx, param *ir.Parameter, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range schema.ResidueKeywords() {
		if paramHoldsResidue(keyword) {
			continue
		}
		at := pointer + ids.Ptr(keyword)
		kept, keptDiags := schema.PreserveSchemaKeyword(c, &param.Unmodeled, s, keyword, ir.ReasonNoIRHome, at)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"parameter schema %s has no ir.Parameter home; kept verbatim under Unmodeled", keyword))
	}
	return diags
}

// paramHoldsResidue reports whether ir.Parameter has a field for a residue
// keyword. Only `default` does — Parameter.Default, which fillParamSchema
// fills — so recording it verbatim as well would give one declaration two homes.
func paramHoldsResidue(keyword string) bool {
	return keyword == "default"
}

// fillParamDetail enriches a parameter with its docs, deprecation, examples, and
// extensions. pptr is the parameter's own pointer, for example diagnostics. Each
// field is written only when the parameter declares it, so it overlays the
// schema-derived annotations fillParamSchema already recorded rather than
// erasing them with an unset value.
//
// The extension promotion runs last, after the parameter's own extensions have
// been preserved: PromoteDeprecation reads the kept Unmodeled entries rather
// than the source node, so a parameter whose x-* keys are not in the map yet
// has nothing to promote from (GitHub #423).
func fillParamDetail(c lowering.Ctx, param *ir.Parameter, p *soa.Parameter, pptr jsontext.Pointer, media []ir.Example) []ir.Diagnostic {
	if d := p.GetDescription(); d != "" {
		param.Docs.Description = d
	}
	if p.GetDeprecated() {
		param.Deprecation = &ir.Deprecation{}
	}
	own, diags := exampleList(c, p.GetExample(), p.GetExamples(), pptr)
	if ex := positionExamples(own, media); len(ex) > 0 {
		param.Examples = ex
	}
	pExt, extDiags := schema.ExtensionsOf(c, p.GetExtensions(), pptr)
	diags = append(diags, extDiags...)
	param.Unmodeled = annotation.MergeUnmodeled(param.Unmodeled, pExt)
	diags = append(diags, annotation.UnknownKeysIn(&param.Unmodeled, p, c.ProvenanceAt, pptr)...)
	diags = append(diags, preserveAllowEmptyValue(c, param, p, pptr)...)
	return append(diags, c.PromoteDeprecation(param.Unmodeled, param.Deprecation, &param.Provenance)...)
}

// preserveAllowEmptyValue keeps a parameter's allowEmptyValue flag, which says
// a query parameter may be sent with an empty value: a wire fact about how it
// serializes, like style, explode and allowReserved, which ir.HTTPParamBinding
// holds. The binding has no field for this one (GitHub #39).
//
// ReasonNoIRHome, not a boundary, since the IR can close the gap by adding the
// field. It is kept on ir.Parameter because that, not ir.HTTPParamBinding, is
// the carrier here with an Unmodeled map.
//
// It keeps presence, not truth: allowEmptyValue: false is a declared fact too,
// and keeping only the true spelling would decide which declarations count.
func preserveAllowEmptyValue(c lowering.Ctx, param *ir.Parameter, p *soa.Parameter, pptr jsontext.Pointer) []ir.Diagnostic {
	at := pptr + ids.Ptr("allowEmptyValue")
	kept, diags := schema.PreserveNode(c, &param.Unmodeled, "openapi:allowEmptyValue",
		annotation.RawChildNode(p.GetRootNode(), "allowEmptyValue"), ir.ReasonNoIRHome, at)
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"parameter allowEmptyValue has no ir.HTTPParamBinding home; kept verbatim under Unmodeled"))
}

// resolveStyleExplode materializes a parameter's resolved serialization style
// and explode flag: an explicit value wins, else the OpenAPI per-location
// default (query/cookie → form/true, path/header → simple/false, querystring →
// neither). The result is declared facts, not policy.
func resolveStyleExplode(p *soa.Parameter, in soa.ParameterIn) (string, *bool) {
	style := defaultParamStyle(in)
	if p.Style != nil {
		style = string(*p.Style)
	}
	// A declared explode lowers as declared wherever it is written, for the same
	// reason a declared style does: 3.2 forbids both at in: querystring, but
	// dropping content the document states is an emitter's call, not a
	// compiler's, and this position has an IR field to hold it.
	if p.Explode != nil {
		return style, new(*p.Explode)
	}
	// Absent one, explode qualifies a style, so a position resolving to no style
	// gets no default either — a querystring binding's ContentType is the whole
	// of its declared serialization (GitHub #334).
	if style == "" {
		return "", nil
	}
	return style, new(style == string(soa.SerializationStyleForm))
}

// defaultParamStyle returns the OpenAPI default serialization style for a
// parameter location, and "" for one that has none: 3.2 binds in: querystring
// from the parameter's content and forbids style there, so that location has no
// default to fall back on.
func defaultParamStyle(in soa.ParameterIn) string {
	switch in {
	case soa.ParameterInQueryString:
		return ""
	case soa.ParameterInQuery, soa.ParameterInCookie:
		return string(soa.SerializationStyleForm)
	default:
		return string(soa.SerializationStyleSimple)
	}
}

// httpLocation maps an OpenAPI parameter location onto the IR HTTP location.
func httpLocation(in soa.ParameterIn) ir.HTTPLocation {
	switch in {
	case soa.ParameterInPath:
		return ir.HTTPLocationPath
	case soa.ParameterInQueryString:
		return ir.HTTPLocationQuerystring
	case soa.ParameterInHeader:
		return ir.HTTPLocationHeader
	case soa.ParameterInCookie:
		return ir.HTTPLocationCookie
	default:
		return ir.HTTPLocationQuery
	}
}
