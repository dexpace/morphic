// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// ExtensionPromotionHeuristic is the name Provenance.Inferred carries on a node
// whose typed field was read out of a vendor extension. It is a constant
// because the marker is what an auditor greps for, and a spelling written at
// the producing site and again at a reading test can drift.
const ExtensionPromotionHeuristic ir.Inferred = "extension-promotion"

// extensionKeyPrefix is the namespace an x-* key is kept under in Unmodeled.
// Promotion reads the preserved entry rather than the source node, so it has to
// spell the same namespace back.
const extensionKeyPrefix = "openapi:"

// ExtensionTarget names one typed IR field a vendor extension can be read into.
//
// It is a closed vocabulary rather than a free-form path because a promotion
// has to be applied by code that knows the field's type, and a name nothing
// implements would be a policy that silently does nothing.
type ExtensionTarget string

// The typed fields promotion can fill today. Every other field an extension is
// the only OpenAPI spelling for — Pagination, LongRunning, Idempotency,
// ErrorCase.Retryable/Throttling, Enum.Flags, EnumMember.Name, Sensitive and
// Secret — is a target this vocabulary is meant to grow, not a decision against
// it (GitHub #252).
const (
	// TargetDeprecationMessage fills ir.Deprecation.Message.
	TargetDeprecationMessage ExtensionTarget = "deprecation.message"
	// TargetDeprecationSince fills ir.Deprecation.Since.
	TargetDeprecationSince ExtensionTarget = "deprecation.since"
	// TargetDeprecationRemovalVersion fills ir.Deprecation.RemovalVersion. No
	// default key names it: the one convention in wide use for a scheduled
	// removal, x-sunset, states a date, and a document that spells a removal
	// *version* names its own key.
	TargetDeprecationRemovalVersion ExtensionTarget = "deprecation.removalVersion"
	// TargetDeprecationRemovalDate fills ir.Deprecation.RemovalDate.
	TargetDeprecationRemovalDate ExtensionTarget = "deprecation.removalDate"

	// TargetEnumOpen clears ir.Enum.Closed, saying the member set admits values
	// the document does not list.
	//
	// It names the fact rather than the field, unlike the rest of this
	// vocabulary: a schema's `enum` is closed by definition, so openness is the
	// only half of that bool a document declares, and a target named for Closed
	// could only be written false and would read as its own opposite.
	//
	// The key's presence is the statement, since x-extensible-enum, the
	// established spelling, writes the member list as its value. An explicit
	// boolean `false` is honoured rather than inverted (extensionOpenness).
	TargetEnumOpen ExtensionTarget = "enum.open"
)

// ExtensionSource is one place a policy reads a fact from: the extension Key
// and, where that key's value is an object holding several facts, the Member of
// it that holds this one.
//
// The two are fields rather than a joined path because an extension name may
// hold any character a path separator would be.
type ExtensionSource struct {
	// Key is the extension name as the document writes it, x- prefix included.
	Key string `json:"key"`
	// Member names a member of the key's object value, or is empty to read the
	// value whole.
	Member string `json:"member,omitempty"`
}

// String spells s as a report names it.
func (s ExtensionSource) String() string {
	if s.Member == "" {
		return s.Key
	}
	return s.Key + "." + s.Member
}

// ExtensionPromotions is the vendor-extension promotion policy: which x-* keys
// are read into which typed IR field.
//
// It is a policy rather than a table in the lowering because OpenAPI assigns an
// x-* key no semantics whatsoever, so reading one as anything is a guess about
// a convention (architecture principle 6). A promoted field is marked
// ExtensionPromotionHeuristic in its node's provenance and the extension stays
// in Unmodeled untouched, which is what makes the guess auditable and
// reversible: a consumer that disagrees can ignore the typed field and read the
// entry itself.
type ExtensionPromotions struct {
	// Disabled turns promotion off. Off means off: every extension is kept
	// verbatim and no typed field is written from one.
	Disabled bool `json:"disabled,omitzero"`
	// Targets replaces the default map rather than extending it, so a caller who
	// states a mapping gets exactly that mapping. Empty means the default. Each
	// list is in order of precedence: the first source stating a value fills the
	// field, and a later one stating another is reported.
	Targets map[ExtensionTarget][]ExtensionSource `json:"targets,omitempty"`
}

