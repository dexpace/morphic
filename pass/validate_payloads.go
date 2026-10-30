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
// A key is a PropID, which no document-level registry resolves, so the
// type-driven walk has nothing to resolve it against and the keys are
// enumerated here. An emitter rendering multipart parts looks the key up among
// the body's properties and silently renders no part for a miss.
//
// The code carries the ir/ namespace because it names the defect, not the
// finder (see the package doc), so a second checker growing this check adopts
// it. Only this pass reports it today. facts indexes doc.
func checkEncodingKeys(doc *ir.Document, facts *irfacts.Index) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var keys []ir.PropID // every content's keys are sorted in this one list
	forEachPayload(doc, func(site payloadSite) {
		diags = appendEncodingKeyDiags(diags, facts, site, &keys)
	})
	return diags
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
		for i, r := range op.Responses {
			if r.Payload != nil {
				fn(siteOf(*r.Payload, owner, "responses", i, false))
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

// siteOf is the payloadSite of p, hanging from field of owner at index, or at
// none for -1, request saying whether that position is a request.
func siteOf(p ir.Payload, owner, field string, index int, request bool) payloadSite {
	return payloadSite{contents: p.Contents, required: p.Required, owner: owner, field: field, index: index, request: request}
}

// appendEncodingKeyDiags appends to dst a diagnostic per unresolvable encoding
// key in each of the contents of the payload at site, sorting each content's
// keys in keys.
//
// A content whose type no entry declares exposes no part, so each of its keys
// is reported beside checkDanglingRefs's finding on the type. The claims
// differ, as with checkMapping.
func appendEncodingKeyDiags(dst []ir.Diagnostic, facts *irfacts.Index, site payloadSite, keys *[]ir.PropID) []ir.Diagnostic {
	for i, c := range site.contents {
		dst = appendUnknownPartDiags(dst, facts, c, site, i, keys)
	}
	return dst
}

// appendUnknownPartDiags appends to dst a diagnostic per encoding key of
// content i of the payload at site that names no property the content's type
// exposes. The keys are asked in ascending order, sorted in keys, so map
// iteration reaches neither the output nor the questions facts is asked, and
// only a key reported has its location spelled. A key naming a declaration the
// type's flattening overrides passes, though ir-design §4.3 has it name the one
// taken: flattening every type asked about grows quadratically down a deep
// hierarchy.
func appendUnknownPartDiags(dst []ir.Diagnostic, facts *irfacts.Index, c ir.Content, site payloadSite, i int,
	keys *[]ir.PropID,
) []ir.Diagnostic {
	*keys = (*keys)[:0]
	for key := range c.Encoding {
		*keys = append(*keys, key)
	}
	slices.Sort(*keys)
	for _, key := range *keys {
		if facts.Exposes(c.Type.Target, key) {
			continue
		}
		at := site.where() + "/contents/" + strconv.Itoa(i) + "/encoding/" + string(key)
		dst = append(dst, diag(ir.SeverityError, "ir/encoding-key-unknown-property",
			fmt.Sprintf("encoding key %q at %s addresses no property of the content's type %q",
				key, at, c.Type.Target), at))
	}
	return dst
}
