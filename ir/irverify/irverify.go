package irverify

import (
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/dexpace/morphic/ir"
)

// Violation is one structural defect found in an ir.Document: an internal
// compiler bug, never a spec-author problem (that is ir.Diagnostic). Code is a
// stable, slash-namespaced string; Path locates the offending node.
type Violation struct {
	// Code is a stable, slash-namespaced identifier for the defect class.
	Code string
	// Message is a human-readable description of the specific defect.
	Message string
	// Path locates the offending node within the document.
	Path string
}

// Verify runs every structural invariant check over doc and returns the
// violations, sorted by (Code, Path) for deterministic output. A structurally
// sound document yields nil.
func Verify(doc *ir.Document) []Violation {
	if doc == nil {
		return []Violation{{Code: "ir/nil-document", Message: "document is nil"}}
	}

	vs := checkRegistryKeys(doc)
	vs = append(vs, checkIDs(doc)...)
	vs = append(vs, checkPrimIDs(doc)...)
	vs = append(vs, checkDiagnostics(doc)...)
	vs = append(vs, runWalkChecks(doc)...)

	// Stable: two violations can share a (Code, Path) — an embedded field
	// contributes no path segment, so two promoted fields of the same name would
	// collide — and a stable sort leaves those in the walk's deterministic order
	// rather than an unspecified one.
	sort.SliceStable(vs, func(i, j int) bool {
		if vs[i].Code != vs[j].Code {
			return vs[i].Code < vs[j].Code
		}
		return vs[i].Path < vs[j].Path
	})
	return vs
}

// walkChecks are the checks that reach their subject through a bounded walk of
// the document. Each returns whether its own walk was cut short, so the flag is
// part of the signature rather than a value a check can quietly drop.
func walkChecks() []func(*ir.Document) ([]Violation, bool) {
	return []func(*ir.Document) ([]Violation, bool){
		checkReferentialIntegrity,
		checkDuplicateIDs,
		checkNaming,
		checkRawPayloads,
		checkProvenance,
		checkIndices,
	}
}

// runWalkChecks runs every walking check and folds their truncation flags into
// one ir/walk-truncated violation.
//
// Truncation is a fact about the document, not about the check that noticed it,
// so it is reported once here rather than once per walk that noticed it — which
// would give one document a violation per walking check, all sharing a code and
// a path. Not reporting it at all is what left the pruning walks silently
// under-checking a too-deep document (GitHub #55): a pruned walk reaches a subset
// of what the unpruned reference walk does, so today that one trips the cap
// first, and depending on that coincidence is exactly what the flag replaces.
func runWalkChecks(doc *ir.Document) []Violation {
	var vs []Violation
	truncated := false
	for _, check := range walkChecks() {
		found, cut := check(doc)
		vs = append(vs, found...)
		truncated = truncated || cut
	}
	if !truncated {
		return vs
	}
	return append(vs, Violation{
		Code:    "ir/walk-truncated",
		Message: "document nests deeper than the bounded verifier walk; part of it went unchecked",
		Path:    ir.DocumentPath,
	})
}

// checkRegistryKeys asserts every entry of each flat, ID-keyed registry
// (Types, Channels, Messages, Auth) is keyed by its own node ID and that the key
// is non-empty (invariant #3). Each registry contributes symmetric empty-*-id and
// *-id-mismatch violations.
//
// A nil type definition is reported rather than dereferenced — Common() panics
// on one — which is what keeps Verify a report-only oracle that never crashes on
// a malformed document. The walk-based checks already tolerate nil entries.
func checkRegistryKeys(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if ir.IsNilTypeDef(td) {
			vs = append(vs, Violation{
				Code:    "ir/nil-type",
				Message: "types registry has a nil type definition",
				Path:    "types[" + string(id) + "]",
			})
			continue
		}
		vs = registryKey(vs, "type", "types", string(id), string(td.Common().ID))
	}
	for id, ch := range doc.Channels {
		vs = registryKey(vs, "channel", "channels", string(id), string(ch.ID))
	}
	for id, msg := range doc.Messages {
		vs = registryKey(vs, "message", "messages", string(id), string(msg.ID))
	}
	for id, scheme := range doc.Auth {
		vs = registryKey(vs, "auth", "auth", string(id), string(scheme.ID))
	}
	return vs
}

// registryKey checks one registry entry: key must be non-empty and equal the
// node's own ID. noun is the diagnostic-code singular ("type", "channel", …) and
// reg is the path/message registry label ("types", "channels", …).
func registryKey(vs []Violation, noun, reg, key, nodeID string) []Violation {
	if key == "" {
		return append(vs, Violation{
			Code:    "ir/empty-" + noun + "-id",
			Message: reg + " registry has an empty key",
			Path:    reg + `[""]`,
		})
	}
	if key != nodeID {
		return append(vs, Violation{
			Code:    "ir/" + noun + "-id-mismatch",
			Message: "registry key " + key + " disagrees with node ID " + nodeID,
			Path:    reg + "[" + key + "]",
		})
	}
	return vs
}

// checkDiagnostics asserts every diagnostic message is well-formed UTF-8
// (invariant #7). A message carrying an ill-formed byte run — as a third-party
// validator emits when it truncates a multibyte rune — reaches
// Document.Diagnostics, and json.Marshal rewrites invalid UTF-8 to U+FFFD, so
// the document stops round-tripping byte-for-byte. Producers coerce messages
// through ir.NewDiagnostic; this check catches any that bypass it. Message is
// the only diagnostic field that carries free-form validator text: a Code may
// embed a validator-supplied rule suffix, but those rule names are bounded
// ASCII identifiers, and Provenance holds line:col or synthetic pointers — so
// neither can carry the ill-formed bytes Message can.
func checkDiagnostics(doc *ir.Document) []Violation {
	var vs []Violation
	for i, d := range doc.Diagnostics {
		if utf8.ValidString(d.Message) {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/diagnostic-invalid-utf8",
			Message: "diagnostic message is not valid UTF-8",
			Path:    "diagnostics[" + strconv.Itoa(i) + "]",
		})
	}
	return vs
}