// DefaultExtensionPromotions is the mapping the policy uses when the caller
// states none. It is a default and not a standard: OpenAPI defines none of
// these keys, and each is simply the spelling that has become common for a
// field the format never gave a keyword. A document using another spelling is
// not wrong — it names its own mapping.
func DefaultExtensionPromotions() map[ExtensionTarget][]ExtensionSource {
	return map[ExtensionTarget][]ExtensionSource{
		TargetDeprecationMessage: {{Key: "x-deprecated-reason"}},
		TargetDeprecationSince:   {{Key: "x-deprecated-since"}},
		// x-sunset echoes the RFC 8594 Sunset header, which is a date by
		// definition, so it fills the date field and not the version one.
		TargetDeprecationRemovalDate: {{Key: "x-sunset"}},
		// x-extensible-enum is the convention for an enum a service may add
		// members to, so it says the set is open and not that it is closed.
		TargetEnumOpen: {{Key: "x-extensible-enum"}},
	}
}

// Promoter reads preserved vendor extensions into typed fields, by a policy
// fixed when it is built. Its zero value is the disabled policy, and reads
// nothing.
//
// Every read takes the node's Unmodeled entries rather than the source node,
// which is what makes "the extension survives its own promotion" structural
// instead of a rule each caller has to remember: nothing here can consume an
// entry.
type Promoter struct {
	targets map[ExtensionTarget][]ExtensionSource
}

// newPromoter normalizes a policy. The caller's map and lists are copied: a
// Promoter travels in a by-value context, and a shared map or slice would be
// the one part of it a lowering could write through.
func newPromoter(p ExtensionPromotions) Promoter {
	if p.Disabled {
		return Promoter{}
	}
	if len(p.Targets) == 0 {
		return Promoter{targets: DefaultExtensionPromotions()}
	}
	targets := make(map[ExtensionTarget][]ExtensionSource, len(p.Targets))
	for target, sources := range p.Targets {
		targets[target] = slices.Clone(sources)
	}
	return Promoter{targets: targets}
}

// Deprecation fills dep's fields from the vendor extensions kept in unmodeled,
// and marks prov with the heuristic when it writes anything.
//
// A nil dep is the whole answer for a node that is not deprecated — the field
// describes a deprecation, so an x-deprecated-reason beside no `deprecated: true`
// annotates nothing and stays where it is.
func (p Promoter) Deprecation(unmodeled ir.Unmodeled, dep *ir.Deprecation, prov *ir.Provenance) []ir.Diagnostic {
	if dep == nil || prov == nil || len(unmodeled) == 0 || len(p.targets) == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	var promoted bool
	for _, target := range []ExtensionTarget{
		TargetDeprecationMessage, TargetDeprecationSince,
		TargetDeprecationRemovalVersion, TargetDeprecationRemovalDate,
	} {
		text, stated, misfits := firstStated(unmodeled, p.targets[target], textReading(target))
		diags = append(diags, misfits...)
		if stated {
			*deprecationField(dep, target) = text
			promoted = true
		}
	}
	if promoted {
		prov.Inferred = ir.JoinInferred(prov.Inferred, ExtensionPromotionHeuristic)
	}
	return diags
}

// EnumOpenness clears e.Closed when the vendor extensions kept in unmodeled
// state the set open, and marks prov with the heuristic when they do.
//
// The key's presence is the statement, so every value shape but an explicit
// `false` reads as open (extensionOpenness).
//
// A document writing x-extensible-enum instead of `enum` lowers to no ir.Enum,
// so there is nothing to open. Minting an enum from a member-list extension is
// out of scope; the entry survives verbatim for a consumer that wants it
// (GitHub #427).
func (p Promoter) EnumOpenness(unmodeled ir.Unmodeled, e *ir.Enum, prov *ir.Provenance) []ir.Diagnostic {
	if e == nil || prov == nil || len(unmodeled) == 0 || len(p.targets) == 0 {
		return nil
	}
	open, stated, diags := firstStated(unmodeled, p.targets[TargetEnumOpen], reading[bool]{
		target: TargetEnumOpen,
		read:   func(_ ExtensionSource, raw ir.RawValue) (bool, statement) { return extensionOpenness(raw), states },
	})
	if stated && open {
		e.Closed = false
		prov.Inferred = ir.JoinInferred(prov.Inferred, ExtensionPromotionHeuristic)
	}
	return diags
}

// statement is what a source's value says to the target reading it.
type statement int

const (
	// statesNothing is a value the target reads as no statement of its fact,
	// which is not a defect of the value.
	statesNothing statement = iota
	// states is a value that fills the target.
	states
	// misfit is a value of a form the target cannot hold, which is reported.
	misfit
)

