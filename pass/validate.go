package pass

import (
	"fmt"
	"sort"

	"github.com/dexpace/morphic/ir"
)

// maxGroupDepth bounds recursion over nested operation groups; deeper nesting is
// pathological and is silently truncated rather than allowed to blow the stack.
const maxGroupDepth = 128

// Validate checks a Document for referential-integrity violations and returns
// the diagnostics it finds, most-structural first. It is pure: it never mutates
// doc and holds no package-level state. An empty result means the document is
// internally consistent for every rule this pass enforces.
func Validate(doc *ir.Document) []ir.Diagnostic {
	if doc == nil {
		return nil
	}
	diags := make([]ir.Diagnostic, 0, 8)
	diags = append(diags, checkDanglingTypeRefs(doc)...)
	diags = append(diags, checkDiscriminators(doc)...)
	diags = append(diags, checkDuplicateWireNames(doc)...)
	diags = append(diags, checkParamBindings(doc)...)
	diags = append(diags, checkOneWay(doc)...)
	diags = append(diags, checkArgsOutsideGraphQL(doc)...)
	diags = append(diags, checkAuthRefs(doc)...)
	return diags
}

// refVisitor receives every TypeRef target reached by the walkers, along with a
// human-readable location used for diagnostic provenance.
type refVisitor func(target ir.TypeID, where string)

// checkDanglingTypeRefs reports every TypeRef.Target that resolves to no entry
// in doc.Types.
func checkDanglingTypeRefs(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	visit := func(target ir.TypeID, where string) {
		if target == "" {
			return
		}
		if _, ok := doc.Types[target]; !ok {
			diags = append(diags, diag(ir.SeverityError, "ir/dangling-type-ref",
				fmt.Sprintf("type reference %q at %s resolves to no type in the registry", target, where),
				where))
		}
	}
	for _, id := range sortedKeys(doc.Types) {
		walkTypeDefRefs(doc.Types[id], visit)
	}
	forEachOperation(doc, func(op ir.Operation) { walkOperationRefs(op, visit) })
	for _, id := range sortedKeys(doc.Messages) {
		msg := doc.Messages[id]
		walkPayloadRefs(&msg.Payload, "message/"+string(id), visit)
	}
	return diags
}

// walkTypeDefRefs visits every TypeRef embedded in a single TypeDef node.
func walkTypeDefRefs(td ir.TypeDef, visit refVisitor) {
	where := string(td.Common().ID)
	switch t := td.(type) {
	case *ir.Model:
		walkModelRefs(t, where, visit)
	case *ir.Union:
		for i, v := range t.Variants {
			visit(v.Type.Target, fmt.Sprintf("%s/variants/%d", where, i))
		}
	case *ir.Scalar:
		if t.Base != nil {
			visit(t.Base.Target, where+"/base")
		}
		visitEncoding(t.Encoding, where, visit)
	case *ir.List:
		visit(t.Elem.Target, where+"/elem")
		visitEncoding(t.Encoding, where, visit)
	case *ir.MapT:
		visit(t.Key.Target, where+"/key")
		visit(t.Value.Target, where+"/value")
	case *ir.Tuple:
		for i, e := range t.Elems {
			visit(e.Target, fmt.Sprintf("%s/elems/%d", where, i))
		}
	default:
		// Enum, Literal, Primitive, External, and Any carry no TypeRefs, so
		// there is nothing to walk. A new ref-bearing TypeDef kind must add a
		// case above rather than fall through here silently.
	}
}

// walkModelRefs visits the TypeRefs owned by a Model: its own properties (and
// their encodings), single-inheritance base, interfaces, mixins, and catch-all.
func walkModelRefs(m *ir.Model, where string, visit refVisitor) {
	for i, p := range m.Properties {
		site := fmt.Sprintf("%s/properties/%d", where, i)
		visit(p.Type.Target, site)
		visitEncoding(p.Encoding, site, visit)
	}
	if m.Base != nil {
		visit(m.Base.Target, where+"/base")
	}
	for i, r := range m.Implements {
		visit(r.Target, fmt.Sprintf("%s/implements/%d", where, i))
	}
	for i, r := range m.Mixins {
		visit(r.Target, fmt.Sprintf("%s/mixins/%d", where, i))
	}
	walkAdditionalPropsRefs(m.AdditionalProps, where, visit)
}

