package openapi

import (
	"strconv"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/ir"
)

// lowerParameters lowers an operation's merged parameter list (path-item plus
// operation) into logical Parameters and their HTTP wire bindings, in source
// order (ir-design §7.2, §8.1). The logical side carries the protocol-neutral
// input; the binding side carries the location, style, and explode facts.
func (l *lowerer) lowerParameters(params []*soa.ReferencedParameter, opPointer string) ([]ir.Parameter, []ir.HTTPParamBinding) {
	if len(params) == 0 {
		return nil, nil
	}
	logical := make([]ir.Parameter, 0, len(params))
	bindings := make([]ir.HTTPParamBinding, 0, len(params))
	for i, rp := range params {
		p := resolveParameter(rp)
		if p == nil {
			continue
		}
		pptr := opPointer + ptr("parameters", strconv.Itoa(i))
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
	l.fillParamDetail(&param, p)
	return param, binding
}

// fillParamType lowers a parameter's type from either its schema or, for a
// content-style parameter, its single media-type entry (recording the media
// type on the binding). Default and constraints come from the same schema.
func (l *lowerer) fillParamType(param *ir.Parameter, binding *ir.HTTPParamBinding, p *soa.Parameter, pptr, name string) {
	if content := p.GetContent(); content != nil && content.Len() > 0 {
		// A content parameter has exactly one media type; take the first entry.
		for mt, media := range content.All() {
			schemaPtr := pptr + ptr("content", mt, "schema")
			param.Type = l.schemaRef(media.GetSchema(), schemaPtr, name)
			binding.ContentType = mt
			l.fillParamSchema(param, media.GetSchema(), schemaPtr)
			break
		}
		return
	}
	schemaPtr := pptr + ptr("schema")
	param.Type = l.schemaRef(p.GetSchema(), schemaPtr, name)
	l.fillParamSchema(param, p.GetSchema(), schemaPtr)
}

// fillParamSchema reads a parameter schema's default value and scalar
// constraints. Numeric bounds flow through constraintsFromSchema, which reads
// raw decimal nodes rather than the *float64 model fields.
func (l *lowerer) fillParamSchema(param *ir.Parameter, js *oas3.JSONSchema[oas3.Referenceable], pointer string) {
	if js == nil || !js.IsSchema() {
		return
	}
	s := js.GetSchema()
	if s == nil {
		return
	}
	if node := s.GetDefault(); node != nil {
		if v, err := valueFromNode(node); err == nil {
			param.Default = &v
		} else {
			l.diags = append(l.diags, diagf(ir.SeverityWarning, codeDegradedConstruct,
				ir.Provenance{Source: l.srcIndex, Pointer: pointer}, "default: %s", err.Error()))
		}
	}
	c, diags := constraintsFromSchema(s)
	for i := range diags {
		diags[i].Provenance = ir.Provenance{Source: l.srcIndex, Pointer: pointer}
	}
	l.diags = append(l.diags, diags...)
	if c != nil {
		param.Constraints = c
	}
}

// fillParamDetail enriches a parameter with its docs, deprecation, examples, and
// extensions.
func (l *lowerer) fillParamDetail(param *ir.Parameter, p *soa.Parameter) {
	param.Docs.Description = p.GetDescription()
	if p.GetDeprecated() {
		param.Deprecation = &ir.Deprecation{}
	}
	if ex := l.exampleList(p.GetExample(), p.GetExamples()); len(ex) > 0 {
		param.Examples = ex
	}
	ext, diags := extensionsFrom(p.GetExtensions())
	l.diags = append(l.diags, diags...)
	if len(ext) > 0 {
		param.Extensions = ext
	}
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
