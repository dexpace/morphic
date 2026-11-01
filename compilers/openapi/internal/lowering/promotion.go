// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/v2"
	"iter"
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
// ErrorCase.Retryable/Throttling, Enum.Flags, EnumMember.Name — is a target this
// vocabulary is meant to grow, not a decision against it (GitHub #252).
const (
	// TargetDeprecation deprecates what a key sits on where no `deprecated`
	// keyword did, giving the members below a Deprecation to fill. The text
	// "deprecated" or a boolean true states it; a key may hold other words, as
	// x-stability does, and false or another word states nothing.
	TargetDeprecation ExtensionTarget = "deprecation"
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

	// TargetStability fills ir.Operation.Stability. The words a value may use
	// are stabilityTier's.
	TargetStability ExtensionTarget = "stability"

	// TargetRedaction asks for a value to be redacted, as `format: password`
	// does: ir.TypeCommon.Sensitive on a type node, Secret on a property or a
	// parameter. A boolean true states it, and false states nothing, so it never
	// clears what another declaration asks for.
	TargetRedaction ExtensionTarget = "redaction"
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
		// Both are stability keys, and `deprecated` is a word of theirs that names
		// no tier: this is what reads it.
		TargetDeprecation: {{Key: "x-fern-availability"}, {Key: "x-stability"}},
		TargetDeprecationMessage: {
			{Key: "x-deprecated-reason"},
			{Key: "x-stainless-deprecation-message"},
			{Key: "x-cfDeprecation", Member: "description"},
		},
		TargetDeprecationSince: {{Key: "x-deprecated-since"}},
		// x-sunset echoes the RFC 8594 Sunset header, which is a date by
		// definition, so it fills the date field and not the version one.
		TargetDeprecationRemovalDate: {
			{Key: "x-sunset"},
			{Key: "x-forge-sunset", Member: "date"},
			{Key: "x-cfDeprecation", Member: "eol"},
		},
		// x-extensible-enum is the convention for an enum a service may add
		// members to, so it says the set is open and not that it is closed.
		TargetEnumOpen: {{Key: "x-extensible-enum"}},
		// Where the two disagree the less stable statement wins: an operation
		// x-stability calls beta and the other key does not is one a consumer
		// should not take for stable (docs/compilers/openapi.md#extension-promotion).
		TargetStability: {{Key: "x-stability"}, {Key: "x-fern-availability"}},
		TargetRedaction: {{Key: "x-sensitive"}},
	}
}

// Promoter reads preserved vendor extensions into typed fields, by a policy
// fixed when it is built. Its zero value is the disabled policy, and reads
// nothing.
//
// Every read takes the node's Unmodeled entries rather than the source node,
// which is what makes "the extension survives its own promotion" structural
// instead of a rule each caller has to remember: nothing here can consume an
// entry. It takes them at the top level, so a key under a scope or inside a
// verbatim entry reaches no field (GitHub #1220, #1221).
type Promoter struct {
	targets map[ExtensionTarget][]ExtensionSource
	// keys is every extension key targets reads, distinct and sorted.
	keys []string
}

// Referent is what a carrier of a $ref reads of the schema the $ref ends on.
// Annotations merge from there field by field, a key on the carrier winning
// (docs/compilers/openapi.md#ref-use-site). The zero value is a carrier with no
// $ref, which is every carrier but a property, a header and a parameter.
type Referent struct {
	// Entries holds the mapped keys the referent writes, in Unmodeled's form.
	Entries ir.Unmodeled
	// InheritsDeprecated reports that the use site writes no `deprecated`, which
	// would decide over the referent's. A key on the referent stating one then
	// counts as the referent's `deprecated`.
	InheritsDeprecated bool
}

