package lowering

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// ExtensionPromotionHeuristic is the name Provenance.Inferred carries on a node
// whose typed field was read out of a vendor extension. It is a constant
// because the marker is what an auditor greps for, and a spelling written at
// the producing site and again at a reading test can drift.
const ExtensionPromotionHeuristic = "extension-promotion"

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
	// It names the fact rather than the field, which the rest of this vocabulary
	// does not, because openness is the only half of that bool a document ever
	// declares: a schema's `enum` is closed by definition, so a target named for
	// Closed could only ever be written false and would read as its own opposite
	// at every mapping that names it.
	//
	// The key's presence is the statement. The established spelling,
	// x-extensible-enum, writes the member list as its value, so there is no flag
	// to read there — but a boolean value *is* a statement about openness, and an
	// explicit `false` is honoured rather than inverted (extensionOpenness).
	TargetEnumOpen ExtensionTarget = "enum.open"
)

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
	Disabled bool `json:"disabled,omitempty"`
	// Targets replaces the default map rather than extending it, so a caller who
	// states a mapping gets exactly that mapping. Empty means the default. Keys
	// are extension names as the document writes them, x- prefix included.
	Targets map[string]ExtensionTarget `json:"targets,omitempty"`
}

// DefaultExtensionPromotions is the mapping the policy uses when the caller
// states none. It is a default and not a standard: OpenAPI defines none of
// these keys, and each is simply the spelling that has become common for a
// field the format never gave a keyword. A document using another spelling is
// not wrong — it names its own mapping.
func DefaultExtensionPromotions() map[string]ExtensionTarget {
	return map[string]ExtensionTarget{
		"x-deprecated-reason": TargetDeprecationMessage,
		"x-deprecated-since":  TargetDeprecationSince,
		// x-sunset echoes the RFC 8594 Sunset header, which is a date by
		// definition, so it fills the date field and not the version one.
		"x-sunset": TargetDeprecationRemovalDate,
		// x-extensible-enum is the convention for an enum a service may add
		// members to, so it says the set is open and not that it is closed.
		"x-extensible-enum": TargetEnumOpen,
	}
}

// PromoteDeprecation fills dep's fields from the vendor extensions kept in
// unmodeled, and marks prov with the heuristic when it writes anything.
//
// It reads the preserved Unmodeled entries rather than the source node, which
// is what makes "the extension survives its own promotion" structural instead
// of a rule each call site has to remember: there is nothing here that could
// consume an entry.
//
// A nil dep is the whole answer for a node that is not deprecated — the field
// describes a deprecation, so an x-deprecated-reason beside no `deprecated: true`
// annotates nothing and stays where it is.
func (c Ctx) PromoteDeprecation(unmodeled ir.Unmodeled, dep *ir.Deprecation, prov *ir.Provenance) []ir.Diagnostic {
	if dep == nil || prov == nil || len(unmodeled) == 0 || len(c.promotions) == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	var promoted bool
	// Sorted, so a policy naming two keys the document writes badly always
	// reports the same one first.
	for _, key := range slices.Sorted(maps.Keys(c.promotions)) {
		field := deprecationField(dep, c.promotions[key])
		entry, declared := unmodeled[extensionKeyPrefix+key]
		if field == nil || !declared {
			continue
		}
		text, ok := extensionText(entry.Value)
		if !ok {
			diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct,
				entry.Provenance.Pointer, "extension %q is not a string, so it does not fill %s",
				key, c.promotions[key]))
			continue
		}
		*field = text
		promoted = true
	}
	if promoted {
		markInferred(prov, ExtensionPromotionHeuristic)
	}
	return diags
}

