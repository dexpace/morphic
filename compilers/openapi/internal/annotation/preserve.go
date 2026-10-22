package annotation

import (
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// PreserveInto records raw under key in p, located at prov, or records nothing
// when there are no bytes to record.
//
// len rather than a nil comparison: nil and a zero-length slice are distinct
// states, and an empty payload is the worse of the two. It preserves no
// construct, and it fails the encoding of the whole document that carries it.
func PreserveInto(p *ir.Unmodeled, key string, raw ir.RawValue,
	reason ir.UnmodeledReason, prov ir.Provenance,
) {
	if len(raw) == 0 {
		return
	}
	if *p == nil {
		*p = ir.Unmodeled{}
	}
	(*p)[key] = ir.UnmodeledEntry{
		Reason:     reason,
		Value:      raw,
		Provenance: prov,
	}
}

// PreserveNodeInto converts node and records it under key, reporting a
// construct that reached the IR in no form at all rather than leaving its
// caller to announce a preservation that did not happen (GitHub #144).
//
// It reports whether an entry was written, which is what a caller with an
// announcement to make gates on. The three outcomes it distinguishes are the
// point: an absent node writes nothing and says nothing, because there was no
// construct; a converted one writes the entry; an unconvertible one writes
// nothing and yields the diagnostic that says so.
func PreserveNodeInto(p *ir.Unmodeled, key string, node *yaml.Node,
	reason ir.UnmodeledReason, prov ir.Provenance,
) (bool, []ir.Diagnostic) {
	raw, err := RawFromNode(node)
	if err != nil {
		return false, []ir.Diagnostic{UnpreservableDiag(key, prov, err)}
	}
	if len(raw) == 0 {
		return false, nil
	}
	PreserveInto(p, key, raw, reason, prov)
	return true, nil
}

// UnpreservableDiag reports a construct the compiler could neither model nor
// keep verbatim, so nothing of it reached the IR.
//
// Error rather than the degradation severity its callers otherwise use: a
// degraded lowering still describes the source in a weaker shape, while this one
// leaves no trace at all, which is a losslessness failure rather than a
// compromise (GitHub #144).
//
// The vendor-extension reader (ExtensionsFrom) reports the same conversion
// failure as a warning and is deliberately left alone: it already branches on the
// error and never claimed to have kept anything, so it is not the defect this
// code exists for.
func UnpreservableDiag(key string, prov ir.Provenance, err error) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.UnpreservableConstruct, prov,
		"%s could not be kept verbatim under Unmodeled and is represented in the IR "+
			"in no form at all: %s", key, err.Error())
}

// PreserveKeywordInto records a validation-only keyword at entry and returns the
// one diagnostic announcing it at note, the schema that wrote it. An absent
// payload records nothing and announces nothing; an unconvertible one never
// reaches here, because its caller reports it through UnpreservableDiag first.
func PreserveKeywordInto(p *ir.Unmodeled, key string, raw ir.RawValue,
	entry, note ir.Provenance, label string,
) []ir.Diagnostic {
	if len(raw) == 0 {
		return nil
	}
	PreserveInto(p, key, raw, ir.ReasonValidationOnly, entry)
	return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.ValidationOnlyKeyword, note,
		"validation-only keyword %q kept verbatim under Unmodeled", label)}
}