// newPromoter normalizes a policy. The caller's map and lists are copied: a
// Promoter travels in a by-value context, and a shared map or slice would be
// the one part of it a lowering could write through.
func newPromoter(p ExtensionPromotions) Promoter {
	if p.Disabled {
		return Promoter{}
	}
	targets := p.Targets
	if len(targets) == 0 {
		targets = DefaultExtensionPromotions()
	}
	out := Promoter{targets: make(map[ExtensionTarget][]ExtensionSource, len(targets))}
	for target, sources := range targets {
		out.targets[target] = slices.Clone(sources)
		for _, src := range sources {
			out.keys = append(out.keys, src.Key)
		}
	}
	slices.Sort(out.keys)
	out.keys = slices.Compact(out.keys)
	return out
}

// Keys yields every extension key the policy reads, for a carrier to find on its
// referent.
func (p Promoter) Keys() iter.Seq[string] { return slices.Values(p.keys) }

// Deprecation reads the extensions in own, and those from lends, into a
// deprecation: a key stating one creates *dep where no keyword did, then the
// members fill what is there, so a date on a referent reaches a use a key
// deprecates. It marks prov with the heuristic when it writes anything.
//
// A *dep still nil is the whole answer for a node that is not deprecated: a
// member beside none annotates nothing and stays where it is. A `deprecated:
// false` does not stop a key, as the marker and the entry show what was read.
func (p Promoter) Deprecation(own ir.Unmodeled, from Referent, dep **ir.Deprecation, prov *ir.Provenance) []ir.Diagnostic {
	if dep == nil || prov == nil {
		return nil
	}
	var lent ir.Unmodeled
	if from.InheritsDeprecated {
		lent = from.Entries
	}
	_, deprecated, diags := useSiteStated(own, lent, p.targets[TargetDeprecation], reading[bool]{
		target: TargetDeprecation,
		want:   "is neither text nor a boolean",
		read:   func(_ ExtensionSource, raw ir.RawValue) (bool, statement) { return statesDeprecated(raw) },
	})
	promoted := deprecated && *dep == nil
	if promoted {
		*dep = &ir.Deprecation{}
	}
	if *dep == nil {
		return diags
	}
	for _, member := range []struct {
		target ExtensionTarget
		field  *string
	}{
		{TargetDeprecationMessage, &(*dep).Message},
		{TargetDeprecationSince, &(*dep).Since},
		{TargetDeprecationRemovalVersion, &(*dep).RemovalVersion},
		{TargetDeprecationRemovalDate, &(*dep).RemovalDate},
	} {
		text, stated, misfits := useSiteStated(own, from.Entries, p.targets[member.target], textReading(member.target))
		diags = append(diags, misfits...)
		if stated {
			*member.field = text
			promoted = true
		}
	}
	if promoted {
		prov.Inferred = ir.JoinInferred(prov.Inferred, ExtensionPromotionHeuristic)
	}
	return diags
}

// Stability fills tier from the vendor extensions kept in unmodeled, and marks
// prov with the heuristic when it does.
func (p Promoter) Stability(unmodeled ir.Unmodeled, tier *ir.Stability, prov *ir.Provenance) []ir.Diagnostic {
	if tier == nil || prov == nil {
		return nil
	}
	stated, found, diags := firstStated(unmodeled, p.targets[TargetStability], reading[ir.Stability]{
		target: TargetStability,
		want:   "names no stability tier",
		read:   p.readStability,
	})
	if found {
		*tier = stated
		prov.Inferred = ir.JoinInferred(prov.Inferred, ExtensionPromotionHeuristic)
	}
	return diags
}

