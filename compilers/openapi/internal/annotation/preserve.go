// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"maps"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
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
		return false, []ir.Diagnostic{unpreservableDiag(key, prov, err)}
	}
	if len(raw) == 0 {
		return false, nil
	}
	PreserveInto(p, key, raw, reason, prov)
	return true, nil
}

// PreserveSchemaKeyword is PreserveNodeInto addressed by keyword: it keeps what
// s, the schema at pointer, writes under keyword, keyed by EntryKey and located
// at the keyword itself. Those are the key and the position the census checks
// before keeping a keyword on its own (unrecorded). Deriving both here leaves
// no caller a way to have the census keep the keyword again under another key,
// or report a collision with this entry.
//
// An absent keyword is no construct: it keeps nothing and asks locate nothing.
func PreserveSchemaKeyword(p *ir.Unmodeled, s *oas3.Schema, keyword string,
	reason ir.UnmodeledReason, pointer jsontext.Pointer, locate Locator,
) (bool, []ir.Diagnostic) {
	node := RawPropertyNode(s, keyword)
	if node == nil {
		return false, nil
	}
	return PreserveNodeInto(p, EntryKey("", keyword), node, reason, locate(pointer+ids.Ptr(keyword)))
}

// PreserveValidationOnlyKeyword is PreserveSchemaKeyword for a validation-only
// keyword: it keeps the keyword under ReasonValidationOnly, at its own node,
// which is what a validation emitter reports against, and announces it at
// pointer, the schema that wrote it. What PreserveSchemaKeyword keeps nothing
// of, it announces nothing of. A validation-only entry combining several
// keywords has no such node and is not kept here but by Read, which attributes
// it by the keywords it holds (GitHub #534).
func PreserveValidationOnlyKeyword(p *ir.Unmodeled, s *oas3.Schema, keyword string,
	pointer jsontext.Pointer, locate Locator,
) []ir.Diagnostic {
	kept, diags := PreserveSchemaKeyword(p, s, keyword, ir.ReasonValidationOnly, pointer, locate)
	if !kept {
		return diags
	}
	return append(diags, validationOnlyNote(locate(pointer), keyword))
}

// unpreservableDiag reports a construct the compiler could neither model nor
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
func unpreservableDiag(key string, prov ir.Provenance, err error) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.UnpreservableConstruct, prov,
		"%s could not be kept verbatim under Unmodeled and is represented in the IR "+
			"in no form at all: %s", key, err.Error())
}

// preserveValidationOnlyInto records a validation-only payload at entry and
// returns the one diagnostic announcing it at note, the schema that wrote it,
// naming it by label. An absent payload records nothing and announces nothing;
// an unconvertible one never reaches here, because its caller reports it
// through unpreservableDiag first.
func preserveValidationOnlyInto(p *ir.Unmodeled, key string, raw ir.RawValue,
	entry, note ir.Provenance, label string,
) []ir.Diagnostic {
	if len(raw) == 0 {
		return nil
	}
	PreserveInto(p, key, raw, ir.ReasonValidationOnly, entry)
	return []ir.Diagnostic{validationOnlyNote(note, label)}
}

// validationOnlyNote announces the validation-only entry named label at note,
// the schema that wrote it: one info per entry kept, whichever keeper kept it.
func validationOnlyNote(note ir.Provenance, label string) ir.Diagnostic {
	return diag.Newf(ir.SeverityInfo, diag.ValidationOnlyKeyword, note,
		"validation-only keyword %q kept verbatim under Unmodeled", label)
}

// MergeUnmodeled overlays src onto dst, allocating dst on first write.
func MergeUnmodeled(dst, src ir.Unmodeled) ir.Unmodeled {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = ir.Unmodeled{}
	}
	maps.Copy(dst, src)
	return dst
}
