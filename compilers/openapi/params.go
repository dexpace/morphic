package openapi

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/ir"
)

// lowerParameters lowers an operation's merged parameter list (path-item plus
// operation) into logical Parameters and their HTTP wire bindings, in source
// order (ir-design §7.2, §8.1). The logical side carries the protocol-neutral
// input; the binding side carries the location, style, and explode facts.
// Each parameter lowers at its declaration: the $ref target for a referenced
// entry, else the list entry itself (issue #107).
func (l *lowerer) lowerParameters(params []sourcedParam) ([]ir.Parameter, []ir.HTTPParamBinding) {
	if len(params) == 0 {
		return nil, nil
	}
	logical := make([]ir.Parameter, 0, len(params))
	bindings := make([]ir.HTTPParamBinding, 0, len(params))
	for _, sp := range params {
		p, pptr := resolveRefAt[soa.Parameter](l, sp.ref, sp.pointer)
		if p == nil {
			continue
		}
		param, binding := l.lowerParameter(p, pptr)
		logical = append(logical, param)
		bindings = append(bindings, binding)
	}
	return logical, bindings
}

// lowerParameter lowers one resolved parameter into its logical Parameter and
// HTTP binding. Path parameters are always required regardless of the declared
// flag (OpenAPI requires it).
func (l *lowerer) lowerParameter(p *soa.Parameter, pptr string) (ir.Parameter, ir.HTTPParamBinding) {
	name, in := p.GetName(), p.GetIn()
	param := ir.Parameter{
		Name:     ir.Naming{Source: name, Canonical: canonicalWords(name)},
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
	l.fillParamType(&param, &binding, p, pptr, name)
	l.fillParamDetail(&param, p, pptr)
	return param, binding
}

// fillParamType lowers a parameter's type from either its schema or, for a
// content-style parameter, its single media-type entry (recording the media
// type on the binding). Constraints come from that same schema position;
// the default comes from it too, falling back to its $ref target (§14).
func (l *lowerer) fillParamType(param *ir.Parameter, binding *ir.HTTPParamBinding, p *soa.Parameter, pptr, name string) {
	if content := p.GetContent(); content != nil && content.Len() > 0 {
		// A content parameter has exactly one media type; take the first entry.
		for mt, media := range content.All() {
			schemaPtr := pptr + ptr("content", mt, "schema")
			param.Type = l.carriedSchemaRef(media.GetSchema(), schemaPtr, name)
			binding.ContentType = mt
			l.fillParamSchema(param, media.GetSchema(), schemaPtr)
			break
		}
		return
	}
	schemaPtr := pptr + ptr("schema")
	param.Type = l.carriedSchemaRef(p.GetSchema(), schemaPtr, name)
	l.fillParamSchema(param, p.GetSchema(), schemaPtr)
}

// fillParamSchema reads a parameter schema's default value and scalar
// constraints. Numeric bounds flow through constraintsFromSchema, which reads
// raw decimal nodes rather than the *float64 model fields.
//
// A schema spelled {$ref: …} resolves its target so the annotations that
// inherit from it still reach the parameter (ir-design §14, GitHub #131).
// Constraints stay use-site-only, exactly as fillPropertyConstraints keeps
// them: a parameter must not inherit more from a referent than a property does.
func (l *lowerer) fillParamSchema(param *ir.Parameter, js *oas3.JSONSchema[oas3.Referenceable], pointer string) {
	if js == nil || !js.IsSchema() {
		return
	}
	s := js.GetSchema()
	if s == nil {
		return
	}
	// refTargetSchema, not siteAt's Referent: the fallback must read the end of a
	// $ref chain, since one hop would take the default and description off an
	// intermediate reference instead of the schema that declares them.
	tgt := l.refTargetSchema(js, s)
	l.fillParamDefault(param, s, tgt, pointer)

	c, diags := constraintsFromSchema(s, l.exclusiveBoundIsBoolean())
	l.appendConstraintDiags(diags, pointer)
	if c != nil {
		param.Constraints = c
	}
	l.fillParamSchemaAnnotations(param, s, tgt, pointer)
}

// fillParamDefault sets the parameter default, preferring the use-site node
// over the $ref target's; an unconvertible node yields a diagnostic. It mirrors
// fillPropertyDefault — the same keyword, read the same way, at the other
// carrier.
func (l *lowerer) fillParamDefault(param *ir.Parameter, s, tgt *oas3.Schema, pointer string) {
	node := s.GetDefault()
	if node == nil && tgt != nil {
		node = tgt.GetDefault()
	}
	if node == nil {
		return
	}
	v, err := valueFromNode(node)
	if err != nil {
		l.diag(ir.SeverityWarning, codeDegradedConstruct, pointer, "default: %s", err.Error())
		return
	}
	param.Default = &v
}

// fillParamSchemaAnnotations records the annotations a parameter's schema
// declares on the parameter itself. ir.Parameter is the carrier for this
// position the way ir.Property is for a model property, so a schema that
// reduced to a shared primitive still keeps what it wrote (GitHub #116); a
// schema that hoisted a node of its own keeps them there instead, one home per
// declaration.
//
// The parameter's own annotations are written afterwards by fillParamDetail and
// win where both are set. tgt is the schema the use-site $ref resolves to, which
// docs and deprecation fall back to when the use-site is silent about them
// (ir-design §14); examples and xml stay site-only, since they describe the
// position rather than the type.
func (l *lowerer) fillParamSchemaAnnotations(param *ir.Parameter, s, tgt *oas3.Schema, pointer string) {
	if l.loweredToOwnNode(pointer, param.Type) {
		return
	}
	a, diags := annotations(site{Kind: siteReference, Node: s, Referent: tgt}, pointer, l.srcIndex)
	l.diags.AppendAll(diags)

	param.Docs = a.Docs
	if a.Deprecated {
		param.Deprecation = &ir.Deprecation{}
	}
	if len(a.Examples) > 0 {
		param.Examples = a.Examples
	}
	if a.XML != nil {
		l.preserveParamXML(param, s, pointer)
	}
	param.Unmodeled = mergeUnmodeled(param.Unmodeled, a.Unmodeled)
}

// preserveParamXML keeps a parameter schema's xml hints instead of dropping
// them: ir.Parameter is the one annotation carrier with no XML field, and the
// hint is not inert at this position — a content-style parameter can bind
// application/xml, the media type OpenAPI §4.8.26 conditions xml on, and the
// binding records that content type. ReasonNoIRHome, since the IR can close the
// gap by adding the field (GitHub #124).
func (l *lowerer) preserveParamXML(param *ir.Parameter, s *oas3.Schema, pointer string) {
	raw := nodeToRaw(rawPropertyNode(s, "xml"))
	if raw == nil {
		return
	}
	l.preserve(&param.Unmodeled, "openapi:xml", raw, ir.ReasonNoIRHome, pointer+ptr("xml"))
	l.diag(ir.SeverityInfo, codeDegradedConstruct, pointer,
		"parameter schema xml hints have no ir.Parameter home; kept verbatim under Unmodeled")
}

// fillParamDetail enriches a parameter with its docs, deprecation, examples, and
// extensions. pptr is the parameter's own pointer, for example diagnostics. Each
// field is written only when the parameter declares it, so it overlays the
// schema-derived annotations fillParamSchema already recorded rather than
// erasing them with an unset value.
func (l *lowerer) fillParamDetail(param *ir.Parameter, p *soa.Parameter, pptr string) {
	if d := p.GetDescription(); d != "" {
		param.Docs.Description = d
	}
	if p.GetDeprecated() {
		param.Deprecation = &ir.Deprecation{}
	}
	if ex := l.exampleList(p.GetExample(), p.GetExamples(), pptr); len(ex) > 0 {
		param.Examples = ex
	}
	param.Unmodeled = mergeUnmodeled(param.Unmodeled, l.extensions(p.GetExtensions(), pptr))
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