// Redaction sets redact from the vendor extensions kept in own, or those from
// lends, and marks prov with the heuristic when that writes it. A redaction
// already asked for by `format: password` needs no guess, so it stays unmarked.
func (p Promoter) Redaction(own ir.Unmodeled, from Referent, redact *bool, prov *ir.Provenance) []ir.Diagnostic {
	if redact == nil || prov == nil {
		return nil
	}
	_, stated, diags := useSiteStated(own, from.Entries, p.targets[TargetRedaction], reading[bool]{
		target: TargetRedaction,
		want:   "is not a boolean",
		read:   func(_ ExtensionSource, raw ir.RawValue) (bool, statement) { return statesBoolean(raw) },
	})
	if stated && !*redact {
		*redact = true
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
	if e == nil || prov == nil {
		return nil
	}
	open, _, diags := firstStated(unmodeled, p.targets[TargetEnumOpen], reading[bool]{
		target: TargetEnumOpen,
		read:   func(_ ExtensionSource, raw ir.RawValue) (bool, statement) { return extensionOpenness(raw), states },
	})
	if open {
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

// useSiteStated is firstStated for a carrier of a $ref: what own states, and
// otherwise what referent does. A key on the carrier wins without a report, as a
// keyword beside a $ref does. The referent's findings are dropped because it
// is a declaration, and reports where it is written; a use repeating them would
// be one report per use.
func useSiteStated[T comparable](own, referent ir.Unmodeled, sources []ExtensionSource, r reading[T]) (T, bool, []ir.Diagnostic) {
	v, stated, diags := firstStated(own, sources, r)
	if !stated {
		v, stated, _ = firstStated(referent, sources, r)
	}
	return v, stated, diags
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

// readStability reads a value as a tier.
//
// One key may also be a source of TargetDeprecation, whose reader takes what
// names no tier: its word `deprecated` and its boolean are that target's
// statement, and a value neither target can hold is its report. Answering those
// here too would report one defect twice, or call a word misread that was read.
// Another word no target reads, and any of these on a key only this target
// reads, is a misfit.
func (p Promoter) readStability(src ExtensionSource, raw ir.RawValue) (ir.Stability, statement) {
	if text, ok := extensionText(raw); ok {
		if tier := stabilityTier(text); tier != ir.StabilityUnstated {
			return tier, states
		}
	}
	if _, said := statesDeprecated(raw); said != statesNothing && slices.Contains(p.targets[TargetDeprecation], src) {
		return ir.StabilityUnstated, statesNothing
	}
	return ir.StabilityUnstated, misfit
}

// stabilityTier returns the tier a source's word names, or StabilityUnstated for
// a word naming none. The set is what the sources share and not any one of them
// (docs/ir/operations.md#stability): `preview` has one source, so it reads as
// the tier it promises least, and `legacy` says a thing is old and not how far
// it is committed to.
func stabilityTier(word string) ir.Stability {
	switch word {
	case "alpha":
		return ir.StabilityAlpha
	case "beta", "preview":
		return ir.StabilityBeta
	case "stable", "generally-available":
		return ir.StabilityStable
	default:
		return ir.StabilityUnstated
	}
}

// statesDeprecated reads a value as saying that what it sits on is deprecated.
// The text "deprecated" and a boolean true state it, false and any other word
// state nothing, and any other shape misfits. The bool is whether it stated it.
func statesDeprecated(raw ir.RawValue) (bool, statement) {
	if text, ok := extensionText(raw); ok {
		if text == "deprecated" {
			return true, states
		}
		return false, statesNothing
	}
	return statesBoolean(raw)
}

// statesBoolean reads a value that is a statement only when true. The target is
// *bool for the reason extensionOpenness says: null decodes into a bool as
// false, which would read a bare key as a statement of nothing rather than as
// another shape.
func statesBoolean(raw ir.RawValue) (bool, statement) {
	var flag *bool
	if err := json.Unmarshal(raw, &flag); err != nil || flag == nil {
		return false, misfit
	}
	if *flag {
		return true, states
	}
	return false, statesNothing
}

// value returns what s reads of entry, and where that sits: the entry's value
// whole, or the member s names. An absent member states nothing, as an absent
// key does; a value with no members cannot hold one (misfit).
func (s ExtensionSource) value(entry ir.UnmodeledEntry) (ir.RawValue, ir.Provenance, statement) {
	if s.Member == "" {
		return entry.Value, entry.Provenance, states
	}
	var members map[string]ir.RawValue
	if err := json.Unmarshal(entry.Value, &members); err != nil || members == nil {
		return nil, entry.Provenance, misfit
	}
	raw, present := members[s.Member]
	if !present {
		return nil, entry.Provenance, statesNothing
	}
	at := entry.Provenance
	at.Pointer += ids.Ptr(s.Member)
	return raw, at, states
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
