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
	diags = append(diags, checkGroupWalkTruncated(doc)...)
	diags = append(diags, checkServerIndices(doc)...)
	diags = append(diags, checkResponseIndices(doc)...)
	diags = append(diags, checkEncodingKeys(doc)...)
	diags = append(diags, checkPropIDRefs(doc)...)
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

// checkPropIDRefs reports every PropID a document carries that names no property
// it declares.
//
// Retyping Content.Encoding made one carrier of a PropID honest about its shape,
// and checkEncodingKeys resolves those keys; every other carrier was resolved by
// nothing. PropPath.Segments, ParamPath.Segments, HTTPParamBinding.ParamPath and
// Discriminator.Property are all invisible to checkDanglingRefs, because that
// walk is registry-driven and a PropID addresses no registry — a property is a
// position inside its model, so resolving one means finding the model that owns
// it. Nothing produces a bad one today, since each is derived from the pointer
// that interned the thing it names; what was missing is that a bad one would have
// gone unreported.
//
// The claim is membership: the ID names a property the document declares
// somewhere. Reflection supplies the sites but not the root each path is meant to
// be walked from, and a root read off a field name would be the hand-maintained
// enumeration this avoids. checkDiscriminators makes the tighter, model-scoped
// claim for the one carrier whose root is written down.
//
// The code carries the ir/ namespace for the reason checkEncodingKeys does: it
// names the defect rather than the finder, so a second checker growing this check
// adopts the code instead of forcing a rename. Only this pass reports it today —
// irverify's walk is registry-driven and cannot reach the class at all.
//
// One member of the same class is deliberately left unchecked. Docs.Description
// is CommonMark that may carry {t:TypeID} cross-reference tokens for emitters to
// resolve (ir/docs.go, normative at ir-design §12), so a description naming a type
// that does not exist is a broken reference inside a plain string, invisible here
// for the same reason a PropID was. Reaching it needs a token parser rather than a
// lookup, and a false positive inside prose is noisier than a missing check.
func checkPropIDRefs(doc *ir.Document) []ir.Diagnostic {
	sites, declared := collectPropIDs(doc, "doc")
	var diags []ir.Diagnostic
	for _, s := range sites {
		if declared[ir.PropID(s.id)] {
			continue
		}
		diags = append(diags, diag(ir.SeverityError, "ir/dangling-prop-ref",
			fmt.Sprintf("prop reference %q at %s resolves to no property declared in the document", s.id, s.where),
			s.where))
	}
	return diags
}

// checkEncodingKeys reports Content.Encoding keys that name no property of the
// model the content's Type addresses.
//
// A key is a PropID, and a property is a position inside its model rather than an
// entry in a document-level registry, so the type-driven walk has nothing to
// resolve one against — the keys are enumerated here for the same reason the
// indices above are. An emitter rendering multipart parts looks the key up among
// the body's properties and silently renders no part for a key that misses.
//
// The code carries the ir/ namespace because it names the defect, not the finder
// (see the package doc): a key addressing nothing is a broken reference, so a
// second checker growing this check adopts the code rather than forcing a rename.
// Only this pass reports it today.
//
// The fields that carry a Payload are named here — Operation.Request,
// Response.Payload and Message.Payload — so a new one has to be added by hand.
func checkEncodingKeys(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	forEachOperation(doc, func(op ir.Operation) {
		diags = appendEncodingKeyDiags(diags, doc, op.Request, string(op.ID)+"/request")
		for i, r := range op.Responses {
			at := fmt.Sprintf("%s/responses/%d", op.ID, i)
			diags = appendEncodingKeyDiags(diags, doc, r.Payload, at)
		}
	})
	for _, id := range sortedKeys(doc.Messages) {
		msg := doc.Messages[id]
		diags = appendEncodingKeyDiags(diags, doc, &msg.Payload, string(id))
	}
	return diags
}

