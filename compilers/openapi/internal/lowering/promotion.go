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
	// TargetDeprecationRemovalVersion fills ir.Deprecation.RemovalVersion.
	TargetDeprecationRemovalVersion ExtensionTarget = "deprecation.removalVersion"
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
		"x-sunset":            TargetDeprecationRemovalVersion,
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
	default:
		return nil
	}
}

// extensionText reads a preserved extension value as a string. Every
// Deprecation field is prose or a version, so a value of any other JSON shape
// is a document meaning something else by the key.
func extensionText(raw ir.RawValue) (string, bool) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", false
	}
	return text, true
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
