package operation

import (
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
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// lowerParameters lowers an operation's merged parameter list (path-item plus
// operation) into logical Parameters and their HTTP wire bindings, in source
// order (ir-design §7.2, §8.1). The logical side carries the protocol-neutral
// input; the binding side carries the location, style, and explode facts.
// Each parameter lowers at its declaration: the $ref target for a referenced
// entry, else the list entry itself (issue #107).
func lowerParameters(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, params []sourcedParam) ([]ir.Parameter, []ir.HTTPParamBinding, []ir.Diagnostic) {
	if len(params) == 0 {
		return nil, nil, nil
	}
	var diags []ir.Diagnostic
	logical := make([]ir.Parameter, 0, len(params))
	bindings := make([]ir.HTTPParamBinding, 0, len(params))
	for _, sp := range params {
		p, pptr := resolve.ObjectAt[soa.Parameter](c.RefScope(), sp.ref, sp.pointer)
		if p == nil {
			continue
		}
		param, binding, paramDiags := lowerParameter(c, ts, anchors, p, pptr)
		diags = append(diags, paramDiags...)
		logical = append(logical, param)
		bindings = append(bindings, binding)
	}
	return logical, bindings, diags
}

// lowerParameter lowers one resolved parameter into its logical Parameter and
// HTTP binding. Path parameters are always required regardless of the declared
// flag (OpenAPI requires it).
func lowerParameter(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, p *soa.Parameter, pptr string) (ir.Parameter, ir.HTTPParamBinding, []ir.Diagnostic) {
	name, in := p.GetName(), p.GetIn()
	param := ir.Parameter{
		Name:     compile.NamingFor(name),
		Required: p.GetRequired() || in == soa.ParameterInPath,
	}
	style, explode := resolveStyleExplode(p, in)
	binding := ir.HTTPParamBinding{
		Param:         name,
		Location:      httpLocation(in),
		WireName:      name,
		Style:         style,
		Explode:       explode,
		AllowReserved: p.GetAllowReserved(),
	}
	diags := fillParamType(c, ts, anchors, &param, &binding, p, pptr, name)
	diags = append(diags, reservedHeaderParamDiag(c, name, in, pptr)...)
	return param, binding, append(diags, fillParamDetail(c, &param, p, pptr)...)
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
func reservedHeaderParamDiag(c lowering.Ctx, name string, in soa.ParameterIn, pptr string) []ir.Diagnostic {
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
// target (§14).
func fillParamType(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, param *ir.Parameter, binding *ir.HTTPParamBinding, p *soa.Parameter, pptr, name string) []ir.Diagnostic {
	elected, diags := electTypeSpelling(c, p.GetSchema(), p.GetContent(), p.GetRootNode(), pptr)
	paramType, typeDiags := schema.CarriedRef(c, ts, anchors, schema.TopLevelDepth, elected.js, elected.pointer, name)
	diags = append(diags, typeDiags...)
	param.Type = paramType
	param.Unmodeled = annotation.MergeUnmodeled(param.Unmodeled, elected.unmodeled)
	binding.ContentType = elected.mediaType
	return append(diags, fillParamSchema(c, ts, param, elected.js, elected.pointer)...)
}

// fillParamSchema reads a parameter schema's default value and scalar
// constraints. Numeric bounds flow through annotation.Constraints, which reads
// raw decimal nodes rather than the *float64 model fields.
//
// A schema spelled {$ref: …} resolves its target so the annotations that
// inherit from it still reach the parameter (ir-design §14, GitHub #131).
// Constraints stay use-site-only, exactly as fillPropertyConstraints keeps
// them: a parameter must not inherit more from a referent than a property does.
func fillParamSchema(c lowering.Ctx, ts *compile.Types, param *ir.Parameter, js *oas3.JSONSchema[oas3.Referenceable], pointer string) []ir.Diagnostic {
	if js == nil || !js.IsSchema() {
		return nil
	}
	s := js.GetSchema()
	if s == nil {
		return nil
	}
	// resolve.TargetSchema, not annotation.At's Referent: the fallback must read the
	// end of a $ref chain, since one hop would take the default and description
	// off an intermediate reference instead of the schema that declares them.
	tgt := resolve.TargetSchema(js, s)
	diags := fillParamDefault(c, param, s, tgt, pointer)

	cons, consDiags := annotation.Constraints(s, c.ExclusiveBoundIsBoolean())
	diags = append(diags, schema.StampConstraintDiags(c, consDiags, pointer)...)
	if cons != nil {
		param.Constraints = cons
	}
	return append(diags, fillParamSchemaAnnotations(c, ts, param, s, tgt, pointer)...)
}

// fillParamDefault sets the parameter default, preferring the use-site node
// over the $ref target's; an unconvertible node yields a diagnostic. It mirrors
// fillPropertyDefault — the same keyword, read the same way, at the other
// carrier.
func fillParamDefault(c lowering.Ctx, param *ir.Parameter, s, tgt *oas3.Schema, pointer string) []ir.Diagnostic {
	node := s.GetDefault()
	if node == nil && tgt != nil {
		node = tgt.GetDefault()
	}
	if node == nil {
		return nil
	}
	v, err := value.FromNode(node)
	if err != nil {
		return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer, "default: %s", err.Error())}
	}
	param.Default = &v
	return nil
}

// fillParamSchemaAnnotations records what a parameter's schema declares on the
// parameter itself. ir.Parameter is the carrier for this position the way
// ir.Property is for a model property, so a schema that reduced to a shared
// primitive still keeps what it wrote (GitHub #116); a schema that hoisted a node
// of its own keeps them there instead, one home per declaration.
//
// The parameter's own annotations are written afterwards by fillParamDetail and
// win where both are set. tgt is the schema the use-site $ref resolves to, which
// docs and deprecation fall back to when the use-site is silent about them
// (ir-design §14); examples, xml and the visibility keywords stay site-only,
// since they describe the position rather than the type.
func fillParamSchemaAnnotations(c lowering.Ctx, ts *compile.Types, param *ir.Parameter, s, tgt *oas3.Schema, pointer string) []ir.Diagnostic {
	// Visibility is kept before the own-node guard, not after it. The guard exists
	// so an annotation with a home on the node is not also copied to the carrier,
	// but readOnly/writeOnly have no home on either: recordDeclarationResidue
	// skips this position because a parameter is an annotation.HomeCarrier, and
	// ir.Parameter has no Visibility field. Keeping them after the guard would
	// drop them for exactly the parameters whose schema owns a node — an object,
	// an enum, an array.
	diags := preserveParamVisibility(c, param, s, pointer)
	if schema.LoweredToOwnNode(ts, pointer, param.Type) {
		return diags
	}
	a, readDiags := annotation.Read(annotation.Site{Kind: annotation.Reference, Node: s, Referent: tgt}, pointer, c.SrcIndex)
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
	param.Unmodeled = annotation.MergeUnmodeled(param.Unmodeled, a.Unmodeled)
	return diags
}

// preserveParamXML keeps a parameter schema's xml hints instead of dropping
// them: ir.Parameter is the one annotation carrier with no XML field, and the
// hint is not inert at this position — a content-style parameter can bind
// application/xml, the media type OpenAPI §4.8.26 conditions xml on, and the
// binding records that content type. ReasonNoIRHome, since the IR can close the
// gap by adding the field (GitHub #124).
func preserveParamXML(c lowering.Ctx, param *ir.Parameter, s *oas3.Schema, pointer string) []ir.Diagnostic {
	at := pointer + ids.Ptr("xml")
	kept, diags := schema.PreserveSchemaKeyword(c, &param.Unmodeled, s, "xml", ir.ReasonNoIRHome, at)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"parameter schema xml hints have no ir.Parameter home; kept verbatim under Unmodeled"))
	}
	return diags
}

