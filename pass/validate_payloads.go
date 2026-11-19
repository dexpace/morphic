// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// checkEncodingKeys reports Content.Encoding keys that name no property of the
// model the content's Type addresses.
//
// A key is a PropertyID, which no document-level registry resolves, so the
// type-driven walk has nothing to resolve it against and the keys are
// enumerated here. An emitter rendering multipart parts looks the key up among
// the body's properties and silently renders no part for a miss.
//
// The code carries the ir/ namespace because it names the defect, not the
// finder (see the package doc), so a second checker growing this check adopts
// it. Only this pass reports it today. facts indexes doc.
func checkEncodingKeys(doc *ir.Document, facts *irfacts.DocumentFacts) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var keys []ir.PropertyID // every content's keys are sorted in this one list
	forEachPayload(doc, func(site payloadSite) {
		diags = appendEncodingKeyDiags(diags, doc.Types, facts, site, &keys)
	})
	return diags
}

// checkCatchAllEncodingKeys reports what a content's CatchAllEncoding states of
// a catch-all its body has not (docs/ir/payloads.md#a-catch-alls-parts): a key
// naming no holder among the body's shapes, a Value for a holder with none, or
// a Patterns key its holder does not list, each ir/encoding-key-unknown-catch-all.
// The shapes are the content's type and, through its unions, each variant,
// each read by ir.CatchAllHolders, as the compiler keys them. An emitter looking
// a key up finds no catch-all and renders its parts with no encoding. An empty
// key is checkEmptyRefs's and one naming no type checkDanglingRefs', so each
// defect draws one code.
func checkCatchAllEncodingKeys(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var keys []ir.TypeID // every content's keys are sorted in this one list
	forEachPayload(doc, func(site payloadSite) {
		for i, content := range site.contents {
			if len(content.CatchAllEncoding) > 0 {
				at := site.where() + "/contents/" + strconv.Itoa(i) + "/catchAllEncoding/"
				diags = appendCatchAllDiags(diags, doc.Types, content, at, &keys)
			}
		}
	})
	return diags
}

// appendCatchAllDiags appends to dst a diagnostic per statement of content's
// CatchAllEncoding, at at and its key, that the holders of content's body refute,
// asking the keys in ascending order, sorted in keys.
func appendCatchAllDiags(dst []ir.Diagnostic, types ir.TypeRegistry, content ir.Content, at string, keys *[]ir.TypeID,
) []ir.Diagnostic {
	holders := map[ir.TypeID]bool{}
	anyHolder(types, content.Type.Target, func(typeID ir.TypeID) bool {
		for _, holder := range ir.CatchAllHolders(types, typeID) {
			holders[holder] = true
		}
		return false
	})
	*keys = slices.AppendSeq((*keys)[:0], maps.Keys(content.CatchAllEncoding))
	slices.Sort(*keys)
	for _, key := range *keys {
		if _, declared := types[key]; !declared {
			continue // empty or dangling, which the reference checks report
		}
		if !holders[key] {
			dst = append(dst, unknownCatchAll(at+string(key),
				fmt.Sprintf("names %q, which holds no catch-all of the content's type %q", key, content.Type.Target)))
			continue
		}
		dst = appendCatchAllPartDiags(dst, types[key], content.CatchAllEncoding[key], at+string(key))
	}
	return dst
}

// appendCatchAllPartDiags appends to dst a diagnostic for a Value parts states
// where holder, at at's key, has none, and for each Patterns key, in ascending
// order, holder does not list. ir.CatchAllHolders gives a map, which has a
// value and no pattern, or a model holding AdditionalProperties.
func appendCatchAllPartDiags(dst []ir.Diagnostic, holder ir.TypeDef, parts ir.CatchAllParts, at string) []ir.Diagnostic {
	value, listed := true, []string(nil)
	if model, ok := holder.(*ir.Model); ok {
		value = model.AdditionalProperties.Value != nil
		for _, patternProperties := range model.AdditionalProperties.Patterns {
			listed = append(listed, patternProperties.Pattern)
		}
	}
	if parts.Value != nil && !value {
		dst = append(dst, unknownCatchAll(at+"/value", "states a value for a catch-all that admits none"))
	}
	for _, pattern := range slices.Sorted(maps.Keys(parts.Patterns)) {
		if !slices.Contains(listed, pattern) {
			dst = append(dst, unknownCatchAll(at+"/patterns/"+pattern,
				fmt.Sprintf("states pattern %q, which the catch-all does not list", pattern)))
		}
	}
	return dst
}