// walkAdditionalPropsRefs visits the value/key/pattern schemas of a model's
// catch-all property map.
func walkAdditionalPropsRefs(ap *ir.AdditionalProps, where string, visit refVisitor) {
	if ap == nil {
		return
	}
	visit(ap.Value.Target, where+"/additionalProps/value")
	if ap.Key != nil {
		visit(ap.Key.Target, where+"/additionalProps/key")
	}
	for i, pp := range ap.Patterns {
		visit(pp.Value.Target, fmt.Sprintf("%s/additionalProps/patterns/%d", where, i))
	}
}

// visitEncoding visits an encoding's wire-type override, when present.
func visitEncoding(enc *ir.Encoding, where string, visit refVisitor) {
	if enc != nil && enc.WireType != nil {
		visit(enc.WireType.Target, where+"/encoding/wireType")
	}
}

// walkOperationRefs visits the TypeRefs on an Operation's params, request,
// responses (bodies and headers), and errors.
func walkOperationRefs(op ir.Operation, visit refVisitor) {
	where := string(op.ID)
	for i, p := range op.Params {
		visit(p.Type.Target, fmt.Sprintf("%s/params/%d", where, i))
	}
	if op.Request != nil {
		walkPayloadRefs(op.Request, where+"/request", visit)
	}
	for i, r := range op.Responses {
		if r.Payload != nil {
			walkPayloadRefs(r.Payload, fmt.Sprintf("%s/responses/%d", where, i), visit)
		}
		for j, h := range r.Headers {
			visit(h.Type.Target, fmt.Sprintf("%s/responses/%d/headers/%d", where, i, j))
		}
	}
	for i, e := range op.Errors {
		visit(e.Type.Target, fmt.Sprintf("%s/errors/%d", where, i))
	}
}

// walkPayloadRefs visits the content and per-item TypeRefs of a Payload.
func walkPayloadRefs(p *ir.Payload, where string, visit refVisitor) {
	for i, c := range p.Contents {
		visit(c.Type.Target, fmt.Sprintf("%s/contents/%d", where, i))
		if c.Item != nil {
			visit(c.Item.Target, fmt.Sprintf("%s/contents/%d/item", where, i))
		}
	}
}

// checkDiscriminators reports discriminator mappings whose target either does
// not exist or is not a legal variant/subtype.
func checkDiscriminators(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, id := range sortedKeys(doc.Types) {
		switch t := doc.Types[id].(type) {
		case *ir.Union:
			diags = append(diags, checkUnionDiscriminator(doc, t)...)
		case *ir.Model:
			diags = append(diags, checkModelDiscriminator(doc, t)...)
		default:
			// Only Union and Model can declare a discriminator; every other
			// TypeDef kind has nothing to check. A new discriminator-bearing
			// kind must add a case above rather than fall through here.
		}
	}
	return diags
}

// checkUnionDiscriminator requires each mapping target to be one of the union's
// variant targets.
func checkUnionDiscriminator(doc *ir.Document, u *ir.Union) []ir.Diagnostic {
	if u.Discriminator == nil {
		return nil
	}
	variants := make(map[ir.TypeID]bool, len(u.Variants))
	for _, v := range u.Variants {
		variants[v.Type.Target] = true
	}
	return checkMapping(doc, u.Discriminator, string(u.ID), func(target ir.TypeID) bool {
		return variants[target]
	})
}

// checkModelDiscriminator requires each mapping target to be a declared subtype
// of the discriminated base model.
func checkModelDiscriminator(doc *ir.Document, m *ir.Model) []ir.Diagnostic {
	if m.Discriminator == nil {
		return nil
	}
	return checkMapping(doc, m.Discriminator, string(m.ID), func(target ir.TypeID) bool {
		return isSubtype(doc, target, m.ID)
	})
}

