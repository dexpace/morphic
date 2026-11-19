// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerParameters lowers an operation's merged parameter list (path-item plus
// operation) into logical Parameters and their HTTP wire bindings, in source
// order (docs/ir/operations.md, docs/ir/http-binding.md). The logical side
// carries the protocol-neutral input; the binding side carries the location,
// style, and explode facts. Each parameter lowers at its declaration: the $ref
// target for a referenced entry, else the list entry itself (issue #107). Its
// ID is rooted at mount, the operation's mount pointer, not at the declaration
// it was copied from; a repeat of one name and location keeps an ID of its own
// (paramLedger).
func lowerParameters(c service.Ctx, w operationLoweringState, mount jsontext.Pointer, params []sourcedParam) ([]ir.Parameter, []ir.HTTPParamBinding, []ir.Diagnostic) {
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
			pc = *sp.in
		}
		p, dc, pptr := service.Declared[soa.Parameter](pc, sp.ref, sp.pointer)
		if p == nil {
			continue
		}
		id := ledger.next(p.GetName(), string(p.GetIn()))
		param, binding, paramDiags := lowerParameter(dc, w, id, p, pptr)
		diags = append(diags, paramDiags...)
		// A summary or description beside a $ref of the entry's chain documents
		// this use of the parameter, over what its declaration says (GitHub #610).
		param.Docs = componentlookup.UseDocs[soa.Parameter](c.Bindings(), sp.ref, param.Docs)
		// What the chain writes beside its $ref rides on the parameter it lowers to.
		beside, besideDiags := service.Beside[soa.Parameter](pc, sp.ref, sp.pointer, true)
		param.Unmodeled = annotation.MergeUnmodeled(param.Unmodeled, beside)
		diags = append(diags, besideDiags...)
		logical = append(logical, param)
		bindings = append(bindings, binding)
	}
	return logical, bindings, diags
}

// lowerParameter lowers one resolved parameter into its logical Parameter and
// HTTP binding, both naming the parameter by id. Path parameters are always
// required regardless of the declared flag (OpenAPI requires it).
func lowerParameter(c service.Ctx, w operationLoweringState, id ir.ParameterID, p *soa.Parameter, pptr jsontext.Pointer) (ir.Parameter, ir.HTTPParamBinding, []ir.Diagnostic) {
	name, in := p.GetName(), p.GetIn()
	param := ir.Parameter{
		ID:         id,
		Name:       ir.NamingFor(name),
		Required:   p.GetRequired() || in == soa.ParameterInPath,
		Provenance: c.Place().At(pptr),
	}
	style, explode := styleAndExplode(p, in)
	binding := ir.HTTPParamBinding{
		Param:         id,
		Location:      httpLocation(in),
		WireName:      name,
		Style:         style,
		Explode:       explode,
		AllowReserved: p.GetAllowReserved(),
		// Read whatever the location, as allowReserved is: the validator refuses
		// it outside in: query, and warns where the style gives it no effect.
		AllowEmptyValue: p.GetAllowEmptyValue(),
	}
	elected, diags := fillParamType(c, w, &param, &binding, p, pptr)
	if elected.mediaType != "" && p.Style == nil {
		// The media type serializes the value, so no style is stated for it.
		binding.Style = ""
	}
	diags = append(diags, fillParamDetail(c, &param, p, pptr, elected)...)
	if p.AllowReserved != nil {
		return param, binding, diags
	}
	// Read once the keys are kept, and only where the parameter decides nothing:
	// its own key, else the nearest object it carries stating one.
	reserved, promoted := promotion.AllowReserved(c.Promoter(),
		promotion.Carried(c.Promoter(), param.Unmodeled, elected.carries...), &param.Provenance)
	binding.AllowReserved = reserved
	return param, binding, append(diags, promoted...)
}