// reading is how one target reads its sources: what a value says, and what a
// report says of a value that misfits.
type reading[T comparable] struct {
	target ExtensionTarget
	read   func(src ExtensionSource, raw ir.RawValue) (T, statement)
	want   string
}

// textReading reads a value as prose, a version or a date. Text is taken as
// written; a date is not parsed, because the mapping is the caller's statement
// that the key holds one.
func textReading(target ExtensionTarget) reading[string] {
	return reading[string]{
		target: target,
		want:   "is not a string",
		read: func(_ ExtensionSource, raw ir.RawValue) (string, statement) {
			text, ok := extensionText(raw)
			if !ok {
				return "", misfit
			}
			return text, states
		},
	}
}

// firstStated returns the value of the first of sources to state one, and
// whether any did. Precedence is the policy's order and never a sorted one, so
// a document writing two mapped keys cannot resolve by how the keys spell.
//
// A later source stating another value loses, and is reported at its own key,
// as is a value the target cannot hold: the entry stays either way, so the
// report is what tells a reader the field did not take it.
func firstStated[T comparable](unmodeled ir.Unmodeled, sources []ExtensionSource, r reading[T]) (T, bool, []ir.Diagnostic) {
	var kept T
	var first ExtensionSource
	var found bool
	var diags []ir.Diagnostic
	for _, src := range sources {
		entry, declared := unmodeled[extensionKeyPrefix+src.Key]
		if !declared {
			continue
		}
		raw, at, located := src.value(entry)
		if located == misfit {
			// Worded without the member, so one key several sources read reports
			// once: compile.Diags drops a repeat of the whole diagnostic.
			diags = append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"extension %q is not an object, so no member of it is read", src.Key))
		}
		if located != states {
			continue
		}
		v, said := r.read(src, raw)
		switch {
		case said == misfit:
			diags = append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"extension %q %s, so it does not fill %s", src, r.want, r.target))
		case said != states:
		case !found:
			kept, first, found = v, src, true
		case v != kept:
			diags = append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"extension %q states another %s than %q, which the policy reads first, and does not fill it",
				src, r.target, first))
		}
	}
	return kept, found, diags
}

// value returns what s reads of entry, and where that sits: the entry's value
// whole, or the member s names. An absent member states nothing, as an absent
// key does; a value with no members cannot hold one (misfit).
func (s ExtensionSource) value(entry ir.UnmodeledEntry) (ir.RawValue, ir.Provenance, statement) {
	if s.Member == "" {
		return entry.Value, entry.Provenance, states
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(entry.Value, &members); err != nil || members == nil {
		return nil, entry.Provenance, misfit
	}
	raw, present := members[s.Member]
	if !present {
		return nil, entry.Provenance, statesNothing
	}
	at := entry.Provenance
	at.Pointer += ids.Ptr(s.Member)
	return ir.RawValue(raw), at, states
}

// deprecationField returns the member of dep target names. A target this
// returns nil for is never asked for: Promoter.Deprecation names the four.
func deprecationField(dep *ir.Deprecation, target ExtensionTarget) *string {
	switch target {
	case TargetDeprecationMessage:
		return &dep.Message
	case TargetDeprecationSince:
		return &dep.Since
	case TargetDeprecationRemovalVersion:
		return &dep.RemovalVersion
	case TargetDeprecationRemovalDate:
		return &dep.RemovalDate
	default:
		return nil
	}
}

// extensionText reads a preserved extension value as a string. Every
// Deprecation field is prose, a version or a date, so any other JSON shape is a
// document meaning something else by the key.
//
// The target is *string because JSON null decodes into a string without error
// and leaves it empty, so a bare `x-sunset:` would write an empty field, mark
// the node inferred and report nothing. A key with no value is reported as
// another shape.
func extensionText(raw ir.RawValue) (string, bool) {
	var text *string
	if err := json.Unmarshal(raw, &text); err != nil || text == nil {
		return "", false
	}
	return *text, true
}

// extensionOpenness reads a preserved extension value as a statement that an
// enum's member set is open.
//
// The key's presence is the statement, so any value but a boolean reads as
// open: x-extensible-enum's established spelling writes the *members* as its
// value, which says nothing the key has not. A boolean states openness itself,
// so an explicit false is read as written, not inverted.
//
// The target is *bool because JSON null decodes into a bool without error and
// leaves it false, so a bare `x-extensible-enum:`, the presence-only spelling
// this reading exists for, would read as the explicit false that declines.
func extensionOpenness(raw ir.RawValue) bool {
	var open *bool
	if err := json.Unmarshal(raw, &open); err != nil || open == nil {
		return true
	}
	return *open
}