// checkMapping validates a discriminator's wire-value mapping: every target must
// resolve in the registry and satisfy member.
func checkMapping(doc *ir.Document, d *ir.Discriminator, where string, member func(ir.TypeID) bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, key := range sortedKeys(d.Mapping) {
		target := d.Mapping[key]
		if _, ok := doc.Types[target]; !ok || !member(target) {
			diags = append(diags, diag(ir.SeverityError, "pass/discriminator-missing-variant",
				fmt.Sprintf("discriminator mapping %q on %s references %q, which is not a variant of it", key, where, target),
				where))
		}
	}
	return diags
}

// isSubtype reports whether target is a declared subtype of base via single
// inheritance or interface conformance.
func isSubtype(doc *ir.Document, target, base ir.TypeID) bool {
	sub, ok := doc.Types[target].(*ir.Model)
	if !ok {
		return false
	}
	if sub.Base != nil && sub.Base.Target == base {
		return true
	}
	for _, r := range sub.Implements {
		if r.Target == base {
			return true
		}
	}
	return false
}

// checkDuplicateWireNames reports wire-name collisions within a single model's
// own properties.
func checkDuplicateWireNames(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, id := range sortedKeys(doc.Types) {
		m, ok := doc.Types[id].(*ir.Model)
		if !ok {
			continue
		}
		seen := make(map[string]bool, len(m.Properties))
		for _, p := range m.Properties {
			name := effectiveWireName(p)
			if name == "" {
				continue
			}
			if seen[name] {
				diags = append(diags, diag(ir.SeverityError, "pass/duplicate-wire-name",
					fmt.Sprintf("model %s has more than one property with wire name %q", id, name),
					string(id)))
				continue
			}
			seen[name] = true
		}
	}
	return diags
}

// effectiveWireName is the on-wire name of a property: its explicit WireName, or
// its source name when no override is set.
func effectiveWireName(p ir.Property) string {
	if p.WireName != "" {
		return p.WireName
	}
	return p.Name.Source
}

// checkParamBindings validates each HTTP binding's parameter bindings against
// the operation's logical parameters.
func checkParamBindings(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	forEachOperation(doc, func(op ir.Operation) {
		for i := range op.Bindings.HTTP {
			diags = append(diags, checkHTTPParamBinding(op, i)...)
		}
	})
	return diags
}

// checkHTTPParamBinding validates one HTTP binding: unknown parameter names are
// errors, a param bound twice in one non-host location is an error, and an
// unbound parameter is a warning (body-carried operations bind nothing).
func checkHTTPParamBinding(op ir.Operation, idx int) []ir.Diagnostic {
	b := op.Bindings.HTTP[idx]
	where := fmt.Sprintf("%s/bindings/http/%d", op.ID, idx)
	known := make(map[string]bool, len(op.Params))
	for _, p := range op.Params {
		known[p.Name.Source] = true
	}
	var diags []ir.Diagnostic
	bound := make(map[string]int, len(op.Params))
	perLocation := make(map[string]int, len(b.ParamBindings))
	for _, pb := range b.ParamBindings {
		if !known[pb.Param] {
			diags = append(diags, diag(ir.SeverityError, "pass/param-binding-mismatch",
				fmt.Sprintf("binding on %s names unknown parameter %q", where, pb.Param), where))
			continue
		}
		bound[pb.Param]++
		if pb.Location == ir.HTTPLocationHost {
			continue // host labels are additive; a param may fill several.
		}
		key := pb.Param + "\x00" + string(pb.Location)
		if perLocation[key]++; perLocation[key] == 2 {
			diags = append(diags, diag(ir.SeverityError, "pass/param-binding-mismatch",
				fmt.Sprintf("parameter %q is bound more than once in location %q on %s", pb.Param, pb.Location, where),
				where))
		}
	}
	return append(diags, unboundParamWarnings(op, bound, where)...)
}

// unboundParamWarnings reports each logical parameter that no binding placed on
// the wire.
func unboundParamWarnings(op ir.Operation, bound map[string]int, where string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, p := range op.Params {
		if bound[p.Name.Source] == 0 {
			diags = append(diags, diag(ir.SeverityWarning, "pass/param-binding-mismatch",
				fmt.Sprintf("parameter %q on %s is not bound to any HTTP location", p.Name.Source, where),
				where))
		}
	}
	return diags
}