// fillParamType lowers a parameter's type from the spelling electTypeSpelling
// elects — its schema, or the single media-type entry of a content-style
// parameter, whose media type goes on the binding. Constraints come from that
// same schema position; the default comes from it too, falling back to its $ref
// target (docs/compilers/openapi.md#ref-use-site).
func fillParamType(c service.Ctx, w operationLoweringState, param *ir.Parameter, binding *ir.HTTPParamBinding, p *soa.Parameter, pptr jsontext.Pointer) (typeSpelling, []ir.Diagnostic) {
	elected, diags := electTypeSpelling(c, w, p.GetSchema(), p.GetContent(), p.GetRootNode(), pptr,
		"parameter", "ir.Parameter")
	paramType, typeDiags := schema.CarriedRef(elected.ctx.Ctx, w.types, schema.TopLevelDepth, c.Statements().Of(elected.js), elected.pointer)
	diags = append(diags, typeDiags...)
	param.Type = paramType
	binding.ContentType = elected.mediaType
	var written ir.Unmodeled
	diags = append(diags, fillParamSchema(elected.ctx, w.types, param, &written, elected.js, elected.pointer)...)
	// What the schema keeps whole it keeps once, its inner keys not again.
	promotion.DropContained(c.Promoter(), &written)
	param.Unmodeled = annotation.MergeUnmodeled(annotation.Under(elected.schemaScope(), written), elected.unmodeled)
	return elected, diags
}

// fillParamSchema reads a parameter schema's default value and scalar
// constraints through the readers a property's go through
// (schema.Use.Default, schema.CarrierConstraintsOf), so the two carriers
// cannot read one schema apart. What no field of the parameter holds goes to
// written, for the caller to key beneath the schema's scope.
//
// A schema spelled {$ref: …} resolves its target so the annotations that
// inherit from it still reach the parameter
// (docs/compilers/openapi.md#ref-use-site, GitHub #131). Constraints stay
// use-site-only, as fillPropertyConstraints keeps them: bounds conjoin rather
// than override, so copying the referent's down would publish the wider bound
// as the whole truth (docs/ir/annotations.md#what-a-reference-carries-down).
func fillParamSchema(c service.Ctx, ts *compile.Types, param *ir.Parameter, written *ir.Unmodeled,
	js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer,
) []ir.Diagnostic {
	if js == nil || !js.IsSchema() {
		return nil
	}
	s := js.GetSchema()
	if s == nil {
		return nil
	}
	// schema.UseOf reads the end of a $ref chain, since one hop would take the
	// default and description off an intermediate reference instead of the
	// schema that declares them.
	use := schema.UseOf(c.Ctx, c.Statements().Of(js), param.Type, pointer)
	def, diags := use.Default(c.Ctx, c.Statements().Of(js).Schema, pointer)
	param.Default = def

	cons, consDiags := schema.CarrierConstraintsOf(c.Ctx, written, c.Statements().Of(js).Schema, use.FoldedAllOfBranches, param.Type, pointer)
	param.Constraints = cons
	diags = append(diags, consDiags...)
	diags = append(diags, fillParamSchemaAnnotations(c, param, written, c.Statements().Of(js).Schema, use, pointer)...)
	// A parameter is the third annotation.HomeCarrier, so the keywords beside a
	// $ref that the alias it resolves to cannot hold are kept here, exactly as a
	// property and a header keep them (schema.FillPropertyDetail).
	return append(diags, schema.PreserveRefSiteKeywords(c.Ctx, ts, written, c.Statements().Of(js), param.Type, pointer)...)
}