// PromoteEnumOpenness clears e.Closed when the vendor extensions kept in
// unmodeled include a key the policy maps to TargetEnumOpen, and marks prov
// with the heuristic when it does.
//
// It is PromoteDeprecation at a second carrier, with the same three properties:
// the entry it reads stays where it was, the node records that a heuristic
// wrote the field, and a disabled policy writes nothing. What differs is that
// the fact is stated by the key being present rather than by a value, so this
// reports nothing: the deprecation reading declines a value it cannot hold and
// says so, while here every value shape but an explicit `false` is a key that
// means what its name says (TargetEnumOpen, extensionOpenness).
//
// The order the policy's keys are visited in is not fixed, because it cannot
// matter: a key that states openness writes the same field the same value as
// any other, a key that does not is skipped rather than deciding anything, and
// none of them reports. Two keys disagreeing therefore read the same either
// way round — open, because one of them said so.
//
// Deliberately out of scope: a document that writes x-extensible-enum *instead*
// of `enum`, listing the members in the extension, lowers to no ir.Enum at all,
// so there is no node here to open. Reading a member list out of an extension
// would be minting an enum from a vendor key rather than promoting a field, and
// the entry survives verbatim for a consumer that wants to (GitHub #427).
func (c Ctx) PromoteEnumOpenness(unmodeled ir.Unmodeled, e *ir.Enum, prov *ir.Provenance) {
	if e == nil || prov == nil || len(unmodeled) == 0 || len(c.promotions) == 0 {
		return
	}
	for key, target := range c.promotions {
		entry, declared := unmodeled[extensionKeyPrefix+key]
		if target != TargetEnumOpen || !declared || !extensionOpenness(entry.Value) {
			continue
		}
		e.Closed = false
		markInferred(prov, ExtensionPromotionHeuristic)
		return
	}
}

// deprecationField returns the field target names on dep, or nil when target
// names something that is not a deprecation field. A policy may map a key to
// any target in the vocabulary, and most carriers answer for only some of it.
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
// Deprecation field is prose, a version or a date, so a value of any other JSON
// shape is a document meaning something else by the key. Text of the right JSON
// shape is taken as written — a date is not parsed here, because the mapping is
// the caller's policy and a key it points at the date field is its statement
// that the key holds one.
func extensionText(raw ir.RawValue) (string, bool) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", false
	}
	return text, true
}

// extensionOpenness reads a preserved extension value as a statement that an
// enum's member set is open.
//
// The key's presence is the statement, so a value of any shape but a boolean
// reads as open: x-extensible-enum's established spelling writes the *members*
// as its value, and a list of members says nothing about openness that the key
// naming it has not already said. A boolean is the one shape that does state
// openness on its own, so an explicit false is read as written — a document
// saying the set is not extensible, which is not something to invert.
//
// The target is *bool rather than bool because JSON null decodes into a bool
// without error and leaves it false, so a bare `x-extensible-enum:` — the
// presence-only spelling this reading exists for — would otherwise be read as
// the explicit false that is the one way to decline.
func extensionOpenness(raw ir.RawValue) bool {
	var open *bool
	if err := json.Unmarshal(raw, &open); err != nil || open == nil {
		return true
	}
	return *open
}

// markInferred adds one heuristic's name to a provenance, keeping any already
// there. Provenance.Inferred holds a single string and more than one heuristic
// can reach a node — an operation grouped by path prefix whose deprecation
// reason was promoted is reached by two — so they are listed rather than one
// overwriting the other.
//
// Adding a name already listed is a no-op. A node reached by two references is
// annotated once per reference, and a marker repeated as many times as a
// component happens to be used would make the field depend on the document's
// reference count rather than on which heuristics ran.
func markInferred(prov *ir.Provenance, marker string) {
	if prov.Inferred == "" {
		prov.Inferred = marker
		return
	}
	if slices.Contains(strings.Split(prov.Inferred, ","), marker) {
		return
	}
	prov.Inferred += "," + marker
}

// promotionSet normalizes a policy into the map PromoteDeprecation reads, or
// nil when promotion is off. The caller's map is copied: the context is passed
// by value and a shared map would be the one part of it a callee could write
// through.
func promotionSet(p ExtensionPromotions) map[string]ExtensionTarget {
	if p.Disabled {
		return nil
	}
	if len(p.Targets) == 0 {
		return DefaultExtensionPromotions()
	}
	return maps.Clone(p.Targets)
}