// appendEncodingKeyDiags appends to dst a diagnostic per unresolvable encoding
// key in each of the payload's contents; where locates the payload's owner.
func appendEncodingKeyDiags(dst []ir.Diagnostic, doc *ir.Document, payload *ir.Payload, where string) []ir.Diagnostic {
	if payload == nil {
		return dst
	}
	for i, c := range payload.Contents {
		if len(c.Encoding) == 0 {
			continue
		}
		at := fmt.Sprintf("%s/contents/%d", where, i)
		dst = appendUnknownPartDiags(dst, c, exposedProps(doc, c.Type.Target), at)
	}
	return dst
}

// appendUnknownPartDiags appends to dst a diagnostic per encoding key of one
// content that names no property in parts, in ascending key order so map
// iteration cannot reach the output.
func appendUnknownPartDiags(dst []ir.Diagnostic, c ir.Content, parts map[ir.PropID]bool, where string) []ir.Diagnostic {
	for _, key := range sortedKeys(c.Encoding) {
		if parts[key] {
			continue
		}
		at := where + "/encoding/" + string(key)
		dst = append(dst, diag(ir.SeverityError, "ir/encoding-key-unknown-property",
			fmt.Sprintf("encoding key %q at %s addresses no property of the content's type %q",
				key, at, c.Type.Target), at))
	}
	return dst
}

// exposedProps returns the property IDs a type exposes: its own, plus the ones it
// composes in (§4.3), which is the flat set an emitter renders. A root naming no
// model — one the registry does not declare, or the empty target — exposes none,
// so every reference against such a root is reported beside whatever
// checkDanglingRefs says about the root itself: the two make different claims, as
// with checkMapping.
//
// It answers the two checks with a root written down — a content's multipart
// parts and a model discriminator's tag property. A body typed by an alias scalar
// exposes the parts of the model its Base names; a $ref carrying siblings hoists
// exactly that shape, so it is how an ordinary referenced multipart body arrives
// here.
//
// The walk is iterative with a visited set, so a cyclic composition or alias
// chain terminates and the finite registry bounds it.
func exposedProps(doc *ir.Document, root ir.TypeID) map[ir.PropID]bool {
	props := map[ir.PropID]bool{}
	seen := map[ir.TypeID]bool{}
	queue := []ir.TypeID{root}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		td := doc.Types[id]
		if isNilTypeDef(td) {
			continue // undeclared, or the typed nil checkNilTypes reports
		}
		queue = appendPartSources(queue, props, td)
	}
	return props
}

// appendPartSources records td's own part properties in props and appends the
// nodes the rest come from: a model's composition parents, an alias scalar's
// base. No other kind carries parts or says where to find them.
func appendPartSources(dst []ir.TypeID, props map[ir.PropID]bool, td ir.TypeDef) []ir.TypeID {
	switch t := td.(type) {
	case *ir.Model:
		for _, p := range t.Properties {
			props[p.ID] = true
		}
		return appendCompositionParents(dst, t)
	case *ir.Scalar:
		if t.Base == nil {
			return dst // an opaque scalar stands for nothing.
		}
		return append(dst, t.Base.Target)
	default:
		return dst // no other kind exposes parts.
	}
}