// fillParamSchemaAnnotations records what a parameter's schema declares on the
// parameter. ir.Parameter is the carrier here as ir.Property is for a
// model property, so a schema that reduced to a shared primitive keeps what it
// wrote (GitHub #116); one that hoisted a node keeps them there, one home per
// declaration.
//
// fillParamDetail writes the parameter's own annotations afterwards, and they
// win where both are set. Docs, deprecation and redaction read what use falls
// back to (docs/compilers/openapi.md#ref-use-site); examples, xml and the
// visibility keywords stay with the position, which they describe, and its
// folded branches. What has no field of the parameter goes to written.
func fillParamSchemaAnnotations(c service.Ctx, param *ir.Parameter, written *ir.Unmodeled,
	s *statement.Schema, use schema.Use, pointer jsontext.Pointer,
) []ir.Diagnostic {
	// Visibility is kept before the own-node guard, not after it. The guard exists
	// so an annotation with a home on the node is not also copied to the carrier,
	// but readOnly/writeOnly have no home on either: the residue a declaration
	// keeps on its node skips this position because a parameter is an
	// annotation.HomeCarrier, and ir.Parameter has no Visibility field. Keeping
	// them after the guard would drop them for exactly the parameters whose
	// schema owns a node — an object, an enum, an array.
	var diags []ir.Diagnostic
	for _, v := range schema.FoldedBranchesInOverwriteOrder(use.FoldedAllOfBranches) {
		diags = append(diags, preserveParamVisibility(c, written, v.Node, v.At)...)
	}
	diags = append(diags, preserveParamVisibility(c, written, s, pointer)...)
	if schema.LoweredToOwnNode(c.Ctx, pointer, param.Type) {
		return diags
	}
	a, readDiags := annotation.Read(use.SiteAt(c.Ctx, s, pointer), pointer, c.Place())
	diags = append(diags, readDiags...)

	param.Docs = a.Docs
	if a.Deprecated {
		param.Deprecation = &ir.Deprecation{}
	}
	if a.Sensitive {
		param.Secret = true
	}
	if len(a.Examples) > 0 {
		param.Examples = a.Examples
	}
	for _, v := range slices.Backward(use.FoldedAllOfBranches) {
		diags = append(diags, preserveParamXML(c, written, v.Node, v.At)...)
	}
	diags = append(diags, preserveParamXML(c, written, s, pointer)...)
	*written = annotation.MergeUnmodeled(*written, a.Unmodeled)
	// Last, as it keeps only the keywords no reader above it kept, and everything
	// annotation.Read recorded is already in written by this line.
	return append(diags, schema.PreserveCarriedKeywords(c.Ctx, written, s, pointer)...)
}

