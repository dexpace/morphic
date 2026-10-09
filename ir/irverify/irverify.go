package irverify

import (
	"sort"

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
	vs = append(vs, checkPrimKinds(doc)...)
	vs = append(vs, checkAuthKinds(doc)...)
	vs = append(vs, checkUnions(doc)...)
	vs = append(vs, checkVersion(doc)...)
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

// declarations is the identities a document's nodes declare, plus whether the
// walk that read them was cut short.
//
// Reading them costs a full walk, and two checks need them:
// checkReferentialIntegrity to resolve the classes Document keys no map by, and
// checkDuplicateIDs to hold each to being declared once. They are read once per
// run and handed down; the checks that do not need them still take them,
// because one signature is what lets walkChecks be a list.
//
// The zero value says the document declares none, so every OpID, ServiceID and
// GroupID reference reports as dangling. Read one with readDeclarations.
type declarations struct {
	ids       []ir.IDDeclaration
	truncated bool
}

// readDeclarations reads the identities doc's nodes declare. It memoizes
// nothing; runWalkChecks is what calls it once and shares the result.
func readDeclarations(doc *ir.Document) declarations {
	ids, truncated := ir.DeclaredIDs(doc)
	return declarations{ids: ids, truncated: truncated}
}

// walkChecks are the checks that reach their subject through a bounded walk of
// the document. Each returns whether the walk its result rests on was cut short,
// so the flag is part of the signature rather than a value a check can quietly
// drop — whether the check runs that walk itself or reads a declarations value
// walked once for the run.
func walkChecks() []func(*ir.Document, declarations) ([]Violation, bool) {
	return []func(*ir.Document, declarations) ([]Violation, bool){
		checkReferentialIntegrity,
		checkTypeRefs,
		checkDuplicateIDs,
		checkDeclaredIDs,
		checkDeclaredIDShapes,
		checkNaming,
		checkRawPayloads,
		checkProvenance,
		checkIndices,
		checkBigVals,
		checkUTF8,
		checkValues,
		checkEmptyRefs,
	}
}

// runWalkChecks runs every walking check and folds their truncation flags into
// one ir/walk-truncated violation.
//
// Truncation is a fact about the document, not about a check, so it is reported
// once. Not reporting it left the pruning walks silently under-checking a
// too-deep document (GitHub #55).
//
// The seed is decls.truncated rather than false because the declaration walk
// runs here and a function that walks owns its own flag. The checks reading the
// declarations return it too, so seeding from false reports the same today, but
// relying on a callee for a walk performed here is the coincidence #55 was.
func runWalkChecks(doc *ir.Document) []Violation {
	decls := readDeclarations(doc)
	var vs []Violation
	truncated := decls.truncated
	for _, check := range walkChecks() {
		found, cut := check(doc, decls)
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
