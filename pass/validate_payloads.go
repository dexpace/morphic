package pass

import (
	"fmt"
	"maps"
	"slices"

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
	forEachPayload(doc, func(site payloadSite) {
		diags = appendEncodingKeyDiags(diags, facts, site.payload, site.where)
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
		if site.request || site.payload.Required == nil {
			return
		}
		diags = append(diags, diag(ir.SeverityError, "ir/payload-required-outside-request",
			fmt.Sprintf("payload at %s sets required, which only a request body can state", site.where),
			site.where))
	})
	return diags
}

// payloadSite is one Payload a document carries: the node, where it hangs, and
// whether that position is a request — the one place Payload.Required is
// defined.
type payloadSite struct {
	payload *ir.Payload
	where   string
	request bool
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
		forEachErrorPayload(svc.CommonErrors, string(svc.ID)+"/commonErrors", fn)
	}
	ir.ForEachOperation(doc, func(op ir.Operation) {
		if op.Request != nil {
			fn(payloadSite{payload: op.Request, where: string(op.ID) + "/request", request: true})
		}
		for i, r := range op.Responses {
			if r.Payload != nil {
				fn(payloadSite{payload: r.Payload, where: fmt.Sprintf("%s/responses/%d", op.ID, i)})
			}
		}
		forEachErrorPayload(op.Errors, string(op.ID)+"/errors", fn)
		if op.Default != nil && op.Default.Payload != nil {
			fn(payloadSite{payload: op.Default.Payload, where: string(op.ID) + "/default"})
		}
	})
	for _, id := range slices.Sorted(maps.Keys(doc.Messages)) {
		msg := doc.Messages[id]
		fn(payloadSite{payload: &msg.Payload, where: string(id)})
	}
}

// forEachErrorPayload calls fn once per error case carrying a payload; where
// locates the list the cases hang from.
func forEachErrorPayload(errs []ir.ErrorCase, where string, fn func(payloadSite)) {
	for i, ec := range errs {
		if ec.Payload != nil {
			fn(payloadSite{payload: ec.Payload, where: fmt.Sprintf("%s/%d", where, i)})
		}
	}
}

// appendEncodingKeyDiags appends to dst a diagnostic per unresolvable encoding
// key in each of the payload's contents; where locates the payload's owner.
//
// A content whose type no entry declares exposes no part, so each of its keys
// is reported beside checkDanglingRefs's finding on the type. The claims
// differ, as with checkMapping.
func appendEncodingKeyDiags(dst []ir.Diagnostic, facts *irfacts.Index, payload *ir.Payload, where string) []ir.Diagnostic {
	for i, c := range payload.Contents {
		if len(c.Encoding) == 0 {
			continue
		}
		at := fmt.Sprintf("%s/contents/%d", where, i)
		dst = appendUnknownPartDiags(dst, facts, c, at)
	}
	return dst
}

// appendUnknownPartDiags appends to dst a diagnostic per encoding key of one
// content that names no property the content's type exposes, in ascending key
// order so map iteration cannot reach the output. A key naming a declaration
// the type's flattening overrides passes, though ir-design §4.3 has it name the
// one taken: flattening every type asked about grows quadratically down a deep
// hierarchy.
func appendUnknownPartDiags(dst []ir.Diagnostic, facts *irfacts.Index, c ir.Content, where string) []ir.Diagnostic {
	for _, key := range slices.Sorted(maps.Keys(c.Encoding)) {
		if facts.Exposes(c.Type.Target, key) {
			continue
		}
		at := where + "/encoding/" + string(key)
		dst = append(dst, diag(ir.SeverityError, "ir/encoding-key-unknown-property",
			fmt.Sprintf("encoding key %q at %s addresses no property of the content's type %q",
				key, at, c.Type.Target), at))
	}
	return dst
}