// preserveParamVisibility keeps the residue keywords a parameter schema writes
// that ir.Parameter has no field for. §14 lowers readOnly/writeOnly to a
// Visibility, which only ir.Property has (ir/operation.go) — so at a parameter
// they reach no field, and nothing else on this path reads them. ReasonNoIRHome,
// since the IR can close the gap by adding the field, exactly as the xml hints
// beside them are kept.
//
// The list is asked of the schema package rather than restated here, so a
// keyword added there is preserved here too instead of being dropped at this one
// carrier.
func preserveParamVisibility(c lowering.Ctx, param *ir.Parameter, s *oas3.Schema, pointer string) []ir.Diagnostic {
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
// keyword. Only `default` does — Parameter.Default, which fillParamDefault
// fills — so recording it verbatim as well would give one declaration two homes.
func paramHoldsResidue(keyword string) bool {
	return keyword == "default"
}

// fillParamDetail enriches a parameter with its docs, deprecation, examples, and
// extensions. pptr is the parameter's own pointer, for example diagnostics. Each
// field is written only when the parameter declares it, so it overlays the
// schema-derived annotations fillParamSchema already recorded rather than
// erasing them with an unset value.
func fillParamDetail(c lowering.Ctx, param *ir.Parameter, p *soa.Parameter, pptr string) []ir.Diagnostic {
	if d := p.GetDescription(); d != "" {
		param.Docs.Description = d
	}
	if p.GetDeprecated() {
		param.Deprecation = &ir.Deprecation{}
	}
	ex, diags := exampleList(c, p.GetExample(), p.GetExamples(), pptr)
	if len(ex) > 0 {
		param.Examples = ex
	}
	pExt, extDiags := schema.ExtensionsOf(c, p.GetExtensions(), pptr)
	diags = append(diags, extDiags...)
	param.Unmodeled = annotation.MergeUnmodeled(param.Unmodeled, pExt)
	return append(diags, preserveAllowEmptyValue(c, param, p, pptr)...)
}

// preserveAllowEmptyValue keeps a parameter's allowEmptyValue flag. It says a
// query parameter may be sent with an empty value — a wire fact about how the
// parameter serializes, alongside style, explode and allowReserved, which
// ir.HTTPParamBinding does hold. It holds no field for this one, and nothing
// else on this path read the flag either, so a document declaring it lost it
// outright (GitHub #39).
//
// ReasonNoIRHome rather than a boundary, for the same reason as its neighbours:
// the IR can close the gap by adding the field. It is kept on ir.Parameter
// because that is the carrier at this position with an Unmodeled map at all —
// ir.HTTPParamBinding has none.
//
// A parameter that does not declare it records nothing: RawChildNode returns nil
// for an absent keyword and PreserveNode keeps nothing for a nil node. That is
// deliberately presence, not truth — allowEmptyValue: false is a declared fact
// too, and a compiler that kept only the true spelling would decide for the
// reader which declarations count.
func preserveAllowEmptyValue(c lowering.Ctx, param *ir.Parameter, p *soa.Parameter, pptr string) []ir.Diagnostic {
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
// default (query/cookie → form/true, path/header → simple/false). The result is
// declared facts, not policy.
func resolveStyleExplode(p *soa.Parameter, in soa.ParameterIn) (string, *bool) {
	style := defaultParamStyle(in)
	if p.Style != nil {
		style = string(*p.Style)
	}
	explode := style == string(soa.SerializationStyleForm)
	if p.Explode != nil {
		explode = *p.Explode
	}
	return style, &explode
}

// defaultParamStyle returns the OpenAPI default serialization style for a
// parameter location.
func defaultParamStyle(in soa.ParameterIn) string {
	switch in {
	case soa.ParameterInQuery, soa.ParameterInCookie, soa.ParameterInQueryString:
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