// unknownCatchAll is the finding that the catch-all encoding entry at at
// states what its holder does not hold, as what says.
func unknownCatchAll(at, what string) ir.Diagnostic {
	return diag(ir.SeverityError, "ir/encoding-key-unknown-catch-all",
		fmt.Sprintf("catch-all encoding at %s %s", at, what), at)
}

// checkPayloadRequired reports a Payload.Required set anywhere but on a request.
//
// Only a request body can be omitted, so ir.Payload defines the field for that
// one position and leaves it nil on a response or message payload. Set there it
// states something no exchange can honour, and an emitter rendering "required"
// off the boolean prints it on a response (GitHub #421). No compiler produces
// the shape today; this guards the rule, and is an error because the document
// is wrong, not merely lossy.
//
// The ir/ code namespace is explained at checkEncodingKeys; only this pass
// reports it today.
func checkPayloadRequired(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	forEachPayload(doc, func(site payloadSite) {
		if site.request || site.required == nil {
			return
		}
		where := site.where()
		diags = append(diags, diag(ir.SeverityError, "ir/payload-required-outside-request",
			fmt.Sprintf("payload at %s sets required, which only a request body can state", where),
			where))
	})
	return diags
}

// payloadSite is one Payload a document carries: what the checks read of it,
// where it hangs, and whether that position is a request, the one place
// Payload.Required is defined. Where it hangs is kept as its parts, the ID of
// the node holding it, the field it hangs from and its index there, or -1 for
// none, and spelled only for a diagnostic.
type payloadSite struct {
	contents []ir.Content
	required *bool
	owner    string
	field    string
	index    int
	request  bool
}

// where spells where the payload hangs: its owner's ID, then its field and its
// index there, each after a "/".
func (s payloadSite) where() string {
	where := s.owner
	if s.field != "" {
		where += "/" + s.field
	}
	if s.index >= 0 {
		where += "/" + strconv.Itoa(s.index)
	}
	return where
}

// forEachPayload calls fn once per Payload the document carries, skipping the
// positions that hold none.
//
// The carrying fields are named here, since nothing in a Payload's Go type says
// who owns one, so a new one has to be added by hand: Operation.Request,
// Response.Payload, ErrorCase.Payload and Message.Payload. A new carrier fails
// TestEncodingCarriers_NameEveryPayloadFieldInTheIR until encodingCarriers
// names it, and TestValidate_EncodingKeyAddressesNoProperty until it is walked
// here. Response.Payload is reached at both its positions, an operation's
// Responses and its Default, and ErrorCase.Payload at both of its, an
// operation's Errors and its service's CommonErrors, since a walk of one would
// leave the other unjudged.
func forEachPayload(doc *ir.Document, fn func(payloadSite)) {
	for _, svc := range doc.Services {
		forEachErrorPayload(svc.CommonErrors, string(svc.ID), "commonErrors", fn)
	}
	ir.ForEachOperation(doc, func(op ir.Operation) {
		owner := string(op.ID)
		if op.Request != nil {
			fn(siteOf(*op.Request, owner, "request", -1, true))
		}
		for i, response := range op.Responses {
			if response.Payload != nil {
				fn(siteOf(*response.Payload, owner, "responses", i, false))
			}
		}
		forEachErrorPayload(op.Errors, owner, "errors", fn)
		if op.Default != nil && op.Default.Payload != nil {
			fn(siteOf(*op.Default.Payload, owner, "default", -1, false))
		}
	})
	for _, id := range slices.Sorted(maps.Keys(doc.Messages)) {
		fn(siteOf(doc.Messages[id].Payload, string(id), "", -1, false))
	}
}

// forEachErrorPayload calls fn once per error case carrying a payload; owner
// and field locate the list the cases hang from.
func forEachErrorPayload(errs []ir.ErrorCase, owner, field string, fn func(payloadSite)) {
	for i, ec := range errs {
		if ec.Payload != nil {
			fn(siteOf(*ec.Payload, owner, field, i, false))
		}
	}
}

// siteOf is the payloadSite of payload, hanging from field of owner at index, or at
// none for -1, request saying whether that position is a request.
func siteOf(payload ir.Payload, owner, field string, index int, request bool) payloadSite {
	return payloadSite{contents: payload.Contents, required: payload.Required, owner: owner, field: field, index: index, request: request}
}

// appendEncodingKeyDiags appends to dst a diagnostic per unresolvable encoding
// key in each of the contents of the payload at site, sorting each content's
// keys in keys.
//
// A content whose type no entry declares exposes no part, so each of its keys
// is reported beside checkDanglingRefs's finding on the type. The claims
// differ, as with checkMapping.
func appendEncodingKeyDiags(dst []ir.Diagnostic, types ir.TypeRegistry, facts *irfacts.DocumentFacts, site payloadSite,
	keys *[]ir.PropertyID,
) []ir.Diagnostic {
	for i, content := range site.contents {
		dst = appendUnknownPartDiags(dst, types, facts, content, site, i, keys)
	}
	return dst
}

