package pass

import (
	"fmt"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// checkServerIndices reports Service.Servers and Channel.Servers entries that
// address no entry of Document.Servers.
//
// These are references carried as integer indices, so nothing in their Go type
// marks them as references and the reference walk cannot reach them — they are
// enumerated here instead, and a new one has to be added by hand; irverify's
// integerFields test is what notices it. An emitter iterating them to render
// base URLs indexes out of range on a document that is otherwise referentially
// closed.
func checkServerIndices(doc *ir.Document) []ir.Diagnostic {
	declared := len(doc.Servers)
	var diags []ir.Diagnostic
	for _, svc := range doc.Services {
		diags = appendServerIndexDiags(diags, svc.Servers, declared, string(svc.ID))
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Channels)) {
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
	ir.ForEachOperation(doc, func(op ir.Operation) {
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
	for _, index := range slices.Sorted(maps.Keys(status)) {
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
