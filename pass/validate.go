package pass

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"

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
	diags = append(diags, checkNilTypes(doc)...)
	diags = append(diags, checkDanglingRefs(doc)...)
	diags = append(diags, checkServerIndices(doc)...)
	diags = append(diags, checkResponseIndices(doc)...)
	diags = append(diags, checkDiscriminators(doc)...)
	diags = append(diags, checkDuplicateWireNames(doc)...)
	diags = append(diags, checkParamBindings(doc)...)
	diags = append(diags, checkOneWay(doc)...)
	diags = append(diags, checkArgsOutsideGraphQL(doc)...)
	return diags
}

// checkDanglingRefs reports every typed-ID reference in the document that
// resolves to no entry in the registry that declares that class of ID — a
// TypeRef.Target into doc.Types, a MessageBinding.Channel into doc.Channels, a
// SchemeUse.Scheme into doc.Auth — wherever it sits.
//
// The sites and the registries both come from reflection over the document's own
// shape (refs.go) rather than a list of field names: referential integrity is the
// guarantee an emitter relies on, and a hand-written enumeration drifts behind
// the IR without anything failing.
func checkDanglingRefs(doc *ir.Document) []ir.Diagnostic {
	regs := documentRegistries(doc)
	sites, truncated := collectRefs(doc, "doc", regs.isRef)
	var diags []ir.Diagnostic
	if truncated {
		diags = append(diags, diag(ir.SeverityError, "ir/walk-truncated",
			"document nests deeper than the bounded reference walk; some references went unchecked",
			"doc"))
	}
	for _, s := range sites {
		if regs.resolves(s) {
			continue
		}
		noun := refNoun(s.idType)
		diags = append(diags, diag(ir.SeverityError, "ir/dangling-"+noun+"-ref",
			fmt.Sprintf("%s reference %q at %s resolves to no %s in the registry", noun, s.id, s.where, noun),
			s.where))
	}
	return diags
}

// checkServerIndices reports Service.Servers and Channel.Servers entries that
// address no entry of Document.Servers.
//
// These are references carried as integer indices, so nothing in their Go type
// marks them as references and the walk in refs.go cannot reach them — they are
// enumerated here instead. An emitter iterating them to render base URLs indexes
// out of range on a document that is otherwise referentially closed.
func checkServerIndices(doc *ir.Document) []ir.Diagnostic {
	declared := len(doc.Servers)
	var diags []ir.Diagnostic
	for _, svc := range doc.Services {
		diags = appendServerIndexDiags(diags, svc.Servers, declared, string(svc.ID))
	}
	for _, id := range sortedKeys(doc.Channels) {
		diags = appendServerIndexDiags(diags, doc.Channels[id].Servers, declared, string(id))
	}
	return diags
}

// appendServerIndexDiags appends to dst a diagnostic per entry of indices that
// addresses none of the declared servers; where locates the owning service or
// channel.
func appendServerIndexDiags(dst []ir.Diagnostic, indices []int, declared int, where string) []ir.Diagnostic {
	for i, index := range indices {
		if index >= 0 && index < declared {
			continue
		}
		at := fmt.Sprintf("%s/servers/%d", where, i)
		dst = append(dst, diag(ir.SeverityError, "ir/server-index-out-of-range",
			fmt.Sprintf("server index %d at %s addresses none of the %d declared servers", index, at, declared),
			at))
	}
	return dst
}

// checkResponseIndices reports HTTPBinding.SuccessStatus keys that address no
// entry of the operation's Responses. Its keys are indices into that slice, so
// they are invisible to the type-driven walk for the same reason server indices
// are, and are enumerated here for the same reason.
func checkResponseIndices(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	forEachOperation(doc, func(op ir.Operation) {
		for i, b := range op.Bindings.HTTP {
			where := fmt.Sprintf("%s/bindings/http/%d", op.ID, i)
			diags = appendSuccessStatusDiags(diags, b.SuccessStatus, len(op.Responses), where)
		}
	})
	return diags
}

// appendSuccessStatusDiags appends to dst a diagnostic per SuccessStatus key that
// addresses none of the declared responses, in ascending key order so map
// iteration cannot reach the output.
func appendSuccessStatusDiags(dst []ir.Diagnostic, status map[int]int, declared int, where string) []ir.Diagnostic {
	for _, index := range sortedKeys(status) {
		if index >= 0 && index < declared {
			continue
		}
		at := fmt.Sprintf("%s/successStatus/%d", where, index)
		dst = append(dst, diag(ir.SeverityError, "ir/response-index-out-of-range",
			fmt.Sprintf("response index %d at %s addresses none of the %d declared responses", index, at, declared),
			at))
	}
	return dst
}