// appendUnknownPartDiags appends to dst a diagnostic per encoding key of
// content i of the payload at site that names no property the content's type
// exposes. The keys are asked in ascending order, sorted in keys, so map
// iteration reaches neither the output nor the questions facts is asked, and
// only a key reported has its location spelled. A key naming a declaration the
// type's flattening overrides passes, though docs/ir/models.md#the-flattening
// has it name the one taken: flattening every type asked about grows
// quadratically down a deep hierarchy.
func appendUnknownPartDiags(dst []ir.Diagnostic, types ir.TypeRegistry, facts *irfacts.DocumentFacts, content ir.Content,
	site payloadSite, i int, keys *[]ir.PropertyID,
) []ir.Diagnostic {
	*keys = (*keys)[:0]
	for key := range content.Encoding {
		*keys = append(*keys, key)
	}
	slices.Sort(*keys)
	for _, key := range *keys {
		if key == "" || partNamed(types, facts, content.Type.Target, key) {
			continue // an empty key names no part, which checkEmptyRefs reports
		}
		at := site.where() + "/contents/" + strconv.Itoa(i) + "/encoding/" + string(key)
		dst = append(dst, diag(ir.SeverityError, "ir/encoding-key-unknown-property",
			fmt.Sprintf("encoding key %q at %s addresses no property of the content's type %q",
				key, at, content.Type.Target), at))
	}
	return dst
}

// partNamed reports whether key names a part of a body typed target: a
// property the type exposes, or one a variant of the union it stands for
// exposes, through the unions nested in its variants, as a composition
// distributed across a union puts the body's properties on every variant;
// else one a shape of such a type that is a model narrowing a union holds
// (narrowedHolds), which is read only then (docs/ir/payloads.md).
func partNamed(types ir.TypeRegistry, facts *irfacts.DocumentFacts, target ir.TypeID, key ir.PropertyID) bool {
	return anyHolder(types, target, func(typeID ir.TypeID) bool { return facts.Exposes(typeID, key) }) ||
		anyHolder(types, target, func(typeID ir.TypeID) bool { return narrowedHolds(types, typeID, key, maxNarrowedSteps) })
}

// anyHolder reports whether holds answers true of target or, where it stands
// for a union, of a variant of that union, through the unions nested in them,
// depth first, stopping at the first. Each union is read once, so a union
// nesting itself ends.
func anyHolder(types ir.TypeRegistry, target ir.TypeID, holds func(ir.TypeID) bool) bool {
	if holds(target) {
		return true
	}
	read := map[*ir.Union]bool{}
	pending := []ir.TypeID{target}
	for len(pending) > 0 {
		next := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		union := unionThrough(types, next)
		if union == nil || read[union] {
			continue
		}
		read[union] = true
		for _, variant := range union.Variants {
			if holds(variant.Type.Target) {
				return true
			}
			pending = append(pending, variant.Type.Target)
		}
	}
	return false
}

// maxNarrowedSteps bounds the steps ir.NarrowedShapes takes for one type a
// key is asked of (styleguide bounded-everything rule). It is the bound the
// OpenAPI compiler keys a narrowing's parts under, so no key it writes is
// refused for a walk cut shorter here.
const maxNarrowedSteps = 100_000

// narrowedHolds reports whether a shape of the model target, where it narrows
// a union (ir.NarrowedShapes), holds a member declared as key: a part only one
// of the union's variants declares, which the model exposes nowhere. A walk
// past limit steps lists no shape, so it holds none.
func narrowedHolds(types ir.TypeRegistry, target ir.TypeID, key ir.PropertyID, limit int) bool {
	narrowing, _, _ := ir.NarrowedShapes(types, target, limit)
	for _, shape := range narrowing.Shapes {
		if slices.ContainsFunc(shape.Properties, func(fp ir.FlatProperty) bool { return fp.Property.ID == key }) {
			return true
		}
	}
	return false
}

// unionThrough returns the union target stands for, read through a chain of
// alias scalars, and nil where it stands for none. The IR permits a cyclic
// chain, so a visited set ends the walk at the first type it meets twice.
func unionThrough(types ir.TypeRegistry, target ir.TypeID) *ir.Union {
	seen := map[ir.TypeID]bool{}
	for !seen[target] {
		seen[target] = true
		if union, ok := types[target].(*ir.Union); ok {
			return union
		}
		base, alias := ir.AliasBase(types, target)
		if !alias {
			return nil
		}
		target = base
	}
	return nil
}