// checkOneWay reports one-way operations that nonetheless declare responses.
func checkOneWay(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	forEachOperation(doc, func(op ir.Operation) {
		if op.OneWay && len(op.Responses) > 0 {
			diags = append(diags, diag(ir.SeverityError, "pass/oneway-with-responses",
				fmt.Sprintf("operation %s is one-way but declares %d response(s)", op.ID, len(op.Responses)),
				string(op.ID)))
		}
	})
	return diags
}

// checkArgsOutsideGraphQL reports field arguments on models that are not
// reachable from a GraphQL binding (the only scope in which Property.Args is
// legal). With no GraphQL compiler yet, any Args is a violation.
func checkArgsOutsideGraphQL(doc *ir.Document) []ir.Diagnostic {
	reachable := graphqlReachableTypes(doc)
	var diags []ir.Diagnostic
	for _, id := range sortedKeys(doc.Types) {
		m, ok := doc.Types[id].(*ir.Model)
		if !ok || reachable[id] {
			continue
		}
		for i, p := range m.Properties {
			if len(p.Args) == 0 {
				continue
			}
			diags = append(diags, diag(ir.SeverityError, "pass/args-outside-graphql",
				fmt.Sprintf("property %s/properties/%d declares field arguments outside a GraphQL binding", id, i),
				string(id)))
		}
	}
	return diags
}

// graphqlReachableTypes returns the set of type IDs transitively reachable from
// the operations that carry a GraphQL binding. The traversal is iterative with a
// visited set, so it terminates on cyclic type graphs.
func graphqlReachableTypes(doc *ir.Document) map[ir.TypeID]bool {
	seen := map[ir.TypeID]bool{}
	var queue []ir.TypeID
	forEachOperation(doc, func(op ir.Operation) {
		if op.Bindings.GraphQL == nil {
			return
		}
		walkOperationRefs(op, func(t ir.TypeID, _ string) { queue = append(queue, t) })
	})
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		if td, ok := doc.Types[id]; ok {
			walkTypeDefRefs(td, func(t ir.TypeID, _ string) { queue = append(queue, t) })
		}
	}
	return seen
}

// checkAuthRefs reports auth requirements whose scheme does not resolve in
// doc.Auth, across service defaults and per-operation overrides.
func checkAuthRefs(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	check := func(reqs []ir.AuthRequirement, where string) {
		for _, req := range reqs {
			for _, use := range req.Schemes {
				if use.Scheme == "" {
					continue
				}
				if _, ok := doc.Auth[use.Scheme]; !ok {
					diags = append(diags, diag(ir.SeverityError, "pass/dangling-auth-ref",
						fmt.Sprintf("auth requirement on %s references unknown scheme %q", where, use.Scheme),
						where))
				}
			}
		}
	}
	for _, svc := range doc.Services {
		check(svc.Auth, string(svc.ID))
	}
	forEachOperation(doc, func(op ir.Operation) { check(op.Auth, string(op.ID)) })
	return diags
}

// forEachOperation invokes fn for every operation in the document, descending
// nested operation groups up to maxGroupDepth.
func forEachOperation(doc *ir.Document, fn func(ir.Operation)) {
	for _, svc := range doc.Services {
		forEachGroupOperation(svc.Groups, 0, fn)
	}
}

// forEachGroupOperation walks a group tree, invoking fn per operation.
func forEachGroupOperation(groups []ir.OperationGroup, depth int, fn func(ir.Operation)) {
	if depth > maxGroupDepth {
		return
	}
	for _, g := range groups {
		for _, op := range g.Operations {
			fn(op)
		}
		forEachGroupOperation(g.Groups, depth+1, fn)
	}
}

// diag builds a Diagnostic with a location-only provenance. The pointer is an
// IR-space id (a TypeID/OpID/ServiceID), not a location inside any source file,
// so Source is -1 to mark "no source file" and stop renderers fabricating a
// file location for it.
func diag(sev ir.Severity, code, message, pointer string) ir.Diagnostic {
	return ir.Diagnostic{
		Severity:   sev,
		Code:       code,
		Message:    message,
		Provenance: ir.Provenance{Source: -1, Pointer: pointer},
	}
}

// sortedKeys returns the keys of a string-keyed map in ascending order, giving
// every check deterministic diagnostic ordering.
func sortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