// preserveParamXML keeps a parameter schema's xml hints instead of dropping
// them: ir.Parameter is the one annotation carrier with no XML field, and the
// hint is not inert at this position — a content-style parameter can bind
// application/xml, the media type OpenAPI §4.8.26 conditions xml on, and the
// binding records that content type. ReasonNoIRHome, since the IR can close the
// gap by adding the field (GitHub #124).
func preserveParamXML(c service.Ctx, written *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	at := pointer + ids.Ptr("xml")
	kept, diags := annotation.PreserveSchemaKeyword(written, s, "xml", ir.ReasonNoIRHome, pointer, c.Place())
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"parameter schema xml hints have no ir.Parameter home; kept verbatim under Unmodeled"))
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
func preserveParamVisibility(c service.Ctx, written *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range schema.ResidueKeywords() {
		if paramHoldsResidue(keyword) {
			continue
		}
		at := pointer + ids.Ptr(keyword)
		kept, keptDiags := annotation.PreserveSchemaKeyword(written, s, keyword, ir.ReasonNoIRHome, pointer, c.Place())
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
// been preserved: a promotion reads the kept Unmodeled entries rather
// than the source node, so a parameter whose x-* keys are not in the map yet
// has nothing to promote from (GitHub #423).
func fillParamDetail(c service.Ctx, param *ir.Parameter, p *soa.Parameter, pptr jsontext.Pointer, elected typeSpelling) []ir.Diagnostic {
	if d := p.GetDescription(); d != "" {
		param.Docs.Description = d
	}
	if p.GetDeprecated() {
		param.Deprecation = &ir.Deprecation{}
	}
	own, diags := exampleList(c, p.GetExample(), p.GetExamples(), pptr)
	if ex := positionExamples(own, elected.examples); len(ex) > 0 {
		param.Examples = ex
	}
	pExt, extDiags := annotation.ExtensionsFrom(p.GetExtensions(), c.Place(), pptr)
	diags = append(diags, extDiags...)
	param.Unmodeled = annotation.MergeUnmodeled(param.Unmodeled, pExt)
	diags = append(diags, annotation.UnknownKeysIn(&param.Unmodeled, p, c.Place(), pptr)...)
	// Redaction and deprecation hold where the parameter or an object it carries
	// states them, as they hold where its schema's $ref target does; a value is
	// read at the nearest key writing one, and a misfit reported at each.
	fallbackReferent := c.Promoter().WithOwnCarriedState(schema.FallbackKeysForCarrier(c.Ctx, c.Statements().Of(elected.js), param.Type, elected.pointer), param.Unmodeled,
		elected.carries...)
	carried := promotion.Carried(c.Promoter(), param.Unmodeled, elected.carries...)
	diags = append(diags, schema.PromoteUse(c.Ctx, fallbackReferent, carried, &param.Deprecation, &param.Secret, &param.Provenance)...)
	return append(diags, promoteShadowed(c, param.Unmodeled, elected.carries...)...)
}

// promoteShadowed reads, for its findings alone, the deprecation and redaction
// each statement Carried passes over beneath scopes states (promotion.Shadowed),
// so a misfit behind a nearer statement of its key is reported once, at its own
// key, as the nearest statement's is by schema.PromoteUse.
func promoteShadowed(c service.Ctx, own ir.Unmodeled, scopes ...string) []ir.Diagnostic {
	layers := promotion.Shadowed(c.Promoter(), own, scopes...)
	diags := make([]ir.Diagnostic, 0, len(layers))
	for _, layer := range layers {
		var dep *ir.Deprecation
		var redact bool
		var prov ir.Provenance
		diags = append(diags, schema.PromoteUse(c.Ctx, promotion.Referent{}, layer, &dep, &redact, &prov)...)
	}
	return diags
}

// styleAndExplode returns a parameter's serialization style and its explode
// flag. The style is the written one, else the location's default, which a
// binding states because the location fixes it, unless the parameter's content
// is elected, whose media type serializes the value (lowerParameter). Explode is only what the
// parameter writes and nil otherwise: its default depends on the style, so filling
// it in would present the default as a declaration, and a deepObject that writes
// none would read as one that wrote false (GitHub #627).
func styleAndExplode(p *soa.Parameter, in soa.ParameterIn) (string, *bool) {
	style := defaultParamStyle(in)
	if p.Style != nil {
		style = string(*p.Style)
	}
	// A declared explode lowers as declared wherever it is written, for the same
	// reason a declared style does: 3.2 forbids both at in: querystring, but
	// dropping content the document states is an emitter's call, not a
	// compiler's, and this position has an IR field to hold it.
	if p.Explode == nil {
		return style, nil
	}
	return style, new(*p.Explode)
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

// sourcedParam pairs a parameter with its declaring list entry's pointer,
// because path-item parameters merge into every operation on the path item and
// so cannot borrow the operation's index space (issue #36). A $ref entry's own
// declaration lies one hop further on, resolved in lowerParameters.
type sourcedParam struct {
	ref     *soa.ReferencedParameter
	pointer jsontext.Pointer
	// in is the context a path-item entry is lowered in, its layer's (see
	// itemParams), whose document its pointer is in, or nil for an
	// operation's own entry, which is lowered in the operation's.
	in *service.Ctx
}

// mergeParameters merges a mount's path-item parameters with an operation's own
// using use-site precedence: an operation parameter with the same (name, in)
// overrides the path-item one; unshadowed path-item parameters follow in source
// order. Each entry keeps the pointer of its own declaration site rather than a
// position recomputed from the merged slice (issue #36), and a $ref'd path
// item's are under its own declaration pointer, not the path it is mounted at
// (issue #107).
func mergeParameters(b *typed.Bindings, pathParams []sourcedParam, opParams []*soa.ReferencedParameter,
	opDeclPtr jsontext.Pointer,
) []sourcedParam {
	merged := make([]sourcedParam, 0, len(opParams)+len(pathParams))
	merged = appendSourced(b, merged, opParams, opDeclPtr, nil, nil)
	if len(pathParams) == 0 {
		return merged
	}
	shadowed := shadowedKeys(b, opParams)
	for _, p := range pathParams {
		if key, ok := paramKey(b, p.ref); ok && shadowed[key] {
			continue
		}
		merged = append(merged, p)
	}
	return merged
}

// appendSourced appends params to dst, pairing each with the pointer of its own
// declaration position under base and the context in it is lowered in, and
// skipping any whose (name, in) key is in shadowed. A nil shadowed map reads as
// all-false, which is what the operation side passes: operation parameters are
// never shadowed, and are lowered in the operation's own context (a nil in).
func appendSourced(b *typed.Bindings, dst []sourcedParam, params []*soa.ReferencedParameter, base jsontext.Pointer,
	shadowed map[ids.ParamSlot]bool, in *service.Ctx,
) []sourcedParam {
	for i, p := range params {
		if key, ok := paramKey(b, p); ok && shadowed[key] {
			continue
		}
		dst = append(dst, sourcedParam{ref: p, pointer: base + ids.Ptr("parameters", strconv.Itoa(i)), in: in})
	}
	return dst
}

// shadowedKeys returns the (in, name) identities declared by opParams, so a
// path-item parameter sharing one is left out of the merge as a duplicate.
func shadowedKeys(b *typed.Bindings, opParams []*soa.ReferencedParameter) map[ids.ParamSlot]bool {
	shadowed := make(map[ids.ParamSlot]bool, len(opParams))
	for _, p := range opParams {
		if key, ok := paramKey(b, p); ok {
			shadowed[key] = true
		}
	}
	return shadowed
}

// paramKey builds the identity a parameter is merged by (ids.SlotOf), read
// through the bindings b, as the lowering reads it.
func paramKey(b *typed.Bindings, rp *soa.ReferencedParameter) (ids.ParamSlot, bool) {
	p := componentlookup.Object[soa.Parameter](b, rp)
	if p == nil {
		return ids.ParamSlot{}, false
	}
	return ids.SlotOf(p.GetName(), string(p.GetIn())), true
}

// paramLedger numbers the parameters of one operation, or of a path item
// mounting none, in list order, and finds each repeat by its slot
// (ids.SlotOf). A repeat is numbered among those spelled alike, as an ID is
// derived from the name as written (ids.Param), so X-Trace and x-trace keep
// the IDs each has alone, and deleting one renames neither.
//
// A repeat in any list, in whichever document writes it, is load's to report,
// once, where it is written (load.listRepeats).
type paramLedger struct {
	mount jsontext.Pointer
	seen  map[paramSpelling]int
}

// paramSpelling is a parameter's name and location as written, which its ID
// is derived from.
type paramSpelling struct {
	name, in string
}

// newParamLedger returns a ledger for the operation mounted at mount.
func newParamLedger(mount jsontext.Pointer) *paramLedger {
	return &paramLedger{mount: mount, seen: map[paramSpelling]int{}}
}

// next returns the ID of the parameter holding name in in.
func (l *paramLedger) next(name, in string) ir.ParameterID {
	return ids.Param(l.mount, name, in, l.count(name, in))
}

// count records one more holder of name in in, and returns which holder of
// that spelling it is, counting from one.
func (l *paramLedger) count(name, in string) int {
	spelled := paramSpelling{name: name, in: in}
	l.seen[spelled]++
	return l.seen[spelled]
}