// isNilTypeDef reports whether td is a nil TypeDef — an untyped nil interface or
// a typed nil pointer. A typed nil satisfies a type switch case, so matching a
// kind says nothing about whether the value is safe to dereference; every walk
// over doc.Types screens entries through this first.
func isNilTypeDef(td ir.TypeDef) bool {
	if td == nil {
		return true
	}
	rv := reflect.ValueOf(td)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// liveTypeIDs returns the registry's type IDs in sorted order, omitting entries
// that hold a nil type definition.
//
// Every check that iterates doc.Types dereferences the value it matched, and a
// typed nil satisfies both a type-switch case and a comma-ok assertion — so the
// match itself is no evidence the value is safe to read. Screening at each call
// site instead would leave the next check added here to rediscover the crash;
// checkNilTypes reports whatever this omits.
func liveTypeIDs(doc *ir.Document) []ir.TypeID {
	ids := sortedKeys(doc.Types)
	live := make([]ir.TypeID, 0, len(ids))
	for _, id := range ids {
		if !isNilTypeDef(doc.Types[id]) {
			live = append(live, id)
		}
	}
	return live
}

// checkNilTypes reports registry entries holding a nil type definition.
//
// Without it a malformed registry reads as internally consistent: the reference
// walk tolerates nil entries and the remaining checks match no case for most
// kinds, so Validate returned no diagnostics at all for a document no emitter
// can consume — and dereferenced the four kinds it did match.
func checkNilTypes(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, id := range sortedKeys(doc.Types) {
		if !isNilTypeDef(doc.Types[id]) {
			continue
		}
		diags = append(diags, diag(ir.SeverityError, "ir/nil-type",
			fmt.Sprintf("types registry entry %q holds a nil type definition", id),
			"types["+string(id)+"]"))
	}
	return diags
}

// checkDiscriminators reports discriminator mappings whose target either does
// not exist or is not a legal variant/subtype.
func checkDiscriminators(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, id := range liveTypeIDs(doc) {
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
//
// A target that resolves nowhere is reported here and again by checkDanglingRefs,
// and that double report is deliberate: the two codes make different claims.
// ir/dangling-type-ref says the document is not referentially closed, which every
// reference in it is held to; pass/discriminator-missing-variant says this
// discriminator cannot route that wire value, which is what an emitter building a
// polymorphic decoder subscribes to. Neither consumer should have to subscribe to
// the other's code to get its own answer.
func checkMapping(doc *ir.Document, d *ir.Discriminator, where string, member func(ir.TypeID) bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, key := range sortedKeys(d.Mapping) {
		target := d.Mapping[key]
		legal, why := mappingTarget(doc, target, member)
		if legal {
			continue
		}
		diags = append(diags, diag(ir.SeverityError, "pass/discriminator-missing-variant",
			fmt.Sprintf("discriminator mapping %q on %s references %q, which %s", key, where, target, why),
			where))
	}
	return diags
}

// mappingTarget reports whether target is a legal mapping target and, when it is
// not, the clause naming why. The two failures read differently on purpose: a
// target no type declares is a broken reference, while a declared one that fails
// member is a well-formed reference to the wrong type, and the reader should not
// have to cross-check the registry to tell them apart.
func mappingTarget(doc *ir.Document, target ir.TypeID, member func(ir.TypeID) bool) (legal bool, why string) {
	if _, declared := doc.Types[target]; !declared {
		return false, "no type in the document declares"
	}
	if !member(target) {
		return false, "is not a variant of it"
	}
	return true, ""
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
	for _, id := range liveTypeIDs(doc) {
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
	for _, id := range liveTypeIDs(doc) {
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
		if op.Bindings.GraphQL != nil {
			queue = appendTypeIDs(queue, op)
		}
	})
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		if td, ok := doc.Types[id]; ok {
			queue = appendTypeIDs(queue, td)
		}
	}
	return seen
}

// appendTypeIDs appends every TypeID reachable from root to dst. Truncation is
// dropped rather than reported: this feeds reachability, where a truncated walk
// can only under-report, and checkDanglingRefs already reports the same
// truncation over the whole document.
func appendTypeIDs(dst []ir.TypeID, root any) []ir.TypeID {
	sites, _ := collectTypeIDs(root, "")
	for _, s := range sites {
		dst = append(dst, ir.TypeID(s.id))
	}
	return dst
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
// IR-space location — a stable ID, or a path through the document's own fields
// as the reference walk spells it — never a position inside a source file, so
// Source is ir.NoSource to stop renderers fabricating a file location for it.
func diag(sev ir.Severity, code, message, pointer string) ir.Diagnostic {
	return ir.NewDiagnostic(sev, code, message, ir.Provenance{Source: ir.NoSource, Pointer: pointer})
}

// sortedKeys returns the keys of a map in ascending order, giving every check
// deterministic diagnostic ordering. Keys are IDs or slice indices, so ordering
// them by value orders the diagnostics by the node they name.
func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
