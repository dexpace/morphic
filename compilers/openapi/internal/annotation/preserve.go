// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// PreserveInto records raw under key in unmodeled, located at prov, or records
// nothing when there are no bytes to record.
//
// len rather than a nil comparison: nil and a zero-length slice are distinct
// states, and an empty payload is the worse of the two. It preserves no
// construct, and it fails the encoding of the whole document that carries it.
func PreserveInto(unmodeled *ir.Unmodeled, key string, raw ir.RawValue,
	reason ir.UnmodeledReason, prov ir.Provenance,
) {
	if len(raw) == 0 {
		return
	}
	if *unmodeled == nil {
		*unmodeled = ir.Unmodeled{}
	}
	(*unmodeled)[key] = ir.UnmodeledEntry{
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
func PreserveNodeInto(unmodeled *ir.Unmodeled, key string, node *yaml.Node,
	reason ir.UnmodeledReason, prov ir.Provenance,
) (bool, []ir.Diagnostic) {
	raw, err := RawFromNode(node)
	if err != nil {
		return false, []ir.Diagnostic{unpreservableDiag(key, prov, err)}
	}
	if len(raw) == 0 {
		return false, nil
	}
	PreserveInto(unmodeled, key, raw, reason, prov)
	return true, nil
}

// PreserveSchemaKeyword is PreserveNodeInto addressed by keyword: it keeps what
// schema, the schema at pointer, writes under keyword, keyed by EntryKey and
// located at the keyword itself. Those are the key and the position the census
// checks before keeping a keyword on its own (unrecorded). Deriving both here
// leaves no caller a way to have the census keep the keyword again under
// another key, or report a collision with this entry.
//
// An absent keyword is no construct: it keeps and places nothing.
func PreserveSchemaKeyword(unmodeled *ir.Unmodeled, schema *statement.Schema, keyword string,
	reason ir.UnmodeledReason, pointer jsontext.Pointer, doc provenance.Doc,
) (bool, []ir.Diagnostic) {
	node := keyNode(schema, keyword)
	if node == nil {
		return false, nil
	}
	return PreserveNodeInto(unmodeled, EntryKey("", keyword), node, reason, doc.At(pointer+ids.Ptr(keyword)))
}

// PreserveValidationOnlyKeyword is PreserveSchemaKeyword for a validation-only
// keyword: it keeps the keyword under ReasonValidationOnly, at its own node,
// which is what a validation emitter reports against, and announces it at
// pointer, the schema that wrote it. What PreserveSchemaKeyword keeps nothing
// of, it announces nothing of, reporting whether the entry was written. A
// validation-only entry combining several keywords has no such node and is not
// kept here but by Read, which attributes it by the keywords it holds
// (GitHub #534).
func PreserveValidationOnlyKeyword(unmodeled *ir.Unmodeled, schema *statement.Schema, keyword string,
	pointer jsontext.Pointer, doc provenance.Doc,
) (bool, []ir.Diagnostic) {
	kept, diags := PreserveSchemaKeyword(unmodeled, schema, keyword, ir.ReasonValidationOnly, pointer, doc)
	if !kept {
		return false, diags
	}
	return true, append(diags, validationOnlyNote(doc.At(pointer), keyword))
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
// saying message. An absent payload records nothing and announces nothing; an
// unconvertible one never reaches here, because its caller reports it through
// unpreservableDiag first.
func preserveValidationOnlyInto(unmodeled *ir.Unmodeled, key string, raw ir.RawValue,
	entry, note ir.Provenance, message string,
) []ir.Diagnostic {
	if len(raw) == 0 {
		return nil
	}
	PreserveInto(unmodeled, key, raw, ir.ReasonValidationOnly, entry)
	return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.ValidationOnlyKeyword, note, "%s", message)}
}

// validationOnlyNote announces the validation-only entry named label at note,
// the schema that wrote it: one info per entry kept, whichever keeper kept it.
func validationOnlyNote(note ir.Provenance, label string) ir.Diagnostic {
	return diag.Newf(ir.SeverityInfo, diag.ValidationOnlyKeyword, note, "%s", validationOnlyMessage(label))
}

// validationOnlyMessage is what the report of the validation-only entry named
// label says.
func validationOnlyMessage(label string) string {
	return fmt.Sprintf("validation-only keyword %q kept verbatim under Unmodeled", label)
}

// CopyEntriesInto writes onto unmodeled a copy of each entry of src, its value
// cloned, for a reader keeping one set of entries on several carriers. Unlike
// MergeUnmodeled it never replaces an entry unmodeled holds: that one stays,
// and the entry from src is reported missing from unmodeled
// (diag.UnknownKeyEntryAlreadyClaimed), in key order as the census reports.
// Another carrier may hold it, so the finding does not say the IR lacks it.
func CopyEntriesInto(unmodeled *ir.Unmodeled, src ir.Unmodeled) []ir.Diagnostic {
	if len(src) == 0 {
		return nil
	}
	if *unmodeled == nil {
		*unmodeled = make(ir.Unmodeled, len(src))
	}
	var taken []string
	for key, entry := range src {
		if _, held := (*unmodeled)[key]; held {
			taken = append(taken, key)
			continue
		}
		entry.Value = entry.Value.Clone()
		entry.Targets = maps.Clone(entry.Targets)
		(*unmodeled)[key] = entry
	}
	slices.Sort(taken)
	var diags []ir.Diagnostic
	for _, key := range taken {
		diags = append(diags, refusedCopyDiag(key, src[key].Provenance, (*unmodeled)[key].Provenance.Pointer))
	}
	return diags
}

// refusedCopyDiag reports an entry CopyEntriesInto left off a carrier that
// already holds its key. Unlike occupiedEntryDiag it claims no loss from the IR,
// since the other carriers the entry is copied onto may hold it.
func refusedCopyDiag(entry string, at ir.Provenance, held jsontext.Pointer) ir.Diagnostic {
	return diag.Newf(ir.SeverityWarning, diag.UnknownKeyEntryAlreadyClaimed, at,
		"%s is already held by the construct at %q, so the node holding that construct keeps no copy of this one",
		entry, held)
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

// underlay is what Read knows of the entries it merged: those the site's own
// schema keeps (own), and those only a folded entry kept whole wrote (filled).
type underlay struct {
	own, filled map[string]bool
}

// add adds to dst each entry of src, one voice's, it lacks, walking src alone:
// overlaying the site's map on each voice would cost it all those before it.
// An entry own keeps stays its. A voice kept whole is no writer of what it
// folds, which its entry holds: it fills a key no other voice writes, which
// the first voice kept apart writing it takes. One two voices kept apart write,
// alike, as voices agree, is no one writer's, so it takes site, and no order of
// the voices decides where it points
// (docs/compilers/openapi.md#allof-inline-residue).
func (u *underlay) add(dst, src ir.Unmodeled, whole bool, site ir.Provenance) ir.Unmodeled {
	if dst == nil && len(src) > 0 {
		dst = make(ir.Unmodeled, len(src))
	}
	for keyword, entry := range src {
		held, written := dst[keyword]
		switch {
		case !written:
			dst[keyword] = entry
			if whole {
				u.fill(keyword)
			}
		case whole || u.own[keyword]:
		case u.filled[keyword]:
			dst[keyword] = entry
			delete(u.filled, keyword)
		default:
			held.Provenance = site
			dst[keyword] = held
		}
	}
	return dst
}

// fill marks keyword as written by a folded entry kept whole alone.
func (u *underlay) fill(keyword string) {
	if u.filled == nil {
		u.filled = map[string]bool{}
	}
	u.filled[keyword] = true
}