// appendCompositionParents appends m's base, interfaces and mixins to dst. All
// three contribute to the flat property set an emitter computes (§4.3), so a part
// inherited through any of them is a legal encoding key.
func appendCompositionParents(dst []ir.TypeID, m *ir.Model) []ir.TypeID {
	if m.Base != nil {
		dst = append(dst, m.Base.Target)
	}
	for _, r := range m.Implements {
		dst = append(dst, r.Target)
	}
	for _, r := range m.Mixins {
		dst = append(dst, r.Target)
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
// of the discriminated base model, and the tag property to be one this model
// exposes.
func checkModelDiscriminator(doc *ir.Document, m *ir.Model) []ir.Diagnostic {
	if m.Discriminator == nil {
		return nil
	}
	diags := checkDiscriminatorProperty(doc, m)
	return append(diags, checkMapping(doc, m.Discriminator, string(m.ID), func(target ir.TypeID) bool {
		return isSubtype(doc, target, m.ID)
	})...)
}

// checkDiscriminatorProperty requires a model discriminator's tag property to be
// one the model exposes — its own, or one it composes in.
//
// checkPropIDRefs already holds every PropID to naming a property the document
// declares somewhere. This is the tighter claim available where the root is
// written down: a tag on a property of some unrelated model routes nothing, and
// an emitter building a decoder reads the tag off *this* model's instance.
func checkDiscriminatorProperty(doc *ir.Document, m *ir.Model) []ir.Diagnostic {
	prop := m.Discriminator.Property
	if prop == "" || exposedProps(doc, m.ID)[prop] {
		return nil
	}
	where := string(m.ID)
	return []ir.Diagnostic{diag(ir.SeverityError, "pass/discriminator-unknown-property",
		fmt.Sprintf("discriminator on %s tags property %q, which the model neither declares nor composes in", where, prop),
		where)}
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

// checkGroupWalkTruncated reports that operation-group nesting exceeded the
// bounded walk.
//
// The bound itself is deliberate, but an unreported bound makes every check that
// reaches operations through it describe a subset of the document while Validate
// still returns "internally consistent". The sibling reference walk reports its
// own cap the same way.
func checkGroupWalkTruncated(doc *ir.Document) []ir.Diagnostic {
	if !forEachOperation(doc, func(ir.Operation) {}) {
		return nil
	}
	return []ir.Diagnostic{diag(ir.SeverityError, "ir/walk-truncated",
		fmt.Sprintf("operation groups nest deeper than %d; some operations went unchecked", maxGroupDepth),
		"doc")}
}

// checkArgsOutsideGraphQL reports field arguments on models that are not
// reachable from a GraphQL binding (the only scope in which Property.Args is
// legal). With no GraphQL compiler yet, any Args is a violation.
func checkArgsOutsideGraphQL(doc *ir.Document) []ir.Diagnostic {
	reachable, truncated := graphqlReachableTypes(doc)
	if truncated {
		// The walk that built the reachability set saw a subset of the document,
		// so "not reached" no longer means "not reachable" — a GraphQL binding
		// past the cap goes unseen and the types it legitimises look illegal.
		// Fail open; checkGroupWalkTruncated reports why nothing is claimed here.
		return nil
	}
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
// It reports whether the operation walk truncated, since a caller cannot tell an
// unreachable type from an unvisited one without it.
func graphqlReachableTypes(doc *ir.Document) (map[ir.TypeID]bool, bool) {
	seen := map[ir.TypeID]bool{}
	var queue []ir.TypeID
	truncated := forEachOperation(doc, func(op ir.Operation) {
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
	return seen, truncated
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
//
// It reports whether the walk stopped at the cap before reaching every
// operation, so a caller deciding something from what it saw can tell an absent
// operation from an unvisited one.
func forEachOperation(doc *ir.Document, fn func(ir.Operation)) bool {
	truncated := false
	for _, svc := range doc.Services {
		if forEachGroupOperation(svc.Groups, 0, fn) {
			truncated = true
		}
	}
	return truncated
}

// forEachGroupOperation walks a group tree, invoking fn per operation.
// forEachGroupOperation walks groups depth-first, reporting whether the bound
// cut the descent short.
func forEachGroupOperation(groups []ir.OperationGroup, depth int, fn func(ir.Operation)) bool {
	if depth > maxGroupDepth {
		// Only a non-empty slice is being skipped. Every leaf recurses once into
		// its own empty Groups, so reporting the cap unconditionally would claim
		// truncation for a walk that reached everything — at exactly the depth
		// where the last operation still fits.
		return len(groups) > 0
	}
	truncated := false
	for _, g := range groups {
		for _, op := range g.Operations {
			fn(op)
		}
		if forEachGroupOperation(g.Groups, depth+1, fn) {
			truncated = true
		}
	}
	return truncated
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
